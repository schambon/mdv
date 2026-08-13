package app

import (
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/schambon/mdv/internal/style"
	"github.com/schambon/mdv/internal/terminal"
)

// fakeTerminal stands in for a real tty. It records lifecycle calls and the
// frames drawn, and replays a canned sequence of events.
type fakeTerminal struct {
	mu sync.Mutex

	size   terminal.Size
	events []terminal.Event
	next   int

	frames  []string
	entered int
	left    int
	// inUse is true while a suspended callback runs; a read during that window
	// would mean the pump was stealing input from the child process.
	inUse       bool
	readWhileIn bool

	sizeErr error
	drawErr error

	// idle unblocks a read that has run out of canned events. A real terminal
	// blocks waiting for a keypress, so the fake must too: returning an error
	// would end the event loop and hide whatever the test meant to exercise.
	idle chan struct{}
}

func newFake(events ...terminal.Event) *fakeTerminal {
	return &fakeTerminal{
		size:   terminal.Size{Width: 40, Height: 6},
		events: events,
		idle:   make(chan struct{}),
	}
}

// close releases any read blocked waiting for input.
func (f *fakeTerminal) close() {
	f.mu.Lock()
	defer f.mu.Unlock()
	select {
	case <-f.idle:
	default:
		close(f.idle)
	}
}

func (f *fakeTerminal) Enter() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.entered++
	return nil
}

func (f *fakeTerminal) Leave() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.left++
	return nil
}

func (f *fakeTerminal) Size() (terminal.Size, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.size, f.sizeErr
}

func (f *fakeTerminal) Draw(frame string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.drawErr != nil {
		return f.drawErr
	}
	f.frames = append(f.frames, frame)
	return nil
}

func (f *fakeTerminal) ReadEvent() (terminal.Event, error) {
	f.mu.Lock()
	if f.inUse {
		f.readWhileIn = true
	}
	if f.next >= len(f.events) {
		idle := f.idle
		f.mu.Unlock()
		<-idle // block like a terminal awaiting a keypress
		return terminal.Event{}, errors.New("terminal closed")
	}
	ev := f.events[f.next]
	f.next++
	f.mu.Unlock()
	return ev, nil
}

func (f *fakeTerminal) Suspend(fn func() error) error {
	if err := f.Leave(); err != nil {
		return err
	}
	f.mu.Lock()
	f.inUse = true
	f.mu.Unlock()

	fnErr := fn()

	f.mu.Lock()
	f.inUse = false
	f.mu.Unlock()

	enterErr := f.Enter()
	if fnErr != nil {
		return fnErr
	}
	return enterErr
}

func (f *fakeTerminal) lastFrame() string {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.frames) == 0 {
		return ""
	}
	return f.frames[len(f.frames)-1]
}

func (f *fakeTerminal) frameCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.frames)
}

// key builds a printable keypress.
func key(r rune) terminal.Event { return terminal.Event{Key: terminal.KeyRune, Rune: r} }

// press builds a non-printable keypress.
func press(k terminal.Key) terminal.Event { return terminal.Event{Key: k} }

// quit is the event that ends the loop cleanly.
var quit = key('q')

// writeSource creates a Markdown file for a test to view.
func writeSource(t *testing.T, contents string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "doc.md")
	if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

// newApp builds an App without entering the event loop, for testing state
// transitions directly.
func newApp(t *testing.T, contents string, term *fakeTerminal) *App {
	t.Helper()

	a := &App{
		cfg: Config{
			Path:  writeSource(t, contents),
			Theme: style.ThemeDark,
			Color: false,
		},
		term:   term,
		styler: style.New(style.ThemeDark, false),
		env:    func(string) string { return "" },
		active: -1,
	}
	a.runEdit = func([]string) error { return nil }

	if err := a.load(); err != nil {
		t.Fatalf("load: %v", err)
	}
	a.size = terminal.Normalize(a.currentSize())
	a.render()
	return a
}

// numberedLines builds a document with n distinct paragraphs.
func numberedLines(n int) string {
	var sb []byte
	for i := 1; i <= n; i++ {
		sb = append(sb, []byte("line"+itoa(i)+"\n\n")...)
	}
	return string(sb)
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var digits []byte
	for n > 0 {
		digits = append([]byte{byte('0' + n%10)}, digits...)
		n /= 10
	}
	return string(digits)
}
