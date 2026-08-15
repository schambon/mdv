package diffdoc

import (
	"strings"
	"testing"

	"github.com/schambon/mdv/internal/md"
)

func buildMD(t *testing.T, a, b string, opts Options) Document {
	t.Helper()
	return BuildMarkdown(md.Parse([]byte(a)), md.Parse([]byte(b)), opts)
}

// rewrap reflows text to a target width, the way an editor's fill-paragraph
// would. The words are untouched; only the line breaks move.
func rewrap(text string, width int) string {
	var out []string
	for _, para := range strings.Split(text, "\n\n") {
		words := strings.Fields(para)
		if len(words) == 0 {
			continue
		}
		line := words[0]
		var lines []string
		for _, w := range words[1:] {
			if len(line)+1+len(w) > width {
				lines = append(lines, line)
				line = w
				continue
			}
			line += " " + w
		}
		out = append(out, strings.Join(append(lines, line), "\n"))
	}
	return strings.Join(out, "\n\n") + "\n"
}

// This is the feature's reason to exist: reflowing prose changes every source
// line and must produce no diff at all.
func TestRewrapProducesNoDiff(t *testing.T) {
	text := "# Heading\n\n" +
		"The quick brown fox jumps over the lazy dog, and then continues on " +
		"its way across the meadow toward the distant treeline where it will " +
		"rest for a while before returning home.\n\n" +
		"A second paragraph with rather different content, long enough that " +
		"it too will be wrapped differently at different widths.\n"

	for _, width := range []int{30, 40, 55, 72, 100} {
		a := rewrap(text, 72)
		b := rewrap(text, width)

		doc := buildMD(t, a, b, Options{Context: -1, WordDiff: true})
		if doc.Changed() {
			t.Errorf("rewrapping at %d produced a diff:\n%s", width, summarise(doc.Rows))
		}
	}
}

// The line-based diff sees the same rewrap as a wall of changes. This is the
// contrast the feature exists to remove; if it ever stops holding, the
// markdown test above has stopped proving anything.
func TestRewrapDoesChangeTheLineDiff(t *testing.T) {
	text := "The quick brown fox jumps over the lazy dog and keeps going for " +
		"quite a while longer so that the wrapping actually differs.\n"

	a, b := rewrap(text, 72), rewrap(text, 30)
	doc := Build(SplitLines([]byte(a)), SplitLines([]byte(b)), Options{Context: -1})
	if !doc.Changed() {
		t.Fatal("expected the line diff to report the rewrap")
	}
}

// A one-word edit inside a wrapped paragraph is one changed block, not one
// changed line per reflowed line.
func TestWordEditIsOneChangedBlock(t *testing.T) {
	a := rewrap("The quick brown fox jumps over the lazy dog and then runs "+
		"onward through the field until it reaches the far fence.", 40)
	b := strings.Replace(a, "quick", "sluggish", 1)
	b = rewrap(strings.ReplaceAll(b, "\n", " "), 40)

	doc := buildMD(t, a, b, Options{Context: -1, WordDiff: true})

	changed := 0
	for _, r := range doc.Rows {
		if r.Kind != RowEqual {
			changed++
		}
	}
	if changed != 1 {
		t.Fatalf("want exactly one changed row, got %d:\n%s", changed, summarise(doc.Rows))
	}
	if doc.Rows[0].Left.Words == nil {
		t.Error("a changed paragraph should carry an intraline diff")
	}
}

// A changed link target must not be invisible. Keyed on inline text alone,
// these two documents look identical.
func TestChangedLinkTargetIsADiff(t *testing.T) {
	a := "See [the docs](http://example.com/old) for details.\n"
	b := "See [the docs](http://example.com/new) for details.\n"

	doc := buildMD(t, a, b, Options{Context: -1})
	if !doc.Changed() {
		t.Fatal("a changed link target must show as a diff")
	}
}

// Likewise emphasis: the visible text is the same, the markup is not.
func TestChangedEmphasisIsADiff(t *testing.T) {
	doc := buildMD(t, "some *emphasised* text\n", "some emphasised text\n",
		Options{Context: -1})
	if !doc.Changed() {
		t.Fatal("removing emphasis must show as a diff")
	}
}

func TestIdenticalDocumentsHaveNoDiff(t *testing.T) {
	text := "# Title\n\nA paragraph.\n\n- one\n- two\n\n> quoted\n\n```\ncode\n```\n"
	doc := buildMD(t, text, text, Options{Context: -1})
	if doc.Changed() {
		t.Fatalf("identical documents differ:\n%s", summarise(doc.Rows))
	}
}

func TestHeadingLevelChangeIsADiff(t *testing.T) {
	doc := buildMD(t, "# Title\n", "## Title\n", Options{Context: -1})
	if !doc.Changed() {
		t.Fatal("a heading level change must show as a diff")
	}
}

func TestAddedParagraph(t *testing.T) {
	a := "First paragraph.\n\nThird paragraph.\n"
	b := "First paragraph.\n\nSecond paragraph.\n\nThird paragraph.\n"

	doc := buildMD(t, a, b, Options{Context: -1})

	added := 0
	for _, r := range doc.Rows {
		if r.Kind == RowAdded {
			added++
		}
	}
	// The paragraph and the blank line before it.
	if added == 0 {
		t.Fatalf("want an added row:\n%s", summarise(doc.Rows))
	}
	for _, r := range doc.Rows {
		if r.Kind == RowAdded && strings.Contains(r.Right.Text, "Second") {
			return
		}
	}
	t.Fatalf("the new paragraph should be an addition:\n%s", summarise(doc.Rows))
}

// A table is one unit, so its rows keep the shared column widths that make
// the columns line up.
func TestTableIsOneUnit(t *testing.T) {
	table := "| a | b |\n|---|---|\n| 1 | 2 |\n| 3 | 4 |\n"
	units := newUnits(md.Parse([]byte(table)))

	if len(units) != 1 {
		t.Fatalf("want one unit for the table, got %d", len(units))
	}
	if len(units[0].blocks) != 3 {
		t.Errorf("unit holds %d blocks, want the header and two rows", len(units[0].blocks))
	}
}

func TestTableCellChangeIsADiff(t *testing.T) {
	a := "| a | b |\n|---|---|\n| 1 | 2 |\n"
	b := "| a | b |\n|---|---|\n| 1 | 9 |\n"

	if !buildMD(t, a, b, Options{Context: -1}).Changed() {
		t.Fatal("a changed table cell must show as a diff")
	}
}

// Units must cover every block of both documents exactly once, or rendering
// would silently drop content.
func TestUnitsCoverEveryBlock(t *testing.T) {
	text := "# Title\n\nPara one.\n\n- a\n- b\n\n| x | y |\n|---|---|\n| 1 | 2 |\n\n" +
		"> quote\n\n```\ncode line\n```\n\nPara two.\n"
	parsed := md.Parse([]byte(text))

	total := 0
	for _, u := range newUnits(parsed) {
		if len(u.blocks) == 0 {
			t.Fatal("empty unit")
		}
		total += len(u.blocks)
	}
	if total != len(parsed.Blocks) {
		t.Fatalf("units cover %d blocks, want %d", total, len(parsed.Blocks))
	}
}

func TestMarkdownFoldingStillApplies(t *testing.T) {
	var a strings.Builder
	for i := range 40 {
		a.WriteString("Paragraph number " + itoa(i) + " with some text.\n\n")
	}
	b := strings.Replace(a.String(), "Paragraph number 20", "CHANGED paragraph", 1)

	doc := BuildMarkdown(md.Parse([]byte(a.String())), md.Parse([]byte(b)),
		Options{Context: DefaultContext})

	folds := 0
	for _, r := range doc.Rows {
		if r.Kind == RowFolded {
			folds++
		}
	}
	if folds != 2 {
		t.Fatalf("want a fold above and below the change, got %d:\n%s", folds, summarise(doc.Rows))
	}
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var digits []byte
	for n > 0 {
		digits = append([]byte{byte('0' + n%10)}, digits...)
		n /= 10
	}
	return string(digits)
}
