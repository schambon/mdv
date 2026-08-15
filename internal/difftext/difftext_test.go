package difftext

import (
	"math/rand"
	"strings"
	"testing"
)

// apply replays an edit script over a, reconstructing b. Every structural
// property of the algorithm is checked through this: if the script is
// self-consistent it must rebuild b exactly.
func apply(t *testing.T, a, b []string, edits []Edit) []string {
	t.Helper()

	var out []string
	aPos, bPos := 0, 0
	for i, e := range edits {
		if e.AStart != aPos || e.BStart != bPos {
			t.Fatalf("edit %d starts at A=%d B=%d, want A=%d B=%d (%v)",
				i, e.AStart, e.BStart, aPos, bPos, edits)
		}
		switch e.Op {
		case OpEqual:
			if e.ALen != e.BLen {
				t.Fatalf("edit %d is equal but spans %d of A and %d of B", i, e.ALen, e.BLen)
			}
			for j := range e.ALen {
				if a[e.AStart+j] != b[e.BStart+j] {
					t.Fatalf("edit %d claims %q equals %q", i, a[e.AStart+j], b[e.BStart+j])
				}
			}
			out = append(out, a[e.AStart:e.AStart+e.ALen]...)
		case OpDelete:
			if e.BLen != 0 {
				t.Fatalf("edit %d is a delete but spans %d of B", i, e.BLen)
			}
		case OpInsert:
			if e.ALen != 0 {
				t.Fatalf("edit %d is an insert but spans %d of A", i, e.ALen)
			}
			out = append(out, b[e.BStart:e.BStart+e.BLen]...)
		}
		aPos += e.ALen
		bPos += e.BLen
	}
	if aPos != len(a) || bPos != len(b) {
		t.Fatalf("script consumed A=%d/%d B=%d/%d", aPos, len(a), bPos, len(b))
	}
	return out
}

func checkRoundTrip(t *testing.T, a, b []string) []Edit {
	t.Helper()
	edits := Lines(a, b)
	got := apply(t, a, b, edits)
	if strings.Join(got, "\n") != strings.Join(b, "\n") {
		t.Fatalf("reconstructed %q, want %q", got, b)
	}
	// Adjacent edits must never share an operation, or a caller counting
	// hunks would see one change reported as several.
	for i := 1; i < len(edits); i++ {
		if edits[i].Op == edits[i-1].Op {
			t.Fatalf("edits %d and %d share op %v: %v", i-1, i, edits[i].Op, edits)
		}
	}
	return edits
}

func lines(s string) []string {
	if s == "" {
		return nil
	}
	return strings.Split(s, "\n")
}

func TestLinesIdentical(t *testing.T) {
	a := lines("one\ntwo\nthree")
	edits := checkRoundTrip(t, a, a)
	if len(edits) != 1 || edits[0].Op != OpEqual || edits[0].ALen != 3 {
		t.Fatalf("identical input should be one equal run of 3, got %v", edits)
	}
}

func TestLinesBothEmpty(t *testing.T) {
	if edits := Lines(nil, nil); len(edits) != 0 {
		t.Fatalf("empty inputs should produce no edits, got %v", edits)
	}
}

func TestLinesPureInsert(t *testing.T) {
	a := lines("one\ntwo")
	b := lines("one\ninserted\ntwo")
	edits := checkRoundTrip(t, a, b)
	if len(edits) != 3 || edits[1].Op != OpInsert || edits[1].BLen != 1 {
		t.Fatalf("want equal/insert/equal, got %v", edits)
	}
}

func TestLinesPureDelete(t *testing.T) {
	a := lines("one\ngone\ntwo")
	b := lines("one\ntwo")
	edits := checkRoundTrip(t, a, b)
	if len(edits) != 3 || edits[1].Op != OpDelete || edits[1].ALen != 1 {
		t.Fatalf("want equal/delete/equal, got %v", edits)
	}
}

func TestLinesFromEmpty(t *testing.T) {
	b := lines("one\ntwo")
	edits := checkRoundTrip(t, nil, b)
	if len(edits) != 1 || edits[0].Op != OpInsert || edits[0].BLen != 2 {
		t.Fatalf("want a single insert of 2, got %v", edits)
	}
}

func TestLinesToEmpty(t *testing.T) {
	a := lines("one\ntwo")
	edits := checkRoundTrip(t, a, nil)
	if len(edits) != 1 || edits[0].Op != OpDelete || edits[0].ALen != 2 {
		t.Fatalf("want a single delete of 2, got %v", edits)
	}
}

func TestLinesCompleteRewrite(t *testing.T) {
	a := lines("aaa\nbbb\nccc")
	b := lines("xxx\nyyy")
	edits := checkRoundTrip(t, a, b)
	if len(edits) != 2 {
		t.Fatalf("a rewrite with nothing in common should be delete+insert, got %v", edits)
	}
}

// The classic worked example from Myers' paper, which has a known optimal
// edit distance of 5.
func TestLinesMyersPaperExample(t *testing.T) {
	a := strings.Split("ABCABBA", "")
	b := strings.Split("CBABAC", "")
	edits := checkRoundTrip(t, a, b)

	distance := 0
	for _, e := range edits {
		distance += e.ALen + e.BLen
		if e.Op == OpEqual {
			distance -= e.ALen + e.BLen
		}
	}
	if distance != 5 {
		t.Fatalf("edit distance %d, want the paper's 5 (%v)", distance, edits)
	}
}

func TestLinesMinimalEditDistance(t *testing.T) {
	// One changed line in the middle of a file must cost exactly one delete
	// and one insert, not a cascade of realignments.
	a := lines("1\n2\n3\n4\n5\n6\n7")
	b := lines("1\n2\n3\nCHANGED\n5\n6\n7")
	edits := checkRoundTrip(t, a, b)
	if len(edits) != 4 {
		t.Fatalf("want equal/delete/insert/equal, got %v", edits)
	}
	if edits[1].ALen != 1 || edits[2].BLen != 1 {
		t.Fatalf("change should span one line each way, got %v", edits)
	}
}

func TestLinesRepeatedContent(t *testing.T) {
	// Repeated identical lines are where a sloppy diff drifts out of
	// alignment, since many equally-long paths exist.
	a := lines("x\nx\nx\nx")
	b := lines("x\nx")
	checkRoundTrip(t, a, b)
}

func TestLinesRoundTripRandom(t *testing.T) {
	rng := rand.New(rand.NewSource(1))
	alphabet := []string{"a", "b", "c", "d"} // a small alphabet forces collisions

	for range 400 {
		a := randomLines(rng, alphabet, 12)
		b := randomLines(rng, alphabet, 12)
		checkRoundTrip(t, a, b)
	}
}

func TestLinesRoundTripRandomLarge(t *testing.T) {
	rng := rand.New(rand.NewSource(2))
	alphabet := make([]string, 200)
	for i := range alphabet {
		alphabet[i] = string(rune('a' + i%26))
	}

	for range 20 {
		a := randomLines(rng, alphabet, 300)
		b := randomLines(rng, alphabet, 300)
		checkRoundTrip(t, a, b)
	}
}

// A file with an edited region is the realistic shape: mostly shared context
// with a localised change. The script should be dominated by equal runs.
func TestLinesLocalisedChangeStaysLocal(t *testing.T) {
	var a, b []string
	for i := range 200 {
		line := string(rune('a'+i%26)) + string(rune('0'+i%10))
		a = append(a, line)
		b = append(b, line)
	}
	b[100] = "CHANGED"

	edits := checkRoundTrip(t, a, b)
	if len(edits) != 4 {
		t.Fatalf("one edited line in 200 should give 4 edits, got %d: %v", len(edits), edits)
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

func TestTokenize(t *testing.T) {
	tests := []struct {
		in   string
		want []string
	}{
		{"", nil},
		{"one", []string{"one"}},
		{"a b", []string{"a", " ", "b"}},
		{"  a", []string{"  ", "a"}},
		{"f(x)", []string{"f", "(", "x", ")"}},
		// Each punctuation rune stands alone so a change can land on one.
		{"))", []string{")", ")"}},
		{"snake_case2", []string{"snake_case2"}},
		{"a.b", []string{"a", ".", "b"}},
	}
	for _, tt := range tests {
		got := tokenize(tt.in)
		if strings.Join(got, "\x00") != strings.Join(tt.want, "\x00") {
			t.Errorf("tokenize(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

// Segments must reproduce both sides exactly, or highlighting would render
// text that is not in the file.
func checkWords(t *testing.T, a, b string) []Segment {
	t.Helper()
	segments := Words(a, b)

	var gotA, gotB strings.Builder
	for _, s := range segments {
		switch s.Op {
		case OpEqual:
			gotA.WriteString(s.Text)
			gotB.WriteString(s.Text)
		case OpDelete:
			gotA.WriteString(s.Text)
		case OpInsert:
			gotB.WriteString(s.Text)
		}
	}
	if gotA.String() != a {
		t.Fatalf("segments rebuild A as %q, want %q", gotA.String(), a)
	}
	if gotB.String() != b {
		t.Fatalf("segments rebuild B as %q, want %q", gotB.String(), b)
	}
	return segments
}

func TestWordsOneWordChanged(t *testing.T) {
	segments := checkWords(t, "the quick brown fox", "the slow brown fox")

	var changed []string
	for _, s := range segments {
		if s.Op != OpEqual {
			changed = append(changed, s.Text)
		}
	}
	// Only the one word differs; the shared words must not be swept in.
	if len(changed) != 2 || changed[0] != "quick" || changed[1] != "slow" {
		t.Fatalf("changed segments %q, want [quick slow]", changed)
	}
}

func TestWordsIdentical(t *testing.T) {
	segments := checkWords(t, "same line", "same line")
	if len(segments) != 1 || segments[0].Op != OpEqual {
		t.Fatalf("identical lines should be one equal segment, got %v", segments)
	}
}

func TestWordsEmptySide(t *testing.T) {
	checkWords(t, "", "added")
	checkWords(t, "removed", "")
	checkWords(t, "", "")
}

func TestWordsPunctuationBoundary(t *testing.T) {
	// The argument changed, not the call: parentheses stay equal.
	segments := checkWords(t, "call(old, x)", "call(new, x)")
	for _, s := range segments {
		if s.Op != OpEqual && strings.ContainsAny(s.Text, "()") {
			t.Fatalf("punctuation swept into a change: %v", segments)
		}
	}
}

func TestWordsRoundTripRandom(t *testing.T) {
	rng := rand.New(rand.NewSource(3))
	alphabet := []string{"a", "bb", " ", "(", ")", ",", "x1"}

	for range 300 {
		checkWords(t, randomString(rng, alphabet, 10), randomString(rng, alphabet, 10))
	}
}

func randomString(rng *rand.Rand, alphabet []string, maxLen int) string {
	var sb strings.Builder
	for range rng.Intn(maxLen + 1) {
		sb.WriteString(alphabet[rng.Intn(len(alphabet))])
	}
	return sb.String()
}
