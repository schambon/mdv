package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/schambon/mdv/internal/diffdoc"
	"github.com/schambon/mdv/internal/style"
	"github.com/schambon/mdv/internal/terminal"
)

// newDiffApp builds an App comparing two files, without entering the event
// loop. The fake terminal is wide enough for two panes unless a test narrows
// it.
func newDiffApp(t *testing.T, old, updated string, term *fakeTerminal, opts ...func(*Config)) *App {
	t.Helper()

	dir := t.TempDir()
	oldPath := filepath.Join(dir, "old.txt")
	newPath := filepath.Join(dir, "new.txt")
	for path, contents := range map[string]string{oldPath: old, newPath: updated} {
		if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	cfg := Config{
		Path:       oldPath,
		Compare:    newPath,
		Theme:      style.ThemeDark,
		Color:      false,
		Context:    diffdoc.DefaultContext,
		SideBySide: true,
		WordDiff:   true,
	}
	for _, opt := range opts {
		opt(&cfg)
	}

	a := &App{
		cfg:    cfg,
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

// wideFake is a terminal roomy enough for a side-by-side view.
func wideFake(events ...terminal.Event) *fakeTerminal {
	f := newFake(events...)
	f.size = terminal.Size{Width: 100, Height: 11}
	return f
}

// numbered builds n distinct plain-text lines.
func numbered(n int) string {
	var sb strings.Builder
	for i := 1; i <= n; i++ {
		sb.WriteString("line" + itoa(i) + "\n")
	}
	return sb.String()
}

// replaceLine returns text with the one-based line n replaced.
func replaceLine(text string, n int, with string) string {
	lines := strings.Split(strings.TrimSuffix(text, "\n"), "\n")
	lines[n-1] = with
	return strings.Join(lines, "\n") + "\n"
}

func rowKinds(a *App) []diffdoc.RowKind {
	kinds := make([]diffdoc.RowKind, len(a.diff.Rows))
	for i, r := range a.diff.Rows {
		kinds[i] = r.Kind
	}
	return kinds
}

func countFolds(a *App) int {
	n := 0
	for _, r := range a.diff.Rows {
		if r.Kind == diffdoc.RowFolded {
			n++
		}
	}
	return n
}

func TestDiffModeRendersBothFiles(t *testing.T) {
	term := wideFake()
	a := newDiffApp(t, "one\ntwo\nthree\n", "one\nTWO\nthree\n", term)

	if len(a.rendered.Lines) == 0 {
		t.Fatal("diff produced no rows")
	}
	if len(a.diffRows) != len(a.rendered.Lines) {
		t.Fatalf("row mapping covers %d of %d lines", len(a.diffRows), len(a.rendered.Lines))
	}

	var text strings.Builder
	for _, line := range a.rendered.Lines {
		text.WriteString(line.SearchText + "\n")
	}
	if !strings.Contains(text.String(), "two") || !strings.Contains(text.String(), "TWO") {
		t.Fatalf("both sides should appear:\n%s", text.String())
	}
}

func TestDiffStatusLine(t *testing.T) {
	a := newDiffApp(t, "one\ntwo\n", "one\nTWO\nthree\n", wideFake())

	status := a.statusText()
	if !strings.Contains(status, "old.txt → new.txt") {
		t.Errorf("status should name both files, got %q", status)
	}
	// One rewritten line and one added one.
	if !strings.Contains(status, "+2 -1") {
		t.Errorf("status should summarise the change, got %q", status)
	}
}

func TestDiffStatusIdentical(t *testing.T) {
	a := newDiffApp(t, "same\n", "same\n", wideFake())
	if got := a.diffSummary(); got != "identical" {
		t.Errorf("diffSummary = %q, want \"identical\"", got)
	}
}

func TestDiffHelpIsModeSpecific(t *testing.T) {
	diff := newDiffApp(t, "a\n", "b\n", wideFake())
	if got := diff.help(); got != diffKeyHelp {
		t.Errorf("diff mode help = %q", got)
	}

	plain := newApp(t, "hello\n", newFake())
	if got := plain.help(); got != keyHelp {
		t.Errorf("viewer help = %q", got)
	}
}

// The diff keys must not fire in the ordinary viewer, where they mean nothing
// and would shadow keys the reader expects to be inert.
func TestDiffKeysInertInViewerMode(t *testing.T) {
	a := newApp(t, numberedLines(40), newFake())
	for _, r := range []rune{'x', 'X', 'z', ']', '['} {
		if a.handleDiffRune(r) {
			t.Errorf("%q was consumed outside diff mode", r)
		}
	}
}

func TestExpandFold(t *testing.T) {
	old := numbered(40)
	updated := replaceLine(old, 20, "CHANGED")
	a := newDiffApp(t, old, updated, wideFake())

	if countFolds(a) != 2 {
		t.Fatalf("want a fold above and below the change, got %v", rowKinds(a))
	}
	before := len(a.rendered.Lines)

	a.handle(key('x'))

	if countFolds(a) != 1 {
		t.Fatalf("x should open one fold, got %v", rowKinds(a))
	}
	if len(a.rendered.Lines) <= before {
		t.Fatalf("expanding did not add rows: %d then %d", before, len(a.rendered.Lines))
	}
	if a.message != "" {
		t.Errorf("unexpected message %q", a.message)
	}
}

// Expanding a fold below the viewport must not move what the reader is looking
// at.
func TestExpandFoldKeepsViewport(t *testing.T) {
	old := numbered(60)
	updated := replaceLine(replaceLine(old, 10, "FIRST"), 50, "SECOND")
	a := newDiffApp(t, old, updated, wideFake())

	a.top = 3
	a.handle(key('x'))
	if a.top != 3 {
		t.Errorf("top moved to %d, want 3", a.top)
	}
}

func TestExpandFoldReportsWhenNoneLeft(t *testing.T) {
	a := newDiffApp(t, "one\ntwo\n", "one\nTWO\n", wideFake())

	a.handle(key('x'))
	if a.message == "" {
		t.Error("expanding with no folds should say so")
	}
}

func TestExpandAllFolds(t *testing.T) {
	old := numbered(40)
	updated := replaceLine(old, 20, "CHANGED")
	a := newDiffApp(t, old, updated, wideFake())

	a.handle(key('X'))
	if countFolds(a) != 0 {
		t.Fatalf("X should open every fold, got %v", rowKinds(a))
	}
	// Every line of both files is now on screen: 40 rows.
	if len(a.diff.Rows) != 40 {
		t.Errorf("expanded document has %d rows, want 40", len(a.diff.Rows))
	}
}

func TestCollapseFolds(t *testing.T) {
	old := numbered(40)
	updated := replaceLine(old, 20, "CHANGED")
	a := newDiffApp(t, old, updated, wideFake())

	a.handle(key('X'))
	a.handle(key('z'))
	if countFolds(a) != 2 {
		t.Fatalf("z should fold the context again, got %v", rowKinds(a))
	}
}

func TestCollapseRefusedWhenFoldingDisabled(t *testing.T) {
	old := numbered(40)
	updated := replaceLine(old, 20, "CHANGED")
	a := newDiffApp(t, old, updated, wideFake(), func(c *Config) { c.Context = -1 })

	if countFolds(a) != 0 {
		t.Fatal("negative context should disable folding")
	}
	a.handle(key('z'))
	if a.message == "" {
		t.Error("z with folding disabled should explain itself")
	}
}

func TestJumpBetweenHunks(t *testing.T) {
	old := numbered(60)
	updated := replaceLine(replaceLine(old, 10, "FIRST"), 40, "SECOND")
	a := newDiffApp(t, old, updated, wideFake(), func(c *Config) { c.Context = -1 })

	starts := a.hunkStarts()
	if len(starts) != 2 {
		t.Fatalf("want 2 hunks, got %d", len(starts))
	}

	a.top = 0
	a.handle(key(']'))
	if a.top != starts[0] {
		t.Errorf("] went to %d, want the first hunk at %d", a.top, starts[0])
	}
	a.handle(key(']'))
	if a.top != a.clamp(starts[1]) {
		t.Errorf("] went to %d, want the second hunk at %d", a.top, a.clamp(starts[1]))
	}

	// Past the last hunk it reports rather than wrapping, so the reader knows
	// they have seen everything.
	a.handle(key(']'))
	if a.message == "" {
		t.Error("] past the last hunk should say so")
	}

	a.handle(key('['))
	if a.top != starts[0] {
		t.Errorf("[ went to %d, want %d", a.top, starts[0])
	}
	a.handle(key('['))
	if a.message == "" {
		t.Error("[ before the first hunk should say so")
	}
}

// A rewritten block is one hunk, not one stop per line.
func TestAdjacentChangesAreOneHunk(t *testing.T) {
	old := numbered(30)
	updated := old
	for i := 10; i <= 13; i++ {
		updated = replaceLine(updated, i, "CHANGED"+itoa(i))
	}
	a := newDiffApp(t, old, updated, wideFake(), func(c *Config) { c.Context = -1 })

	if got := len(a.hunkStarts()); got != 1 {
		t.Errorf("four adjacent changed lines gave %d hunks, want 1", got)
	}
}

func TestJumpWithNoChanges(t *testing.T) {
	a := newDiffApp(t, "same\n", "same\n", wideFake(), func(c *Config) { c.Context = -1 })
	a.handle(key(']'))
	if a.message == "" {
		t.Error("] with no changes should say so")
	}
}

// Resizing must reflow the diff without discarding folds the reader opened.
func TestResizeKeepsExpandedFolds(t *testing.T) {
	old := numbered(40)
	updated := replaceLine(old, 20, "CHANGED")
	term := wideFake()
	a := newDiffApp(t, old, updated, term)

	a.handle(key('X'))
	if countFolds(a) != 0 {
		t.Fatal("X should have opened every fold")
	}

	term.size = terminal.Size{Width: 60, Height: 11}
	a.resize()

	if countFolds(a) != 0 {
		t.Fatalf("resize re-folded the document: %v", rowKinds(a))
	}
	if len(a.diffRows) != len(a.rendered.Lines) {
		t.Fatal("row mapping fell out of step with the rendered lines after resize")
	}
}

// Reload re-reads both files, so a change on disk to either side shows up.
func TestReloadPicksUpBothFiles(t *testing.T) {
	a := newDiffApp(t, "one\ntwo\n", "one\ntwo\n", wideFake())
	if a.diffSummary() != "identical" {
		t.Fatal("files should start identical")
	}

	if err := os.WriteFile(a.compare.Path, []byte("one\nCHANGED\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := a.reload(); err != nil {
		t.Fatalf("reload: %v", err)
	}

	if a.diffSummary() == "identical" {
		t.Error("reload did not pick up the edited file")
	}
}

// v opens the new file: that is the side whose line numbers the status bar and
// the row mapping report.
func TestEditTargetIsTheNewFile(t *testing.T) {
	a := newDiffApp(t, "one\n", "ONE\n", wideFake())
	if got := a.editTarget(); got != a.compare.Path {
		t.Errorf("editTarget = %q, want the new file %q", got, a.compare.Path)
	}

	plain := newApp(t, "hello\n", newFake())
	if got := plain.editTarget(); got != plain.src.Path {
		t.Errorf("viewer editTarget = %q, want %q", got, plain.src.Path)
	}
}

func TestSearchWorksInDiffMode(t *testing.T) {
	old := numbered(40)
	updated := replaceLine(old, 20, "NEEDLE")
	a := newDiffApp(t, old, updated, wideFake())

	a.handle(key('/'))
	for _, r := range "NEEDLE" {
		a.handle(key(r))
	}
	if len(a.matches) == 0 {
		t.Fatal("search found nothing in a diff")
	}
	a.handle(press(terminal.KeyEnter))
	if a.mode != modeNormal {
		t.Error("enter should leave search mode")
	}
}

// Expanding a fold rebuilds the rows the matches point at, so they must be
// recomputed or highlighting would land on the wrong lines.
func TestExpandRefreshesSearchMatches(t *testing.T) {
	old := numbered(40)
	updated := replaceLine(old, 20, "NEEDLE")
	a := newDiffApp(t, old, updated, wideFake())

	a.handle(key('/'))
	for _, r := range "line" {
		a.handle(key(r))
	}
	a.handle(press(terminal.KeyEnter))
	before := len(a.matches)

	a.handle(key('X'))

	if len(a.matches) <= before {
		t.Errorf("after expanding, matches = %d, want more than %d", len(a.matches), before)
	}
	for _, m := range a.matches {
		if m.Line >= len(a.rendered.Lines) {
			t.Fatalf("match points at line %d, beyond the %d rendered", m.Line, len(a.rendered.Lines))
		}
	}
}

// Diff mode accepts files the Markdown viewer would refuse.
func TestDiffAcceptsNonMarkdownFiles(t *testing.T) {
	dir := t.TempDir()
	oldPath := filepath.Join(dir, "a.go")
	newPath := filepath.Join(dir, "b.go")
	if err := os.WriteFile(oldPath, []byte("package a\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(newPath, []byte("package b\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	a := &App{
		cfg: Config{Path: oldPath, Compare: newPath, Theme: style.ThemeDark,
			Context: diffdoc.DefaultContext, SideBySide: true},
		term:   wideFake(),
		styler: style.New(style.ThemeDark, false),
		env:    func(string) string { return "" },
		active: -1,
	}
	if err := a.load(); err != nil {
		t.Fatalf("diff mode should accept .go files: %v", err)
	}
}

func TestToggleLineNumbersInDiffMode(t *testing.T) {
	old := numbered(40)
	updated := replaceLine(old, 20, "CHANGED")
	a := newDiffApp(t, old, updated, wideFake(), func(c *Config) { c.LineNumbers = false })

	joined := func() string {
		var sb strings.Builder
		for _, line := range a.rendered.Lines {
			sb.WriteString(line.SearchText + "\n")
		}
		return sb.String()
	}

	a.handle(key('l'))
	if !a.cfg.LineNumbers {
		t.Fatal("l did not turn the gutters on")
	}
	// Both panes get their own gutter, so line 20 appears on each side.
	if !strings.Contains(joined(), "20") {
		t.Errorf("no line numbers after toggling:\n%s", joined())
	}
	if len(a.diffRows) != len(a.rendered.Lines) {
		t.Fatal("row mapping fell out of step with the rendered lines")
	}

	a.handle(key('l'))
	if a.cfg.LineNumbers {
		t.Fatal("l did not turn the gutters back off")
	}
	if len(a.diffRows) != len(a.rendered.Lines) {
		t.Fatal("row mapping fell out of step after toggling back")
	}
}

// Toggling re-lays out but must not rebuild the diff, or folds the reader has
// opened would snap shut.
func TestToggleLineNumbersKeepsExpandedFolds(t *testing.T) {
	old := numbered(40)
	updated := replaceLine(old, 20, "CHANGED")
	a := newDiffApp(t, old, updated, wideFake())

	a.handle(key('X'))
	if countFolds(a) != 0 {
		t.Fatal("X should have opened every fold")
	}

	a.handle(key('l'))

	if countFolds(a) != 0 {
		t.Fatalf("toggling re-folded the document: %v", rowKinds(a))
	}
}
