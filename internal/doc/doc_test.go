package doc

import "testing"

// "ab\ncd\n\nefg" -> line starts 0, 3, 6, 7
func testDoc() Document {
	return Document{Lines: []int{0, 3, 6, 7}, Size: 10}
}

func TestPosition(t *testing.T) {
	d := testDoc()
	tests := []struct {
		name         string
		offset       int
		line, column int
	}{
		{"first line start", 0, 1, 1},
		{"first line middle", 1, 1, 2},
		{"second line start", 3, 2, 1},
		{"second line middle", 4, 2, 2},
		{"blank line", 6, 3, 1},
		{"last line start", 7, 4, 1},
		{"last line middle", 9, 4, 3},
		{"past end clamps to last line", 40, 4, 34},
		{"negative clamps to start", -5, 1, 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := d.Position(tt.offset)
			if got.Line != tt.line || got.Column != tt.column {
				t.Errorf("Position(%d) = line %d col %d, want line %d col %d",
					tt.offset, got.Line, got.Column, tt.line, tt.column)
			}
		})
	}
}

func TestPositionRecordsOffset(t *testing.T) {
	if got := testDoc().Position(4); got.ByteOffset != 4 {
		t.Errorf("ByteOffset = %d, want 4", got.ByteOffset)
	}
}

func TestPositionEmptyDocument(t *testing.T) {
	got := Document{}.Position(7)
	if got.Line != 1 || got.Column != 1 {
		t.Errorf("empty document: got line %d col %d, want 1/1", got.Line, got.Column)
	}
}

func TestRange(t *testing.T) {
	r := testDoc().Range(1, 4)
	if r.Start.Line != 1 || r.Start.Column != 2 {
		t.Errorf("Start = line %d col %d, want 1/2", r.Start.Line, r.Start.Column)
	}
	if r.End.Line != 2 || r.End.Column != 2 {
		t.Errorf("End = line %d col %d, want 2/2", r.End.Line, r.End.Column)
	}
}
