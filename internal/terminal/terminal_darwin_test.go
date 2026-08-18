//go:build darwin

package terminal

import (
	"bufio"
	"os"
	"strings"
	"testing"
	"time"
)

func TestDecodeSequence(t *testing.T) {
	tests := []struct {
		seq  string
		want Key
	}{
		{"A", KeyUp},
		{"B", KeyDown},
		{"C", KeyRight},
		{"D", KeyLeft},
		{"H", KeyHome},
		{"F", KeyEnd},
		{"1~", KeyHome},
		{"7~", KeyHome},
		{"4~", KeyEnd},
		{"8~", KeyEnd},
		{"5~", KeyPageUp},
		{"6~", KeyPageDown},
		{"Z", KeyEscape},   // unknown sequences degrade to Escape
		{"99~", KeyEscape}, // unknown numeric parameter
	}
	for _, tt := range tests {
		t.Run(tt.seq, func(t *testing.T) {
			if got := decodeSequence([]rune(tt.seq)).Key; got != tt.want {
				t.Errorf("decodeSequence(%q) = %v, want %v", tt.seq, got, tt.want)
			}
		})
	}
}

// readEvents decodes a canned input stream, standing in for a real tty.
func readEvents(t *testing.T, input string, n int) []Event {
	t.Helper()

	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()

	if _, err := w.WriteString(input); err != nil {
		t.Fatal(err)
	}
	w.Close()

	term := &darwinTerminal{in: r, out: os.Stdout, reader: bufio.NewReader(r)}
	events := make([]Event, 0, n)
	for range n {
		ev, err := term.ReadEvent()
		if err != nil {
			t.Fatalf("ReadEvent: %v", err)
		}
		events = append(events, ev)
	}
	return events
}

func TestReadEventPrintableAndControls(t *testing.T) {
	got := readEvents(t, "j\rq\x7f", 4)
	want := []Key{KeyRune, KeyEnter, KeyRune, KeyBackspace}
	for i, k := range want {
		if got[i].Key != k {
			t.Errorf("event %d = %v, want %v", i, got[i].Key, k)
		}
	}
	if got[0].Rune != 'j' || got[2].Rune != 'q' {
		t.Errorf("runes = %q, %q, want j, q", got[0].Rune, got[2].Rune)
	}
}

func TestReadEventBothEnterAndBackspaceForms(t *testing.T) {
	got := readEvents(t, "\r\n\x7f\x08", 4)
	want := []Key{KeyEnter, KeyEnter, KeyBackspace, KeyBackspace}
	for i, k := range want {
		if got[i].Key != k {
			t.Errorf("event %d = %v, want %v", i, got[i].Key, k)
		}
	}
}

func TestReadEventEscapeSequences(t *testing.T) {
	// All buffered at once, so each Escape is recognized as a sequence.
	input := "\x1b[A\x1b[B\x1b[5~\x1b[6~\x1b[H\x1bOF"
	got := readEvents(t, input, 6)
	want := []Key{KeyUp, KeyDown, KeyPageUp, KeyPageDown, KeyHome, KeyEnd}
	for i, k := range want {
		if got[i].Key != k {
			t.Errorf("event %d = %v, want %v", i, got[i].Key, k)
		}
	}
}

// A lone Escape at end of input has nothing following it, so it stays Escape.
func TestReadEventBareEscape(t *testing.T) {
	got := readEvents(t, "\x1b", 1)
	if got[0].Key != KeyEscape {
		t.Errorf("event = %v, want KeyEscape", got[0].Key)
	}
}

func TestReadEventOverlongSequenceDegradesToEscape(t *testing.T) {
	got := readEvents(t, "\x1b[123456789", 1)
	if got[0].Key != KeyEscape {
		t.Errorf("event = %v, want KeyEscape", got[0].Key)
	}
}

func TestReadEventUTF8(t *testing.T) {
	got := readEvents(t, "é漢", 2)
	if got[0].Rune != 'é' || got[1].Rune != '漢' {
		t.Errorf("runes = %q, %q", got[0].Rune, got[1].Rune)
	}
}

func TestReadable(t *testing.T) {
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	defer w.Close()

	if readable(r.Fd(), 10*time.Millisecond) {
		t.Error("empty pipe reported readable")
	}
	if _, err := w.WriteString("x"); err != nil {
		t.Fatal(err)
	}
	if !readable(r.Fd(), 100*time.Millisecond) {
		t.Error("pipe with data reported not readable")
	}
}

// Leave without Enter must be safe, since Leave is deferred immediately.
func TestLeaveWithoutEnterIsSafe(t *testing.T) {
	term := &darwinTerminal{in: os.Stdin, out: os.Stdout, reader: bufio.NewReader(os.Stdin)}
	if err := term.Leave(); err != nil {
		t.Errorf("Leave: %v", err)
	}
}

func TestDrawWritesFrameToOutput(t *testing.T) {
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()

	term := &darwinTerminal{in: os.Stdin, out: w, reader: bufio.NewReader(os.Stdin)}
	if err := term.Draw("frame\r\n"); err != nil {
		t.Fatalf("Draw: %v", err)
	}
	w.Close()

	buf := make([]byte, 64)
	n, _ := r.Read(buf)
	if got := string(buf[:n]); got != "frame\r\n" {
		t.Errorf("wrote %q, want %q", got, "frame\r\n")
	}
}

// New refuses to run against anything that is not a terminal.
func TestNewRejectsNonTerminal(t *testing.T) {
	file, err := os.CreateTemp(t.TempDir(), "stdin")
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()

	saved := os.Stdin
	os.Stdin = file
	defer func() { os.Stdin = saved }()

	if _, err := New(); err == nil {
		t.Error("New accepted a regular file as stdin")
	} else if !strings.Contains(err.Error(), "not a terminal") {
		t.Errorf("error = %v", err)
	}
}

func TestParseBackgroundReply(t *testing.T) {
	tests := []struct {
		name     string
		reply    string
		wantDark bool
		wantOK   bool
	}{
		{"black rgb 16-bit", "\x1b]11;rgb:0000/0000/0000", true, true},
		{"white rgb 16-bit", "\x1b]11;rgb:ffff/ffff/ffff", false, true},
		{"black rgb 8-bit", "]11;rgb:00/00/00", true, true},
		{"white rgb 8-bit", "]11;rgb:ff/ff/ff", false, true},
		{"dark grey", "rgb:2020/2020/2020", true, true},
		{"light grey", "rgb:d0d0/d0d0/d0d0", false, true},
		{"hash form six digits white", "\x1b]11;#ffffff", false, true},
		{"hash form six digits black", "\x1b]11;#000000", true, true},
		{"trailing terminator bytes tolerated", "rgb:ffff/ffff/ffff\x1b", false, true},
		{"no colour in reply", "\x1b]11;", false, false},
		{"garbage", "not a reply", false, false},
		{"too few channels", "rgb:ffff/ffff", false, false},
		{"non-hex channel", "rgb:zz/00/00", false, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dark, ok := parseBackgroundReply(tt.reply)
			if ok != tt.wantOK {
				t.Fatalf("ok = %v, want %v", ok, tt.wantOK)
			}
			if ok && dark != tt.wantDark {
				t.Errorf("dark = %v, want %v", dark, tt.wantDark)
			}
		})
	}
}

func TestLeadingHex(t *testing.T) {
	tests := []struct{ in, want string }{
		{"ffff", "ffff"},
		{"ab/cd", "ab"},
		{"00\x1b", "00"},
		{"zzz", ""},
		{"", ""},
	}
	for _, tt := range tests {
		if got := leadingHex(tt.in); got != tt.want {
			t.Errorf("leadingHex(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

// TestReadOSCReplyDrainsBufferedReply reproduces the startup bug where bufio
// pulled the whole OSC 11 reply off the fd in one read, after which a raw
// select on the fd looked empty and the reply was abandoned half-parsed, its
// tail leaking into the input stream. The read must consume the whole reply up
// to the terminator and leave everything after it untouched.
func TestReadOSCReplyDrainsBufferedReply(t *testing.T) {
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	// The write end stays open for the whole test: a closed pipe reports EOF as
	// readable, which would hide the very bug this guards — a real tty does not
	// hit EOF, so once bufio has drained the fd a select on it sees nothing.
	defer w.Close()

	// A light background, terminated by ST, with a stray keystroke behind it.
	const reply = "\x1b]11;rgb:e9e9/e9e9/ecec\x1b\\j"
	if _, err := w.WriteString(reply); err != nil {
		t.Fatal(err)
	}

	term := &darwinTerminal{in: r, reader: bufio.NewReader(r)}

	got, ok := term.readOSCReply(queryTimeout)
	if !ok {
		t.Fatal("readOSCReply reported no reply")
	}
	if !strings.Contains(got, "rgb:e9e9/e9e9/ecec") {
		t.Errorf("reply = %q, want the whole rgb triplet", got)
	}
	if dark, ok := parseBackgroundReply(got); !ok || dark {
		t.Errorf("parse = (dark=%v ok=%v), want a light verdict", dark, ok)
	}

	// The byte after the terminator must not have been swallowed: it is a real
	// keystroke the pump will read next.
	b, err := term.reader.ReadByte()
	if err != nil || b != 'j' {
		t.Errorf("leftover byte = %q (err %v), want 'j': the read overran the terminator", b, err)
	}
}
