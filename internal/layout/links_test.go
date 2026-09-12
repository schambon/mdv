package layout

import "testing"

// targets is each collected link's target, for asserting order and count.
func targets(links []Link) []string {
	out := make([]string, len(links))
	for i, l := range links {
		out[i] = l.Target
	}
	return out
}

func TestLinksAreCollectedInRowOrder(t *testing.T) {
	d := render(t, "[one](a.md) then [two](b.md)\n\n[three](c.md)\n", Options{Width: 60})
	got := targets(Links(d))
	want := []string{"a.md", "b.md", "c.md"}

	if len(got) != len(want) {
		t.Fatalf("Links found %d links (%v), want %d", len(got), got, len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("link %d = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestLinksEmptyDocument(t *testing.T) {
	if got := Links(render(t, "no links at all here\n", Options{})); got != nil {
		t.Errorf("Links = %v, want nil", got)
	}
}

// A label wrapped across rows is one link, not one per row: it is a single
// place in the document, and Tab must stop on it once.
func TestLinksJoinALabelWrappedAcrossRows(t *testing.T) {
	d := render(t, "[a label long enough to wrap onto another row](target.md)\n", Options{Width: 20})

	links := Links(d)
	if len(links) != 1 {
		t.Fatalf("Links found %d links (%v), want 1", len(links), targets(links))
	}
	rows := map[int]bool{}
	for _, ref := range links[0].Refs {
		rows[ref.Line] = true
	}
	if len(rows) < 2 {
		t.Fatalf("link occupies %d rows, want it wrapped across at least 2", len(rows))
	}
	if links[0].Refs[0].Line != 0 {
		t.Errorf("first Ref is on row %d, want the first row", links[0].Refs[0].Line)
	}
}

// A wrapped label inside a quote continues after the prefix span that opens
// each continuation row, so joining cannot depend on the link resuming at
// span 0.
func TestLinksJoinAcrossAContinuationPrefix(t *testing.T) {
	d := render(t, "> [a label long enough to wrap onto another row](target.md)\n", Options{Width: 20})

	links := Links(d)
	if len(links) != 1 {
		t.Fatalf("Links found %d links (%v), want 1", len(links), targets(links))
	}
}

// Two links sharing a target are still two links: they are different places,
// and they differ by source range even though the target matches.
func TestLinksWithTheSameTargetStayDistinct(t *testing.T) {
	d := render(t, "[one](same.md) and [two](same.md)\n", Options{Width: 60})
	if got := Links(d); len(got) != 2 {
		t.Errorf("Links found %d links (%v), want 2", len(got), targets(got))
	}
}

// Every Ref must point at a span that really carries the link, or banding the
// selection would colour the wrong text.
func TestLinkRefsPointAtTheirOwnSpans(t *testing.T) {
	d := render(t, "text [one](a.md) more [two](b.md) end\n", Options{Width: 60})

	for _, l := range Links(d) {
		if len(l.Refs) == 0 {
			t.Fatalf("link %q has no refs", l.Target)
		}
		for _, ref := range l.Refs {
			if ref.Line >= len(d.Lines) {
				t.Fatalf("ref %+v is past the document", ref)
			}
			spans := d.Lines[ref.Line].Spans
			if ref.Span >= len(spans) {
				t.Fatalf("ref %+v is past the row", ref)
			}
			if got := spans[ref.Span].LinkTarget; got != l.Target {
				t.Errorf("ref %+v carries target %q, want %q", ref, got, l.Target)
			}
		}
	}
}

// Bare URLs are links too, and a document mixing them with explicit ones must
// collect both.
func TestLinksIncludeBareURLs(t *testing.T) {
	d := render(t, "see https://example.com and [docs](d.md)\n", Options{Width: 60})
	got := targets(Links(d))
	if len(got) != 2 || got[0] != "https://example.com" || got[1] != "d.md" {
		t.Errorf("Links = %v, want the bare URL then d.md", got)
	}
}
