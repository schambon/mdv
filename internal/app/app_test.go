package app

import (
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/schambon/mdv/internal/style"
	"github.com/schambon/mdv/internal/terminal"
)

func TestPagingAndClamping(t *testing.T) {
	term := newFake()
	a := newApp(t, numberedLines(40), term) // height 6 -> a 5-row page

	if a.pageHeight() != 5 {
		t.Fatalf("pageHeight = %d, want 5", a.pageHeight())
	}

	a.handle(key('j'))
	if a.top != 1 {
		t.Errorf("after j, top = %d, want 1", a.top)
	}
	a.handle(key('k'))
	if a.top != 0 {
		t.Errorf("after k, top = %d, want 0", a.top)
	}

	// Up at the top of the document stays put.
	a.handle(key('k'))
	if a.top != 0 {
		t.Errorf("k at the top moved to %d, want 0", a.top)
	}

	a.handle(key(' '))
	if a.top != 5 {
		t.Errorf("after space, top = %d, want 5", a.top)
	}
	a.handle(key('b'))
	if a.top != 0 {
		t.Errorf("after b, top = %d, want 0", a.top)
	}

	a.handle(key('G'))
	if a.top != a.maxTop() {
		t.Errorf("after G, top = %d, want %d", a.top, a.maxTop())
	}
	// Paging past the end clamps to the last page.
	a.handle(key(' '))
	if a.top != a.maxTop() {
		t.Errorf("space at the end moved to %d, want %d", a.top, a.maxTop())
	}

	a.handle(key('g'))
	if a.top != 0 {
		t.Errorf("after g, top = %d, want 0", a.top)
	}
}

func TestArrowAndPageKeys(t *testing.T) {
	a := newApp(t, numberedLines(40), newFake())

	tests := []struct {
		name string
		key  terminal.Key
		from int
		want int
	}{
		{"down", terminal.KeyDown, 0, 1},
		{"enter scrolls", terminal.KeyEnter, 0, 1},
		{"up", terminal.KeyUp, 4, 3},
		{"page down", terminal.KeyPageDown, 0, 5},
		{"page up", terminal.KeyPageUp, 10, 5},
		{"home", terminal.KeyHome, 20, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			a.top = tt.from
			a.handle(press(tt.key))
			if a.top != tt.want {
				t.Errorf("top = %d, want %d", a.top, tt.want)
			}
		})
	}

	a.top = 0
	a.handle(press(terminal.KeyEnd))
	if a.top != a.maxTop() {
		t.Errorf("End -> %d, want %d", a.top, a.maxTop())
	}
}

func TestControlKeysPage(t *testing.T) {
	a := newApp(t, numberedLines(40), newFake())

	a.handle(key(ctrlF))
	if a.top != 5 {
		t.Errorf("Ctrl-F -> %d, want 5", a.top)
	}
	a.handle(key(ctrlB))
	if a.top != 0 {
		t.Errorf("Ctrl-B -> %d, want 0", a.top)
	}
}

func TestShortDocumentDoesNotScroll(t *testing.T) {
	a := newApp(t, "one\n", newFake())
	a.handle(key('G'))
	if a.top != 0 {
		t.Errorf("top = %d, want 0 for a document shorter than a page", a.top)
	}
}

func TestQuit(t *testing.T) {
	a := newApp(t, "text\n", newFake())
	got, err := a.handle(quit)
	if err != nil {
		t.Fatalf("handle: %v", err)
	}
	if !got {
		t.Error("q did not quit")
	}
}

func TestHelpMessageLastsOneFrame(t *testing.T) {
	term := newFake(key('h'), quit)
	a := newApp(t, "text\n", term)
	a.term = term

	a.handle(key('h'))
	if !strings.Contains(a.status(), "quit") {
		t.Errorf("status = %q, want the key summary", a.status())
	}
}

func TestStatusLineFormat(t *testing.T) {
	a := newApp(t, numberedLines(40), newFake())
	status := a.status()

	if !strings.HasPrefix(status, "doc.md") {
		t.Errorf("status = %q, want it to start with the filename", status)
	}
	if !strings.Contains(status, "0%") {
		t.Errorf("status = %q, want a percentage", status)
	}
	if !strings.Contains(status, "source line 1/") {
		t.Errorf("status = %q, want the source line", status)
	}
}

// The total is the file's line count, not the last block's starting line: a
// final line that merges into the preceding paragraph must still be counted.
func TestStatusTotalIsFileLineCount(t *testing.T) {
	a := newApp(t, "one\ntwo\nthree\n", newFake())
	if !strings.Contains(a.status(), "source line 1/3") {
		t.Errorf("status = %q, want a total of 3", a.status())
	}
}

func TestStatusPercentReachesHundred(t *testing.T) {
	a := newApp(t, numberedLines(40), newFake())
	a.handle(key('G'))
	if !strings.Contains(a.status(), "100%") {
		t.Errorf("status at the end = %q, want 100%%", a.status())
	}
}

func TestFrameRowsEndWithCRLF(t *testing.T) {
	term := newFake()
	a := newApp(t, numberedLines(10), term)

	if err := a.draw(); err != nil {
		t.Fatalf("draw: %v", err)
	}

	frame := term.lastFrame()
	if strings.Count(frame, "\r\n") != a.pageHeight() {
		t.Errorf("frame has %d CRLF endings, want %d",
			strings.Count(frame, "\r\n"), a.pageHeight())
	}
	// A bare LF would be misplaced with OPOST disabled.
	if strings.Contains(strings.ReplaceAll(frame, "\r\n", ""), "\n") {
		t.Error("frame contains a bare LF")
	}
}

func TestFrameStartsWithHomeAndClear(t *testing.T) {
	term := newFake()
	a := newApp(t, "text\n", term)
	a.draw()

	frame := term.lastFrame()
	if !strings.HasPrefix(frame, terminal.CursorHome+terminal.ClearScreen) {
		t.Errorf("frame does not begin by homing and clearing: %q", frame[:20])
	}
}

// Short documents still fill the page, so stale rows are erased.
func TestFrameFillsUnusedRows(t *testing.T) {
	term := newFake()
	a := newApp(t, "one\n", term)
	a.draw()

	rows := strings.Split(term.lastFrame(), "\r\n")
	if len(rows) != a.pageHeight()+1 { // the trailing element holds the status
		t.Errorf("frame has %d rows, want %d", len(rows)-1, a.pageHeight())
	}
}

// A row that fills the terminal to its final column must reach the screen
// whole. The per-row erase that used to follow the content erased that last
// column, dropping the final character of a full-width line.
func TestFullWidthRowKeepsItsLastColumn(t *testing.T) {
	term := newFake()
	term.size = terminal.Size{Width: 30, Height: 12}
	// A fenced code line longer than the content width hard-splits into rows of
	// exactly the terminal width.
	a := newApp(t, "```\n"+strings.Repeat("x", 80)+"\n```\n", term)
	a.draw()
	frame := term.lastFrame()

	full := ""
	for _, line := range a.rendered.Lines {
		cells := 0
		for _, s := range line.Spans {
			cells += s.Cells
		}
		if cells == a.size.Width {
			full = line.SearchText
			break
		}
	}
	if full == "" {
		t.Fatal("no full-width row was produced; test no longer exercises the bug")
	}
	// Colour is off, so the row's text reaches the frame verbatim. It must be
	// terminated by CRLF, never by an erase that would clear its final column.
	if !strings.Contains(frame, full+"\r\n") {
		t.Errorf("full-width row %q not emitted intact before CRLF", full)
	}
	if strings.Contains(frame, full+terminal.ClearToEOL) {
		t.Errorf("full-width row %q is followed by ClearToEOL, which erases its last column", full)
	}
}

func TestStatusRowIsClippedToWidth(t *testing.T) {
	term := newFake()
	term.size = terminal.Size{Width: 20, Height: 6}
	a := newApp(t, numberedLines(40), term)
	a.draw()

	rows := strings.Split(term.lastFrame(), "\r\n")
	status := strings.TrimSuffix(rows[len(rows)-1], terminal.ClearToEOL)
	if len([]rune(status)) > 20 {
		t.Errorf("status row %q is wider than the terminal", status)
	}
}

func TestDrawErrorEndsTheLoop(t *testing.T) {
	term := newFake(quit)
	term.drawErr = errors.New("write failed")

	cfg := Config{Path: writeSource(t, "text\n"), Theme: style.ThemeDark}
	if err := run(cfg, term); err == nil {
		t.Error("run ignored a draw failure")
	}
}

func TestRunEntersAndLeavesTheTerminal(t *testing.T) {
	term := newFake(quit)
	cfg := Config{Path: writeSource(t, "text\n"), Theme: style.ThemeDark}

	if err := run(cfg, term); err != nil {
		t.Fatalf("run: %v", err)
	}
	if term.entered != 1 {
		t.Errorf("entered %d times, want 1", term.entered)
	}
	if term.left != 1 {
		t.Errorf("left %d times, want 1", term.left)
	}
}

// A bad path must fail before the terminal is touched.
func TestRunFailsBeforeEnteringOnLoadError(t *testing.T) {
	term := newFake(quit)
	cfg := Config{Path: "/nonexistent/file.md", Theme: style.ThemeDark}

	if err := run(cfg, term); err == nil {
		t.Fatal("run accepted a missing file")
	}
	if term.entered != 0 {
		t.Errorf("entered the terminal %d times, want 0", term.entered)
	}
}

func TestRunDrawsAndQuits(t *testing.T) {
	term := newFake(key('j'), key('j'), quit)
	cfg := Config{Path: writeSource(t, numberedLines(20)), Theme: style.ThemeDark}

	if err := run(cfg, term); err != nil {
		t.Fatalf("run: %v", err)
	}
	if term.frameCount() < 3 {
		t.Errorf("drew %d frames, want one per event", term.frameCount())
	}
}

func TestWidthOptionCapsRendering(t *testing.T) {
	term := newFake()
	term.size = terminal.Size{Width: 100, Height: 10}

	a := newApp(t, "text\n", term)
	a.cfg.Width = 30
	if got := a.renderWidth(); got != 30 {
		t.Errorf("renderWidth = %d, want 30", got)
	}

	// A width wider than the terminal does not stretch the output.
	a.cfg.Width = 200
	if got := a.renderWidth(); got != 100 {
		t.Errorf("renderWidth = %d, want the terminal width", got)
	}
}

func TestSizeFallbacks(t *testing.T) {
	term := newFake()
	term.sizeErr = errors.New("no size")

	a := newApp(t, "text\n", term)
	if a.size.Width != terminal.FallbackWidth || a.size.Height != terminal.FallbackHeight {
		t.Errorf("size = %+v, want the fallbacks", a.size)
	}
}

func TestUnusableSizeFallsBack(t *testing.T) {
	term := newFake()
	term.size = terminal.Size{Width: 3, Height: 1}

	a := newApp(t, "text\n", term)
	if a.size.Width != terminal.FallbackWidth {
		t.Errorf("width = %d, want the fallback", a.size.Width)
	}
	if a.size.Height != terminal.FallbackHeight {
		t.Errorf("height = %d, want the fallback", a.size.Height)
	}
}

func TestSourceLineTracksViewport(t *testing.T) {
	a := newApp(t, numberedLines(40), newFake())
	if got := a.sourceLine(); got != 1 {
		t.Errorf("sourceLine at the top = %d, want 1", got)
	}

	a.top = 4
	if got := a.sourceLine(); got <= 1 {
		t.Errorf("sourceLine after scrolling = %d, want it to advance", got)
	}
}

func TestPanicRestoresTerminal(t *testing.T) {
	term := newFake()
	a := newApp(t, "text\n", term)

	func() {
		defer func() {
			if r := recover(); r != "mdv: internal panic" {
				t.Errorf("panic value = %v, want the wrapped message", r)
			}
		}()
		defer a.recoverTerminal()
		panic("something broke")
	}()

	if term.left != 1 {
		t.Errorf("Leave called %d times, want 1", term.left)
	}
}

func TestEmptyFileRenders(t *testing.T) {
	term := newFake()
	a := newApp(t, "", term)
	if err := a.draw(); err != nil {
		t.Fatalf("draw: %v", err)
	}
	if a.status() == "" {
		t.Error("status is empty")
	}
}

func TestHighlightOnlyAffectsMatchingRows(t *testing.T) {
	a := newApp(t, "alpha\n\nbeta\n", newFake())
	a.query = "alpha"
	a.updateSearch()

	// The row with no match must come back untouched.
	for i := range a.rendered.Lines {
		got := a.highlight(i)
		if len(got.Spans) != len(a.rendered.Lines[i].Spans) &&
			!strings.Contains(a.rendered.Lines[i].SearchText, "alpha") {
			t.Errorf("row %d was re-split without a match", i)
		}
	}
}

func TestConfigColorDisablesSGR(t *testing.T) {
	term := newFake()
	a := newApp(t, "# Heading\n", term)
	a.draw()

	if strings.Contains(term.lastFrame(), "\x1b[1;36m") {
		t.Error("frame contains SGR despite colour being disabled")
	}
}

func TestConfigColorEnablesSGR(t *testing.T) {
	term := newFake()
	a := newApp(t, "# Heading\n", term)
	a.styler = style.New(style.ThemeDark, true)
	a.draw()

	if !strings.Contains(term.lastFrame(), "\x1b[") {
		t.Error("frame has no SGR despite colour being enabled")
	}
}

func TestMain(m *testing.M) {
	os.Exit(m.Run())
}

func TestToggleLineNumbers(t *testing.T) {
	a := newApp(t, numberedLines(40), newFake())

	if a.cfg.LineNumbers {
		t.Fatal("line numbers should start off")
	}
	widthWithout := renderedWidth(a)

	a.handle(key('l'))
	if !a.cfg.LineNumbers {
		t.Fatal("l did not turn line numbers on")
	}
	if got := renderedWidth(a); got <= widthWithout {
		t.Errorf("gutter did not widen the rows: %d then %d", widthWithout, got)
	}
	if !strings.Contains(a.rendered.Lines[0].SearchText, "1") {
		t.Errorf("first row should carry a line number: %q", a.rendered.Lines[0].SearchText)
	}

	a.handle(key('l'))
	if a.cfg.LineNumbers {
		t.Fatal("l did not turn line numbers back off")
	}
	if got := renderedWidth(a); got != widthWithout {
		t.Errorf("width after toggling back = %d, want %d", got, widthWithout)
	}
}

// renderedWidth is the widest row in the document, which grows by the gutter.
func renderedWidth(a *App) int {
	widest := 0
	for _, line := range a.rendered.Lines {
		cells := 0
		for _, s := range line.Spans {
			cells += s.Cells
		}
		if cells > widest {
			widest = cells
		}
	}
	return widest
}

// The gutter takes width out of the content, so the document reflows. The
// reader must not lose their place.
func TestToggleLineNumbersKeepsPosition(t *testing.T) {
	a := newApp(t, numberedLines(60), newFake())

	a.top = 30
	line := a.sourceLine()

	a.handle(key('l'))
	if got := a.sourceLine(); got != line {
		t.Errorf("source line after toggling = %d, want %d", got, line)
	}
}

// Rows are rebuilt by the toggle, so matches pointing at the old ones must be
// recomputed rather than left dangling.
func TestToggleLineNumbersRefreshesMatches(t *testing.T) {
	a := newApp(t, numberedLines(40), newFake())

	a.handle(key('/'))
	for _, r := range "line3" {
		a.handle(key(r))
	}
	a.handle(press(terminal.KeyEnter))
	if len(a.matches) == 0 {
		t.Fatal("expected matches before toggling")
	}

	a.handle(key('l'))

	if len(a.matches) == 0 {
		t.Fatal("matches were lost by the toggle")
	}
	for _, m := range a.matches {
		if m.Line >= len(a.rendered.Lines) {
			t.Fatalf("match points at line %d, beyond the %d rendered", m.Line, len(a.rendered.Lines))
		}
	}
}
