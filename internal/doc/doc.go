// Package doc defines the semantic model produced by the Markdown parser and
// consumed by layout. It is deliberately free of terminal and styling concerns:
// a Document describes what the source says, not how it is drawn.
package doc

import "sort"

// Position is a location in the source file. Line and Column are one-based;
// Column counts bytes, not runes or display cells.
type Position struct {
	ByteOffset int
	Line       int
	Column     int
}

// SourceRange spans from Start (inclusive) to End (exclusive).
type SourceRange struct {
	Start Position
	End   Position
}

// InlineKind identifies a span of inline markup.
type InlineKind int

const (
	InlineText InlineKind = iota
	InlineEmphasis
	InlineStrong
	InlineStrike
	InlineCode
	InlineLink
	// InlineTableSeparator is synthetic: it is never parsed from source, but
	// emitted by layout to separate table cells.
	InlineTableSeparator
)

// Inline is a run of text with a single kind. Contents are not recursively
// parsed, so Text holds the literal inner text of the construct.
//
// Mark is never set by the parser. Diffing sets it on the parts of a changed
// block that actually differ, cutting inlines at word-diff boundaries so that
// the mark is carried by the ordinary inline list rather than by offsets a
// renderer would have to map. What a mark means is the caller's business:
// layout only propagates it, as it propagates Target.
type Inline struct {
	Kind   InlineKind
	Text   string
	Target string
	Source SourceRange
	Mark   bool
}

// BlockKind identifies a block-level construct.
type BlockKind int

const (
	BlockBlank BlockKind = iota
	BlockParagraph
	BlockHeading
	BlockRule
	BlockCode
	BlockQuote
	BlockListItem
	BlockTableRow
)

// Block is one block-level construct. Level carries the heading depth, Prefix
// the display prefix a renderer should repeat (quote bar, list marker), and
// Header marks a table row as its table's header.
type Block struct {
	Kind    BlockKind
	Inlines []Inline
	Source  SourceRange
	Level   int
	Prefix  string
	Header  bool
}

// Document is a parsed source file. Lines holds the byte offset at which each
// one-based source line begins, so Position can map an offset back to a
// line and column. Size is the length of the source in bytes.
type Document struct {
	Blocks []Block
	Lines  []int
	Size   int
}

// Position maps a byte offset to a source position. Offsets below the first
// line start clamp to the start of the document; offsets past the end clamp to
// the last line. A Document with no recorded lines reports line 1, column 1.
func (d Document) Position(byteOffset int) Position {
	if byteOffset < 0 {
		byteOffset = 0
	}
	if len(d.Lines) == 0 {
		return Position{ByteOffset: byteOffset, Line: 1, Column: 1}
	}
	// The last line whose start is <= byteOffset.
	i := sort.Search(len(d.Lines), func(i int) bool {
		return d.Lines[i] > byteOffset
	}) - 1
	if i < 0 {
		i = 0
	}
	return Position{
		ByteOffset: byteOffset,
		Line:       i + 1,
		Column:     byteOffset - d.Lines[i] + 1,
	}
}

// Range builds a SourceRange from two byte offsets.
func (d Document) Range(start, end int) SourceRange {
	return SourceRange{Start: d.Position(start), End: d.Position(end)}
}
