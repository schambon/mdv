package layout

import (
	"strings"

	"github.com/schambon/mdv/internal/doc"
	"github.com/schambon/mdv/internal/md"
)

const (
	// cellSeparator sits between columns and costs three cells.
	cellSeparator  = " │ "
	separatorCells = 3
	// minColumnCells is the narrowest a column may be squeezed.
	minColumnCells = 3
)

// table lays out a group of adjacent table rows, which share column widths.
func (r *renderer) table(rows []doc.Block) {
	cells := make([][]cell, len(rows))
	columns := 0
	for i, row := range rows {
		cells[i] = parseCells(row)
		if n := len(cells[i]); n > columns {
			columns = n
		}
	}
	if columns == 0 {
		return
	}

	widths := columnWidths(cells, columns, r.contentWidth())

	for i, row := range rows {
		r.tableRow(row, cells[i], widths)
		if row.Header {
			r.tableRule(row, widths)
		}
	}
}

// cell is one table cell, re-parsed as inline Markdown so styling inside cells
// works the same as anywhere else.
type cell struct {
	inlines []doc.Inline
	width   int
}

func parseCells(row doc.Block) []cell {
	out := make([]cell, len(row.Inlines))
	for i, raw := range row.Inlines {
		inlines := md.ParseInline(raw.Text, raw.Source.Start.ByteOffset, func(offset int) doc.Position {
			// Cell contents share the row's position: wrapping does not
			// compute finer source ranges.
			return raw.Source.Start
		})
		width := 0
		for _, in := range inlines {
			width += Width(in.Text)
		}
		out[i] = cell{inlines: inlines, width: width}
	}
	return out
}

// columnWidths sizes columns to their natural width, then shrinks the widest
// column one cell at a time until the table fits. Ties pick the earlier column,
// and no column drops below minColumnCells.
func columnWidths(rows [][]cell, columns, available int) []int {
	widths := make([]int, columns)
	for _, row := range rows {
		for c, cl := range row {
			if cl.width > widths[c] {
				widths[c] = cl.width
			}
		}
	}
	for c := range widths {
		if widths[c] < 1 {
			widths[c] = 1
		}
	}

	separators := separatorCells * (columns - 1)
	for total(widths)+separators > available {
		widest, size := -1, minColumnCells
		for c, w := range widths {
			if w > size {
				widest, size = c, w
			}
		}
		if widest < 0 {
			break // every column is already at the minimum
		}
		widths[widest]--
	}
	return widths
}

func total(widths []int) int {
	sum := 0
	for _, w := range widths {
		sum += w
	}
	return sum
}

// tableRow draws one row, clipping and padding each cell to its column width.
func (r *renderer) tableRow(row doc.Block, cells []cell, widths []int) {
	line := r.newRow(row, true)
	contentStart := len(line.Spans)
	base := StyleNone
	if row.Header {
		base = StyleStrong
	}

	for c, w := range widths {
		if c > 0 {
			r.appendSpan(&line, Span{
				Text: cellSeparator, Cells: separatorCells,
				Style: StyleRule, Source: row.Source,
			})
		}

		used := 0
		if c < len(cells) {
			for _, in := range cells[c].inlines {
				if used >= w {
					break
				}
				text, cellsUsed := clip(in.Text, w-used)
				if text == "" {
					continue
				}
				r.appendSpan(&line, Span{
					Text: text, Cells: cellsUsed,
					Style:      inlineStyle(in.Kind, base),
					LinkTarget: in.Target,
					Source:     in.Source,
				})
				used += cellsUsed
			}
		}
		if used < w {
			filler := strings.Repeat(" ", w-used)
			r.appendSpan(&line, Span{Text: filler, Cells: w - used, Style: base, Source: row.Source})
		}
	}

	// A table with more columns than the row has room for cannot be squeezed
	// below the per-column minimum, so the overflow is cut here.
	clipSpans(&line, contentStart, r.contentWidth())
	r.lines = append(r.lines, line)
}

// tableRule draws the rule under a header row.
func (r *renderer) tableRule(row doc.Block, widths []int) {
	var sb strings.Builder
	for c, w := range widths {
		if c > 0 {
			sb.WriteString("─┼─")
		}
		sb.WriteString(strings.Repeat("─", w))
	}

	line := r.newRow(row, false)
	contentStart := len(line.Spans)
	text := sb.String()
	r.appendSpan(&line, Span{Text: text, Cells: Width(text), Style: StyleRule, Source: row.Source})
	clipSpans(&line, contentStart, r.contentWidth())
	r.lines = append(r.lines, line)
}
