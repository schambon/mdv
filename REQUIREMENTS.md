# mdv — Implemented Requirements

## 1. Purpose and scope

`mdv` is an interactive, standard-library-only Markdown viewer for macOS terminals. It displays one local Markdown file in an alternate-screen pager, supports literal search, emits OSC 8 hyperlinks for safe external URLs, and can open the source file in an editor. It also compares two files side by side in the same pager (§9).

The implementation is intentionally a bounded Markdown renderer, not a CommonMark or GitHub Flavored Markdown implementation. HTML, standard input, file watching, local-link resolution, syntax highlighting, and non-macOS terminals are not supported. More than two files at once is not supported.

## 2. Command line

```text
mdv [options] FILE
mdv [options] diff OLD NEW
```

`FILE` is required and must name a regular file whose extension, matched case-insensitively, is `.md` or `.markdown`. The file must be no larger than 32 MiB. Relative paths are converted to absolute paths before use. The `diff` form is described in §9; it applies the same regular-file and size checks but no extension check.

Implemented options:

```text
-w, --width N          Maximum rendering width; 0 uses terminal width
-s, --style STYLE      auto, dark, or light
-l, --line-numbers     Show source line numbers (toggled in the viewer with l)
    --no-color         Disable SGR styling
-V, --version          Print the version and exit
-h                     Print flag help and exit
```

Diff-mode options are listed in §9. They parse in both forms but have no effect on the single-file form.

Width must be non-negative. A positive width caps rendering only when it is narrower than the terminal. Invalid arguments return exit status 2; startup or runtime errors return 1; normal completion returns 0.

Flags may appear on either side of a subcommand: `mdv --no-split diff a b` and `mdv diff --no-split a b` are equivalent. When the same flag is given twice, the later one wins.

Both stdin and stdout must be interactive character devices. The path is validated before a terminal is required, so a bad filename reports the file problem rather than a terminal problem. On platforms other than macOS, startup fails with an unsupported-platform error.

## 3. Viewer

The viewer enters raw mode, switches to the alternate screen, hides the cursor, reserves the last terminal row for status, and redraws the complete frame after each event. Toggling the gutter with `l` reflows the document, since the gutter takes its width out of the content; the viewport is restored to the rendered row nearest the source line that was showing, as on a resize. Terminal resize events update the viewport dimensions, reflow the document at the new width, and restore the viewport to the rendered row nearest the source line that was showing. Terminal widths below 10 or failed size reads fall back to 80 columns; heights below 2 fall back to 24 rows.

The normal status text is:

```text
FILENAME  PERCENT%  source line CURRENT/TOTAL
```

`CURRENT` is the source line of the first mapped rendered row at or below the top of the viewport, and `TOTAL` is the number of lines in the file.

Messages and the search prompt temporarily replace it. Messages are displayed for one frame.

Implemented keys:

| Key | Action |
|---|---|
| `j`, Down, Enter | Move down one rendered row |
| `k`, Up | Move up one rendered row |
| Space, PageDown, `Ctrl-F` | Move down one page |
| `b`, PageUp, `Ctrl-B` | Move up one page |
| `g`, Home | Go to the first rendered row |
| `G`, End | Go to the last page |
| `/`, `?` | Enter forward or backward search |
| `n`, `N` | Repeat the last search in the same or opposite direction |
| `l` | Show or hide the line-number gutter |
| `v` | Edit the source at the current mapped line |
| `r`, `Ctrl-R` | Reload the source |
| `h` | Show a one-line key summary |
| `q` | Quit |
| Esc | Cancel search, or do nothing in normal mode |

## 4. Markdown subset

The block parser implements:

- ATX headings with one to six `#` characters followed by a space;
- paragraphs, joining consecutive source lines with one space;
- blank lines;
- horizontal rules made from at least three `-`, `*`, or `_` characters with spaces or tabs allowed;
- fenced code opened by a trimmed line beginning with three backticks or tildes and closed by a trimmed line beginning with the same three-character marker;
- consecutive four-space-indented code lines;
- single-line block quotes whose first non-space character is `>`;
- single-line unordered list items using `-`, `+`, or `*` and ordered items using digits followed by `.` or `)`;
- task markers `[ ]` and case-insensitive `[x]` immediately after a list marker;
- pipe tables identified by a header containing `|` followed by a delimiter row whose cells contain at least three hyphens, with optional colons.

Nested block structure is not modeled. Fence language labels are ignored. Table alignment colons do not affect alignment. Escaped pipes and other complex table syntax are not supported.

The inline parser implements paired `**` and `__` strong emphasis, paired `*` and `_` emphasis, paired `~~` strikethrough, single-backtick code spans, `[label](target)` links, and bare lowercase `http://` or `https://` URLs. Delimiters are matched by the next closing delimiter; nesting, escaping, reference links, autolinks, images, and pathological delimiter rules are not supported. A pair enclosing nothing, as in `a ** b`, is treated as unmatched so the delimiters stay visible rather than rendering as an empty span.

Inline markup is parsed in paragraphs, headings, quotes, list bodies, and table cells. Unsupported or unmatched syntax remains visible text.

## 5. Layout and styling

Content has two columns of left and right horizontal allowance. Text wraps at transitions between whitespace and non-whitespace runs. Continuation rows repeat the block prefix width as spaces. Code rows use four leading spaces; quote rows use `│ `; headings retain their `#` prefix; list markers are preserved.

No rendered row ever exceeds the requested width. A run with no wrapping opportunity, such as a long URL or code line, is split across rows rather than allowed to overflow, and trailing whitespace is trimmed from a row broken after a space. An overflowing row would be wrapped by the terminal itself, displacing every row below it and corrupting the frame.

Tables share column widths across adjacent table rows. Width is measured from visible inline text, excluding supported Markdown delimiters and link targets. If a table is too wide, its widest columns are reduced one cell at a time, but not below three cells; cell contents that still do not fit their column wrap onto further rows rather than being truncated, and a table row occupies as many physical rows as its tallest cell, with shorter cells padded blank beneath. A table with more columns than the row can hold even at that minimum is clipped at the row edge. Columns are separated with ` │ ` and the header is followed by a rule using `─` and `┼`, placed after all of the header's wrapped rows. Table headers are bold, and inline styles also apply inside cells.

Cell width treats combining marks, enclosing marks, variation selectors, and zero-width joiners as zero-width. A fixed set of East Asian and emoji ranges is treated as double-width; other runes are single-width. This is rune-based, not full grapheme-cluster layout.

`dark` and `light` are distinct palettes: the light theme substitutes darker foregrounds so text stays legible against a light background. `auto` reads `COLORFGBG`, whose last field is the background colour index: 0-6 and 8 select `dark`, other values select `light`, and an absent or unparseable variable defaults to `dark`. Styling uses 256-colour sequences for code and inline code, plus standard bold, italic, strike, underline, and reverse-video sequences. `--no-color` disables these SGR sequences but deliberately does not disable OSC 8 hyperlinks, which remain useful on a monochrome terminal.

## 6. Links

Markdown links retain their labels. Bare HTTP and HTTPS URLs use the URL itself as label and target. A target is emitted as an OSC 8 hyperlink only when:

- it contains no C0 control character or DEL;
- `net/url` can parse it; and
- its scheme is exactly `http`, `https`, or `mailto`.

Invalid, relative, and unsupported-scheme targets remain visible but are not clickable. Targets are not resolved against the source directory, and the application never opens links itself.

## 7. Search

Search is a literal substring search over each rendered row's visible `SearchText`, including layout prefixes and padding but excluding Markdown delimiters and terminal escape sequences.

Search is case-insensitive unless the query contains an uppercase Unicode letter. Matching uses Go string byte offsets and `strings.ToLower`; it performs no Unicode normalization. Matches do not overlap.

Typing after `/` or `?` updates matches and moves the viewport immediately. Enter accepts a non-empty query as the last query. Esc restores the viewport saved on entering search. All matches are highlighted and one active match is highlighted distinctly. Directional navigation selects the next match on a later rendered row, or the previous match on an earlier rendered row; it wraps at document boundaries and reports wrapping. Multiple matches on the same row are highlighted but skipped as separate navigation stops.

## 8. Editor and reload

`v` uses the first mapped rendered row at or below the top of the viewport and selects `$VISUAL`, then `$EDITOR`, then `vi`. The environment value is split without a shell. Whitespace, single and double quotes, and backslash escaping are supported; shell operators and unterminated quoting are rejected.

Editor arguments are:

| Editor basename | Added arguments |
|---|---|
| `vi`, `vim`, `nvim`, `nano` | `+LINE FILE` |
| `emacs`, `emacsclient` | `+LINE:1 FILE` |
| `code`, `codium` | `--goto FILE:LINE:1` |
| `subl`, `mate` | `FILE:LINE` |
| any other editor | `FILE` |

The viewer restores the terminal, runs the editor synchronously with the controlling terminal attached, then re-enters the viewer and reloads the original file unconditionally. Reload revalidates the extension, regular-file status, and size limit. The viewport is restored to the rendered row nearest the saved source line. Reload errors appear as transient messages.

## 9. Diff mode

```text
mdv [options] diff OLD NEW
```

`diff` as the first positional argument compares two files instead of rendering one. Both files may be of any type: the `.md`/`.markdown` requirement is not applied, though the regular-file and 32 MiB limits still are. `diff` given without exactly two further paths is a usage error, which also means a file named literally `diff` cannot be opened by the single-file form; such a file has no Markdown extension and would be refused anyway.

Additional options:

```text
-U, --context N        unchanged lines kept around a change (default 3)
    --no-fold          show all unchanged lines instead of folding them
    --no-split         use one column instead of two panes
    --no-word-diff     do not highlight changes within a line
    --raw              compare Markdown as text, not as rendered blocks
    --md               compare as rendered Markdown whatever the extensions
```

Context must not be negative. `-w`, `-s`, `-l`, and `--no-color` keep their meanings; `-l` shows both files' line numbers, each in its own gutter sized to the widest number the diff contains.

Differences are computed line by line. A deletion immediately followed by an insertion is treated as a rewrite and the two runs are paired opposite each other, leftover lines on the longer side trailing as pure removals or additions. Within a paired line, a word-level difference is highlighted, unless less than a quarter of the two lines is common text, in which case the whole line is marked changed rather than scattering fragments across it.

Runs of unchanged lines further than the context window from any change collapse into a single row naming how many lines are hidden. Runs shorter than three lines are left visible, since folding them saves nothing. Nothing is ever hidden by folding that cannot be recovered: expanding restores exactly the alignment that `--no-fold` would have produced.

Side-by-side is the default and is used whenever each pane can have at least 24 columns; below that the view falls back to one column regardless of `--no-split`. In two panes each side wraps independently and a row occupies as many physical rows as its taller side, so the panes stay in step. In one column a rewritten line becomes two rows, the old line then the new. Lines are broken at the pane edge rather than at word boundaries, and whitespace is never dropped: indentation is meaningful in a diff. Tabs are expanded to four-column stops.

Removals are marked `-` and additions `+` in a two-column marker, so the view stays readable under `--no-color`.

The status line is:

```text
OLD → NEW  PERCENT%  +ADDED -REMOVED
```

`identical` replaces the counts when the files do not differ. A rewritten line counts as both an addition and a removal.

Keys added in diff mode:

| Key | Action |
|---|---|
| `]`, `[` | Move to the next or previous hunk |
| `x` | Expand the first folded run at or below the top of the view |
| `X` | Expand every fold |
| `z` | Collapse unchanged context again |

`l` also works here, toggling both panes' gutters at once.

A run of adjacent changed rows is one hunk. Hunk navigation does not wrap; it reports when there is nothing further in that direction. Expanding a fold below the top of the view leaves the view where it is. `h` shows the diff key summary instead of the viewer's.

`v` opens the new file, at the line the row maps to; a row that exists only in the old file maps to its old line number. `r` re-reads both files. Search, paging, resize, and reflow behave as in the viewer, and search matches are recomputed whenever folding or a resize rebuilds the rows.

### 9.1 Markdown-aware comparison

Two files whose extensions are both `.md`/`.markdown` are compared as **rendered documents** rather than as lines of text. `--raw` forces the line comparison; `--md` forces the Markdown comparison whatever the extensions. Giving both is a usage error.

The unit of comparison is a parsed block — a paragraph, heading, quote, list item, or a whole table — rather than a source line. Because the parser joins a paragraph's lines with single spaces, reflowing a paragraph produces an identical block and therefore **no difference at all**, and editing one word inside a wrapped paragraph is one changed block rather than one changed line for every line the edit reflowed. Two blocks are the same only if their kind, heading level, prefix, table-header flag, and every inline's kind, text and link target match; a changed link target is a difference even though the visible text is unchanged.

A table is compared as a single unit, because its columns are sized across all its rows: one changed cell marks the whole table changed.

Unchanged blocks are drawn once across the full width, since both sides are identical and two half-width copies would wrap prose needlessly. Only changed blocks are drawn in two panes. A horizontal rule marks every transition between a full-width section and a split one, and closes a split section that reaches the end of the document. Below the two-pane width threshold, a changed block is drawn stacked: the old version, then the new.

Markdown styling survives the comparison: a changed heading keeps its heading colour and gains the added or removed background behind it.

In this mode a row counts as a block, so the fold marker reads "N unchanged blocks" and the status summary reads `+N -M blocks`.

## 10. Terminal lifecycle

The Darwin backend saves and restores termios, disables echo, canonical mode, signals, extended processing, CR translation, software flow control, and output post-processing, and makes enter/leave idempotent. Escape-sequence decoding supports arrows, Page Up/Down, Home, and End. A 35 ms readiness check distinguishes a bare Escape key.

`SIGWINCH` updates the stored size, reflows the document, and redraws. `SIGINT`, `SIGTERM`, and `SIGHUP` exit cleanly through deferred terminal restoration. A recovered application panic restores the terminal and is rethrown as `mdv: internal panic`.
