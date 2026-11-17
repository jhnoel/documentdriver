package laws

import (
	"testing"
)

// spin does n*n (quadratic) or n (linear) steps of work that allocate
// nothing.
func spin(n int, quadratic bool) int {
	s := 0
	m := n
	if quadratic {
		m = n * n
	}
	for i := range m {
		s += i ^ (s >> 3)
	}
	return s
}

// TestTimeRatio checks the measurement TimeScales relies on: interleaved
// best-of times give a ratio near 4 for linear work and well over the
// bound for quadratic work at the gate's sizes (scaled up so the runs
// last long enough to time).
func TestTimeRatio(t *testing.T) {
	if testing.Short() {
		t.Skip("timing")
	}
	sink := 0
	mk := func(quadratic bool, scale int) [2]func() error {
		var r [2]func() error
		for i, n := range []int{Small, Large} {
			n := n * scale
			r[i] = func() error { sink += spin(n, quadratic); return nil }
		}
		return r
	}
	lin, _ := timeRatio(t, "linear", mk(false, 2000), timeRounds)
	quad, _ := timeRatio(t, "quadratic", mk(true, 1), timeRounds)
	t.Logf("linear %.2f, quadratic %.2f (sink %d)", lin, quad, sink)
	if lin > MaxTimeRatio {
		t.Errorf("linear work measured at ratio %.2f, over the bound %d", lin, MaxTimeRatio)
	}
	if quad < MaxTimeRatio {
		t.Errorf("quadratic work measured at ratio %.2f, under the bound %d: the check cannot fail", quad, MaxTimeRatio)
	}
}
