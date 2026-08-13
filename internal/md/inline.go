package md

import (
	"regexp"
	"strings"

	"github.com/schambon/mdv/internal/doc"
)

// bareURL matches an unadorned http(s) URL. The trailing class excludes
// punctuation that more often ends a sentence than a URL.
var bareURL = regexp.MustCompile(`^https?://[^\s<>]+[^\s<>.,;:!?)]`)

// pairedMarkers are tried in order, so the two-byte markers win over the
// one-byte ones that prefix them.
var pairedMarkers = []struct {
	marker string
	kind   doc.InlineKind
}{
	{"**", doc.InlineStrong},
	{"__", doc.InlineStrong},
	{"~~", doc.InlineStrike},
	{"`", doc.InlineCode},
	{"*", doc.InlineEmphasis},
	{"_", doc.InlineEmphasis},
}

// constructBytes are the bytes that may begin a non-text inline. Literal text
// runs stop at one of these so the scanner can retry.
const constructBytes = "[*_~`h"

// ParseInline scans a single logical line of inline Markdown. It is exported
// because table layout re-parses individual cells with the same rules.
//
// The scan is a single left-to-right pass: contents of a paired marker are
// stored verbatim and never recursively parsed, there is no delimiter stack,
// and there is no escape processing. Unmatched syntax survives as literal text.
// Offset is the byte position of the text within the source file, used to give
// each inline a source range.
func ParseInline(text string, offset int, pos func(int) doc.Position) []doc.Inline {
	var inlines []doc.Inline
	add := func(kind doc.InlineKind, body, target string, start, end int) {
		inlines = append(inlines, doc.Inline{
			Kind:   kind,
			Text:   body,
			Target: target,
			Source: doc.SourceRange{Start: pos(offset + start), End: pos(offset + end)},
		})
	}

	i := 0
	for i < len(text) {
		rest := text[i:]

		if label, target, size, ok := matchLink(rest); ok {
			add(doc.InlineLink, label, target, i, i+size)
			i += size
			continue
		}

		if kind, body, size, ok := matchPaired(rest); ok {
			add(kind, body, "", i, i+size)
			i += size
			continue
		}

		if url := bareURL.FindString(rest); url != "" {
			add(doc.InlineLink, url, url, i, i+len(url))
			i += len(url)
			continue
		}

		// Literal text up to the next byte that might start a construct.
		// Always consume at least one byte so the scan cannot stall.
		end := i + 1
		for end < len(text) && !strings.ContainsRune(constructBytes, rune(text[end])) {
			end++
		}
		add(doc.InlineText, text[i:end], "", i, end)
		i = end
	}

	return mergeText(inlines)
}

// matchLink recognizes [label](target) using the first "](" and the ")" that
// follows it.
func matchLink(s string) (label, target string, size int, ok bool) {
	if len(s) == 0 || s[0] != '[' {
		return "", "", 0, false
	}
	mid := strings.Index(s, "](")
	if mid < 0 {
		return "", "", 0, false
	}
	closeIdx := strings.Index(s[mid+2:], ")")
	if closeIdx < 0 {
		return "", "", 0, false
	}
	end := mid + 2 + closeIdx + 1
	return s[1:mid], s[mid+2 : end-1], end, true
}

// matchPaired recognizes a marker closed by the next identical marker.
func matchPaired(s string) (kind doc.InlineKind, body string, size int, ok bool) {
	for _, m := range pairedMarkers {
		if !strings.HasPrefix(s, m.marker) {
			continue
		}
		rest := s[len(m.marker):]
		closeIdx := strings.Index(rest, m.marker)
		// An empty body means the delimiters would render as nothing at all,
		// so treat it as unmatched and let the text stay visible instead.
		if closeIdx <= 0 {
			continue // unmatched: fall through to another marker or literal text
		}
		return m.kind, rest[:closeIdx], len(m.marker)*2 + closeIdx, true
	}
	return 0, "", 0, false
}

// mergeText coalesces adjacent literal runs, which the scanner emits in small
// pieces whenever it retries at a construct byte.
func mergeText(inlines []doc.Inline) []doc.Inline {
	if len(inlines) < 2 {
		return inlines
	}
	merged := inlines[:1]
	for _, in := range inlines[1:] {
		last := &merged[len(merged)-1]
		if last.Kind == doc.InlineText && in.Kind == doc.InlineText {
			last.Text += in.Text
			last.Source.End = in.Source.End
			continue
		}
		merged = append(merged, in)
	}
	return merged
}
