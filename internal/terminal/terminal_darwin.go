//go:build darwin

package terminal

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"sync"
	"syscall"
	"time"
	"unsafe"
)

// escapeTimeout distinguishes a bare Escape keypress from the start of an
// escape sequence: nothing follows within this window for a real Escape.
const escapeTimeout = 35 * time.Millisecond

// maxSequence caps how many bytes a CSI sequence may collect before it is
// abandoned, so malformed input cannot stall the reader.
const maxSequence = 5

type darwinTerminal struct {
	in  *os.File
	out *os.File

	reader *bufio.Reader

	mu      sync.Mutex
	entered bool
	saved   syscall.Termios
}

// New returns a terminal bound to stdin and stdout. Both must be character
// devices: mdv is interactive only, and will not render into a pipe.
func New() (Terminal, error) {
	in, out := os.Stdin, os.Stdout
	for _, f := range []*os.File{in, out} {
		info, err := f.Stat()
		if err != nil {
			return nil, fmt.Errorf("inspect %s: %w", f.Name(), err)
		}
		if info.Mode()&os.ModeCharDevice == 0 {
			return nil, fmt.Errorf("%s is not a terminal", f.Name())
		}
	}
	return &darwinTerminal{in: in, out: out, reader: bufio.NewReader(in)}, nil
}

// Enter saves the terminal modes, switches to raw mode, and shows the
// alternate screen. Calling it twice is harmless.
func (t *darwinTerminal) Enter() error {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.entered {
		return nil
	}

	saved, err := getTermios(t.in.Fd())
	if err != nil {
		return fmt.Errorf("read terminal modes: %w", err)
	}
	t.saved = saved

	raw := saved
	raw.Iflag &^= syscall.ICRNL | syscall.IXON
	raw.Oflag &^= syscall.OPOST
	raw.Lflag &^= syscall.ECHO | syscall.ICANON | syscall.ISIG | syscall.IEXTEN
	raw.Cc[syscall.VMIN] = 1
	raw.Cc[syscall.VTIME] = 0
	if err := setTermios(t.in.Fd(), raw); err != nil {
		return fmt.Errorf("enter raw mode: %w", err)
	}

	if _, err := t.out.WriteString(EnterAltScreen + HideCursor); err != nil {
		// Put the modes back rather than leaving a half-entered terminal.
		_ = setTermios(t.in.Fd(), t.saved)
		return err
	}

	t.entered = true
	return nil
}

// Leave restores the original screen and terminal modes. It is idempotent, so
// a deferred Leave is safe even when Enter failed.
func (t *darwinTerminal) Leave() error {
	t.mu.Lock()
	defer t.mu.Unlock()
	if !t.entered {
		return nil
	}
	t.entered = false

	_, writeErr := t.out.WriteString(ResetSGR + ShowCursor + LeaveAltScreen)
	modeErr := setTermios(t.in.Fd(), t.saved)
	return errors.Join(writeErr, modeErr)
}

func (t *darwinTerminal) Size() (Size, error) {
	var ws struct{ Row, Col, Xpixel, Ypixel uint16 }
	if err := ioctl(t.out.Fd(), syscall.TIOCGWINSZ, unsafe.Pointer(&ws)); err != nil {
		return Size{}, err
	}
	return Size{Width: int(ws.Col), Height: int(ws.Row)}, nil
}

func (t *darwinTerminal) Draw(frame string) error {
	_, err := t.out.WriteString(frame)
	return err
}

// Suspend restores the terminal, runs fn with the tty under its control, then
// re-enters. fn's own error is reported in preference to a re-entry failure.
func (t *darwinTerminal) Suspend(fn func() error) error {
	if err := t.Leave(); err != nil {
		return err
	}
	fnErr := fn()
	enterErr := t.Enter()
	if fnErr != nil {
		return fnErr
	}
	return enterErr
}

// ReadEvent blocks for one event. It performs exactly one logical read, so the
// caller can stop calling it and hand stdin to a child process.
func (t *darwinTerminal) ReadEvent() (Event, error) {
	r, _, err := t.reader.ReadRune()
	if err != nil {
		return Event{}, err
	}

	switch r {
	case '\r', '\n':
		return Event{Key: KeyEnter}, nil
	case 0x7F, 0x08:
		return Event{Key: KeyBackspace}, nil
	case 0x1B:
		return t.readEscape()
	}
	return Event{Key: KeyRune, Rune: r}, nil
}

// readEscape decides whether an Escape byte stands alone or opens a sequence.
func (t *darwinTerminal) readEscape() (Event, error) {
	if !t.pending() {
		return Event{Key: KeyEscape}, nil
	}

	r, _, err := t.reader.ReadRune()
	if err != nil {
		return Event{Key: KeyEscape}, nil
	}
	if r != '[' && r != 'O' {
		// Alt-modified keys and unknown introducers degrade to Escape.
		return Event{Key: KeyEscape}, nil
	}

	var seq []rune
	for len(seq) < maxSequence {
		next, _, err := t.reader.ReadRune()
		if err != nil {
			return Event{Key: KeyEscape}, nil
		}
		seq = append(seq, next)
		if next >= '@' && next <= '~' {
			return decodeSequence(seq), nil
		}
	}
	return Event{Key: KeyEscape}, nil
}

// pending reports whether more input is already available, either buffered or
// readable within the escape timeout.
func (t *darwinTerminal) pending() bool {
	if t.reader.Buffered() > 0 {
		return true
	}
	return readable(t.in.Fd(), escapeTimeout)
}

// decodeSequence maps the tail of a CSI or SS3 sequence to a key.
func decodeSequence(seq []rune) Event {
	switch string(seq) {
	case "A":
		return Event{Key: KeyUp}
	case "B":
		return Event{Key: KeyDown}
	case "C":
		return Event{Key: KeyRight}
	case "D":
		return Event{Key: KeyLeft}
	case "H", "1~", "7~":
		return Event{Key: KeyHome}
	case "F", "4~", "8~":
		return Event{Key: KeyEnd}
	case "5~":
		return Event{Key: KeyPageUp}
	case "6~":
		return Event{Key: KeyPageDown}
	}
	return Event{Key: KeyEscape}
}

// readable waits up to timeout for the file descriptor to have input.
func readable(fd uintptr, timeout time.Duration) bool {
	var set syscall.FdSet
	set.Bits[fd/64] |= 1 << (fd % 64)

	tv := syscall.Timeval{
		Sec:  int64(timeout / time.Second),
		Usec: int32((timeout % time.Second) / time.Microsecond),
	}
	if err := syscall.Select(int(fd)+1, &set, nil, nil, &tv); err != nil {
		return false
	}
	return set.Bits[fd/64]&(1<<(fd%64)) != 0
}

func getTermios(fd uintptr) (syscall.Termios, error) {
	var t syscall.Termios
	err := ioctl(fd, syscall.TIOCGETA, unsafe.Pointer(&t))
	return t, err
}

func setTermios(fd uintptr, t syscall.Termios) error {
	return ioctl(fd, syscall.TIOCSETA, unsafe.Pointer(&t))
}

func ioctl(fd uintptr, request uintptr, arg unsafe.Pointer) error {
	if _, _, errno := syscall.Syscall(syscall.SYS_IOCTL, fd, request, uintptr(arg)); errno != 0 {
		return errno
	}
	return nil
}
