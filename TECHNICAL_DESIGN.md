# mdv — Implemented Technical Design

## 1. Constraints

The program is written in Go and imports only the standard library. `go.mod` has no third-party requirements. The interactive terminal backend is implemented only for Darwin; the non-Darwin build returns an error from terminal construction.

The implementation owns file loading, Markdown parsing, semantic data, layout, styling, searching, terminal I/O, and editor execution. It does not invoke Glow or another renderer.

## 2. Package structure

```text
cmd/mdv/           flags, validation, exit codes, version output
internal/app/      state, event loop, viewport, search/edit/reload actions
internal/source/   one-file loading and validation
internal/md/       bounded block and inline parser
internal/doc/      semantic blocks, inlines, and source ranges
internal/layout/   table formatting, wrapping, width, rendered spans
internal/style/    fixed ANSI styles and OSC 8 composition
internal/search/   literal matching and directional selection
internal/editor/   safe command splitting and editor-specific arguments
internal/link/     external-target validation and OSC 8 encoding
internal/terminal/ Darwin raw mode, sizing, events, and screen lifecycle
```

The main data flow is:

```text
path -> source.Load -> md.Parse -> doc.Document
                                  |
                                  v
                            layout.Render
                                  |
                                  v
                         layout.Document
                          /      |      \
                     search   styling   source-line lookup
                                  |
                                  v
                            terminal frame
```

## 3. CLI and source loading

`cmd/mdv` uses `flag.FlagSet` with `ContinueOnError`. It accepts exactly one positional path and the flags documented in `REQUIREMENTS.md`. Style validation is limited to `auto`, `dark`, and `light`; width must be non-negative.

`source.Load`:

1. converts the path with `filepath.Abs`;
2. accepts only case-insensitive `.md` and `.markdown` extensions;
3. checks `os.Stat` and requires a regular file;
4. rejects sizes above `32 << 20` bytes;
5. reads the complete file with `os.ReadFile`; and
6. returns the absolute path, basename, directory, and bytes.

There is no source abstraction for stdin or multiple files. `BaseDir` is retained in `source.Source` but is not consumed by link handling.

## 4. Semantic model

```go
type Position struct {
    ByteOffset int
    Line       int
    Column     int
}

type SourceRange struct {
    Start Position
    End   Position
}

type Inline struct {
    Kind         InlineKind
    Text, Target string
    Source       SourceRange
}

type Block struct {
    Kind    BlockKind
    Inlines []Inline
    Source  SourceRange
    Level   int
    Prefix  string
    Header  bool
}

type Document struct {
    Blocks []Block
    Lines  []int
    Size   int
}
```

Block kinds are blank, paragraph, heading, rule, code, quote, list item, and table row. Inline kinds are text, emphasis, strong, strike, inline code, link, and the synthetic table separator.

The parser records byte offsets and one-based source lines. Columns are currently set to 1 by parsed ranges. `Document.Position` can derive a one-based line and byte-based column from a byte offset, but current application paths do not call it.

## 5. Parser

### 5.1 Line model

`md.Parse` converts the byte slice to a Go string, uses `strings.SplitAfter` on LF, removes LF and an optional preceding CR from each logical line, and records the original byte start. A final synthetic empty element caused by a trailing LF is removed.

The block scanner is ordered. Blank lines, fences, rules, headings, quotes, lists, tables, and indented code are tested before the paragraph fallback. This ordering is part of the implemented syntax.

### 5.2 Blocks

- A fence is any trimmed line beginning with ` ``` ` or `~~~`; only its first three characters select the closing marker. Content continues until a trimmed line begins with that marker or EOF. The opener and closer are omitted.
- A rule contains only one of `-`, `*`, or `_` after spaces and tabs are removed and has length at least three.
- An ATX heading begins at byte zero with one to six `#` bytes followed by one ASCII space. Leading indentation prevents heading recognition.
- A quote permits leading Unicode whitespace, strips one `>`, and becomes a single block with prefix `│ `.
- The list regular expression is `^(\s*)([-+*]|[0-9]+[.)])\s+(.*)$`. Captured indentation is not preserved. A leading `[ ] ` or case-insensitive `[x] ` in the body is appended to the displayed prefix.
- A table starts when a line containing `|` is followed by a valid delimiter row. The delimiter row is consumed but not represented. Subsequent nonblank lines containing `|` become rows. Cells are split on every pipe after trimming outer spaces and pipes; empty fields are discarded by `strings.FieldsFunc`.
- Indented code consumes consecutive lines beginning with four literal spaces and strips those four spaces.
- Paragraphs consume lines until blank or until `special` recognizes a heading, rule, fence, quote, or list. Their trimmed contents are joined with ASCII spaces. A table start and indented code are not paragraph terminators in `special`, so they are recognized only when encountered at the start of a new block scan.

### 5.3 Inlines

The inline scanner operates left to right. At the current position it checks:

1. `[label](target)` using the first `](` and following `)`;
2. paired markers in this order: `**`, `__`, `~~`, backtick, `*`, `_`;
3. a bare URL matching `https?://[^\s<>]+[^\s<>.,;:!?)]`; then
4. literal text up to a byte that may start another construct.

Paired-marker contents are stored as a single inline and are not recursively parsed. There is no delimiter stack or escape processing. Bare URL detection is lowercase and requires at least two characters after the scheme because of the regular expression shape.

`ParseInline` exposes the same scanner to table layout so table cells receive identical supported inline styling.

## 6. Layout

The rendered model is:

```go
type Span struct {
    Text       string
    Cells      int
    Style      Style
    LinkTarget string
    Source     doc.SourceRange
}

type RenderedLine struct {
    Spans      []Span
    SearchText string
    Source     doc.SourceRange
}

type Document struct {
    Lines []RenderedLine
}
```

`layout.Render` enforces a minimum width of 10. It reserves two cells on each horizontal side conceptually, though only the two-cell left prefix is emitted. Line numbers reserve a seven-cell gutter: six right-aligned decimal digits and one space. Values longer than six digits are truncated to their last six digits.

Multi-line code blocks are expanded into one block per physical code row before normal layout. Every expanded row retains the code block's source range.

Inline text is split into alternating Unicode-whitespace and non-whitespace runs. Before adding a run, layout wraps when the current row already contains content beyond its prefix/gutter and the run would exceed `width - horizontalPadding`. A single run wider than the row is not split and may exceed the requested width.

Continuation rows reproduce the display width of the block prefix as spaces. Source ranges are copied from the semantic block and inline; wrapping does not calculate more precise per-row source ranges.

### 6.1 Tables

Adjacent semantic table rows form one sizing group. Each column's natural width is the maximum visible width of that cell after inline parsing. Separators consume three cells each. While the table exceeds available content, the currently widest column above three cells is reduced by one; ties select the earlier column.

Each cell is reparsed as inline Markdown, clipped rune by rune to its assigned cell width, and padded with plain spaces. A clipped inline retains its kind and target. Headers receive the base strong style, followed by a synthetic rule row. Inline kinds override the row's base style, so explicit emphasis, code, or links in a header use their inline styles.

### 6.2 Width

`CellWidth` returns zero for NUL, U+200D, U+FE00–U+FE0F, and Unicode Mn/Me categories. It returns two for the hard-coded East Asian, full-width, supplementary CJK, and U+1F300–U+1FAFF ranges. Everything else returns one. `Width` sums rune widths. There is no tab expansion or grapheme segmentation.

## 7. Styles and hyperlinks

Semantic layout styles map to fixed SGR sequences. Headings are bold cyan; emphasis italic; strong bold; strike crossed out; code gray; inline code pink; quotes and rules gray; links underlined blue; search matches use yellow backgrounds; status uses reverse video.

`style.New` stores the requested theme name and an enabled flag. `auto` always becomes `dark`; its `COLORFGBG` check does not currently select light. Dark and light names do not alter the style map. With `--no-color`, SGR prefixes and resets are omitted.

`style.Span` applies `link.Open` whenever a span has a nonempty target, independently of the SGR enabled flag. `link.Valid` rejects C0 and DEL, parses with `net/url`, and allows only exact `http`, `https`, and `mailto` schemes. Valid links are wrapped with ST-terminated OSC 8 open and close sequences. Invalid targets return the unchanged label.

## 8. Search

`search.Find` scans each `RenderedLine.SearchText`. Smart case is determined with `unicode.IsUpper` on the query. Case-insensitive operation applies `strings.ToLower` to both strings. Match positions are byte offsets, and the next search within a row begins at the end of the previous match, preventing overlap.

`search.Next` compares rendered line numbers only. Forward selection chooses the first match on a line greater than the current line; backward selection chooses the last match on a line less than the current line. If none exists it wraps to the first or last match.

The application keeps `mode`, `query`, `lastQuery`, direction, matches, active index, and a saved viewport. Interactive typing recomputes the complete match list. Highlighting splits rendered spans at byte-offset match boundaries and replaces their style with search or active-search style; link targets remain attached to the split spans.

## 9. Application and viewport

`app.Run` creates the terminal, loads and renders before entering raw mode, enters the terminal, and then loops over full-frame draws and events. A request-gated event pump performs exactly one blocking terminal read at a time so an editor can take exclusive ownership of stdin.

The usable page height is terminal height minus one status row. Viewport movement is clamped between zero and the first row of the last page. The source line for status, reload, and editing is the first rendered row at or after the viewport top whose source start line is positive, falling back to line 1.

Reload calls `source.Load`, reads the current terminal size, selects terminal width or a narrower configured width, reparses and rerenders, and positions the viewport at `layout.Nearest`. `Nearest` minimizes absolute distance between requested and rendered source start lines and selects the earliest row on ties.

Resize calls the same render path only indirectly: `SIGWINCH` invokes `resize`, which updates stored dimensions, but the current implementation does not call `layout.Render` on resize. The already-rendered document therefore retains its previous wrapping until a reload or editor return. Frame height and viewport bounds do use the new dimensions.

## 10. Terminal backend

`terminal.New` on Darwin requires stdin and stdout modes to include `os.ModeCharDevice`. `Enter` saves termios, clears `ECHO`, `ICANON`, `ISIG`, `IEXTEN`, `ICRNL`, `IXON`, and `OPOST`, sets `VMIN=1` and `VTIME=0`, then enters the alternate screen and hides the cursor. `Leave` resets SGR, shows the cursor, leaves the alternate screen, and restores termios. Both are guarded by a mutex and an `entered` flag.

Window size uses `TIOCGWINSZ` on stdout. Input uses `bufio.Reader.ReadRune`. Enter accepts CR or LF; backspace accepts DEL or BS. CSI decoding recognizes arrows, Page Up/Down, and two Home/End variants. Escape waits up to 35 ms using `select`; unknown sequences become Escape events. CSI collection is capped at five bytes.

`Suspend` calls `Leave`, runs a callback, then calls `Enter`, preferring the callback error over a re-entry error. Frames are written directly to stdout in one call. The application starts each frame with cursor-home and clear-screen, emits every visible row with erase-to-end, fills unused page rows, and writes the reverse-video status row. Because `OPOST` is disabled, row endings are explicit CRLF.

Signals are delivered to the application loop. `SIGWINCH` updates dimensions. `SIGINT`, `SIGTERM`, and `SIGHUP` return normally, allowing deferred `Leave`. A deferred recovery calls `Leave` and replaces any panic value with `panic("mdv: internal panic")`.

## 11. Editor integration

`editor.Split` is a small argument lexer. Backslash escapes the next rune both inside and outside quotes. Matching single or double quotes group text and are removed. Outside quotes, Unicode whitespace separates arguments and `;|&<>` plus backtick, dollar, parentheses, braces, CR, and LF are rejected. Empty quoted arguments are not retained. A trailing backslash, unclosed quote, or empty result is an error.

`editor.Command` chooses `$VISUAL`, `$EDITOR`, or `vi`, derives the adapter from the executable basename with its extension removed, and appends the arguments described in `REQUIREMENTS.md`. It passes an argument vector directly to `exec.Command`; no shell is involved.

Editing suspends the terminal, connects the child to process stdin/stdout/stderr, waits synchronously, then reloads even if editor execution failed. An editor error is initially recorded, but a subsequent reload error replaces it.

## 12. Error and resource behavior

Initial load and render happen before raw mode. Terminal entry failures are returned directly. After entry, `Leave` is deferred. Reload and editor failures are recoverable status messages; frame writes and terminal-read failures terminate the application. The entire file and rendered document are held in memory. There is no render cache, filesystem watcher, background reload, or configurable size limit.

## 13. Implemented tests

The repository's Go tests cover:

- block and inline parsing plus basic source mappings;
- Unicode width, wrapping, blank rows, code row expansion, shared table widths, and inline Markdown in tables;
- literal search, smart case, and navigation wrapping;
- editor command splitting and adapter arguments;
- link validation and OSC 8 output;
- theme/no-colour behavior;
- application paging, editing/reload behavior, input-pump ownership, CRLF frame rows, and terminal lifecycle through fakes.

Tests run with `go test ./...`. There are no golden files, pseudo-terminal integration tests, race-test automation, CI configuration, benchmarks, or packaging scripts in the repository.
