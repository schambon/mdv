package layout

import (
	"strings"
	"testing"

	"github.com/schambon/mdv/internal/diffdoc"
	"github.com/schambon/mdv/internal/md"
)

func buildMDDiff(a, b string, opts diffdoc.Options) diffdoc.Document {
	return diffdoc.BuildMarkdown(md.Parse([]byte(a)), md.Parse([]byte(b)), opts)
}

var mdUnfolded = diffdoc.Options{Context: -1, WordDiff: true}

func renderedText(d DiffDocument) string {
	var sb strings.Builder
	for _, line := range d.Lines {
		sb.WriteString(line.SearchText + "\n")
	}
	return sb.String()
}

// hasDivider reports whether a row is the full-width rule that brackets a
// split section.
func hasDivider(line RenderedLine) bool {
	return strings.TrimSpace(line.SearchText) != "" &&
		strings.Trim(line.SearchText, "─") == ""
}

func TestMarkdownDiffNoRowExceedsWidth(t *testing.T) {
	a := "# Title\n\nA paragraph that is quite long and will need to wrap at " +
		"most of the widths under test here.\n\n- item one\n- item two\n\n" +
		"| a | b |\n|---|---|\n| 1 | 2 |\n\n```\nsome code\n```\n"
	b := strings.Replace(a, "item one", "item ONE changed", 1)
	b = strings.Replace(b, "| 1 | 2 |", "| 1 | 9 |", 1)

	for _, width := range []int{10, 20, 40, 80, 120, 200} {
		for _, numbers := range []bool{false, true} {
			for _, split := range []bool{false, true} {
				d := RenderDiff(buildMDDiff(a, b, mdUnfolded), DiffOptions{
					Width: width, LineNumbers: numbers, SideBySide: split,
				})
				checkRendered(t, d, width)
			}
		}
	}
}

// Unchanged Markdown is drawn once across the whole width: both sides are
// identical by construction, so two half-width copies would waste the screen
// and wrap prose badly.
func TestUnchangedUnitsRenderFullWidth(t *testing.T) {
	a := "# Title\n\nUnchanged paragraph here.\n\nSecond paragraph.\n"
	b := strings.Replace(a, "Second paragraph.", "Second paragraph, edited.", 1)

	d := RenderDiff(buildMDDiff(a, b, mdUnfolded), DiffOptions{
		Width: 100, SideBySide: true,
	})

	// The heading is unchanged, so it must appear once and carry no divider.
	titles := 0
	for _, line := range d.Lines {
		if strings.Contains(line.SearchText, "# Title") {
			titles++
			if strings.Contains(line.SearchText, cellSeparator) {
				t.Errorf("unchanged heading was split into panes: %q", line.SearchText)
			}
		}
	}
	if titles != 1 {
		t.Errorf("unchanged heading rendered %d times, want once:\n%s", titles, renderedText(d))
	}
}

// A changed unit is the only thing that splits.
func TestChangedUnitsSplit(t *testing.T) {
	a := "Unchanged.\n\nOld text here.\n"
	b := "Unchanged.\n\nNew text here.\n"

	d := RenderDiff(buildMDDiff(a, b, mdUnfolded), DiffOptions{
		Width: 100, SideBySide: true,
	})

	split := 0
	for _, line := range d.Lines {
		if strings.Contains(line.SearchText, cellSeparator) {
			split++
			if strings.Contains(line.SearchText, "Unchanged") {
				t.Errorf("unchanged text appeared in a split row: %q", line.SearchText)
			}
		}
	}
	if split == 0 {
		t.Fatalf("the changed paragraph should split:\n%s", renderedText(d))
	}
}

// A rule brackets each split section so the reader can tell a comparison from
// the document around it.
func TestDividersBracketSplitSections(t *testing.T) {
	a := "First para.\n\nOld middle.\n\nLast para.\n"
	b := "First para.\n\nNew middle.\n\nLast para.\n"

	d := RenderDiff(buildMDDiff(a, b, mdUnfolded), DiffOptions{
		Width: 60, SideBySide: true,
	})

	var firstSplit, lastSplit = -1, -1
	for i, line := range d.Lines {
		if strings.Contains(line.SearchText, cellSeparator) {
			if firstSplit < 0 {
				firstSplit = i
			}
			lastSplit = i
		}
	}
	if firstSplit < 1 {
		t.Fatalf("expected a split section with content above it:\n%s", renderedText(d))
	}
	if !hasDivider(d.Lines[firstSplit-1]) {
		t.Errorf("no divider before the split section: %q", d.Lines[firstSplit-1].SearchText)
	}
	if lastSplit+1 >= len(d.Lines) || !hasDivider(d.Lines[lastSplit+1]) {
		t.Errorf("no divider after the split section:\n%s", renderedText(d))
	}
}

// A trailing split section is closed off rather than running into the bottom
// of the screen unmarked.
func TestTrailingSplitSectionIsClosed(t *testing.T) {
	a := "Unchanged.\n\nOld last.\n"
	b := "Unchanged.\n\nNew last.\n"

	d := RenderDiff(buildMDDiff(a, b, mdUnfolded), DiffOptions{
		Width: 60, SideBySide: true,
	})
	if !hasDivider(d.Lines[len(d.Lines)-1]) {
		t.Errorf("document should end with a closing divider:\n%s", renderedText(d))
	}
}

// The divider-alignment invariant still holds, but only within split rows:
// full-width rows have no divider by design.
func TestMarkdownSplitRowsStayAligned(t *testing.T) {
	a := "Unchanged para.\n\nOld paragraph that is long enough to wrap onto " +
		"several lines inside its pane.\n\nAnother unchanged one.\n"
	b := strings.Replace(a, "Old paragraph", "New paragraph", 1)

	width := 90
	d := RenderDiff(buildMDDiff(a, b, mdUnfolded), DiffOptions{
		Width: width, LineNumbers: true, SideBySide: true,
	})
	checkRendered(t, d, width)

	want := -1
	for i, line := range d.Lines {
		at, cells := -1, 0
		for _, s := range line.Spans {
			if s.Text == cellSeparator {
				at = cells
				break
			}
			cells += s.Cells
		}
		if at < 0 {
			continue // a full-width row
		}
		if want < 0 {
			want = at
		} else if at != want {
			t.Fatalf("row %d puts the divider at %d, want %d: %q", i, at, want, line.SearchText)
		}
	}
	if want < 0 {
		t.Fatal("expected at least one split row")
	}
}

// Markdown styling must survive the diff wash: a changed heading is still a
// heading. This is what Span.Background exists for.
func TestChangedHeadingKeepsItsStyle(t *testing.T) {
	d := RenderDiff(buildMDDiff("# Old title\n", "# New title\n", mdUnfolded),
		DiffOptions{Width: 100, SideBySide: true})

	var found bool
	for _, line := range d.Lines {
		for _, s := range line.Spans {
			if s.Style != StyleHeading {
				continue
			}
			found = true
			// Either the whole-unit wash or, on the changed word itself, the
			// brighter emphasis — but never none.
			switch s.Background {
			case StyleDiffRemove, StyleDiffAdd, StyleDiffRemoveWord, StyleDiffAddWord:
			default:
				t.Errorf("heading span %q has no diff background", s.Text)
			}
		}
	}
	if !found {
		t.Fatalf("no heading span survived the diff:\n%s", renderedText(d))
	}
}

// Unchanged Markdown keeps its styling and gains no wash.
func TestUnchangedUnitsAreNotWashed(t *testing.T) {
	a := "# Title\n\nChanged old.\n"
	b := "# Title\n\nChanged new.\n"

	d := RenderDiff(buildMDDiff(a, b, mdUnfolded), DiffOptions{
		Width: 100, SideBySide: true,
	})

	for _, line := range d.Lines {
		if !strings.Contains(line.SearchText, "Title") {
			continue
		}
		for _, s := range line.Spans {
			if s.Background != StyleNone {
				t.Errorf("unchanged heading was washed: %q has background %v", s.Text, s.Background)
			}
		}
	}
}

func TestMarkdownStackedWhenNarrow(t *testing.T) {
	a := "Unchanged.\n\nOld paragraph.\n"
	b := "Unchanged.\n\nNew paragraph.\n"

	d := RenderDiff(buildMDDiff(a, b, mdUnfolded), DiffOptions{
		Width: 30, SideBySide: true,
	})
	checkRendered(t, d, 30)

	text := renderedText(d)
	for _, line := range d.Lines {
		if strings.Contains(line.SearchText, cellSeparator) {
			t.Fatalf("a narrow terminal should stack, not split:\n%s", text)
		}
	}
	if !strings.Contains(text, "Old paragraph") || !strings.Contains(text, "New paragraph") {
		t.Fatalf("both versions should be shown stacked:\n%s", text)
	}
}

// The fold marker counts what it actually hides.
func TestFoldSaysBlocksInMarkdownMode(t *testing.T) {
	var a strings.Builder
	for i := range 30 {
		a.WriteString("Paragraph " + string(rune('a'+i%26)) + " text.\n\n")
	}
	b := strings.Replace(a.String(), "Paragraph a text.", "CHANGED.", 1)

	d := RenderDiff(
		buildMDDiff(a.String(), b, diffdoc.Options{Context: 2}),
		DiffOptions{Width: 60, SideBySide: true},
	)

	text := renderedText(d)
	if !strings.Contains(text, "unchanged blocks") {
		t.Errorf("fold marker should count blocks in markdown mode:\n%s", text)
	}
	if strings.Contains(text, "unchanged lines") {
		t.Errorf("fold marker should not say lines in markdown mode:\n%s", text)
	}
}

// Text mode is untouched by any of this.
func TestTextModeFoldStillSaysLines(t *testing.T) {
	var a strings.Builder
	for i := range 30 {
		a.WriteString("line " + string(rune('a'+i%26)) + "\n")
	}
	b := strings.Replace(a.String(), "line a\n", "CHANGED\n", 1)

	d := RenderDiff(buildDiff(a.String(), b, diffdoc.Options{Context: 2}),
		DiffOptions{Width: 60, SideBySide: true})

	if !strings.Contains(renderedText(d), "unchanged lines") {
		t.Errorf("text mode should still count lines:\n%s", renderedText(d))
	}
}

func TestEveryMarkdownRowCarriesASourceLine(t *testing.T) {
	a := "# Title\n\nOld para.\n\nKept.\n"
	b := "# Title\n\nNew para.\n\nKept.\n"

	for _, split := range []bool{false, true} {
		d := RenderDiff(buildMDDiff(a, b, mdUnfolded), DiffOptions{
			Width: 100, SideBySide: split,
		})
		for i, line := range d.Lines {
			if line.Source.Start.Line <= 0 {
				t.Fatalf("side-by-side=%v row %d has no source line: %q",
					split, i, line.SearchText)
			}
		}
	}
}

// emphasised collects the text carrying each word background, which is what a
// reader sees as "this is the part that changed".
func emphasised(d DiffDocument, background Style) string {
	var sb strings.Builder
	for _, line := range d.Lines {
		for _, s := range line.Spans {
			if s.Background == background {
				sb.WriteString(s.Text)
			}
		}
	}
	return sb.String()
}

// The point of marking inlines rather than washing the block: a one-word edit
// inside a paragraph shows as one word, on each side.
func TestMarkedWordsAreEmphasised(t *testing.T) {
	d := RenderDiff(buildMDDiff("The quick brown fox.\n", "The slow brown fox.\n", mdUnfolded),
		DiffOptions{Width: 100, SideBySide: true})

	if got := emphasised(d, StyleDiffRemoveWord); got != "quick" {
		t.Errorf("emphasised removal %q, want quick", got)
	}
	if got := emphasised(d, StyleDiffAddWord); got != "slow" {
		t.Errorf("emphasised addition %q, want slow", got)
	}
}

// The rest of a changed block still reads as changed: the marks pick out the
// words, the wash keeps the block a band.
func TestUnmarkedTextKeepsTheBlockWash(t *testing.T) {
	d := RenderDiff(buildMDDiff("The quick brown fox.\n", "The slow brown fox.\n", mdUnfolded),
		DiffOptions{Width: 100, SideBySide: true})

	if got := emphasised(d, StyleDiffRemove); !strings.Contains(got, "brown") {
		t.Errorf("unchanged words lost the removal wash: %q", got)
	}
	if got := emphasised(d, StyleDiffAdd); !strings.Contains(got, "brown") {
		t.Errorf("unchanged words lost the addition wash: %q", got)
	}
}

// A mark inside a link must not cost the link its styling or its target: the
// mark is one more property of the piece, not a replacement for them.
func TestMarkInsideALinkKeepsTheLink(t *testing.T) {
	d := RenderDiff(buildMDDiff(
		"See [the old docs](http://example.com/a).\n",
		"See [the new docs](http://example.com/a).\n",
		mdUnfolded), DiffOptions{Width: 100, SideBySide: true})

	var marked bool
	for _, line := range d.Lines {
		for _, s := range line.Spans {
			if s.Style != StyleLink {
				continue
			}
			if s.LinkTarget != "http://example.com/a" {
				t.Errorf("link span %q lost its target: %q", s.Text, s.LinkTarget)
			}
			if s.Background == StyleDiffAddWord || s.Background == StyleDiffRemoveWord {
				marked = true
			}
		}
	}
	if !marked {
		t.Errorf("the changed word inside the link should be emphasised:\n%s", renderedText(d))
	}
}

// Code and tables keep the whole-unit band, since a mark on them could not be
// drawn: nothing must claim a precision the renderer cannot deliver.
func TestCodeAndTablesGetNoWordEmphasis(t *testing.T) {
	tests := []struct {
		name string
		a, b string
	}{
		{"code", "```\none two\nthree four\n```\n", "```\none two\nthree FIVE\n```\n"},
		{"table", "| a | b |\n|---|---|\n| 1 | 2 |\n", "| a | b |\n|---|---|\n| 1 | 3 |\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d := RenderDiff(buildMDDiff(tt.a, tt.b, mdUnfolded), DiffOptions{Width: 100, SideBySide: true})
			if got := emphasised(d, StyleDiffAddWord) + emphasised(d, StyleDiffRemoveWord); got != "" {
				t.Errorf("emphasis = %q, want none", got)
			}
			// It is still visibly changed.
			if emphasised(d, StyleDiffAdd) == "" || emphasised(d, StyleDiffRemove) == "" {
				t.Errorf("the unit lost its band:\n%s", renderedText(d))
			}
		})
	}
}

// A mark must survive wrapping, including the hard split of a run too wide for
// the pane: the fields of a run are what carry it.
func TestMarksSurviveWrapping(t *testing.T) {
	long := strings.Repeat("word ", 40)
	d := RenderDiff(buildMDDiff(long+"quick end\n", long+"slow end\n", mdUnfolded),
		DiffOptions{Width: 60, SideBySide: true})

	if got := emphasised(d, StyleDiffRemoveWord); got != "quick" {
		t.Errorf("emphasised removal %q, want quick", got)
	}
	if got := emphasised(d, StyleDiffAddWord); got != "slow" {
		t.Errorf("emphasised addition %q, want slow", got)
	}
}
