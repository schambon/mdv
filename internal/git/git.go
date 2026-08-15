// Package git fetches file contents from a git repository so the diff viewer
// can compare two revisions of a file.
//
// Git is used as a *content source*, not as a diff source: mdv asks git for the
// bytes of each side and runs its own comparison. Parsing git's unified output
// instead would throw away side-by-side layout, folding, expansion beyond the
// context git chose to print, and the word diff.
//
// Everything runs through an injected Runner, so tests need no repository. No
// shell is involved, but a git operand beginning with "-" would still be read
// as a flag, so operands are rejected rather than passed through, and pathspecs
// are separated with "--".
package git

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// Runner executes a git command and returns its standard output. It mirrors
// App.runEdit: the one place the process talks to the outside world, replaced
// wholesale in tests.
type Runner func(args ...string) ([]byte, error)

// Exec is the real Runner.
//
// Output is captured rather than inherited. Unlike the editor, git must never
// be given the terminal: the viewer is in raw mode on the alternate screen, and
// a child writing there would corrupt the frame with no way to repair it.
func Exec(args ...string) ([]byte, error) {
	cmd := exec.Command("git", args...)

	var out, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &stderr
	// A repository we did not write should not be able to make us take locks.
	cmd.Env = append(os.Environ(), "GIT_OPTIONAL_LOCKS=0")

	if err := cmd.Run(); err != nil {
		if errors.Is(err, exec.ErrNotFound) {
			return nil, errors.New("git is not installed or not on PATH")
		}
		if msg := firstLine(stderr.String()); msg != "" {
			return nil, errors.New(msg)
		}
		return nil, err
	}
	return out.Bytes(), nil
}

func firstLine(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i]
	}
	return strings.TrimPrefix(strings.TrimSpace(s), "fatal: ")
}

// Repo is an open repository.
type Repo struct {
	Root string
	run  Runner
}

// Open locates the repository containing the working directory.
func Open(run Runner) (*Repo, error) {
	out, err := run("rev-parse", "--show-toplevel")
	if err != nil {
		return nil, err
	}
	root := strings.TrimSpace(string(out))
	if root == "" {
		return nil, errors.New("not a git repository")
	}
	return &Repo{Root: root, run: run}, nil
}

// git runs a command rooted at the repository, with the options that keep a
// repository's own configuration from running code of its choosing.
//
//   - --no-optional-locks: never write to the index just by looking.
//   - --no-ext-diff, --no-textconv: a .gitattributes can otherwise name an
//     external diff driver or a textconv filter, and mdv would then execute a
//     program it did not choose from a repository it did not write.
func (r *Repo) git(args ...string) ([]byte, error) {
	head := []string{"--no-optional-locks", "-C", r.Root}
	return r.run(append(head, args...)...)
}

// Path is the absolute working-tree path of a repository-relative name.
func (r *Repo) Path(rel string) string { return filepath.Join(r.Root, rel) }

// Side is one end of a comparison: a revision, the index, or the work tree.
type Side struct {
	Rev      string // revision to read the blob from; empty means the index
	WorkTree bool   // read from disk instead
}

// Name labels the side for the status line.
func (s Side) Name(path string) string {
	switch {
	case s.WorkTree:
		return path
	case s.Rev == "":
		return path + "@index"
	default:
		return path + "@" + s.Rev
	}
}

// Describe names the side on its own, for a message that is not about a
// particular file.
func (s Side) Describe() string {
	switch {
	case s.WorkTree:
		return "the working tree"
	case s.Rev == "":
		return "the index"
	default:
		return s.Rev
	}
}

// Spec is a comparison: what to compare against what.
type Spec struct {
	Old Side
	New Side
}

// Spec resolves a revision argument into the two sides to compare, mirroring
// git's own defaults so that a git user is not surprised:
//
//	(none)          index      → work tree   (git diff)
//	--staged        HEAD       → index       (git diff --cached)
//	REV             REV        → work tree   (git diff REV)
//	REV --staged    REV        → index
//	A..B            A          → B
//	A...B           merge-base → B
func (r *Repo) Spec(rev string, staged bool) (Spec, error) {
	if old, new, ok := splitRange(rev, "..."); ok {
		if staged {
			return Spec{}, errors.New("--staged does not combine with a revision range")
		}
		base, err := r.mergeBase(old, new)
		if err != nil {
			return Spec{}, err
		}
		return Spec{Old: Side{Rev: base}, New: Side{Rev: new}}, nil
	}
	if old, new, ok := splitRange(rev, ".."); ok {
		if staged {
			return Spec{}, errors.New("--staged does not combine with a revision range")
		}
		return Spec{Old: Side{Rev: old}, New: Side{Rev: new}}, nil
	}

	switch {
	case staged && rev == "":
		return Spec{Old: Side{Rev: "HEAD"}, New: Side{}}, nil
	case staged:
		return Spec{Old: Side{Rev: rev}, New: Side{}}, nil
	case rev == "":
		return Spec{Old: Side{}, New: Side{WorkTree: true}}, nil
	default:
		return Spec{Old: Side{Rev: rev}, New: Side{WorkTree: true}}, nil
	}
}

// splitRange cuts A<sep>B. An omitted endpoint means HEAD, as it does in git.
func splitRange(rev, sep string) (old, new string, ok bool) {
	i := strings.Index(rev, sep)
	if i < 0 {
		return "", "", false
	}
	old, new = rev[:i], rev[i+len(sep):]
	// "A...B" contains "..", so the two-dot caller must not match it. It is
	// tried second, and a trailing dot left here is what a three-dot range
	// looks like to it.
	if strings.HasPrefix(new, ".") || strings.HasSuffix(old, ".") {
		return "", "", false
	}
	if old == "" {
		old = "HEAD"
	}
	if new == "" {
		new = "HEAD"
	}
	return old, new, true
}

func (r *Repo) mergeBase(a, b string) (string, error) {
	if err := checkOperands(a, b); err != nil {
		return "", err
	}
	out, err := r.git("merge-base", a, b)
	if err != nil {
		return "", err
	}
	base := strings.TrimSpace(string(out))
	if base == "" {
		return "", fmt.Errorf("%s and %s have no common ancestor", a, b)
	}
	return base, nil
}

// diffArgs is the range selector for `git diff` matching the spec.
func (s Spec) diffArgs() []string {
	switch {
	case s.New.Rev != "":
		return []string{s.Old.Rev, s.New.Rev}
	case s.New.WorkTree && s.Old.Rev == "":
		return nil
	case s.New.WorkTree:
		return []string{s.Old.Rev}
	case s.Old.Rev == "HEAD":
		return []string{"--cached"}
	default:
		return []string{"--cached", s.Old.Rev}
	}
}

// File is one changed file, named relative to the repository root.
type File struct {
	Status byte // M, A, D, T ...
	Path   string
}

// Changed lists the files that differ between the two sides, optionally
// restricted to one path.
func (r *Repo) Changed(s Spec, path string) ([]File, error) {
	if s.Old.Rev != "" && s.New.Rev != "" {
		if err := checkOperands(s.Old.Rev, s.New.Rev); err != nil {
			return nil, err
		}
	}

	args := []string{"diff", "--no-ext-diff", "--no-textconv", "--no-renames", "--name-status", "-z"}
	args = append(args, s.diffArgs()...)
	// Always terminate, so a path that looks like a revision or a flag cannot
	// be read as one.
	args = append(args, "--")
	if path != "" {
		args = append(args, path)
	}

	out, err := r.git(args...)
	if err != nil {
		return nil, err
	}
	return parseNameStatus(out), nil
}

// parseNameStatus reads the NUL-separated form, which alternates status and
// path. Renames are switched off, so the records are always pairs.
func parseNameStatus(out []byte) []File {
	fields := strings.Split(strings.TrimSuffix(string(out), "\x00"), "\x00")

	var files []File
	for i := 0; i+1 < len(fields); i += 2 {
		status := strings.TrimSpace(fields[i])
		if status == "" || fields[i+1] == "" {
			continue
		}
		files = append(files, File{Status: status[0], Path: fields[i+1]})
	}
	return files
}

// Load fetches both sides of one file. A file that does not exist on a side —
// added, so absent from the old one; deleted, so absent from the new — comes
// back empty rather than as an error, which is what the diff wants anyway.
func (r *Repo) Load(s Spec, f File) (old, new []byte, err error) {
	if old, err = r.content(s.Old, f, f.Status == 'A'); err != nil {
		return nil, nil, err
	}
	if new, err = r.content(s.New, f, f.Status == 'D'); err != nil {
		return nil, nil, err
	}
	return old, new, nil
}

func (r *Repo) content(s Side, f File, absent bool) ([]byte, error) {
	if absent {
		return nil, nil
	}
	if s.WorkTree {
		data, err := os.ReadFile(r.Path(f.Path))
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return data, err
	}

	// The object name begins with the revision or with ":", never with "-", so
	// it cannot be read as a flag. It is not a pathspec, so no "--" here.
	return r.git("show", "--no-textconv", s.Rev+":"+f.Path)
}

// IsBinary reports whether content should be shown as a marker rather than
// rendered. A NUL byte is git's own test, and it is the one that matters here:
// raw bytes written to a terminal in raw mode are not recoverable.
func IsBinary(data []byte) bool {
	const window = 8000
	if len(data) > window {
		data = data[:window]
	}
	return bytes.IndexByte(data, 0) >= 0
}

// Resolve separates a revision from a path. With one operand the ambiguity is
// real, and an existing file wins: naming a file is the common case, and a
// revision that shares its name with a file is already ambiguous to git.
func (r *Repo) Resolve(args []string) (rev, path string, err error) {
	for _, arg := range args {
		if err := checkOperands(arg); err != nil {
			return "", "", err
		}
	}

	switch len(args) {
	case 0:
		return "", "", nil

	case 1:
		rel, relErr := r.relPath(args[0])
		if _, err := os.Stat(args[0]); relErr == nil && err == nil {
			return "", rel, nil
		}
		if r.isRev(args[0]) {
			return args[0], "", nil
		}
		if relErr != nil {
			return "", "", relErr
		}
		// A file deleted from the working tree is still a file git knows, and
		// is exactly the file someone would want to look at.
		if r.tracked(rel) {
			return "", rel, nil
		}
		return "", "", fmt.Errorf("%s: not a revision or a file", args[0])

	case 2:
		if !r.isRev(args[0]) {
			return "", "", fmt.Errorf("%s: not a revision", args[0])
		}
		path, err := r.relPath(args[1])
		return args[0], path, err

	default:
		return "", "", errors.New("git takes at most a revision and a path")
	}
}

// isRev reports whether s names something git can resolve. A range is checked
// endpoint by endpoint, since rev-parse --verify rejects the range form.
func (r *Repo) isRev(s string) bool {
	if old, new, ok := splitRange(s, "..."); ok {
		return r.verify(old) && r.verify(new)
	}
	if old, new, ok := splitRange(s, ".."); ok {
		return r.verify(old) && r.verify(new)
	}
	return r.verify(s)
}

// tracked reports whether the index holds the path, so that a file which is no
// longer on disk can still be named.
func (r *Repo) tracked(rel string) bool {
	out, err := r.git("ls-files", "--", rel)
	return err == nil && len(strings.TrimSpace(string(out))) > 0
}

func (r *Repo) verify(rev string) bool {
	if checkOperands(rev) != nil {
		return false
	}
	_, err := r.git("rev-parse", "--verify", "--quiet", rev+"^{commit}")
	return err == nil
}

// relPath turns a path on the command line, which is relative to the working
// directory, into the repository-relative form git wants.
func (r *Repo) relPath(path string) (string, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	rel, err := filepath.Rel(r.Root, abs)
	if err != nil {
		return "", err
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("%s: outside the repository", path)
	}
	return filepath.ToSlash(rel), nil
}

// checkOperands rejects arguments git would read as flags. There is no shell,
// but "-i" passed where a revision belongs is still an option, and the caller
// meant it as a name.
func checkOperands(args ...string) error {
	for _, arg := range args {
		if strings.HasPrefix(arg, "-") {
			return fmt.Errorf("%s: a revision or path may not begin with %q", arg, "-")
		}
	}
	return nil
}
