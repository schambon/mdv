package app

import (
	"errors"
	"os"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/schambon/mdv/internal/search"
	"github.com/schambon/mdv/internal/style"
	"github.com/schambon/mdv/internal/terminal"
)

// typeQuery feeds a query into search mode one keystroke at a time.
func typeQuery(a *App, query string) {
	for _, r := range query {
		a.handle(key(r))
	}
}

func TestSearchPromptShowsDirection(t *testing.T) {
	a := newApp(t, "alpha\n", newFake())

	a.handle(key('/'))
	if got := a.status(); got != "/" {
		t.Errorf("forward prompt = %q, want /", got)
	}
	a.handle(press(terminal.KeyEscape))

	a.handle(key('?'))
	if got := a.status(); got != "?" {
		t.Errorf("backward prompt = %q, want ?", got)
	}
}

func TestSearchTypingUpdatesPromptAndMatches(t *testing.T) {
	a := newApp(t, "alpha\n\nbeta\n\nalpha again\n", newFake())

	a.handle(key('/'))
	typeQuery(a, "alpha")

	if got := a.status(); got != "/alpha" {
		t.Errorf("prompt = %q, want /alpha", got)
	}
	if len(a.matches) != 2 {
		t.Errorf("got %d matches, want 2", len(a.matches))
	}
}

// Typing moves the viewport immediately, before Enter is pressed.
func TestSearchMovesViewportWhileTyping(t *testing.T) {
	a := newApp(t, numberedLines(40)+"\nneedle here\n", newFake())

	a.handle(key('/'))
	typeQuery(a, "needle")

	if a.top == 0 {
		t.Error("viewport did not follow the match while typing")
	}
	if len(a.matches) == 0 {
		t.Fatal("no matches found")
	}
	line := a.matches[a.active].Line
	if line < a.top || line >= a.top+a.pageHeight() {
		t.Errorf("match on row %d is not visible in [%d,%d)", line, a.top, a.top+a.pageHeight())
	}
}

func TestSearchBackspace(t *testing.T) {
	a := newApp(t, "alpha\n", newFake())

	a.handle(key('/'))
	typeQuery(a, "alx")
	a.handle(press(terminal.KeyBackspace))

	if a.query != "al" {
		t.Errorf("query = %q, want al", a.query)
	}
	if len(a.matches) != 1 {
		t.Errorf("got %d matches after backspace, want 1", len(a.matches))
	}
}

func TestSearchBackspaceOnEmptyQueryIsSafe(t *testing.T) {
	a := newApp(t, "alpha\n", newFake())
	a.handle(key('/'))
	a.handle(press(terminal.KeyBackspace))

	if a.query != "" {
		t.Errorf("query = %q, want empty", a.query)
	}
}

func TestSearchBackspaceHandlesMultibyte(t *testing.T) {
	a := newApp(t, "漢字\n", newFake())
	a.handle(key('/'))
	typeQuery(a, "漢字")
	a.handle(press(terminal.KeyBackspace))

	if a.query != "漢" {
		t.Errorf("query = %q, want 漢", a.query)
	}
}

func TestSearchEnterAcceptsQuery(t *testing.T) {
	a := newApp(t, "alpha\n", newFake())

	a.handle(key('/'))
	typeQuery(a, "alpha")
	a.handle(press(terminal.KeyEnter))

	if a.mode != modeNormal {
		t.Error("Enter did not leave search mode")
	}
	if a.lastQuery != "alpha" {
		t.Errorf("lastQuery = %q, want alpha", a.lastQuery)
	}
}

func TestSearchEnterOnEmptyQueryKeepsPrevious(t *testing.T) {
	a := newApp(t, "alpha\n", newFake())
	a.lastQuery = "previous"

	a.handle(key('/'))
	a.handle(press(terminal.KeyEnter))

	if a.lastQuery != "previous" {
		t.Errorf("lastQuery = %q, want the earlier query retained", a.lastQuery)
	}
}

// Escape restores the viewport saved when the search began.
func TestSearchEscapeRestoresViewport(t *testing.T) {
	a := newApp(t, numberedLines(40)+"\nneedle\n", newFake())
	a.top = 3
	before := a.top

	a.handle(key('/'))
	typeQuery(a, "needle")
	if a.top == before {
		t.Fatal("search did not move the viewport, so the test proves nothing")
	}

	a.handle(press(terminal.KeyEscape))
	if a.top != before {
		t.Errorf("top = %d, want the saved %d", a.top, before)
	}
	if a.mode != modeNormal {
		t.Error("Escape did not leave search mode")
	}
	if a.matches != nil {
		t.Error("Escape left matches highlighted")
	}
}

func TestSearchNoMatchesKeepsViewport(t *testing.T) {
	a := newApp(t, numberedLines(40), newFake())
	a.top = 4

	a.handle(key('/'))
	typeQuery(a, "zzzznotfound")

	if a.top != 4 {
		t.Errorf("top = %d, want it unchanged at 4", a.top)
	}
	if len(a.matches) != 0 {
		t.Errorf("got %d matches, want none", len(a.matches))
	}
}

func TestRepeatSearchForwardAndBackward(t *testing.T) {
	src := "needle\n\n" + numberedLines(20) + "\nneedle\n"
	a := newApp(t, src, newFake())

	a.handle(key('/'))
	typeQuery(a, "needle")
	a.handle(press(terminal.KeyEnter))
	first := a.matches[a.active].Line

	a.handle(key('n'))
	second := a.matches[a.active].Line
	if second <= first {
		t.Errorf("n moved from row %d to %d, want a later row", first, second)
	}

	a.handle(key('N'))
	if got := a.matches[a.active].Line; got != first {
		t.Errorf("N moved to row %d, want back to %d", got, first)
	}
}

func TestRepeatSearchWrapsAndReports(t *testing.T) {
	src := "needle\n\n" + numberedLines(20) + "\nneedle\n"
	a := newApp(t, src, newFake())

	a.handle(key('/'))
	typeQuery(a, "needle")
	a.handle(press(terminal.KeyEnter))

	a.handle(key('n')) // to the last match
	a.handle(key('n')) // wraps
	if !strings.Contains(a.status(), "wrapped") {
		t.Errorf("status = %q, want a wrap notice", a.status())
	}
}

func TestRepeatSearchWithoutPreviousQuery(t *testing.T) {
	a := newApp(t, "alpha\n", newFake())
	a.handle(key('n'))
	if !strings.Contains(a.status(), "no previous search") {
		t.Errorf("status = %q, want a no-previous-search notice", a.status())
	}
}

func TestSearchIsSmartCase(t *testing.T) {
	a := newApp(t, "Alpha\n\nalpha\n", newFake())

	a.handle(key('/'))
	typeQuery(a, "alpha")
	if len(a.matches) != 2 {
		t.Errorf("lowercase query matched %d rows, want 2", len(a.matches))
	}

	a.handle(press(terminal.KeyEscape))
	a.handle(key('/'))
	typeQuery(a, "Alpha")
	if len(a.matches) != 1 {
		t.Errorf("capitalised query matched %d rows, want 1", len(a.matches))
	}
}

func TestEditRunsEditorAtCurrentLine(t *testing.T) {
	term := newFake()
	a := newApp(t, numberedLines(20), term)
	a.env = func(key string) string {
		if key == "EDITOR" {
			return "vim"
		}
		return ""
	}
	a.top = 4
	wantLine := a.sourceLine()

	var got []string
	a.runEdit = func(argv []string) error {
		got = argv
		return nil
	}

	if err := a.edit(); err != nil {
		t.Fatalf("edit: %v", err)
	}
	if len(got) != 3 || got[0] != "vim" {
		t.Fatalf("argv = %q", got)
	}
	if got[1] != "+"+itoa(wantLine) {
		t.Errorf("line argument = %q, want +%d", got[1], wantLine)
	}
	if !strings.HasSuffix(got[2], "doc.md") {
		t.Errorf("file argument = %q", got[2])
	}
}

// The editor must run with the terminal suspended, and no read may happen
// while it owns stdin.
func TestEditSuspendsTerminal(t *testing.T) {
	term := newFake()
	a := newApp(t, "text\n", term)

	var suspended bool
	a.runEdit = func([]string) error {
		term.mu.Lock()
		suspended = term.inUse
		term.mu.Unlock()
		return nil
	}

	a.edit()
	if !suspended {
		t.Error("the editor ran without the terminal suspended")
	}
	if term.readWhileIn {
		t.Error("input was read while the editor owned stdin")
	}
	if term.left != 1 || term.entered != 1 {
		t.Errorf("Leave/Enter called %d/%d times, want 1/1", term.left, term.entered)
	}
}

// The file may have been changed even when the editor exits non-zero.
func TestEditReloadsEvenWhenEditorFails(t *testing.T) {
	term := newFake()
	a := newApp(t, "before\n", term)
	a.runEdit = func([]string) error {
		return os.WriteFile(a.cfg.Path, []byte("after\n"), 0o644)
	}

	a.edit()
	if !strings.Contains(string(a.src.Bytes), "after") {
		t.Error("the file was not reloaded after editing")
	}

	a.runEdit = func([]string) error {
		os.WriteFile(a.cfg.Path, []byte("later\n"), 0o644)
		return errors.New("editor exited 1")
	}
	a.edit()
	if !strings.Contains(string(a.src.Bytes), "later") {
		t.Error("a failing editor prevented the reload")
	}
	if !strings.Contains(a.status(), "editor exited 1") {
		t.Errorf("status = %q, want the editor error", a.status())
	}
}

// A reload failure is the more urgent problem, so it replaces an editor error.
func TestReloadErrorSupersedesEditorError(t *testing.T) {
	term := newFake()
	a := newApp(t, "text\n", term)
	a.runEdit = func([]string) error {
		os.Remove(a.cfg.Path)
		return errors.New("editor exited 1")
	}

	a.edit()
	if strings.Contains(a.status(), "editor exited 1") {
		t.Errorf("status = %q, want the reload error instead", a.status())
	}
	if a.status() == "" {
		t.Error("no message was shown for the failed reload")
	}
}

func TestEditRejectsUnsafeEditorCommand(t *testing.T) {
	term := newFake()
	a := newApp(t, "text\n", term)
	a.env = func(string) string { return "vim; rm -rf /" }

	called := false
	a.runEdit = func([]string) error {
		called = true
		return nil
	}

	a.edit()
	if called {
		t.Error("an editor command with a shell operator was executed")
	}
	if !strings.Contains(a.status(), "unsupported shell character") {
		t.Errorf("status = %q, want the rejection message", a.status())
	}
}

func TestReloadKeepsPosition(t *testing.T) {
	term := newFake()
	a := newApp(t, numberedLines(40), term)
	a.top = 10
	line := a.sourceLine()

	if err := a.reload(); err != nil {
		t.Fatalf("reload: %v", err)
	}
	if got := a.sourceLine(); got != line {
		t.Errorf("source line after reload = %d, want %d", got, line)
	}
}

func TestReloadErrorBecomesMessage(t *testing.T) {
	term := newFake()
	a := newApp(t, "text\n", term)
	os.Remove(a.cfg.Path)

	a.handle(key('r'))
	if a.status() == "" {
		t.Error("a failed reload produced no message")
	}
}

func TestReloadPicksUpExternalChanges(t *testing.T) {
	term := newFake()
	a := newApp(t, "before\n", term)

	if err := os.WriteFile(a.cfg.Path, []byte("after\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	a.handle(key('r'))

	if !strings.Contains(string(a.src.Bytes), "after") {
		t.Error("reload did not pick up the change")
	}
}

// Resizing must reflow the text, not merely redraw it at the old wrapping.
func TestResizeReflowsDocument(t *testing.T) {
	term := newFake()
	term.size = terminal.Size{Width: 80, Height: 10}
	a := newApp(t, "alpha beta gamma delta epsilon zeta eta theta iota kappa\n", term)

	wide := len(a.rendered.Lines)

	term.mu.Lock()
	term.size = terminal.Size{Width: 20, Height: 10}
	term.mu.Unlock()
	a.resize()

	if len(a.rendered.Lines) <= wide {
		t.Errorf("narrowing produced %d rows, want more than %d", len(a.rendered.Lines), wide)
	}
	for _, line := range a.rendered.Lines {
		if got := len([]rune(line.SearchText)); got > 20 {
			t.Errorf("row %q is wider than the new terminal", line.SearchText)
		}
	}
}

func TestResizeKeepsPosition(t *testing.T) {
	term := newFake()
	term.size = terminal.Size{Width: 80, Height: 10}
	a := newApp(t, numberedLines(60), term)
	a.top = 20
	line := a.sourceLine()

	term.mu.Lock()
	term.size = terminal.Size{Width: 40, Height: 10}
	term.mu.Unlock()
	a.resize()

	if got := a.sourceLine(); got != line {
		t.Errorf("source line after resize = %d, want %d", got, line)
	}
}

func TestResizeUpdatesPageHeight(t *testing.T) {
	term := newFake()
	term.size = terminal.Size{Width: 40, Height: 10}
	a := newApp(t, numberedLines(60), term)

	term.mu.Lock()
	term.size = terminal.Size{Width: 40, Height: 20}
	term.mu.Unlock()
	a.resize()

	if a.pageHeight() != 19 {
		t.Errorf("pageHeight = %d, want 19", a.pageHeight())
	}
}

// SIGWINCH reflows; the terminating signals exit through the deferred Leave.
func TestSignalsExitCleanly(t *testing.T) {
	for _, sig := range []syscall.Signal{syscall.SIGINT, syscall.SIGTERM, syscall.SIGHUP} {
		t.Run(sig.String(), func(t *testing.T) {
			term := newFake() // no events: only the signal can end the loop
			defer term.close()
			cfg := Config{Path: writeSource(t, "text\n"), Theme: style.ThemeDark}

			done := make(chan error, 1)
			go func() { done <- run(cfg, term) }()

			// Give the loop a moment to install its handlers.
			deadline := time.After(2 * time.Second)
			for term.frameCount() == 0 {
				select {
				case <-deadline:
					t.Fatal("the viewer never drew a frame")
				default:
					time.Sleep(time.Millisecond)
				}
			}
			syscall.Kill(syscall.Getpid(), sig)

			select {
			case err := <-done:
				if err != nil {
					t.Errorf("run: %v", err)
				}
			case <-deadline:
				t.Fatal("the viewer did not exit on the signal")
			}

			if term.left != 1 {
				t.Errorf("Leave called %d times, want 1", term.left)
			}
		})
	}
}

func TestSearchModeIgnoresNormalKeys(t *testing.T) {
	a := newApp(t, numberedLines(40), newFake())
	a.handle(key('/'))
	a.handle(key('j')) // becomes part of the query, not a scroll

	if a.query != "j" {
		t.Errorf("query = %q, want j", a.query)
	}
	if a.mode != modeSearch {
		t.Error("left search mode unexpectedly")
	}
}

func TestNextDirectionFollowsSearchDirection(t *testing.T) {
	src := "needle\n\n" + numberedLines(20) + "\nneedle\n"
	a := newApp(t, src, newFake())

	// A backward search means n travels backward.
	a.handle(key('?'))
	typeQuery(a, "needle")
	a.handle(press(terminal.KeyEnter))
	if a.direction != search.Backward {
		t.Fatalf("direction = %v, want backward", a.direction)
	}

	start := a.matches[a.active].Line
	a.handle(key('n'))
	if got := a.matches[a.active].Line; got > start && !strings.Contains(a.status(), "wrapped") {
		t.Errorf("n moved forward from %d to %d in a backward search", start, got)
	}
}
