package read

import (
	"fmt"
	"os"
	"runtime"
	"testing"

	"github.com/jhnoel/documentdriver/internal/laws"
	"github.com/jhnoel/documentdriver/internal/vsdx/vsdxtest"
)

// The RM1 memory gate (make scaling): read.Page default, --detail
// full and Find on Generated(5000) and Generated(20000) (7,500 and
// 30,000 objects) hold peak live heap at most 2 KB per object plus
// 3 times the output's bytes, and Find at most 45 MB, and the
// 20k/5k peak ratio is at most 5.5. Page and Find have no checkpoint
// of their own, so each run marks after the read with the output
// kept alive, as the older TestMemoryBudget does. The allocation
// budgets are the measured numbers with about twenty percent of
// margin (default: 79.5 MB at 5k, 322.8 MB at 20k; full: 136.4 MB
// and 551.0 MB; find: 56.2 MB and 228.6 MB).

// generatedBytes returns the bytes of Generated(n).
func generatedBytes(t *testing.T, n int) []byte {
	t.Helper()
	data, err := os.ReadFile(vsdxtest.Generated(t, n))
	if err != nil {
		t.Fatalf("read Generated(%d): %v", n, err)
	}
	return data
}

// memoryCeiling is the peak-live ceiling for an operation with
// objects objects and outBytes bytes of output: 2 KB per object plus
// 3 times the output.
func memoryCeiling(objects, outBytes int) uint64 {
	return uint64(objects)*2048 + 3*uint64(outBytes)
}

// peakRatio checks the 20k/5k peak-live ratio row over the runs of
// one operation: each run marks after the read with its result kept
// alive, and the ratio of the two peaks is at most 5.5.
func peakRatio(t *testing.T, name string, run func(n int, mark func(string)) error) {
	t.Helper()
	var peaks [2]uint64
	for i, n := range []int{5000, 20000} {
		m, err := laws.MeasureMemory(func(mark func(string)) error {
			return run(n, mark)
		})
		if err != nil {
			t.Fatalf("%s Generated(%d): %v", name, n, err)
		}
		peaks[i] = m.PeakLive
	}
	ratio := float64(peaks[1]) / float64(max(peaks[0], 1))
	t.Logf("%s: peak live Generated(5000) %d bytes, Generated(20000) %d bytes, ratio %.2f", name, peaks[0], peaks[1], ratio)
	if ratio > 5.5 {
		t.Errorf("%s: peak live ratio 20k/5k is %.2f, want at most 5.5", name, ratio)
	}
}

// TestMemoryBudgetPageDefault is the RM1 gate for read.Page default:
// Generated(5000) and Generated(20000) stay under 2 KB per object
// plus 3 times the output, and the peak ratio is at most 5.5.
func TestMemoryBudgetPageDefault(t *testing.T) {
	alloc := map[int]uint64{5000: laws.MB(96), 20000: laws.MB(388)}
	for _, n := range []int{5000, 20000} {
		data := generatedBytes(t, n)
		out, err := Page(data, "B", "page0", Options{})
		if err != nil {
			t.Fatalf("Page(Generated(%d)): %v", n, err)
		}
		budget := laws.Budget{PeakLive: memoryCeiling(n+n/2, len(out)), Alloc: alloc[n]}
		laws.WithinMemory(t, fmt.Sprintf("vsdx Page default Generated(%d)", n), budget, func(mark func(string)) error {
			out, err := Page(data, "B", "page0", Options{})
			if err != nil {
				return err
			}
			mark("page read")
			runtime.KeepAlive(out)
			runtime.KeepAlive(data)
			if len(out) == 0 {
				return fmt.Errorf("Page(Generated(%d)): empty output", n)
			}
			return nil
		})
	}
	peakRatio(t, "vsdx Page default", func(n int, mark func(string)) error {
		data := generatedBytes(t, n)
		out, err := Page(data, "B", "page0", Options{})
		if err != nil {
			return err
		}
		mark("page read")
		runtime.KeepAlive(out)
		return nil
	})
}

// TestMemoryBudgetPageFull is the RM1 gate for --detail full, as
// TestMemoryBudgetPageDefault for the full view.
func TestMemoryBudgetPageFull(t *testing.T) {
	alloc := map[int]uint64{5000: laws.MB(164), 20000: laws.MB(662)}
	for _, n := range []int{5000, 20000} {
		data := generatedBytes(t, n)
		out, err := Page(data, "B", "page0", Options{Detail: "full"})
		if err != nil {
			t.Fatalf("Page(Generated(%d), full): %v", n, err)
		}
		budget := laws.Budget{PeakLive: memoryCeiling(n+n/2, len(out)), Alloc: alloc[n]}
		laws.WithinMemory(t, fmt.Sprintf("vsdx Page full Generated(%d)", n), budget, func(mark func(string)) error {
			out, err := Page(data, "B", "page0", Options{Detail: "full"})
			if err != nil {
				return err
			}
			mark("page read")
			runtime.KeepAlive(out)
			runtime.KeepAlive(data)
			if len(out) == 0 {
				return fmt.Errorf("Page(Generated(%d), full): empty output", n)
			}
			return nil
		})
	}
	peakRatio(t, "vsdx Page full", func(n int, mark func(string)) error {
		data := generatedBytes(t, n)
		out, err := Page(data, "B", "page0", Options{Detail: "full"})
		if err != nil {
			return err
		}
		mark("page read")
		runtime.KeepAlive(out)
		return nil
	})
}

// TestMemoryBudgetFind is the RM1 gate for Find: Generated(5000)
// and Generated(20000) stay under 2 KB per object (at most 45 MB),
// and the peak ratio is at most 5.5.
func TestMemoryBudgetFind(t *testing.T) {
	alloc := map[int]uint64{5000: laws.MB(68), 20000: laws.MB(275)}
	for _, n := range []int{5000, 20000} {
		data := generatedBytes(t, n)
		res, err := Find(data, "B", FindRequest{Query: "Box"})
		if err != nil {
			t.Fatalf("Find(Generated(%d)): %v", n, err)
		}
		if len(res.Matches) != n {
			t.Fatalf("Find(Generated(%d)): len(Matches) = %d, want %d", n, len(res.Matches), n)
		}
		ceiling := min(uint64(n+n/2)*2048, laws.MB(45))
		budget := laws.Budget{PeakLive: ceiling, Alloc: alloc[n]}
		laws.WithinMemory(t, fmt.Sprintf("vsdx Find Generated(%d)", n), budget, func(mark func(string)) error {
			res, err := Find(data, "B", FindRequest{Query: "Box"})
			if err != nil {
				return err
			}
			mark("find read")
			runtime.KeepAlive(res)
			runtime.KeepAlive(data)
			if len(res.Matches) != n {
				return fmt.Errorf("Find(Generated(%d)): len(Matches) = %d, want %d", n, len(res.Matches), n)
			}
			return nil
		})
	}
	peakRatio(t, "vsdx Find", func(n int, mark func(string)) error {
		data := generatedBytes(t, n)
		res, err := Find(data, "B", FindRequest{Query: "Box"})
		if err != nil {
			return err
		}
		mark("find read")
		runtime.KeepAlive(res)
		return nil
	})
}

// TestScalingReadPageFull checks the scaling row: Page --detail full
// on Generated(1000) and Generated(4000) (N = 1000, 4N = 4000 shapes)
// allocates at most 5.5 times as much (laws.Scales) and the
// laws.TimeScales time ratio is at most 8.
func TestScalingReadPageFull(t *testing.T) {
	setup := func(n int) (func() error, error) {
		data, err := os.ReadFile(vsdxtest.Generated(t, n))
		if err != nil {
			return nil, err
		}
		return func() error {
			got, err := Page(data, "B", "page0", Options{Detail: "full"})
			if err != nil {
				return err
			}
			if len(got) == 0 {
				return fmt.Errorf("Generated(%d): empty output", n)
			}
			return nil
		}, nil
	}
	laws.Scales(t, "vsdx Page full shapes", setup)
	laws.TimeScales(t, "vsdx Page full shapes", setup)
}
