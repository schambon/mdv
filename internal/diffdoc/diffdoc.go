// Package diffdoc turns the flat edit script from difftext into aligned rows:
// pairs of left and right lines that a renderer can draw opposite each other.
// It is the diff-mode counterpart of package doc — the semantic model, with no
// terminal or styling concerns — and like doc it records where each line came
// from so the viewer can map a row back to a source line.
package diffdoc

import (
	"strings"

	"github.com/schambon/mdv/internal/difftext"
)

// RowKind classifies an aligned row.
type RowKind int

const (
	RowEqual   RowKind = iota // both sides present and identical
	RowChanged                // both sides present and different
	RowAdded                  // right side only
	RowRemoved                // left side only
	RowFolded                 // a collapsed run of equal rows
)

// Line is one side of a row. Number is the one-based line number in that
// side's file, or zero when the side has no line here — an added line has no
// number on the left.
type Line struct {
	Text   string
	Number int
	// Words is the intraline diff, set only on a RowChanged row whose two
	// sides are similar enough to be worth aligning word by word. It is nil
	// when the line should be highlighted as wholly changed.
	Words []difftext.Segment
}

// Present reports whether this side has a line at all, as opposed to being
// filler opposite an insertion or deletion.
func (l Line) Present() bool { return l.Number > 0 }

// Row is one drawable unit of a diff: the left line, the right line, or both.
type Row struct {
	Kind  RowKind
	Left  Line
	Right Line
	// Hidden holds the collapsed rows of a RowFolded row, so expanding a fold
	// is a splice rather than a re-diff.
	Hidden []Row
}

// Count is the number of underlying rows a row stands for: one, except for a
// fold, which stands for everything it hides.
func (r Row) Count() int {
	if r.Kind == RowFolded {
		return len(r.Hidden)
	}
	return 1
}

// Document is an aligned diff.
type Document struct {
	Rows []Row
}

// Changed reports whether the two sides differ at all.
func (d Document) Changed() bool {
	for _, r := range d.Rows {
		if r.Kind != RowEqual && r.Kind != RowFolded {
			return true
		}
	}
	return false
}

// Options controls alignment and folding.
type Options struct {
	// Context is how many unchanged lines stay visible on each side of a
	// change. Negative means unlimited: nothing is folded.
	Context int
	// WordDiff computes intraline segments for changed rows.
	WordDiff bool
}

// DefaultContext matches the conventional unified-diff window.
const DefaultContext = 3

// minFold is the shortest run of equal rows worth collapsing. Folding one or
// two lines behind a "N unchanged lines" marker saves no space and costs the
// reader a keystroke to get back.
const minFold = 3

// similarityFloor is the fraction of a changed row's text that must survive
// as equal for the intraline diff to be kept. Below it the two lines have
// little to do with each other, and word highlighting would scatter fragments
// across the row instead of showing a change.
const similarityFloor = 0.25

// Build aligns two slices of lines into a diff document.
func Build(a, b []string, opts Options) Document {
	rows := align(a, b, opts.WordDiff)
	if opts.Context >= 0 {
		rows = fold(rows, opts.Context)
	}
	return Document{Rows: rows}
}

// align walks the edit script and pairs it into rows. A delete immediately
// followed by an insert is the interesting case: those are the lines that were
// rewritten, and pairing them opposite each other is what makes a side-by-side
// view readable rather than a list of removals followed by a list of
// additions.
func align(a, b []string, wordDiff bool) []Row {
	edits := difftext.Lines(a, b)

	var rows []Row
	for i := 0; i < len(edits); i++ {
		e := edits[i]
		switch e.Op {
		case difftext.OpEqual:
			for j := range e.ALen {
				rows = append(rows, Row{
					Kind:  RowEqual,
					Left:  Line{Text: a[e.AStart+j], Number: e.AStart + j + 1},
					Right: Line{Text: b[e.BStart+j], Number: e.BStart + j + 1},
				})
			}

		case difftext.OpDelete:
			// Look ahead for the insert that turns this deletion into a
			// rewrite. difftext never emits two edits of one kind in a row, so
			// the neighbour is either the matching insert or unrelated text.
			if i+1 < len(edits) && edits[i+1].Op == difftext.OpInsert {
				rows = append(rows, pair(a, b, e, edits[i+1], wordDiff)...)
				i++
				continue
			}
			rows = append(rows, removed(a, e)...)

		case difftext.OpInsert:
			// The same rewrite reported the other way round. Which order the
			// algorithm chooses depends on the path it takes through the edit
			// graph, and the reader should not be able to tell.
			if i+1 < len(edits) && edits[i+1].Op == difftext.OpDelete {
				rows = append(rows, pair(a, b, edits[i+1], e, wordDiff)...)
				i++
				continue
			}
			rows = append(rows, added(b, e)...)
		}
	}
	return rows
}

// pair aligns a delete/insert run into changed rows, with the leftovers of the
// longer side trailing as pure removals or additions.
func pair(a, b []string, del, ins difftext.Edit, wordDiff bool) []Row {
	common := min(del.ALen, ins.BLen)

	rows := make([]Row, 0, max(del.ALen, ins.BLen))
	for j := range common {
		left := Line{Text: a[del.AStart+j], Number: del.AStart + j + 1}
		right := Line{Text: b[ins.BStart+j], Number: ins.BStart + j + 1}
		if wordDiff {
			if words, ok := intraline(left.Text, right.Text); ok {
				left.Words, right.Words = words, words
			}
		}
		rows = append(rows, Row{Kind: RowChanged, Left: left, Right: right})
	}
	for j := common; j < del.ALen; j++ {
		rows = append(rows, Row{
			Kind: RowRemoved,
			Left: Line{Text: a[del.AStart+j], Number: del.AStart + j + 1},
		})
	}
	for j := common; j < ins.BLen; j++ {
		rows = append(rows, Row{
			Kind:  RowAdded,
			Right: Line{Text: b[ins.BStart+j], Number: ins.BStart + j + 1},
		})
	}
	return rows
}

// removed turns a lone deletion into rows with an empty right side.
func removed(a []string, e difftext.Edit) []Row {
	rows := make([]Row, 0, e.ALen)
	for j := range e.ALen {
		rows = append(rows, Row{
			Kind: RowRemoved,
			Left: Line{Text: a[e.AStart+j], Number: e.AStart + j + 1},
		})
	}
	return rows
}

// added turns a lone insertion into rows with an empty left side.
func added(b []string, e difftext.Edit) []Row {
	rows := make([]Row, 0, e.BLen)
	for j := range e.BLen {
		rows = append(rows, Row{
			Kind:  RowAdded,
			Right: Line{Text: b[e.BStart+j], Number: e.BStart + j + 1},
		})
	}
	return rows
}

// intraline computes the word-level diff of a changed row, reporting false
// when the two lines are too dissimilar for it to mean anything.
func intraline(left, right string) ([]difftext.Segment, bool) {
	segments := difftext.Words(left, right)

	equal := 0
	for _, s := range segments {
		if s.Op == difftext.OpEqual {
			equal += len(s.Text)
		}
	}
	total := len(left) + len(right)
	if total == 0 {
		return nil, false
	}
	if float64(2*equal)/float64(total) < similarityFloor {
		return nil, false
	}
	return segments, true
}

// fold collapses runs of equal rows that are more than context rows away from
// any change. The hidden rows travel inside the fold so the viewer can expand
// one without recomputing the diff.
func fold(rows []Row, context int) []Row {
	if context < 0 {
		return rows
	}

	// A row is visible if it changed, or is close enough to something that
	// did. Marking first and collapsing second keeps the two windows around
	// adjacent changes from being computed against each other.
	visible := make([]bool, len(rows))
	for i, r := range rows {
		if r.Kind == RowEqual {
			continue
		}
		lo := max(i-context, 0)
		hi := min(i+context, len(rows)-1)
		for j := lo; j <= hi; j++ {
			visible[j] = true
		}
	}

	var out []Row
	for i := 0; i < len(rows); {
		if visible[i] {
			out = append(out, rows[i])
			i++
			continue
		}
		run := i
		for i < len(rows) && !visible[i] {
			i++
		}
		if i-run < minFold {
			out = append(out, rows[run:i]...)
			continue
		}
		out = append(out, Row{Kind: RowFolded, Hidden: rows[run:i]})
	}
	return out
}

// Expand replaces the fold at index with the rows it hides. It returns the
// rows unchanged if the index is not a fold, so a keystroke on an ordinary row
// is harmless.
func Expand(rows []Row, index int) []Row {
	if index < 0 || index >= len(rows) || rows[index].Kind != RowFolded {
		return rows
	}
	hidden := rows[index].Hidden

	out := make([]Row, 0, len(rows)+len(hidden)-1)
	out = append(out, rows[:index]...)
	out = append(out, hidden...)
	return append(out, rows[index+1:]...)
}

// ExpandAll replaces every fold with the rows it hides.
func ExpandAll(rows []Row) []Row {
	folded := false
	for _, r := range rows {
		if r.Kind == RowFolded {
			folded = true
			break
		}
	}
	if !folded {
		return rows
	}

	out := make([]Row, 0, len(rows))
	for _, r := range rows {
		if r.Kind == RowFolded {
			out = append(out, ExpandAll(r.Hidden)...)
			continue
		}
		out = append(out, r)
	}
	return out
}

// SplitLines breaks a file into lines for diffing, accepting either LF or CRLF
// and dropping the empty element a trailing newline would otherwise produce.
// A file's last line is its last line whether or not it ends in a newline.
func SplitLines(data []byte) []string {
	if len(data) == 0 {
		return nil
	}
	text := strings.ReplaceAll(string(data), "\r\n", "\n")
	text = strings.TrimSuffix(text, "\n")
	return strings.Split(text, "\n")
}
