package app

import (
	"fmt"
	"strings"

	"github.com/schambon/mdv/internal/layout"
)

// The sidebar lists the changed files when git mode found more than one. It
// has exactly one interaction — pick a file — so there is no focus model:
// dedicated keys move the selection and the diff pane is rebuilt to match. A
// focus model would buy nothing and cost per-focus key routing, a per-focus
// status line, and an answer to "which pane does j scroll".
//
// It is drawn by composition in draw, not by RenderDiff: the diff is laid out
// at a narrower width and knows nothing about the list beside it.

const (
	// sidebarSeparator divides the list from the diff.
	sidebarSeparator      = "│ "
	sidebarSeparatorCells = 2
	// entryCells is what an entry spends before the path: a selection marker
	// and git's status letter.
	entryCells = 4
	// The list is sized to its longest entry, held between these bounds: too
	// narrow and every path is an ellipsis, too wide and it is all padding.
	minSidebarCells = 18
	maxSidebarCells = 32
	// minNameCells is the shortest stub of a path worth showing. Below it the
	// counts are dropped instead, for the whole list at once.
	minNameCells = 10
	// minSidebarDiffCells is what the diff must be left with. Below it the
	// list is dropped entirely, mirroring the side-by-side fallback.
	minSidebarDiffCells = 40
	// ellipsis marks a path clipped at its head, since the tail of a path is
	// the part that identifies the file.
	ellipsis = "…"
)

// sidebarWidth is the cells the file list itself occupies, or 0 when there is
// no list to show or the terminal cannot spare them.
//
// It is sized to its longest entry rather than to a fraction of the width: a
// list of short paths should not reserve columns it will only pad, and a list
// of long ones is not helped by clipping every entry to the same fraction.
func (a *App) sidebarWidth() int {
	if len(a.files) < 2 {
		return 0
	}

	spare := a.renderWidth() - sidebarSeparatorCells - minSidebarDiffCells
	if spare < minSidebarCells {
		return 0
	}

	// Each entry wants room for its path and its counts. A path shorter than
	// minNameCells still asks for that much, so that a list of short names
	// does not size itself down to where sidebarShowsCounts then drops the
	// counts the width was computed to include.
	widest := 0
	for _, file := range a.files {
		name := max(cells(file.Path), minNameCells)
		widest = max(widest, entryCells+name+cells(a.sidebarCounts(file.Path)))
	}
	return min(max(widest, minSidebarCells), maxSidebarCells, spare)
}

// sidebarCells is what the sidebar costs the diff, including the separator.
func (a *App) sidebarCells() int {
	if width := a.sidebarWidth(); width > 0 {
		return width + sidebarSeparatorCells
	}
	return 0
}

// selectRelative moves the selection through the list. It is gated on the list
// existing, not on the sidebar being drawn: on a terminal too narrow for the
// list the status line still says "file 2/5", and the keys must still work.
func (a *App) selectRelative(delta int) bool {
	if len(a.files) < 2 {
		return false
	}
	index := a.current + delta
	if index < 0 || index >= len(a.files) {
		a.message = "no further changed file"
		return true
	}
	a.selectFile(index)
	return true
}

// sidebarRows builds one row of spans per screen row. They are drawn into a
// throwaway RenderedLine and never stored: such a line's SearchText would not
// equal the concatenation of its spans, breaking the invariant search relies
// on.
func (a *App) sidebarRows(width, height int) [][]layout.Span {
	rows := make([][]layout.Span, height)
	top := a.sidebarTop(height)
	counts := a.sidebarShowsCounts(width)

	for row := range height {
		file := top + row
		if file >= len(a.files) {
			rows[row] = []layout.Span{pad(width, layout.StyleNone)}
			continue
		}
		rows[row] = a.sidebarEntry(file, width, counts)
	}
	return rows
}

// sidebarShowsCounts decides for the whole list at once whether there is room
// for git's numbers beside the paths. Deciding per entry would leave the paths
// stopping at a different column on every row.
func (a *App) sidebarShowsCounts(width int) bool {
	widest := 0
	for _, file := range a.files {
		widest = max(widest, cells(a.sidebarCounts(file.Path)))
	}
	return width-entryCells-widest >= minNameCells
}

// sidebarTop scrolls the list only when it does not fit, keeping the selection
// on screen with context around it.
func (a *App) sidebarTop(height int) int {
	if len(a.files) <= height {
		return 0
	}
	top := a.current - height/2
	if maxTop := len(a.files) - height; top > maxTop {
		top = maxTop
	}
	return max(top, 0)
}

// sidebarEntry draws one file: a selection marker, git's status letter, the
// path, and git's line counts. The marker duplicates what the highlight
// conveys, deliberately, so --no-color still shows which file is open.
func (a *App) sidebarEntry(index, width int, showCounts bool) []layout.Span {
	file := a.files[index]

	marker := "  "
	if index == a.current {
		marker = "> "
	}
	head := fmt.Sprintf("%s%c ", marker, file.Status)

	counts := ""
	if showCounts {
		counts = a.sidebarCounts(file.Path)
	}
	// The path is what identifies the file, so it keeps whatever is left of
	// the row after the fixed columns and the counts.
	body := width - cells(head) - cells(counts)
	name := clipCellsLeft(file.Path, body)

	text := head + name + strings.Repeat(" ", max(body-cells(name), 0)) + counts
	style, background := layout.StyleNone, layout.StyleNone
	if index == a.current {
		background = layout.StyleStatus
	} else {
		style = layout.StyleDiffGutter
	}
	return []layout.Span{{Text: text, Cells: cells(text), Style: style, Background: background}}
}

// sidebarCounts labels a file with git's own line counts. They come from
// `git diff --numstat` for the whole list at once, so the label costs nothing
// per file; in Markdown mode they will not match the status line, which counts
// blocks of the file actually on screen.
func (a *App) sidebarCounts(path string) string {
	stat, ok := a.stats[path]
	switch {
	case !ok:
		return ""
	case stat.Binary:
		return " bin"
	default:
		return fmt.Sprintf(" +%d -%d", stat.Added, stat.Removed)
	}
}

// compose builds the frame row for one screen line: the sidebar entry, the
// separator, then the diff row with its search highlighting already applied.
func (a *App) compose(sidebar []layout.Span, content layout.RenderedLine) layout.RenderedLine {
	spans := make([]layout.Span, 0, len(sidebar)+1+len(content.Spans))
	spans = append(spans, sidebar...)
	spans = append(spans, layout.Span{
		Text: sidebarSeparator, Cells: sidebarSeparatorCells, Style: layout.StyleRule,
	})
	return layout.RenderedLine{Spans: append(spans, content.Spans...)}
}

func pad(width int, style layout.Style) layout.Span {
	if width < 0 {
		width = 0
	}
	return layout.Span{Text: strings.Repeat(" ", width), Cells: width, Style: style}
}

func cells(text string) int {
	total := 0
	for _, r := range text {
		total += layout.CellWidth(r)
	}
	return total
}

// clipCellsLeft keeps the tail of a string, which for a path is the part that
// names the file, and marks the cut with an ellipsis.
func clipCellsLeft(text string, budget int) string {
	if budget <= 0 {
		return ""
	}
	if cells(text) <= budget {
		return text
	}
	if budget == 1 {
		return ellipsis
	}

	runes := []rune(text)
	used, cut := 0, len(runes)
	for i := len(runes) - 1; i >= 0; i-- {
		w := layout.CellWidth(runes[i])
		if used+w > budget-1 { // one cell for the ellipsis
			break
		}
		used += w
		cut = i
	}
	return ellipsis + string(runes[cut:])
}
