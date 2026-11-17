package doc

import "testing"

// TestColorResolutionV1 checks the D4 rows on V1's document: the stored
// hex "#4472c4", the document indexes 24 and 25, the built-in indexes
// 0, 1, 2 and 4, and the values that resolve to nothing.
func TestColorResolutionV1(t *testing.T) {
	d := openFixture(t, "v1-minimal")
	for _, tc := range []struct {
		in   string
		want string
		ok   bool
	}{
		{"#4472c4", "#4472C4", true},
		{"24", "#7F7F7F", true},
		{"25", "#5B9BD5", true},
		{"0", "#000000", true},
		{"1", "#FFFFFF", true},
		{"2", "#FF0000", true},
		{"4", "#0000FF", true},
		{"26", "", false},
		{"255", "", false},
		{"Themed", "", false},
		{"", "", false},
		{" 1", "", false},
		{"-1", "", false},
	} {
		got, ok := d.Color(tc.in)
		if ok != tc.ok || (ok && got != tc.want) {
			t.Errorf("Color(%q) = %q, %v; want %q, %v", tc.in, got, ok, tc.want, tc.ok)
		}
	}
}
