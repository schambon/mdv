package md

import (
	"strings"
	"testing"

	"github.com/schambon/mdv/internal/doc"
)

func TestFrontMatterSplitsKeysFromValues(t *testing.T) {
	src := "---\nid: doc:kb-conventions\ntype: doc\n---\n\n# Title\n"
	d := Parse([]byte(src))

	if got := d.Blocks[0].Kind; got != doc.BlockRule {
		t.Fatalf("block 0 kind = %v, want BlockRule", got)
	}
	want := []struct{ key, value string }{
		{"id", "doc:kb-conventions"},
		{"type", "doc"},
	}
	for i, w := range want {
		b := d.Blocks[i+1]
		if b.Kind != doc.BlockMeta {
			t.Fatalf("block %d kind = %v, want BlockMeta", i+1, b.Kind)
		}
		if b.Prefix != w.key {
			t.Errorf("block %d key = %q, want %q", i+1, b.Prefix, w.key)
		}
		if len(b.Inlines) != 1 || b.Inlines[0].Text != w.value {
			t.Errorf("block %d value = %v, want %q", i+1, b.Inlines, w.value)
		}
	}
	if got := d.Blocks[3].Kind; got != doc.BlockRule {
		t.Errorf("closing block kind = %v, want BlockRule", got)
	}
	if got := d.Blocks[len(d.Blocks)-1].Kind; got != doc.BlockHeading {
		t.Errorf("last block kind = %v, want BlockHeading", got)
	}
}

// The whole point of the feature: the lines must not be reflowed into one
// paragraph, which is what they did before frontmatter was recognised.
func TestFrontMatterIsNotAParagraph(t *testing.T) {
	d := Parse([]byte("---\na: 1\nb: 2\n---\n"))
	for _, b := range d.Blocks {
		if b.Kind == doc.BlockParagraph {
			t.Fatalf("frontmatter produced a paragraph: %v", b.Inlines)
		}
	}
}

func TestFrontMatterClosedByDocumentEndMarker(t *testing.T) {
	d := Parse([]byte("---\na: 1\n...\ntext\n"))
	if d.Blocks[1].Kind != doc.BlockMeta {
		t.Errorf("block 1 kind = %v, want BlockMeta", d.Blocks[1].Kind)
	}
	if d.Blocks[2].Kind != doc.BlockRule {
		t.Errorf("block 2 kind = %v, want BlockRule", d.Blocks[2].Kind)
	}
}

// An unterminated opener is not frontmatter, so the ordinary grammar must get
// the lines back untouched: a rule, then a paragraph.
func TestUnterminatedFrontMatterFallsBackToRuleAndParagraph(t *testing.T) {
	d := Parse([]byte("---\njust some text\nmore text\n"))
	if d.Blocks[0].Kind != doc.BlockRule {
		t.Fatalf("block 0 kind = %v, want BlockRule", d.Blocks[0].Kind)
	}
	if d.Blocks[1].Kind != doc.BlockParagraph {
		t.Fatalf("block 1 kind = %v, want BlockParagraph", d.Blocks[1].Kind)
	}
	for _, b := range d.Blocks {
		if b.Kind == doc.BlockMeta {
			t.Errorf("unterminated opener produced a meta block")
		}
	}
}

// Only line one opens frontmatter; a fence later in the file is a rule.
func TestFrontMatterOnlyAtTheTopOfTheFile(t *testing.T) {
	d := Parse([]byte("# Title\n\n---\na: 1\n---\n"))
	for _, b := range d.Blocks {
		if b.Kind == doc.BlockMeta {
			t.Fatalf("a mid-document fence was taken for frontmatter")
		}
	}
}

func TestFrontMatterValuesAreLiteral(t *testing.T) {
	d := Parse([]byte("---\nglob: *.md*\n---\n"))
	b := d.Blocks[1]
	if len(b.Inlines) != 1 || b.Inlines[0].Kind != doc.InlineText {
		t.Fatalf("value was parsed as markup: %v", b.Inlines)
	}
	if b.Inlines[0].Text != "*.md*" {
		t.Errorf("value = %q, want %q", b.Inlines[0].Text, "*.md*")
	}
}

func TestFrontMatterKeepsNestingAndBlankLines(t *testing.T) {
	d := Parse([]byte("---\ntags:\n  - one\n\nname: x\n---\n"))
	kinds := []doc.BlockKind{
		doc.BlockRule, doc.BlockMeta, doc.BlockMeta, doc.BlockBlank, doc.BlockMeta, doc.BlockRule,
	}
	if len(d.Blocks) != len(kinds) {
		t.Fatalf("got %d blocks, want %d", len(d.Blocks), len(kinds))
	}
	for i, want := range kinds {
		if d.Blocks[i].Kind != want {
			t.Errorf("block %d kind = %v, want %v", i, d.Blocks[i].Kind, want)
		}
	}
	if got := d.Blocks[1].Inlines; len(got) != 1 || got[0].Text != "" {
		t.Errorf("empty value = %v, want one empty inline", got)
	}
	// A line with no key colon keeps its whole text, indentation included.
	if got := d.Blocks[2]; got.Prefix != "" || got.Inlines[0].Text != "  - one" {
		t.Errorf("list line = %q / %q, want an empty key and the full text", got.Prefix, got.Inlines[0].Text)
	}
}

// Source mapping is per line, which is what makes v and position restoration
// land on the right key.
func TestFrontMatterMapsEachLineToItsOwnSource(t *testing.T) {
	src := "---\nid: one\ntype: two\n---\n"
	d := Parse([]byte(src))
	for i, want := range []int{2, 3} {
		b := d.Blocks[i+1]
		if b.Source.Start.Line != want {
			t.Errorf("meta block %d starts at line %d, want %d", i, b.Source.Start.Line, want)
		}
		// The value's own offset must point at the value, not the key.
		off := b.Inlines[0].Source.Start.ByteOffset
		if !strings.HasPrefix(src[off:], b.Inlines[0].Text) {
			t.Errorf("value offset %d does not point at %q", off, b.Inlines[0].Text)
		}
	}
}

func TestSplitMeta(t *testing.T) {
	cases := []struct{ in, key, value string }{
		{"id: doc:kb-conventions", "id", "doc:kb-conventions"},
		{"empty:", "empty", ""},
		{"  nested: yes", "  nested", "yes"},
		{"tabbed:\tyes", "tabbed", "yes"},
		{"no colon here", "", "no colon here"},
		{"http://example.com", "", "http://example.com"},
		{": leading colon", "", ": leading colon"},
	}
	for _, c := range cases {
		key, value, _ := splitMeta(c.in)
		if key != c.key || value != c.value {
			t.Errorf("splitMeta(%q) = %q, %q; want %q, %q", c.in, key, value, c.key, c.value)
		}
	}
}
