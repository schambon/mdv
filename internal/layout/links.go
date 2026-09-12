package layout

import "github.com/schambon/mdv/internal/doc"

// Link collection lives in package layout for the same reason table sizing and
// the frontmatter key column do: it reads the Span model, and a separate
// package would have to have Span, Cells and the row list exported to it. It is
// the inverse of rendering — given rows, find where each link ended up — and it
// is what lets the viewer select a link without knowing how one was wrapped.

// Ref locates one span of a link within a laid-out document.
type Ref struct {
	Line int
	Span int
}

// Link is one link occurrence. A link whose label wraps across rows, or whose
// label was split into several runs by wrapping, has one Ref per span it
// occupies; Refs is never empty and is in row order.
type Link struct {
	Target string
	Source doc.SourceRange
	Refs   []Ref
}

// Links collects every link in a laid-out document, in row order.
//
// Spans belonging to one link are recognised by carrying the same target *and*
// the same source range: runs takes both from the whole inline, so every piece
// of one link shares them, while `[a](x)[b](x)` is two inlines with two ranges
// and stays two links. Only the link built most recently can be extended, and
// the runs of one inline are emitted contiguously, so no unrelated span can
// come between them — which is why this needs no adjacency test, and why a
// label that wraps onto a continuation row still joins across the prefix span
// that opens it.
//
// The one place the range is coarser than a link is inside a table cell, whose
// inlines all share the cell's start position (see parseCells): two identical
// links in one cell collapse into one. That costs a Tab stop and nothing else,
// both halves pointing at the same target.
func Links(d Document) []Link {
	var links []Link

	for i, line := range d.Lines {
		for j, span := range line.Spans {
			if span.LinkTarget == "" {
				continue
			}
			if last := len(links) - 1; last >= 0 &&
				links[last].Target == span.LinkTarget &&
				links[last].Source == span.Source {
				links[last].Refs = append(links[last].Refs, Ref{Line: i, Span: j})
				continue
			}
			links = append(links, Link{
				Target: span.LinkTarget,
				Source: span.Source,
				Refs:   []Ref{{Line: i, Span: j}},
			})
		}
	}
	return links
}
