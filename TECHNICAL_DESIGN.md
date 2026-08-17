# mdv — Implemented Technical Design

## 1. Constraints

The program is written in Go and imports only the standard library. `go.mod` has no third-party requirements. The interactive terminal backend is implemented only for Darwin; the non-Darwin build returns an error from terminal construction.

The implementation owns file loading, Markdown parsing, semantic data, diffing, layout, styling, searching, terminal I/O, and editor execution. It does not invoke Glow or another renderer, nor `diff`.

`git` is the one exception, and only in git mode: `mdv git` runs the `git` binary to fetch file contents, never to compute a difference. Everything else works with no repository and no `git` on `PATH`.

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
internal/difftext/ Myers line and word diff over any comparable slice
internal/diffdoc/  aligned diff rows, intraline segments, folding
internal/git/      repository blobs as a content source for diff mode
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

Diff mode replaces the first two stages and rejoins at `layout.Document`:

```text
two paths -> source.LoadAny -> difftext.Lines -> diffdoc.Build -> diffdoc.Document
                                                                        |
                                                                        v
                                                              layout.RenderDiff
                                                                        |
                                                                        v
                                                              layout.DiffDocument
```

Git mode replaces only the first stage of that: `git.Repo` fetches the two sides' bytes and `source.FromBytes` wraps them, after which the path is identical.

```text
revisions -> git.Repo.Load -> source.FromBytes -> (as above)
```

`DiffDocument` embeds a `layout.Document`, so everything downstream — search, styling, the viewport, resize, the editor key — is shared rather than reimplemented. Its extra `Rows` field maps each rendered line back to the diff row that produced it, which is what fold expansion and hunk navigation index through.

## 3. CLI and source loading

`cmd/mdv` uses `flag.FlagSet` with `ContinueOnError`. Flags are parsed in two passes over one shared `options` struct, because Go's `flag` package stops at the first non-flag argument: the first pass consumes the flags before a subcommand, and if a subcommand is what stopped it, a second `FlagSet` bound to the same fields consumes the flags after it. Both orders therefore behave identically, and a repeated flag resolves to its last occurrence. It accepts exactly one positional path and the flags documented in `REQUIREMENTS.md`. Style validation is limited to `auto`, `dark`, and `light`; width must be non-negative.

`source.Validate`:

1. converts the path with `filepath.Abs`;
2. accepts only case-insensitive `.md` and `.markdown` extensions;
3. checks `os.Stat` and requires a regular file;
4. rejects sizes above `32 << 20` bytes; and
5. returns the absolute path.

`ValidateAny` is the same without step 2. Diff mode uses it, since refusing to compare a `.go` file would apply a rendering constraint to a comparison; `Validate` is `ValidateAny` plus the extension check. `source.Load` and `LoadAny` validate, read the complete file with `os.ReadFile`, and return the absolute path, basename, and bytes. Splitting validation from reading lets `app.Run` reject a bad path before it demands a terminal.

`source.FromBytes` wraps content that never came from the file system — a git blob has no path on disk, and a side that is a revision may not exist there at all. It enforces `MaxSize` itself, since nothing has stat'd the content, and its `Path` may be empty, which is what tells the editor key there is nothing to open.

There is no source abstraction for stdin or multiple files, and no `BaseDir`: nothing resolves local links, so storing the source directory would only invite the assumption that something does.

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

`Document.Position` binary-searches `Lines` to derive a one-based line and byte-based column from a byte offset, and the parser calls it for every range it records, so columns are real rather than placeholders. Offsets outside the document clamp to its first or last line.

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

Paired-marker contents are stored as a single inline and are not recursively parsed. There is no delimiter stack or escape processing. A pair with an empty body is rejected as unmatched, so `**` alone stays visible instead of collapsing to nothing. Bare URL detection is lowercase and requires at least two characters after the scheme because of the regular expression shape. Adjacent literal runs, which the scanner emits whenever it retries at a construct byte, are merged before the inlines are returned.

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

Inline text is split into alternating Unicode-whitespace and non-whitespace runs. `wrapRuns` folds a run list into rows of at most the available width: it wraps before a run that would overflow the current row, hard-splits a run too wide for an empty row via `hardSplit`, drops trailing whitespace from each row, and never opens a row with whitespace. Block wrapping (`wrapped`) calls `wrapRuns` for a block's inlines and reproduces its own prefix/gutter on each returned row; table cell wrapping (§6.1) calls the same function per column. A prefix wider than the row is itself clipped so at least one column remains for text. `clipSpans` is the final backstop: no row leaves layout wider than requested, because the terminal would wrap an over-wide row and displace the rest of the frame.

Continuation rows reproduce the display width of the block prefix as spaces, and leave the line-number gutter blank rather than repeating the number. Source ranges are copied from the semantic block and inline; wrapping does not calculate more precise per-row source ranges.

### 6.1 Tables

Adjacent semantic table rows form one sizing group. Each column's natural width is the maximum visible width of that cell after inline parsing. Separators consume three cells each. While the table exceeds available content, the currently widest column above three cells is reduced by one; ties select the earlier column. Shrinking bottoms out at `minColumnCells` per column, minimizing how much wrapping the columns force rather than fitting the table exactly.

Each cell is reparsed as inline Markdown and wrapped to its assigned column width with the same `wrapRuns` used for block text; nothing is truncated. A logical table row occupies as many physical rows as its tallest cell, and shorter cells in that row pad the extra physical rows with blank space. Headers receive the base strong style, followed by a synthetic rule row placed after all of the header's physical rows. Inline kinds override the row's base style, so explicit emphasis, code, or links in a header use their inline styles. `clipSpans` remains the backstop for the case a row still cannot fit: a table with more columns than fit even at the per-column minimum is clipped at the row edge.

### 6.2 Width

`CellWidth` returns zero for NUL, U+200D, U+FE00–U+FE0F, and Unicode Mn/Me categories. It returns two for the hard-coded East Asian, full-width, supplementary CJK, and U+1F300–U+1FAFF ranges. Everything else returns one. `Width` sums rune widths. There is no tab expansion or grapheme segmentation.

## 7. Styles and hyperlinks

A `Span` carries two styles: `Style` is what the text *is* (heading, link, code span) and `Background` is what has happened to it (added, removed, a changed word). Diffing Markdown needs both at once, since a changed heading is still a heading. `Styler.paint` emits them as a single SGR sequence — background parameters, then foreground, then one reset. They are not nested because `Apply` appends a reset that would clear the background mid-span, and the order is deliberate: a search hit's `30;43` carries its own background in the foreground slot, so putting the foreground last keeps a match visible on top of a diff band. `joinParams` skips empty parameter lists so `StyleNone` cannot produce a stray separator.

Semantic layout styles map to fixed SGR sequences. Headings are bold cyan; emphasis italic; strong bold; strike crossed out; code gray; inline code pink; quotes and rules gray; links underlined blue; search matches use yellow backgrounds; status uses reverse video.

`style.New` resolves the theme and stores an enabled flag. `auto` inspects the last field of `COLORFGBG`, treating background indices 0-6 and 8 as dark and anything else as light, and defaulting to dark when the variable is absent or unparseable. `darkPalette` and `lightPalette` are separate maps; the light one uses darker foregrounds. With `--no-color`, SGR prefixes and resets are omitted.

`style.Span` applies `link.Open` whenever a span has a nonempty target, independently of the SGR enabled flag. `link.Valid` rejects C0 and DEL, parses with `net/url`, and allows only exact `http`, `https`, and `mailto` schemes. Valid links are wrapped with ST-terminated OSC 8 open and close sequences. Invalid targets return the unchanged label.

## 8. Search

`search.Find` scans each `RenderedLine.SearchText`. Smart case is determined with `unicode.IsUpper` on the query. Case-insensitive operation applies `strings.ToLower` to both strings, except on a row whose lowered form changes byte length, which stays case sensitive rather than corrupting every offset on that row. Match positions are byte offsets, and the next search within a row begins at the end of the previous match, preventing overlap.

`search.Next` compares rendered line numbers only. Forward selection chooses the first match on a line greater than the current line; backward selection chooses the last match on a line less than the current line. If none exists it wraps to the first or last match.

The application keeps `mode`, `query`, `lastQuery`, direction, matches, active index, and a saved viewport. Interactive typing recomputes the complete match list. Highlighting splits rendered spans at byte-offset match boundaries and replaces their style with search or active-search style; link targets remain attached to the split spans.

## 9. Application and viewport

`app.Run` creates the terminal, loads and renders before entering raw mode, enters the terminal, and then loops over full-frame draws and events. A request-gated event pump performs exactly one blocking terminal read at a time so an editor can take exclusive ownership of stdin.

The usable page height is terminal height minus one status row. Viewport movement is clamped between zero and the first row of the last page. The source line for status, reload, and editing is the first rendered row at or after the viewport top whose source start line is positive, falling back to line 1.

Reload calls `source.Load`, reads the current terminal size, selects terminal width or a narrower configured width, reparses and rerenders, and positions the viewport at `layout.Nearest`. `Nearest` minimizes absolute distance between requested and rendered source start lines and selects the earliest row on ties.

Resize takes the same path through the shared `reflow` helper: it records the source line on screen, applies the change, calls `render`, and restores the viewport with `Nearest`. Text therefore reflows to the new width while the reader keeps their place. Anything that invalidates the layout goes through `reflow` — `SIGWINCH`, the `l` gutter toggle, and diff folding — and every path recomputes search matches through `refreshMatches`, since the rows the matches refer to have been rebuilt.

`l` toggles `Config.LineNumbers` at runtime. The gutter takes its width out of the content rather than overlaying it, so the toggle is a relayout and not merely a redraw; in diff mode it re-lays out the existing rows without rebuilding the diff, leaving expanded folds open.

## 10. Terminal backend

`terminal.New` on Darwin requires stdin and stdout modes to include `os.ModeCharDevice`. `Enter` saves termios, clears `ECHO`, `ICANON`, `ISIG`, `IEXTEN`, `ICRNL`, `IXON`, and `OPOST`, sets `VMIN=1` and `VTIME=0`, then enters the alternate screen and hides the cursor. `Leave` resets SGR, shows the cursor, leaves the alternate screen, and restores termios. Both are guarded by a mutex and an `entered` flag.

Window size uses `TIOCGWINSZ` on stdout. Input uses `bufio.Reader.ReadRune`. Enter accepts CR or LF; backspace accepts DEL or BS. CSI decoding recognizes arrows, Page Up/Down, and two Home/End variants. Escape waits up to 35 ms using `select`; unknown sequences become Escape events. CSI collection is capped at five bytes.

`Suspend` calls `Leave`, runs a callback, then calls `Enter`, preferring the callback error over a re-entry error. Frames are written directly to stdout in one call. The application starts each frame with cursor-home and clear-screen, emits every visible row with erase-to-end, fills unused page rows, and writes the reverse-video status row, clipped to the terminal width so it cannot wrap. Because `OPOST` is disabled, row endings are explicit CRLF.

The input pump owns the one outstanding read. Its reader goroutine waits for a request before each `ReadEvent`, and the viewer goroutine tracks whether a read is already in flight, so a second read is never queued behind the first. Without that, a read requested during a resize could still be pending when the user presses `v`, and would steal the first keystroke from the editor.

Signals are delivered to the application loop. `SIGWINCH` updates dimensions. `SIGINT`, `SIGTERM`, and `SIGHUP` return normally, allowing deferred `Leave`. A deferred recovery calls `Leave` and replaces any panic value with `panic("mdv: internal panic")`.

## 11. Editor integration

`editor.Split` is a small argument lexer. Backslash escapes the next rune both inside and outside quotes. Matching single or double quotes group text and are removed. Outside quotes, Unicode whitespace separates arguments and `;|&<>` plus backtick, dollar, parentheses, braces, CR, and LF are rejected. Empty quoted arguments are not retained. A trailing backslash, unclosed quote, or empty result is an error.

`editor.Command` chooses `$VISUAL`, `$EDITOR`, or `vi`, derives the adapter from the executable basename with its extension removed, and appends the arguments described in `REQUIREMENTS.md`. It passes an argument vector directly to `exec.Command`; no shell is involved.

Editing suspends the terminal, connects the child to process stdin/stdout/stderr, waits synchronously, then reloads even if editor execution failed. An editor error is initially recorded, but a subsequent reload error replaces it.

## 12. Error and resource behavior

Initial load and render happen before raw mode. Terminal entry failures are returned directly. After entry, `Leave` is deferred. Reload and editor failures are recoverable status messages; frame writes and terminal-read failures terminate the application. The entire file and rendered document are held in memory. There is no render cache, filesystem watcher, background reload, or configurable size limit.

## 13. Diff mode

### 13.1 Algorithm

`difftext` implements Myers' O(ND) algorithm in the linear-space divide-and-conquer form of section 4b of the paper. The greedy form recording a full edit graph would be shorter, but costs O(D^2) memory, which a largely rewritten file reaches easily. `diffRange` trims the common prefix and suffix before any real work, since shared context is the common case and Myers' cost grows with the number of differences rather than the size of the input. `diffMiddle` then splits at a middle snake and recurses.

Because prefix and suffix are trimmed first, a region with content on both sides needs at least two edits, so the middle snake always divides it into two strictly smaller problems and the recursion terminates. `middleSnake` returns a bool for the case where the forward and reverse searches fail to meet; that cannot happen, but returning an empty snake at the origin instead would spin the recursion forever, so the caller degrades to rewriting the whole region.

The core is generic over `comparable`. `Lines` diffs `[]string`; `Words` tokenizes two strings and diffs the tokens. Tokens are runs of whitespace, runs of letters/digits/underscore, and every other rune on its own — punctuation splits individually so a changed argument does not swallow the parentheses around it.

The edit script is a flat sequence of Equal/Delete/Insert runs, merged so no two adjacent edits share an operation. Deletes and inserts are deliberately *not* paired into a "replace": pairing depends on how the result will be drawn, so it belongs to the caller.

### 13.2 Alignment and folding

`diffdoc.align` walks the edit script and pairs a delete run with an adjacent insert run — in either order, since which one the algorithm emits first depends on the path it took through the edit graph and the reader should not be able to tell. Pairs become `RowChanged`, and the longer side's leftovers trail as `RowRemoved` or `RowAdded`.

Intraline segments are computed per changed row and then discarded when the two lines share less than `similarityFloor` (0.25) of their text: below that the lines have little to do with each other and word highlighting scatters fragments instead of showing a change. A row with nil `Words` is highlighted whole.

`fold` marks every row within `Context` of a change as visible, then collapses each maximal run of remaining equal rows into one `RowFolded` carrying them in `Hidden`. Runs shorter than `minFold` (3) are left alone, since folding them saves no space. Keeping the hidden rows inside the fold makes `Expand` a splice rather than a re-diff, and makes it verifiable that folding is presentation only: flattening a folded document reproduces the unfolded alignment exactly.

### 13.3 Diff layout

Diff rendering lives in `internal/layout/diff.go`, inside package `layout`, because it is the same job as Markdown layout and reuses `run`, `clip`, `clipSpans`, `Width`, `decodeRune` and the `Span` model. A separate package would have to export all of them.

`packRuns` replaces `wrapRuns` for diff content. It breaks runs at the pane edge rather than at word boundaries and never drops whitespace: leading indentation is meaningful in a diff, and two panes stay aligned only if every cell of the original line is accounted for. `expandTabs` converts tabs against a running column, because a terminal advances a tab to its own stop regardless of which column a pane begins at, which would tear the right-hand pane loose.

A side-by-side row draws `[gutter][marker][content] │ [gutter][marker][content]`. Each side wraps independently and the row occupies as many physical rows as the taller side, with the shorter side padded — the same shape as a table row (§6.1). Only the first physical row carries a line number and marker; repeating them on a continuation would read as a second change. Side-by-side is refused when a pane would fall below `minPaneCells` (24), because one column is more readable than two cramped ones.

The unified form draws `[old gutter][new gutter][marker][content]`, and a changed row becomes two physical rows. Added and removed rows are padded out to the full width so they read as a band rather than a ragged stripe.

The line-number gutter is sized to the widest number the document actually contains rather than a fixed width, capped at `maxGutterDigits`: in a split pane every wasted column comes off the content.

Diff colours are backgrounds rather than foregrounds, since the line is the unit of change; the `Word` variants are a step brighter so the differing part stands out from the line holding it. The `-`/`+` marker duplicates what colour conveys deliberately, so `--no-color` stays readable.

`rowSource` maps a row to a source line, preferring the new side: the reader is normally looking at what the file became, and that is the line `v` should open. A row present only in the old file maps to its old line number.

### 13.4 Viewer integration

`Config.Compare` selects diff mode. `App.buildDiff` is kept separate from `App.render` because which folds are open is part of the row list: a resize must lay the diff out again without discarding what the reader has expanded. `load` and `reload` rebuild the diff; resize and fold changes only re-render.

Fold and hunk keys are single letters rather than vim's `z`-prefixed chords, because the event loop dispatches one keypress at a time and a pending-prefix state machine is a poor trade for two keystrokes of familiarity. `handleDiffRune` is consulted before the base bindings and declines every key when not in diff mode.

`x` expands the first fold at or below the top of the viewport — the same row the status line and the editor key already act on. Nothing above the viewport moves, so the view needs no repositioning; `X` and `z`, which do move rows above the top, go through `reflow`, which holds position with `layout.Nearest` the way resize does. `z` re-folds by rebuilding rather than trying to reverse individual expansions.

`hunkStarts` treats a run of adjacent changed rows as one hunk, so `[`/`]` do not stop on every line of a rewritten block. Comparing consecutive rendered lines rather than rows also skips wrapped continuations, which share a row with the line above.

Any rebuild of the rows invalidates the row indices search matches hold, so `refreshMatches` is called from resize, reload, and every fold change.

### 13.5 Git mode

`mdv git [REV | REV..REV] [PATH]` sets `Config.Git`, and `App.load` dispatches to `loadGit` instead of reading paths. Git is a *content* source: `internal/git` fetches each side's bytes and hands them to the same alignment, folding, word diff and side-by-side layout the two-file form uses. Parsing `git diff`'s unified output would throw all of that away, along with the ability to expand context beyond what git chose to print.

Every command goes through an injected `git.Runner`, mirroring `App.runEdit`, so the tests use canned output and need neither a repository nor the binary.

`git.Spec` maps the command line onto two `Side`s — a revision, the index, or the work tree — following git's own defaults, so `mdv git` compares the index with the work tree exactly as `git diff` does, and `--staged` compares `HEAD` with the index. `A..B` names two revisions; `A...B` resolves the merge base first. A revision cannot be told from a path without asking the repository, so `cmd/mdv` passes the operands through as typed and `Repo.Resolve` decides: an existing file wins, then a revision, then a path git still tracks — which is how a file deleted from the work tree can be named.

`Repo.Load` fetches both sides in one call, because whether a side exists is a property of the pair: an added file is absent from the old side and a deleted one from the new, and both must come back empty rather than as an error.

The hardening is not incidental:

- Operands beginning with `-` are rejected and pathspecs are terminated with `--`. There is no shell, but git parses its own arguments, and a revision named `--upload-pack=…` is an option.
- Every command carries `--no-ext-diff` and `--no-textconv`, and `GIT_OPTIONAL_LOCKS=0` with `--no-optional-locks`. A repository's `.gitattributes` can otherwise name an external diff driver or a textconv filter, and mdv would run a program it did not choose from a repository it did not write.
- Output is captured, never inherited, and git never goes through `term.Suspend` the way the editor does. The viewer is in raw mode on the alternate screen, where a child's output cannot be repaired.
- Content holding a NUL byte is replaced with a `Binary file, N bytes` marker. Raw bytes written to a terminal in raw mode are not recoverable.

`loadGit` keeps the whole changed-file list but fetches only the file on screen. `Repo.Stats` runs `git diff --numstat` once for the entire list, which is what lets the sidebar label every file without diffing any of them; laziness that a labelling pass undid would not be laziness.

`editTarget` is empty when no side is on disk, as for `A..B`, and `v` says so instead of opening a path that does not exist. `reload` re-runs the whole of `loadGit`, so `r` picks up a commit or a stage made outside the viewer. The list it rebuilds need not be the one it replaced, so the file on screen is followed by name through `indexOf`, not by position.

### 13.5.1 The changed-file sidebar

The sidebar has **no focus model**, and that is the design rather than a simplification of one. It has exactly one interaction — pick a file — so dedicated keys move the selection and the diff is rebuilt to reflect it. A focus model would have bought nothing and cost focus state, per-focus key routing, a per-focus status line, and an answer to "which pane does `j` scroll". `>`/`<` and the arrows were free: `[`, `]`, `x`, `X` and `z` are taken, and nothing scrolls horizontally.

Drawing needs no new machinery in `layout`. `render` lays the diff out at `renderWidth() - sidebarCells()`; `RenderDiff` never learns the sidebar exists, and its own `clipSpans` guarantees the budget. `draw` then builds a **throwaway** `RenderedLine` per screen row — the sidebar entry, a `StyleRule` separator, then `highlight(index).Spans` — and hands it to `styler.Line`, which reads only `Spans`. It must not be stored: its `SearchText` would not equal the concatenation of its spans, which is the invariant search rests on. Search highlighting is applied before composition, so it is unchanged by any of this.

`selectFile` stores the outgoing `diffdoc.Document` in `App.diffs` before loading the new one, so folds expanded in a file survive a switch away and back. The viewport and the active match are reset instead, since a row index measured against one document means nothing in another. The cache is dropped whole on `reload`, where the documents behind it may no longer describe what git reports.

The width is sized to the longest entry rather than to a fraction of the terminal, clamped to `[minSidebarCells, maxSidebarCells]` and to what leaves the diff `minSidebarDiffCells`. Below that the list is dropped, mirroring the `minPaneCells` fallback, and `reflow` re-renders correctly when it appears or disappears. The keys stay live without it, because the status line still reads `file N/M`. Whether the counts fit is decided once for the whole list, not per entry, or the paths would stop at a different column on every row — which is also why the width calculation reserves `minNameCells` even for a short path, so sizing and drawing cannot disagree about whether the counts are there.

### 13.6 Markdown-aware comparison

`diffdoc.BuildMarkdown` compares two parsed documents block by block. The unit is a `doc.Block`, except that adjacent table rows form one unit, since a table's columns are sized across all of its rows and diffing them separately would let the two sides pick different widths.

Comparing blocks rather than rendered lines is what makes the feature width-independent: a resize re-renders without re-diffing, preserving the `buildDiff`/`render` split. It also fixes more than a rewrap — a one-word edit inside a wrapped paragraph reflows every line below it, and a rendered-line diff would report all of them.

Alignment itself is shared with the line path. `build` takes a `lineAt` per side, so the aligner only ever asks for line *i* of a side and never learns whether the underlying element was a source line or a parsed block; folding, pairing and the intraline diff are the same code.

The alignment key is `{kind, level, prefix, header}` per block followed by `{kind, text, target}` per inline, joined with ASCII control characters that Markdown source cannot contain. Both halves are load-bearing. Keyed on inline text alone, `[docs](old)` and `[docs](new)` are identical and a changed link shows no diff at all — a wrong answer, not an untidy one. Source position is deliberately excluded, so moving a paragraph down a file is not a change. Rewrap-invariance follows from `md.paragraph` joining a paragraph's lines with single spaces: any rewrapping produces a byte-identical block.

`Line.Text` stays the unit's visible text — the concatenation of its inlines — because that is what the word diff runs on, while `Line.Blocks` is what the renderer draws. The two must not be conflated: they are the same string only by construction.

Rendering is in `internal/layout/mddiff.go`. An unchanged unit is drawn **once at full width**: both sides are identical by definition of the key, so two half-width copies would waste the terminal and wrap prose into narrow columns for nothing. Only a changed unit splits into panes, and a horizontal rule marks every transition between the two shapes, including one closing a split section that reaches the end of the document. A unit renders through `Render(doc.Document{Blocks: unit})`, which reads only `Blocks`, and each spliced line's `Source` is overwritten with the row's, since `sourceLine`, `Nearest` and `v` all read it.

Word marks are computed in the semantic layer, not in layout. `markInlines` cuts each side's inlines at the word-diff segment boundaries and sets `doc.Inline.Mark` on the pieces that differ; layout carries the bit through `run` and `Span` exactly as it carries `LinkTarget`, and `unitLines` turns it into the emphasis background. Passing diff ranges into `layout.runs` instead would make the Markdown renderer learn what a diff is, inverting the layering; `search.Highlight` is not reusable either, since its offsets are into `SearchText`, which includes gutter, indent, prefixes and padding. The cut is exact rather than approximate because a unit's text is by construction the concatenation of its inlines, so a byte offset into one is a byte offset into the other. Marking copies the blocks: the parsed document is shared with every other row.

Code blocks and tables are excluded from marking. `renderer.code` renders from `Inlines[0].Text` and ignores the rest, and `parseCells` re-parses a cell from its raw text, so a mark landing on a later inline would silently vanish. Those units keep the whole-unit band, which is honest about what it knows.

Because a row is a block in this mode, the fold marker reads "N unchanged blocks" and `diffSummary` reads `+N -M blocks`. Saying "lines" would misreport the size of what is hidden.

## 14. Implemented tests

The repository's Go tests cover:

- block and inline parsing plus basic source mappings;
- Unicode width, wrapping, blank rows, code row expansion, shared table widths, and inline Markdown in tables;
- literal search, smart case, and navigation wrapping;
- editor command splitting and adapter arguments;
- link validation and OSC 8 output;
- theme/no-colour behavior;
- terminal event decoding, escape-sequence handling, and size normalization;
- command-line parsing, validation, and exit codes, including the diff form and its flags;
- the diff algorithm, through randomised round-trip tests asserting the edit script rebuilds the second input, adjacent edits never share an operation, and the Myers paper's worked example costs its known 5 edits;
- diff alignment, through randomised tests asserting every unit of both sides appears exactly once and that folding reproduces the unfolded alignment when flattened — run over both paths, on random line sequences and on random documents built from an alphabet of Markdown blocks;
- diff layout: no row exceeding the width at any width or gutter setting, the divider landing on the same column of every row, preserved indentation, expanded tabs, and the rendered-line-to-row mapping staying in step;
- diff viewer behaviour over fakes: folding, expansion, hunk navigation, the status summary, reload of both files, and search matches surviving a fold change;
- Markdown-aware comparison: that rewrapping a corpus of paragraphs at several widths produces no hunks at all, that the line diff still would (so the first test means something), that a changed link target is a difference, that every unit appears exactly once, and that unchanged units render full width while split ones stay aligned;
- word marks: the marked pieces of a one-word edit, that cutting inlines preserves each side's text exactly and keeps a cut link's kind and target, that the parsed document is never written through, that marks survive wrapping and a hard split, and that code blocks and tables get the band instead;
- git mode over a fake runner: the arguments and hardening flags of each command, revision-against-path resolution, the spec each command line maps to, added, deleted and binary files, the editor key with no file on disk, refetching on reload, and clean errors for a missing repository, a missing git, an unresolvable operand, and a failed listing;
- the changed-file sidebar: the list and its numstat labels, selecting with the keys and with the arrows, the ends of the list, the viewport reset and the folds that survive a switch, entries all of one width, head-clipped paths, the fallback on a narrow terminal, and a reload that reorders or drops the file on screen;
- application paging, status text, search interaction, editing/reload behavior, resize reflow, input-pump ownership, signal handling, CRLF frame rows, and terminal lifecycle through fakes.

Tests run with `go test ./...`, and `go test -race ./...` covers the signal handler, input pump, and terminal state. There are no golden files, pseudo-terminal integration tests, CI configuration, benchmarks, or packaging scripts in the repository.
