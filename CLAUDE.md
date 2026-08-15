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
- Out of scope: stdin, file watching, syntax highlighting, inline HTML, local-link resolution, images, footnotes.
- **Two files, only in diff mode.** `mdv FILE` still takes exactly one Markdown file. `mdv diff OLD NEW` takes two files of any type — the Markdown extension check is deliberately skipped there (`source.ValidateAny`), since refusing to diff a `.go` file would be enforcing a rendering constraint on a comparison.

## Architecture

A one-way pipeline; lower layers never import higher ones:

```
path  → source.Load    → md.Parse      → doc.Document     → layout.Render     ┐
paths → source.LoadAny → difftext.Lines → diffdoc.Document → layout.RenderDiff ┤
                                                                              ↓
                              layout.Document → style/search → terminal frame
```

Diff mode is a second data path into the same viewer, not a second viewer. Both branches converge on `layout.Document`, which is why paging, search, highlighting, resize, and the editor key work in a diff without knowing one is being shown.

`layout` defines the `Style` enum carried on each `Span`; `style` imports `layout` and maps that enum to SGR. Keep that direction — it is what stops layout from knowing about escape sequences.

- `cmd/mdv` — flags, validation, exit codes (2 = usage, 1 = runtime, 0 = ok). `runMain` takes its writers as arguments so it is testable.
- `internal/source` — `Validate` (path, extension, regular file, ≤32 MiB) split from `Load` (validate + read), so `app.Run` can reject a bad path before demanding a terminal.
- `internal/doc` — semantic model and `Position`/`Range` offset mapping. The hinge between parser and layout.
- `internal/md` — ordered block scanner and a single-pass, non-recursive inline scanner. `ParseInline` is exported because table cells are re-parsed by layout.
- `internal/layout` — wrapping, prefixes, gutters, table sizing, rune cell widths, `Nearest`.
- `internal/style`, `internal/link` — palettes and OSC 8.
- `internal/search`, `internal/editor`, `internal/terminal`, `internal/app`.
- `internal/difftext` — Myers O(ND) diff in its linear-space divide-and-conquer form, generic over `comparable`. Emits a flat edit script of Equal/Delete/Insert runs; it never pairs a delete with an insert, because that is an alignment decision.
- `internal/diffdoc` — the diff semantic model, parallel to `doc`. Pairs the edit script into left/right `Row`s, computes intraline word diffs, and folds distant context. `Row.Hidden` carries the collapsed rows so expanding a fold is a splice, not a re-diff.
- `internal/layout/diff.go` — diff rendering, in package `layout` rather than its own package because it reuses `run`, `clip`, `clipSpans`, `Width` and the `Span` model. A separate package would have to export all of them.

### Invariants worth preserving

- **The block scanner's check order is the grammar** (`parseBlocks` in `internal/md/md.go`): blank, fence, rule, heading, quote, list, table, indented code, paragraph. It is a straight-line sequence for a reason — reordering it changes what the language accepts, e.g. that a four-space-indented list item is a list rather than code. `special()`, which terminates a paragraph, deliberately omits tables and indented code.
- **No rendered row may exceed the requested width.** `hardSplit`, prefix clipping, `wrapRuns` (which drops trailing whitespace per row), and `clipSpans` all exist to enforce this. An over-wide row gets wrapped by the terminal, which displaces every row below it and corrupts the frame. `TestNoRowExceedsWidth` guards it across widths and gutter settings.
- **Tables wrap, they don't hide text.** A cell too wide for its column wraps onto further physical rows (`wrapRuns`, shared with block text) rather than being truncated; only the pathological case of more columns than fit even at `minColumnCells` each still gets clipped at the row edge via `clipSpans`. See `internal/layout/table.go` and `REQUIREMENTS.md` §4.
- **Source mapping is the point.** Every `Block`, `Inline`, `Span`, and `RenderedLine` carries a `SourceRange`. It drives the status line, `v` (open editor at line), and position restoration across reload and resize via `layout.Nearest`. New render paths must propagate ranges, even approximately — expanded code rows all share their block's range.
- **`SearchText` is the search substrate**: visible text including prefixes and padding, excluding delimiters and escapes. It must always equal the concatenation of the row's span texts (`TestSearchTextMatchesSpans`). Search never touches spans; highlighting splits them afterward at byte offsets and preserves `LinkTarget`.
- **Rendering happens before raw mode**, and `Leave` is deferred immediately after `Enter`. Every exit path — signals, recovered panics (re-panicked as `mdv: internal panic`), editor suspend — must restore termios, the cursor, and the primary screen.
- **One outstanding read, tracked by the viewer goroutine** (`internal/app/pump.go`). A second queued read would still be pending when the user presses `v` and would steal the editor's first keystroke.
- **No shell, ever.** `editor.Split` lexes `$VISUAL`/`$EDITOR` itself and rejects `;|&<>`, backticks, `$`, parens, braces, CR, and LF; the argv goes straight to `exec.Command`.
- **Diff rows never lose a line.** Folding is presentation only: `diffdoc.Expand`/`ExpandAll` must reproduce exactly the alignment that building with `Context: -1` gives, and every line of both files must appear exactly once across a document's rows and their `Hidden` contents. The random round-trip tests in `internal/diffdoc` guard both.
- **Diff layout packs, it does not wrap.** `packRuns`, not `wrapRuns`: a diff must preserve leading indentation and every space, and two panes only stay aligned if each cell of the original line is accounted for. Tabs are expanded against a running column (`expandTabs`) because a terminal advances a tab to its own stop, which would tear the right-hand pane loose. `TestSideBySidePanesStayAligned` guards the divider column.
- **The diff row mapping must stay in step.** `layout.DiffDocument.Rows` has one entry per rendered line, mapping it to the `diffdoc` row it came from; every physical row goes through `diffRenderer.emit` to keep the two slices the same length. Fold expansion and `[`/`]` hunk navigation both index through it.
- **`Span.Style` is what the text is; `Span.Background` is what happened to it.** Diff colouring belongs in `Background`, leaving `Style` for content. They compose into one SGR sequence in `style.paint`, background first — never nest `Apply`, whose reset would clear the background mid-span. Any code that reconstructs a `run` or `Span` must carry both fields; `packRuns` and `wrapRuns` silently dropped `background` when it was introduced.
- **`--no-color` suppresses SGR but not OSC 8.** Colour and hyperlinking are separate capabilities; a link is still useful on a monochrome terminal. This asymmetry is intentional.

## Testing

Unit tests over fakes — `internal/app` runs against a `fakeTerminal` that records lifecycle calls and frames, and blocks (rather than erroring) when out of canned events, because returning an error would end the event loop and hide what the test meant to exercise. The signal tests deliver real signals to the test process. No golden files, no PTY integration tests, no CI.

The docs in `REQUIREMENTS.md` and `TECHNICAL_DESIGN.md` describe the implementation as built; keep them in sync when behavior changes.
