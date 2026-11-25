package vsd

import "testing"

// TestTextBackground checks the stored TextBkgnd forms: 0 (and any
// number below 1) none, #RRGGBB a colour, n of 1 to 255 the index
// n-1, and Themed, other text and numbers past 255 not derived.
func TestTextBackground(t *testing.T) {
	for _, c := range []struct {
		v     string
		none  bool
		hex   string
		index int
		ok    bool
	}{
		{"0", true, "", -1, true},
		{"0.5", true, "", -1, true},
		{"#ffffff", false, "#FFFFFF", -1, true},
		{"2", false, "", 1, true},
		{"255", false, "", 254, true},
		{"256", false, "", -1, false},
		{"Themed", false, "", -1, false},
		{"", false, "", -1, false},
	} {
		none, hex, index, ok := TextBackground(c.v)
		if none != c.none || hex != c.hex || index != c.index || ok != c.ok {
			t.Errorf("TextBackground(%q) = %v %q %d %v, want %v %q %d %v", c.v, none, hex, index, ok, c.none, c.hex, c.index, c.ok)
		}
	}
}
