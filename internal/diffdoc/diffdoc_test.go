package diffdoc

import (
	"math/rand"
	"strings"
	"testing"

	"github.com/schambon/mdv/internal/difftext"
)

func lines(s string) []string {
	if s == "" {
		return nil
	}
	return strings.Split(s, "\n")
}

// noFold builds without folding, so alignment can be tested on its own.
var noFold = Options{Context: -1, WordDiff: true}

// summarise renders rows compactly so a failure shows the whole alignment.
func summarise(rows []Row) string {
	var sb strings.Builder
	for _, r := range rows {
		switch r.Kind {
		case RowEqual:
			sb.WriteString("= " + r.Left.Text + "\n")
		case RowChanged:
			sb.WriteString("~ " + r.Left.Text + " | " + r.Right.Text + "\n")
		case RowAdded:
			sb.WriteString("+ " + r.Right.Text + "\n")
		case RowRemoved:
			sb.WriteString("- " + r.Left.Text + "\n")
		case RowFolded:
			sb.WriteString("⋯ " + string(rune('0'+len(r.Hidden))) + "\n")
		}
	}
	return sb.String()
}

// flatten expands every fold, giving the row sequence as if nothing were
// collapsed. Folding is presentation: the underlying alignment must be
// identical with and without it.
func flatten(rows []Row) []Row {
	var out []Row
	for _, r := range rows {
		if r.Kind == RowFolded {
			out = append(out, flatten(r.Hidden)...)
			continue
		}
		out = append(out, r)
	}
	return out
}

// checkRows asserts the invariants every aligned document must hold: line
// numbers ascend without gaps on each side, and a side is present exactly when
// its row kind says it should be.
func checkRows(t *testing.T, a, b []string, rows []Row) {
	t.Helper()

	leftSeen, rightSeen := 0, 0
	for i, r := range flatten(rows) {
		wantLeft := r.Kind == RowEqual || r.Kind == RowChanged || r.Kind == RowRemoved
		wantRight := r.Kind == RowEqual || r.Kind == RowChanged || r.Kind == RowAdded

		if got := r.Left.Present(); got != wantLeft {
			t.Fatalf("row %d (%v): left present = %v, want %v", i, r.Kind, got, wantLeft)
		}
		if got := r.Right.Present(); got != wantRight {
			t.Fatalf("row %d (%v): right present = %v, want %v", i, r.Kind, got, wantRight)
		}

		if wantLeft {
			leftSeen++
			if r.Left.Number != leftSeen {
				t.Fatalf("row %d: left line number %d, want %d", i, r.Left.Number, leftSeen)
			}
			if r.Left.Text != a[r.Left.Number-1] {
				t.Fatalf("row %d: left text %q, want %q", i, r.Left.Text, a[r.Left.Number-1])
			}
		}
		if wantRight {
			rightSeen++
			if r.Right.Number != rightSeen {
				t.Fatalf("row %d: right line number %d, want %d", i, r.Right.Number, rightSeen)
			}
			if r.Right.Text != b[r.Right.Number-1] {
				t.Fatalf("row %d: right text %q, want %q", i, r.Right.Text, b[r.Right.Number-1])
			}
		}
	}
}

// checkComplete additionally asserts every line of both files appears exactly
// once. Folding must hide lines, never lose them.
func checkComplete(t *testing.T, a, b []string, doc Document) {
	t.Helper()
	checkRows(t, a, b, doc.Rows)

	left, right := 0, 0
	for _, r := range flatten(doc.Rows) {
		if r.Left.Present() {
			left++
		}
		if r.Right.Present() {
			right++
		}
	}

	if left != len(a) || right != len(b) {
		t.Fatalf("document covers %d/%d left and %d/%d right lines",
			left, len(a), right, len(b))
	}
}

func TestAlignIdentical(t *testing.T) {
	a := lines("one\ntwo\nthree")
	doc := Build(a, a, noFold)
	checkComplete(t, a, a, doc)

	if doc.Changed() {
		t.Fatal("identical files should not report a change")
	}
	for _, r := range doc.Rows {
		if r.Kind != RowEqual {
			t.Fatalf("want all equal rows, got:\n%s", summarise(doc.Rows))
		}
	}
}

func TestAlignPairsRewrittenLines(t *testing.T) {
	a := lines("keep\nold line here\nkeep2")
	b := lines("keep\nnew line here\nkeep2")
	doc := Build(a, b, noFold)
	checkComplete(t, a, b, doc)

	want := "= keep\n~ old line here | new line here\n= keep2\n"
	if got := summarise(doc.Rows); got != want {
		t.Fatalf("got:\n%swant:\n%s", got, want)
	}
}

func TestAlignUnevenRewriteTrails(t *testing.T) {
	// Three lines become one: one pairs, two trail as removals.
	a := lines("aaa one\naaa two\naaa three")
	b := lines("aaa uno")
	doc := Build(a, b, noFold)
	checkComplete(t, a, b, doc)

	if doc.Rows[0].Kind != RowChanged {
		t.Fatalf("first row should pair, got:\n%s", summarise(doc.Rows))
	}
	for _, r := range doc.Rows[1:] {
		if r.Kind != RowRemoved {
			t.Fatalf("trailing rows should be removals, got:\n%s", summarise(doc.Rows))
		}
	}
}

func TestAlignPureInsertHasNoLeftSide(t *testing.T) {
	a := lines("one\ntwo")
	b := lines("one\nextra\ntwo")
	doc := Build(a, b, noFold)
	checkComplete(t, a, b, doc)

	want := "= one\n+ extra\n= two\n"
	if got := summarise(doc.Rows); got != want {
		t.Fatalf("got:\n%swant:\n%s", got, want)
	}
}

func TestAlignPureDeleteHasNoRightSide(t *testing.T) {
	a := lines("one\ngone\ntwo")
	b := lines("one\ntwo")
	doc := Build(a, b, noFold)
	checkComplete(t, a, b, doc)

	want := "= one\n- gone\n= two\n"
	if got := summarise(doc.Rows); got != want {
		t.Fatalf("got:\n%swant:\n%s", got, want)
	}
}

func TestAlignEmptySides(t *testing.T) {
	b := lines("one\ntwo")
	doc := Build(nil, b, noFold)
	checkComplete(t, nil, b, doc)
	for _, r := range doc.Rows {
		if r.Kind != RowAdded {
			t.Fatalf("new file should be all additions, got:\n%s", summarise(doc.Rows))
		}
	}

	doc = Build(b, nil, noFold)
	checkComplete(t, b, nil, doc)
	for _, r := range doc.Rows {
		if r.Kind != RowRemoved {
			t.Fatalf("deleted file should be all removals, got:\n%s", summarise(doc.Rows))
		}
	}

	if Build(nil, nil, noFold).Changed() {
		t.Fatal("two empty files do not differ")
	}
}

func TestWordDiffOnSimilarLines(t *testing.T) {
	a := lines("the quick brown fox")
	b := lines("the slow brown fox")
	doc := Build(a, b, noFold)

	row := doc.Rows[0]
	if row.Kind != RowChanged || row.Left.Words == nil {
		t.Fatalf("similar lines should carry an intraline diff, got %+v", row)
	}
	var changed []string
	for _, s := range row.Left.Words {
		if s.Op != difftext.OpEqual {
			changed = append(changed, s.Text)
		}
	}
	if len(changed) != 2 {
		t.Fatalf("want one word replaced, got %q", changed)
	}
}

func TestWordDiffDroppedOnDissimilarLines(t *testing.T) {
	// Nothing in common: word segments would scatter single characters across
	// the row rather than showing a change.
	a := lines("zzzzzzzzzzzzzzzz")
	b := lines("qqqqqqqqqqqqqqqq")
	doc := Build(a, b, noFold)

	row := doc.Rows[0]
	if row.Kind != RowChanged {
		t.Fatalf("want a changed row, got %v", row.Kind)
	}
	if row.Left.Words != nil {
		t.Fatalf("dissimilar lines should have no intraline diff, got %v", row.Left.Words)
	}
}

func TestWordDiffDisabled(t *testing.T) {
	a := lines("the quick brown fox")
	b := lines("the slow brown fox")
	doc := Build(a, b, Options{Context: -1, WordDiff: false})

	if doc.Rows[0].Left.Words != nil {
		t.Fatal("word diff was disabled but segments were computed")
	}
}

func TestFoldCollapsesDistantContext(t *testing.T) {
	var a []string
	for i := range 40 {
		a = append(a, strings.Repeat("x", i%5+1)+string(rune('a'+i%26)))
	}
	b := append([]string(nil), a...)
	b[20] = "CHANGED"

	doc := Build(a, b, Options{Context: DefaultContext})
	checkComplete(t, a, b, doc)

	var folds, visible int
	for _, r := range doc.Rows {
		if r.Kind == RowFolded {
			folds++
			continue
		}
		visible++
	}
	if folds != 2 {
		t.Fatalf("one change in the middle should leave a fold above and below, got %d:\n%s",
			folds, summarise(doc.Rows))
	}
	// One changed row plus three context rows on each side.
	if visible != 7 {
		t.Fatalf("want 7 visible rows, got %d:\n%s", visible, summarise(doc.Rows))
	}
}

func TestFoldKeepsShortRuns(t *testing.T) {
	// Two equal rows between changes are cheaper to show than to fold.
	a := lines("A\nsame1\nsame2\nB")
	b := lines("A2\nsame1\nsame2\nB2")

	doc := Build(a, b, Options{Context: 0})
	checkComplete(t, a, b, doc)

	for _, r := range doc.Rows {
		if r.Kind == RowFolded {
			t.Fatalf("a run of %d should not fold:\n%s", minFold-1, summarise(doc.Rows))
		}
	}
}

func TestFoldDisabled(t *testing.T) {
	var a []string
	for i := range 40 {
		a = append(a, string(rune('a'+i%26))+string(rune('0'+i%10)))
	}
	b := append([]string(nil), a...)
	b[20] = "CHANGED"

	doc := Build(a, b, Options{Context: -1})
	for _, r := range doc.Rows {
		if r.Kind == RowFolded {
			t.Fatal("negative context should disable folding")
		}
	}
}

func TestFoldIdenticalFilesCollapseEntirely(t *testing.T) {
	var a []string
	for i := range 20 {
		a = append(a, string(rune('a'+i)))
	}
	doc := Build(a, a, Options{Context: DefaultContext})
	checkComplete(t, a, a, doc)

	if len(doc.Rows) != 1 || doc.Rows[0].Kind != RowFolded {
		t.Fatalf("identical files should fold into one row, got:\n%s", summarise(doc.Rows))
	}
	if doc.Rows[0].Count() != 20 {
		t.Fatalf("fold covers %d rows, want 20", doc.Rows[0].Count())
	}
}

func TestExpand(t *testing.T) {
	var a []string
	for i := range 20 {
		a = append(a, string(rune('a'+i)))
	}
	doc := Build(a, a, Options{Context: DefaultContext})

	rows := Expand(doc.Rows, 0)
	if len(rows) != 20 {
		t.Fatalf("expanding a 20-row fold gave %d rows", len(rows))
	}
	for _, r := range rows {
		if r.Kind == RowFolded {
			t.Fatal("expansion left a fold behind")
		}
	}
	checkRows(t, a, a, rows)
}

func TestExpandAll(t *testing.T) {
	var a []string
	for i := range 40 {
		a = append(a, string(rune('a'+i%26))+string(rune('0'+i%10)))
	}
	b := append([]string(nil), a...)
	b[10], b[30] = "ONE", "TWO"

	doc := Build(a, b, Options{Context: DefaultContext})
	rows := ExpandAll(doc.Rows)

	for _, r := range rows {
		if r.Kind == RowFolded {
			t.Fatal("ExpandAll left a fold behind")
		}
	}
	checkRows(t, a, b, rows)

	// The result must match what building without folding would have given.
	want := Build(a, b, Options{Context: -1})
	if got, wantText := summarise(rows), summarise(want.Rows); got != wantText {
		t.Fatalf("ExpandAll gave:\n%swant:\n%s", got, wantText)
	}
}

func TestExpandAllWithoutFoldsIsIdentity(t *testing.T) {
	doc := Build(lines("one\ntwo"), lines("one\nTWO"), noFold)
	if got := ExpandAll(doc.Rows); len(got) != len(doc.Rows) {
		t.Fatalf("ExpandAll changed an unfolded document: %d rows, want %d", len(got), len(doc.Rows))
	}
}

func TestExpandIgnoresOrdinaryRows(t *testing.T) {
	a := lines("one\ntwo")
	b := lines("one\nchanged")
	doc := Build(a, b, noFold)

	if got := Expand(doc.Rows, 0); len(got) != len(doc.Rows) {
		t.Fatal("expanding a non-fold row should change nothing")
	}
	if got := Expand(doc.Rows, 99); len(got) != len(doc.Rows) {
		t.Fatal("expanding out of range should change nothing")
	}
}

func TestBuildRandomKeepsEveryLine(t *testing.T) {
	rng := rand.New(rand.NewSource(7))
	alphabet := []string{"alpha", "beta", "gamma", "delta", "epsilon"}

	for range 300 {
		a := randomLines(rng, alphabet, 15)
		b := randomLines(rng, alphabet, 15)

		unfolded := Build(a, b, Options{Context: -1, WordDiff: true})
		checkComplete(t, a, b, unfolded)

		for _, context := range []int{0, 1, DefaultContext} {
			doc := Build(a, b, Options{Context: context, WordDiff: true})
			checkComplete(t, a, b, doc)

			// Folding is presentation only: what it hides must be exactly
			// what the unfolded document showed, in the same order.
			if got, want := summarise(flatten(doc.Rows)), summarise(unfolded.Rows); got != want {
				t.Fatalf("context %d changed the alignment:\ngot:\n%swant:\n%s", context, got, want)
			}
		}
	}
}

func randomLines(rng *rand.Rand, alphabet []string, maxLen int) []string {
	n := rng.Intn(maxLen + 1)
	out := make([]string, n)
	for i := range out {
		out[i] = alphabet[rng.Intn(len(alphabet))]
	}
	return out
}

func TestSplitLines(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want []string
	}{
		{"empty", "", nil},
		{"no trailing newline", "a\nb", []string{"a", "b"}},
		{"trailing newline", "a\nb\n", []string{"a", "b"}},
		{"crlf", "a\r\nb\r\n", []string{"a", "b"}},
		{"blank line preserved", "a\n\nb\n", []string{"a", "", "b"}},
		{"single newline is one empty line", "\n", []string{""}},
		{"two trailing newlines keep the blank", "a\n\n", []string{"a", ""}},
	}
	for _, tt := range tests {
		got := SplitLines([]byte(tt.in))
		if strings.Join(got, "\x00") != strings.Join(tt.want, "\x00") {
			t.Errorf("%s: SplitLines(%q) = %q, want %q", tt.name, tt.in, got, tt.want)
		}
	}
}
