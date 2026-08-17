package app

import (
	"strings"
	"testing"

	"github.com/schambon/mdv/internal/diffdoc"
	"github.com/schambon/mdv/internal/layout"
	"github.com/schambon/mdv/internal/terminal"
)

// threeFiles is a repository with a list worth showing.
func threeFiles(t *testing.T) *fakeRepo {
	t.Helper()
	repo := newRepo(t)
	repo.changes(t,
		change{path: "alpha.txt", staged: "one\n", worktree: "ONE\n"},
		change{path: "beta.txt", staged: "two\n", worktree: "TWO\n"},
		change{path: "gamma.txt", staged: "three\n", worktree: "THREE\n"},
	)
	return repo
}

// frameRows is the visible text of one composed frame, row by row, with the
// escape sequences and the trailing status row dropped.
func frameRows(a *App) []string {
	var rows []string
	sidebar := a.sidebarRows(a.sidebarWidth(), a.pageHeight())

	for row := range a.pageHeight() {
		index := a.top + row
		var line layout.RenderedLine
		if index < len(a.rendered.Lines) {
			line = a.highlight(index)
		}
		if a.sidebarWidth() > 0 {
			line = a.compose(sidebar[row], line)
		}
		var sb strings.Builder
		for _, span := range line.Spans {
			sb.WriteString(span.Text)
		}
		rows = append(rows, sb.String())
	}
	return rows
}

func TestSidebarListsEveryChangedFile(t *testing.T) {
	a := mustGitApp(t, threeFiles(t), GitRequest{}, wideFake())

	if a.sidebarWidth() == 0 {
		t.Fatal("three changed files should get a sidebar on a wide terminal")
	}
	frame := strings.Join(frameRows(a), "\n")
	for _, name := range []string{"alpha.txt", "beta.txt", "gamma.txt"} {
		if !strings.Contains(frame, name) {
			t.Errorf("%s is missing from the sidebar:\n%s", name, frame)
		}
	}
	// The marker, not the highlight, is what makes the selection visible
	// without colour.
	if !strings.Contains(frame, "> M alpha.txt") {
		t.Errorf("the first file should be marked as selected:\n%s", frame)
	}
	// git's own counts, fetched once for the whole list.
	if !strings.Contains(frame, "+1 -1") {
		t.Errorf("numstat counts are missing:\n%s", frame)
	}
}

// One changed file is not a list, so nothing is drawn beside the diff.
func TestSidebarAbsentForOneFile(t *testing.T) {
	repo := newRepo(t)
	repo.changed(t, "notes.txt", "a\n", "b\n")

	a := mustGitApp(t, repo, GitRequest{}, wideFake())

	if a.sidebarWidth() != 0 {
		t.Errorf("sidebarWidth = %d, want none for a single file", a.sidebarWidth())
	}
	if got := a.filePosition(); got != "" {
		t.Errorf("filePosition = %q, want none", got)
	}
}

func TestSidebarSelectsAnotherFile(t *testing.T) {
	a := mustGitApp(t, threeFiles(t), GitRequest{}, wideFake())

	if _, err := a.handleNormalKey(key('>')); err != nil {
		t.Fatal(err)
	}
	if a.current != 1 {
		t.Fatalf("current = %d, want the second file", a.current)
	}
	if !strings.Contains(frameText(a), "TWO") {
		t.Errorf("the second file's contents are not on screen:\n%s", frameText(a))
	}
	if !strings.Contains(a.statusText(), "file 2/3") {
		t.Errorf("status = %q, want the position in the list", a.statusText())
	}

	// The arrows do the same thing: nothing scrolls horizontally, so they are
	// free for the list.
	if _, err := a.handleNormalKey(press(terminal.KeyLeft)); err != nil {
		t.Fatal(err)
	}
	if a.current != 0 {
		t.Errorf("current = %d, want back at the first file", a.current)
	}
}

func TestSidebarStopsAtTheEndsOfTheList(t *testing.T) {
	a := mustGitApp(t, threeFiles(t), GitRequest{}, wideFake())

	if _, err := a.handleNormalKey(key('<')); err != nil {
		t.Fatal(err)
	}
	if a.current != 0 {
		t.Errorf("current = %d, want no movement before the first file", a.current)
	}
	if a.message == "" {
		t.Error("moving off the end of the list should say so")
	}
}

// Switching files must not lose the folds the reader opened, which is the only
// state a rebuilt diff would throw away silently.
func TestSidebarKeepsExpandedFolds(t *testing.T) {
	repo := newRepo(t)
	repo.changes(t,
		change{path: "a.txt", staged: numbered(40), worktree: strings.Replace(numbered(40), "line20", "LINE20", 1)},
		change{path: "b.txt", staged: "one\n", worktree: "two\n"},
	)
	a := mustGitApp(t, repo, GitRequest{}, wideFake())

	before := len(a.diff.Rows)
	a.diff.Rows = diffdoc.ExpandAll(a.diff.Rows)
	a.render()
	expanded := len(a.diff.Rows)
	if expanded <= before {
		t.Fatalf("nothing was folded to begin with: %d rows", before)
	}

	a.selectFile(1)
	a.selectFile(0)

	if got := len(a.diff.Rows); got != expanded {
		t.Errorf("rows = %d after coming back, want the %d the reader had expanded", got, expanded)
	}
}

// Switching files resets the viewport: a row index means nothing in a document
// that was not the one it was measured against.
func TestSidebarResetsTheViewport(t *testing.T) {
	repo := newRepo(t)
	repo.changes(t,
		change{path: "a.txt", staged: numbered(40), worktree: strings.ToUpper(numbered(40))},
		change{path: "b.txt", staged: "one\n", worktree: "two\n"},
	)
	a := mustGitApp(t, repo, GitRequest{}, wideFake())

	a.top = a.maxTop()
	if a.top == 0 {
		t.Fatal("the first file should be longer than one screen")
	}
	a.selectFile(1)

	if a.top != 0 {
		t.Errorf("top = %d, want the top of the newly opened file", a.top)
	}
	if a.active != -1 {
		t.Errorf("active = %d, want no active match", a.active)
	}
}

// The diff is laid out narrower, and the composed row still fits the terminal.
func TestSidebarComposedRowsFitTheTerminal(t *testing.T) {
	term := wideFake()
	a := mustGitApp(t, threeFiles(t), GitRequest{}, term)

	budget := a.renderWidth() - a.sidebarCells()
	for i, line := range a.rendered.Lines {
		if got := lineCells(line); got > budget {
			t.Fatalf("diff row %d is %d cells, over the %d left beside the sidebar", i, got, budget)
		}
	}

	sidebar := a.sidebarRows(a.sidebarWidth(), a.pageHeight())
	for row := range a.pageHeight() {
		var index = a.top + row
		var content layout.RenderedLine
		if index < len(a.rendered.Lines) {
			content = a.rendered.Lines[index]
		}
		if got := lineCells(a.compose(sidebar[row], content)); got > a.renderWidth() {
			t.Errorf("frame row %d is %d cells, over the terminal's %d", row, got, a.renderWidth())
		}
	}
}

// Every sidebar entry is exactly the list's width, or the separator column
// walks about from row to row.
func TestSidebarEntriesAreOneWidth(t *testing.T) {
	repo := newRepo(t)
	repo.changes(t,
		change{path: "a.txt", staged: "1\n", worktree: "2\n"},
		change{path: "deeply/nested/directory/with/a/very/long/name.md", staged: "1\n", worktree: "2\n"},
	)
	a := mustGitApp(t, repo, GitRequest{}, wideFake())

	width := a.sidebarWidth()
	for row, spans := range a.sidebarRows(width, a.pageHeight()) {
		total := 0
		for _, span := range spans {
			total += span.Cells
		}
		if total != width {
			t.Errorf("sidebar row %d is %d cells, want %d", row, total, width)
		}
	}
}

// A path too long for the list keeps its tail: that is the part that names the
// file.
func TestSidebarClipsTheHeadOfALongPath(t *testing.T) {
	got := clipCellsLeft("deeply/nested/directory/name.md", 12)
	if !strings.HasSuffix(got, "name.md") {
		t.Errorf("clipCellsLeft = %q, want the tail of the path", got)
	}
	if cells(got) > 12 {
		t.Errorf("clipCellsLeft = %q, %d cells, over the budget", got, cells(got))
	}
	if !strings.HasPrefix(got, ellipsis) {
		t.Errorf("clipCellsLeft = %q, want the cut marked", got)
	}
}

// Below the fallback width the list is dropped rather than squeezed, and the
// keys still work because the status line still names the position.
func TestSidebarDroppedOnANarrowTerminal(t *testing.T) {
	term := newFake()
	term.size = terminal.Size{Width: 50, Height: 10}
	a := mustGitApp(t, threeFiles(t), GitRequest{}, term)

	if a.sidebarWidth() != 0 {
		t.Fatalf("sidebarWidth = %d, want the list dropped at 50 cells", a.sidebarWidth())
	}
	if !strings.Contains(a.statusText(), "file 1/3") {
		t.Errorf("status = %q, want the list still named", a.statusText())
	}
	if !a.selectRelative(1) {
		t.Error("the file keys must keep working without the sidebar")
	}
	if a.current != 1 {
		t.Errorf("current = %d, want the second file", a.current)
	}
}

// A reload re-asks git, and the list can come back in a different order; the
// file on screen is followed by name.
func TestReloadFollowsTheFileByName(t *testing.T) {
	repo := threeFiles(t)
	a := mustGitApp(t, repo, GitRequest{}, wideFake())

	a.selectFile(2)
	if a.currentFile != "gamma.txt" {
		t.Fatalf("currentFile = %q", a.currentFile)
	}

	// alpha.txt is committed away; gamma.txt moves up the list.
	repo.replies["diff --no-ext-diff --no-textconv --no-renames --name-status -z --"] =
		"M\x00beta.txt\x00M\x00gamma.txt\x00"
	repo.replies["diff --no-ext-diff --no-textconv --no-renames --numstat -z --"] =
		"1\t1\tbeta.txt\x001\t1\tgamma.txt\x00"

	if err := a.reload(); err != nil {
		t.Fatal(err)
	}
	if a.currentFile != "gamma.txt" {
		t.Errorf("currentFile = %q, want the same file after the list changed", a.currentFile)
	}
	if a.current != 1 {
		t.Errorf("current = %d, want it to follow the file to its new position", a.current)
	}
}

// The file that was on screen is gone after a reload, so the viewer falls back
// to the first of what remains rather than to a stale index.
func TestReloadAfterTheFileStoppedChanging(t *testing.T) {
	repo := threeFiles(t)
	a := mustGitApp(t, repo, GitRequest{}, wideFake())
	a.selectFile(1)

	repo.replies["diff --no-ext-diff --no-textconv --no-renames --name-status -z --"] =
		"M\x00alpha.txt\x00M\x00gamma.txt\x00"
	repo.replies["diff --no-ext-diff --no-textconv --no-renames --numstat -z --"] =
		"1\t1\talpha.txt\x001\t1\tgamma.txt\x00"

	if err := a.reload(); err != nil {
		t.Fatal(err)
	}
	if a.currentFile != "alpha.txt" {
		t.Errorf("currentFile = %q, want the first of what still differs", a.currentFile)
	}
}

func TestSidebarScrollsToKeepTheSelectionVisible(t *testing.T) {
	a := mustGitApp(t, threeFiles(t), GitRequest{}, wideFake())

	if got := a.sidebarTop(10); got != 0 {
		t.Errorf("sidebarTop = %d, want no scrolling when the list fits", got)
	}
	a.current = 2
	if got := a.sidebarTop(2); got != 1 {
		t.Errorf("sidebarTop = %d, want the selection on screen", got)
	}
}

func TestHelpMentionsTheFileKeysOnlyWithAList(t *testing.T) {
	repo := newRepo(t)
	repo.changed(t, "notes.txt", "a\n", "b\n")
	if got := mustGitApp(t, repo, GitRequest{}, wideFake()).help(); strings.Contains(got, "file") {
		t.Errorf("help = %q, want no file keys for a single file", got)
	}

	if got := mustGitApp(t, threeFiles(t), GitRequest{}, wideFake()).help(); !strings.Contains(got, "< > file") {
		t.Errorf("help = %q, want the file keys", got)
	}
}

func lineCells(line layout.RenderedLine) int {
	total := 0
	for _, span := range line.Spans {
		total += span.Cells
	}
	return total
}
