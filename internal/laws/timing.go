package laws

import (
	"fmt"
	"os"
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

// timeVerdictEnv is the environment variable that turns TimeScales'
// time ratio into a verdict: LAWS_TIME_VERDICT=1, a test-configuration
// variable like the corpus variables, never read by the binaries.
// make scaling sets it; every other run only logs the ratio, because
// package-level parallel load moves time ratios on linear work past
// the bound while bytes and allocations stay deterministic (TSG).
const timeVerdictEnv = "LAWS_TIME_VERDICT"

// TimeScales checks that run's wall time grows linearly: a quadratic
// loop that allocates nothing passes the allocation gate (Scales), so
// chunks whose loops can grow carry this too. It runs the small and the
// large input in TimeRounds interleaved pairs, so both see the same
// machine load, and compares the best time of each against
// MaxTimeRatio. The ratio is a verdict only when LAWS_TIME_VERDICT=1
// (make scaling, which runs packages one at a time); in every other
// run it is logged, so make check never fails on load. Under -race it
// is not measured at all: the detector distorts ratios. Bytes and
// allocations are Scales' job and stay strict there.
func TimeScales(t *testing.T, name string, setup func(n int) (func() error, error)) {
	t.Helper()
	if RaceEnabled {
		// The race detector slows memory accesses unevenly, so time
		// ratios under -race say nothing about the code (make ci's race
		// step measured a linear op at 10.72). make check and make
		// scaling run without -race; Scales keeps bytes and allocations
		// enforced under -race too.
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
	_, best := timeRatio(t, name, runs, TimeRounds)
	if os.Getenv(timeVerdictEnv) == "1" {
		t.Logf("%s: best %v at n=%d, %v at n=%d (setup's n): time ratio %.2f",
			name, best[0], Small, best[1], Large, float64(best[1])/float64(max(best[0], 1)))
		if fail := timeVerdict(name, best); fail != "" {
			t.Error(fail)
		}
		return
	}
	t.Logf("%s: time ratio %.2f (verdict in make scaling)", name, float64(best[1])/float64(max(best[0], 1)))
}

// timeVerdict returns the TimeScales failure of name at best times, or
// "" when the large run scales: the ratio of the best large time to
// the best small time is at most MaxTimeRatio, the bound inclusive.
func timeVerdict(name string, best [2]time.Duration) string {
	ratio := float64(best[1]) / float64(max(best[0], 1))
	if ratio > MaxTimeRatio {
		return fmt.Sprintf("%s takes %.2f times as long for %d times the input (bound %d, best of %d interleaved pairs): super-linear",
			name, ratio, Large/Small, MaxTimeRatio, TimeRounds)
	}
	return ""
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
