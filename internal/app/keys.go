package app

import (
	"fmt"
	"time"

	"github.com/schambon/mdv/internal/search"
	"github.com/schambon/mdv/internal/terminal"
)

// Control keys that arrive as bare runes in raw mode.
const (
	ctrlB = 0x02
	ctrlF = 0x06
	ctrlR = 0x12
)

// wheelRows is how far one wheel notch moves the viewport. Mouse tracking
// takes the wheel away from the terminal, so mdv has to scroll for it; three
// rows is what terminals themselves send.
const wheelRows = 3

// swipeGap is how quiet a horizontal gesture must go before the next notch
// counts as a new swipe. A trackpad reports one flick as a burst of notches —
// often dozens — so acting on every one would run the whole changed-file list
// past in a single gesture. A tilt wheel, which sends one notch at a time with
// human pauses between them, is unaffected.
const swipeGap = 400 * time.Millisecond

const keyHelp = "j/k move  space/b page  g/G ends  tab link  enter open  < > back/fwd  / ? search  n/N next  l numbers  t theme  v edit  r reload  q quit"

const diffKeyHelp = "j/k move  space/b page  [ ] hunk  x/X expand  z collapse  l numbers  t theme  / search  v edit  q quit"

// fileKeyHelp is added only when git mode found more than one changed file,
// since the keys do nothing otherwise.
const fileKeyHelp = "< > file  "

// help is the key summary for the current mode.
func (a *App) help() string {
	if !a.cfg.diffMode() {
		return keyHelp
	}
	if len(a.files) > 1 {
		return fileKeyHelp + diffKeyHelp
	}
	return diffKeyHelp
}

// handle dispatches one event and reports whether the viewer should quit.
func (a *App) handle(ev terminal.Event) (quit bool, err error) {
	// The wheel is dispatched ahead of the modes because it is not a binding:
	// it is the reader physically scrolling, and it means the same thing in
	// the viewer, in a diff and with a search prompt open. Enabling mouse
	// tracking stops the terminal from scrolling the alternate screen itself,
	// so moving the viewport here is what keeps the wheel working at all.
	switch ev.Key {
	case terminal.KeyWheelUp:
		a.scroll(-wheelRows)
		return false, nil
	case terminal.KeyWheelDown:
		a.scroll(wheelRows)
		return false, nil
	}

	if a.mode == modeSearch {
		a.handleSearchKey(ev)
		return false, nil
	}
	return a.handleNormalKey(ev)
}

// handleNormalKey handles a keypress in normal mode.
func (a *App) handleNormalKey(ev terminal.Event) (bool, error) {
	// As with the rune bindings, diff mode is consulted first and declines
	// every key when not comparing two files.
	if a.handleDiffKey(ev.Key) {
		return false, nil
	}

	switch ev.Key {
	case terminal.KeyDown:
		a.scroll(1)
	case terminal.KeyEnter:
		// Enter opens the link Tab selected, and otherwise keeps its original
		// meaning. Escape clears the selection to get plain scrolling back.
		if !a.openActiveLink() {
			a.scroll(1)
		}
	case terminal.KeyTab:
		a.cycleLink(1)
	case terminal.KeyShiftTab:
		a.cycleLink(-1)
	case terminal.KeyMouse:
		a.clickLink(ev)
	case terminal.KeyWheelLeft:
		a.swipe(-1)
	case terminal.KeyWheelRight:
		a.swipe(1)
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
		// Nothing to cancel in normal mode but a link selection.
		a.clearLink()
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
	case 't':
		a.toggleTheme()
	case 'r', ctrlR:
		if err := a.reload(); err != nil {
			a.message = err.Error()
		}
	case 'v':
		return false, a.edit()
	case '<':
		// Unreachable in diff mode: handleDiffRune claims both keys there for
		// the changed-file list before this switch is ever entered.
		a.goBack()
	case '>':
		a.goForward()
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

// swipe moves one step through whatever `<` and `>` move through: the changed
// file list in diff mode, the visited-file history in the viewer. Unlike the
// vertical wheel it is dispatched with the other normal-mode keys rather than
// ahead of them, because a stray gesture should not be able to leave the file
// out from under a half-typed search query.
func (a *App) swipe(delta int) {
	now := a.clock()
	fresh := delta != a.swipeDir || now.Sub(a.lastSwipe) > swipeGap
	a.lastSwipe, a.swipeDir = now, delta
	if !fresh {
		return
	}

	switch {
	case a.cfg.diffMode():
		a.selectRelative(delta)
	case delta < 0:
		a.goBack()
	default:
		a.goForward()
	}
}

// clock reads the swipe debounce's time source. An App built without one — as
// every test that does not play a gesture is — uses the real clock.
func (a *App) clock() time.Time {
	if a.now == nil {
		return time.Now()
	}
	return a.now()
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
