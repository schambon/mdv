# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Current state

The repository contains **specifications only** — `README.md`, `REQUIREMENTS.md`, and `TECHNICAL_DESIGN.md`. There is no `go.mod`, no source, no tests, and no git repository yet. The two design docs are written in the present tense ("the implementation does X"), but they describe the *target* implementation, not existing code. Treat them as the authoritative spec to build against, and keep them in sync when behavior changes.

## Commands

```sh
go build -o mdv ./cmd/mdv    # build
go test ./...                # all tests
go test ./internal/layout    # one package
go test ./internal/md -run TestParseTable -v   # one test
./mdv README.md              # run (needs an interactive tty on both stdin and stdout)
```

`go` is not currently on PATH in this environment; install Go before attempting builds.

## Hard constraints

- **Standard library only.** `go.mod` must have zero third-party requirements. No Glow, no Bubble Tea, no termbox — file loading, parsing, layout, styling, search, terminal I/O, and editor exec are all owned by this codebase.
- **macOS only.** The terminal backend is Darwin-specific (termios via syscall, `TIOCGWINSZ`). Non-Darwin builds must return an error from terminal construction rather than failing to compile.
- **Bounded Markdown, deliberately.** Not CommonMark, not GFM. Anything outside the subset in `REQUIREMENTS.md` §4 renders as literal text. Resist the pull to "fix" this by adding nesting, escapes, reference links, or a delimiter stack — that is an explicit non-goal, not an oversight.
- Explicitly out of scope: stdin, multiple files, file watching, syntax highlighting, inline HTML, local-link resolution, images, footnotes.

## Architecture

Packages (`TECHNICAL_DESIGN.md` §2) form a one-way pipeline; lower layers never import higher ones:

```
path → source.Load → md.Parse → doc.Document → layout.Render → layout.Document → terminal frame
```

- `cmd/mdv` — flags, validation, exit codes (2 = bad args, 1 = runtime error, 0 = ok).
- `internal/source` — single-file load: abs path, `.md`/`.markdown` extension, regular file, ≤32 MiB.
- `internal/doc` — semantic model (`Block`, `Inline`, `SourceRange`). The hinge between parser and layout.
- `internal/md` — line-based block scanner with a fixed test order (blank, fence, rule, heading, quote, list, table, indented code, then paragraph fallback); that order *is* the grammar. Inline scanning is a single left-to-right pass, non-recursive, no escapes.
- `internal/layout` — wrapping, prefixes, gutters, table sizing, rune-based cell widths. Produces `RenderedLine`s carrying `Spans`, a `SearchText`, and a `SourceRange`.
- `internal/style`, `internal/link` — fixed SGR map and OSC 8. Note the asymmetry: `--no-color` suppresses SGR but *not* hyperlinks.
- `internal/search`, `internal/editor`, `internal/terminal`, `internal/app`.

### Invariants worth preserving

- **Source mapping is the point.** Every `Block`, `Inline`, `Span`, and `RenderedLine` carries a `SourceRange`. It drives the status line, `v` (open editor at line), and reload-position restoration via `layout.Nearest`. Any new render path must propagate ranges, even approximately (code rows all share their block's range).
- **`SearchText` is the search substrate** — visible text including layout prefixes and padding, excluding Markdown delimiters and escape sequences. Search never touches `Spans`; highlighting splits spans afterward at byte offsets.
- **Rendering happens before raw mode**, and `Leave` is deferred immediately after `Enter`. Every exit path — signals, panics (recovered, re-panicked as `mdv: internal panic`), editor suspend — must restore termios, the cursor, and the primary screen. `Enter`/`Leave` are mutex-guarded and idempotent.
- **One blocking terminal read at a time**, gated by a request pump, so the editor subprocess can take exclusive ownership of stdin.
- **No shell, ever.** `editor.Split` lexes `$VISUAL`/`$EDITOR` itself and *rejects* `;|&<>`, backticks, `$`, parens, braces, and newlines; the argv goes straight to `exec.Command`.
- Resize (`SIGWINCH`) updates dimensions and redraws but intentionally does **not** re-run `layout.Render`; reflow waits for a reload. Changing this is a real design decision, not a bug fix.

### Known intentional gaps in the spec

`auto` style always resolves to dark (the `COLORFGBG` check exists but never selects light); `dark` and `light` share one style map; `source.BaseDir` is stored but unused; `doc.Document.Position` is unused; parsed columns are always 1. Don't silently "correct" these — they are documented as-is.

## Testing

Tests are unit tests over fakes (a fake terminal for `internal/app`); no golden files, no PTY integration tests, no CI. When adding behavior, extend the package-level tests listed in `TECHNICAL_DESIGN.md` §13.
