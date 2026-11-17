package doc

import (
	"fmt"
	"runtime"
	"testing"

	"github.com/jhnoel/documentdriver/internal/laws"
	"github.com/jhnoel/documentdriver/internal/opc"
	"github.com/jhnoel/documentdriver/internal/vsdx/vsd"
	"github.com/jhnoel/documentdriver/internal/vsdx/vsdxtest"
)

// openGenerated opens a Generated drawing of n shapes and returns its
// Document.
func openGenerated(t *testing.T, n int) *Document {
	t.Helper()
	p, err := opc.Open(vsdxtest.Generated(t, n))
	if err != nil {
		t.Fatalf("opc.Open(Generated(%d)): %v", n, err)
	}
	t.Cleanup(func() {
		if err := p.Close(); err != nil {
			t.Errorf("close Generated(%d): %v", n, err)
		}
	})
	d, err := Open(p)
	if err != nil {
		t.Fatalf("Open(Generated(%d)): %v", n, err)
	}
	return d
}

// TestPageContentsV1 checks the V1 row: PageContents of v1-minimal's
// page 0 returns a tree whose root is PageContents in 2012/main, and a
// second call (after the first part was forgotten) returns a new tree.
func TestPageContentsV1(t *testing.T) {
	d := openFixture(t, "v1-minimal")
	e, ok := d.Page(0)
	if !ok {
		t.Fatalf("Page(0) not found, Pages = %v", d.Pages)
	}
	first, err := d.PageContents(e)
	if err != nil {
		t.Fatalf("PageContents(page0): %v", err)
	}
	root := first.Root()
	if root == nil || root.Local() != vsd.ElemPageContents || root.Space() != vsd.NSMain2012 {
		t.Errorf("PageContents root = %v, want PageContents in 2012/main", root)
	}
	second, err := d.PageContents(e)
	if err != nil {
		t.Fatalf("second PageContents(page0): %v", err)
	}
	if first == second {
		t.Error("second PageContents(page0) returned the first tree, want a new tree")
	}
	if _, err := d.PageContents(nil); err == nil {
		t.Error("PageContents(nil) succeeded, want an error")
	}
}

// TestPageContentsV10 checks the V10 row: page 2 of v10-defects, whose
// part is not well formed, is an error (and so is page 4, which has no
// part).
func TestPageContentsV10(t *testing.T) {
	d := openFixture(t, "v10-defects")
	e, ok := d.Page(2)
	if !ok {
		t.Fatalf("Page(2) not found, Pages = %v", d.Pages)
	}
	if doc, err := d.PageContents(e); err == nil || doc != nil {
		t.Errorf("PageContents(page2) = %v, %v, want nil, an error", doc, err)
	}
	missing, ok := d.Page(4)
	if !ok {
		t.Fatalf("Page(4) not found, Pages = %v", d.Pages)
	}
	if doc, err := d.PageContents(missing); err == nil || doc != nil {
		t.Errorf("PageContents(page4) = %v, %v, want nil, an error", doc, err)
	}
}

// TestMasterContentsV2 checks the V2 row: MasterContents(2) of
// v2-master twice returns the same *tree.Document, whose root is
// MasterContents in 2012/main.
func TestMasterContentsV2(t *testing.T) {
	d := openFixture(t, "v2-master")
	first, err := d.MasterContents(2)
	if err != nil {
		t.Fatalf("MasterContents(2): %v", err)
	}
	root := first.Root()
	if root == nil || root.Local() != vsd.ElemMasterContents || root.Space() != vsd.NSMain2012 {
		t.Errorf("MasterContents(2) root = %v, want MasterContents in 2012/main", root)
	}
	second, err := d.MasterContents(2)
	if err != nil {
		t.Fatalf("second MasterContents(2): %v", err)
	}
	if first != second {
		t.Error("MasterContents(2) twice returned different trees, want the same *tree.Document")
	}
}

// TestMasterContents99 checks that MasterContents(99) of v2-master,
// which has no master 99, is an error.
func TestMasterContents99(t *testing.T) {
	d := openFixture(t, "v2-master")
	if doc, err := d.MasterContents(99); err == nil || doc != nil {
		t.Errorf("MasterContents(99) = %v, %v, want nil, an error", doc, err)
	}
}

// TestPageContentsMemory checks the Generated(1000) row: after
// PageContents of a Generated(1000) drawing is dropped, live heap is
// within 1 MB of before (runtime.GC, ReadMemStats).
func TestPageContentsMemory(t *testing.T) {
	if laws.RaceEnabled {
		t.Skip("memory is not measured under the race detector")
	}
	d := openGenerated(t, 1000)
	e, ok := d.Page(0)
	if !ok {
		t.Fatalf("Page(0) not found, Pages = %v", d.Pages)
	}
	var before, after runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&before)
	doc, err := d.PageContents(e)
	if err != nil {
		t.Fatalf("PageContents(Generated(1000)): %v", err)
	}
	if doc.Root() == nil {
		t.Fatal("PageContents(Generated(1000)) root is nil")
	}
	doc = nil
	runtime.GC()
	runtime.ReadMemStats(&after)
	t.Logf("live heap before %d, after %d, delta %d (budget %d)",
		before.HeapAlloc, after.HeapAlloc, int64(after.HeapAlloc)-int64(before.HeapAlloc), 1<<20)
	if after.HeapAlloc > before.HeapAlloc+(1<<20) {
		t.Errorf("live heap after dropping PageContents = %d, more than 1 MB over before %d",
			after.HeapAlloc, before.HeapAlloc)
	}
}

// TestScalingPageContents checks the scaling row: PageContents on
// Generated(1000) and Generated(4000) (N = 1000, 4N = 4000) allocates
// at most 5.5 times as much (laws.Scales) and the best-of-5 time ratio
// is at most 8.
func TestScalingPageContents(t *testing.T) {
	setup := func(n int) (func() error, error) {
		d := openGenerated(t, n)
		e, ok := d.Page(0)
		if !ok {
			return nil, fmt.Errorf("Generated(%d): no page 0", n)
		}
		return func() error {
			doc, err := d.PageContents(e)
			if err != nil {
				return err
			}
			if doc.Root() == nil {
				return fmt.Errorf("Generated(%d): PageContents root is nil", n)
			}
			return nil
		}, nil
	}
	laws.Scales(t, "vsdx PageContents", setup)
	var costs [2]laws.Cost
	for i, n := range []int{laws.Small, laws.Large} {
		run, err := setup(n)
		if err != nil {
			t.Fatalf("setup at %d: %v", n, err)
		}
		if costs[i], err = laws.Measure(5, run); err != nil {
			t.Fatalf("at %d: %v", n, err)
		}
	}
	tm := float64(costs[1].Time) / float64(max(costs[0].Time, 1))
	t.Logf("vsdx PageContents: %d units best time %s, %d units best time %s: time ratio %.2f",
		laws.Small, costs[0].Time, laws.Large, costs[1].Time, tm)
	if tm > 8 {
		t.Errorf("vsdx PageContents time ratio %.2f for 4 times the input (budget 8): super-linear", tm)
	}
}
