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

// options holds every flag the command line accepts. One set is shared by all
// forms so that a flag means the same thing wherever it appears.
type options struct {
	width       int
	theme       string
	lineNumbers bool
	noColor     bool
	showVersion bool
	context     int
	noFold      bool
	noSplit     bool
	noWordDiff  bool
}

// flagSet builds a parser bound to these options. It is called more than once
// per command line — see parseArgs — and each call rebinds the same fields, so
// a later parse simply continues filling them in.
func (o *options) flagSet(stderr io.Writer) *flag.FlagSet {
	fs := flag.NewFlagSet("mdv", flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.Usage = func() {
		fmt.Fprint(stderr, usage)
		fs.PrintDefaults()
	}

	fs.IntVar(&o.width, "w", o.width, "maximum rendering `width`; 0 uses the terminal width")
	fs.IntVar(&o.width, "width", o.width, "maximum rendering `width`; 0 uses the terminal width")
	fs.StringVar(&o.theme, "s", o.theme, "colour `style`: auto, dark, or light")
	fs.StringVar(&o.theme, "style", o.theme, "colour `style`: auto, dark, or light")
	fs.BoolVar(&o.lineNumbers, "l", o.lineNumbers, "show source line numbers")
	fs.BoolVar(&o.lineNumbers, "line-numbers", o.lineNumbers, "show source line numbers")
	fs.BoolVar(&o.noColor, "no-color", o.noColor, "disable colour output")
	fs.BoolVar(&o.showVersion, "V", o.showVersion, "print the version and exit")
	fs.BoolVar(&o.showVersion, "version", o.showVersion, "print the version and exit")
	fs.IntVar(&o.context, "U", o.context, "diff: unchanged `lines` kept around a change")
	fs.IntVar(&o.context, "context", o.context, "diff: unchanged `lines` kept around a change")
	fs.BoolVar(&o.noFold, "no-fold", o.noFold, "diff: show all unchanged lines instead of folding them")
	fs.BoolVar(&o.noSplit, "no-split", o.noSplit, "diff: use one column instead of two panes")
	fs.BoolVar(&o.noWordDiff, "no-word-diff", o.noWordDiff, "diff: do not highlight changes within a line")
	return fs
}

// isSubcommand reports whether an argument selects a mode rather than naming a
// file.
func isSubcommand(arg string) bool {
	return arg == diffCommand
}

// parseArgs validates the command line. done reports that the program has
// already produced its output, as for --version.
//
// Flags are parsed in two passes because Go's flag package stops at the first
// non-flag argument. The first pass consumes any flags before the subcommand;
// if a subcommand is what stopped it, the second pass consumes the flags after
// it. Both orders therefore work, and `mdv diff --no-split a b` is accepted
// rather than silently treating --no-split as a filename.
func parseArgs(args []string, stdout, stderr io.Writer) (cfg app.Config, done bool, err error) {
	opts := &options{theme: "auto", context: diffdoc.DefaultContext}

	fs := opts.flagSet(stderr)
	if err := fs.Parse(args); err != nil {
		return cfg, false, errUsage // flag has already reported the problem
	}
	rest := fs.Args()

	command := ""
	if len(rest) > 0 && isSubcommand(rest[0]) {
		command = rest[0]
		sub := opts.flagSet(stderr)
		if err := sub.Parse(rest[1:]); err != nil {
			return cfg, false, errUsage
		}
		rest = sub.Args()
	}

	if opts.showVersion {
		fmt.Fprintf(stdout, "mdv %s\n", version)
		return cfg, true, nil
	}

	if opts.width < 0 {
		fmt.Fprintf(stderr, "mdv: width must not be negative\n")
		return cfg, false, errUsage
	}

	resolved, ok := parseTheme(opts.theme)
	if !ok {
		fmt.Fprintf(stderr, "mdv: unknown style %q (want auto, dark, or light)\n", opts.theme)
		return cfg, false, errUsage
	}

	if opts.context < 0 {
		fmt.Fprintf(stderr, "mdv: context must not be negative\n")
		return cfg, false, errUsage
	}
	// Folding is switched off internally by a negative context, but the flag
	// is the honest way to ask for it.
	context := opts.context
	if opts.noFold {
		context = -1
	}

	cfg = app.Config{
		Width:       opts.width,
		Theme:       resolved,
		LineNumbers: opts.lineNumbers,
		Color:       !opts.noColor,
		Context:     context,
		SideBySide:  !opts.noSplit,
		WordDiff:    !opts.noWordDiff,
	}

	if command == diffCommand {
		if len(rest) != 2 {
			fmt.Fprintf(stderr, "mdv: diff needs exactly two files\n")
			return cfg, false, errUsage
		}
		cfg.Path, cfg.Compare = rest[0], rest[1]
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
