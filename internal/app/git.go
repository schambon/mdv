package app

import (
	"errors"
	"fmt"
	"strings"

	"github.com/schambon/mdv/internal/git"
	"github.com/schambon/mdv/internal/source"
)

// GitRequest is the `mdv git` command line: the operands as typed, since only
// an open repository can tell a revision from a path.
type GitRequest struct {
	Args   []string
	Staged bool
}

// loadGit fetches both sides of one changed file from the repository. Git is a
// content source only: what comes back is two byte slices, and the ordinary
// diff engine does the rest.
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
	file, err := onlyFile(files, spec, path)
	if err != nil {
		return err
	}

	oldBytes, newBytes, err := repo.Load(spec, file)
	if err != nil {
		return err
	}
	old, err := gitSide(repo, spec.Old, file, oldBytes)
	if err != nil {
		return err
	}
	updated, err := gitSide(repo, spec.New, file, newBytes)
	if err != nil {
		return err
	}

	a.src, a.compare = old, updated
	// Both sides are the same file, so the Markdown decision is made on its
	// one name. Config normally carries the paths; here they are the repository
	// -relative name, which is all markdownDiff and the extension check want.
	a.cfg.Path, a.cfg.Compare = file.Path, file.Path
	a.buildDiff()
	return nil
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

// onlyFile picks the single file to show. Choosing one of several silently
// would be a guess; the sidebar that makes many files navigable comes later.
func onlyFile(files []git.File, spec git.Spec, path string) (git.File, error) {
	switch len(files) {
	case 1:
		return files[0], nil

	case 0:
		where := fmt.Sprintf("between %s and %s", spec.Old.Describe(), spec.New.Describe())
		if path != "" {
			return git.File{}, fmt.Errorf("no changes to %s %s", path, where)
		}
		return git.File{}, fmt.Errorf("no changes %s", where)

	default:
		names := make([]string, len(files))
		for i, f := range files {
			names[i] = f.Path
		}
		return git.File{}, fmt.Errorf("%d files changed; name one:\n  %s",
			len(files), strings.Join(names, "\n  "))
	}
}

var errNoWorkTreeFile = errors.New("neither side of this comparison is a file on disk")
