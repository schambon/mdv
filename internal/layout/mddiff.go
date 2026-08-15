package layout

import (
	"strings"

	"github.com/schambon/mdv/internal/diffdoc"
	"github.com/schambon/mdv/internal/doc"
)

// Markdown diff rendering. A row whose sides carry parsed blocks is drawn as
// rendered Markdown rather than as text, which changes the shape of the view:
//
//   - An unchanged unit is drawn ONCE across the full width. Both sides are
//     identical by construction — the alignment key covers everything that
//     affects rendering — so showing it twice would waste half the terminal
//     and wrap prose into two narrow columns for no benefit.
//   - A changed unit is drawn in two panes, because that is the only part the
//     reader is comparing.
//   - A horizontal rule marks every transition between the two, so the eye can
//     tell "this is the document" from "this is a comparison".

// markdownRow reports whether a row carries parsed blocks and should be drawn
// as rendered Markdown.
func markdownRow(row diffdoc.Row) bool {
	return row.Left.Blocks != nil || row.Right.Blocks != nil
}

// mdRow draws one Markdown row, emitting a section divider first if the
// presentation is changing between full width and split panes.
func (r *diffRenderer) mdRow(row diffdoc.Row) {
	source := rowSource(row)
	split := row.Kind != diffdoc.RowEqual

	r.section(split, false, source)
	if split {
		r.splitUnit(row, source)
		return
	}
	r.fullWidthUnit(row, source)
}

// section emits a divider when the presentation changes.
//
// A fold draws its own full-width band, so it neither needs a divider of its
// own nor one on the way back out of it. A divider as the very first row would
// read as a stray rule rather than a boundary, so the opening transition is
// suppressed too.
func (r *diffRenderer) section(split, isFold bool, source doc.SourceRange) {
	if split != r.inSplit && !isFold && !r.lastWasFold && len(r.lines) > 0 {
		r.divider(source)
	}
	r.inSplit, r.lastWasFold = split, isFold
}

// divider draws the rule between a full-width section and a split one.
func (r *diffRenderer) divider(source doc.SourceRange) {
	text := strings.Repeat("─", max(r.opts.Width, 1))

	line := RenderedLine{Source: source}
	addSpan(&line, Span{Text: text, Cells: Width(text), Style: StyleRule, Source: source})
	r.emit(line)
}

// close finishes the document, bracketing a trailing split section so it does
// not run into the bottom of the screen unmarked.
func (r *diffRenderer) close(source doc.SourceRange) {
	if r.inSplit && !r.lastWasFold {
		r.divider(source)
	}
}

// fullWidthUnit draws an unchanged unit once, across the whole width.
func (r *diffRenderer) fullWidthUnit(row diffdoc.Row, source doc.SourceRange) {
	blocks := row.Right.Blocks
	if blocks == nil {
		blocks = row.Left.Blocks
	}

	for _, line := range renderBlocks(blocks, r.opts.Width) {
		line.Source = source
		clipSpans(&line, 0, r.opts.Width)
		r.emit(line)
	}
}

// splitUnit draws a changed unit as two panes, each side rendered
// independently and the row occupying as many physical rows as the taller.
func (r *diffRenderer) splitUnit(row diffdoc.Row, source doc.SourceRange) {
	if !r.sideBySide {
		r.stackedUnit(row, source)
		return
	}

	content := r.contentCells(r.paneWidth())
	left, right := r.sides(row)

	leftLines := r.unitLines(left, content)
	rightLines := r.unitLines(right, content)

	for i := range max(len(leftLines), len(rightLines)) {
		line := RenderedLine{Source: source}

		r.unitPane(&line, left, leftLines, i, content, source)
		addSpan(&line, Span{
			Text: cellSeparator, Cells: separatorCells, Style: StyleRule, Source: source,
		})
		r.unitPane(&line, right, rightLines, i, content, source)

		clipSpans(&line, 0, r.opts.Width)
		r.emit(line)
	}
}

// stackedUnit is the one-column form: the old unit, then the new one. It is
// what a terminal too narrow for two panes gets.
func (r *diffRenderer) stackedUnit(row diffdoc.Row, source doc.SourceRange) {
	left, right := r.sides(row)
	content := max(r.opts.Width-2*r.gutter-markerCells, 1)

	for _, drawing := range []struct {
		s      side
		isLeft bool
	}{{left, true}, {right, false}} {
		if !drawing.s.line.Present() {
			continue
		}

		for i, rendered := range r.unitLines(drawing.s, content) {
			line := RenderedLine{Source: source}

			// Both gutters are drawn so a number stays in its own file's
			// column, matching the unified text form.
			if drawing.isLeft {
				r.gutterSpan(&line, drawing.s, i == 0, source)
				r.gutterSpan(&line, side{}, false, source)
			} else {
				r.gutterSpan(&line, side{}, false, source)
				r.gutterSpan(&line, drawing.s, i == 0, source)
			}
			r.markerSpan(&line, drawing.s, i == 0, source)

			for _, span := range rendered.Spans {
				span.Source = source
				addSpan(&line, span)
			}

			clipSpans(&line, 0, r.opts.Width)
			r.emit(line)
		}
	}
}

// unitLines renders one side's blocks and washes them with that side's diff
// colour. The wash is a background, so the Markdown styling underneath — a
// heading's colour, a link's underline — survives it.
func (r *diffRenderer) unitLines(s side, content int) []RenderedLine {
	if !s.line.Present() {
		return nil
	}

	lines := renderBlocks(s.line.Blocks, content)
	for i := range lines {
		for j := range lines[i].Spans {
			if lines[i].Spans[j].Background == StyleNone {
				lines[i].Spans[j].Background = s.base
			}
		}
	}
	return lines
}

// unitPane places one side's i'th physical row, padded to the pane width so
// the divider and the opposite pane stay put.
func (r *diffRenderer) unitPane(line *RenderedLine, s side, rendered []RenderedLine, i, content int, source doc.SourceRange) {
	first := i == 0
	r.gutterSpan(line, s, first, source)
	r.markerSpan(line, s, first, source)

	used := 0
	if i < len(rendered) {
		for _, span := range rendered[i].Spans {
			span.Source = source
			addSpan(line, span)
			used += span.Cells
		}
	}

	fill := s.base
	if !s.line.Present() {
		fill = StyleNone
	}
	if pad := content - used; pad > 0 {
		addSpan(line, Span{Text: strings.Repeat(" ", pad), Cells: pad, Background: fill, Source: source})
	}
}

// renderBlocks lays out a unit's blocks on their own.
//
// Render reads only Document.Blocks — it never touches Lines or Size — so a
// bare slice of blocks renders exactly as it would in the whole document.
// Line numbers are off because the diff draws its own gutters, and the width
// passed is the row budget: Render subtracts its own two-cell indent, so the
// rows come back no wider than asked.
func renderBlocks(blocks []doc.Block, width int) []RenderedLine {
	return Render(doc.Document{Blocks: blocks}, Options{Width: width}).Lines
}
