package link

import (
	"strings"
	"testing"
)

func TestValid(t *testing.T) {
	tests := []struct {
		name   string
		target string
		want   bool
	}{
		{"http", "http://example.com", true},
		{"https", "https://example.com/path?q=1#frag", true},
		{"mailto", "mailto:someone@example.com", true},
		{"empty", "", false},
		{"relative path", "./docs/other.md", false},
		{"bare filename", "other.md", false},
		{"absolute path", "/etc/passwd", false},
		{"anchor", "#section", false},
		{"file scheme", "file:///etc/passwd", false},
		{"javascript scheme", "javascript:alert(1)", false},
		{"data scheme", "data:text/html,<script>", false},
		{"uppercase scheme is normalized by url.Parse", "HTTPS://example.com", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := Valid(tt.target); got != tt.want {
				t.Errorf("Valid(%q) = %v, want %v", tt.target, got, tt.want)
			}
		})
	}
}

// A target containing control bytes could break out of the OSC 8 sequence.
func TestValidRejectsControlCharacters(t *testing.T) {
	for _, target := range []string{
		"https://example.com\x07",
		"https://example.com\x1b]8;;",
		"https://exa\x00mple.com",
		"https://example.com\n",
		"https://example.com\x7f",
	} {
		if Valid(target) {
			t.Errorf("Valid(%q) = true, want false", target)
		}
	}
}

func TestWrapValidTarget(t *testing.T) {
	got := Wrap("label", "https://example.com")
	want := "\x1b]8;;https://example.com\x1b\\label\x1b]8;;\x1b\\"
	if got != want {
		t.Errorf("Wrap = %q, want %q", got, want)
	}
}

func TestWrapInvalidTargetReturnsLabelUnchanged(t *testing.T) {
	for _, target := range []string{"", "./local.md", "javascript:alert(1)"} {
		if got := Wrap("label", target); got != "label" {
			t.Errorf("Wrap(label, %q) = %q, want the bare label", target, got)
		}
	}
}

func TestClassify(t *testing.T) {
	tests := []struct {
		name   string
		target string
		kind   Kind
		path   string
	}{
		{"plain relative file", "other.md", KindRelative, "other.md"},
		{"explicitly relative", "./other.md", KindRelative, "./other.md"},
		{"parent directory", "../docs/other.md", KindRelative, "../docs/other.md"},
		{"absolute path", "/tmp/other.md", KindRelative, "/tmp/other.md"},
		{"fragment is dropped", "other.md#section", KindRelative, "other.md"},
		{"percent escapes are decoded", "my%20notes.md", KindRelative, "my notes.md"},
		{"bare fragment has no path", "#section", KindRelative, ""},
		{"malformed escape", "bad%zz.md", KindNone, ""},

		{"http", "http://example.com", KindExternal, ""},
		{"https", "https://example.com/a/b", KindExternal, ""},
		{"mailto", "mailto:someone@example.com", KindExternal, ""},

		{"empty", "", KindNone, ""},
		{"file scheme is not a path", "file:///etc/passwd", KindNone, ""},
		{"unknown scheme", "ftp://example.com", KindNone, ""},
		{"javascript", "javascript:alert(1)", KindNone, ""},
		{"network-path reference", "//example.com/x.md", KindNone, ""},
		{"control character", "other\x1b.md", KindNone, ""},
		{"newline", "other\n.md", KindNone, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			kind, path := Classify(tt.target)
			if kind != tt.kind || path != tt.path {
				t.Errorf("Classify(%q) = %v, %q, want %v, %q",
					tt.target, kind, path, tt.kind, tt.path)
			}
		})
	}
}

// Classify decides how to follow a target; Valid decides what may be dressed
// as an OSC 8 hyperlink. A relative target is followable but not clickable by
// the terminal, and keeping the two apart is the point of having both.
func TestClassifyAndValidDisagreeOnRelativeTargets(t *testing.T) {
	const target = "other.md"
	if kind, _ := Classify(target); kind != KindRelative {
		t.Errorf("Classify(%q) is not relative", target)
	}
	if Valid(target) {
		t.Errorf("Valid(%q) = true, want false", target)
	}
}

func TestOpenCloseAreSTTerminated(t *testing.T) {
	open := Open("https://example.com")
	if !strings.HasPrefix(open, "\x1b]8;;") || !strings.HasSuffix(open, "\x1b\\") {
		t.Errorf("Open = %q", open)
	}
	if got := Close(); got != "\x1b]8;;\x1b\\" {
		t.Errorf("Close = %q", got)
	}
}
