package app

import (
	"errors"
	"fmt"

	"github.com/schambon/mdv/internal/diffdoc"
	"github.com/schambon/mdv/internal/git"
	"github.com/schambon/mdv/internal/source"
)

// GitRequest is the `mdv git` command line: the operands as typed, since only
// an open repository can tell a revision from a path.
type GitRequest struct {
	Args   []string
	Staged bool
}

// loadGit lists the changed files and fetches both sides of one of them. Git is
// a content source only: what comes back is two byte slices, and the ordinary
// diff engine does the rest.
//
// Only the file on screen is fetched. The rest of the list is labelled from
// `git diff --numstat`, which costs one command for all of them, so opening a
// repository with fifty changed files does not diff fifty files.
func (a *App) loadGit() error {
	repo, err := git.Open(a.runGit)
	if err != nil {
		return err
	}
	rev, path, err := repo.Resolve(a.cfg.Git.Args)
	if err != nil {
		return err
	}
	spec, err := repo.Spec(rev, a.cfg.Git.Staged)
	if err != nil {
		return err
	}

	files, err := repo.Changed(spec, path)
	if err != nil {
		return err
	}
	if len(files) == 0 {
		return noChanges(spec, path)
	}

	stats := map[string]git.Stat{}
	if len(files) > 1 {
		// One command for the whole list; skipped when there is no list to
		// label.
		if stats, err = repo.Stats(spec, path); err != nil {
			return err
		}
	}

	a.repo, a.spec = repo, spec
	a.files, a.stats = files, stats
	// A reload re-asks git and the list can change under us, so follow the file
	// by name: its position is not stable, and neither are the expanded folds
	// keyed on it.
	a.diffs = nil
	return a.loadFile(indexOf(files, a.currentFile))
}

// loadFile fetches both sides of one changed file and builds its diff.
func (a *App) loadFile(index int) error {
	file := a.files[index]

	oldBytes, newBytes, err := a.repo.Load(a.spec, file)
	if err != nil {
		return err
	}
	old, err := gitSide(a.repo, a.spec.Old, file, oldBytes)
	if err != nil {
		return err
	}
	updated, err := gitSide(a.repo, a.spec.New, file, newBytes)
	if err != nil {
		return err
	}

	a.src, a.compare = old, updated
	// Both sides are the same file, so the Markdown decision is made on its
	// one name. Config normally carries the paths; here they are the repository
	// -relative name, which is all markdownDiff and the extension check want.
	a.cfg.Path, a.cfg.Compare = file.Path, file.Path
	a.current, a.currentFile = index, file.Path

	if kept, ok := a.diffs[index]; ok {
		a.diff = kept // the folds this reader already opened
		return nil
	}
	a.buildDiff()
	return nil
}

// selectFile switches the viewer to another changed file. The diff being left
// is kept, so coming back finds the folds still open; the viewport and the
// search matches are not, since they index rows of a document that is gone.
func (a *App) selectFile(index int) {
	if index < 0 || index >= len(a.files) || index == a.current {
		return
	}
	if a.diffs == nil {
		a.diffs = map[int]diffdoc.Document{}
	}
	a.diffs[a.current] = a.diff

	if err := a.loadFile(index); err != nil {
		a.message = err.Error()
		return
	}
	a.top = 0
	a.matches, a.active = nil, -1
	a.render()
	a.refreshMatches()
}

// indexOf finds a file by name, falling back to the first one. It is how the
// viewer holds its place across a reload, where the list is rebuilt and the
// file that was on screen may have moved or gone.
func indexOf(files []git.File, path string) int {
	for i, f := range files {
		if f.Path == path {
			return i
		}
	}
	return 0
}

// gitSide wraps one end of the comparison, substituting a marker for content
// that must not be written to a terminal in raw mode.
func gitSide(repo *git.Repo, side git.Side, file git.File, data []byte) (source.Source, error) {
	if git.IsBinary(data) {
		data = []byte(fmt.Sprintf("Binary file, %d bytes\n", len(data)))
	}

	// Only a work-tree side exists on disk, and only then can v open it.
	path := ""
	if side.WorkTree {
		path = repo.Path(file.Path)
	}
	return source.FromBytes(side.Name(file.Path), path, data)
}

// noChanges explains an empty file list in the terms the command line used.
func noChanges(spec git.Spec, path string) error {
	where := fmt.Sprintf("between %s and %s", spec.Old.Describe(), spec.New.Describe())
	if path != "" {
		return fmt.Errorf("no changes to %s %s", path, where)
	}
	return fmt.Errorf("no changes %s", where)
}

var errNoWorkTreeFile = errors.New("neither side of this comparison is a file on disk")
