package layout

import (
	"strings"

	"github.com/schambon/mdv/internal/doc"
)

const (
	// metaGap separates the key column from the value column.
	metaGap = 2
	// minMetaValueCells is the narrowest the value column may be squeezed
	// before the keys start losing characters instead.
	minMetaValueCells = 8
)

// meta lays out a run of adjacent frontmatter lines, which share a key column
// the way table rows share column widths. Keys are padded to the widest key in
// the run, so the values line up; a value too long for its column wraps with a
// hanging indent to that column rather than back to the margin.
func (r *renderer) meta(blocks []doc.Block) {
	width := r.contentWidth()
	keyCells := metaKeyCells(blocks, width)
	value := width - keyCells - metaGap
	if value < 1 {
		value = 1
	}

	for _, b := range blocks {
		r.metaRow(b, keyCells, value)
	}
}

// metaKeyCells sizes the key column to the widest key, then squeezes it if
// that would leave the values no room to speak of.
func metaKeyCells(blocks []doc.Block, width int) int {
	cells := 0
	for _, b := range blocks {
		if w := Width(b.Prefix); w > cells {
			cells = w
		}
	}
	if limit := width - metaGap - minMetaValueCells; cells > limit {
		cells = limit
	}
	if cells < 1 {
		cells = 1
	}
	return cells
}

// metaRow draws one frontmatter line: the key, padded to the shared column,
// then the value wrapped into what is left.
func (r *renderer) metaRow(b doc.Block, keyCells, valueCells int) {
	key, cells := b.Prefix, Width(b.Prefix)
	if cells > keyCells {
		key, cells = clip(key, keyCells)
	}
	pad := strings.Repeat(" ", keyCells-cells+metaGap)
	blank := strings.Repeat(" ", keyCells+metaGap)

	for i, wl := range wrapRuns(r.runs(b.Inlines, StyleNone), valueCells) {
		line := r.newRow(b, i == 0)
		contentStart := len(line.Spans)

		if i == 0 && key != "" {
			r.appendSpan(&line, Span{Text: key, Cells: cells, Style: StyleMetaKey, Source: b.Source})
		}
		// The column padding is only written when something follows it: a key
		// with no value must not leave trailing whitespace on the row.
		if len(wl) > 0 {
			gap := pad
			if i > 0 || key == "" {
				gap = blank
			}
			r.appendSpan(&line, Span{Text: gap, Cells: Width(gap), Style: StyleNone, Source: b.Source})
		}
		for _, rn := range wl {
			r.appendSpan(&line, Span{
				Text: rn.text, Cells: rn.cells, Style: rn.style,
				LinkTarget: rn.linkTarget, Source: rn.source, Mark: rn.mark,
			})
		}

		clipSpans(&line, contentStart, r.contentWidth())
		r.lines = append(r.lines, line)
	}
}
