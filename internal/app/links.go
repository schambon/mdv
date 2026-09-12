package app

import (
	"os/exec"
	"path/filepath"

	"github.com/schambon/mdv/internal/layout"
	"github.com/schambon/mdv/internal/link"
	"github.com/schambon/mdv/internal/source"
	"github.com/schambon/mdv/internal/terminal"
)

// Following links is a viewer-mode feature. Diff mode shows two files named on
// the command line or fetched from a repository, neither of which is a place to
// navigate away from, and it has already claimed < and > for the changed-file
// list — so every entry point here declines when comparing.

// visit is a file the reader has been in, with the source line they left it at,
// so going back returns them to what they were reading rather than to the top.
type visit struct {
	path string
	line int
}

// refreshLinks recollects the links after the rows they refer to have been
// rebuilt. It is the link counterpart of refreshMatches, and is called from the
// same places for the same reason: a layout.Ref is an index into the row list,
// and a relaid-out document has a different one.
func (a *App) refreshLinks() {
	if a.cfg.diffMode() {
		a.links, a.activeLink = nil, -1
		return
	}
	a.links = layout.Links(a.rendered)
	a.activeLink = -1
}

// cycleLink moves the selection to the next or previous link, wrapping at
// either end. With nothing selected it starts from what is on screen, so Tab
// picks up where the reader is looking rather than at the top of the file.
func (a *App) cycleLink(delta int) {
	if a.cfg.diffMode() {
		return
	}
	if len(a.links) == 0 {
		a.message = "no links in this document"
		return
	}

	switch {
	case a.activeLink < 0 && delta > 0:
		a.activeLink = a.firstLinkFrom(a.top)
	case a.activeLink < 0:
		a.activeLink = a.lastLinkBefore(a.top)
	default:
		a.activeLink = (a.activeLink + delta + len(a.links)) % len(a.links)
	}
	a.reveal(a.links[a.activeLink].Refs[0].Line)
}

// firstLinkFrom is the first link at or after a row, wrapping to the first link
// in the document when there is none.
func (a *App) firstLinkFrom(row int) int {
	for i, l := range a.links {
		if l.Refs[0].Line >= row {
			return i
		}
	}
	return 0
}

// lastLinkBefore is the last link strictly above a row, wrapping to the final
// link in the document when there is none.
func (a *App) lastLinkBefore(row int) int {
	for i := len(a.links) - 1; i >= 0; i-- {
		if a.links[i].Refs[0].Line < row {
			return i
		}
	}
	return len(a.links) - 1
}

// clearLink drops the selection, which is what Escape does in normal mode.
func (a *App) clearLink() { a.activeLink = -1 }

// openActiveLink follows the selected link, reporting whether there was one.
// Enter falls back to scrolling when it reports false, so the key keeps its
// original meaning for a reader who never pressed Tab.
func (a *App) openActiveLink() bool {
	if a.cfg.diffMode() || a.activeLink < 0 || a.activeLink >= len(a.links) {
		return false
	}
	a.openTarget(a.links[a.activeLink].Target)
	return true
}

// clickLink follows the link under a mouse click, if there is one. A click
// anywhere else is ignored rather than treated as a scroll: the reader asked
// for a particular cell, and moving the viewport is not what they asked for.
func (a *App) clickLink(ev terminal.Event) {
	if a.cfg.diffMode() {
		return
	}
	index, ok := a.linkAt(ev.Col, ev.Row)
	if !ok {
		return
	}
	a.activeLink = index
	a.openTarget(a.links[index].Target)
}

// linkAt maps a 1-based screen cell to the link occupying it. The status row is
// not part of the document, so a click on it finds nothing.
func (a *App) linkAt(col, row int) (int, bool) {
	if col < 1 || row < 1 || row > a.pageHeight() {
		return 0, false
	}
	line := a.top + row - 1
	if line >= len(a.rendered.Lines) {
		return 0, false
	}

	// Walk the row's cells to find the span the click landed in. Cells, not
	// bytes: a wide rune occupies two columns and a span several.
	used := 0
	span := -1
	for i, s := range a.rendered.Lines[line].Spans {
		if col <= used+s.Cells {
			span = i
			break
		}
		used += s.Cells
	}
	if span < 0 {
		return 0, false
	}

	for i, l := range a.links {
		for _, ref := range l.Refs {
			if ref.Line == line && ref.Span == span {
				return i, true
			}
		}
	}
	return 0, false
}

// openTarget follows one link target, however it was chosen. Every failure
// reports through the status line and leaves the view exactly where it was: a
// link that cannot be followed should cost the reader nothing.
func (a *App) openTarget(target string) {
	kind, path := link.Classify(target)
	switch kind {
	case link.KindExternal:
		if err := a.runOpen(target); err != nil {
			a.message = err.Error()
		}
		return

	case link.KindNone:
		a.message = "cannot follow link: " + target
		return
	}

	if path == "" {
		// A bare "#section": mdv has no in-document anchors.
		a.message = "link points inside this document"
		return
	}

	resolved := path
	if !filepath.IsAbs(resolved) {
		// Relative to the document holding the link, not to the working
		// directory: that is what the link means, and after following one the
		// two are no longer the same place.
		resolved = filepath.Join(filepath.Dir(a.src.Path), resolved)
	}

	// Validate before touching any state. It reads no content and reports
	// exactly the two failures this feature has to explain — the file is not
	// there, or it is not Markdown.
	if _, err := source.Validate(resolved); err != nil {
		a.message = err.Error()
		return
	}
	if err := a.visit(resolved, true); err != nil {
		a.message = err.Error()
	}
}

// visit shows a different file. It is the only place outside git mode that
// changes cfg.Path, and it resets the viewport, the search matches and the link
// selection for the reason selectFile documents: a row index measured against
// one document means nothing in another.
//
// push records where the reader came from. Going back and forward replays
// visits that are already on the stacks, so they pass false.
func (a *App) visit(path string, push bool) error {
	from := visit{path: a.src.Path, line: a.sourceLine()}
	previous := a.cfg.Path

	a.cfg.Path = path
	if err := a.load(); err != nil {
		a.cfg.Path = previous // nothing was shown, so nothing changed
		return err
	}

	if push {
		a.back = append(a.back, from)
		// A new destination invalidates whatever the reader had gone back
		// from; keeping it would offer a forward that no longer follows.
		a.forward = nil
	}

	a.render()
	a.top = 0
	a.query, a.matches, a.active = "", nil, -1
	a.refreshLinks()
	return nil
}

// goBack returns to the previous file, at the line it was left at.
func (a *App) goBack() {
	a.step(&a.back, &a.forward, "no earlier file")
}

// goForward undoes a goBack.
func (a *App) goForward() {
	a.step(&a.forward, &a.back, "no later file")
}

// step moves one entry from one history stack to the other. Back and forward
// are the same operation in opposite directions, and writing it once keeps them
// from drifting apart.
func (a *App) step(from, to *[]visit, empty string) {
	if a.cfg.diffMode() {
		return
	}
	if len(*from) == 0 {
		a.message = empty
		return
	}

	target := (*from)[len(*from)-1]
	here := visit{path: a.src.Path, line: a.sourceLine()}
	if err := a.visit(target.path, false); err != nil {
		// The file went away while the reader was elsewhere. Leave the entry
		// in place: retrying is cheap, and silently dropping history would be
		// more confusing than the error.
		a.message = err.Error()
		return
	}

	*from = (*from)[:len(*from)-1]
	*to = append(*to, here)
	a.top = a.clamp(layout.Nearest(a.rendered, target.line))
}

// markActiveLink returns the row with the selected link's spans banded. The
// band goes in Background rather than Style: the span is still a link, and
// being selected is something that happened to it — which also lets search
// highlighting, which rewrites Style, compose with it instead of erasing it.
func (a *App) markActiveLink(index int) layout.RenderedLine {
	line := a.rendered.Lines[index]
	if a.activeLink < 0 || a.activeLink >= len(a.links) {
		return line
	}

	var marked bool
	for _, ref := range a.links[a.activeLink].Refs {
		if ref.Line != index {
			continue
		}
		if !marked {
			// The document is shared by every frame; copy before writing.
			line.Spans = append([]layout.Span(nil), line.Spans...)
			marked = true
		}
		if ref.Span < len(line.Spans) {
			line.Spans[ref.Span].Background = layout.StyleLinkActive
		}
	}
	return line
}

// activeTarget is the selected link's target, or empty when nothing is
// selected. The status line shows it, because a label rarely reveals the path
// it points at.
func (a *App) activeTarget() string {
	if a.activeLink < 0 || a.activeLink >= len(a.links) {
		return ""
	}
	return a.links[a.activeLink].Target
}

// execOpen hands an external URL to the system opener. No shell is involved —
// the argv goes straight to exec.Command, as the editor's does — and
// link.Classify has already established that the target is an http, https or
// mailto URL free of control characters, so it cannot be read as a flag. The
// absolute path is used rather than a PATH lookup, for the same reason.
func execOpen(target string) error {
	return exec.Command("/usr/bin/open", target).Run()
}
