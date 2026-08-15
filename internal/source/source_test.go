package source

import (
	"os"
	"path/filepath"
	"testing"
)

func write(t *testing.T, dir, name, contents string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestLoad(t *testing.T) {
	dir := t.TempDir()
	path := write(t, dir, "note.md", "# Title\n")

	src, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if src.Name != "note.md" {
		t.Errorf("Name = %q, want note.md", src.Name)
	}
	if !filepath.IsAbs(src.Path) {
		t.Errorf("Path = %q, want absolute", src.Path)
	}
	if string(src.Bytes) != "# Title\n" {
		t.Errorf("Bytes = %q", src.Bytes)
	}
}

func TestLoadAcceptsUppercaseAndMarkdownExtension(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"a.MD", "b.markdown", "c.MarkDown"} {
		if _, err := Load(write(t, dir, name, "x")); err != nil {
			t.Errorf("Load(%s): %v", name, err)
		}
	}
}

func TestLoadRelativePathBecomesAbsolute(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "rel.md", "x")
	t.Chdir(dir)

	src, err := Load("rel.md")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !filepath.IsAbs(src.Path) {
		t.Errorf("Path = %q, want absolute", src.Path)
	}
}

func TestLoadRejects(t *testing.T) {
	dir := t.TempDir()
	txt := write(t, dir, "notes.txt", "x")
	noExt := write(t, dir, "README", "x")

	big := filepath.Join(dir, "big.md")
	f, err := os.Create(big)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.Truncate(MaxSize + 1); err != nil {
		t.Fatal(err)
	}
	f.Close()

	tests := []struct{ name, path string }{
		{"wrong extension", txt},
		{"no extension", noExt},
		{"directory", dir},
		{"missing file", filepath.Join(dir, "absent.md")},
		{"oversize", big},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := Load(tt.path); err == nil {
				t.Errorf("Load(%s) succeeded, want error", tt.path)
			}
		})
	}
}

// Validate applies the same rules as Load without reading the file.
func TestValidate(t *testing.T) {
	dir := t.TempDir()
	good := write(t, dir, "note.md", "# Title\n")

	abs, err := Validate(good)
	if err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if !filepath.IsAbs(abs) {
		t.Errorf("returned %q, want an absolute path", abs)
	}

	for _, bad := range []string{
		write(t, dir, "notes.txt", "x"),
		dir,
		filepath.Join(dir, "absent.md"),
	} {
		if _, err := Validate(bad); err == nil {
			t.Errorf("Validate(%s) succeeded, want an error", bad)
		}
	}
}

func TestLoadAcceptsExactlyMaxSize(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "max.md")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.Truncate(MaxSize); err != nil {
		t.Fatal(err)
	}
	f.Close()

	if _, err := Load(path); err != nil {
		t.Errorf("Load at exactly MaxSize: %v", err)
	}
}

func TestFromBytes(t *testing.T) {
	src, err := FromBytes("note.md@HEAD", "", []byte("# Title\n"))
	if err != nil {
		t.Fatalf("FromBytes: %v", err)
	}
	if src.Name != "note.md@HEAD" || src.Path != "" {
		t.Errorf("got %q at %q", src.Name, src.Path)
	}
	if string(src.Bytes) != "# Title\n" {
		t.Errorf("Bytes = %q", src.Bytes)
	}
}

// Content that never came from the file system was never stat'd, so the size
// limit has to be enforced here or it is not enforced at all.
func TestFromBytesEnforcesTheSizeLimit(t *testing.T) {
	if _, err := FromBytes("big", "", make([]byte, MaxSize+1)); err == nil {
		t.Fatal("oversized content should be rejected")
	}
	if _, err := FromBytes("ok", "", make([]byte, MaxSize)); err != nil {
		t.Fatalf("content at the limit should be accepted: %v", err)
	}
}
