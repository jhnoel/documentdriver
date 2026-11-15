package formula

import "testing"

// TestShiftKeepsUnmovedSpelling: Shift rewrites only the references it
// moves (in Excel's form); an unmoved one keeps its spelling, leading
// zeros and lowercase included (the law FuzzSheetData relies on).
func TestShiftKeepsUnmovedSpelling(t *testing.T) {
	sh := Sheets{Own: "Budget"}
	for _, c := range []struct {
		src, want string
		at        int
	}{
		{"A01+b2+Z0001", "A01+b2+AA1", 5},
		{"A01+b2", "B1+C2", 1},
	} {
		got, err := Shift(c.src, sh, Edit{Sheet: "Budget", Kind: InsertCols, At: c.at, Count: 1})
		if err != nil || got != c.want {
			t.Errorf("Shift(%q) at %d = %q, %v; want %q", c.src, c.at, got, err, c.want)
		}
	}
}
