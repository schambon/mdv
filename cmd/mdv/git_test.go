package main

import (
	"strings"
	"testing"
)

func TestParseGitCommand(t *testing.T) {
	cfg, done, code := parse(t, "git")
	if code != 0 || done {
		t.Fatalf("code = %d, done = %v", code, done)
	}
	if cfg.Git == nil {
		t.Fatal("Git should be set")
	}
	if len(cfg.Git.Args) != 0 || cfg.Git.Staged {
		t.Errorf("Git = %+v, want no operands and no --staged", cfg.Git)
	}
	// Git mode is a diff, so the diff defaults must apply to it too.
	if !cfg.SideBySide || !cfg.WordDiff {
		t.Errorf("cfg = %+v, want the diff defaults", cfg)
	}
}

// A revision cannot be told from a path without asking the repository, so the
// operands are passed through as typed and resolved there.
func TestParseGitOperands(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want []string
	}{
		{"revision", []string{"git", "HEAD~3"}, []string{"HEAD~3"}},
		{"path", []string{"git", "README.md"}, []string{"README.md"}},
		{"range", []string{"git", "v1..v2"}, []string{"v1..v2"}},
		{"both", []string{"git", "HEAD~3", "README.md"}, []string{"HEAD~3", "README.md"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg, _, code := parse(t, tt.args...)
			if code != 0 {
				t.Fatalf("code = %d", code)
			}
			if strings.Join(cfg.Git.Args, " ") != strings.Join(tt.want, " ") {
				t.Errorf("Args = %q, want %q", cfg.Git.Args, tt.want)
			}
		})
	}
}

func TestParseGitStaged(t *testing.T) {
	for _, flag := range []string{"--staged", "--cached"} {
		cfg, _, code := parse(t, "git", flag)
		if code != 0 {
			t.Fatalf("%s: code = %d", flag, code)
		}
		if !cfg.Git.Staged {
			t.Errorf("%s did not set Staged", flag)
		}
	}
}

// Flags must work on either side of the subcommand, as they do for diff.
func TestGitFlagsOnEitherSideOfSubcommand(t *testing.T) {
	for _, args := range [][]string{
		{"--staged", "--no-split", "git", "README.md"},
		{"git", "--staged", "--no-split", "README.md"},
	} {
		cfg, _, code := parse(t, args...)
		if code != 0 {
			t.Fatalf("%q: code = %d", args, code)
		}
		if !cfg.Git.Staged || cfg.SideBySide {
			t.Errorf("%q: Staged = %v, SideBySide = %v", args, cfg.Git.Staged, cfg.SideBySide)
		}
	}
}

func TestGitArgumentErrors(t *testing.T) {
	tests := []struct {
		name string
		args []string
	}{
		{"three operands", []string{"git", "a", "b", "c"}},
		{"unknown flag", []string{"git", "--no-such-flag"}},
		{"negative context", []string{"git", "-U", "-1"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, _, code := parse(t, tt.args...); code != 2 {
				t.Errorf("code = %d, want 2", code)
			}
		})
	}
}

// The viewer and diff forms are untouched by git mode existing.
func TestGitDoesNotAffectTheOtherForms(t *testing.T) {
	cfg, _, code := parse(t, "notes.md")
	if code != 0 || cfg.Git != nil {
		t.Errorf("viewer form: code = %d, Git = %+v", code, cfg.Git)
	}
	cfg, _, code = parse(t, "diff", "a", "b")
	if code != 0 || cfg.Git != nil {
		t.Errorf("diff form: code = %d, Git = %+v", code, cfg.Git)
	}
}
