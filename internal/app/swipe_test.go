package app

import (
	"testing"
	"time"

	"github.com/schambon/mdv/internal/terminal"
)

// fakeClock drives the swipe debounce so a gesture can be played out without
// sleeping: advance is the pause between notches.
type fakeClock struct{ t time.Time }

func (c *fakeClock) now() time.Time          { return c.t }
func (c *fakeClock) advance(d time.Duration) { c.t = c.t.Add(d) }

// swipeApp is a viewer with the debounce on a clock the test controls.
func swipeApp(t *testing.T, text string) (*App, *fakeClock) {
	t.Helper()
	a := newLinkApp(t, text, newFake())
	clock := &fakeClock{t: time.Unix(0, 0)}
	a.now = clock.now
	return a, clock
}

// A sideways swipe is back and forward, the same as < and >.
func TestSwipeGoesBackAndForward(t *testing.T) {
	a, _ := swipeApp(t, "[one](other.md)\n")

	a.handle(press(terminal.KeyTab))
	a.handle(press(terminal.KeyEnter))
	if a.src.Name != "other.md" {
		t.Fatalf("src.Name = %q, want other.md", a.src.Name)
	}

	a.handle(press(terminal.KeyWheelLeft))
	if a.src.Name != "doc.md" {
		t.Fatalf("src.Name after a left swipe = %q, want doc.md", a.src.Name)
	}

	// A swipe the other way is a new gesture however recently the last one
	// ended, so it takes effect without waiting out the gap.
	a.handle(press(terminal.KeyWheelRight))
	if a.src.Name != "other.md" {
		t.Errorf("src.Name after a right swipe = %q, want other.md", a.src.Name)
	}
}

// One flick of a trackpad is a burst of notches. Acting on each of them would
// run the whole history past in a single gesture.
func TestSwipeBurstCountsOnce(t *testing.T) {
	a, clock := swipeApp(t, "[one](other.md)\n[two](deeper/nested.md)\n")

	// Visit other.md, come back, then visit nested.md: two steps of history.
	a.handle(press(terminal.KeyTab))
	a.handle(press(terminal.KeyEnter))
	a.handle(key('<'))
	a.handle(press(terminal.KeyTab))
	a.handle(press(terminal.KeyTab))
	a.handle(press(terminal.KeyEnter))
	if a.src.Name != "nested.md" {
		t.Fatalf("src.Name = %q, want nested.md", a.src.Name)
	}

	for range 20 {
		a.handle(press(terminal.KeyWheelLeft))
		clock.advance(10 * time.Millisecond)
	}
	if a.src.Name != "doc.md" {
		t.Fatalf("src.Name after one flick = %q, want one step back to doc.md", a.src.Name)
	}

	// Once the gesture has gone quiet, the next notch is a new swipe.
	clock.advance(swipeGap + time.Millisecond)
	a.handle(press(terminal.KeyWheelRight))
	if a.src.Name != "nested.md" {
		t.Errorf("src.Name after a second gesture = %q, want nested.md", a.src.Name)
	}
}

// Swiping is a normal-mode navigation, not a scroll: it must not pull the file
// out from under a half-typed search query.
func TestSwipeIgnoredWhileSearching(t *testing.T) {
	a, _ := swipeApp(t, "[one](other.md)\n")

	a.handle(press(terminal.KeyTab))
	a.handle(press(terminal.KeyEnter))
	a.handle(key('/'))
	a.handle(press(terminal.KeyWheelLeft))

	if a.mode != modeSearch {
		t.Error("a swipe left search mode")
	}
	if a.src.Name != "other.md" {
		t.Errorf("src.Name = %q, want the search to keep its file", a.src.Name)
	}
}

// In git mode < and > walk the changed-file list, so a swipe does too.
func TestSwipeMovesThroughTheFileList(t *testing.T) {
	a := mustGitApp(t, threeFiles(t), GitRequest{}, wideFake())
	clock := &fakeClock{t: time.Unix(0, 0)}
	a.now = clock.now

	if _, err := a.handleNormalKey(press(terminal.KeyWheelRight)); err != nil {
		t.Fatal(err)
	}
	if a.current != 1 {
		t.Fatalf("current = %d after a right swipe, want the second file", a.current)
	}

	// The rest of that flick's notches belong to the same gesture, so they
	// move nothing: one flick is one file, not the whole list.
	for range 20 {
		clock.advance(10 * time.Millisecond)
		if _, err := a.handleNormalKey(press(terminal.KeyWheelRight)); err != nil {
			t.Fatal(err)
		}
	}
	if a.current != 1 {
		t.Errorf("current = %d after one flick, want a single step to the second file", a.current)
	}

	clock.advance(swipeGap + time.Millisecond)
	if _, err := a.handleNormalKey(press(terminal.KeyWheelLeft)); err != nil {
		t.Fatal(err)
	}
	if a.current != 0 {
		t.Errorf("current = %d after a left swipe, want back at the first file", a.current)
	}
}
