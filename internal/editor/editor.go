// Package editor turns $VISUAL or $EDITOR into an argument vector for
// exec.Command. No shell is ever involved: the value is lexed here, and shell
// metacharacters are rejected rather than interpreted.
package editor

import (
	"fmt"
	"path/filepath"
	"strconv"
	"strings"
	"unicode"
)

// shellOperators may not appear outside quotes. They would be meaningful to a
// shell, and mdv does not run one, so accepting them would silently do the
// wrong thing.
const shellOperators = ";|&<>`$(){}\r\n"

// Split lexes a command line into arguments. Backslash escapes the next rune
// inside and outside quotes; matching single or double quotes group text and
// are removed.
func Split(s string) ([]string, error) {
	var (
		args    []string
		current strings.Builder
		started bool // the current argument exists, even if it is empty
		quote   rune
		escaped bool
	)

	for _, r := range s {
		switch {
		case escaped:
			current.WriteRune(r)
			started = true
			escaped = false

		case r == '\\':
			escaped = true

		case quote != 0:
			if r == quote {
				quote = 0
			} else {
				current.WriteRune(r)
			}

		case r == '\'' || r == '"':
			quote = r
			started = true

		// Checked before whitespace so CR and LF are rejected rather than
		// treated as argument separators.
		case strings.ContainsRune(shellOperators, r):
			return nil, fmt.Errorf("unsupported shell character %q in editor command", r)

		case unicode.IsSpace(r):
			if started {
				args = append(args, current.String())
				current.Reset()
				started = false
			}

		default:
			current.WriteRune(r)
			started = true
		}
	}

	if escaped {
		return nil, fmt.Errorf("editor command ends with a trailing backslash")
	}
	if quote != 0 {
		return nil, fmt.Errorf("editor command has an unterminated %c quote", quote)
	}
	if started {
		args = append(args, current.String())
	}

	// Empty quoted arguments carry no meaning to exec, so they are dropped.
	kept := args[:0]
	for _, a := range args {
		if a != "" {
			kept = append(kept, a)
		}
	}
	if len(kept) == 0 {
		return nil, fmt.Errorf("editor command is empty")
	}
	return kept, nil
}

// Command builds the argument vector that opens file at line. It prefers
// $VISUAL, then $EDITOR, then vi. lookup is usually os.Getenv.
func Command(lookup func(string) string, file string, line int) ([]string, error) {
	command := firstNonEmpty(lookup("VISUAL"), lookup("EDITOR"), "vi")

	args, err := Split(command)
	if err != nil {
		return nil, err
	}
	return append(args, positionArgs(args[0], file, line)...), nil
}

// positionArgs returns the editor-specific arguments that open a file at a
// line. Editors mdv does not recognize are simply given the filename.
func positionArgs(command, file string, line int) []string {
	if line < 1 {
		line = 1
	}
	at := strconv.Itoa(line)

	switch basename(command) {
	case "vi", "vim", "nvim", "nano":
		return []string{"+" + at, file}
	case "emacs", "emacsclient":
		return []string{"+" + at + ":1", file}
	case "code", "codium":
		return []string{"--goto", file + ":" + at + ":1"}
	case "subl", "mate":
		return []string{file + ":" + at}
	default:
		return []string{file}
	}
}

// basename reduces a command to the executable name used to pick an adapter,
// dropping any directory and extension.
func basename(command string) string {
	name := filepath.Base(command)
	return strings.TrimSuffix(name, filepath.Ext(name))
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}
