// Package terminal owns raw mode, screen lifecycle, sizing, and input decoding.
// Only Darwin has a real backend; other platforms fail at construction.
package terminal

// Key identifies a decoded keypress. Printable input arrives as KeyRune with
// the rune in Event.Rune.
type Key int

const (
	KeyNone Key = iota
	KeyRune
	KeyEnter
	KeyEscape
	KeyBackspace
	KeyUp
	KeyDown
	KeyLeft
	KeyRight
	KeyPageUp
	KeyPageDown
	KeyHome
	KeyEnd
	KeyTab
	KeyShiftTab
	// KeyMouse is a press of the primary mouse button. Releases, the other
	// buttons and drags are discarded by the decoder, so a KeyMouse event
	// always means "the reader clicked here".
	KeyMouse
	// KeyWheelUp and KeyWheelDown are wheel notches. Tracking takes the wheel
	// away from the terminal, which would otherwise scroll the alternate
	// screen itself, so the application has to move the viewport for it.
	KeyWheelUp
	KeyWheelDown
	// KeyWheelLeft and KeyWheelRight are horizontal wheel notches, which is
	// what a trackpad reports for a two-finger sideways swipe. Nothing
	// scrolls horizontally, so the viewer reads them as the gesture they are
	// rather than as movement.
	KeyWheelLeft
	KeyWheelRight
)

// Event is one input event. Col and Row are the 1-based cell the pointer was
// over and are meaningful only for KeyMouse; every other key leaves them zero.
type Event struct {
	Key  Key
	Rune rune
	Col  int
	Row  int
}

// Size is a terminal's dimensions in character cells.
type Size struct {
	Width  int
	Height int
}

// Terminal is the screen and keyboard interface the application drives. It is
// an interface so the application can be tested against a fake.
type Terminal interface {
	// Enter switches to raw mode and the alternate screen. It is idempotent.
	Enter() error
	// Leave restores the original screen and terminal modes. It is idempotent
	// and safe to call without a matching Enter.
	Leave() error
	// Size reports the current dimensions.
	Size() (Size, error)
	// QueryBackground asks the terminal for its background colour via OSC 11
	// and reports whether that colour is dark. ok is false when the terminal
	// does not answer within a short timeout, so auto detection can fall back
	// to another signal. It must be called in raw mode — after Enter — and
	// before any ReadEvent, since it reads the reply off the same input.
	QueryBackground() (dark bool, ok bool)
	// ReadEvent blocks until one input event is available.
	ReadEvent() (Event, error)
	// Draw writes a complete frame in a single write.
	Draw(frame string) error
	// Suspend leaves the terminal, runs fn with the tty restored, and
	// re-enters. fn's error is preferred over a re-entry error.
	Suspend(fn func() error) error
}

// Control sequences. OPOST is disabled in raw mode, so callers must write
// explicit CRLF at the end of each row.
const (
	EnterAltScreen = "\x1b[?1049h"
	LeaveAltScreen = "\x1b[?1049l"
	HideCursor     = "\x1b[?25l"
	ShowCursor     = "\x1b[?25h"
	CursorHome     = "\x1b[H"
	ClearScreen    = "\x1b[2J"
	ClearToEOL     = "\x1b[K"
	ResetSGR       = "\x1b[0m"

	// Mouse tracking. Mode 1000 reports button presses and releases only —
	// not motion, which would flood the input pump with events the viewer has
	// no use for. Mode 1006 asks for SGR-encoded reports, whose coordinates
	// are decimal and so are not capped at column 223 the way the original
	// encoding is. Turning tracking on costs the terminal's own drag-select,
	// which is why Leave must always turn it back off.
	EnableMouse  = "\x1b[?1000h\x1b[?1006h"
	DisableMouse = "\x1b[?1006l\x1b[?1000l"
)

// Fallback dimensions, used when the terminal reports something unusable.
const (
	FallbackWidth  = 80
	FallbackHeight = 24
	MinWidth       = 10
	MinHeight      = 2
)

// Normalize replaces unusable dimensions with the fallbacks.
func Normalize(s Size) Size {
	if s.Width < MinWidth {
		s.Width = FallbackWidth
	}
	if s.Height < MinHeight {
		s.Height = FallbackHeight
	}
	return s
}
