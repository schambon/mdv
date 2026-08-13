// Package app is the viewer: it owns the viewport, the event loop, search and
// edit modes, and frame composition. It is the only package that knows about
// all the others.
package app

import (
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"strings"
	"syscall"

	"github.com/schambon/mdv/internal/editor"
	"github.com/schambon/mdv/internal/layout"
	"github.com/schambon/mdv/internal/md"
	"github.com/schambon/mdv/internal/search"
	"github.com/schambon/mdv/internal/source"
	"github.com/schambon/mdv/internal/style"
	"github.com/schambon/mdv/internal/terminal"
)

// Config is the viewer's startup configuration, assembled from the command
// line.
type Config struct {
	Path        string
	Width       int // 0 means use the terminal width
	Theme       style.Theme
	LineNumbers bool
	Color       bool
}

// mode is the viewer's input mode.
type mode int

const (
	modeNormal mode = iota
	modeSearch
)

// App holds all viewer state.
type App struct {
	cfg     Config
	term    terminal.Terminal
	styler  style.Styler
	env     func(string) string
	runEdit func(argv []string) error

	src      source.Source
	rendered layout.Document
	size     terminal.Size

	top     int
	mode    mode
	message string

	query     string
	lastQuery string
	direction search.Direction
	matches   []search.Match
	active    int
	savedTop  int
}

// Run opens the file and drives the viewer until the user quits.
func Run(cfg Config) error {
	term, err := terminal.New()
	if err != nil {
		return err
	}
	return run(cfg, term)
}

// run is Run with the terminal injected, so tests can supply a fake.
func run(cfg Config, term terminal.Terminal) error {
	a := &App{
		cfg:    cfg,
		term:   term,
		styler: style.New(cfg.Theme, cfg.Color),
		env:    os.Getenv,
		active: -1,
	}
	a.runEdit = a.execEditor

	// Loading and rendering happen before raw mode, so a failure here leaves
	// the terminal untouched.
	if err := a.load(); err != nil {
		return err
	}
	a.size = terminal.Normalize(a.currentSize())
	a.render()

	if err := term.Enter(); err != nil {
		return err
	}
	defer term.Leave()
	defer a.recoverTerminal()

	return a.loop()
}

// recoverTerminal restores the screen before a panic escapes, so a crash does
// not leave the user in raw mode on the alternate screen.
func (a *App) recoverTerminal() {
	if r := recover(); r != nil {
		_ = a.term.Leave()
		panic("mdv: internal panic")
	}
}

// loop draws a frame, waits for an event, and repeats. Input is read through a
// request-gated pump so exactly one blocking read is outstanding at a time,
// letting an editor take exclusive ownership of stdin.
func (a *App) loop() error {
	signals := make(chan os.Signal, 8)
	signal.Notify(signals, syscall.SIGWINCH, syscall.SIGINT, syscall.SIGTERM, syscall.SIGHUP)
	defer signal.Stop(signals)

	pump := newPump(a.term)
	defer pump.stop()

	for {
		if err := a.draw(); err != nil {
			return err
		}
		// A message is shown for exactly one frame.
		a.message = ""

		pump.request()
		select {
		case res := <-pump.events:
			pump.received()
			if res.err != nil {
				return res.err
			}
			quit, err := a.handle(res.event)
			if err != nil {
				return err
			}
			if quit {
				return nil
			}

		case sig := <-signals:
			if sig != syscall.SIGWINCH {
				return nil // exit cleanly; the deferred Leave restores the terminal
			}
			a.resize()
		}
	}
}

// load reads the source file and reparses it.
func (a *App) load() error {
	src, err := source.Load(a.cfg.Path)
	if err != nil {
		return err
	}
	a.src = src
	return nil
}

// currentSize reads the terminal size, falling back when it cannot be read.
func (a *App) currentSize() terminal.Size {
	size, err := a.term.Size()
	if err != nil {
		return terminal.Size{Width: terminal.FallbackWidth, Height: terminal.FallbackHeight}
	}
	return size
}

// renderWidth is the terminal width, capped by --width when that is narrower.
func (a *App) renderWidth() int {
	width := a.size.Width
	if a.cfg.Width > 0 && a.cfg.Width < width {
		width = a.cfg.Width
	}
	return width
}

// render lays the document out at the current width.
func (a *App) render() {
	a.rendered = layout.Render(md.Parse(a.src.Bytes), layout.Options{
		Width:       a.renderWidth(),
		LineNumbers: a.cfg.LineNumbers,
	})
}

// pageHeight is the number of document rows on screen, leaving the last row
// for the status line.
func (a *App) pageHeight() int {
	if h := a.size.Height - 1; h > 0 {
		return h
	}
	return 1
}

// maxTop is the first row of the last page.
func (a *App) maxTop() int {
	if top := len(a.rendered.Lines) - a.pageHeight(); top > 0 {
		return top
	}
	return 0
}

func (a *App) clamp(top int) int {
	if top > a.maxTop() {
		top = a.maxTop()
	}
	if top < 0 {
		top = 0
	}
	return top
}

// sourceLine is the source line the viewport is showing, used for the status
// line, for editing, and for holding position across a reload or resize.
func (a *App) sourceLine() int {
	for i := a.top; i < len(a.rendered.Lines); i++ {
		if line := a.rendered.Lines[i].Source.Start.Line; line > 0 {
			return line
		}
	}
	return 1
}

// resize re-renders at the new width and holds the reader's place. The
// document must be reflowed, or the text would keep its old wrapping.
func (a *App) resize() {
	line := a.sourceLine()
	a.size = terminal.Normalize(a.currentSize())
	a.render()
	a.top = a.clamp(layout.Nearest(a.rendered, line))
	if a.query != "" {
		a.matches = search.Find(a.rendered, a.query)
		a.active = -1
	}
}

// reload re-reads the file from disk, keeping the viewport near the same
// source line.
func (a *App) reload() error {
	line := a.sourceLine()
	if err := a.load(); err != nil {
		return err
	}
	a.size = terminal.Normalize(a.currentSize())
	a.render()
	a.top = a.clamp(layout.Nearest(a.rendered, line))
	if a.query != "" {
		a.matches = search.Find(a.rendered, a.query)
		a.active = -1
	}
	return nil
}

// edit suspends the viewer, runs the editor on the current source line, then
// reloads unconditionally: the file may have changed even if the editor failed.
func (a *App) edit() error {
	argv, err := editor.Command(a.env, a.src.Path, a.sourceLine())
	if err != nil {
		a.message = err.Error()
		return nil
	}

	editErr := a.term.Suspend(func() error { return a.runEdit(argv) })

	// A reload problem is the more urgent of the two, so it wins.
	if err := a.reload(); err != nil {
		a.message = err.Error()
		return nil
	}
	if editErr != nil {
		a.message = editErr.Error()
	}
	return nil
}

// execEditor runs the editor with the controlling terminal attached.
func (a *App) execEditor(argv []string) error {
	cmd := exec.Command(argv[0], argv[1:]...)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	return cmd.Run()
}

// statusText is the normal status line.
func (a *App) statusText() string {
	total := len(a.rendered.Lines)
	percent := 100
	if a.maxTop() > 0 {
		percent = a.top * 100 / a.maxTop()
	}
	sourceTotal := 1
	if total > 0 {
		sourceTotal = a.rendered.Lines[total-1].Source.Start.Line
	}
	return fmt.Sprintf("%s  %d%%  source line %d/%d",
		a.src.Name, percent, a.sourceLine(), sourceTotal)
}

// status is what the last row shows: a message, the search prompt, or the
// normal status text.
func (a *App) status() string {
	switch {
	case a.message != "":
		return a.message
	case a.mode == modeSearch:
		prompt := "/"
		if a.direction == search.Backward {
			prompt = "?"
		}
		return prompt + a.query
	default:
		return a.statusText()
	}
}

// draw composes and writes one complete frame.
func (a *App) draw() error {
	var sb strings.Builder
	sb.WriteString(terminal.CursorHome)
	sb.WriteString(terminal.ClearScreen)

	height := a.pageHeight()
	for row := range height {
		index := a.top + row
		if index < len(a.rendered.Lines) {
			sb.WriteString(a.styler.Line(a.highlight(index)))
		}
		sb.WriteString(terminal.ClearToEOL)
		// OPOST is off, so line endings must be written explicitly.
		sb.WriteString("\r\n")
	}

	status, _ := clipCells(a.status(), a.size.Width)
	sb.WriteString(a.styler.Apply(status, layout.StyleStatus))
	sb.WriteString(terminal.ClearToEOL)

	return a.term.Draw(sb.String())
}

// highlight applies search highlighting to one rendered row.
func (a *App) highlight(index int) layout.RenderedLine {
	line := a.rendered.Lines[index]
	if len(a.matches) == 0 {
		return line
	}

	var onLine []search.Match
	activeOnLine := -1
	for i, m := range a.matches {
		if m.Line == index {
			if i == a.active {
				activeOnLine = len(onLine)
			}
			onLine = append(onLine, m)
		}
	}
	if len(onLine) == 0 {
		return line
	}
	return search.Highlight(line, onLine, activeOnLine)
}

// clipCells truncates text to a cell budget, so the status row cannot wrap.
func clipCells(text string, cells int) (string, int) {
	used := 0
	for i, r := range text {
		w := layout.CellWidth(r)
		if used+w > cells {
			return text[:i], used
		}
		used += w
	}
	return text, used
}
