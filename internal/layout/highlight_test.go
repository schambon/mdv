package layout

import (
	"strings"
	"testing"
)

// A fenced block with a known language splits its lines into styled token runs,
// so at least one span carries a code-token style beyond the base StyleCode.
func TestHighlightAppliesTokenStyles(t *testing.T) {
	d := render(t, "```go\nfunc main() {} // hi\n```\n", Options{Width: 60})
	seen := map[Style]bool{}
	for _, line := range d.Lines {
		for _, s := range line.Spans {
			seen[s.Style] = true
		}
	}
	for _, want := range []Style{StyleCodeKeyword, StyleCodeComment} {
		if !seen[want] {
			t.Errorf("expected a span of style %v, got styles %v", want, seen)
		}
	}
}

// An unknown language must render byte-for-byte like today: every code span is
// StyleCode and the visible text is unchanged.
func TestHighlightUnknownLangUnchanged(t *testing.T) {
	src := "```nolang\nint x = 1; // plain\n  indented\n```\n"
	d := render(t, src, Options{Width: 60})
	want := []string{"      int x = 1; // plain", "        indented"}
	got := texts(d)
	if len(got) != len(want) {
		t.Fatalf("got %d rows, want %d: %q", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("row %d = %q, want %q", i, got[i], want[i])
		}
	}
	for _, line := range d.Lines {
		for _, s := range line.Spans {
			if strings.TrimSpace(s.Text) == "" {
				continue // the leading indent span is StyleCode too
			}
			if s.Style != StyleCode {
				t.Errorf("unknown-lang span %q has style %v, want StyleCode", s.Text, s.Style)
			}
		}
	}
}

// SearchText must still equal the concatenation of spans once a line is split
// into many token runs, and no highlighted row may exceed the width.
func TestHighlightPreservesInvariants(t *testing.T) {
	src := "```go\n" + strings.Repeat("func Println(x) { return 42 } // comment ", 4) + "\n```\n"
	for _, w := range []int{20, 40, 80} {
		d := render(t, src, Options{Width: w})
		for i, line := range d.Lines {
			var sb strings.Builder
			cells := 0
			for _, s := range line.Spans {
				sb.WriteString(s.Text)
				cells += s.Cells
			}
			if sb.String() != line.SearchText {
				t.Errorf("width %d row %d: spans != SearchText", w, i)
			}
			if cells > w {
				t.Errorf("width %d row %d: %d cells exceeds width", w, i, cells)
			}
		}
	}
}
