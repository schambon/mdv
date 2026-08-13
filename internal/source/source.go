// Package source loads the single Markdown file mdv displays. There is no
// abstraction for stdin or multiple files: both are out of scope.
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

// Load reads path after validating that it names a regular Markdown file of
// acceptable size. Relative paths are made absolute first.
func Load(path string) (Source, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return Source{}, fmt.Errorf("resolve %s: %w", path, err)
	}

	if !hasMarkdownExtension(abs) {
		return Source{}, fmt.Errorf("%s: not a Markdown file (expected .md or .markdown)", abs)
	}

	info, err := os.Stat(abs)
	if err != nil {
		return Source{}, err
	}
	if !info.Mode().IsRegular() {
		return Source{}, fmt.Errorf("%s: not a regular file", abs)
	}
	if info.Size() > MaxSize {
		return Source{}, fmt.Errorf("%s: file is %d bytes, limit is %d", abs, info.Size(), MaxSize)
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
