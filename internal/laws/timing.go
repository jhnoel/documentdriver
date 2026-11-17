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

// TimeRounds is the one fixed sample plan of TimeScales: it runs this
// many interleaved small/large pairs and compares the best time of
// each, once. There is no second measurement: a ratio over the bound
// fails (AGENTS.md: never retry a test to get green). Eleven pairs
// separate linear from quadratic work under parallel package load
// (TestTimeRatio).
const TimeRounds = 11

// TimeScales checks that run's wall time grows linearly: a quadratic
// loop that allocates nothing passes the allocation gate (Scales), so
// chunks whose loops can grow carry this too. It runs the small and the
// large input in TimeRounds interleaved pairs, so both see the same
// machine load, and compares the best time of each against
// MaxTimeRatio, one verdict, enforced in every run without -race (under
// -race it is not measured: the detector distorts ratios). Bytes and allocations are
// Scales' job and stay strict there.
func TimeScales(t *testing.T, name string, setup func(n int) (func() error, error)) {
	t.Helper()
	if RaceEnabled {
		// The race detector slows memory accesses unevenly, so time
		// ratios under -race say nothing about the code (make ci's race
		// step measured a linear op at 10.72). make check and make
		// scaling run without -race and give the verdict; Scales keeps
		// bytes and allocations enforced under -race too.
		t.Logf("%s: time not checked under -race", name)
		return
	}
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
	ratio, best := timeRatio(t, name, runs, TimeRounds)
	t.Logf("%s: best %v at %d units, %v at %d units: time ratio %.2f", name, best[0], Small, best[1], Large, ratio)
	if ratio > MaxTimeRatio {
		t.Errorf("%s takes %.2f times as long for %d times the input (bound %d, best of %d interleaved pairs): super-linear", name, ratio, Large/Small, MaxTimeRatio, TimeRounds)
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
