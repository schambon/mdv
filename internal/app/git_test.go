package app

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/schambon/mdv/internal/diffdoc"
	"github.com/schambon/mdv/internal/style"
	"github.com/schambon/mdv/internal/terminal"
)

// fakeRepo answers canned git output. Nothing is executed and no repository is
// needed, so the viewer's git mode can be exercised on any machine.
type fakeRepo struct {
	root    string
	replies map[string]string
	errs    map[string]error
}

func newRepo(t *testing.T) *fakeRepo {
	t.Helper()
	// Paths on the command line are relative to the working directory, so the
	// test has to stand inside the repository it is pretending to be in.
	root := t.TempDir()
	t.Chdir(root)
	return &fakeRepo{
		root:    root,
		replies: map[string]string{},
		errs:    map[string]error{},
	}
}

func (f *fakeRepo) runner(args ...string) ([]byte, error) {
	if args[0] == "rev-parse" && args[1] == "--show-toplevel" {
		return []byte(f.root + "\n"), nil
	}
	// Strip the fixed leading options so the keys read like typed commands.
	if len(args) > 3 && args[0] == "--no-optional-locks" {
		args = args[3:]
	}
	k := strings.Join(args, " ")
	if err, ok := f.errs[k]; ok {
		return nil, err
	}
	if out, ok := f.replies[k]; ok {
		return []byte(out), nil
	}
	return nil, errors.New("unexpected: git " + k)
}

// changed declares one changed file for the default `mdv git` comparison, with
// contents for the index side and for the work tree.
func (f *fakeRepo) changed(t *testing.T, path, staged, worktree string) {
	t.Helper()
	f.replies["diff --no-ext-diff --no-textconv --no-renames --name-status -z --"] = "M\x00" + path + "\x00"
	f.replies["show --no-textconv :"+path] = staged
	if err := os.WriteFile(filepath.Join(f.root, path), []byte(worktree), 0o644); err != nil {
		t.Fatal(err)
	}
}

// newGitApp builds a git-mode App without entering the event loop.
func newGitApp(t *testing.T, repo *fakeRepo, req GitRequest, term *fakeTerminal) (*App, error) {
	t.Helper()

	a := &App{
		cfg: Config{
			Theme:      style.ThemeDark,
			Color:      false,
			Context:    diffdoc.DefaultContext,
			SideBySide: true,
			WordDiff:   true,
			Git:        &req,
		},
		term:   term,
		styler: style.New(style.ThemeDark, false),
		env:    func(string) string { return "" },
		runGit: repo.runner,
		active: -1,
	}
	a.runEdit = func([]string) error { return nil }

	if err := a.load(); err != nil {
		return nil, err
	}
	a.size = terminal.Normalize(a.currentSize())
	a.render()
	return a, nil
}

func mustGitApp(t *testing.T, repo *fakeRepo, req GitRequest, term *fakeTerminal) *App {
	t.Helper()
	a, err := newGitApp(t, repo, req, term)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	return a
}

// frameText is every rendered row, for assertions about what is on screen.
func frameText(a *App) string {
	var sb strings.Builder
	for _, line := range a.rendered.Lines {
		sb.WriteString(line.SearchText + "\n")
	}
	return sb.String()
}

func TestGitModeDiffsIndexAgainstWorkTree(t *testing.T) {
	repo := newRepo(t)
	repo.changed(t, "notes.txt", "alpha\nbeta\n", "alpha\nGAMMA\n")

	a := mustGitApp(t, repo, GitRequest{}, wideFake())

	if got := frameText(a); !strings.Contains(got, "GAMMA") {
		t.Errorf("the work-tree line is missing:\n%s", got)
	}
	if got := frameText(a); !strings.Contains(got, "beta") {
		t.Errorf("the index line is missing:\n%s", got)
	}
	if !strings.Contains(a.statusText(), "notes.txt@index → notes.txt") {
		t.Errorf("status = %q, want both sides named", a.statusText())
	}
}

// The file name comes from git, not from the command line, so the Markdown
// decision has to be made on it.
func TestGitModeComparesMarkdownAsMarkdown(t *testing.T) {
	repo := newRepo(t)
	// The same paragraph, rewrapped: as Markdown that is not a change at all.
	repo.changed(t, "notes.md", "one two three\nfour five\n", "one two\nthree four five\n")

	a := mustGitApp(t, repo, GitRequest{}, wideFake())

	if !a.cfg.markdownDiff() {
		t.Fatal("a .md file from git should be compared as Markdown")
	}
	if got := a.diffSummary(); got != "identical" {
		t.Errorf("summary = %q, want identical: a rewrap is not a change", got)
	}
}

func TestGitModeRaw(t *testing.T) {
	repo := newRepo(t)
	repo.changed(t, "notes.md", "one two three\nfour five\n", "one two\nthree four five\n")

	raw := mustGitApp(t, repo, GitRequest{}, wideFake())
	raw.cfg.ForceRaw = true
	raw.buildDiff()

	if got := raw.diffSummary(); got == "identical" {
		t.Error("--raw should see the rewrap as changed lines")
	}
}

func TestGitModeStaged(t *testing.T) {
	repo := newRepo(t)
	repo.replies["diff --no-ext-diff --no-textconv --no-renames --name-status -z --cached --"] = "M\x00a.txt\x00"
	repo.replies["show --no-textconv HEAD:a.txt"] = "committed\n"
	repo.replies["show --no-textconv :a.txt"] = "staged\n"

	a := mustGitApp(t, repo, GitRequest{Staged: true}, wideFake())

	if !strings.Contains(a.statusText(), "a.txt@HEAD → a.txt@index") {
		t.Errorf("status = %q, want HEAD against the index", a.statusText())
	}
}

func TestGitModeRevisionRange(t *testing.T) {
	repo := newRepo(t)
	repo.replies["rev-parse --verify --quiet v1..v2^{commit}"] = "" // not consulted for a range
	repo.replies["rev-parse --verify --quiet v1^{commit}"] = "1111\n"
	repo.replies["rev-parse --verify --quiet v2^{commit}"] = "2222\n"
	repo.replies["diff --no-ext-diff --no-textconv --no-renames --name-status -z v1 v2 --"] = "M\x00a.txt\x00"
	repo.replies["show --no-textconv v1:a.txt"] = "old\n"
	repo.replies["show --no-textconv v2:a.txt"] = "new\n"

	a := mustGitApp(t, repo, GitRequest{Args: []string{"v1..v2"}}, wideFake())

	if !strings.Contains(a.statusText(), "a.txt@v1 → a.txt@v2") {
		t.Errorf("status = %q", a.statusText())
	}
	// Neither side is a file, so v has nothing to open and must say so rather
	// than run an editor on a path that does not exist.
	if got := a.editTarget(); got != "" {
		t.Errorf("editTarget = %q, want none", got)
	}
	if err := a.edit(); err != nil {
		t.Fatal(err)
	}
	if a.message != errNoWorkTreeFile.Error() {
		t.Errorf("message = %q, want %q", a.message, errNoWorkTreeFile)
	}
}

// The work-tree side is a real file, so v opens it.
func TestGitModeEditsTheWorkTreeFile(t *testing.T) {
	repo := newRepo(t)
	repo.changed(t, "notes.txt", "a\n", "b\n")

	a := mustGitApp(t, repo, GitRequest{}, wideFake())

	if want := filepath.Join(repo.root, "notes.txt"); a.editTarget() != want {
		t.Errorf("editTarget = %q, want %q", a.editTarget(), want)
	}
}

func TestGitModeAddedFile(t *testing.T) {
	repo := newRepo(t)
	repo.replies["diff --no-ext-diff --no-textconv --no-renames --name-status -z --"] = "A\x00new.txt\x00"
	if err := os.WriteFile(filepath.Join(repo.root, "new.txt"), []byte("fresh\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	a := mustGitApp(t, repo, GitRequest{}, wideFake())

	if len(a.src.Bytes) != 0 {
		t.Errorf("old side = %q, want empty for an added file", a.src.Bytes)
	}
	if string(a.compare.Bytes) != "fresh\n" {
		t.Errorf("new side = %q", a.compare.Bytes)
	}
}

func TestGitModeDeletedFile(t *testing.T) {
	repo := newRepo(t)
	repo.replies["diff --no-ext-diff --no-textconv --no-renames --name-status -z --"] = "D\x00gone.txt\x00"
	repo.replies["show --no-textconv :gone.txt"] = "was here\n"

	a := mustGitApp(t, repo, GitRequest{}, wideFake())

	if string(a.src.Bytes) != "was here\n" {
		t.Errorf("old side = %q", a.src.Bytes)
	}
	if len(a.compare.Bytes) != 0 {
		t.Errorf("new side = %q, want empty for a deleted file", a.compare.Bytes)
	}
}

// Binary content is described, never rendered: the bytes would go to a
// terminal in raw mode, where escape sequences are not recoverable.
func TestGitModeBinaryFile(t *testing.T) {
	repo := newRepo(t)
	repo.changed(t, "logo.png", "\x89PNG\x00\x01\x02", "\x89PNG\x00\x03\x04")

	a := mustGitApp(t, repo, GitRequest{}, wideFake())

	for _, side := range []string{string(a.src.Bytes), string(a.compare.Bytes)} {
		if !strings.HasPrefix(side, "Binary file, ") {
			t.Errorf("side = %q, want a marker", side)
		}
		if strings.ContainsRune(side, 0) {
			t.Errorf("side = %q, still carries raw bytes", side)
		}
	}
}

func TestGitModeErrors(t *testing.T) {
	tests := []struct {
		name  string
		setup func(*fakeRepo)
		req   GitRequest
		want  string
	}{
		{
			// An empty toplevel is what a runner that found no repository
			// returns; the real git also writes a message to stderr.
			name:  "not a repository",
			setup: func(f *fakeRepo) { f.root = "" },
			want:  "not a git repository",
		},
		{
			name: "no changes",
			setup: func(f *fakeRepo) {
				f.replies["diff --no-ext-diff --no-textconv --no-renames --name-status -z --"] = ""
			},
			want: "no changes",
		},
		{
			name: "several files",
			setup: func(f *fakeRepo) {
				f.replies["diff --no-ext-diff --no-textconv --no-renames --name-status -z --"] =
					"M\x00a.txt\x00M\x00b.txt\x00"
			},
			want: "2 files changed",
		},
		{
			name:  "operand is neither a revision nor a file",
			setup: func(f *fakeRepo) { f.errs["rev-parse --verify --quiet nope^{commit}"] = errors.New("bad") },
			req:   GitRequest{Args: []string{"nope"}},
			want:  "not a revision or a file",
		},
		{
			name:  "flag-like operand",
			setup: func(f *fakeRepo) {},
			req:   GitRequest{Args: []string{"--upload-pack=touch /tmp/x"}},
			want:  "may not begin with",
		},
		{
			name:  "staged with a range",
			setup: func(f *fakeRepo) { f.replies["rev-parse --verify --quiet a^{commit}"] = "1\n" },
			req:   GitRequest{Args: []string{"a..a"}, Staged: true},
			want:  "--staged does not combine",
		},
		{
			name: "git itself fails",
			setup: func(f *fakeRepo) {
				f.errs["diff --no-ext-diff --no-textconv --no-renames --name-status -z --"] =
					errors.New("bad object")
			},
			want: "bad object",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := newRepo(t)
			tt.setup(repo)

			term := wideFake()
			defer term.close()

			_, err := newGitApp(t, repo, tt.req, term)
			if err == nil {
				t.Fatalf("want an error containing %q, got none", tt.want)
			}
			if !strings.Contains(err.Error(), tt.want) {
				t.Errorf("err = %v, want it to mention %q", err, tt.want)
			}
		})
	}
}

// Reloading in git mode asks git again, so r picks up an edit made outside the
// viewer.
func TestGitModeReloadRefetches(t *testing.T) {
	repo := newRepo(t)
	repo.changed(t, "notes.txt", "alpha\n", "alpha\n")

	a := mustGitApp(t, repo, GitRequest{}, wideFake())
	if got := a.diffSummary(); got != "identical" {
		t.Fatalf("summary = %q, want identical to start", got)
	}

	if err := os.WriteFile(filepath.Join(repo.root, "notes.txt"), []byte("changed\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := a.reload(); err != nil {
		t.Fatal(err)
	}
	if got := a.diffSummary(); got == "identical" {
		t.Error("reload should have picked up the new work-tree contents")
	}
}
