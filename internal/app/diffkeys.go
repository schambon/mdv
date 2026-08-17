package app

import (
	"github.com/schambon/mdv/internal/diffdoc"
	"github.com/schambon/mdv/internal/terminal"
)

// Diff mode adds folding and hunk navigation. The bindings are single keys
// rather than vim's z-prefixed chords: the event loop dispatches one keypress
// at a time, and a pending-prefix state machine is a poor trade for two
// keystrokes of familiarity.

// handleDiffRune handles the keys that only exist while comparing two files,
// reporting whether it consumed the keypress.
func (a *App) handleDiffRune(r rune) bool {
	if !a.cfg.diffMode() {
		return false
	}

	switch r {
	case 'x':
		a.expandFold()
	case 'X':
		a.expandAllFolds()
	case 'z':
		a.collapseFolds()
	case ']':
		a.jumpChange(forward)
	case '[':
		a.jumpChange(backward)
	case '>':
		return a.selectRelative(1)
	case '<':
		return a.selectRelative(-1)
	default:
		return false
	}
	return true
}

// handleDiffKey handles the non-rune keys diff mode adds. The arrows move
// through the changed-file list: nothing scrolls horizontally, so they are
// free, and they are what a reader tries first on a list.
func (a *App) handleDiffKey(key terminal.Key) bool {
	if !a.cfg.diffMode() {
		return false
	}
	switch key {
	case terminal.KeyRight:
		return a.selectRelative(1)
	case terminal.KeyLeft:
		return a.selectRelative(-1)
	}
	return false
}

type direction int

const (
	forward direction = iota
	backward
)

// expandFold opens the first fold at or below the top of the viewport, which
// is the same row the status line and the editor key already act on.
//
// Nothing above the top of the viewport moves, so the view stays put without
// any position bookkeeping.
func (a *App) expandFold() {
	index := a.foldBelow(a.top)
	if index < 0 {
		a.message = "no folded context below"
		return
	}
	a.diff.Rows = diffdoc.Expand(a.diff.Rows, index)
	a.render()
	a.top = a.clamp(a.top)
	a.refreshMatches()
}

// foldBelow returns the index in a.diff.Rows of the first fold at or after a
// rendered line, or -1 when there is none.
func (a *App) foldBelow(line int) int {
	for i := max(line, 0); i < len(a.diffRows); i++ {
		row := a.diffRows[i]
		if row < len(a.diff.Rows) && a.diff.Rows[row].Kind == diffdoc.RowFolded {
			return row
		}
	}
	return -1
}

func (a *App) expandAllFolds() {
	a.reflow(func() { a.diff.Rows = diffdoc.ExpandAll(a.diff.Rows) })
}

// collapseFolds re-folds the document by rebuilding it, which is both simpler
// and more predictable than trying to reverse individual expansions.
func (a *App) collapseFolds() {
	if a.cfg.Context < 0 {
		a.message = "folding is disabled"
		return
	}
	a.reflow(a.buildDiff)
}

// jumpChange moves the viewport to the next or previous hunk. A run of
// adjacent changed rows is one hunk: stopping on every line of a rewritten
// block would make the key useless on the diffs that need it most.
func (a *App) jumpChange(dir direction) {
	starts := a.hunkStarts()
	if len(starts) == 0 {
		a.message = "no changes"
		return
	}

	if dir == forward {
		for _, line := range starts {
			if line > a.top {
				a.top = a.clamp(line)
				return
			}
		}
		a.message = "no further changes"
		return
	}

	for i := len(starts) - 1; i >= 0; i-- {
		if starts[i] < a.top {
			a.top = a.clamp(starts[i])
			return
		}
	}
	a.message = "no earlier changes"
}

// hunkStarts lists the rendered lines that begin a run of changed rows.
func (a *App) hunkStarts() []int {
	var starts []int
	for i, row := range a.diffRows {
		if !a.isChange(row) {
			continue
		}
		// The previous rendered line belonging to an unchanged row — or no
		// previous line at all — makes this the start of a hunk. Comparing
		// lines rather than rows also skips a wrapped continuation, which
		// shares its row with the line above.
		if i == 0 || !a.isChange(a.diffRows[i-1]) {
			starts = append(starts, i)
		}
	}
	return starts
}

func (a *App) isChange(row int) bool {
	if row < 0 || row >= len(a.diff.Rows) {
		return false
	}
	switch a.diff.Rows[row].Kind {
	case diffdoc.RowAdded, diffdoc.RowRemoved, diffdoc.RowChanged:
		return true
	}
	return false
}
