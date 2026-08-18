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
)

// Event is one input event.
type Event struct {
	Key  Key
	Rune rune
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
