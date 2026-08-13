package terminal

import "testing"

func TestNormalize(t *testing.T) {
	tests := []struct {
		name string
		in   Size
		want Size
	}{
		{"usable size kept", Size{100, 40}, Size{100, 40}},
		{"minimum accepted", Size{MinWidth, MinHeight}, Size{MinWidth, MinHeight}},
		{"narrow falls back", Size{4, 40}, Size{FallbackWidth, 40}},
		{"short falls back", Size{100, 1}, Size{100, FallbackHeight}},
		{"zero falls back", Size{0, 0}, Size{FallbackWidth, FallbackHeight}},
		{"negative falls back", Size{-5, -5}, Size{FallbackWidth, FallbackHeight}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := Normalize(tt.in); got != tt.want {
				t.Errorf("Normalize(%+v) = %+v, want %+v", tt.in, got, tt.want)
			}
		})
	}
}
