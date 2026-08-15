package diffdoc

import (
	"strconv"
	"strings"

	"github.com/schambon/mdv/internal/difftext"
	"github.com/schambon/mdv/internal/doc"
)

// Markdown-aware diffing compares two parsed documents block by block rather
// than line by line.
//
// The point is that Markdown's line breaks are not content. Rewrapping a
// paragraph rewrites every one of its source lines while changing nothing a
// reader would see, and a line diff reports all of them. md.Parse joins a
// paragraph's lines with single spaces, so two rewrappings of the same
// paragraph produce byte-identical blocks — and therefore no diff at all.
// Editing one word inside a wrapped paragraph likewise becomes one changed
// block instead of a cascade of reflowed lines.
//
// Alignment itself is shared with the line-based path: only the elements and
// how they turn into rows differ. See lineAt in diffdoc.go.

// BuildMarkdown aligns two parsed Markdown documents.
func BuildMarkdown(a, b doc.Document, opts Options) Document {
	ua, ub := newUnits(a), newUnits(b)

	return build(
		difftext.Lines(keys(ua), keys(ub)),
		func(i int) Line { return ua[i].line() },
		func(i int) Line { return ub[i].line() },
		opts,
	)
}

// unit is the element markdown diffing compares: normally one block, but a
// whole table at once.
type unit struct {
	blocks []doc.Block
	key    string
	text   string
	number int
}

// line converts a unit into one side of a row. Text is the unit's visible
// text, which is what the intraline word diff runs on; Blocks is what the
// renderer draws.
func (u unit) line() Line {
	return Line{Text: u.text, Number: u.number, Blocks: u.blocks}
}

func keys(units []unit) []string {
	out := make([]string, len(units))
	for i, u := range units {
		out[i] = u.key
	}
	return out
}

// newUnits splits a document into comparable elements.
//
// Adjacent table rows form a single unit because a table's column widths are
// sized across the whole group: diffing rows individually would let each side
// render its rows at different widths, and the columns would not line up.
// The cost is that one changed cell marks the whole table changed.
func newUnits(d doc.Document) []unit {
	var out []unit
	for i := 0; i < len(d.Blocks); {
		if d.Blocks[i].Kind == doc.BlockTableRow {
			start := i
			for i < len(d.Blocks) && d.Blocks[i].Kind == doc.BlockTableRow {
				i++
			}
			out = append(out, newUnit(d.Blocks[start:i]))
			continue
		}
		out = append(out, newUnit(d.Blocks[i:i+1]))
		i++
	}
	return out
}

func newUnit(blocks []doc.Block) unit {
	u := unit{blocks: blocks}
	if len(blocks) > 0 {
		u.number = blocks[0].Source.Start.Line
	}

	var key, text strings.Builder
	for _, b := range blocks {
		// The key must capture everything that makes two blocks render
		// differently, and nothing that does not. Source position is excluded
		// deliberately: moving a paragraph down a file does not change it.
		key.WriteString(strconv.Itoa(int(b.Kind)))
		key.WriteByte(unitFieldSep)
		key.WriteString(strconv.Itoa(b.Level))
		key.WriteByte(unitFieldSep)
		key.WriteString(b.Prefix)
		key.WriteByte(unitFieldSep)
		key.WriteString(strconv.FormatBool(b.Header))
		key.WriteByte(unitRecordSep)

		for _, in := range b.Inlines {
			// Kind and Target belong in the key as much as the text does.
			// Keyed on text alone, [docs](old) and [docs](new) are identical
			// and a changed link would show no diff — a wrong answer rather
			// than an untidy one. Likewise *foo* against plain foo.
			key.WriteString(strconv.Itoa(int(in.Kind)))
			key.WriteByte(unitFieldSep)
			key.WriteString(in.Text)
			key.WriteByte(unitFieldSep)
			key.WriteString(in.Target)
			key.WriteByte(unitRecordSep)

			text.WriteString(in.Text)
		}
		key.WriteByte(unitBlockSep)
	}

	u.key, u.text = key.String(), text.String()
	return u
}

// Separators are ASCII control characters that Markdown source will not
// contain, so no combination of text and target can forge a key boundary.
const (
	unitFieldSep  = 0x1f
	unitRecordSep = 0x1e
	unitBlockSep  = 0x1d
)
