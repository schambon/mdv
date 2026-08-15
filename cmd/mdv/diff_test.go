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

// Flags must work on either side of the subcommand. Go's flag package stops at
// the first non-flag argument, so the form people reach for first —
// `mdv diff --no-split a b` — used to treat --no-split as a filename.
func TestDiffFlagsOnEitherSideOfSubcommand(t *testing.T) {
	tests := []struct {
		name string
		args []string
	}{
		{"before", []string{"--no-split", "-U", "7", "diff", "a", "b"}},
		{"after", []string{"diff", "--no-split", "-U", "7", "a", "b"}},
		{"straddling", []string{"--no-split", "diff", "-U", "7", "a", "b"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg, _, code := parse(t, tt.args...)
			if code != 0 {
				t.Fatalf("code = %d, want 0", code)
			}
			if cfg.SideBySide {
				t.Error("--no-split was not applied")
			}
			if cfg.Context != 7 {
				t.Errorf("Context = %d, want 7", cfg.Context)
			}
			if cfg.Path != "a" || cfg.Compare != "b" {
				t.Errorf("got %q → %q, want a → b", cfg.Path, cfg.Compare)
			}
		})
	}
}

// Common flags keep working after the subcommand too, not just diff-specific
// ones.
func TestCommonFlagsAfterSubcommand(t *testing.T) {
	cfg, _, code := parse(t, "diff", "-l", "-w", "100", "--no-color", "a", "b")
	if code != 0 {
		t.Fatalf("code = %d", code)
	}
	if !cfg.LineNumbers {
		t.Error("-l was not applied")
	}
	if cfg.Width != 100 {
		t.Errorf("Width = %d, want 100", cfg.Width)
	}
	if cfg.Color {
		t.Error("--no-color was not applied")
	}
}

// A later flag wins over an earlier one, whichever side of the subcommand each
// is on, because both passes fill the same options.
func TestFlagAfterSubcommandOverridesBefore(t *testing.T) {
	cfg, _, code := parse(t, "-w", "50", "diff", "-w", "120", "a", "b")
	if code != 0 {
		t.Fatalf("code = %d", code)
	}
	if cfg.Width != 120 {
		t.Errorf("Width = %d, want the later 120", cfg.Width)
	}
}

func TestUnknownFlagAfterSubcommandIsUsageError(t *testing.T) {
	if _, _, code := parse(t, "diff", "--nope", "a", "b"); code != 2 {
		t.Errorf("code = %d, want 2", code)
	}
}
