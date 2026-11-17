package model

import (
	"fmt"
	"runtime"
	"testing"

	"github.com/jhnoel/documentdriver/internal/laws"
	"github.com/jhnoel/documentdriver/internal/ooxml/tree"
	"github.com/jhnoel/documentdriver/internal/vsdx/sheet"
	"github.com/jhnoel/documentdriver/internal/vsdx/vsd"
)

// TestBuildPageBudgetPerObject checks the MF5 budget row: BuildPage
// of Generated(5000), page parse included, costs at most 10 KB and
// 45 allocations per object, measured with laws.Measure over
// repeated BuildPage runs and divided by the object count.
func TestBuildPageBudgetPerObject(t *testing.T) {
	d, r := openGenerated(t, 5000)
	e, ok := d.Page(0)
	if !ok {
		t.Fatal("Generated(5000): no page 0")
	}
	p, err := BuildPage(d, r, e, Options{})
	if err != nil {
		t.Fatalf("BuildPage(Generated(5000)): %v", err)
	}
	objects := len(p.Objects)
	if objects == 0 {
		t.Fatal("Generated(5000): no objects")
	}
	runtime.KeepAlive(p)
	run := func() error {
		pp, err := BuildPage(d, r, e, Options{})
		if err != nil {
			return err
		}
		if len(pp.Objects) == 0 {
			return fmt.Errorf("Generated(5000): no objects")
		}
		runtime.KeepAlive(pp)
		return nil
	}
	if err := run(); err != nil {
		t.Fatalf("BuildPage warm: %v", err)
	}
	cost, err := laws.Measure(3, run)
	if err != nil {
		t.Fatalf("Measure: %v", err)
	}
	perBytes := float64(cost.Bytes) / float64(objects)
	perAllocs := float64(cost.Mallocs) / float64(objects)
	t.Logf("BuildPage Generated(5000): %d objects, %d bytes total (%.0f bytes per object, budget %d), %d allocations total (%.2f per object, budget 45), best time %s",
		objects, cost.Bytes, perBytes, 10*1024, cost.Mallocs, perAllocs, cost.Time)
	if perBytes > 10*1024 {
		t.Errorf("BuildPage allocates %.0f bytes per object, want at most %d (10 KB)", perBytes, 10*1024)
	}
	if perAllocs > 45 {
		t.Errorf("BuildPage makes %.2f allocations per object, want at most 45", perAllocs)
	}
}

// shapeNodes returns every Shape element under root, walking the
// tree iteratively.
func shapeNodes(root *tree.Node) []*tree.Node {
	var shapes []*tree.Node
	stack := []*tree.Node{root}
	for len(stack) > 0 {
		node := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if node.Kind() == tree.ElementNode && node.Local() == vsd.ElemShape {
			shapes = append(shapes, node)
		}
		for child := range node.ElementsSeq() {
			stack = append(stack, child)
		}
	}
	return shapes
}

// TestSheetIndexBudgetPerObject checks the MF5 budget row for
// sheet.Index: indexing Generated(5000)'s page shapes alone makes
// at most 5 allocations per shape, measured with laws.Measure over
// repeated runs and divided by the shape count.
func TestSheetIndexBudgetPerObject(t *testing.T) {
	d, _ := openGenerated(t, 5000)
	e, ok := d.Page(0)
	if !ok {
		t.Fatal("Generated(5000): no page 0")
	}
	contents, err := d.PageContents(e)
	if err != nil {
		t.Fatalf("PageContents(Generated(5000)): %v", err)
	}
	shapes := shapeNodes(contents.Root())
	if len(shapes) == 0 {
		t.Fatal("Generated(5000): no shapes")
	}
	run := func() error {
		for _, shape := range shapes {
			_, _ = sheet.Index(shape)
		}
		return nil
	}
	if err := run(); err != nil {
		t.Fatalf("sheet.Index warm: %v", err)
	}
	cost, err := laws.Measure(3, run)
	if err != nil {
		t.Fatalf("Measure: %v", err)
	}
	perAllocs := float64(cost.Mallocs) / float64(len(shapes))
	t.Logf("sheet.Index over Generated(5000) page shapes: %d shapes, %d allocations total (%.2f per shape, budget 5), %d bytes total (%.0f per shape)",
		len(shapes), cost.Mallocs, perAllocs, cost.Bytes, float64(cost.Bytes)/float64(len(shapes)))
	if perAllocs > 5 {
		t.Errorf("sheet.Index makes %.2f allocations per shape, want at most 5", perAllocs)
	}
}

// TestBuildPagePeakMemory30000 checks the MF5 peak row: peak live
// heap of BuildPage of Generated(30000) is at most 120 MB
// (laws.WithinMemory). The allocation budget is the per-object byte
// budget (10 KB) times the 45,000 objects of Generated(30000).
func TestBuildPagePeakMemory30000(t *testing.T) {
	d, r := openGenerated(t, 30000)
	e, ok := d.Page(0)
	if !ok {
		t.Fatal("Generated(30000): no page 0")
	}
	laws.WithinMemory(t, "vsdx BuildPage Generated(30000)", laws.Budget{PeakLive: laws.MB(120), Alloc: laws.MB(450)}, func(mark func(string)) error {
		p, err := BuildPage(d, r, e, Options{})
		if err != nil {
			return err
		}
		if len(p.Objects) == 0 {
			return fmt.Errorf("Generated(30000): no objects")
		}
		mark("BuildPage")
		runtime.KeepAlive(p)
		return nil
	})
}
