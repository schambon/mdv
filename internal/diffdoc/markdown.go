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

// markInlines cuts a unit's inlines at the word-diff boundaries and marks the
// pieces that differ, returning fresh blocks: the originals belong to the
// parsed document and are shared with every other row.
//
// The cut is exact rather than approximate because a unit's text is by
// construction the concatenation of its inlines' text — the same walk newUnit
// makes — so a byte offset into one is a byte offset into the other. Passing
// ranges down to the renderer instead would make the Markdown renderer learn
// what a diff is.
//
// Marks are deliberately not applied to code blocks or tables. A code block
// renders from Inlines[0] alone and a table cell is re-parsed from its raw
// text, so a mark on a later inline would silently vanish; those units keep
// the whole-unit band, which is honest about what it knows.
func markInlines(blocks []doc.Block, words []difftext.Segment, op difftext.Op) []doc.Block {
	// Text mode has no blocks at all, and must keep its nil: a non-nil empty
	// slice would make the row claim to be a Markdown one.
	if len(blocks) == 0 {
		return blocks
	}
	for _, b := range blocks {
		if b.Kind == doc.BlockCode || b.Kind == doc.BlockTableRow {
			return blocks
		}
	}

	cuts := sideSegments(words, op)
	if len(cuts) == 0 {
		return blocks
	}

	out := make([]doc.Block, len(blocks))
	for i, b := range blocks {
		out[i] = b
		out[i].Inlines = make([]doc.Inline, 0, len(b.Inlines))
		for _, in := range b.Inlines {
			out[i].Inlines = append(out[i].Inlines, cuts.cut(in)...)
		}
	}
	return out
}

// segment is one stretch of a side's text and whether it differs.
type segment struct {
	length int
	mark   bool
}

// segments walks a side's text, handing out its next n bytes at a time.
type segments []segment

// sideSegments keeps the word-diff segments belonging to one side. Their texts
// concatenated are exactly that side's text, which is what makes the walk below
// a simple counter rather than a search.
func sideSegments(words []difftext.Segment, op difftext.Op) segments {
	var out segments
	for _, s := range words {
		if s.Op != difftext.OpEqual && s.Op != op {
			continue // the other side's half of the change
		}
		if len(s.Text) == 0 {
			continue
		}
		out = append(out, segment{length: len(s.Text), mark: s.Op == op})
	}
	return out
}

// cut splits one inline wherever a segment boundary falls inside it. An inline
// with no text — a table separator, an empty link — is passed through, so the
// inline list keeps its shape.
func (s *segments) cut(in doc.Inline) []doc.Inline {
	if in.Text == "" {
		return []doc.Inline{in}
	}

	var out []doc.Inline
	text := in.Text
	for text != "" {
		if len(*s) == 0 {
			// The segments ran out: keep the rest rather than lose it. The two
			// walks agree by construction, so this is a bug guard, not a case.
			piece := in
			piece.Text = text
			return append(out, piece)
		}

		n := min(len(text), (*s)[0].length)
		piece := in
		piece.Text, piece.Mark = text[:n], (*s)[0].mark
		out = append(out, piece)

		text = text[n:]
		if (*s)[0].length -= n; (*s)[0].length == 0 {
			*s = (*s)[1:]
		}
	}
	return out
}

// Separators are ASCII control characters that Markdown source will not
// contain, so no combination of text and target can forge a key boundary.
const (
	unitFieldSep  = 0x1f
	unitRecordSep = 0x1e
	unitBlockSep  = 0x1d
)
