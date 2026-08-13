package layout

import (
	"strings"
	"testing"
)

const simpleTable = "| a | bb |\n|---|----|\n| ccc | d |\n"

func TestTableRowsAndRule(t *testing.T) {
	rows := texts(render(t, simpleTable, Options{Width: 40}))
	if len(rows) != 3 {
		t.Fatalf("got %d rows, want header, rule, body: %q", len(rows), rows)
	}
	if !strings.Contains(rows[0], "a") || !strings.Contains(rows[0], "bb") {
		t.Errorf("header = %q", rows[0])
	}
	if !strings.Contains(rows[1], "─") || !strings.Contains(rows[1], "┼") {
		t.Errorf("rule row = %q, want box drawing", rows[1])
	}
	if !strings.Contains(rows[2], "ccc") {
		t.Errorf("body row = %q", rows[2])
	}
}

// Adjacent rows form one sizing group, so columns line up across the table.
func TestTableColumnsShareWidths(t *testing.T) {
	d := render(t, simpleTable, Options{Width: 40})
	header, body := d.Lines[0], d.Lines[2]
	if Width(header.SearchText) != Width(body.SearchText) {
		t.Errorf("header %q and body %q have different widths",
			header.SearchText, body.SearchText)
	}
	if strings.Index(header.SearchText, "│") != strings.Index(body.SearchText, "│") {
		t.Errorf("separators not aligned:\n%q\n%q", header.SearchText, body.SearchText)
	}
}

func TestTableHeaderIsBold(t *testing.T) {
	d := render(t, simpleTable, Options{Width: 40})
	found := false
	for _, s := range d.Lines[0].Spans {
		if strings.TrimSpace(s.Text) == "a" {
			found = true
			if s.Style != StyleStrong {
				t.Errorf("header cell style = %v, want StyleStrong", s.Style)
			}
		}
	}
	if !found {
		t.Error("header cell not found")
	}
}

func TestTableBodyIsNotBold(t *testing.T) {
	d := render(t, simpleTable, Options{Width: 40})
	for _, s := range d.Lines[2].Spans {
		if strings.TrimSpace(s.Text) == "ccc" && s.Style == StyleStrong {
			t.Error("body cell should not be bold")
		}
	}
}

// Inline markup inside a cell is parsed, and its style overrides the row base.
func TestTableCellsCarryInlineMarkup(t *testing.T) {
	src := "| **b** | [l](https://e.com) |\n|---|---|\n| `c` | *e* |\n"
	d := render(t, src, Options{Width: 60})

	styles := map[Style]string{}
	targets := map[string]string{}
	for _, line := range d.Lines {
		for _, s := range line.Spans {
			if text := strings.TrimSpace(s.Text); text != "" {
				styles[s.Style] = text
				if s.LinkTarget != "" {
					targets[text] = s.LinkTarget
				}
			}
		}
	}

	for style, want := range map[Style]string{
		StyleInlineCode: "c",
		StyleEmphasis:   "e",
	} {
		if styles[style] != want {
			t.Errorf("style %v carried %q, want %q", style, styles[style], want)
		}
	}
	if targets["l"] != "https://e.com" {
		t.Errorf("link target in cell = %q", targets["l"])
	}
	// A bold cell in the header is bold either way; check it parsed as markup
	// rather than leaving the asterisks visible.
	for _, line := range d.Lines {
		if strings.Contains(line.SearchText, "**") {
			t.Errorf("row %q still shows delimiters", line.SearchText)
		}
	}
}

func TestTableShrinksWidestColumnFirst(t *testing.T) {
	src := "| short | a very long column indeed |\n|---|---|\n| x | y |\n"
	d := render(t, src, Options{Width: 30})

	if !strings.Contains(d.Lines[0].SearchText, "short") {
		t.Errorf("narrow column was clipped before the wide one: %q", d.Lines[0].SearchText)
	}
	for i, row := range texts(d) {
		if w := Width(row); w > 30 {
			t.Errorf("row %d %q is %d cells, over 30", i, row, w)
		}
	}
}

func TestTableColumnsNotShrunkBelowMinimum(t *testing.T) {
	rows := [][]cell{{{width: 20}, {width: 20}, {width: 20}}}
	widths := columnWidths(rows, 3, 10)
	for c, w := range widths {
		if w < minColumnCells {
			t.Errorf("column %d = %d, want at least %d", c, w, minColumnCells)
		}
	}
}

func TestTableFitsWhenPossible(t *testing.T) {
	rows := [][]cell{{{width: 5}, {width: 5}}}
	widths := columnWidths(rows, 2, 40)
	if widths[0] != 5 || widths[1] != 5 {
		t.Errorf("widths = %v, want natural widths preserved", widths)
	}
}

func TestTableRaggedRowsPadToColumnCount(t *testing.T) {
	src := "| a | b | c |\n|---|---|---|\n| only |\n"
	d := render(t, src, Options{Width: 40})
	if len(d.Lines) != 3 {
		t.Fatalf("got %d rows, want 3", len(d.Lines))
	}
	if Width(d.Lines[0].SearchText) != Width(d.Lines[2].SearchText) {
		t.Errorf("ragged row %q not padded to match header %q",
			d.Lines[2].SearchText, d.Lines[0].SearchText)
	}
}

// Two tables separated by a blank line size independently.
func TestSeparateTablesSizeIndependently(t *testing.T) {
	src := "| a |\n|---|\n| b |\n\n| wide column here |\n|---|\n| x |\n"
	d := render(t, src, Options{Width: 40})

	first := Width(d.Lines[0].SearchText)
	last := Width(d.Lines[len(d.Lines)-1].SearchText)
	if first == last {
		t.Errorf("tables share a width (%d), want independent sizing", first)
	}
}

func TestTableCellsClippedNotWrapped(t *testing.T) {
	src := "| a long cell value that will not fit |\n|---|\n| x |\n"
	d := render(t, src, Options{Width: 20})
	if len(d.Lines) != 3 {
		t.Errorf("got %d rows, want the cell clipped into 3 rows, not wrapped", len(d.Lines))
	}
}
