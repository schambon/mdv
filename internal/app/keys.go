package app

import (
	"fmt"

	"github.com/schambon/mdv/internal/search"
	"github.com/schambon/mdv/internal/terminal"
)

// Control keys that arrive as bare runes in raw mode.
const (
	ctrlB = 0x02
	ctrlF = 0x06
	ctrlR = 0x12
)

const keyHelp = "j/k move  space/b page  g/G ends  / ? search  n/N next  l numbers  v edit  r reload  q quit"

const diffKeyHelp = "j/k move  space/b page  [ ] hunk  x/X expand  z collapse  l numbers  / search  v edit  q quit"

// help is the key summary for the current mode.
func (a *App) help() string {
	if a.cfg.diffMode() {
		return diffKeyHelp
	}
	return keyHelp
}

// handle dispatches one event and reports whether the viewer should quit.
func (a *App) handle(ev terminal.Event) (quit bool, err error) {
	if a.mode == modeSearch {
		a.handleSearchKey(ev)
		return false, nil
	}
	return a.handleNormalKey(ev)
}

// handleNormalKey handles a keypress in normal mode.
func (a *App) handleNormalKey(ev terminal.Event) (bool, error) {
	switch ev.Key {
	case terminal.KeyDown, terminal.KeyEnter:
		a.scroll(1)
	case terminal.KeyUp:
		a.scroll(-1)
	case terminal.KeyPageDown:
		a.scroll(a.pageHeight())
	case terminal.KeyPageUp:
		a.scroll(-a.pageHeight())
	case terminal.KeyHome:
		a.top = 0
	case terminal.KeyEnd:
		a.top = a.maxTop()
	case terminal.KeyEscape:
		// Nothing to cancel in normal mode.
	case terminal.KeyRune:
		return a.handleRune(ev.Rune)
	}
	return false, nil
}

func (a *App) handleRune(r rune) (bool, error) {
	// Diff bindings are checked first so they can claim keys the base viewer
	// does not use. They decline everything when not comparing two files.
	if a.handleDiffRune(r) {
		return false, nil
	}

	switch r {
	case 'q':
		return true, nil
	case 'j':
		a.scroll(1)
	case 'k':
		a.scroll(-1)
	case ' ', ctrlF:
		a.scroll(a.pageHeight())
	case 'b', ctrlB:
		a.scroll(-a.pageHeight())
	case 'g':
		a.top = 0
	case 'G':
		a.top = a.maxTop()
	case 'h':
		a.message = a.help()
	case 'l':
		a.toggleLineNumbers()
	case 'r', ctrlR:
		if err := a.reload(); err != nil {
			a.message = err.Error()
		}
	case 'v':
		return false, a.edit()
	case '/':
		a.beginSearch(search.Forward)
	case '?':
		a.beginSearch(search.Backward)
	case 'n':
		a.repeatSearch(a.direction)
	case 'N':
		a.repeatSearch(opposite(a.direction))
	}
	return false, nil
}

func (a *App) scroll(delta int) {
	a.top = a.clamp(a.top + delta)
}

func opposite(d search.Direction) search.Direction {
	if d == search.Forward {
		return search.Backward
	}
	return search.Forward
}

// beginSearch enters search mode, remembering where to return to on cancel.
func (a *App) beginSearch(dir search.Direction) {
	a.mode = modeSearch
	a.direction = dir
	a.query = ""
	a.matches = nil
	a.active = -1
	a.savedTop = a.top
}

// handleSearchKey handles a keypress while typing a query. Matches are
// recomputed on every keystroke and the viewport follows them live.
func (a *App) handleSearchKey(ev terminal.Event) {
	switch ev.Key {
	case terminal.KeyEscape:
		a.mode = modeNormal
		a.query = ""
		a.matches = nil
		a.active = -1
		a.top = a.clamp(a.savedTop) // restore the view the search started from

	case terminal.KeyEnter:
		a.mode = modeNormal
		if a.query != "" {
			a.lastQuery = a.query
		}

	case terminal.KeyBackspace:
		if a.query != "" {
			_, size := lastRune(a.query)
			a.query = a.query[:len(a.query)-size]
			a.updateSearch()
		}

	case terminal.KeyRune:
		a.query += string(ev.Rune)
		a.updateSearch()
	}
}

// updateSearch recomputes matches for the query typed so far and jumps to the
// first one at or after where the search began.
func (a *App) updateSearch() {
	a.matches = search.Find(a.rendered, a.query)
	if len(a.matches) == 0 {
		a.active = -1
		a.top = a.clamp(a.savedTop)
		return
	}
	a.active = search.First(a.matches, a.savedTop)
	a.reveal(a.matches[a.active].Line)
}

// repeatSearch moves to the next or previous match of the last accepted query.
func (a *App) repeatSearch(dir search.Direction) {
	if a.lastQuery == "" {
		a.message = "no previous search"
		return
	}
	if a.query != a.lastQuery || len(a.matches) == 0 {
		a.query = a.lastQuery
		a.matches = search.Find(a.rendered, a.query)
	}
	if len(a.matches) == 0 {
		a.message = fmt.Sprintf("not found: %s", a.lastQuery)
		return
	}

	index, wrapped, ok := search.Next(a.matches, a.currentMatchLine(), dir)
	if !ok {
		return
	}
	a.active = index
	a.reveal(a.matches[index].Line)
	if wrapped {
		a.message = "search wrapped"
	}
}

// currentMatchLine is the row navigation moves away from.
func (a *App) currentMatchLine() int {
	if a.active >= 0 && a.active < len(a.matches) {
		return a.matches[a.active].Line
	}
	return a.top
}

// reveal scrolls the viewport so a row is visible, centring it when it is off
// screen and leaving the view alone when it is already in view.
func (a *App) reveal(line int) {
	if line >= a.top && line < a.top+a.pageHeight() {
		return
	}
	a.top = a.clamp(line - a.pageHeight()/2)
}

// lastRune returns the final rune of a string and its byte width.
func lastRune(s string) (rune, int) {
	var last rune
	var index int
	for i, r := range s {
		last, index = r, i
	}
	return last, len(s) - index
}
