package layout

import "unicode"

// doubleWidthRanges is a fixed table of East Asian, full-width, and emoji
// ranges. This is rune-based approximation, not full grapheme-cluster layout.
var doubleWidthRanges = [][2]rune{
	{0x1100, 0x115F},   // Hangul Jamo
	{0x2E80, 0x303E},   // CJK radicals, Kangxi, CJK symbols
	{0x3041, 0x33FF},   // Hiragana through CJK compatibility
	{0x3400, 0x4DBF},   // CJK extension A
	{0x4E00, 0x9FFF},   // CJK unified ideographs
	{0xA000, 0xA4CF},   // Yi
	{0xAC00, 0xD7A3},   // Hangul syllables
	{0xF900, 0xFAFF},   // CJK compatibility ideographs
	{0xFE10, 0xFE19},   // vertical forms
	{0xFE30, 0xFE6F},   // CJK compatibility forms
	{0xFF00, 0xFF60},   // full-width forms
	{0xFFE0, 0xFFE6},   // full-width signs
	{0x1F300, 0x1FAFF}, // emoji and pictographs
	{0x20000, 0x2FFFD}, // CJK extension B and beyond
	{0x30000, 0x3FFFD},
}

// CellWidth returns the number of terminal cells a rune occupies: zero for
// marks and joiners that compose with a preceding rune, two for East Asian and
// emoji ranges, one otherwise.
func CellWidth(r rune) int {
	switch {
	case r == 0:
		return 0
	case r == 0x200D: // zero-width joiner
		return 0
	case r >= 0xFE00 && r <= 0xFE0F: // variation selectors
		return 0
	case unicode.Is(unicode.Mn, r), unicode.Is(unicode.Me, r):
		return 0
	}
	for _, rng := range doubleWidthRanges {
		if r >= rng[0] && r <= rng[1] {
			return 2
		}
	}
	return 1
}

// Width sums the cell widths of a string. Tabs are not expanded.
func Width(s string) int {
	total := 0
	for _, r := range s {
		total += CellWidth(r)
	}
	return total
}

// clip truncates s rune by rune to at most cells columns, returning the kept
// text and its width. A double-width rune that would straddle the boundary is
// dropped.
func clip(s string, cells int) (string, int) {
	if cells <= 0 {
		return "", 0
	}
	used := 0
	for i, r := range s {
		w := CellWidth(r)
		if used+w > cells {
			return s[:i], used
		}
		used += w
	}
	return s, used
}

// pad appends spaces until the text occupies exactly cells columns.
func pad(s string, width, cells int) string {
	for width < cells {
		s += " "
		width++
	}
	return s
}
