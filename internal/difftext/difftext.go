// Package difftext computes line- and word-level differences between two
// versions of a text. It is the only place the diff algorithm lives: callers
// receive a flat edit script and decide for themselves how to align, fold, and
// draw it.
//
// The algorithm is Myers' O(ND) difference algorithm in the linear-space
// divide-and-conquer form from section 4b of the paper. Recording the whole
// edit graph would be simpler but costs O(D^2) memory, which a largely
// rewritten file reaches easily; splitting each problem at a middle snake
// keeps memory proportional to the input instead.
package difftext

import (
	"strings"
	"unicode"
)

// Op is the kind of change an Edit describes.
type Op int

const (
	OpEqual Op = iota
	OpDelete
	OpInsert
)

func (o Op) String() string {
	switch o {
	case OpEqual:
		return "equal"
	case OpDelete:
		return "delete"
	case OpInsert:
		return "insert"
	default:
		return "unknown"
	}
}

// Edit is a run of consecutive elements sharing one operation. The index
// ranges are half-open: an OpEqual edit spans both sides and its lengths are
// equal, an OpDelete edit spans only A, an OpInsert only B.
//
// Deletes and inserts stay separate rather than being paired into a single
// "replace". Pairing is an alignment decision — which removed line sits
// opposite which added one — and it depends on how the result will be drawn,
// so it belongs to the caller, not here.
type Edit struct {
	Op     Op
	AStart int
	ALen   int
	BStart int
	BLen   int
}

// Segment is a run of text on one side of a word-level diff.
type Segment struct {
	Op   Op
	Text string
}

// Lines diffs two slices of lines and returns the edit script that turns a
// into b.
func Lines(a, b []string) []Edit {
	return diff(a, b)
}

// Words diffs two single lines at word granularity, returning text segments
// rather than index ranges because that is what an intraline highlighter
// needs. Concatenating the OpEqual and OpDelete segments reproduces a;
// concatenating OpEqual and OpInsert reproduces b.
func Words(a, b string) []Segment {
	ta, tb := tokenize(a), tokenize(b)
	edits := diff(ta, tb)

	segments := make([]Segment, 0, len(edits))
	for _, e := range edits {
		switch e.Op {
		case OpDelete:
			segments = append(segments, Segment{
				Op:   OpDelete,
				Text: strings.Join(ta[e.AStart:e.AStart+e.ALen], ""),
			})
		case OpInsert:
			segments = append(segments, Segment{
				Op:   OpInsert,
				Text: strings.Join(tb[e.BStart:e.BStart+e.BLen], ""),
			})
		default:
			segments = append(segments, Segment{
				Op:   OpEqual,
				Text: strings.Join(ta[e.AStart:e.AStart+e.ALen], ""),
			})
		}
	}
	return segments
}

// tokenize splits text into word-diff units: runs of whitespace, runs of
// letters, digits and underscore, and every other rune on its own. Splitting
// punctuation individually keeps a changed argument in a call from swallowing
// the parentheses around it, which would highlight far more of the line than
// actually changed.
func tokenize(text string) []string {
	if text == "" {
		return nil
	}

	var tokens []string
	start := 0
	prev := classOf(firstRune(text))

	for i, r := range text {
		if i == 0 {
			continue
		}
		cur := classOf(r)
		// classOther runes never join a run, not even another classOther:
		// "))" is two tokens so a change can land on just one of them.
		if cur != prev || cur == classOther {
			tokens = append(tokens, text[start:i])
			start = i
			prev = cur
		}
	}
	return append(tokens, text[start:])
}

type class int

const (
	classSpace class = iota
	classWord
	classOther
)

func classOf(r rune) class {
	switch {
	case unicode.IsSpace(r):
		return classSpace
	case r == '_' || unicode.IsLetter(r) || unicode.IsDigit(r):
		return classWord
	default:
		return classOther
	}
}

func firstRune(s string) rune {
	for _, r := range s {
		return r
	}
	return 0
}

// diff is the entry point of the recursion. It trims the common prefix and
// suffix before doing any real work: shared context is the common case
// between two versions of a file, and Myers' cost grows with the number of
// differences, not the size of the input.
func diff[T comparable](a, b []T) []Edit {
	var out []Edit
	diffRange(a, b, 0, 0, &out)
	return out
}

func diffRange[T comparable](a, b []T, aOff, bOff int, out *[]Edit) {
	prefix := 0
	for prefix < len(a) && prefix < len(b) && a[prefix] == b[prefix] {
		prefix++
	}
	suffix := 0
	for suffix < len(a)-prefix && suffix < len(b)-prefix &&
		a[len(a)-1-suffix] == b[len(b)-1-suffix] {
		suffix++
	}

	if prefix > 0 {
		emit(out, Edit{Op: OpEqual, AStart: aOff, ALen: prefix, BStart: bOff, BLen: prefix})
	}

	diffMiddle(
		a[prefix:len(a)-suffix],
		b[prefix:len(b)-suffix],
		aOff+prefix, bOff+prefix, out,
	)

	if suffix > 0 {
		emit(out, Edit{
			Op:     OpEqual,
			AStart: aOff + len(a) - suffix, ALen: suffix,
			BStart: bOff + len(b) - suffix, BLen: suffix,
		})
	}
}

// diffMiddle handles a region with no common prefix or suffix left. Because of
// that trimming, any such region with content on both sides needs at least two
// edits, so the middle snake always splits it into two strictly smaller
// problems and the recursion terminates.
func diffMiddle[T comparable](a, b []T, aOff, bOff int, out *[]Edit) {
	switch {
	case len(a) == 0 && len(b) == 0:
		return
	case len(a) == 0:
		emit(out, Edit{Op: OpInsert, AStart: aOff, BStart: bOff, BLen: len(b)})
		return
	case len(b) == 0:
		emit(out, Edit{Op: OpDelete, AStart: aOff, ALen: len(a), BStart: bOff})
		return
	}

	s, ok := middleSnake(a, b)
	if !ok {
		// Defensive: the searches are guaranteed to meet, but recursing on an
		// empty snake at the origin would never terminate, so degrade to
		// rewriting the whole region instead of hanging.
		emit(out, Edit{Op: OpDelete, AStart: aOff, ALen: len(a), BStart: bOff})
		emit(out, Edit{Op: OpInsert, AStart: aOff + len(a), BStart: bOff, BLen: len(b)})
		return
	}

	diffRange(a[:s.xStart], b[:s.yStart], aOff, bOff, out)
	if n := s.xEnd - s.xStart; n > 0 {
		emit(out, Edit{
			Op:     OpEqual,
			AStart: aOff + s.xStart, ALen: n,
			BStart: bOff + s.yStart, BLen: n,
		})
	}
	diffRange(a[s.xEnd:], b[s.yEnd:], aOff+s.xEnd, bOff+s.yEnd, out)
}

// emit appends an edit, merging it into the previous one when they share an
// operation. The recursion naturally produces adjacent runs of the same kind
// at the seams between subproblems, and a caller reading hunks should not have
// to stitch them back together.
func emit(out *[]Edit, e Edit) {
	if e.ALen == 0 && e.BLen == 0 {
		return
	}
	if n := len(*out); n > 0 {
		if last := &(*out)[n-1]; last.Op == e.Op {
			last.ALen += e.ALen
			last.BLen += e.BLen
			return
		}
	}
	*out = append(*out, e)
}

// snake is a diagonal run of equal elements lying on an optimal edit path.
type snake struct{ xStart, yStart, xEnd, yEnd int }

// middleSnake advances a forward search from (0,0) and a reverse search from
// (n,m) one D-step at a time until their furthest-reaching paths overlap. The
// snake at that meeting point lies on some optimal path, so the problem can be
// split there without ever materialising the full edit graph.
//
// Both a and b must be non-empty. The bool is false only if the two searches
// somehow fail to meet, which the algorithm guarantees cannot happen; callers
// must still handle it rather than recurse on a meaningless snake.
func middleSnake[T comparable](a, b []T) (snake, bool) {
	n, m := len(a), len(b)
	delta := n - m
	odd := delta%2 != 0

	// Diagonals run from -(n+m) to n+m, so store them in one slice biased by
	// offset. vf and vr hold the furthest x reached on each diagonal by the
	// forward and reverse searches.
	offset := n + m
	vf := make([]int, 2*offset+1)
	vr := make([]int, 2*offset+1)
	vf[offset+1] = 0
	vr[offset+1] = 0

	for d := 0; d <= (n+m+1)/2; d++ {
		for k := -d; k <= d; k += 2 {
			var x int
			if k == -d || (k != d && vf[offset+k-1] < vf[offset+k+1]) {
				x = vf[offset+k+1] // extend the path from the diagonal above
			} else {
				x = vf[offset+k-1] + 1 // extend the one to the left
			}
			y := x - k

			startX, startY := x, y
			for x < n && y < m && a[x] == b[y] {
				x, y = x+1, y+1
			}
			vf[offset+k] = x

			// Only an odd delta lets a forward path of d steps meet a reverse
			// path of d-1; the reverse diagonal for forward k is delta-k.
			if odd && delta-k >= -(d-1) && delta-k <= d-1 {
				if x+vr[offset+delta-k] >= n {
					return snake{startX, startY, x, y}, true
				}
			}
		}

		for k := -d; k <= d; k += 2 {
			var x int
			if k == -d || (k != d && vr[offset+k-1] < vr[offset+k+1]) {
				x = vr[offset+k+1]
			} else {
				x = vr[offset+k-1] + 1
			}
			y := x - k

			startX, startY := x, y
			for x < n && y < m && a[n-1-x] == b[m-1-y] {
				x, y = x+1, y+1
			}
			vr[offset+k] = x

			if !odd && delta-k >= -d && delta-k <= d {
				if x+vf[offset+delta-k] >= n {
					// Reverse coordinates count from the end, so mirror them.
					return snake{n - x, m - y, n - startX, m - startY}, true
				}
			}
		}
	}

	return snake{}, false
}
