# mdv

`mdv` is a small, dependency-free Markdown pager for macOS terminals. It renders a practical Markdown subset with colour, paging, literal search, OSC 8 web links, source line numbers, and `$VISUAL`/`$EDITOR` integration.

```sh
go build -o mdv ./cmd/mdv
install mdv /usr/local/bin/mdv
mdv README.md
```

The first release accepts one real `.md` or `.markdown` file and requires interactive stdin and stdout. Run `mdv --help` for options. Keys are shown with `h` inside the viewer.

Supported Markdown includes headings, paragraphs, rules, fenced and indented code, quotes, lists and task lists, simple pipe tables, emphasis, strong, strikethrough, code spans, inline links, and bare HTTP(S) URLs. Code is styled without syntax highlighting. Reference links, footnotes, inline HTML, complex nesting, stdin, multiple files, watching, and local-link activation are deferred.
