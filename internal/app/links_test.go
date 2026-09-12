package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/schambon/mdv/internal/layout"
	"github.com/schambon/mdv/internal/terminal"
)

// newLinkApp builds a viewer on doc.md in a directory that also holds
// other.md, deeper/nested.md and notes.txt, so a test can follow a link that
// works and several that must not.
func newLinkApp(t *testing.T, contents string, term *fakeTerminal) *App {
	t.Helper()

	a := newApp(t, contents, term)
	dir := filepath.Dir(a.src.Path)

	write := func(rel, body string) {
		path := filepath.Join(dir, rel)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("other.md", "# Other\n\nthe other document\n\n[home](doc.md)\n")
	write("deeper/nested.md", "# Nested\n\nfurther in\n")
	write("notes.txt", "not markdown\n")
	return a
}

func TestTabSelectsAndCyclesLinks(t *testing.T) {
	a := newLinkApp(t, "[one](other.md) and [two](notes.txt) and [three](deeper/nested.md)\n", newFake())

	if len(a.links) != 3 {
		t.Fatalf("collected %d links, want 3", len(a.links))
	}
	if a.activeLink != -1 {
		t.Fatalf("activeLink = %d before any Tab, want -1", a.activeLink)
	}

	for want := 0; want < 3; want++ {
		a.handle(press(terminal.KeyTab))
		if a.activeLink != want {
			t.Fatalf("after %d tabs activeLink = %d, want %d", want+1, a.activeLink, want)
		}
	}
	// A fourth Tab wraps to the start.
	a.handle(press(terminal.KeyTab))
	if a.activeLink != 0 {
		t.Errorf("Tab past the last link = %d, want it to wrap to 0", a.activeLink)
	}

	a.handle(press(terminal.KeyShiftTab))
	if a.activeLink != 2 {
		t.Errorf("Shift-Tab from the first link = %d, want it to wrap to 2", a.activeLink)
	}
	a.handle(press(terminal.KeyShiftTab))
	if a.activeLink != 1 {
		t.Errorf("Shift-Tab = %d, want 1", a.activeLink)
	}
}

func TestEscapeClearsTheLinkSelection(t *testing.T) {
	a := newLinkApp(t, "[one](other.md)\n", newFake())

	a.handle(press(terminal.KeyTab))
	if a.activeLink != 0 {
		t.Fatalf("activeLink = %d, want 0", a.activeLink)
	}
	a.handle(press(terminal.KeyEscape))
	if a.activeLink != -1 {
		t.Errorf("activeLink after Escape = %d, want -1", a.activeLink)
	}
}

func TestTabWithNoLinksReports(t *testing.T) {
	a := newLinkApp(t, "nothing to follow here\n", newFake())

	a.handle(press(terminal.KeyTab))
	if a.activeLink != -1 {
		t.Errorf("activeLink = %d, want -1", a.activeLink)
	}
	if !strings.Contains(a.status(), "no links") {
		t.Errorf("status = %q, want it to report there are no links", a.status())
	}
}

// The selection has to follow the reader down the document, or Tab from the
// bottom of a long file would jump back to the top.
func TestTabStartsFromTheVisibleRegionAndScrolls(t *testing.T) {
	body := "[first](other.md)\n\n" + numberedLines(40) + "\n[last](other.md)\n"
	a := newLinkApp(t, body, newFake())

	a.top = a.maxTop()
	a.handle(press(terminal.KeyTab))
	if a.activeLink != 1 {
		t.Fatalf("Tab from the last page selected link %d, want the second", a.activeLink)
	}

	// Selecting the first link again must bring it back into view.
	a.handle(press(terminal.KeyTab))
	row := a.links[a.activeLink].Refs[0].Line
	if row < a.top || row >= a.top+a.pageHeight() {
		t.Errorf("selected link is on row %d, outside the viewport [%d,%d)",
			row, a.top, a.top+a.pageHeight())
	}
}

func TestEnterOpensTheSelectedLink(t *testing.T) {
	a := newLinkApp(t, "some text\n\n[one](other.md)\n", newFake())

	a.handle(press(terminal.KeyTab))
	a.handle(press(terminal.KeyEnter))

	if a.src.Name != "other.md" {
		t.Fatalf("src.Name = %q, want other.md", a.src.Name)
	}
	if a.top != 0 {
		t.Errorf("top = %d after following a link, want 0", a.top)
	}
	if a.activeLink != -1 {
		t.Errorf("activeLink = %d in the new document, want -1", a.activeLink)
	}
	// The new document's own links must have been collected.
	if len(a.links) != 1 || a.links[0].Target != "doc.md" {
		t.Errorf("links in the followed file = %v, want one to doc.md", a.links)
	}
}

// Enter keeps its original meaning for a reader who never pressed Tab.
func TestEnterWithoutASelectionStillScrolls(t *testing.T) {
	a := newLinkApp(t, "[one](other.md)\n\n"+numberedLines(20), newFake())

	before := a.top
	a.handle(press(terminal.KeyEnter))
	if a.top != before+1 {
		t.Errorf("top = %d after Enter with no selection, want %d", a.top, before+1)
	}
	if a.src.Name != "doc.md" {
		t.Errorf("src.Name = %q, want Enter not to have followed anything", a.src.Name)
	}
}

func TestFollowingALinkIntoASubdirectory(t *testing.T) {
	a := newLinkApp(t, "[down](deeper/nested.md)\n", newFake())

	a.handle(press(terminal.KeyTab))
	a.handle(press(terminal.KeyEnter))

	if a.src.Name != "nested.md" {
		t.Errorf("src.Name = %q, want nested.md", a.src.Name)
	}
}

// A link is relative to the file holding it, not to the working directory —
// which is why following one twice has to keep working.
func TestRelativeLinksResolveAgainstTheCurrentDocument(t *testing.T) {
	a := newLinkApp(t, "[down](deeper/nested.md)\n", newFake())
	dir := filepath.Dir(a.src.Path)

	if err := os.WriteFile(filepath.Join(dir, "deeper", "nested.md"),
		[]byte("[sibling](also.md)\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "deeper", "also.md"),
		[]byte("# Also\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	a.handle(press(terminal.KeyTab))
	a.handle(press(terminal.KeyEnter))
	a.handle(press(terminal.KeyTab))
	a.handle(press(terminal.KeyEnter))

	if a.src.Name != "also.md" {
		t.Fatalf("src.Name = %q, want also.md", a.src.Name)
	}
	if filepath.Dir(a.src.Path) != filepath.Join(dir, "deeper") {
		t.Errorf("resolved to %q, want it inside deeper/", a.src.Path)
	}
}

func TestUnfollowableTargetsReportAndLeaveTheViewAlone(t *testing.T) {
	tests := []struct {
		name   string
		target string
		want   string
	}{
		{"missing file", "gone.md", "no such file"},
		{"not markdown", "notes.txt", "not a Markdown file"},
		{"unsupported scheme", "file:///etc/passwd", "cannot follow link"},
		{"in-document anchor", "#section", "points inside this document"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			a := newLinkApp(t, "[x]("+tt.target+")\n\n"+numberedLines(20), newFake())

			a.handle(press(terminal.KeyTab))
			// Selecting scrolls the link into view; opening it must not move
			// the viewport again.
			before := a.top
			a.handle(press(terminal.KeyEnter))

			if a.src.Name != "doc.md" {
				t.Errorf("src.Name = %q, want the view to have stayed on doc.md", a.src.Name)
			}
			if a.top != before {
				t.Errorf("top = %d, want the viewport left at %d", a.top, before)
			}
			if !strings.Contains(a.status(), tt.want) {
				t.Errorf("status = %q, want it to mention %q", a.status(), tt.want)
			}
		})
	}
}

func TestExternalLinkGoesToTheOpener(t *testing.T) {
	a := newLinkApp(t, "[site](https://example.com/page)\n", newFake())

	var opened []string
	a.runOpen = func(target string) error {
		opened = append(opened, target)
		return nil
	}

	a.handle(press(terminal.KeyTab))
	a.handle(press(terminal.KeyEnter))

	if len(opened) != 1 || opened[0] != "https://example.com/page" {
		t.Errorf("opener was asked for %v, want the one URL", opened)
	}
	if a.src.Name != "doc.md" {
		t.Errorf("src.Name = %q, want an external link not to change the document", a.src.Name)
	}
}

func TestOpenerFailureReportsInTheStatusLine(t *testing.T) {
	a := newLinkApp(t, "[site](https://example.com)\n", newFake())
	a.runOpen = func(string) error { return os.ErrPermission }

	a.handle(press(terminal.KeyTab))
	a.handle(press(terminal.KeyEnter))

	if !strings.Contains(a.status(), os.ErrPermission.Error()) {
		t.Errorf("status = %q, want the opener's error", a.status())
	}
}

// An unfollowable target must not reach the opener at all.
func TestUnsupportedSchemeNeverReachesTheOpener(t *testing.T) {
	a := newLinkApp(t, "[x](file:///etc/passwd)\n", newFake())

	called := false
	a.runOpen = func(string) error { called = true; return nil }

	a.handle(press(terminal.KeyTab))
	a.handle(press(terminal.KeyEnter))

	if called {
		t.Error("the opener was called for a file: target")
	}
}

func TestBackAndForward(t *testing.T) {
	a := newLinkApp(t, numberedLines(20)+"\n[one](other.md)\n", newFake())

	// Read partway down, so going back has a position to restore.
	a.top = a.maxTop()
	left := a.sourceLine()

	a.handle(press(terminal.KeyTab))
	a.handle(press(terminal.KeyEnter))
	if a.src.Name != "other.md" {
		t.Fatalf("src.Name = %q, want other.md", a.src.Name)
	}

	a.handle(key('<'))
	if a.src.Name != "doc.md" {
		t.Fatalf("src.Name after '<' = %q, want doc.md", a.src.Name)
	}
	if got := a.sourceLine(); got != left {
		t.Errorf("returned to source line %d, want %d", got, left)
	}

	a.handle(key('>'))
	if a.src.Name != "other.md" {
		t.Errorf("src.Name after '>' = %q, want other.md", a.src.Name)
	}
}

func TestBackAndForwardAtTheEndsOfHistory(t *testing.T) {
	a := newLinkApp(t, "[one](other.md)\n", newFake())

	a.handle(key('<'))
	if !strings.Contains(a.status(), "no earlier file") {
		t.Errorf("status = %q, want 'no earlier file'", a.status())
	}
	a.message = ""

	a.handle(key('>'))
	if !strings.Contains(a.status(), "no later file") {
		t.Errorf("status = %q, want 'no later file'", a.status())
	}
	if a.src.Name != "doc.md" {
		t.Errorf("src.Name = %q, want the view unchanged", a.src.Name)
	}
}

// Following a new link from a position reached by going back discards the
// forward history, which no longer follows from where the reader is.
func TestFollowingALinkClearsTheForwardHistory(t *testing.T) {
	a := newLinkApp(t, "[one](other.md) [two](deeper/nested.md)\n", newFake())

	a.handle(press(terminal.KeyTab))
	a.handle(press(terminal.KeyEnter)) // into other.md
	a.handle(key('<'))                 // back to doc.md
	if len(a.forward) != 1 {
		t.Fatalf("forward has %d entries after going back, want 1", len(a.forward))
	}

	a.handle(press(terminal.KeyTab))
	a.handle(press(terminal.KeyTab))
	a.handle(press(terminal.KeyEnter)) // into nested.md instead
	if a.src.Name != "nested.md" {
		t.Fatalf("src.Name = %q, want nested.md", a.src.Name)
	}
	if len(a.forward) != 0 {
		t.Errorf("forward has %d entries, want it cleared", len(a.forward))
	}

	a.handle(key('>'))
	if !strings.Contains(a.status(), "no later file") {
		t.Errorf("status = %q, want 'no later file'", a.status())
	}
}

func TestClickOpensTheLinkUnderThePointer(t *testing.T) {
	a := newLinkApp(t, "[one](other.md)\n", newFake())

	ref := a.links[0].Refs[0]
	col := 1
	for i := 0; i < ref.Span; i++ {
		col += a.rendered.Lines[ref.Line].Spans[i].Cells
	}

	a.handle(click(col, ref.Line-a.top+1))
	if a.src.Name != "other.md" {
		t.Errorf("src.Name = %q, want a click on the label to have followed it", a.src.Name)
	}
}

func TestClickOffALinkDoesNothing(t *testing.T) {
	a := newLinkApp(t, "plain text with no link at all\n\n[one](other.md)\n", newFake())

	before := a.top
	a.handle(click(2, 1)) // the first row, which holds no link
	if a.src.Name != "doc.md" {
		t.Errorf("src.Name = %q, want the click ignored", a.src.Name)
	}
	if a.top != before {
		t.Errorf("top = %d, want a click off a link not to scroll", a.top)
	}
}

// The status row is not part of the document, and neither is a row past its
// end; clicking either must not panic or follow anything.
func TestClickOutsideTheDocumentIsIgnored(t *testing.T) {
	a := newLinkApp(t, "[one](other.md)\n", newFake())

	for _, ev := range []terminal.Event{
		click(1, a.size.Height),  // the status row
		click(1, a.pageHeight()), // past the end of a short document
		click(0, 1),              // left of the first column
		click(1, 0),              // above the first row
		click(10_000, 1),         // right of every span
	} {
		a.handle(ev)
	}
	if a.src.Name != "doc.md" {
		t.Errorf("src.Name = %q, want every out-of-range click ignored", a.src.Name)
	}
}

func TestSelectedLinkIsBandedInTheBackground(t *testing.T) {
	a := newLinkApp(t, "[one](other.md) plain\n", newFake())
	a.handle(press(terminal.KeyTab))

	ref := a.links[0].Refs[0]
	line := a.markActiveLink(ref.Line)
	if got := line.Spans[ref.Span].Background; got != layout.StyleLinkActive {
		t.Errorf("span background = %v, want StyleLinkActive", got)
	}
	if got := line.Spans[ref.Span].Style; got != layout.StyleLink {
		t.Errorf("span style = %v, want it still to be a link", got)
	}
	// The shared document must not have been written through.
	if got := a.rendered.Lines[ref.Line].Spans[ref.Span].Background; got != layout.StyleNone {
		t.Errorf("the rendered document was modified: background = %v", got)
	}
}

// A selected link inside a search match keeps its band: the band is a
// background and the match rewrites the foreground style.
func TestSelectionSurvivesSearchHighlighting(t *testing.T) {
	a := newLinkApp(t, "[findme](other.md)\n", newFake())
	a.handle(press(terminal.KeyTab))

	a.handle(key('/'))
	typeQuery(a, "findme")
	a.handle(press(terminal.KeyEnter))

	ref := a.links[0].Refs[0]
	line := a.highlight(a.markActiveLink(ref.Line), ref.Line)

	var banded bool
	for _, s := range line.Spans {
		if s.Background == layout.StyleLinkActive {
			banded = true
		}
	}
	if !banded {
		t.Error("no span kept the selection band through search highlighting")
	}
}

func TestStatusShowsTheSelectedTarget(t *testing.T) {
	a := newLinkApp(t, "[one](other.md)\n", newFake())

	a.handle(press(terminal.KeyTab))
	if !strings.Contains(a.status(), "other.md") {
		t.Errorf("status = %q, want it to show the target", a.status())
	}

	a.handle(press(terminal.KeyEscape))
	if strings.Contains(a.status(), "→") {
		t.Errorf("status = %q, want the normal readout back", a.status())
	}
}

// A reflow rebuilds the rows, so a Ref measured against the old ones is
// meaningless and the selection is dropped rather than left dangling.
func TestReflowDropsTheSelection(t *testing.T) {
	term := newFake()
	a := newLinkApp(t, "[one](other.md)\n", term)

	a.handle(press(terminal.KeyTab))
	if a.activeLink != 0 {
		t.Fatalf("activeLink = %d, want 0", a.activeLink)
	}

	term.size = terminal.Size{Width: 30, Height: 6}
	a.resize()

	if a.activeLink != -1 {
		t.Errorf("activeLink after a resize = %d, want -1", a.activeLink)
	}
	if len(a.links) != 1 {
		t.Errorf("collected %d links after a resize, want 1", len(a.links))
	}
}

// Reloading keeps the reader in place, and must leave the link list matching
// the rows it was rebuilt from.
func TestReloadRefreshesLinks(t *testing.T) {
	a := newLinkApp(t, "[one](other.md)\n", newFake())

	if err := os.WriteFile(a.src.Path,
		[]byte("[one](other.md) and [two](notes.txt)\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := a.reload(); err != nil {
		t.Fatal(err)
	}
	if len(a.links) != 2 {
		t.Errorf("collected %d links after a reload, want 2", len(a.links))
	}
}

// v opens the file being viewed, which after following a link is the file the
// reader is actually looking at.
func TestEditTargetFollowsTheViewedFile(t *testing.T) {
	a := newLinkApp(t, "[one](other.md)\n", newFake())

	a.handle(press(terminal.KeyTab))
	a.handle(press(terminal.KeyEnter))

	if filepath.Base(a.editTarget()) != "other.md" {
		t.Errorf("editTarget = %q, want other.md", a.editTarget())
	}
}
