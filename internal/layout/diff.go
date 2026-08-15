package layout

import (
	"strconv"
	"strings"

	"github.com/schambon/mdv/internal/diffdoc"
	"github.com/schambon/mdv/internal/difftext"
	"github.com/schambon/mdv/internal/doc"
)

// Diff rendering lives in package layout rather than a package of its own
// because it is the same job — turning a semantic model into rows of styled
// spans — and it reuses run packing, cell widths, clipping and the Span model.
// A separate package would have to export all of those, weakening the
// encapsulation that keeps escape sequences out of layout.

const (
	// markerCells is the width of the -/+ column. It duplicates what colour
	// already conveys, deliberately: --no-color must stay readable.
	markerCells = 2
	// minPaneCells is the narrowest a side-by-side pane may be before the
	// view falls back to a single column.
	minPaneCells = 24
	// diffTabWidth is the tab stop used to expand tabs. Tabs must become
	// spaces or the two panes lose their alignment, since a terminal advances
	// a tab to its own stop regardless of what column the pane starts at.
	diffTabWidth = 4
	// maxGutterDigits caps the line-number column. Beyond this the numbers
	// cost more than they are worth in a split pane.
	maxGutterDigits = 6
)

// DiffOptions controls diff rendering.
type DiffOptions struct {
	Width       int
	LineNumbers bool
	// SideBySide requests two panes. It is honoured only if the terminal is
	// wide enough to give each pane minPaneCells; otherwise one column is
	// more readable than two cramped ones.
	SideBySide bool
}

// DiffDocument is a laid-out diff. It embeds an ordinary Document, so paging,
// search and highlighting work on a diff exactly as they do on a rendered
// Markdown file, and adds the mapping back to the rows that produced it.
type DiffDocument struct {
	Document
	// Rows maps each rendered line to the index of the diffdoc row it came
	// from. One row can span several lines when it wraps, so this is what
	// lets the viewer expand the fold or jump to the hunk under the viewport.
	Rows []int
}

// RenderDiff lays out an aligned diff at the requested width.
func RenderDiff(d diffdoc.Document, opts DiffOptions) DiffDocument {
	if opts.Width < minWidth {
		opts.Width = minWidth
	}

	r := &diffRenderer{opts: opts, gutter: gutterCells(d, opts.LineNumbers)}
	r.sideBySide = opts.SideBySide && r.paneWidth() >= minPaneCells

	for i, row := range d.Rows {
		r.index = i
		r.row(row)
	}
	return DiffDocument{Document: Document{Lines: r.lines}, Rows: r.rows}
}

type diffRenderer struct {
	opts       DiffOptions
	gutter     int // cells for one line-number column, 0 when disabled
	sideBySide bool
	index      int // the diffdoc row being drawn
	lines      []RenderedLine
	rows       []int
}

// emit records one physical row along with the diff row it belongs to. Every
// line must go through here or the two slices fall out of step.
func (r *diffRenderer) emit(line RenderedLine) {
	r.lines = append(r.lines, line)
	r.rows = append(r.rows, r.index)
}

// gutterCells sizes the line-number column to the widest number the document
// actually contains, rather than a fixed width: in a split pane every wasted
// column comes off the content.
func gutterCells(d diffdoc.Document, enabled bool) int {
	if !enabled {
		return 0
	}

	widest := 0
	var scan func(rows []diffdoc.Row)
	scan = func(rows []diffdoc.Row) {
		for _, row := range rows {
			if row.Kind == diffdoc.RowFolded {
				scan(row.Hidden)
				continue
			}
			widest = max(widest, row.Left.Number, row.Right.Number)
		}
	}
	scan(d.Rows)

	digits := min(len(strconv.Itoa(max(widest, 1))), maxGutterDigits)
	return digits + 1 // one space between the number and the marker
}

// paneWidth is the width of one side-by-side pane, including its gutter and
// marker.
func (r *diffRenderer) paneWidth() int {
	return (r.opts.Width - separatorCells) / 2
}

// contentCells is the room left for text after the gutter and marker.
func (r *diffRenderer) contentCells(pane int) int {
	return max(pane-r.gutter-markerCells, 1)
}

func (r *diffRenderer) row(row diffdoc.Row) {
	switch {
	case row.Kind == diffdoc.RowFolded:
		r.fold(row)
	case r.sideBySide:
		r.splitRow(row)
	default:
		r.unifiedRow(row)
	}
}

// side describes how one side of a row is drawn.
type side struct {
	line      diffdoc.Line
	marker    string
	base      Style
	emphasis  Style
	op        difftext.Op // which word segments belong to this side
	numbering bool        // false suppresses the line number on a filler side
}

// sides returns the left and right descriptions of a row.
func (r *diffRenderer) sides(row diffdoc.Row) (left, right side) {
	left = side{line: row.Left, marker: " ", base: StyleNone, emphasis: StyleNone,
		op: difftext.OpDelete, numbering: row.Left.Present()}
	right = side{line: row.Right, marker: " ", base: StyleNone, emphasis: StyleNone,
		op: difftext.OpInsert, numbering: row.Right.Present()}

	switch row.Kind {
	case diffdoc.RowChanged:
		left.marker, left.base, left.emphasis = "-", StyleDiffRemove, StyleDiffRemoveWord
		right.marker, right.base, right.emphasis = "+", StyleDiffAdd, StyleDiffAddWord
	case diffdoc.RowRemoved:
		left.marker, left.base, left.emphasis = "-", StyleDiffRemove, StyleDiffRemove
	case diffdoc.RowAdded:
		right.marker, right.base, right.emphasis = "+", StyleDiffAdd, StyleDiffAdd
	}
	return left, right
}

// splitRow draws one row as two panes. Each side wraps independently and the
// row occupies as many physical rows as the taller side, so the two panes stay
// in step down the screen.
func (r *diffRenderer) splitRow(row diffdoc.Row) {
	pane := r.paneWidth()
	content := r.contentCells(pane)
	left, right := r.sides(row)
	source := rowSource(row)

	leftLines := packRuns(r.runs(left, content, source), content)
	rightLines := packRuns(r.runs(right, content, source), content)
	height := max(len(leftLines), len(rightLines))

	for i := range height {
		line := RenderedLine{Source: source}

		r.pane(&line, left, leftLines, i, pane, content, source)
		addSpan(&line, Span{
			Text: cellSeparator, Cells: separatorCells, Style: StyleRule, Source: source,
		})
		r.pane(&line, right, rightLines, i, pane, content, source)

		clipSpans(&line, 0, r.opts.Width)
		r.emit(line)
	}
}

// pane draws the i'th physical row of one side, padded to the full pane width
// so the divider and the other pane stay put.
func (r *diffRenderer) pane(line *RenderedLine, s side, wrapped [][]run, i, pane, content int, source doc.SourceRange) {
	// Only the first physical row carries the number and marker; a wrapped
	// continuation is the same source line, and repeating them would read as
	// a second change.
	first := i == 0
	r.gutterSpan(line, s, first, source)
	r.markerSpan(line, s, first, source)

	used := 0
	if i < len(wrapped) {
		for _, rn := range wrapped[i] {
			addSpan(line, Span{Text: rn.text, Cells: rn.cells, Style: rn.style, Background: rn.background, Source: source})
			used += rn.cells
		}
	}

	// A side with no line at all is filler opposite an insertion or deletion.
	// Painting its background makes the gap visible as part of the change.
	fill := s.base
	if !s.line.Present() {
		fill = StyleNone
	}
	if pad := content - used; pad > 0 {
		addSpan(line, Span{Text: strings.Repeat(" ", pad), Cells: pad, Background: fill, Source: source})
	}
}

// unifiedRow draws one row as a single column. A changed row becomes two
// physical rows, the old line then the new one, which is the classic unified
// shape and the only sensible one when there is no room for two panes.
func (r *diffRenderer) unifiedRow(row diffdoc.Row) {
	left, right := r.sides(row)
	source := rowSource(row)

	switch row.Kind {
	case diffdoc.RowEqual:
		r.unifiedSide(left, right, source)
	case diffdoc.RowRemoved:
		r.unifiedSide(left, side{}, source)
	case diffdoc.RowAdded:
		r.unifiedSide(side{}, right, source)
	case diffdoc.RowChanged:
		r.unifiedSide(left, side{}, source)
		r.unifiedSide(side{}, right, source)
	}
}

// unifiedSide draws one physical line of the unified view. Exactly one of the
// two sides carries the text; the other contributes only its line number, so
// an unchanged line can show both.
func (r *diffRenderer) unifiedSide(left, right side, source doc.SourceRange) {
	text := left
	if !left.line.Present() {
		text = right
	}

	content := max(r.opts.Width-2*r.gutter-markerCells, 1)
	wrapped := packRuns(r.runs(text, content, source), content)

	for i, runs := range wrapped {
		line := RenderedLine{Source: source}
		first := i == 0

		r.gutterSpan(&line, left, first, source)
		r.gutterSpan(&line, right, first, source)
		r.markerSpan(&line, text, first, source)

		used := 0
		for _, rn := range runs {
			addSpan(&line, Span{Text: rn.text, Cells: rn.cells, Style: rn.style, Background: rn.background, Source: source})
			used += rn.cells
		}
		// Painting the row out to the edge makes an added or removed line read
		// as a band rather than a ragged stripe.
		if text.base != StyleNone {
			if pad := content - used; pad > 0 {
				addSpan(&line, Span{Text: strings.Repeat(" ", pad), Cells: pad, Background: text.base, Source: source})
			}
		}

		clipSpans(&line, 0, r.opts.Width)
		r.emit(line)
	}
}

func (r *diffRenderer) gutterSpan(line *RenderedLine, s side, show bool, source doc.SourceRange) {
	if r.gutter == 0 {
		return
	}

	text := strings.Repeat(" ", r.gutter)
	if show && s.numbering && s.line.Number > 0 {
		n := strconv.Itoa(s.line.Number)
		if len(n) > r.gutter-1 {
			n = n[len(n)-(r.gutter-1):]
		}
		text = strings.Repeat(" ", r.gutter-1-len(n)) + n + " "
	}
	addSpan(line, Span{Text: text, Cells: r.gutter, Style: StyleDiffGutter, Source: source})
}

func (r *diffRenderer) markerSpan(line *RenderedLine, s side, show bool, source doc.SourceRange) {
	marker := "  "
	if show && s.marker != "" {
		marker = s.marker + " "
	}
	addSpan(line, Span{Text: marker, Cells: markerCells, Background: s.base, Source: source})
}

// fold draws a collapsed run of unchanged rows as one marker row.
func (r *diffRenderer) fold(row diffdoc.Row) {
	n := row.Count()
	noun := "lines"
	if n == 1 {
		noun = "line"
	}
	text := "⋯ " + strconv.Itoa(n) + " unchanged " + noun + " "

	// Ruling out to the edge makes the fold read as a seam in the document
	// rather than a line of text that happens to start with an ellipsis.
	if fill := r.opts.Width - Width(text); fill > 0 {
		text += strings.Repeat("─", fill)
	}
	text, _ = clip(text, r.opts.Width)

	source := rowSource(row)
	line := RenderedLine{Source: source}
	addSpan(&line, Span{Text: text, Cells: Width(text), Style: StyleDiffFold, Source: source})
	r.emit(line)
}

// runs converts one side of a row into wrapping units, splitting at word-diff
// boundaries so the changed part of the line can be emphasised.
func (r *diffRenderer) runs(s side, content int, source doc.SourceRange) []run {
	if !s.line.Present() {
		return nil
	}

	// Tabs are expanded against a running column so a tab in the middle of a
	// line lands where the reader expects, not where the terminal would put it.
	col := 0
	emit := func(text string, background Style) []run {
		expanded, next := expandTabs(text, col)
		col = next
		if expanded == "" {
			return nil
		}
		return []run{{text: expanded, cells: Width(expanded), background: background, source: source}}
	}

	if s.line.Words == nil {
		return emit(s.line.Text, s.base)
	}

	var out []run
	for _, seg := range s.line.Words {
		if seg.Op != difftext.OpEqual && seg.Op != s.op {
			continue // the other side's half of the change
		}
		background := s.base
		if seg.Op == s.op {
			background = s.emphasis
		}
		out = append(out, emit(seg.Text, background)...)
	}
	return out
}

// packRuns breaks runs into rows of at most avail cells, splitting a run
// wherever the row fills up.
//
// Unlike wrapRuns this never breaks at word boundaries and never drops
// whitespace: leading indentation is meaningful in a diff, and two panes only
// stay aligned if every cell of the original line is accounted for.
func packRuns(runs []run, avail int) [][]run {
	if avail < 1 {
		avail = 1
	}

	var rows [][]run
	var cur []run
	used := 0

	for _, rn := range runs {
		text := rn.text
		for text != "" {
			if used >= avail {
				rows = append(rows, cur)
				cur, used = nil, 0
			}
			chunk, w := clip(text, avail-used)
			if chunk == "" {
				// A rune too wide for the space left: start a new row rather
				// than lose it, and accept the overflow if it cannot fit at
				// all. clipSpans is the backstop.
				if used > 0 {
					rows = append(rows, cur)
					cur, used = nil, 0
					continue
				}
				_, size := decodeRune(text)
				chunk, w = text[:size], Width(text[:size])
			}
			cur = append(cur, run{
				text: chunk, cells: w,
				style: rn.style, background: rn.background, source: rn.source,
			})
			used += w
			text = text[len(chunk):]
		}
	}

	// A blank line still occupies one row.
	if cur != nil || len(rows) == 0 {
		rows = append(rows, cur)
	}
	return rows
}

// expandTabs replaces tabs with spaces to the next stop, starting from column
// start, and returns the text with the column it ends on.
func expandTabs(text string, start int) (string, int) {
	col := start
	if !strings.ContainsRune(text, '\t') {
		return text, col + Width(text)
	}

	var sb strings.Builder
	for _, r := range text {
		if r == '\t' {
			n := diffTabWidth - col%diffTabWidth
			sb.WriteString(strings.Repeat(" ", n))
			col += n
			continue
		}
		sb.WriteRune(r)
		col += CellWidth(r)
	}
	return sb.String(), col
}

// rowSource maps a row back to a source line. The new side wins when both are
// present: the reader is normally looking at what the file became, and that is
// the line the editor should open.
func rowSource(row diffdoc.Row) doc.SourceRange {
	line := row.Right.Number
	if line == 0 {
		line = row.Left.Number
	}
	if row.Kind == diffdoc.RowFolded && len(row.Hidden) > 0 {
		return rowSource(row.Hidden[0])
	}

	p := doc.Position{Line: line, Column: 1}
	return doc.SourceRange{Start: p, End: p}
}
