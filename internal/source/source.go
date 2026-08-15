// Package source loads the files mdv displays. The viewer opens one Markdown
// file; diff mode compares two files of any type, so the extension check is
// separable from the rest of validation. Standard input is still out of scope.
package source

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// MaxSize is the largest file mdv will open. The whole file is held in memory.
const MaxSize = 32 << 20

// Source is a loaded file. Path is absolute; Name is its basename.
type Source struct {
	Path  string
	Name  string
	Bytes []byte
}

// Validate checks that path names a readable regular Markdown file of
// acceptable size, and returns its absolute form. It reads no content, so
// callers can reject a bad path cheaply and early.
func Validate(path string) (string, error) {
	abs, err := ValidateAny(path)
	if err != nil {
		return "", err
	}
	if !hasMarkdownExtension(abs) {
		return "", fmt.Errorf("%s: not a Markdown file (expected .md or .markdown)", abs)
	}
	return abs, nil
}

// ValidateAny is Validate without the Markdown extension check. Diff mode
// compares whatever two files it is given: refusing to diff a .go file because
// it is not Markdown would be an odd way to enforce a rendering constraint.
func ValidateAny(path string) (string, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", fmt.Errorf("resolve %s: %w", path, err)
	}

	info, err := os.Stat(abs)
	if err != nil {
		return "", err
	}
	if !info.Mode().IsRegular() {
		return "", fmt.Errorf("%s: not a regular file", abs)
	}
	if info.Size() > MaxSize {
		return "", fmt.Errorf("%s: file is %d bytes, limit is %d", abs, info.Size(), MaxSize)
	}

	return abs, nil
}

// Load reads path after validating it. Relative paths are made absolute first.
func Load(path string) (Source, error) {
	return read(path, Validate)
}

// LoadAny reads path without requiring a Markdown extension.
func LoadAny(path string) (Source, error) {
	return read(path, ValidateAny)
}

func read(path string, validate func(string) (string, error)) (Source, error) {
	abs, err := validate(path)
	if err != nil {
		return Source{}, err
	}

	data, err := os.ReadFile(abs)
	if err != nil {
		return Source{}, err
	}

	return Source{Path: abs, Name: filepath.Base(abs), Bytes: data}, nil
}

func hasMarkdownExtension(path string) bool {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".md", ".markdown":
		return true
	}
	return false
}
