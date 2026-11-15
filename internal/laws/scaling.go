package laws

import (
	"fmt"
	"runtime"
	"testing"
	"time"
)

// The scaling gate (make scaling, part of make ci): an operation run on
// inputs of two sizes, Small and Large units, may cost at most MaxRatio
// times as much on the large one, in bytes allocated and in allocations.
// Linear work gives about Large/Small (4); quadratic work gives 16. Both
// are deterministic, so the gate holds on a loaded machine. Time is
// reported, never checked: a loaded machine slows the small and the large
// run differently (go test ./... running packages side by side measured
// a linear read at 10.9). The budget is AGENTS.md's (Performance and
// memory).
const (
	Small    = 1000
	Large    = 4000
	MaxRatio = 5.5
)

// Cost is what one run of an operation cost.
type Cost struct {
	Bytes   uint64        // allocated, in total
	Mallocs uint64        // allocations, in number
	Time    time.Duration // wall time, the best of the runs
}

// Measure runs f runs times (at least once) and returns the bytes and
// allocations of the first run and the best time of all runs.
func Measure(runs int, f func() error) (Cost, error) {
	var c Cost
	for i := range max(runs, 1) {
		var before, after runtime.MemStats
		runtime.GC()
		runtime.ReadMemStats(&before)
		start := time.Now()
		if err := f(); err != nil {
			return Cost{}, err
		}
		d := time.Since(start)
		runtime.ReadMemStats(&after)
		if i == 0 {
			c.Bytes = after.TotalAlloc - before.TotalAlloc
			c.Mallocs = after.Mallocs - before.Mallocs
		}
		if i == 0 || d < c.Time {
			c.Time = d
		}
	}
	return c, nil
}

// Scales measures run on an input built for Small and for Large units
// (setup returns the operation to time, which may reuse what setup
// built) and fails t when the large run allocates more than MaxRatio
// times the bytes or allocations of the small one. The time ratio is
// logged only.
func Scales(t *testing.T, name string, setup func(n int) (func() error, error)) {
	t.Helper()
	var costs [2]Cost
	for i, n := range []int{Small, Large} {
		run, err := setup(n)
		if err != nil {
			t.Fatalf("%s: setup at %d: %v", name, n, err)
		}
		if err := run(); err != nil { // warm: lazy tables, first-use caches
			t.Fatalf("%s at %d: %v", name, n, err)
		}
		if costs[i], err = Measure(2, run); err != nil {
			t.Fatalf("%s at %d: %v", name, n, err)
		}
	}
	bytes := float64(costs[1].Bytes) / float64(max(costs[0].Bytes, 1))
	mallocs := float64(costs[1].Mallocs) / float64(max(costs[0].Mallocs, 1))
	tm := float64(costs[1].Time) / float64(max(costs[0].Time, 1))
	t.Logf("%s: %d units %s %d allocs %s, %d units %s %d allocs %s: ratio bytes %.2f, allocs %.2f, time %.2f", name,
		Small, mb(costs[0].Bytes), costs[0].Mallocs, costs[0].Time.Round(time.Millisecond),
		Large, mb(costs[1].Bytes), costs[1].Mallocs, costs[1].Time.Round(time.Millisecond), bytes, mallocs, tm)
	if bytes > MaxRatio {
		t.Errorf("%s allocates %.2f times the bytes for %d times the input (budget %.1f): super-linear", name, bytes, Large/Small, MaxRatio)
	}
	if mallocs > MaxRatio {
		t.Errorf("%s makes %.2f times the allocations for %d times the input (budget %.1f): super-linear", name, mallocs, Large/Small, MaxRatio)
	}
}

// mb formats a byte count as megabytes with one decimal.
func mb(b uint64) string { return fmt.Sprintf("%.1fMB", float64(b)/(1<<20)) }
