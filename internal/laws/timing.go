package laws

import (
	"runtime"
	"testing"
	"time"
)

// MaxTimeRatio bounds the time of TimeScales' large run against its
// small one. Linear work gives about Large/Small (4), quadratic 16; the
// bound leaves room for noise the interleaving does not remove.
const MaxTimeRatio = 8

// timeRounds is how many interleaved small/large pairs TimeScales runs,
// and timeRetry how many more it runs before failing a ratio over the
// bound.
const (
	timeRounds = 7
	timeRetry  = 15
)

// TimeScales checks that run's wall time grows linearly: a quadratic
// loop that allocates nothing passes the allocation gate (Scales), so
// chunks whose loops can grow carry this too. It runs the small and the
// large input in interleaved pairs, so both see the same machine load,
// and compares the best time of each; a ratio over MaxTimeRatio is
// measured again with more pairs before it fails. With -short, or when
// GOMAXPROCS is 1 (a contended runner), the ratio is logged, not
// checked. Bytes and allocations are Scales' job and stay strict there.
func TimeScales(t *testing.T, name string, setup func(n int) (func() error, error)) {
	t.Helper()
	var runs [2]func() error
	for i, n := range []int{Small, Large} {
		run, err := setup(n)
		if err != nil {
			t.Fatalf("%s: setup at %d: %v", name, n, err)
		}
		if err := run(); err != nil { // warm
			t.Fatalf("%s at %d: %v", name, n, err)
		}
		runs[i] = run
	}
	ratio, best := timeRatio(t, name, runs, timeRounds)
	if ratio > MaxTimeRatio {
		ratio, best = timeRatio(t, name, runs, timeRetry)
	}
	t.Logf("%s: best %v at %d units, %v at %d units: time ratio %.2f", name, best[0], Small, best[1], Large, ratio)
	if testing.Short() || runtime.GOMAXPROCS(0) < 2 {
		return
	}
	if ratio > MaxTimeRatio {
		t.Errorf("%s takes %.2f times as long for %d times the input (bound %d, best of %d interleaved pairs): super-linear", name, ratio, Large/Small, MaxTimeRatio, timeRetry)
	}
}

// timeRatio runs the pair rounds times, interleaved, and returns the
// ratio of the best large time to the best small time and both bests.
func timeRatio(t *testing.T, name string, runs [2]func() error, rounds int) (float64, [2]time.Duration) {
	t.Helper()
	var best [2]time.Duration
	for r := range rounds {
		for i, run := range runs {
			runtime.GC()
			start := time.Now()
			if err := run(); err != nil {
				t.Fatalf("%s: %v", name, err)
			}
			if d := time.Since(start); r == 0 || d < best[i] {
				best[i] = d
			}
		}
	}
	return float64(best[1]) / float64(max(best[0], 1)), best
}
