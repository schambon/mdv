package main

import (
	"testing"

	"github.com/schambon/mdv/internal/diffdoc"
)

func TestParseDiffCommand(t *testing.T) {
	cfg, done, code := parse(t, "diff", "old.go", "new.go")
	if code != 0 || done {
		t.Fatalf("code = %d, done = %v", code, done)
	}
	if cfg.Path != "old.go" || cfg.Compare != "new.go" {
		t.Errorf("got %q → %q, want old.go → new.go", cfg.Path, cfg.Compare)
	}
	// Defaults: fold at the conventional window, two panes, word highlighting.
	if cfg.Context != diffdoc.DefaultContext {
		t.Errorf("Context = %d, want %d", cfg.Context, diffdoc.DefaultContext)
	}
	if !cfg.SideBySide {
		t.Error("SideBySide should default on")
	}
	if !cfg.WordDiff {
		t.Error("WordDiff should default on")
	}
}

func TestParseDiffFlags(t *testing.T) {
	cfg, _, code := parse(t, "-U", "7", "--no-split", "--no-word-diff", "diff", "a", "b")
	if code != 0 {
		t.Fatalf("code = %d", code)
	}
	if cfg.Context != 7 {
		t.Errorf("Context = %d, want 7", cfg.Context)
	}
	if cfg.SideBySide {
		t.Error("--no-split should clear SideBySide")
	}
	if cfg.WordDiff {
		t.Error("--no-word-diff should clear WordDiff")
	}
}

// --no-fold is expressed internally as a negative context.
func TestParseNoFold(t *testing.T) {
	cfg, _, code := parse(t, "--no-fold", "diff", "a", "b")
	if code != 0 {
		t.Fatalf("code = %d", code)
	}
	if cfg.Context >= 0 {
		t.Errorf("Context = %d, want a negative value to disable folding", cfg.Context)
	}
}

func TestDiffArgumentErrors(t *testing.T) {
	tests := []struct {
		name string
		args []string
	}{
		{"no files", []string{"diff"}},
		{"one file", []string{"diff", "only.go"}},
		{"three files", []string{"diff", "a", "b", "c"}},
		{"negative context", []string{"-U", "-1", "diff", "a", "b"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, _, code := parse(t, tt.args...); code != 2 {
				t.Errorf("code = %d, want 2", code)
			}
		})
	}
}

// The single-file form must keep working exactly as before, including the
// diff flags being accepted but inert.
func TestViewerModeUnaffectedByDiffFlags(t *testing.T) {
	cfg, _, code := parse(t, "-U", "5", "notes.md")
	if code != 0 {
		t.Fatalf("code = %d", code)
	}
	if cfg.Compare != "" {
		t.Errorf("Compare = %q, want empty for the viewer form", cfg.Compare)
	}
	if cfg.Path != "notes.md" {
		t.Errorf("Path = %q", cfg.Path)
	}
}

// "diff" as the sole argument is read as the subcommand missing its files,
// not as a file named "diff". Someone who typed it almost certainly meant the
// subcommand, and a file called "diff" could not be opened by the viewer
// anyway: it has no .md extension. The cost of the ambiguity is a file that
// the viewer would refuse regardless.
func TestBareDiffIsAMissingArgumentError(t *testing.T) {
	if _, _, code := parse(t, "diff"); code != 2 {
		t.Errorf("code = %d, want 2", code)
	}
}
