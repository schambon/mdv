package md

import (
	"strings"
	"testing"

	"github.com/schambon/mdv/internal/doc"
)

// kinds returns the block kinds of a parsed document, which is usually the
// most interesting assertion about the ordered block scanner.
func kinds(d doc.Document) []doc.BlockKind {
	out := make([]doc.BlockKind, len(d.Blocks))
	for i, b := range d.Blocks {
		out[i] = b.Kind
	}
	return out
}

func parse(t *testing.T, src string) doc.Document {
	t.Helper()
	return Parse([]byte(src))
}

// blockText concatenates a block's inline text, ignoring markup.
func blockText(b doc.Block) string {
	var sb strings.Builder
	for _, in := range b.Inlines {
		sb.WriteString(in.Text)
	}
	return sb.String()
}

func TestParseBlockKinds(t *testing.T) {
	tests := []struct {
		name string
		src  string
		want []doc.BlockKind
	}{
		{"blank", "\n", []doc.BlockKind{doc.BlockBlank}},
		{"whitespace is blank", "   \t\n", []doc.BlockKind{doc.BlockBlank}},
		{"paragraph", "hello\n", []doc.BlockKind{doc.BlockParagraph}},
		{"heading", "## Title\n", []doc.BlockKind{doc.BlockHeading}},
		{"rule", "---\n", []doc.BlockKind{doc.BlockRule}},
		{"spaced rule", "- - -\n", []doc.BlockKind{doc.BlockRule}},
		{"quote", "> quoted\n", []doc.BlockKind{doc.BlockQuote}},
		{"unordered list", "- item\n", []doc.BlockKind{doc.BlockListItem}},
		{"ordered list", "1. item\n", []doc.BlockKind{doc.BlockListItem}},
		{"paren list", "1) item\n", []doc.BlockKind{doc.BlockListItem}},
		{"fenced code", "```go\ncode\n```\n", []doc.BlockKind{doc.BlockCode}},
		{"indented code", "    code\n", []doc.BlockKind{doc.BlockCode}},
		{
			"heading then paragraph",
			"# T\n\nbody\n",
			[]doc.BlockKind{doc.BlockHeading, doc.BlockBlank, doc.BlockParagraph},
		},
		{
			"table",
			"| a | b |\n|---|---|\n| 1 | 2 |\n",
			[]doc.BlockKind{doc.BlockTableRow, doc.BlockTableRow},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := kinds(parse(t, tt.src))
			if len(got) != len(tt.want) {
				t.Fatalf("got %v, want %v", got, tt.want)
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Fatalf("got %v, want %v", got, tt.want)
				}
			}
		})
	}
}

func TestHeadingRequiresColumnZeroAndSpace(t *testing.T) {
	tests := []struct {
		name string
		src  string
		want doc.BlockKind
	}{
		{"indented is not a heading", " # Title\n", doc.BlockParagraph},
		{"no space is not a heading", "#Title\n", doc.BlockParagraph},
		{"seven hashes is not a heading", "####### Title\n", doc.BlockParagraph},
		{"six hashes is a heading", "###### Title\n", doc.BlockHeading},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := parse(t, tt.src).Blocks[0].Kind; got != tt.want {
				t.Errorf("kind = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestHeadingLevelAndPrefix(t *testing.T) {
	b := parse(t, "### Deep\n").Blocks[0]
	if b.Level != 3 {
		t.Errorf("Level = %d, want 3", b.Level)
	}
	if b.Prefix != "### " {
		t.Errorf("Prefix = %q, want %q", b.Prefix, "### ")
	}
	if got := blockText(b); got != "Deep" {
		t.Errorf("text = %q, want Deep", got)
	}
}

func TestQuotePrefixAndBody(t *testing.T) {
	b := parse(t, "> quoted\n").Blocks[0]
	if b.Prefix != quotePrefix {
		t.Errorf("Prefix = %q, want %q", b.Prefix, quotePrefix)
	}
	if got := blockText(b); got != "quoted" {
		t.Errorf("text = %q, want quoted", got)
	}
}

func TestListPrefixesIncludingTasks(t *testing.T) {
	tests := []struct{ src, prefix, text string }{
		{"- item\n", "- ", "item"},
		{"* item\n", "* ", "item"},
		{"+ item\n", "+ ", "item"},
		{"2. item\n", "2. ", "item"},
		{"3) item\n", "3) ", "item"},
		{"- [ ] todo\n", "- [ ] ", "todo"},
		{"- [x] done\n", "- [x] ", "done"},
		{"- [X] done\n", "- [X] ", "done"},
		{"- [y] not a task\n", "- ", "[y] not a task"},
	}
	for _, tt := range tests {
		t.Run(tt.src, func(t *testing.T) {
			b := parse(t, tt.src).Blocks[0]
			if b.Prefix != tt.prefix {
				t.Errorf("Prefix = %q, want %q", b.Prefix, tt.prefix)
			}
			if got := blockText(b); got != tt.text {
				t.Errorf("text = %q, want %q", got, tt.text)
			}
		})
	}
}

// A soft-wrapped list item reflows: continuation lines join the item body with
// a single space rather than falling out as a separate flush-left paragraph.
func TestListReflowsWrappedContinuation(t *testing.T) {
	d := parse(t, "- first item\n  wrapped continuation\n  more\n- second item\n")
	if len(d.Blocks) != 2 {
		t.Fatalf("got %d blocks, want 2", len(d.Blocks))
	}
	if d.Blocks[0].Kind != doc.BlockListItem || d.Blocks[1].Kind != doc.BlockListItem {
		t.Fatalf("kinds = %v, %v, want two list items", d.Blocks[0].Kind, d.Blocks[1].Kind)
	}
	if got := blockText(d.Blocks[0]); got != "first item wrapped continuation more" {
		t.Errorf("first item text = %q", got)
	}
	if got := blockText(d.Blocks[1]); got != "second item" {
		t.Errorf("second item text = %q", got)
	}
}

// A blank line ends a wrapped item; a task prefix survives the gathering.
func TestListWrapStopsAtBlankAndKeepsTask(t *testing.T) {
	d := parse(t, "- [ ] todo\n  rest of todo\n\nafter\n")
	b := d.Blocks[0]
	if b.Kind != doc.BlockListItem || b.Prefix != "- [ ] " {
		t.Errorf("item = %v %q, want list item %q", b.Kind, b.Prefix, "- [ ] ")
	}
	if got := blockText(b); got != "todo rest of todo" {
		t.Errorf("text = %q", got)
	}
	last := d.Blocks[len(d.Blocks)-1]
	if last.Kind != doc.BlockParagraph || blockText(last) != "after" {
		t.Errorf("trailing block = %v %q, want paragraph %q", last.Kind, blockText(last), "after")
	}
}

// special() constructs end a wrapped item instead of being folded into it.
func TestListWrapStopsAtSpecialLines(t *testing.T) {
	tests := []struct{ name, next string }{
		{"heading", "# head\n"},
		{"fence", "```\ncode\n```\n"},
		{"rule", "---\n"},
		{"quote", "> quote\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d := parse(t, "- item text\n"+tt.next)
			if got := blockText(d.Blocks[0]); got != "item text" {
				t.Errorf("item text = %q, want %q", got, "item text")
			}
			if len(d.Blocks) < 2 {
				t.Fatalf("special line was folded into the item")
			}
		})
	}
}

// The block source range spans every line a wrapped item consumed.
func TestListWrapSourceSpan(t *testing.T) {
	b := parse(t, "- one\n  two\n  three\n").Blocks[0]
	if b.Source.Start.Line != 1 {
		t.Errorf("start line = %d, want 1", b.Source.Start.Line)
	}
	if b.Source.End.Line != 3 {
		t.Errorf("end line = %d, want 3", b.Source.End.Line)
	}
}

// An indented list item is a list, not indented code: the list check runs first.
func TestIndentedListBeatsIndentedCode(t *testing.T) {
	if got := parse(t, "    - item\n").Blocks[0].Kind; got != doc.BlockListItem {
		t.Errorf("kind = %v, want list item", got)
	}
}

func TestFencedCodeExcludesMarkersAndIgnoresLanguage(t *testing.T) {
	b := parse(t, "```go\nline one\nline two\n```\n").Blocks[0]
	if b.Kind != doc.BlockCode {
		t.Fatalf("kind = %v, want code", b.Kind)
	}
	if got := blockText(b); got != "line one\nline two" {
		t.Errorf("text = %q", got)
	}
}

func TestFencedCodeUnterminatedRunsToEOF(t *testing.T) {
	b := parse(t, "```\nunclosed\n").Blocks[0]
	if got := blockText(b); got != "unclosed" {
		t.Errorf("text = %q, want unclosed", got)
	}
}

func TestFencedCodeTildeMarkerNotClosedByBacktick(t *testing.T) {
	d := parse(t, "~~~\na\n```\nb\n~~~\n")
	if len(d.Blocks) != 1 {
		t.Fatalf("got %d blocks, want 1", len(d.Blocks))
	}
	if got := blockText(d.Blocks[0]); got != "a\n```\nb" {
		t.Errorf("text = %q", got)
	}
}

func TestIndentedCodeStripsFourSpaces(t *testing.T) {
	b := parse(t, "    one\n      two\n").Blocks[0]
	if got := blockText(b); got != "one\n  two" {
		t.Errorf("text = %q", got)
	}
}

func TestParagraphJoinsLinesWithSpace(t *testing.T) {
	b := parse(t, "one\n  two  \nthree\n").Blocks[0]
	if got := blockText(b); got != "one two three" {
		t.Errorf("text = %q, want %q", got, "one two three")
	}
}

// special() deliberately omits tables and indented code, so neither ends a
// paragraph already in progress.
func TestParagraphNotInterruptedByTableOrIndentedCode(t *testing.T) {
	tests := []struct{ name, src, want string }{
		{"table", "text\n| a | b |\n|---|---|\n", "text | a | b | |---|---|"},
		{"indented code", "text\n    code\n", "text code"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d := parse(t, tt.src)
			if d.Blocks[0].Kind != doc.BlockParagraph {
				t.Fatalf("kind = %v, want paragraph", d.Blocks[0].Kind)
			}
			if got := blockText(d.Blocks[0]); got != tt.want {
				t.Errorf("text = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestParagraphInterruptedBySpecialLines(t *testing.T) {
	for _, src := range []string{"text\n# H\n", "text\n---\n", "text\n> q\n", "text\n- l\n", "text\n```\n"} {
		d := parse(t, src)
		if len(d.Blocks) < 2 {
			t.Errorf("%q: got %d blocks, want the paragraph to be interrupted", src, len(d.Blocks))
			continue
		}
		if got := blockText(d.Blocks[0]); got != "text" {
			t.Errorf("%q: paragraph text = %q, want text", src, got)
		}
	}
}

func TestTableCellsAndHeader(t *testing.T) {
	d := parse(t, "| a | b |\n| --- | :---: |\n| 1 | 2 |\n")
	if len(d.Blocks) != 2 {
		t.Fatalf("got %d blocks, want 2", len(d.Blocks))
	}
	if !d.Blocks[0].Header {
		t.Error("first row should be the header")
	}
	if d.Blocks[1].Header {
		t.Error("body row should not be the header")
	}
	for i, want := range [][]string{{"a", "b"}, {"1", "2"}} {
		got := make([]string, len(d.Blocks[i].Inlines))
		for j, in := range d.Blocks[i].Inlines {
			got[j] = in.Text
		}
		if strings.Join(got, ",") != strings.Join(want, ",") {
			t.Errorf("row %d cells = %v, want %v", i, got, want)
		}
	}
}

func TestTableRequiresDelimiterRow(t *testing.T) {
	d := parse(t, "| a | b |\nnot a delimiter\n")
	if d.Blocks[0].Kind != doc.BlockParagraph {
		t.Errorf("kind = %v, want paragraph", d.Blocks[0].Kind)
	}
}

func TestTableEndsAtBlankLine(t *testing.T) {
	d := parse(t, "| a |\n|---|\n| 1 |\n\n| 2 |\n")
	if got := kinds(d); len(got) != 4 || got[2] != doc.BlockBlank {
		t.Errorf("kinds = %v, want table, table, blank, paragraph", got)
	}
}

func TestCRLFLineEndings(t *testing.T) {
	d := parse(t, "# Title\r\nbody\r\n")
	if got := blockText(d.Blocks[0]); got != "Title" {
		t.Errorf("heading text = %q, want Title", got)
	}
	if got := blockText(d.Blocks[1]); got != "body" {
		t.Errorf("paragraph text = %q, want body", got)
	}
}

func TestSourceLineMapping(t *testing.T) {
	d := parse(t, "# One\n\npara\n\n- item\n")
	wantLines := []int{1, 2, 3, 4, 5}
	if len(d.Blocks) != len(wantLines) {
		t.Fatalf("got %d blocks, want %d", len(d.Blocks), len(wantLines))
	}
	for i, want := range wantLines {
		if got := d.Blocks[i].Source.Start.Line; got != want {
			t.Errorf("block %d starts at line %d, want %d", i, got, want)
		}
	}
}

// Columns are real, not hardcoded to 1: the heading body starts after "## ".
func TestInlineSourceColumns(t *testing.T) {
	d := parse(t, "## Title\n")
	in := d.Blocks[0].Inlines[0]
	if in.Source.Start.Line != 1 || in.Source.Start.Column != 4 {
		t.Errorf("inline starts at line %d col %d, want 1/4",
			in.Source.Start.Line, in.Source.Start.Column)
	}
}

func TestMultiLineBlockSourceSpan(t *testing.T) {
	d := parse(t, "```\na\nb\n```\n")
	b := d.Blocks[0]
	if b.Source.Start.Line != 1 {
		t.Errorf("start line = %d, want 1", b.Source.Start.Line)
	}
	if b.Source.End.Line != 4 {
		t.Errorf("end line = %d, want 4", b.Source.End.Line)
	}
}

func TestParseEmptyInput(t *testing.T) {
	d := Parse(nil)
	if d.Size != 0 {
		t.Errorf("Size = %d, want 0", d.Size)
	}
	if len(d.Blocks) != 1 || d.Blocks[0].Kind != doc.BlockBlank {
		t.Errorf("kinds = %v, want a single blank", kinds(d))
	}
}

func TestFenceLanguage(t *testing.T) {
	tests := []struct {
		name, src, want string
	}{
		{"plain word", "```go\ncode\n```\n", "go"},
		{"lowercased", "```Python\nx\n```\n", "python"},
		{"first word of info string", "```js  {.line-numbers}\nx\n```\n", "js"},
		{"tilde fence", "~~~rust\nx\n~~~\n", "rust"},
		{"bare fence", "```\ncode\n```\n", ""},
		{"tilde bare fence", "~~~\ncode\n~~~\n", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d := parse(t, tt.src)
			if d.Blocks[0].Kind != doc.BlockCode {
				t.Fatalf("first block kind = %v, want code", d.Blocks[0].Kind)
			}
			if got := d.Blocks[0].Lang; got != tt.want {
				t.Errorf("Lang = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestParseNoTrailingNewline(t *testing.T) {
	d := parse(t, "one\ntwo")
	if len(d.Blocks) != 1 {
		t.Fatalf("got %d blocks, want 1", len(d.Blocks))
	}
	if got := blockText(d.Blocks[0]); got != "one two" {
		t.Errorf("text = %q", got)
	}
}
