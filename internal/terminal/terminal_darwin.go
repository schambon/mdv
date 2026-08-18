//go:build darwin

package terminal

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
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

// oscQueryBackground asks the terminal to report its background colour. A
// terminal that understands it replies with the same 11; prefix; one that does
// not stays silent, which the read below handles by timing out.
const oscQueryBackground = "\x1b]11;?\x07"

// queryTimeout bounds the wait for a reply, so a terminal that does not answer
// OSC 11 costs a short pause at startup rather than hanging the viewer.
const queryTimeout = 100 * time.Millisecond

// QueryBackground sends OSC 11 and parses the reply into a dark/light verdict.
// It must run in raw mode and before the input pump starts, so its reply cannot
// be mistaken for — or stolen by — a keypress.
func (t *darwinTerminal) QueryBackground() (bool, bool) {
	t.mu.Lock()
	entered := t.entered
	t.mu.Unlock()
	if !entered {
		return false, false
	}

	if _, err := t.out.WriteString(oscQueryBackground); err != nil {
		return false, false
	}

	resp, ok := t.readOSCReply(queryTimeout)
	if !ok {
		return false, false
	}
	return parseBackgroundReply(resp)
}

// readOSCReply reads bytes until an OSC terminator (BEL, or ESC \) or the
// deadline, whichever comes first. It polls readable before each byte so a
// silent terminal never blocks the read.
func (t *darwinTerminal) readOSCReply(timeout time.Duration) (string, bool) {
	deadline := time.Now().Add(timeout)
	var buf []byte
	for len(buf) < 64 {
		// The first ReadByte drains the whole reply from the fd into bufio's
		// buffer, so consult the buffer before select: checking the fd alone
		// would look empty and abandon the rest of a reply already in hand.
		if t.reader.Buffered() == 0 {
			remaining := time.Until(deadline)
			if remaining <= 0 || !readable(t.in.Fd(), remaining) {
				return "", false
			}
		}
		b, err := t.reader.ReadByte()
		if err != nil {
			return "", false
		}
		switch {
		case b == 0x07: // BEL terminates an OSC string
			return string(buf), true
		case b == '\\' && len(buf) > 0 && buf[len(buf)-1] == 0x1b: // ST is ESC \
			return string(buf[:len(buf)-1]), true
		default:
			buf = append(buf, b)
		}
	}
	return string(buf), true
}

// parseBackgroundReply extracts the RGB triplet from an OSC 11 reply and reports
// whether it is dark. Both the xterm "rgb:RRRR/GGGG/BBBB" form and the shorter
// "#RRGGBB" form are accepted; channels may be one to four hex digits.
func parseBackgroundReply(reply string) (dark bool, ok bool) {
	var r, g, b float64
	switch {
	case strings.Contains(reply, "rgb:"):
		parts := strings.SplitN(reply[strings.Index(reply, "rgb:")+4:], "/", 3)
		if len(parts) != 3 {
			return false, false
		}
		var ok1, ok2, ok3 bool
		r, ok1 = parseHexChannel(parts[0])
		g, ok2 = parseHexChannel(parts[1])
		b, ok3 = parseHexChannel(parts[2])
		if !ok1 || !ok2 || !ok3 {
			return false, false
		}
	case strings.Contains(reply, "#"):
		hex := leadingHex(reply[strings.IndexByte(reply, '#')+1:])
		if len(hex)%3 != 0 || len(hex) == 0 {
			return false, false
		}
		w := len(hex) / 3
		var ok1, ok2, ok3 bool
		r, ok1 = parseHexChannel(hex[:w])
		g, ok2 = parseHexChannel(hex[w : 2*w])
		b, ok3 = parseHexChannel(hex[2*w:])
		if !ok1 || !ok2 || !ok3 {
			return false, false
		}
	default:
		return false, false
	}

	// Rec. 601 luma on channels normalised to 0..1; below the midpoint is dark.
	return 0.299*r+0.587*g+0.114*b < 0.5, true
}

// parseHexChannel reads the leading hex digits of a channel and scales them to
// 0..1 against their own width, so "ff", "ffff" and "f" all map to 1.
func parseHexChannel(s string) (float64, bool) {
	hex := leadingHex(s)
	if hex == "" {
		return 0, false
	}
	v, err := strconv.ParseUint(hex, 16, 64)
	if err != nil {
		return 0, false
	}
	max := float64(uint64(1)<<(uint(len(hex))*4) - 1)
	return float64(v) / max, true
}

// leadingHex returns the run of hex digits at the start of s, dropping any
// trailing terminator bytes the reply may still carry.
func leadingHex(s string) string {
	for i := 0; i < len(s); i++ {
		c := s[i]
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f' || c >= 'A' && c <= 'F') {
			return s[:i]
		}
	}
	return s
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
