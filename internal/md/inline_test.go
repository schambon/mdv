package md

import (
	"testing"

	"github.com/schambon/mdv/internal/doc"
)

// plainPos maps an offset to a position on a single notional line, which is
// enough for tests that only care about inline structure.
func plainPos(offset int) doc.Position {
	return doc.Position{ByteOffset: offset, Line: 1, Column: offset + 1}
}

type want struct {
	kind   doc.InlineKind
	text   string
	target string
}

func check(t *testing.T, input string, wants ...want) {
	t.Helper()
	got := ParseInline(input, 0, plainPos)
	if len(got) != len(wants) {
		t.Fatalf("ParseInline(%q) returned %d inlines, want %d: %+v", input, len(got), len(wants), got)
	}
	for i, w := range wants {
		if got[i].Kind != w.kind || got[i].Text != w.text || got[i].Target != w.target {
			t.Errorf("ParseInline(%q)[%d] = {kind %v, text %q, target %q}, want {kind %v, text %q, target %q}",
				input, i, got[i].Kind, got[i].Text, got[i].Target, w.kind, w.text, w.target)
		}
	}
}

func TestInlinePlainText(t *testing.T) {
	check(t, "plain words", want{doc.InlineText, "plain words", ""})
}

func TestInlinePairedMarkers(t *testing.T) {
	check(t, "**bold**", want{doc.InlineStrong, "bold", ""})
	check(t, "__bold__", want{doc.InlineStrong, "bold", ""})
	check(t, "*em*", want{doc.InlineEmphasis, "em", ""})
	check(t, "_em_", want{doc.InlineEmphasis, "em", ""})
	check(t, "~~gone~~", want{doc.InlineStrike, "gone", ""})
	check(t, "`code`", want{doc.InlineCode, "code", ""})
}

// The two-byte markers are tried before the one-byte markers they start with.
func TestInlineStrongBeatsEmphasis(t *testing.T) {
	check(t, "**b**", want{doc.InlineStrong, "b", ""})
}

func TestInlineMixedRun(t *testing.T) {
	check(t, "a *b* c",
		want{doc.InlineText, "a ", ""},
		want{doc.InlineEmphasis, "b", ""},
		want{doc.InlineText, " c", ""},
	)
}

func TestInlineUnmatchedDelimitersStayLiteral(t *testing.T) {
	check(t, "*unclosed", want{doc.InlineText, "*unclosed", ""})
	check(t, "a ** b", want{doc.InlineText, "a ** b", ""})
	check(t, "`unclosed", want{doc.InlineText, "`unclosed", ""})
}

// Contents are stored verbatim: there is no recursive parsing.
func TestInlineContentsNotRecursive(t *testing.T) {
	check(t, "**bold *inner* here**", want{doc.InlineStrong, "bold *inner* here", ""})
}

func TestInlineLink(t *testing.T) {
	check(t, "[label](https://example.com)",
		want{doc.InlineLink, "label", "https://example.com"})
}

func TestInlineLinkWithSurroundingText(t *testing.T) {
	check(t, "see [here](/docs) now",
		want{doc.InlineText, "see ", ""},
		want{doc.InlineLink, "here", "/docs"},
		want{doc.InlineText, " now", ""},
	)
}

func TestInlineMalformedLinkStaysLiteral(t *testing.T) {
	check(t, "[label](unclosed", want{doc.InlineText, "[label](unclosed", ""})
	check(t, "[label]", want{doc.InlineText, "[label]", ""})
}

func TestInlineBareURL(t *testing.T) {
	check(t, "https://example.com",
		want{doc.InlineLink, "https://example.com", "https://example.com"})
	check(t, "http://example.com/a",
		want{doc.InlineLink, "http://example.com/a", "http://example.com/a"})
}

// Trailing sentence punctuation is not part of the URL.
func TestInlineBareURLExcludesTrailingPunctuation(t *testing.T) {
	check(t, "see https://example.com.",
		want{doc.InlineText, "see ", ""},
		want{doc.InlineLink, "https://example.com", "https://example.com"},
		want{doc.InlineText, ".", ""},
	)
}

func TestInlineBareURLIsLowercaseOnly(t *testing.T) {
	check(t, "HTTPS://EXAMPLE.COM", want{doc.InlineText, "HTTPS://EXAMPLE.COM", ""})
}

func TestInlineNoEscapeProcessing(t *testing.T) {
	check(t, `\*not em\*`,
		want{doc.InlineText, `\`, ""},
		want{doc.InlineEmphasis, `not em\`, ""},
	)
}

func TestInlineSourceOffsets(t *testing.T) {
	got := ParseInline("ab *em*", 100, plainPos)
	if len(got) != 2 {
		t.Fatalf("got %d inlines, want 2", len(got))
	}
	if got[0].Source.Start.ByteOffset != 100 {
		t.Errorf("text starts at %d, want 100", got[0].Source.Start.ByteOffset)
	}
	if got[1].Source.Start.ByteOffset != 103 {
		t.Errorf("emphasis starts at %d, want 103", got[1].Source.Start.ByteOffset)
	}
	if got[1].Source.End.ByteOffset != 107 {
		t.Errorf("emphasis ends at %d, want 107", got[1].Source.End.ByteOffset)
	}
}

func TestInlineEmptyInput(t *testing.T) {
	if got := ParseInline("", 0, plainPos); len(got) != 0 {
		t.Errorf("got %d inlines, want none", len(got))
	}
}

// Whatever the input, the scan must consume every byte exactly once and never
// stall: the inline source ranges tile the input contiguously.
func TestInlineConsumesEveryByteExactlyOnce(t *testing.T) {
	inputs := []string{
		"a*b_c~d`e[f](g)h",
		"****",
		"[](_)",
		"~~~~a",
		"h ht htt http http:/ http://x",
		"**a* b",
		"[a](b)[c](d)",
		"_*~`[]()_",
		"héllo — ünïcode",
	}
	for _, in := range inputs {
		t.Run(in, func(t *testing.T) {
			next := 0
			for i, out := range ParseInline(in, 0, plainPos) {
				if out.Source.Start.ByteOffset != next {
					t.Fatalf("inline %d starts at %d, want %d (gap or overlap)",
						i, out.Source.Start.ByteOffset, next)
				}
				if out.Source.End.ByteOffset <= next {
					t.Fatalf("inline %d ends at %d, did not advance past %d",
						i, out.Source.End.ByteOffset, next)
				}
				next = out.Source.End.ByteOffset
			}
			if next != len(in) {
				t.Errorf("consumed %d bytes, want %d", next, len(in))
			}
		})
	}
}
