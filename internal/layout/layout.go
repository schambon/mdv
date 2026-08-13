// Package layout turns a semantic document into rows of styled spans ready to
// be drawn. It owns wrapping, prefixes, table sizing and cell widths, but knows
// nothing about ANSI sequences: spans carry a Style that package style maps to
// escape codes.
package layout

import (
	"strconv"
	"strings"

	"github.com/schambon/mdv/internal/doc"
)

// Style names the visual role of a span.
type Style int

const (
	StyleNone Style = iota
	StyleHeading
	StyleEmphasis
	StyleStrong
	StyleStrike
	StyleCode
	StyleInlineCode
	StyleQuote
	StyleRule
	StyleLink
	StyleSearch
	StyleSearchActive
	StyleStatus
)

const (
	// minWidth is the narrowest document mdv will lay out.
	minWidth = 10
	// horizontalPadding is the allowance on each side; only the left is drawn.
	horizontalPadding = 2
	// gutterWidth is six right-aligned digits plus one space.
	gutterWidth  = 7
	gutterDigits = 6
)

// Span is a run of text sharing one style and link target.
type Span struct {
	Text       string
	Cells      int
	Style      Style
	LinkTarget string
	Source     doc.SourceRange
}

// RenderedLine is one drawable row. SearchText is the row's visible text,
// including layout prefixes and padding but excluding Markdown delimiters, and
// is the substrate searching operates on.
type RenderedLine struct {
	Spans      []Span
	SearchText string
	Source     doc.SourceRange
}

// Document is a laid-out document.
type Document struct {
	Lines []RenderedLine
}

// Options controls rendering.
type Options struct {
	Width       int
	LineNumbers bool
}

// Render lays out a semantic document at the requested width.
func Render(d doc.Document, opts Options) Document {
	if opts.Width < minWidth {
		opts.Width = minWidth
	}

	r := &renderer{opts: opts}
	for i := 0; i < len(d.Blocks); {
		if d.Blocks[i].Kind == doc.BlockTableRow {
			group := i
			for i < len(d.Blocks) && d.Blocks[i].Kind == doc.BlockTableRow {
				i++
			}
			r.table(d.Blocks[group:i])
			continue
		}
		r.block(d.Blocks[i])
		i++
	}
	return Document{Lines: r.lines}
}

type renderer struct {
	opts  Options
	lines []RenderedLine
}

// contentWidth is the space available to a row's text, after the side
// allowance and the line-number gutter.
func (r *renderer) contentWidth() int {
	w := r.opts.Width - horizontalPadding
	if r.opts.LineNumbers {
		w -= gutterWidth
	}
	if w < 1 {
		w = 1
	}
	return w
}

// gutter renders the line-number column. Numbers longer than six digits keep
// their last six.
func (r *renderer) gutter(sourceLine int, show bool) string {
	if !r.opts.LineNumbers {
		return ""
	}
	if !show || sourceLine <= 0 {
		return strings.Repeat(" ", gutterWidth)
	}
	n := strconv.Itoa(sourceLine)
	if len(n) > gutterDigits {
		n = n[len(n)-gutterDigits:]
	}
	return strings.Repeat(" ", gutterDigits-len(n)) + n + " "
}

// block dispatches a single non-table block.
func (r *renderer) block(b doc.Block) {
	switch b.Kind {
	case doc.BlockBlank:
		r.emitPlain("", StyleNone, b, true)
	case doc.BlockRule:
		r.emitPlain(strings.Repeat("─", r.contentWidth()), StyleRule, b, true)
	case doc.BlockCode:
		r.code(b)
	case doc.BlockHeading:
		r.wrapped(b, b.Prefix, StyleHeading)
	case doc.BlockQuote:
		r.wrapped(b, b.Prefix, StyleQuote)
	case doc.BlockListItem:
		r.wrapped(b, b.Prefix, StyleNone)
	default:
		r.wrapped(b, "", StyleNone)
	}
}

// emitPlain writes a single row holding one unstyled run of text.
func (r *renderer) emitPlain(text string, style Style, b doc.Block, first bool) {
	line := r.newRow(b, first)
	if text != "" {
		r.appendSpan(&line, Span{Text: text, Cells: Width(text), Style: style, Source: b.Source})
	}
	r.lines = append(r.lines, line)
}

// code expands a code block into one row per physical line, each keeping the
// block's source range. Code rows are indented four spaces and never reflowed.
func (r *renderer) code(b doc.Block) {
	var text string
	if len(b.Inlines) > 0 {
		text = b.Inlines[0].Text
	}
	for i, physical := range strings.Split(text, "\n") {
		row := "    " + physical
		for _, chunk := range hardSplit(row, r.contentWidth()) {
			line := r.newRow(b, i == 0)
			r.appendSpan(&line, Span{
				Text:   chunk,
				Cells:  Width(chunk),
				Style:  StyleCode,
				Source: b.Source,
			})
			r.lines = append(r.lines, line)
		}
	}
}

// newRow starts a row with its gutter and side allowance.
func (r *renderer) newRow(b doc.Block, first bool) RenderedLine {
	line := RenderedLine{Source: b.Source}
	if g := r.gutter(b.Source.Start.Line, first); g != "" {
		r.appendSpan(&line, Span{Text: g, Cells: Width(g), Style: StyleNone, Source: b.Source})
	}
	indent := strings.Repeat(" ", horizontalPadding)
	r.appendSpan(&line, Span{Text: indent, Cells: horizontalPadding, Style: StyleNone, Source: b.Source})
	return line
}

func (r *renderer) appendSpan(line *RenderedLine, s Span) {
	line.Spans = append(line.Spans, s)
	line.SearchText += s.Text
}

// retext recomputes a row's search text after its spans have been altered.
func retext(line *RenderedLine) {
	var sb strings.Builder
	for _, s := range line.Spans {
		sb.WriteString(s.Text)
	}
	line.SearchText = sb.String()
}

// trimTrailingSpace drops whitespace-only spans from the end of a row, so a
// row broken after a space does not carry that space to the line end.
func trimTrailingSpace(line *RenderedLine, from int) {
	for len(line.Spans) > from && isSpaceRun(line.Spans[len(line.Spans)-1].Text) {
		line.Spans = line.Spans[:len(line.Spans)-1]
	}
	retext(line)
}

// clipSpans truncates a row's spans, starting at index from, so they occupy at
// most cells columns. It is the backstop that keeps any row from overflowing
// the frame the terminal draws.
func clipSpans(line *RenderedLine, from, cells int) {
	used := 0
	for i := from; i < len(line.Spans); i++ {
		if used >= cells {
			line.Spans = line.Spans[:i]
			retext(line)
			return
		}
		if used+line.Spans[i].Cells > cells {
			text, w := clip(line.Spans[i].Text, cells-used)
			line.Spans[i].Text, line.Spans[i].Cells = text, w
			line.Spans = line.Spans[:i+1]
			retext(line)
			return
		}
		used += line.Spans[i].Cells
	}
}

// run is a unit of wrapping: a maximal whitespace or non-whitespace stretch of
// one inline, carrying that inline's style and target.
type run struct {
	text       string
	cells      int
	space      bool
	style      Style
	linkTarget string
	source     doc.SourceRange
}

// wrapped lays out a block's inlines, breaking rows between runs. Continuation
// rows reproduce the prefix as spaces.
func (r *renderer) wrapped(b doc.Block, prefix string, base Style) {
	runs := r.runs(b.Inlines, base)
	if len(runs) == 0 {
		r.emitPlain(prefix, base, b, true)
		return
	}

	limit := r.contentWidth()
	prefixCells := Width(prefix)
	// At very narrow widths the prefix alone can fill the row; clip it so at
	// least one column is left for text.
	if prefixCells >= limit {
		prefix, prefixCells = clip(prefix, limit-1)
	}
	avail := limit - prefixCells

	line := r.newRow(b, true)
	if prefix != "" {
		r.appendSpan(&line, Span{Text: prefix, Cells: prefixCells, Style: base, Source: b.Source})
	}
	contentStart := len(line.Spans)
	used := 0
	hasContent := false

	flush := func() {
		trimTrailingSpace(&line, contentStart)
		r.lines = append(r.lines, line)
		line = r.newRow(b, false)
		if prefixCells > 0 {
			blank := strings.Repeat(" ", prefixCells)
			r.appendSpan(&line, Span{Text: blank, Cells: prefixCells, Style: StyleNone, Source: b.Source})
		}
		contentStart = len(line.Spans)
		used = 0
		hasContent = false
	}

	emit := func(text string, rn run) {
		r.appendSpan(&line, Span{
			Text: text, Cells: Width(text), Style: rn.style,
			LinkTarget: rn.linkTarget, Source: rn.source,
		})
		used += Width(text)
		hasContent = true
	}

	for _, rn := range runs {
		// A run too wide for an empty row is split rather than allowed to
		// overflow, which would corrupt the frame the terminal draws.
		if rn.cells > avail {
			if hasContent {
				flush()
			}
			if rn.space {
				continue
			}
			for i, chunk := range hardSplit(rn.text, avail) {
				if i > 0 {
					flush()
				}
				emit(chunk, rn)
			}
			continue
		}

		if hasContent && used+rn.cells > avail {
			flush()
		}
		// Whitespace never opens a row: it would render as a ragged indent.
		if !hasContent && rn.space {
			continue
		}
		emit(rn.text, rn)
	}

	trimTrailingSpace(&line, contentStart)
	r.lines = append(r.lines, line)
}

// runs converts inlines into wrapping units. Inline kinds override the block's
// base style.
func (r *renderer) runs(inlines []doc.Inline, base Style) []run {
	var out []run
	for _, in := range inlines {
		style := inlineStyle(in.Kind, base)
		for _, piece := range splitRuns(in.Text) {
			out = append(out, run{
				text:       piece,
				cells:      Width(piece),
				space:      isSpaceRun(piece),
				style:      style,
				linkTarget: in.Target,
				source:     in.Source,
			})
		}
	}
	return out
}

// inlineStyle maps an inline kind onto a style, leaving the block's base style
// in place for plain text.
func inlineStyle(kind doc.InlineKind, base Style) Style {
	switch kind {
	case doc.InlineEmphasis:
		return StyleEmphasis
	case doc.InlineStrong:
		return StyleStrong
	case doc.InlineStrike:
		return StyleStrike
	case doc.InlineCode:
		return StyleInlineCode
	case doc.InlineLink:
		return StyleLink
	default:
		return base
	}
}

// splitRuns breaks text at every transition between whitespace and
// non-whitespace.
func splitRuns(text string) []string {
	if text == "" {
		return nil
	}
	var runs []string
	start := 0
	prev := isSpaceRune(rune(text[0]))
	for i, r := range text {
		if i == 0 {
			continue
		}
		if cur := isSpaceRune(r); cur != prev {
			runs = append(runs, text[start:i])
			start = i
			prev = cur
		}
	}
	return append(runs, text[start:])
}

func isSpaceRune(r rune) bool {
	return r == ' ' || r == '\t' || r == '\n' || r == '\r' || r == '\v' || r == '\f' || r == 0xA0
}

func isSpaceRun(s string) bool {
	for _, r := range s {
		if !isSpaceRune(r) {
			return false
		}
	}
	return s != ""
}

// hardSplit breaks text into chunks of at most cells columns. It is the last
// resort for content with no wrapping opportunity, such as a long URL.
func hardSplit(text string, cells int) []string {
	if cells < 1 {
		cells = 1
	}
	if Width(text) <= cells {
		return []string{text}
	}
	var chunks []string
	for text != "" {
		chunk, _ := clip(text, cells)
		if chunk == "" { // a single rune wider than the row
			_, size := decodeRune(text)
			chunk = text[:size]
		}
		chunks = append(chunks, chunk)
		text = text[len(chunk):]
	}
	return chunks
}

func decodeRune(s string) (rune, int) {
	for i, r := range s {
		if i > 0 {
			return r, i
		}
	}
	return rune(s[0]), len(s)
}

// Nearest returns the index of the rendered row closest to a source line,
// preferring the earliest row when distances tie.
func Nearest(d Document, sourceLine int) int {
	best, bestDist := 0, -1
	for i, line := range d.Lines {
		start := line.Source.Start.Line
		if start <= 0 {
			continue
		}
		dist := start - sourceLine
		if dist < 0 {
			dist = -dist
		}
		if bestDist < 0 || dist < bestDist {
			best, bestDist = i, dist
		}
	}
	return best
}
