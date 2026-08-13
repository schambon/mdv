package editor

import (
	"strings"
	"testing"
)

func equal(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestSplit(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want []string
	}{
		{"single word", "vi", []string{"vi"}},
		{"with flag", "code --wait", []string{"code", "--wait"}},
		{"extra spaces", "  vim   -p  ", []string{"vim", "-p"}},
		{"tabs separate", "vim\t-p", []string{"vim", "-p"}},
		{"absolute path", "/usr/local/bin/nvim", []string{"/usr/local/bin/nvim"}},
		{"double quotes group", `"my editor" --flag`, []string{"my editor", "--flag"}},
		{"single quotes group", `'my editor'`, []string{"my editor"}},
		{"quotes are removed", `vim "a b"`, []string{"vim", "a b"}},
		{"escaped space", `my\ editor`, []string{"my editor"}},
		{"escape inside quotes", `"a\"b"`, []string{`a"b`}},
		{"quote inside other quote", `"it's"`, []string{"it's"}},
		{"adjacent quoted parts", `a"b"c`, []string{"abc"}},
		{"empty quoted argument dropped", `vim "" -p`, []string{"vim", "-p"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Split(tt.in)
			if err != nil {
				t.Fatalf("Split(%q): %v", tt.in, err)
			}
			if !equal(got, tt.want) {
				t.Errorf("Split(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

// Every shell operator must be rejected, since mdv never runs a shell.
func TestSplitRejectsShellOperators(t *testing.T) {
	for _, in := range []string{
		"vim; rm -rf /",
		"vim | tee log",
		"vim & disown",
		"vim > out",
		"vim < in",
		"vim `whoami`",
		"vim $HOME/file",
		"vim $(whoami)",
		"vim (a)",
		"vim {a}",
		"vim\nrm",
		"vim\rrm",
	} {
		if got, err := Split(in); err == nil {
			t.Errorf("Split(%q) = %q, want an error", in, got)
		}
	}
}

// Quoting does not launder an operator into an executable position, but it does
// let one appear as literal text in an argument.
func TestSplitAllowsQuotedOperators(t *testing.T) {
	got, err := Split(`vim "a;b"`)
	if err != nil {
		t.Fatalf("Split: %v", err)
	}
	if !equal(got, []string{"vim", "a;b"}) {
		t.Errorf("Split = %q", got)
	}
}

func TestSplitErrors(t *testing.T) {
	tests := []struct{ name, in string }{
		{"empty", ""},
		{"only spaces", "   "},
		{"unterminated double quote", `vim "abc`},
		{"unterminated single quote", `vim 'abc`},
		{"trailing backslash", `vim \`},
		{"only an empty quoted argument", `""`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got, err := Split(tt.in); err == nil {
				t.Errorf("Split(%q) = %q, want an error", tt.in, got)
			}
		})
	}
}

func TestCommandAdapters(t *testing.T) {
	tests := []struct {
		editor string
		want   []string
	}{
		{"vi", []string{"vi", "+42", "/tmp/a.md"}},
		{"vim", []string{"vim", "+42", "/tmp/a.md"}},
		{"nvim", []string{"nvim", "+42", "/tmp/a.md"}},
		{"nano", []string{"nano", "+42", "/tmp/a.md"}},
		{"emacs", []string{"emacs", "+42:1", "/tmp/a.md"}},
		{"emacsclient", []string{"emacsclient", "+42:1", "/tmp/a.md"}},
		{"code", []string{"code", "--goto", "/tmp/a.md:42:1"}},
		{"codium", []string{"codium", "--goto", "/tmp/a.md:42:1"}},
		{"subl", []string{"subl", "/tmp/a.md:42"}},
		{"mate", []string{"mate", "/tmp/a.md:42"}},
		{"ed", []string{"ed", "/tmp/a.md"}},
		{"helix", []string{"helix", "/tmp/a.md"}},
	}
	for _, tt := range tests {
		t.Run(tt.editor, func(t *testing.T) {
			env := func(key string) string {
				if key == "EDITOR" {
					return tt.editor
				}
				return ""
			}
			got, err := Command(env, "/tmp/a.md", 42)
			if err != nil {
				t.Fatalf("Command: %v", err)
			}
			if !equal(got, tt.want) {
				t.Errorf("Command = %q, want %q", got, tt.want)
			}
		})
	}
}

// The adapter is chosen by basename, so a full path or a .exe suffix still
// resolves to the right editor.
func TestCommandAdapterFromPathAndExtension(t *testing.T) {
	tests := []struct {
		editor string
		want   []string
	}{
		{"/usr/local/bin/nvim", []string{"/usr/local/bin/nvim", "+7", "/tmp/a.md"}},
		{"/opt/homebrew/bin/code", []string{"/opt/homebrew/bin/code", "--goto", "/tmp/a.md:7:1"}},
		{"vim.exe", []string{"vim.exe", "+7", "/tmp/a.md"}},
	}
	for _, tt := range tests {
		t.Run(tt.editor, func(t *testing.T) {
			env := func(string) string { return tt.editor }
			got, _ := Command(env, "/tmp/a.md", 7)
			if !equal(got, tt.want) {
				t.Errorf("Command = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestCommandKeepsExtraArguments(t *testing.T) {
	env := func(string) string { return "code --wait --new-window" }
	got, err := Command(env, "/tmp/a.md", 3)
	if err != nil {
		t.Fatalf("Command: %v", err)
	}
	want := []string{"code", "--wait", "--new-window", "--goto", "/tmp/a.md:3:1"}
	if !equal(got, want) {
		t.Errorf("Command = %q, want %q", got, want)
	}
}

func TestCommandPrecedence(t *testing.T) {
	tests := []struct {
		name           string
		visual, editor string
		want           string
	}{
		{"visual wins", "nvim", "nano", "nvim"},
		{"editor when visual is unset", "", "nano", "nano"},
		{"editor when visual is blank", "   ", "nano", "nano"},
		{"vi when both are unset", "", "", "vi"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			env := func(key string) string {
				switch key {
				case "VISUAL":
					return tt.visual
				case "EDITOR":
					return tt.editor
				}
				return ""
			}
			got, err := Command(env, "/tmp/a.md", 1)
			if err != nil {
				t.Fatalf("Command: %v", err)
			}
			if got[0] != tt.want {
				t.Errorf("editor = %q, want %q", got[0], tt.want)
			}
		})
	}
}

func TestCommandClampsLine(t *testing.T) {
	env := func(string) string { return "vim" }
	for _, line := range []int{0, -1} {
		got, _ := Command(env, "/tmp/a.md", line)
		if got[1] != "+1" {
			t.Errorf("line %d produced %q, want +1", line, got[1])
		}
	}
}

func TestCommandPropagatesSplitErrors(t *testing.T) {
	env := func(string) string { return "vim; rm -rf /" }
	if _, err := Command(env, "/tmp/a.md", 1); err == nil {
		t.Error("Command accepted a command with a shell operator")
	} else if !strings.Contains(err.Error(), "unsupported shell character") {
		t.Errorf("error = %v", err)
	}
}
