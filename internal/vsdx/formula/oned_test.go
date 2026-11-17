package formula

import (
	"math"
	"testing"
)

// TestOneD checks the 1-D transform kernel against the designer's rows
// (chunk K2) and the non-finite rows (chunk K2F): any argument NaN or
// infinite, and ends (0,0) and (1e308,1e308) whose stored SQRT width
// overflows, are ok false; further rows pin and delta overflow.
func TestOneD(t *testing.T) {
	rows := []struct {
		beginX, beginY, endX, endY float64
		pinX, pinY, width, angle   float64
		ok                         bool
	}{
		{3, 9.5, 5, 9.5, 4, 9.5, 2, 0, true},
		{3, 8, 3, 7, 3, 7.5, 1, -1.5707963267948966, true},
		{0, 0, 3, 4, 1.5, 2, 5, 0.9272952180016122, true},
		{5, 9.5, 3, 9.5, 4, 9.5, 2, math.Pi, true},
		{1, 1, 1, 1, 0, 0, 0, 0, false},
		{math.NaN(), 0, 1, 1, 0, 0, 0, 0, false},
		{0, math.NaN(), 1, 1, 0, 0, 0, 0, false},
		{0, 0, math.NaN(), 1, 0, 0, 0, 0, false},
		{0, 0, 1, math.NaN(), 0, 0, 0, 0, false},
		{math.Inf(1), 0, 1, 1, 0, 0, 0, 0, false},
		{0, math.Inf(1), 1, 1, 0, 0, 0, 0, false},
		{0, 0, math.Inf(1), 1, 0, 0, 0, 0, false},
		{0, 0, 1, math.Inf(1), 0, 0, 0, 0, false},
		{math.Inf(-1), 0, 1, 1, 0, 0, 0, 0, false},
		{0, math.Inf(-1), 1, 1, 0, 0, 0, 0, false},
		{0, 0, math.Inf(-1), 1, 0, 0, 0, 0, false},
		{0, 0, 1, math.Inf(-1), 0, 0, 0, 0, false},
		{0, 0, 1e308, 1e308, 0, 0, 0, 0, false},
		{1e308, 0, 1e308, 1, 0, 0, 0, 0, false},
		{-1e308, 0, 1e308, 0, 0, 0, 0, 0, false},
		{0, 0, 1.5e308, 1.5e308, 0, 0, 0, 0, false},
	}
	for _, r := range rows {
		pinX, pinY, width, angle, ok := OneD(r.beginX, r.beginY, r.endX, r.endY)
		if ok != r.ok {
			t.Errorf("OneD(%v, %v, %v, %v) ok = %v, want %v", r.beginX, r.beginY, r.endX, r.endY, ok, r.ok)
		}
		if !r.ok {
			continue
		}
		if pinX != r.pinX || pinY != r.pinY || width != r.width || angle != r.angle {
			t.Errorf("OneD(%v, %v, %v, %v) = %v, %v, %v, %v, %v; want %v, %v, %v, %v, %v",
				r.beginX, r.beginY, r.endX, r.endY, pinX, pinY, width, angle, ok, r.pinX, r.pinY, r.width, r.angle, r.ok)
		}
	}
}
