package formula

import (
	"math"
	"testing"
)

// TestCellBounds checks the CC1 clamp rows: CellBounds has one
// entry, the Character section's Size cell at a lower bound of
// 1/72 in (1 pt) with no upper bound, and Clamp lifts a smaller
// value to that bound and leaves a larger one alone, while a cell
// with no bounds is left unchanged.
func TestCellBounds(t *testing.T) {
	rows := []struct {
		in string
		ok bool
	}{
		{"Size", true},
		{"size", true},
		{"Char.Size[1]", true},
		{"Char.Size[2]", true},
		{"char.size[10]", true},
		{"Character.Size", true},
		{"Width", false},
		{"Char.Color[1]", false},
		{"User.Size", false},
		{"SizeX", false},
		{"", false},
	}
	for _, row := range rows {
		lo, hi, ok := CellBounds(row.in)
		if ok != row.ok {
			t.Errorf("CellBounds(%q) ok = %v, want %v", row.in, ok, row.ok)
			continue
		}
		if ok && (lo != 1.0/72.0 || !math.IsInf(hi, 1)) {
			t.Errorf("CellBounds(%q) = (%v, %v), want (1/72, +Inf)", row.in, lo, hi)
		}
	}
	clamps := []struct {
		in   string
		v    float64
		want float64
	}{
		{"Char.Size[1]", 0, 1.0 / 72.0},
		{"Char.Size[1]", 0.5 / 72.0, 1.0 / 72.0},
		{"Size", 0, 1.0 / 72.0},
		{"Char.Size[1]", 1.0 / 72.0, 1.0 / 72.0},
		{"Char.Size[1]", 12.0 / 72.0, 12.0 / 72.0},
		{"Width", 0, 0},
		{"Width", -1, -1},
		{"", 0, 0},
	}
	for _, row := range clamps {
		if got := Clamp(row.in, row.v); got != row.want {
			t.Errorf("Clamp(%q, %v) = %v, want %v", row.in, row.v, got, row.want)
		}
	}
}
