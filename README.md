# mdv

`mdv` is a small, dependency-free Markdown pager for macOS terminals. It renders a practical Markdown subset with colour, paging, literal search, OSC 8 web links, source line numbers, and `$VISUAL`/`$EDITOR` integration.

```sh
go build -o mdv ./cmd/mdv
install mdv /usr/local/bin/mdv
mdv README.md
```

The first release accepts one real `.md` or `.markdown` file and requires interactive stdin and stdout. Run `mdv --help` for options. Keys are shown with `h` inside the viewer.

`mdv diff OLD NEW` compares two files of any type in the same pager:

```sh
mdv -l diff old.go new.go
```

It shows a side-by-side view with per-file line numbers, `-`/`+` markers, word-level highlighting inside rewritten lines, and unchanged context folded behind `⋯ N unchanged lines` markers. `[` and `]` jump between hunks, `x` opens the next fold, `X` opens all of them, `z` collapses them again, and `l` toggles the line-number gutters. `--no-split` gives a classic one-column diff, `--no-fold` shows every line, and `-U N` sets the context window. Narrow terminals fall back to one column automatically.

Two Markdown files are compared as *rendered Markdown*: the unit is a block, so reflowing a paragraph is not a change and a one-word edit shows as one word rather than as every line below it. Unchanged blocks are drawn once at full width and only changed ones split into panes. `--raw` asks for the literal line diff instead, `--md` forces the Markdown comparison on.

`mdv git` compares a file's two versions in a repository:

```sh
mdv git                # the index against the working tree
mdv git --staged       # HEAD against the index
mdv git HEAD~3 README.md
mdv git v1.2..v1.3
```

Git supplies the file contents; mdv computes the difference itself, so folding, expansion, search and the Markdown comparison all work as they do on two files. When several files changed, they are listed in a sidebar and `<` and `>` — or the left and right arrows — move between them.

Supported Markdown includes headings, paragraphs, rules, fenced and indented code, quotes, lists and task lists, simple pipe tables, emphasis, strong, strikethrough, code spans, inline links, and bare HTTP(S) URLs. Code is styled without syntax highlighting. Reference links, footnotes, inline HTML, complex nesting, stdin, multiple files, watching, and local-link activation are deferred.
