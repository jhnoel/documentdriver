package vsd

import "testing"

// TestBuiltinColor checks every palette entry against the designer's
// rows (chunk V3) and the bounds.
func TestBuiltinColor(t *testing.T) {
	want := []string{
		"#000000", "#FFFFFF", "#FF0000", "#00FF00", "#0000FF", "#FFFF00",
		"#FF00FF", "#00FFFF", "#800000", "#008000", "#000080", "#808000",
		"#800080", "#008080", "#C0C0C0", "#E6E6E6", "#CDCDCD", "#B3B3B3",
		"#9A9A9A", "#808080", "#666666", "#4D4D4D", "#333333", "#1A1A1A",
	}
	for ix, w := range want {
		if got, ok := BuiltinColor(ix); !ok || got != w {
			t.Errorf("BuiltinColor(%d) = %q, %v; want %q", ix, got, ok, w)
		}
	}
	for _, ix := range []int{24, -1, 255} {
		if got, ok := BuiltinColor(ix); ok {
			t.Errorf("BuiltinColor(%d) = %q, accepted", ix, got)
		}
	}
}
