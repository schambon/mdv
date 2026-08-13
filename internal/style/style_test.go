package style

import (
	"os"
	"strings"
	"testing"

	"github.com/schambon/mdv/internal/layout"
)

func TestApplyWrapsInSGR(t *testing.T) {
	s := New(ThemeDark, true)
	got := s.Apply("text", layout.StyleStrong)
	if !strings.HasPrefix(got, "\x1b[") || !strings.HasSuffix(got, reset) {
		t.Errorf("Apply = %q, want an SGR-wrapped string", got)
	}
	if !strings.Contains(got, "text") {
		t.Errorf("Apply = %q, lost the text", got)
	}
}

func TestApplyDisabledEmitsPlainText(t *testing.T) {
	s := New(ThemeDark, false)
	for _, style := range []layout.Style{
		layout.StyleHeading, layout.StyleStrong, layout.StyleLink, layout.StyleCode,
	} {
		if got := s.Apply("text", style); got != "text" {
			t.Errorf("Apply(style %v) = %q, want plain text", style, got)
		}
	}
}

func TestApplyStyleNoneIsPlain(t *testing.T) {
	if got := New(ThemeDark, true).Apply("text", layout.StyleNone); got != "text" {
		t.Errorf("Apply = %q, want plain text", got)
	}
}

func TestEveryStyleHasAnEntryInBothPalettes(t *testing.T) {
	all := []layout.Style{
		layout.StyleNone, layout.StyleHeading, layout.StyleEmphasis, layout.StyleStrong,
		layout.StyleStrike, layout.StyleCode, layout.StyleInlineCode, layout.StyleQuote,
		layout.StyleRule, layout.StyleLink, layout.StyleSearch, layout.StyleSearchActive,
		layout.StyleStatus,
	}
	for _, style := range all {
		if _, ok := darkPalette[style]; !ok {
			t.Errorf("dark palette is missing style %v", style)
		}
		if _, ok := lightPalette[style]; !ok {
			t.Errorf("light palette is missing style %v", style)
		}
	}
}

// The two themes are genuinely different, not one map behind two names.
func TestPalettesDiffer(t *testing.T) {
	same := 0
	for style, dark := range darkPalette {
		if lightPalette[style] == dark {
			same++
		}
	}
	if same == len(darkPalette) {
		t.Error("light and dark palettes are identical")
	}

	for _, style := range []layout.Style{layout.StyleHeading, layout.StyleLink, layout.StyleCode} {
		if darkPalette[style] == lightPalette[style] {
			t.Errorf("style %v is the same in both themes", style)
		}
	}
}

func TestAutoResolvesFromCOLORFGBG(t *testing.T) {
	tests := []struct {
		name  string
		value string
		set   bool
		want  Theme
	}{
		{"unset defaults to dark", "", false, ThemeDark},
		{"black background", "15;0", true, ThemeDark},
		{"dark background", "15;1", true, ThemeDark},
		{"bright black background", "15;8", true, ThemeDark},
		{"white background", "0;15", true, ThemeLight},
		{"light grey background", "0;7", true, ThemeLight},
		{"unparseable defaults to dark", "nonsense", true, ThemeDark},
		{"empty defaults to dark", "", true, ThemeDark},
		{"three fields uses the last", "15;default;0", true, ThemeDark},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// t.Setenv registers the restore hook even when the test then
			// removes the variable entirely.
			t.Setenv("COLORFGBG", tt.value)
			if !tt.set {
				os.Unsetenv("COLORFGBG")
			}
			if got := New(ThemeAuto, true).Theme(); got != tt.want {
				t.Errorf("theme = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestExplicitThemesIgnoreCOLORFGBG(t *testing.T) {
	t.Setenv("COLORFGBG", "0;15") // a light background
	if got := New(ThemeDark, true).Theme(); got != ThemeDark {
		t.Errorf("explicit dark resolved to %q", got)
	}
	if got := New(ThemeLight, true).Theme(); got != ThemeLight {
		t.Errorf("explicit light resolved to %q", got)
	}
}

func TestSpanEmitsHyperlink(t *testing.T) {
	s := New(ThemeDark, true)
	got := s.Span(layout.Span{Text: "label", Style: layout.StyleLink, LinkTarget: "https://example.com"})
	if !strings.Contains(got, "\x1b]8;;https://example.com") {
		t.Errorf("Span = %q, want an OSC 8 hyperlink", got)
	}
}

// Disabling colour must not disable hyperlinks: they are separate capabilities.
func TestSpanKeepsHyperlinkWithoutColour(t *testing.T) {
	s := New(ThemeDark, false)
	got := s.Span(layout.Span{Text: "label", Style: layout.StyleLink, LinkTarget: "https://example.com"})
	if !strings.Contains(got, "\x1b]8;;https://example.com") {
		t.Errorf("Span = %q, want the hyperlink retained", got)
	}
	if strings.Contains(got, "\x1b[") {
		t.Errorf("Span = %q, want no SGR sequences", got)
	}
}

func TestSpanUnsafeTargetIsNotLinked(t *testing.T) {
	s := New(ThemeDark, false)
	got := s.Span(layout.Span{Text: "label", Style: layout.StyleLink, LinkTarget: "./local.md"})
	if got != "label" {
		t.Errorf("Span = %q, want the bare label", got)
	}
}

func TestLineConcatenatesSpans(t *testing.T) {
	s := New(ThemeDark, false)
	line := layout.RenderedLine{Spans: []layout.Span{
		{Text: "a "}, {Text: "b", Style: layout.StyleStrong}, {Text: " c"},
	}}
	if got := s.Line(line); got != "a b c" {
		t.Errorf("Line = %q, want %q", got, "a b c")
	}
}
