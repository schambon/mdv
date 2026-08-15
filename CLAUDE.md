# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Commands

```sh
go build -o mdv ./cmd/mdv    # build
go test ./...                # all tests
go test -race ./...          # the pump, signal handler, and terminal state are concurrent
go test ./internal/layout    # one package
go test ./internal/md -run TestParseTable -v   # one test
./mdv README.md              # run (needs an interactive tty on both stdin and stdout)
```

`go vet ./...` and `gofmt -l .` should both come back clean. The viewer refuses to run when stdout is a pipe, so to see it render under a harness use a pty: `printf 'q' | script -q /dev/null ./mdv README.md | cat -v`.

## Hard constraints

- **Standard library only.** `go.mod` has zero third-party requirements. No Glow, no Bubble Tea, no termbox — file loading, parsing, layout, styling, search, terminal I/O, and editor exec are all owned by this codebase.
- **macOS only.** `terminal_darwin.go` drives termios and `TIOCGWINSZ` through `syscall`; `terminal_other.go` returns an unsupported-platform error. Check cross-platform builds with `GOOS=linux go build ./internal/terminal/`.
- **Bounded Markdown, deliberately.** Not CommonMark, not GFM. Anything outside the subset in `REQUIREMENTS.md` §4 renders as literal text. Resist "fixing" this by adding nesting, escapes, reference links, or a delimiter stack — that is an explicit non-goal.
- Out of scope: stdin, multiple files, file watching, syntax highlighting, inline HTML, local-link resolution, images, footnotes.

## Architecture

A one-way pipeline; lower layers never import higher ones:

```
path → source.Load → md.Parse → doc.Document → layout.Render → layout.Document → style/search → terminal frame
```

`layout` defines the `Style` enum carried on each `Span`; `style` imports `layout` and maps that enum to SGR. Keep that direction — it is what stops layout from knowing about escape sequences.

- `cmd/mdv` — flags, validation, exit codes (2 = usage, 1 = runtime, 0 = ok). `runMain` takes its writers as arguments so it is testable.
- `internal/source` — `Validate` (path, extension, regular file, ≤32 MiB) split from `Load` (validate + read), so `app.Run` can reject a bad path before demanding a terminal.
- `internal/doc` — semantic model and `Position`/`Range` offset mapping. The hinge between parser and layout.
- `internal/md` — ordered block scanner and a single-pass, non-recursive inline scanner. `ParseInline` is exported because table cells are re-parsed by layout.
- `internal/layout` — wrapping, prefixes, gutters, table sizing, rune cell widths, `Nearest`.
- `internal/style`, `internal/link` — palettes and OSC 8.
- `internal/search`, `internal/editor`, `internal/terminal`, `internal/app`.

### Invariants worth preserving

- **The block scanner's check order is the grammar** (`parseBlocks` in `internal/md/md.go`): blank, fence, rule, heading, quote, list, table, indented code, paragraph. It is a straight-line sequence for a reason — reordering it changes what the language accepts, e.g. that a four-space-indented list item is a list rather than code. `special()`, which terminates a paragraph, deliberately omits tables and indented code.
- **No rendered row may exceed the requested width.** `hardSplit`, prefix clipping, `wrapRuns` (which drops trailing whitespace per row), and `clipSpans` all exist to enforce this. An over-wide row gets wrapped by the terminal, which displaces every row below it and corrupts the frame. `TestNoRowExceedsWidth` guards it across widths and gutter settings.
- **Tables wrap, they don't hide text.** A cell too wide for its column wraps onto further physical rows (`wrapRuns`, shared with block text) rather than being truncated; only the pathological case of more columns than fit even at `minColumnCells` each still gets clipped at the row edge via `clipSpans`. See `internal/layout/table.go` and `REQUIREMENTS.md` §4.
- **Source mapping is the point.** Every `Block`, `Inline`, `Span`, and `RenderedLine` carries a `SourceRange`. It drives the status line, `v` (open editor at line), and position restoration across reload and resize via `layout.Nearest`. New render paths must propagate ranges, even approximately — expanded code rows all share their block's range.
- **`SearchText` is the search substrate**: visible text including prefixes and padding, excluding delimiters and escapes. It must always equal the concatenation of the row's span texts (`TestSearchTextMatchesSpans`). Search never touches spans; highlighting splits them afterward at byte offsets and preserves `LinkTarget`.
- **Rendering happens before raw mode**, and `Leave` is deferred immediately after `Enter`. Every exit path — signals, recovered panics (re-panicked as `mdv: internal panic`), editor suspend — must restore termios, the cursor, and the primary screen.
- **One outstanding read, tracked by the viewer goroutine** (`internal/app/pump.go`). A second queued read would still be pending when the user presses `v` and would steal the editor's first keystroke.
- **No shell, ever.** `editor.Split` lexes `$VISUAL`/`$EDITOR` itself and rejects `;|&<>`, backticks, `$`, parens, braces, CR, and LF; the argv goes straight to `exec.Command`.
- **`--no-color` suppresses SGR but not OSC 8.** Colour and hyperlinking are separate capabilities; a link is still useful on a monochrome terminal. This asymmetry is intentional.

## Testing

Unit tests over fakes — `internal/app` runs against a `fakeTerminal` that records lifecycle calls and frames, and blocks (rather than erroring) when out of canned events, because returning an error would end the event loop and hide what the test meant to exercise. The signal tests deliver real signals to the test process. No golden files, no PTY integration tests, no CI.

The docs in `REQUIREMENTS.md` and `TECHNICAL_DESIGN.md` describe the implementation as built; keep them in sync when behavior changes.
