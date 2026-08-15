# mdv — Implemented Requirements

## 1. Purpose and scope

`mdv` is an interactive, standard-library-only Markdown viewer for macOS terminals. It displays one local Markdown file in an alternate-screen pager, supports literal search, emits OSC 8 hyperlinks for safe external URLs, and can open the source file in an editor.

The implementation is intentionally a bounded Markdown renderer, not a CommonMark or GitHub Flavored Markdown implementation. HTML, standard input, multiple files, file watching, local-link resolution, syntax highlighting, and non-macOS terminals are not supported.

## 2. Command line

```text
mdv [options] FILE
```

`FILE` is required and must name a regular file whose extension, matched case-insensitively, is `.md` or `.markdown`. The file must be no larger than 32 MiB. Relative paths are converted to absolute paths before use.

Implemented options:

```text
-w, --width N          Maximum rendering width; 0 uses terminal width
-s, --style STYLE      auto, dark, or light
-l, --line-numbers     Show source line numbers
    --no-color         Disable SGR styling
-V, --version          Print the version and exit
-h                     Print flag help and exit
```

Width must be non-negative. A positive width caps rendering only when it is narrower than the terminal. Invalid arguments return exit status 2; startup or runtime errors return 1; normal completion returns 0.

Both stdin and stdout must be interactive character devices. The path is validated before a terminal is required, so a bad filename reports the file problem rather than a terminal problem. On platforms other than macOS, startup fails with an unsupported-platform error.

## 3. Viewer

The viewer enters raw mode, switches to the alternate screen, hides the cursor, reserves the last terminal row for status, and redraws the complete frame after each event. Terminal resize events update the viewport dimensions, reflow the document at the new width, and restore the viewport to the rendered row nearest the source line that was showing. Terminal widths below 10 or failed size reads fall back to 80 columns; heights below 2 fall back to 24 rows.

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

## 9. Terminal lifecycle

The Darwin backend saves and restores termios, disables echo, canonical mode, signals, extended processing, CR translation, software flow control, and output post-processing, and makes enter/leave idempotent. Escape-sequence decoding supports arrows, Page Up/Down, Home, and End. A 35 ms readiness check distinguishes a bare Escape key.

`SIGWINCH` updates the stored size, reflows the document, and redraws. `SIGINT`, `SIGTERM`, and `SIGHUP` exit cleanly through deferred terminal restoration. A recovered application panic restores the terminal and is rethrown as `mdv: internal panic`.
