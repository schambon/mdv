package layout

import (
	"strings"
	"testing"
)

const frontMatter = "---\n" +
	"id: doc:kb-conventions\n" +
	"type: doc\n" +
	"status: draft\n" +
	"updated: 2026-09-12\n" +
	"---\n"

func TestFrontMatterAlignsValuesInOneColumn(t *testing.T) {
	rows := texts(render(t, frontMatter, Options{Width: 40}))

	want := []string{
		"  id       doc:kb-conventions",
		"  type     doc",
		"  status   draft",
		"  updated  2026-09-12",
	}
	for i, w := range want {
		if got := rows[i+1]; got != w {
			t.Errorf("row %d = %q, want %q", i+1, got, w)
		}
	}
}

// The rules around the block come from the delimiter lines themselves, so the
// header reads as a header rather than as body text.
func TestFrontMatterIsBracketedByRules(t *testing.T) {
	d := render(t, frontMatter, Options{Width: 40})
	for _, i := range []int{0, len(d.Lines) - 1} {
		if !strings.Contains(d.Lines[i].SearchText, "─") {
			t.Errorf("row %d = %q, want a rule", i, d.Lines[i].SearchText)
		}
	}
}

func TestFrontMatterKeysAreStyled(t *testing.T) {
	d := render(t, frontMatter, Options{Width: 40})
	line := d.Lines[1]

	var key, value *Span
	for i := range line.Spans {
		switch line.Spans[i].Style {
		case StyleMetaKey:
			key = &line.Spans[i]
		case StyleNone:
			if strings.TrimSpace(line.Spans[i].Text) != "" {
				value = &line.Spans[i]
			}
		}
	}
	if key == nil || key.Text != "id" {
		t.Fatalf("key span = %v, want %q styled StyleMetaKey", key, "id")
	}
	if value == nil || value.Text != "doc:kb-conventions" {
		t.Errorf("value span = %v, want the unstyled value", value)
	}
}

// A long value wraps to the value column, not back to the margin.
func TestFrontMatterValueWrapsWithAHangingIndent(t *testing.T) {
	src := "---\nkey: one two three four five six seven eight nine ten\nother: x\n---\n"
	rows := texts(render(t, src, Options{Width: 24}))

	if !strings.HasPrefix(rows[1], "  key    one") {
		t.Fatalf("first row = %q", rows[1])
	}
	if !strings.HasPrefix(rows[2], "         ") || strings.TrimSpace(rows[2]) == "" {
		t.Errorf("continuation row = %q, want an indent to the value column", rows[2])
	}
}

// A key with no value must not leave trailing whitespace on the row: that
// would show up as a stray match in search and as padding in a copy-paste.
func TestFrontMatterEmptyValueLeavesNoTrailingSpace(t *testing.T) {
	rows := texts(render(t, "---\ntags:\n  - one\n---\n", Options{Width: 40}))
	if rows[1] != "  tags" {
		t.Errorf("row = %q, want %q", rows[1], "  tags")
	}
}

// A line the scanner could not split renders under the value column, so a
// nested list reads as belonging to the key above it.
func TestFrontMatterContinuationSitsInTheValueColumn(t *testing.T) {
	rows := texts(render(t, "---\ntags:\n  - one\n---\n", Options{Width: 40}))
	if rows[2] != "        - one" {
		t.Errorf("row = %q, want %q", rows[2], "        - one")
	}
}

func TestFrontMatterNoRowExceedsWidth(t *testing.T) {
	src := "---\n" +
		"id: doc:kb-conventions\n" +
		"a-very-long-key-that-will-not-fit-in-a-narrow-terminal: value\n" +
		"url: https://example.com/an/extremely/long/path/that/will/not/fit\n" +
		"---\n\n# Body\n"

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

func TestFrontMatterSearchTextMatchesSpans(t *testing.T) {
	d := render(t, "---\nid: one\ntags:\n  - two\n---\n", Options{Width: 40})
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

// Every meta row keeps its own source line, which is what v and position
// restoration across a resize rely on.
func TestFrontMatterRowsKeepTheirOwnSource(t *testing.T) {
	d := render(t, frontMatter, Options{Width: 40})
	for i, want := range []int{2, 3, 4, 5} {
		if got := d.Lines[i+1].Source.Start.Line; got != want {
			t.Errorf("row %d source line = %d, want %d", i+1, got, want)
		}
	}
}
