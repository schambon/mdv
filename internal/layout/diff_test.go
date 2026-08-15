package layout

import (
	"strings"
	"testing"

	"github.com/schambon/mdv/internal/diffdoc"
)

func buildDiff(a, b string, opts diffdoc.Options) diffdoc.Document {
	return diffdoc.Build(diffdoc.SplitLines([]byte(a)), diffdoc.SplitLines([]byte(b)), opts)
}

var unfolded = diffdoc.Options{Context: -1, WordDiff: true}

// rowText is the visible text of a rendered row.
func rowText(line RenderedLine) string { return line.SearchText }

// checkRendered asserts the invariants shared with the Markdown renderer: no
// row may overflow the frame, and SearchText must equal the row's spans.
func checkRendered(t *testing.T, d DiffDocument, width int) {
	t.Helper()

	// The row mapping must cover every line and never run backwards, or
	// expanding a fold and jumping between hunks would land on the wrong row.
	if len(d.Rows) != len(d.Lines) {
		t.Fatalf("row mapping covers %d of %d lines", len(d.Rows), len(d.Lines))
	}
	for i := 1; i < len(d.Rows); i++ {
		if d.Rows[i] < d.Rows[i-1] {
			t.Fatalf("row mapping goes backwards at line %d: %d then %d", i, d.Rows[i-1], d.Rows[i])
		}
	}

	for i, line := range d.Lines {
		cells := 0
		var sb strings.Builder
		for _, s := range line.Spans {
			cells += s.Cells
			sb.WriteString(s.Text)
			if s.Cells != Width(s.Text) {
				t.Fatalf("row %d: span %q claims %d cells, measures %d", i, s.Text, s.Cells, Width(s.Text))
			}
		}
		if cells > width {
			t.Fatalf("row %d occupies %d cells, want at most %d: %q", i, cells, width, rowText(line))
		}
		if sb.String() != line.SearchText {
			t.Fatalf("row %d: SearchText %q does not match spans %q", i, line.SearchText, sb.String())
		}
	}
}

func TestRenderDiffNoRowExceedsWidth(t *testing.T) {
	a := "short\n\ttabbed line\n" + strings.Repeat("x", 300) + "\nこんにちは世界\n"
	b := "short\n\tchanged line\n" + strings.Repeat("y", 300) + "\nこんにちは\n"

	for _, width := range []int{10, 13, 20, 40, 80, 81, 120, 200} {
		for _, numbers := range []bool{false, true} {
			for _, split := range []bool{false, true} {
				d := RenderDiff(buildDiff(a, b, unfolded), DiffOptions{
					Width: width, LineNumbers: numbers, SideBySide: split,
				})
				checkRendered(t, d, width)
			}
		}
	}
}

func TestRenderDiffFoldedNoRowExceedsWidth(t *testing.T) {
	var a, b strings.Builder
	for i := range 60 {
		line := strings.Repeat("context ", i%4+1) + "\n"
		a.WriteString(line)
		b.WriteString(line)
	}
	changed := strings.Replace(b.String(), "context \n", "CHANGED\n", 1)

	for _, width := range []int{10, 40, 120} {
		d := RenderDiff(
			buildDiff(a.String(), changed, diffdoc.Options{Context: 3, WordDiff: true}),
			DiffOptions{Width: width, LineNumbers: true, SideBySide: true},
		)
		checkRendered(t, d, width)
	}
}

// The divider must land on the same column of every row, or the two panes
// drift apart and the view becomes unreadable.
func TestSideBySidePanesStayAligned(t *testing.T) {
	a := "one\n" + strings.Repeat("long ", 40) + "\nthree\nremoved\n"
	b := "one\n" + strings.Repeat("wide ", 40) + "\nthree\n"

	width := 100
	d := RenderDiff(buildDiff(a, b, unfolded), DiffOptions{
		Width: width, LineNumbers: true, SideBySide: true,
	})
	checkRendered(t, d, width)

	want := -1
	for i, line := range d.Lines {
		at := -1
		cells := 0
		for _, s := range line.Spans {
			if s.Text == cellSeparator {
				at = cells
				break
			}
			cells += s.Cells
		}
		if at < 0 {
			t.Fatalf("row %d has no divider: %q", i, rowText(line))
		}
		if want < 0 {
			want = at
		} else if at != want {
			t.Fatalf("row %d puts the divider at %d, want %d: %q", i, at, want, rowText(line))
		}
	}
}

func TestSideBySideFallsBackWhenNarrow(t *testing.T) {
	// Two panes of fewer than minPaneCells each are less readable than one
	// column, so the request is refused rather than honoured badly.
	d := RenderDiff(buildDiff("aaa\n", "bbb\n", unfolded), DiffOptions{
		Width: 30, SideBySide: true,
	})
	for _, line := range d.Lines {
		if strings.Contains(rowText(line), cellSeparator) {
			t.Fatalf("narrow terminal should not split panes: %q", rowText(line))
		}
	}
}

func TestUnifiedChangedLineBecomesTwoRows(t *testing.T) {
	d := RenderDiff(buildDiff("keep\nold\n", "keep\nnew\n", unfolded), DiffOptions{
		Width: 40, SideBySide: false,
	})

	var markers []string
	for _, line := range d.Lines {
		markers = append(markers, strings.TrimSpace(rowText(line))[:1])
	}
	// keep, -old, +new
	if len(d.Lines) != 3 {
		t.Fatalf("want 3 rows, got %d: %q", len(d.Lines), markers)
	}
	if !strings.HasPrefix(strings.TrimLeft(rowText(d.Lines[1]), " "), "- old") {
		t.Fatalf("second row should be the removal, got %q", rowText(d.Lines[1]))
	}
	if !strings.HasPrefix(strings.TrimLeft(rowText(d.Lines[2]), " "), "+ new") {
		t.Fatalf("third row should be the addition, got %q", rowText(d.Lines[2]))
	}
}

// Indentation is meaningful in a diff: a wrapped or realigned line that loses
// its leading spaces misrepresents the file.
func TestLeadingWhitespacePreserved(t *testing.T) {
	d := RenderDiff(buildDiff("    indented old\n", "    indented new\n", unfolded), DiffOptions{
		Width: 80, SideBySide: false,
	})
	for _, line := range d.Lines {
		if !strings.Contains(rowText(line), "    indented") {
			t.Fatalf("indentation lost: %q", rowText(line))
		}
	}
}

func TestTabsExpanded(t *testing.T) {
	d := RenderDiff(buildDiff("\tone\n", "\ttwo\n", unfolded), DiffOptions{
		Width: 80, SideBySide: false,
	})
	for _, line := range d.Lines {
		if strings.ContainsRune(rowText(line), '\t') {
			t.Fatalf("tab survived into output: %q", rowText(line))
		}
	}
}

func TestExpandTabs(t *testing.T) {
	tests := []struct {
		text  string
		start int
		want  string
		end   int
	}{
		{"", 0, "", 0},
		{"no tabs", 0, "no tabs", 7},
		{"\t", 0, "    ", 4},
		{"a\tb", 0, "a   b", 5},
		{"abc\td", 0, "abc d", 5},
		{"abcd\te", 0, "abcd    e", 9},
		// A tab mid-line advances from the running column, not from zero.
		{"\tx", 2, "  x", 5},
	}
	for _, tt := range tests {
		got, end := expandTabs(tt.text, tt.start)
		if got != tt.want || end != tt.end {
			t.Errorf("expandTabs(%q, %d) = (%q, %d), want (%q, %d)",
				tt.text, tt.start, got, end, tt.want, tt.end)
		}
	}
}

func TestLineNumbersShown(t *testing.T) {
	d := RenderDiff(buildDiff("one\ntwo\n", "one\nTWO\n", unfolded), DiffOptions{
		Width: 100, LineNumbers: true, SideBySide: true,
	})
	if !strings.Contains(rowText(d.Lines[0]), "1") {
		t.Fatalf("first row should carry line 1: %q", rowText(d.Lines[0]))
	}
	if !strings.Contains(rowText(d.Lines[1]), "2") {
		t.Fatalf("second row should carry line 2: %q", rowText(d.Lines[1]))
	}
}

// A wrapped line is still one source line; repeating its number would read as
// a second change.
func TestWrappedRowsDoNotRepeatTheNumber(t *testing.T) {
	long := strings.Repeat("word ", 60)
	d := RenderDiff(buildDiff(long+"\n", long+"tail\n", unfolded), DiffOptions{
		Width: 60, LineNumbers: true, SideBySide: false,
	})
	if len(d.Lines) < 2 {
		t.Fatalf("expected the line to wrap, got %d rows", len(d.Lines))
	}
	if strings.Contains(rowText(d.Lines[1]), "1") {
		t.Fatalf("continuation row repeated a line number: %q", rowText(d.Lines[1]))
	}
}

func TestGutterSizedToWidestNumber(t *testing.T) {
	var a strings.Builder
	for i := range 12 {
		a.WriteString(strings.Repeat("x", i%3+1) + "\n")
	}
	d := buildDiff(a.String(), a.String(), unfolded)

	// 12 lines need two digits plus a space.
	if got := gutterCells(d, true); got != 3 {
		t.Errorf("gutterCells = %d, want 3", got)
	}
	if got := gutterCells(d, false); got != 0 {
		t.Errorf("gutterCells disabled = %d, want 0", got)
	}
	if got := gutterCells(diffdoc.Document{}, true); got != 2 {
		t.Errorf("empty document gutter = %d, want 2", got)
	}
}

func TestFoldRow(t *testing.T) {
	var a strings.Builder
	for i := range 30 {
		a.WriteString(strings.Repeat("y", i%3+1) + "\n")
	}
	d := RenderDiff(
		diffdoc.Build(diffdoc.SplitLines([]byte(a.String())), diffdoc.SplitLines([]byte(a.String())),
			diffdoc.Options{Context: 3}),
		DiffOptions{Width: 60, SideBySide: true},
	)

	if len(d.Lines) != 1 {
		t.Fatalf("an unchanged file should fold to one row, got %d", len(d.Lines))
	}
	text := rowText(d.Lines[0])
	if !strings.Contains(text, "30 unchanged lines") {
		t.Fatalf("fold row should name the count, got %q", text)
	}
	if Width(text) != 60 {
		t.Fatalf("fold row should rule out to the full width, got %d cells", Width(text))
	}
}

func TestFoldRowSingular(t *testing.T) {
	rows := []diffdoc.Row{{Kind: diffdoc.RowFolded, Hidden: []diffdoc.Row{
		{Kind: diffdoc.RowEqual, Left: diffdoc.Line{Text: "x", Number: 1}, Right: diffdoc.Line{Text: "x", Number: 1}},
	}}}
	d := RenderDiff(diffdoc.Document{Rows: rows}, DiffOptions{Width: 40})

	if !strings.Contains(rowText(d.Lines[0]), "1 unchanged line ") {
		t.Fatalf("want the singular noun, got %q", rowText(d.Lines[0]))
	}
}

// Word-level styling must emphasise only what changed, and each side must show
// its own half of the change.
func TestWordDiffStyling(t *testing.T) {
	d := RenderDiff(buildDiff("the quick fox\n", "the slow fox\n", unfolded), DiffOptions{
		Width: 100, SideBySide: true,
	})

	var removedWords, addedWords []string
	for _, s := range d.Lines[0].Spans {
		switch s.Style {
		case StyleDiffRemoveWord:
			removedWords = append(removedWords, s.Text)
		case StyleDiffAddWord:
			addedWords = append(addedWords, s.Text)
		}
	}
	if strings.Join(removedWords, "") != "quick" {
		t.Errorf("emphasised removal %q, want \"quick\"", removedWords)
	}
	if strings.Join(addedWords, "") != "slow" {
		t.Errorf("emphasised addition %q, want \"slow\"", addedWords)
	}
	// The unchanged words must not be swept into the emphasis.
	if strings.Contains(strings.Join(removedWords, ""), "fox") {
		t.Error("unchanged text was emphasised")
	}
}

func TestRowSourcePrefersTheNewSide(t *testing.T) {
	changed := diffdoc.Row{
		Kind:  diffdoc.RowChanged,
		Left:  diffdoc.Line{Text: "a", Number: 4},
		Right: diffdoc.Line{Text: "b", Number: 9},
	}
	if got := rowSource(changed).Start.Line; got != 9 {
		t.Errorf("changed row maps to line %d, want the new side's 9", got)
	}

	removed := diffdoc.Row{Kind: diffdoc.RowRemoved, Left: diffdoc.Line{Text: "a", Number: 4}}
	if got := rowSource(removed).Start.Line; got != 4 {
		t.Errorf("removed row maps to line %d, want the old side's 4", got)
	}

	fold := diffdoc.Row{Kind: diffdoc.RowFolded, Hidden: []diffdoc.Row{removed}}
	if got := rowSource(fold).Start.Line; got != 4 {
		t.Errorf("fold maps to line %d, want its first hidden row's 4", got)
	}
}

// Every rendered row must map to a source line, or the status line and the
// editor key have nothing to work with.
func TestEveryRowCarriesASourceLine(t *testing.T) {
	a := "one\ntwo\nthree\nremoved\n"
	b := "one\nTWO\nthree\n"

	for _, split := range []bool{false, true} {
		d := RenderDiff(buildDiff(a, b, unfolded), DiffOptions{Width: 100, SideBySide: split})
		for i, line := range d.Lines {
			if line.Source.Start.Line <= 0 {
				t.Fatalf("side-by-side=%v row %d has no source line: %q", split, i, rowText(line))
			}
		}
	}
}

func TestPackRuns(t *testing.T) {
	src := []run{{text: "abcdefgh", cells: 8}}

	rows := packRuns(src, 3)
	var got []string
	for _, r := range rows {
		var sb strings.Builder
		for _, rn := range r {
			sb.WriteString(rn.text)
		}
		got = append(got, sb.String())
	}
	if strings.Join(got, "|") != "abc|def|gh" {
		t.Errorf("packRuns split into %q, want abc|def|gh", got)
	}

	// Whitespace is never dropped, unlike prose wrapping.
	rows = packRuns([]run{{text: "  x", cells: 3}}, 10)
	if rows[0][0].text != "  x" {
		t.Errorf("leading spaces lost: %q", rows[0][0].text)
	}

	// A blank line still occupies one row.
	if rows := packRuns(nil, 10); len(rows) != 1 {
		t.Errorf("empty input gave %d rows, want 1", len(rows))
	}
}

func TestPackRunsPreservesEveryRune(t *testing.T) {
	text := "  func(x)\tand more こんにちは trailing  "
	for _, avail := range []int{1, 2, 3, 7, 40} {
		rows := packRuns([]run{{text: text, cells: Width(text)}}, avail)

		var sb strings.Builder
		for _, r := range rows {
			for _, rn := range r {
				sb.WriteString(rn.text)
			}
		}
		if sb.String() != text {
			t.Errorf("avail %d: packRuns rebuilt %q, want %q", avail, sb.String(), text)
		}
	}
}
