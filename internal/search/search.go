// Package search implements literal substring search over rendered rows. It
// searches each row's SearchText, which is the visible text including layout
// prefixes and padding but excluding Markdown delimiters and escape sequences.
package search

import (
	"strings"
	"unicode"

	"github.com/schambon/mdv/internal/layout"
)

// Direction is the direction of travel through matches.
type Direction int

const (
	Forward Direction = iota
	Backward
)

// Match is one occurrence, located by rendered row and byte offsets into that
// row's SearchText.
type Match struct {
	Line  int
	Start int
	End   int
}

// Find returns every match in document order. Matching is literal: there is no
// regular-expression syntax and no Unicode normalization. It is case
// insensitive unless the query contains an uppercase letter.
func Find(d layout.Document, query string) []Match {
	if query == "" {
		return nil
	}
	fold := !hasUpper(query)
	needle := query
	if fold {
		needle = strings.ToLower(query)
	}

	var matches []Match
	for i, line := range d.Lines {
		haystack := line.SearchText
		if fold {
			// A few runes change byte length when lowercased, which would
			// shift every offset on the row; those rows stay case sensitive.
			if lowered := strings.ToLower(haystack); len(lowered) == len(haystack) {
				haystack = lowered
			}
		}
		// Searching resumes past each match, so occurrences never overlap.
		from := 0
		for {
			at := strings.Index(haystack[from:], needle)
			if at < 0 {
				break
			}
			start := from + at
			end := start + len(needle)
			matches = append(matches, Match{Line: i, Start: start, End: end})
			from = end
		}
	}
	return matches
}

// hasUpper reports whether the query contains an uppercase letter, which turns
// smart case off.
func hasUpper(s string) bool {
	return strings.ContainsFunc(s, unicode.IsUpper)
}

// Next selects the match to travel to from the given rendered row, and reports
// whether the selection wrapped around the document. Rows are compared whole:
// several matches on one row are highlighted but are not separate stops.
func Next(matches []Match, line int, dir Direction) (index int, wrapped bool, ok bool) {
	if len(matches) == 0 {
		return 0, false, false
	}

	if dir == Forward {
		for i, m := range matches {
			if m.Line > line {
				return i, false, true
			}
		}
		return 0, true, true
	}

	for i := len(matches) - 1; i >= 0; i-- {
		if matches[i].Line < line {
			// Land on the first match of that row, so repeated presses do not
			// stall on a row holding several matches.
			for i > 0 && matches[i-1].Line == matches[i].Line {
				i--
			}
			return i, false, true
		}
	}
	last := len(matches) - 1
	for last > 0 && matches[last-1].Line == matches[last].Line {
		last--
	}
	return last, true, true
}

// First returns the index of the match at or after a row, used when a search
// begins. It never wraps past the end.
func First(matches []Match, line int) int {
	for i, m := range matches {
		if m.Line >= line {
			return i
		}
	}
	return 0
}

// Highlight returns a copy of a rendered row with the given matches restyled.
// The matches must be those falling on this row, and active is an index into
// that slice, or -1 when the active match is elsewhere. Spans are split at
// match boundaries and keep their link targets, so a match inside a link stays
// clickable.
func Highlight(line layout.RenderedLine, matches []Match, active int) layout.RenderedLine {
	if len(matches) == 0 {
		return line
	}

	out := line
	out.Spans = nil
	offset := 0

	for _, span := range line.Spans {
		start, end := offset, offset+len(span.Text)
		offset = end

		// Cut points inside this span, in order.
		cuts := []int{start}
		for _, m := range matches {
			for _, at := range []int{m.Start, m.End} {
				if at > start && at < end {
					cuts = append(cuts, at)
				}
			}
		}
		cuts = append(cuts, end)
		sortInts(cuts)

		for i := 0; i+1 < len(cuts); i++ {
			from, to := cuts[i], cuts[i+1]
			if from == to {
				continue
			}
			piece := span
			piece.Text = span.Text[from-start : to-start]
			piece.Cells = layout.Width(piece.Text)
			if style, ok := matchStyle(matches, active, from); ok {
				piece.Style = style
			}
			out.Spans = append(out.Spans, piece)
		}
	}

	return out
}

// matchStyle reports the highlight style covering an offset, if any.
func matchStyle(matches []Match, active, offset int) (layout.Style, bool) {
	for i, m := range matches {
		if offset >= m.Start && offset < m.End {
			if i == active {
				return layout.StyleSearchActive, true
			}
			return layout.StyleSearch, true
		}
	}
	return 0, false
}

func sortInts(xs []int) {
	for i := 1; i < len(xs); i++ {
		for j := i; j > 0 && xs[j] < xs[j-1]; j-- {
			xs[j], xs[j-1] = xs[j-1], xs[j]
		}
	}
}
