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

	"github.com/schambon/mdv/internal/diffdoc"
	"github.com/schambon/mdv/internal/editor"
	"github.com/schambon/mdv/internal/git"
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

	// Compare names a second file. When it is set the viewer shows the
	// difference between Path and Compare instead of rendering Path.
	Compare    string
	Context    int // unchanged lines kept around a change; negative disables folding
	SideBySide bool
	WordDiff   bool
	// ForceRaw and ForceMarkdown override the automatic choice between
	// comparing two files as rendered Markdown and comparing them as text.
	ForceRaw      bool
	ForceMarkdown bool

	// Git, when set, selects git mode: the two sides are fetched from the
	// repository rather than read from paths given on the command line.
	Git *GitRequest
}

// markdownDiff reports whether the two files should be compared as rendered
// Markdown rather than as lines of text. Two Markdown files are compared as
// Markdown by default, because a rewrapped paragraph is not a change; --raw
// asks for the literal line diff, which is a reasonable thing to want.
func (c Config) markdownDiff() bool {
	switch {
	case !c.diffMode(), c.ForceRaw:
		return false
	case c.ForceMarkdown:
		return true
	default:
		return source.IsMarkdown(c.Path) && source.IsMarkdown(c.Compare)
	}
}

// diffMode reports whether the viewer is comparing two files. Git mode always
// is, before load has had a chance to name the file being compared.
func (c Config) diffMode() bool { return c.Compare != "" || c.Git != nil }

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
	runGit  git.Runner

	src         source.Source
	rendered    layout.Document
	sourceLines int // lines in the file, for the status line
	size        terminal.Size

	// Diff mode. compare is the new file, diff the aligned rows including
	// whatever folds the reader has expanded, and diffRows maps each rendered
	// line back to its row in diff.
	compare  source.Source
	diff     diffdoc.Document
	diffRows []int

	// Git mode with more than one changed file. files is the list the sidebar
	// shows, current the one being compared, and diffs holds the documents of
	// the files already visited so their expanded folds survive a switch back.
	repo        *git.Repo
	spec        git.Spec
	files       []git.File
	stats       map[string]git.Stat
	current     int
	currentFile string
	diffs       map[int]diffdoc.Document

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
	// Check the paths before demanding a terminal, so a bad filename reports
	// the real problem rather than complaining about the terminal. Git mode has
	// no paths to check: it names revisions, and load reports what git says.
	switch {
	case cfg.Git != nil:
		// Nothing on the command line names a file; load reports what git says.

	case cfg.diffMode():
		for _, path := range []string{cfg.Path, cfg.Compare} {
			if _, err := source.ValidateAny(path); err != nil {
				return err
			}
		}

	default:
		if _, err := source.Validate(cfg.Path); err != nil {
			return err
		}
	}
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
		runGit: git.Exec,
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

	// An auto theme is resolved now, in raw mode but before the pump starts, so
	// the OSC 11 reply is read here rather than being mistaken for a keystroke.
	// The styler is only used at draw time, so re-resolving after the initial
	// render costs nothing.
	a.resolveTheme()

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

// load reads the source file, or both sides in diff mode.
func (a *App) load() error {
	if a.cfg.Git != nil {
		return a.loadGit()
	}
	if !a.cfg.diffMode() {
		src, err := source.Load(a.cfg.Path)
		if err != nil {
			return err
		}
		a.src = src
		return nil
	}

	old, err := source.LoadAny(a.cfg.Path)
	if err != nil {
		return err
	}
	updated, err := source.LoadAny(a.cfg.Compare)
	if err != nil {
		return err
	}
	a.src, a.compare = old, updated
	a.buildDiff()
	return nil
}

// buildDiff aligns the two files. It is deliberately separate from render:
// which folds are open is part of the row list, so a resize must lay the diff
// out again without discarding what the reader has expanded.
func (a *App) buildDiff() {
	opts := diffdoc.Options{Context: a.cfg.Context, WordDiff: a.cfg.WordDiff}

	if a.cfg.markdownDiff() {
		a.diff = diffdoc.BuildMarkdown(md.Parse(a.src.Bytes), md.Parse(a.compare.Bytes), opts)
		return
	}
	a.diff = diffdoc.Build(
		diffdoc.SplitLines(a.src.Bytes),
		diffdoc.SplitLines(a.compare.Bytes),
		opts,
	)
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
	if a.cfg.diffMode() {
		rendered := layout.RenderDiff(a.diff, layout.DiffOptions{
			// The sidebar takes its cells out of the diff. RenderDiff never
			// learns it exists; its own clipping keeps to the budget.
			Width:       a.renderWidth() - a.sidebarCells(),
			LineNumbers: a.cfg.LineNumbers,
			SideBySide:  a.cfg.SideBySide,
		})
		a.rendered, a.diffRows = rendered.Document, rendered.Rows
		return
	}

	parsed := md.Parse(a.src.Bytes)
	a.sourceLines = len(parsed.Lines)
	a.rendered = layout.Render(parsed, layout.Options{
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
	a.reflow(func() { a.size = terminal.Normalize(a.currentSize()) })
}

// reflow applies a change that invalidates the current layout, lays the
// document out again, and holds the reader's place against the source line
// they were looking at. Anything that alters how rows are built — a resize, a
// fold, toggling the gutter — has to go through here: the rows are rebuilt, so
// a viewport index means nothing afterwards and search matches point at rows
// that no longer exist.
func (a *App) reflow(change func()) {
	line := a.sourceLine()
	change()
	a.render()
	a.top = a.clamp(layout.Nearest(a.rendered, line))
	a.refreshMatches()
}

// refreshMatches recomputes search matches after the rows they refer to have
// been rebuilt.
func (a *App) refreshMatches() {
	if a.query == "" {
		return
	}
	a.matches = search.Find(a.rendered, a.query)
	a.active = -1
}

// toggleLineNumbers shows or hides the line-number gutter. The gutter takes
// its width out of the content, so the document has to be laid out again
// rather than merely redrawn.
func (a *App) toggleLineNumbers() {
	a.reflow(func() { a.cfg.LineNumbers = !a.cfg.LineNumbers })
}

// resolveTheme decides an auto theme against the live terminal. A concrete
// theme was already resolved from the environment when the styler was built, so
// this only re-resolves when the user asked for auto; the OSC 11 probe replaces
// that guess with what the terminal actually reports.
func (a *App) resolveTheme() {
	if a.cfg.Theme != style.ThemeAuto {
		return
	}
	resolved := style.Detect(a.term.QueryBackground)
	a.styler = style.New(resolved, a.cfg.Color)
}

// toggleTheme flips between the dark and light palettes at runtime. Styling is
// applied when a frame is drawn, so swapping the styler is enough — the next
// draw repaints with no relayout. With colour off the swap would be invisible,
// so it reports that instead of pretending to have done something.
func (a *App) toggleTheme() {
	if !a.cfg.Color {
		a.message = "colour is off"
		return
	}
	next := style.ThemeLight
	if a.styler.Theme() == style.ThemeLight {
		next = style.ThemeDark
	}
	a.styler = style.New(next, a.cfg.Color)
	a.message = "theme: " + string(next)
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
	a.refreshMatches()
	return nil
}

// edit suspends the viewer, runs the editor on the current source line, then
// reloads unconditionally: the file may have changed even if the editor failed.
func (a *App) edit() error {
	target := a.editTarget()
	if target == "" {
		// Comparing two revisions: neither side is a file, so there is nothing
		// to open, and inventing a path would edit the wrong thing.
		a.message = errNoWorkTreeFile.Error()
		return nil
	}

	argv, err := editor.Command(a.env, target, a.sourceLine())
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
	percent := 100
	if a.maxTop() > 0 {
		percent = a.top * 100 / a.maxTop()
	}

	if a.cfg.diffMode() {
		return fmt.Sprintf("%s → %s%s  %d%%  %s",
			a.src.Name, a.compare.Name, a.filePosition(), percent, a.diffSummary())
	}

	total := a.sourceLines
	if total < 1 {
		total = 1
	}
	return fmt.Sprintf("%s  %d%%  source line %d/%d",
		a.src.Name, percent, a.sourceLine(), total)
}

// filePosition places the file being compared in the changed-file list. It is
// shown whenever there is a list, including when the terminal is too narrow
// for the sidebar and this is the only sign that other files changed.
func (a *App) filePosition() string {
	if len(a.files) < 2 {
		return ""
	}
	return fmt.Sprintf("  file %d/%d", a.current+1, len(a.files))
}

// diffSummary counts the changed lines, so the reader knows the size of what
// they are paging through without reaching the end.
func (a *App) diffSummary() string {
	added, removed := 0, 0
	var count func(rows []diffdoc.Row)
	count = func(rows []diffdoc.Row) {
		for _, row := range rows {
			switch row.Kind {
			case diffdoc.RowFolded:
				count(row.Hidden)
			case diffdoc.RowAdded:
				added++
			case diffdoc.RowRemoved:
				removed++
			case diffdoc.RowChanged:
				added, removed = added+1, removed+1
			}
		}
	}
	count(a.diff.Rows)

	if added == 0 && removed == 0 {
		return "identical"
	}
	// In Markdown mode a row is a block, not a line, and an unqualified
	// "+3 -1" would be read as lines.
	if a.cfg.markdownDiff() {
		return fmt.Sprintf("+%d -%d blocks", added, removed)
	}
	return fmt.Sprintf("+%d -%d", added, removed)
}

// editTarget is the file v opens. In diff mode that is the new file, matching
// the line number the status bar and the row mapping report. It is empty when
// no side is on disk, as when git compares two revisions.
func (a *App) editTarget() string {
	if a.cfg.diffMode() {
		return a.compare.Path
	}
	return a.src.Path
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
	var sidebar [][]layout.Span
	if width := a.sidebarWidth(); width > 0 {
		sidebar = a.sidebarRows(width, height)
	}

	for row := range height {
		index := a.top + row
		var line layout.RenderedLine
		if index < len(a.rendered.Lines) {
			line = a.highlight(index)
		}
		if sidebar != nil {
			line = a.compose(sidebar[row], line)
		}
		if len(line.Spans) > 0 {
			sb.WriteString(a.styler.Line(line))
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
