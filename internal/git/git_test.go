package git

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// fakeGit answers canned output, keyed by the command line joined with spaces
// after the fixed leading options. It records every call, so a test can assert
// on the arguments as well as on the result.
type fakeGit struct {
	root    string
	replies map[string]string
	errs    map[string]error
	calls   [][]string
}

func newFake(t *testing.T) *fakeGit {
	t.Helper()
	// Paths on the command line are relative to the working directory, so the
	// test has to stand inside the repository it is pretending to be in.
	root := t.TempDir()
	t.Chdir(root)
	return &fakeGit{
		root:    root,
		replies: map[string]string{},
		errs:    map[string]error{},
	}
}

// key is the command with the boilerplate stripped, so tests read as the git
// command a person would type.
func key(args []string) string {
	if len(args) > 3 && args[0] == "--no-optional-locks" && args[1] == "-C" {
		args = args[3:]
	}
	return strings.Join(args, " ")
}

func (f *fakeGit) run(args ...string) ([]byte, error) {
	f.calls = append(f.calls, args)
	if args[0] == "rev-parse" && args[1] == "--show-toplevel" {
		return []byte(f.root + "\n"), nil
	}
	k := key(args)
	if err, ok := f.errs[k]; ok {
		return nil, err
	}
	if out, ok := f.replies[k]; ok {
		return []byte(out), nil
	}
	return nil, errors.New("unexpected: git " + k)
}

func (f *fakeGit) open(t *testing.T) *Repo {
	t.Helper()
	repo, err := Open(f.run)
	if err != nil {
		t.Fatal(err)
	}
	return repo
}

func (f *fakeGit) ran(want string) bool {
	for _, call := range f.calls {
		if key(call) == want {
			return true
		}
	}
	return false
}

func TestOpenReportsGitsError(t *testing.T) {
	_, err := Open(func(...string) ([]byte, error) {
		return nil, errors.New("not a git repository (or any of the parent directories): .git")
	})
	if err == nil || !strings.Contains(err.Error(), "not a git repository") {
		t.Fatalf("err = %v, want git's own message", err)
	}
}

func TestOpenRejectsEmptyRoot(t *testing.T) {
	if _, err := Open(func(...string) ([]byte, error) { return []byte("\n"), nil }); err == nil {
		t.Fatal("an empty toplevel should not open a repository")
	}
}

func TestSpec(t *testing.T) {
	f := newFake(t)
	f.replies["merge-base main topic"] = "beef\n"
	repo := f.open(t)

	tests := []struct {
		name     string
		rev      string
		staged   bool
		old, new Side
	}{
		{"default", "", false, Side{}, Side{WorkTree: true}},
		{"staged", "", true, Side{Rev: "HEAD"}, Side{}},
		{"revision", "HEAD~3", false, Side{Rev: "HEAD~3"}, Side{WorkTree: true}},
		{"revision staged", "HEAD~3", true, Side{Rev: "HEAD~3"}, Side{}},
		{"range", "a..b", false, Side{Rev: "a"}, Side{Rev: "b"}},
		{"open range", "..b", false, Side{Rev: "HEAD"}, Side{Rev: "b"}},
		{"merge base", "main...topic", false, Side{Rev: "beef"}, Side{Rev: "topic"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			spec, err := repo.Spec(tt.rev, tt.staged)
			if err != nil {
				t.Fatal(err)
			}
			if spec.Old != tt.old || spec.New != tt.new {
				t.Errorf("got %+v → %+v, want %+v → %+v", spec.Old, spec.New, tt.old, tt.new)
			}
		})
	}
}

func TestSpecRejectsStagedRange(t *testing.T) {
	repo := newFake(t).open(t)
	if _, err := repo.Spec("a..b", true); err == nil {
		t.Fatal("--staged with a range should be an error, not a silent choice")
	}
}

// A revision that git would read as an option must be refused, not passed on.
// There is no shell here, but git parses its own arguments.
func TestSpecRejectsFlagLikeRevision(t *testing.T) {
	repo := newFake(t).open(t)
	if _, err := repo.Spec("--exec=rm -rf /...HEAD", false); err == nil {
		t.Fatal("a revision beginning with - should be rejected")
	}
}

func TestChangedArguments(t *testing.T) {
	tests := []struct {
		name string
		spec Spec
		path string
		want string
	}{
		{
			"work tree",
			Spec{New: Side{WorkTree: true}},
			"",
			"diff --no-ext-diff --no-textconv --no-renames --name-status -z --",
		},
		{
			"staged",
			Spec{Old: Side{Rev: "HEAD"}},
			"a.md",
			"diff --no-ext-diff --no-textconv --no-renames --name-status -z --cached -- a.md",
		},
		{
			"revision against work tree",
			Spec{Old: Side{Rev: "HEAD~2"}, New: Side{WorkTree: true}},
			"",
			"diff --no-ext-diff --no-textconv --no-renames --name-status -z HEAD~2 --",
		},
		{
			"range",
			Spec{Old: Side{Rev: "a"}, New: Side{Rev: "b"}},
			"",
			"diff --no-ext-diff --no-textconv --no-renames --name-status -z a b --",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newFake(t)
			f.replies[tt.want] = ""
			if _, err := f.open(t).Changed(tt.spec, tt.path); err != nil {
				t.Fatalf("Changed: %v", err)
			}
			if !f.ran(tt.want) {
				t.Errorf("ran %q, want %q", key(f.calls[len(f.calls)-1]), tt.want)
			}
		})
	}
}

// The command is rooted at the repository and refuses to run anything the
// repository's own configuration might name.
func TestChangedIsHardened(t *testing.T) {
	f := newFake(t)
	want := "diff --no-ext-diff --no-textconv --no-renames --name-status -z --"
	f.replies[want] = ""
	if _, err := f.open(t).Changed(Spec{New: Side{WorkTree: true}}, ""); err != nil {
		t.Fatal(err)
	}

	last := f.calls[len(f.calls)-1]
	if last[0] != "--no-optional-locks" || last[1] != "-C" || last[2] != f.root {
		t.Errorf("call = %q, want it rooted at the repository with locks off", last)
	}
}

func TestParseNameStatus(t *testing.T) {
	files := parseNameStatus([]byte("M\x00a.md\x00A\x00dir/b.go\x00D\x00c\x00"))
	want := []File{{'M', "a.md"}, {'A', "dir/b.go"}, {'D', "c"}}
	if len(files) != len(want) {
		t.Fatalf("got %+v, want %+v", files, want)
	}
	for i := range want {
		if files[i] != want[i] {
			t.Errorf("[%d] = %+v, want %+v", i, files[i], want[i])
		}
	}
}

func TestParseNameStatusEmpty(t *testing.T) {
	if files := parseNameStatus(nil); len(files) != 0 {
		t.Errorf("got %+v, want none", files)
	}
}

func TestLoadFromRevisions(t *testing.T) {
	f := newFake(t)
	f.replies["show --no-textconv a:x.md"] = "before\n"
	f.replies["show --no-textconv b:x.md"] = "after\n"

	spec := Spec{Old: Side{Rev: "a"}, New: Side{Rev: "b"}}
	old, new, err := f.open(t).Load(spec, File{'M', "x.md"})
	if err != nil {
		t.Fatal(err)
	}
	if string(old) != "before\n" || string(new) != "after\n" {
		t.Errorf("got %q → %q", old, new)
	}
}

// The index is the empty revision: git spells that blob ":path".
func TestLoadFromIndexAndWorkTree(t *testing.T) {
	f := newFake(t)
	f.replies["show --no-textconv :x.md"] = "staged\n"
	if err := os.WriteFile(filepath.Join(f.root, "x.md"), []byte("on disk\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	spec := Spec{Old: Side{}, New: Side{WorkTree: true}}
	old, new, err := f.open(t).Load(spec, File{'M', "x.md"})
	if err != nil {
		t.Fatal(err)
	}
	if string(old) != "staged\n" || string(new) != "on disk\n" {
		t.Errorf("got %q → %q", old, new)
	}
}

// A side where the file does not exist is empty, not an error: that is exactly
// what an added or a deleted file should diff as. Nothing is asked of git, so
// the fake would fail the test if a blob were fetched anyway.
func TestLoadOfAbsentSide(t *testing.T) {
	f := newFake(t)
	f.replies["show --no-textconv HEAD:new.md"] = "added\n"
	f.replies["show --no-textconv HEAD:gone.md"] = "removed\n"
	repo := f.open(t)

	spec := Spec{Old: Side{Rev: "HEAD"}, New: Side{WorkTree: true}}

	old, new, err := repo.Load(spec, File{'A', "new.md"})
	if err != nil || len(old) != 0 {
		t.Errorf("added: old = %q, %v; want empty", old, err)
	}
	if len(new) != 0 {
		// It is not on disk in the fake repository either.
		t.Errorf("added: new = %q", new)
	}

	old, new, err = repo.Load(spec, File{'D', "gone.md"})
	if err != nil || string(old) != "removed\n" {
		t.Errorf("deleted: old = %q, %v; want the blob", old, err)
	}
	if len(new) != 0 {
		t.Errorf("deleted: new = %q, want empty", new)
	}
}

// A work-tree file that is not there reads as empty rather than failing: the
// file may have been removed since git listed it.
func TestLoadOfMissingWorkTreeFile(t *testing.T) {
	f := newFake(t)
	f.replies["show --no-textconv HEAD:x.md"] = "before\n"

	spec := Spec{Old: Side{Rev: "HEAD"}, New: Side{WorkTree: true}}
	_, new, err := f.open(t).Load(spec, File{'M', "x.md"})
	if err != nil || len(new) != 0 {
		t.Errorf("got %q, %v; want empty and no error", new, err)
	}
}

func TestIsBinary(t *testing.T) {
	if IsBinary([]byte("# Title\nplain text\n")) {
		t.Error("text should not be binary")
	}
	if !IsBinary([]byte("PK\x03\x04\x00\x00")) {
		t.Error("a NUL byte should mark the content binary")
	}
	// Only the leading window is examined, as git does.
	long := append(make([]byte, 9000), 0)
	for i := range long[:9000] {
		long[i] = 'a'
	}
	if IsBinary(long) {
		t.Error("a NUL past the window should not count")
	}
}

func TestResolve(t *testing.T) {
	f := newFake(t)
	f.replies["rev-parse --verify --quiet HEAD~2^{commit}"] = "beef\n"
	f.errs["rev-parse --verify --quiet nope^{commit}"] = errors.New("bad revision")
	repo := f.open(t)

	if err := os.WriteFile(filepath.Join(f.root, "a.md"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	onDisk := filepath.Join(f.root, "a.md")

	t.Run("nothing", func(t *testing.T) {
		rev, path, err := repo.Resolve(nil)
		if rev != "" || path != "" || err != nil {
			t.Errorf("got %q %q %v", rev, path, err)
		}
	})

	t.Run("revision alone", func(t *testing.T) {
		rev, path, err := repo.Resolve([]string{"HEAD~2"})
		if rev != "HEAD~2" || path != "" || err != nil {
			t.Errorf("got %q %q %v", rev, path, err)
		}
	})

	t.Run("path alone", func(t *testing.T) {
		rev, path, err := repo.Resolve([]string{onDisk})
		if rev != "" || path != "a.md" || err != nil {
			t.Errorf("got %q %q %v", rev, path, err)
		}
	})

	t.Run("both", func(t *testing.T) {
		rev, path, err := repo.Resolve([]string{"HEAD~2", onDisk})
		if rev != "HEAD~2" || path != "a.md" || err != nil {
			t.Errorf("got %q %q %v", rev, path, err)
		}
	})

	t.Run("neither", func(t *testing.T) {
		f.replies["ls-files -- nope"] = ""
		if _, _, err := repo.Resolve([]string{"nope"}); err == nil {
			t.Error("an operand that is neither should be an error")
		}
	})

	// A file deleted from the working tree is still a file git knows about,
	// and is exactly the one someone would want to look at.
	t.Run("deleted file", func(t *testing.T) {
		f.errs["rev-parse --verify --quiet gone.md^{commit}"] = errors.New("bad revision")
		f.replies["ls-files -- gone.md"] = "gone.md\n"

		rev, path, err := repo.Resolve([]string{"gone.md"})
		if rev != "" || path != "gone.md" || err != nil {
			t.Errorf("got %q %q %v", rev, path, err)
		}
	})

	t.Run("flag", func(t *testing.T) {
		if _, _, err := repo.Resolve([]string{"--output=/tmp/x"}); err == nil {
			t.Error("an operand beginning with - should be rejected")
		}
	})

	t.Run("too many", func(t *testing.T) {
		if _, _, err := repo.Resolve([]string{"a", "b", "c"}); err == nil {
			t.Error("three operands should be an error")
		}
	})
}

func TestResolveRejectsPathOutsideRepository(t *testing.T) {
	f := newFake(t)
	repo := f.open(t)

	outside := filepath.Join(t.TempDir(), "elsewhere.md")
	if err := os.WriteFile(outside, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, _, err := repo.Resolve([]string{outside}); err == nil {
		t.Fatal("a path outside the repository should be rejected")
	}
}

func TestSideNames(t *testing.T) {
	tests := []struct {
		side     Side
		name     string
		describe string
	}{
		{Side{WorkTree: true}, "a.md", "the working tree"},
		{Side{}, "a.md@index", "the index"},
		{Side{Rev: "HEAD~1"}, "a.md@HEAD~1", "HEAD~1"},
	}
	for _, tt := range tests {
		if got := tt.side.Name("a.md"); got != tt.name {
			t.Errorf("Name = %q, want %q", got, tt.name)
		}
		if got := tt.side.Describe(); got != tt.describe {
			t.Errorf("Describe = %q, want %q", got, tt.describe)
		}
	}
}
