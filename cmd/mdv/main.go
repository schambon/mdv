// Command mdv is an interactive Markdown viewer for macOS terminals.
package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/schambon/mdv/internal/app"
	"github.com/schambon/mdv/internal/diffdoc"
	"github.com/schambon/mdv/internal/style"
)

// version is the reported release. It is a variable so a build can stamp it
// with -ldflags "-X main.version=...".
var version = "0.1.0"

// Exit codes: 2 for a usage error, 1 for a runtime failure, 0 for success.
const (
	exitOK    = 0
	exitError = 1
	exitUsage = 2
)

const usage = `mdv [options] FILE
mdv [options] diff OLD NEW

An interactive Markdown viewer. FILE must be a .md or .markdown file.
The diff form compares two files of any type side by side.
Press h inside the viewer for the key bindings.

Options:
`

// diffCommand is the subcommand that selects diff mode. It is matched as a
// positional argument rather than a real subcommand so the existing single-file
// form and all of its flags keep working unchanged.
const diffCommand = "diff"

// errUsage marks a failure that should exit with the usage status.
var errUsage = errors.New("usage")

func main() {
	code := runMain(os.Args[1:], os.Stdout, os.Stderr)
	os.Exit(code)
}

// runMain is main with its environment injected, so it can be tested.
func runMain(args []string, stdout, stderr io.Writer) int {
	cfg, done, err := parseArgs(args, stdout, stderr)
	switch {
	case errors.Is(err, errUsage):
		return exitUsage
	case err != nil:
		fmt.Fprintf(stderr, "mdv: %v\n", err)
		return exitError
	case done:
		return exitOK
	}

	if err := app.Run(cfg); err != nil {
		fmt.Fprintf(stderr, "mdv: %v\n", err)
		return exitError
	}
	return exitOK
}

// parseArgs validates the command line. done reports that the program has
// already produced its output, as for --version.
func parseArgs(args []string, stdout, stderr io.Writer) (cfg app.Config, done bool, err error) {
	fs := flag.NewFlagSet("mdv", flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.Usage = func() {
		fmt.Fprint(stderr, usage)
		fs.PrintDefaults()
	}

	var (
		width       int
		theme       string
		lineNumbers bool
		noColor     bool
		showVersion bool
		context     int
		noFold      bool
		noSplit     bool
		noWordDiff  bool
	)
	fs.IntVar(&width, "w", 0, "maximum rendering `width`; 0 uses the terminal width")
	fs.IntVar(&width, "width", 0, "maximum rendering `width`; 0 uses the terminal width")
	fs.StringVar(&theme, "s", "auto", "colour `style`: auto, dark, or light")
	fs.StringVar(&theme, "style", "auto", "colour `style`: auto, dark, or light")
	fs.BoolVar(&lineNumbers, "l", false, "show source line numbers")
	fs.BoolVar(&lineNumbers, "line-numbers", false, "show source line numbers")
	fs.BoolVar(&noColor, "no-color", false, "disable colour output")
	fs.BoolVar(&showVersion, "V", false, "print the version and exit")
	fs.BoolVar(&showVersion, "version", false, "print the version and exit")
	fs.IntVar(&context, "U", diffdoc.DefaultContext, "diff: unchanged `lines` kept around a change")
	fs.IntVar(&context, "context", diffdoc.DefaultContext, "diff: unchanged `lines` kept around a change")
	fs.BoolVar(&noFold, "no-fold", false, "diff: show all unchanged lines instead of folding them")
	fs.BoolVar(&noSplit, "no-split", false, "diff: use one column instead of two panes")
	fs.BoolVar(&noWordDiff, "no-word-diff", false, "diff: do not highlight changes within a line")

	if err := fs.Parse(args); err != nil {
		return cfg, false, errUsage // flag has already reported the problem
	}

	if showVersion {
		fmt.Fprintf(stdout, "mdv %s\n", version)
		return cfg, true, nil
	}

	if width < 0 {
		fmt.Fprintf(stderr, "mdv: width must not be negative\n")
		return cfg, false, errUsage
	}

	resolved, ok := parseTheme(theme)
	if !ok {
		fmt.Fprintf(stderr, "mdv: unknown style %q (want auto, dark, or light)\n", theme)
		return cfg, false, errUsage
	}

	if context < 0 {
		fmt.Fprintf(stderr, "mdv: context must not be negative\n")
		return cfg, false, errUsage
	}
	// Folding is switched off internally by a negative context, but the flag
	// is the honest way to ask for it.
	if noFold {
		context = -1
	}

	cfg = app.Config{
		Width:       width,
		Theme:       resolved,
		LineNumbers: lineNumbers,
		Color:       !noColor,
		Context:     context,
		SideBySide:  !noSplit,
		WordDiff:    !noWordDiff,
	}

	rest := fs.Args()
	if len(rest) > 0 && rest[0] == diffCommand {
		paths := rest[1:]
		if len(paths) != 2 {
			fmt.Fprintf(stderr, "mdv: diff needs exactly two files\n")
			return cfg, false, errUsage
		}
		cfg.Path, cfg.Compare = paths[0], paths[1]
		return cfg, false, nil
	}

	switch len(rest) {
	case 1:
	case 0:
		fmt.Fprintf(stderr, "mdv: no file given\n")
		fs.Usage()
		return cfg, false, errUsage
	default:
		fmt.Fprintf(stderr, "mdv: only one file may be given\n")
		return cfg, false, errUsage
	}

	cfg.Path = rest[0]
	return cfg, false, nil
}

func parseTheme(name string) (style.Theme, bool) {
	switch style.Theme(name) {
	case style.ThemeAuto, style.ThemeDark, style.ThemeLight:
		return style.Theme(name), true
	}
	return "", false
}
