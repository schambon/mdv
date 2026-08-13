package main

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/schambon/mdv/internal/app"
	"github.com/schambon/mdv/internal/style"
)

func parse(t *testing.T, args ...string) (cfg app.Config, done bool, code int) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	c, d, err := parseArgs(args, &stdout, &stderr)

	switch {
	case errors.Is(err, errUsage):
		return app.Config{}, false, 2
	case err != nil:
		return app.Config{}, false, 1
	}
	return c, d, 0
}

func TestParseMinimalArguments(t *testing.T) {
	cfg, done, code := parse(t, "notes.md")
	if code != 0 || done {
		t.Fatalf("code = %d, done = %v", code, done)
	}
	if cfg.Path != "notes.md" {
		t.Errorf("Path = %q", cfg.Path)
	}
	if cfg.Theme != style.ThemeAuto {
		t.Errorf("Theme = %q, want auto", cfg.Theme)
	}
	if !cfg.Color {
		t.Error("colour should be on by default")
	}
	if cfg.Width != 0 {
		t.Errorf("Width = %d, want 0", cfg.Width)
	}
}

func TestParseFlags(t *testing.T) {
	tests := []struct {
		name  string
		args  []string
		check func(app.Config) string
	}{
		{"short width", []string{"-w", "60", "a.md"}, func(c app.Config) string {
			if c.Width != 60 {
				return "width not applied"
			}
			return ""
		}},
		{"long width", []string{"--width", "60", "a.md"}, func(c app.Config) string {
			if c.Width != 60 {
				return "width not applied"
			}
			return ""
		}},
		{"short style", []string{"-s", "dark", "a.md"}, func(c app.Config) string {
			if c.Theme != style.ThemeDark {
				return "theme not applied"
			}
			return ""
		}},
		{"long style", []string{"--style", "light", "a.md"}, func(c app.Config) string {
			if c.Theme != style.ThemeLight {
				return "theme not applied"
			}
			return ""
		}},
		{"short line numbers", []string{"-l", "a.md"}, func(c app.Config) string {
			if !c.LineNumbers {
				return "line numbers not enabled"
			}
			return ""
		}},
		{"long line numbers", []string{"--line-numbers", "a.md"}, func(c app.Config) string {
			if !c.LineNumbers {
				return "line numbers not enabled"
			}
			return ""
		}},
		{"no colour", []string{"--no-color", "a.md"}, func(c app.Config) string {
			if c.Color {
				return "colour not disabled"
			}
			return ""
		}},
		{"flags after the file", []string{"-l", "a.md"}, func(c app.Config) string {
			if c.Path != "a.md" {
				return "path not found"
			}
			return ""
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg, _, code := parse(t, tt.args...)
			if code != 0 {
				t.Fatalf("exit code = %d, want 0", code)
			}
			if msg := tt.check(cfg); msg != "" {
				t.Error(msg)
			}
		})
	}
}

func TestUsageErrors(t *testing.T) {
	tests := []struct {
		name string
		args []string
	}{
		{"no file", nil},
		{"two files", []string{"a.md", "b.md"}},
		{"negative width", []string{"-w", "-1", "a.md"}},
		{"unknown style", []string{"-s", "solarized", "a.md"}},
		{"unknown flag", []string{"--nope", "a.md"}},
		{"non-numeric width", []string{"-w", "wide", "a.md"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, _, code := parse(t, tt.args...); code != 2 {
				t.Errorf("exit code = %d, want 2", code)
			}
		})
	}
}

func TestVersionPrintsAndStops(t *testing.T) {
	var stdout, stderr bytes.Buffer
	_, done, err := parseArgs([]string{"-V"}, &stdout, &stderr)

	if err != nil {
		t.Fatalf("parseArgs: %v", err)
	}
	if !done {
		t.Error("version did not stop the program")
	}
	if !strings.Contains(stdout.String(), version) {
		t.Errorf("stdout = %q, want the version", stdout.String())
	}
}

func TestVersionLongFlag(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if _, done, _ := parseArgs([]string{"--version"}, &stdout, &stderr); !done {
		t.Error("--version did not stop the program")
	}
}

// -V wins even with a file, and does not require one.
func TestVersionBeforeValidation(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if _, done, err := parseArgs([]string{"-V", "-w", "-5"}, &stdout, &stderr); !done || err != nil {
		t.Errorf("done = %v, err = %v, want the version to short-circuit", done, err)
	}
}

func TestHelpExitsWithUsageStatus(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := runMain([]string{"-h"}, &stdout, &stderr); code != 2 {
		t.Errorf("exit code = %d, want 2", code)
	}
	if !strings.Contains(stderr.String(), "mdv [options] FILE") {
		t.Errorf("stderr = %q, want the usage text", stderr.String())
	}
}

func TestNoFilePrintsUsage(t *testing.T) {
	var stdout, stderr bytes.Buffer
	runMain(nil, &stdout, &stderr)
	if !strings.Contains(stderr.String(), "no file given") {
		t.Errorf("stderr = %q", stderr.String())
	}
}

// A runtime failure, such as an unopenable file, exits 1 rather than 2.
func TestRuntimeErrorExitsOne(t *testing.T) {
	var stdout, stderr bytes.Buffer
	missing := filepath.Join(t.TempDir(), "absent.md")

	if code := runMain([]string{missing}, &stdout, &stderr); code != 1 {
		t.Errorf("exit code = %d, want 1", code)
	}
	if !strings.HasPrefix(stderr.String(), "mdv: ") {
		t.Errorf("stderr = %q, want an mdv-prefixed error", stderr.String())
	}
}

// Under `go test` stdout is not a terminal, so the viewer must refuse to run.
func TestNonInteractiveOutputIsRejected(t *testing.T) {
	path := filepath.Join(t.TempDir(), "a.md")
	if err := os.WriteFile(path, []byte("# Title\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	var stdout, stderr bytes.Buffer
	if code := runMain([]string{path}, &stdout, &stderr); code != 1 {
		t.Errorf("exit code = %d, want 1", code)
	}
	if !strings.Contains(stderr.String(), "not a terminal") {
		t.Errorf("stderr = %q, want a terminal error", stderr.String())
	}
}
