package layout

import (
	"strings"
	"testing"

	"github.com/schambon/mdv/internal/doc"
	"github.com/schambon/mdv/internal/md"
)

func render(t *testing.T, src string, opts Options) Document {
	t.Helper()
	if opts.Width == 0 {
		opts.Width = 40
	}
	return Render(md.Parse([]byte(src)), opts)
}

// texts returns each rendered row's visible text.
func texts(d Document) []string {
	out := make([]string, len(d.Lines))
	for i, line := range d.Lines {
		out[i] = line.SearchText
	}
	return out
}

func TestRenderIndentsEveryRow(t *testing.T) {
	d := render(t, "hello\n", Options{})
	if got := texts(d)[0]; got != "  hello" {
		t.Errorf("row = %q, want %q", got, "  hello")
	}
}

// SearchText is the concatenation of the row's spans, so search sees exactly
// what is drawn.
func TestSearchTextMatchesSpans(t *testing.T) {
	d := render(t, "# Head\n\n- **bold** item\n\n> quote\n", Options{})
	for i, line := range d.Lines {
		var sb strings.Builder
		for _, s := range line.Spans {
			sb.WriteString(s.Text)
		}
		if sb.String() != line.SearchText {
			t.Errorf("row %d: spans %q != SearchText %q", i, sb.String(), line.SearchText)
		}
	}
}

func TestRenderPrefixes(t *testing.T) {
	tests := []struct{ name, src, want string }{
		{"heading keeps hashes", "## Title\n", "  ## Title"},
		{"quote bar", "> quoted\n", "  │ quoted"},
		{"list marker", "- item\n", "  - item"},
		{"ordered marker", "1. item\n", "  1. item"},
		{"task marker", "- [x] done\n", "  - [x] done"},
		{"code indents four", "```\ncode\n```\n", "      code"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := texts(render(t, tt.src, Options{}))[0]; got != tt.want {
				t.Errorf("row = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestBlankBlockRendersEmptyRow(t *testing.T) {
	rows := texts(render(t, "a\n\nb\n", Options{}))
	if len(rows) != 3 {
		t.Fatalf("got %d rows, want 3", len(rows))
	}
	if rows[1] != "  " {
		t.Errorf("blank row = %q, want the bare indent", rows[1])
	}
}

func TestRuleFillsContentWidth(t *testing.T) {
	d := render(t, "---\n", Options{Width: 20})
	row := d.Lines[0]
	if got := Width(row.SearchText); got != 20 {
		t.Errorf("rule row width = %d, want 20", got)
	}
	if row.Spans[len(row.Spans)-1].Style != StyleRule {
		t.Error("rule row should use StyleRule")
	}
}

func TestCodeBlockExpandsPerPhysicalRow(t *testing.T) {
	d := render(t, "```\none\ntwo\nthree\n```\n", Options{})
	rows := texts(d)
	want := []string{"      one", "      two", "      three"}
	if len(rows) != len(want) {
		t.Fatalf("got %d rows, want %d: %q", len(rows), len(want), rows)
	}
	for i := range want {
		if rows[i] != want[i] {
			t.Errorf("row %d = %q, want %q", i, rows[i], want[i])
		}
	}
}

// Every expanded code row keeps the whole block's source range.
func TestCodeRowsShareBlockSource(t *testing.T) {
	d := render(t, "```\na\nb\n```\n", Options{})
	for i, line := range d.Lines {
		if line.Source.Start.Line != 1 {
			t.Errorf("code row %d starts at line %d, want 1", i, line.Source.Start.Line)
		}
	}
}

func TestWrappingBreaksBetweenWords(t *testing.T) {
	d := render(t, "alpha beta gamma delta\n", Options{Width: 16})
	rows := texts(d)
	if len(rows) < 2 {
		t.Fatalf("expected wrapping, got %q", rows)
	}
	for i, row := range rows {
		if w := Width(row); w > 16 {
			t.Errorf("row %d %q is %d cells, over the 16 limit", i, row, w)
		}
	}
	if strings.Contains(rows[0], "beta ") && strings.HasSuffix(rows[0], " ") {
		t.Errorf("row should not end in trailing space: %q", rows[0])
	}
}

// Continuation rows reproduce the prefix as spaces so text stays aligned.
func TestWrappedContinuationAlignsUnderPrefix(t *testing.T) {
	d := render(t, "- alpha beta gamma delta epsilon\n", Options{Width: 20})
	rows := texts(d)
	if len(rows) < 2 {
		t.Fatalf("expected wrapping, got %q", rows)
	}
	if !strings.HasPrefix(rows[0], "  - ") {
		t.Errorf("first row = %q, want a list marker", rows[0])
	}
	if !strings.HasPrefix(rows[1], "    ") {
		t.Errorf("continuation row = %q, want it aligned under the marker", rows[1])
	}
}

func TestNoRowExceedsWidth(t *testing.T) {
	src := "# A heading that is quite long indeed\n\n" +
		"A paragraph with a very long unbroken token " +
		"https://example.com/an/extremely/long/path/that/will/not/fit/anywhere\n\n" +
		"- list item with plenty of words to force wrapping behaviour\n\n" +
		"```\nsome code line that is long enough to exceed the available width\n```\n" +
		"| column one | column two | column three |\n|---|---|---|\n| a | b | c |\n"

	for _, width := range []int{10, 16, 24, 40, 80} {
		for _, numbers := range []bool{false, true} {
			d := render(t, src, Options{Width: width, LineNumbers: numbers})
			for i, row := range texts(d) {
				if w := Width(row); w > width {
					t.Errorf("width %d numbers %v: row %d %q is %d cells",
						width, numbers, i, row, w)
				}
			}
		}
	}
}

func TestLineNumberGutter(t *testing.T) {
	d := render(t, "one\ntwo\n", Options{LineNumbers: true})
	if got := texts(d)[0]; got != "     1   one two" {
		t.Errorf("row = %q, want a six-digit gutter", got)
	}
}

// Continuation rows leave the gutter blank rather than repeating the number.
func TestLineNumberGutterBlankOnContinuation(t *testing.T) {
	d := render(t, "alpha beta gamma delta epsilon zeta\n", Options{Width: 26, LineNumbers: true})
	rows := texts(d)
	if len(rows) < 2 {
		t.Fatalf("expected wrapping, got %q", rows)
	}
	if !strings.HasPrefix(rows[0], "     1 ") {
		t.Errorf("first row = %q, want it numbered", rows[0])
	}
	if !strings.HasPrefix(rows[1], strings.Repeat(" ", gutterWidth)) {
		t.Errorf("continuation row = %q, want a blank gutter", rows[1])
	}
}

func TestLineNumberGutterKeepsLastSixDigits(t *testing.T) {
	r := &renderer{opts: Options{LineNumbers: true, Width: 40}}
	if got := r.gutter(1234567, true); got != "234567 " {
		t.Errorf("gutter = %q, want %q", got, "234567 ")
	}
}

func TestInlineStylesApplied(t *testing.T) {
	d := render(t, "plain **bold** `code` *em* ~~gone~~ [l](https://e.com)\n", Options{Width: 80})
	got := map[Style]string{}
	for _, s := range d.Lines[0].Spans {
		got[s.Style] = s.Text
	}
	for style, want := range map[Style]string{
		StyleStrong:     "bold",
		StyleInlineCode: "code",
		StyleEmphasis:   "em",
		StyleStrike:     "gone",
		StyleLink:       "l",
	} {
		if got[style] != want {
			t.Errorf("style %v carried %q, want %q", style, got[style], want)
		}
	}
}

func TestLinkTargetPreserved(t *testing.T) {
	d := render(t, "[label](https://example.com)\n", Options{Width: 60})
	for _, s := range d.Lines[0].Spans {
		if s.Text == "label" {
			if s.LinkTarget != "https://example.com" {
				t.Errorf("LinkTarget = %q", s.LinkTarget)
			}
			return
		}
	}
	t.Error("no span carried the link label")
}

func TestHeadingBaseStyleAppliesToPlainText(t *testing.T) {
	d := render(t, "# Title here\n", Options{})
	for _, s := range d.Lines[0].Spans {
		if strings.Contains(s.Text, "Title") && s.Style != StyleHeading {
			t.Errorf("heading text span has style %v, want StyleHeading", s.Style)
		}
	}
}

func TestMinimumWidthEnforced(t *testing.T) {
	d := render(t, "some text here\n", Options{Width: 1})
	for i, row := range texts(d) {
		if w := Width(row); w > minWidth {
			t.Errorf("row %d %q is %d cells, over the %d minimum", i, row, w, minWidth)
		}
	}
}

func TestNearest(t *testing.T) {
	d := render(t, "one\n\ntwo\n\nthree\n", Options{})
	tests := []struct {
		name       string
		sourceLine int
		wantLine   int
	}{
		{"exact first", 1, 1},
		{"exact middle", 3, 3},
		{"exact last", 5, 5},
		{"before start clamps", -4, 1},
		{"past end clamps", 99, 5},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			row := Nearest(d, tt.sourceLine)
			if got := d.Lines[row].Source.Start.Line; got != tt.wantLine {
				t.Errorf("Nearest(%d) -> row %d at source line %d, want line %d",
					tt.sourceLine, row, got, tt.wantLine)
			}
		})
	}
}

// Ties resolve to the earliest matching row, which keeps reload stable.
func TestNearestPrefersEarliestOnTie(t *testing.T) {
	d := render(t, "```\na\nb\nc\n```\n", Options{})
	if got := Nearest(d, 1); got != 0 {
		t.Errorf("Nearest = %d, want 0", got)
	}
}

func TestNearestEmptyDocument(t *testing.T) {
	if got := Nearest(Document{}, 5); got != 0 {
		t.Errorf("Nearest on empty document = %d, want 0", got)
	}
}

func TestRenderEmptyDocument(t *testing.T) {
	d := Render(doc.Document{}, Options{Width: 40})
	if len(d.Lines) != 0 {
		t.Errorf("got %d rows, want none", len(d.Lines))
	}
}
