package layout

import "testing"

func TestCellWidth(t *testing.T) {
	tests := []struct {
		name string
		r    rune
		want int
	}{
		{"ascii", 'a', 1},
		{"latin accent", 'é', 1},
		{"nul", 0, 0},
		{"zero width joiner", 0x200D, 0},
		{"variation selector", 0xFE0F, 0},
		{"combining acute", 0x0301, 0},
		{"enclosing circle", 0x20DD, 0},
		{"hiragana", 'あ', 2},
		{"cjk ideograph", '漢', 2},
		{"hangul syllable", '한', 2},
		{"full width A", 'Ａ', 2},
		{"emoji", '\U0001F600', 2},
		{"cjk extension b", '\U00020000', 2},
		{"box drawing", '─', 1},
		{"em dash", '—', 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := CellWidth(tt.r); got != tt.want {
				t.Errorf("CellWidth(%U) = %d, want %d", tt.r, got, tt.want)
			}
		})
	}
}

func TestWidth(t *testing.T) {
	tests := []struct {
		s    string
		want int
	}{
		{"", 0},
		{"abc", 3},
		{"漢字", 4},
		{"a漢b", 4},
		{"é", 1},         // e + combining acute
		{"\U0001F600", 2}, // emoji
		{"a‍b", 2},        // joiner contributes nothing
		{"│ quoted", 8},
	}
	for _, tt := range tests {
		if got := Width(tt.s); got != tt.want {
			t.Errorf("Width(%q) = %d, want %d", tt.s, got, tt.want)
		}
	}
}

func TestClip(t *testing.T) {
	tests := []struct {
		name      string
		s         string
		cells     int
		want      string
		wantCells int
	}{
		{"fits", "abc", 5, "abc", 3},
		{"exact", "abc", 3, "abc", 3},
		{"truncates", "abcdef", 3, "abc", 3},
		{"zero", "abc", 0, "", 0},
		{"negative", "abc", -1, "", 0},
		{"wide fits", "漢字", 4, "漢字", 4},
		{"wide truncates", "漢字", 3, "漢", 2},
		{"wide straddles boundary", "漢字", 2, "漢", 2},
		{"wide too narrow", "漢", 1, "", 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, gotCells := clip(tt.s, tt.cells)
			if got != tt.want || gotCells != tt.wantCells {
				t.Errorf("clip(%q, %d) = %q/%d, want %q/%d",
					tt.s, tt.cells, got, gotCells, tt.want, tt.wantCells)
			}
		})
	}
}

func TestHardSplit(t *testing.T) {
	tests := []struct {
		name  string
		s     string
		cells int
		want  []string
	}{
		{"fits whole", "abc", 5, []string{"abc"}},
		{"splits evenly", "abcdef", 3, []string{"abc", "def"}},
		{"splits with remainder", "abcde", 2, []string{"ab", "cd", "e"}},
		{"wide runes", "漢字漢", 2, []string{"漢", "字", "漢"}},
		{"rune wider than row", "漢字", 1, []string{"漢", "字"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := hardSplit(tt.s, tt.cells)
			if len(got) != len(tt.want) {
				t.Fatalf("hardSplit(%q, %d) = %q, want %q", tt.s, tt.cells, got, tt.want)
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Fatalf("hardSplit(%q, %d) = %q, want %q", tt.s, tt.cells, got, tt.want)
				}
			}
		})
	}
}

// hardSplit must always consume the whole input, whatever the width.
func TestHardSplitLosesNothing(t *testing.T) {
	for _, s := range []string{"abcdef", "漢字漢字", "a漢b字c", ""} {
		for cells := 1; cells <= 6; cells++ {
			var joined string
			for _, chunk := range hardSplit(s, cells) {
				joined += chunk
			}
			if joined != s {
				t.Errorf("hardSplit(%q, %d) rejoined to %q", s, cells, joined)
			}
		}
	}
}

func TestSplitRuns(t *testing.T) {
	tests := []struct {
		s    string
		want []string
	}{
		{"", nil},
		{"word", []string{"word"}},
		{"two words", []string{"two", " ", "words"}},
		{" leading", []string{" ", "leading"}},
		{"trailing ", []string{"trailing", " "}},
		{"a  b", []string{"a", "  ", "b"}},
	}
	for _, tt := range tests {
		got := splitRuns(tt.s)
		if len(got) != len(tt.want) {
			t.Errorf("splitRuns(%q) = %q, want %q", tt.s, got, tt.want)
			continue
		}
		for i := range got {
			if got[i] != tt.want[i] {
				t.Errorf("splitRuns(%q) = %q, want %q", tt.s, got, tt.want)
				break
			}
		}
	}
}
