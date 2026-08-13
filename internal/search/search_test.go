package search

import (
	"testing"

	"github.com/schambon/mdv/internal/layout"
)

// document builds a rendered document from plain row texts.
func document(rows ...string) layout.Document {
	d := layout.Document{}
	for _, row := range rows {
		d.Lines = append(d.Lines, layout.RenderedLine{
			SearchText: row,
			Spans:      []layout.Span{{Text: row, Cells: layout.Width(row)}},
		})
	}
	return d
}

func TestFindLiteral(t *testing.T) {
	d := document("alpha beta", "gamma", "beta again")
	got := Find(d, "beta")
	if len(got) != 2 {
		t.Fatalf("got %d matches, want 2: %+v", len(got), got)
	}
	if got[0].Line != 0 || got[0].Start != 6 || got[0].End != 10 {
		t.Errorf("first match = %+v", got[0])
	}
	if got[1].Line != 2 || got[1].Start != 0 {
		t.Errorf("second match = %+v", got[1])
	}
}

func TestFindEmptyQuery(t *testing.T) {
	if got := Find(document("text"), ""); got != nil {
		t.Errorf("got %+v, want no matches", got)
	}
}

func TestFindIsLiteralNotRegex(t *testing.T) {
	d := document("a.c", "abc")
	got := Find(d, "a.c")
	if len(got) != 1 || got[0].Line != 0 {
		t.Errorf("got %+v, want only the literal match", got)
	}
}

func TestSmartCase(t *testing.T) {
	d := document("Alpha", "alpha", "ALPHA")

	if got := Find(d, "alpha"); len(got) != 3 {
		t.Errorf("lowercase query matched %d rows, want all 3", len(got))
	}
	if got := Find(d, "Alpha"); len(got) != 1 || got[0].Line != 0 {
		t.Errorf("mixed-case query matched %+v, want only the exact row", got)
	}
	if got := Find(d, "ALPHA"); len(got) != 1 || got[0].Line != 2 {
		t.Errorf("uppercase query matched %+v, want only the exact row", got)
	}
}

func TestMatchesDoNotOverlap(t *testing.T) {
	d := document("aaaa")
	got := Find(d, "aa")
	if len(got) != 2 {
		t.Fatalf("got %d matches, want 2 non-overlapping: %+v", len(got), got)
	}
	if got[0].End > got[1].Start {
		t.Errorf("matches overlap: %+v", got)
	}
}

func TestFindSearchesPrefixesAndPadding(t *testing.T) {
	// SearchText includes layout prefixes, so they are searchable.
	d := document("  │ quoted text")
	if got := Find(d, "│"); len(got) != 1 {
		t.Errorf("prefix not searchable: %+v", got)
	}
}

func TestNextForward(t *testing.T) {
	matches := []Match{{Line: 1}, {Line: 3}, {Line: 5}}

	i, wrapped, ok := Next(matches, 0, Forward)
	if !ok || wrapped || i != 0 {
		t.Errorf("from row 0: i=%d wrapped=%v ok=%v, want 0/false/true", i, wrapped, ok)
	}
	i, wrapped, _ = Next(matches, 1, Forward)
	if i != 1 || wrapped {
		t.Errorf("from row 1: i=%d wrapped=%v, want 1/false", i, wrapped)
	}
	i, wrapped, _ = Next(matches, 5, Forward)
	if i != 0 || !wrapped {
		t.Errorf("from the last match: i=%d wrapped=%v, want 0/true", i, wrapped)
	}
}

func TestNextBackward(t *testing.T) {
	matches := []Match{{Line: 1}, {Line: 3}, {Line: 5}}

	i, wrapped, _ := Next(matches, 4, Backward)
	if i != 1 || wrapped {
		t.Errorf("from row 4: i=%d wrapped=%v, want 1/false", i, wrapped)
	}
	i, wrapped, _ = Next(matches, 1, Backward)
	if i != 2 || !wrapped {
		t.Errorf("from the first match: i=%d wrapped=%v, want 2/true", i, wrapped)
	}
}

func TestNextNoMatches(t *testing.T) {
	if _, _, ok := Next(nil, 0, Forward); ok {
		t.Error("Next reported a match in an empty set")
	}
}

// Several matches on one row are one navigation stop, not many.
func TestNextSkipsSameRowMatches(t *testing.T) {
	matches := []Match{{Line: 2, Start: 0}, {Line: 2, Start: 5}, {Line: 4}}

	i, _, _ := Next(matches, 2, Forward)
	if i != 2 {
		t.Errorf("forward from row 2: i=%d, want the row-4 match", i)
	}
	i, _, _ = Next(matches, 4, Backward)
	if i != 0 {
		t.Errorf("backward from row 4: i=%d, want the first match on row 2", i)
	}
}

func TestFirst(t *testing.T) {
	matches := []Match{{Line: 1}, {Line: 3}, {Line: 5}}
	if got := First(matches, 0); got != 0 {
		t.Errorf("First(0) = %d, want 0", got)
	}
	if got := First(matches, 3); got != 1 {
		t.Errorf("First(3) = %d, want 1", got)
	}
	if got := First(matches, 99); got != 0 {
		t.Errorf("First past the end = %d, want 0", got)
	}
}

func TestHighlightSplitsSpans(t *testing.T) {
	line := layout.RenderedLine{
		SearchText: "alpha beta",
		Spans:      []layout.Span{{Text: "alpha beta", Cells: 10}},
	}
	got := Highlight(line, []Match{{Line: 0, Start: 6, End: 10}}, 0)

	if len(got.Spans) != 2 {
		t.Fatalf("got %d spans, want 2: %+v", len(got.Spans), got.Spans)
	}
	if got.Spans[0].Text != "alpha " || got.Spans[0].Style != layout.StyleNone {
		t.Errorf("first span = %+v", got.Spans[0])
	}
	if got.Spans[1].Text != "beta" || got.Spans[1].Style != layout.StyleSearchActive {
		t.Errorf("second span = %+v", got.Spans[1])
	}
}

func TestHighlightActiveVersusOther(t *testing.T) {
	line := layout.RenderedLine{
		SearchText: "aa bb aa",
		Spans:      []layout.Span{{Text: "aa bb aa", Cells: 8}},
	}
	matches := []Match{{Line: 0, Start: 0, End: 2}, {Line: 0, Start: 6, End: 8}}
	got := Highlight(line, matches, 1)

	var active, other int
	for _, s := range got.Spans {
		switch s.Style {
		case layout.StyleSearchActive:
			active++
		case layout.StyleSearch:
			other++
		}
	}
	if active != 1 || other != 1 {
		t.Errorf("got %d active and %d other highlights, want 1 and 1", active, other)
	}
}

// Splitting a span must not drop its link target.
func TestHighlightPreservesLinkTarget(t *testing.T) {
	line := layout.RenderedLine{
		SearchText: "click here",
		Spans: []layout.Span{{
			Text: "click here", Cells: 10,
			Style: layout.StyleLink, LinkTarget: "https://example.com",
		}},
	}
	got := Highlight(line, []Match{{Line: 0, Start: 6, End: 10}}, 0)

	for _, s := range got.Spans {
		if s.LinkTarget != "https://example.com" {
			t.Errorf("span %q lost its target", s.Text)
		}
	}
}

func TestHighlightAcrossSpanBoundary(t *testing.T) {
	line := layout.RenderedLine{
		SearchText: "onetwo",
		Spans: []layout.Span{
			{Text: "one", Cells: 3},
			{Text: "two", Cells: 3, Style: layout.StyleStrong},
		},
	}
	got := Highlight(line, []Match{{Line: 0, Start: 2, End: 4}}, 0)

	var rebuilt string
	for _, s := range got.Spans {
		rebuilt += s.Text
	}
	if rebuilt != "onetwo" {
		t.Errorf("rebuilt %q, want onetwo", rebuilt)
	}

	highlighted := ""
	for _, s := range got.Spans {
		if s.Style == layout.StyleSearchActive {
			highlighted += s.Text
		}
	}
	if highlighted != "et" {
		t.Errorf("highlighted %q, want et", highlighted)
	}
}

func TestHighlightNoMatchesIsUnchanged(t *testing.T) {
	line := layout.RenderedLine{
		SearchText: "text",
		Spans:      []layout.Span{{Text: "text", Cells: 4}},
	}
	if got := Highlight(line, nil, -1); len(got.Spans) != 1 {
		t.Errorf("got %d spans, want the row untouched", len(got.Spans))
	}
}

// Cell counts must be recomputed after a split, or wide runes break alignment.
func TestHighlightRecomputesCells(t *testing.T) {
	line := layout.RenderedLine{
		SearchText: "漢字ab",
		Spans:      []layout.Span{{Text: "漢字ab", Cells: 6}},
	}
	got := Highlight(line, []Match{{Line: 0, Start: 6, End: 8}}, 0)

	total := 0
	for _, s := range got.Spans {
		total += s.Cells
	}
	if total != 6 {
		t.Errorf("cells total %d, want 6", total)
	}
}
