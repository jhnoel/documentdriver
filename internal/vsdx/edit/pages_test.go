package edit

import (
	"bytes"
	"errors"
	"fmt"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/jhnoel/documentdriver/internal/laws"
	"github.com/jhnoel/documentdriver/internal/ooxml/ctypes"
	"github.com/jhnoel/documentdriver/internal/opc"
	"github.com/jhnoel/documentdriver/internal/vsdx/emit"
	"github.com/jhnoel/documentdriver/internal/vsdx/vsd"
	"github.com/jhnoel/documentdriver/internal/vsdx/vsdxtest"
)

// TestAddPageOnBlank checks the E2 row: AddPage on the blank returns
// page1, its part is /visio/pages/page2.xml with one Override and one
// relationship rId2 in pages.xml.rels, and its entry follows page0.
func TestAddPageOnBlank(t *testing.T) {
	w := mustBegin(t, blankPath(t))
	addr, err := w.AddPage(func(relID string) ([]byte, error) {
		return emit.PageXML(emit.Page{ID: 99, Name: "Wide", Width: 17, Height: 11, Orientation: 2, RelID: relID})
	}, "")
	if err != nil {
		t.Fatalf("AddPage: %v", err)
	}
	if addr != "page1" {
		t.Errorf("AddPage returned %q, want page1", addr)
	}
	if err := w.Verify(); err != nil {
		t.Fatalf("Verify: %v", err)
	}
	d, err := w.Core().Tree("/visio/pages/pages.xml")
	if err != nil {
		t.Fatalf("Tree(pages.xml): %v", err)
	}
	var ids []uint32
	for p := range d.Root().ElementsSeq() {
		if !isElem(p, vsd.ElemPage) {
			continue
		}
		if id, ok := shapeID(p); ok {
			ids = append(ids, id)
		}
	}
	if len(ids) != 2 || ids[0] != 0 || ids[1] != 1 {
		t.Errorf("pages order = %v, want [0 1] (entry after page0)", ids)
	}
	page1 := findPageElem(d.Root(), 1, 1)
	if page1 == nil {
		t.Fatal("no page1 entry")
	}
	if n, _ := page1.Attr("Name"); n != "Wide" {
		t.Errorf("page1 Name = %q, want Wide", n)
	}
	rel := child(page1, "Rel")
	if rel == nil {
		t.Fatal("page1 has no Rel")
	}
	if got, _ := rel.AttrNS(vsd.NSRel, "id"); got != "rId2" {
		t.Errorf("page1 Rel r:id = %q, want rId2", got)
	}
	pt, err := w.Package().Part("/visio/pages/page2.xml")
	if err != nil {
		t.Fatalf("Part(page2.xml): %v", err)
	}
	body, err := pt.Bytes()
	if err != nil {
		t.Fatalf("Bytes(page2.xml): %v", err)
	}
	if !bytes.Equal(body, emit.PageContentsXML()) {
		t.Errorf("page2.xml = %q, want the empty page contents", body)
	}
	if ct, err := pt.ContentType(); err != nil || ct != ctypes.CTMsVisioPage {
		t.Errorf("page2.xml content type = %q, %v, want %q", ct, err, ctypes.CTMsVisioPage)
	}
	ctPart, err := w.Package().Part(opc.ContentTypesName)
	if err != nil {
		t.Fatalf("Part(content types): %v", err)
	}
	ctBytes, err := ctPart.Bytes()
	if err != nil {
		t.Fatalf("Bytes(content types): %v", err)
	}
	if got := strings.Count(string(ctBytes), "/visio/pages/page2.xml"); got != 1 {
		t.Errorf("content types hold the page2.xml Override %d times, want 1", got)
	}
	rels, err := w.Package().Rels("/visio/pages/pages.xml")
	if err != nil {
		t.Fatalf("Rels(pages.xml): %v", err)
	}
	all := rels.All()
	if len(all) != 2 {
		t.Fatalf("pages.xml.rels holds %d relationships, want 2", len(all))
	}
	rel2, ok := rels.Get("rId2")
	if !ok {
		t.Fatal("no rId2 in pages.xml.rels")
	}
	if rel2.Type != ctypes.RelPage {
		t.Errorf("rId2 type = %q, want %q", rel2.Type, ctypes.RelPage)
	}
	part, err := rels.TargetPart(rel2)
	if err != nil || part != "/visio/pages/page2.xml" {
		t.Errorf("rId2 target = %q, %v, want /visio/pages/page2.xml", part, err)
	}
}

// TestAddPageTwice checks the E2 row: a second AddPage returns page2
// at /visio/pages/page3.xml with relationship rId3.
func TestAddPageTwice(t *testing.T) {
	w := mustBegin(t, blankPath(t))
	entry := func(name string) func(string) ([]byte, error) {
		return func(relID string) ([]byte, error) {
			return emit.PageXML(emit.Page{Name: name, Width: 8.5, Height: 11, RelID: relID})
		}
	}
	first, err := w.AddPage(entry("Wide"), "")
	if err != nil {
		t.Fatalf("first AddPage: %v", err)
	}
	if first != "page1" {
		t.Errorf("first AddPage = %q, want page1", first)
	}
	second, err := w.AddPage(entry("Wide2"), "")
	if err != nil {
		t.Fatalf("second AddPage: %v", err)
	}
	if second != "page2" {
		t.Errorf("second AddPage = %q, want page2", second)
	}
	if err := w.Verify(); err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if !w.Package().Has("/visio/pages/page3.xml") {
		t.Error("no /visio/pages/page3.xml after the second AddPage")
	}
	rels, err := w.Package().Rels("/visio/pages/pages.xml")
	if err != nil {
		t.Fatalf("Rels(pages.xml): %v", err)
	}
	rel3, ok := rels.Get("rId3")
	if !ok {
		t.Fatal("no rId3 in pages.xml.rels")
	}
	part, err := rels.TargetPart(rel3)
	if err != nil || part != "/visio/pages/page3.xml" {
		t.Errorf("rId3 target = %q, %v, want /visio/pages/page3.xml", part, err)
	}
	d, err := w.Core().Tree("/visio/pages/pages.xml")
	if err != nil {
		t.Fatalf("Tree(pages.xml): %v", err)
	}
	var ids []uint32
	for p := range d.Root().ElementsSeq() {
		if !isElem(p, vsd.ElemPage) {
			continue
		}
		if id, ok := shapeID(p); ok {
			ids = append(ids, id)
		}
	}
	if len(ids) != 3 || ids[0] != 0 || ids[1] != 1 || ids[2] != 2 {
		t.Errorf("pages order = %v, want [0 1 2]", ids)
	}
}

// TestRenamePage checks the E2 row: RenamePage(page0, "Overview")
// sets NameU and Name, and leaves every other part unchanged.
func TestRenamePage(t *testing.T) {
	src := blankPath(t)
	w := mustBegin(t, src)
	if err := w.RenamePage("page0", "Overview"); err != nil {
		t.Fatalf("RenamePage: %v", err)
	}
	if err := w.Verify(); err != nil {
		t.Fatalf("Verify: %v", err)
	}
	d, err := w.Core().Tree("/visio/pages/pages.xml")
	if err != nil {
		t.Fatalf("Tree(pages.xml): %v", err)
	}
	p0 := findPageElem(d.Root(), 0, 1)
	if p0 == nil {
		t.Fatal("no page0")
	}
	if n, _ := p0.Attr("Name"); n != "Overview" {
		t.Errorf("page0 Name = %q, want Overview", n)
	}
	if n, _ := p0.Attr("NameU"); n != "Overview" {
		t.Errorf("page0 NameU = %q, want Overview", n)
	}
	snap := src + ".snap.vsdx"
	if err := w.Tx().Snapshot(snap); err != nil {
		t.Fatalf("Snapshot: %v", err)
	}
	if err := laws.SameParts(src, snap, "/visio/pages/pages.xml"); err != nil {
		t.Errorf("parts other than pages.xml changed: %v", err)
	}
}

// TestRenamePageRefused checks the E2 row: RenamePage to "page-1"
// when "Page-1" exists elsewhere is refused and leaves the name.
func TestRenamePageRefused(t *testing.T) {
	w := mustBegin(t, blankPath(t))
	addr, err := w.AddPage(func(relID string) ([]byte, error) {
		return emit.PageXML(emit.Page{Name: "Wide", Width: 8.5, Height: 11, RelID: relID})
	}, "")
	if err != nil {
		t.Fatalf("AddPage: %v", err)
	}
	if addr != "page1" {
		t.Fatalf("AddPage = %q, want page1", addr)
	}
	if err := w.RenamePage("page1", "page-1"); err == nil {
		t.Error("RenamePage(page1, page-1) succeeded, want a refusal (Page-1 exists on page0)")
	}
	if err := w.Verify(); err != nil {
		t.Fatalf("Verify: %v", err)
	}
	d, err := w.Core().Tree("/visio/pages/pages.xml")
	if err != nil {
		t.Fatalf("Tree(pages.xml): %v", err)
	}
	if p1 := findPageElem(d.Root(), 1, 1); p1 == nil {
		t.Fatal("no page1")
	} else if n, _ := p1.Attr("Name"); n != "Wide" {
		t.Errorf("page1 Name after the refused rename = %q, want Wide (unchanged)", n)
	}
}

// TestPagesStepsErrors checks the refusals the rows imply: a nil or
// failing entry, a bad fragment, a wrong relationship, an unknown
// after page, a non-page address and a duplicate name.
func TestPagesStepsErrors(t *testing.T) {
	w := mustBegin(t, blankPath(t))
	if _, err := w.AddPage(func(relID string) ([]byte, error) {
		return emit.PageXML(emit.Page{Name: "Wide", Width: 8.5, Height: 11, RelID: relID})
	}, ""); err != nil {
		t.Fatalf("AddPage: %v", err)
	}
	type step struct {
		name string
		run  func() error
	}
	steps := []step{
		{name: "add with a nil entry", run: func() error { _, err := w.AddPage(nil, ""); return err }},
		{name: "add with a failing entry", run: func() error {
			_, err := w.AddPage(func(string) ([]byte, error) { return nil, errors.New("boom") }, "")
			return err
		}},
		{name: "add with a bad fragment", run: func() error {
			_, err := w.AddPage(func(string) ([]byte, error) { return []byte("<Connect/>"), nil }, "")
			return err
		}},
		{name: "add with the wrong relationship", run: func() error {
			_, err := w.AddPage(func(string) ([]byte, error) {
				return emit.PageXML(emit.Page{Name: "X", Width: 8.5, Height: 11, RelID: "rId99"})
			}, "")
			return err
		}},
		{name: "add after an unknown page", run: func() error {
			_, err := w.AddPage(func(relID string) ([]byte, error) {
				return emit.PageXML(emit.Page{Name: "X", Width: 8.5, Height: 11, RelID: relID})
			}, "page9")
			return err
		}},
		{name: "add after a shape", run: func() error {
			_, err := w.AddPage(func(relID string) ([]byte, error) {
				return emit.PageXML(emit.Page{Name: "X", Width: 8.5, Height: 11, RelID: relID})
			}, "page0/s1")
			return err
		}},
		{name: "rename an unknown page", run: func() error { return w.RenamePage("page9", "X") }},
		{name: "rename a shape", run: func() error { return w.RenamePage("page0/s1", "X") }},
		{name: "rename to an empty name", run: func() error { return w.RenamePage("page0", "") }},
		{name: "rename to a duplicate name", run: func() error { return w.RenamePage("page1", "PAGE-1") }},
	}
	for _, st := range steps {
		if err := st.run(); err == nil {
			t.Errorf("%s: want an error, got nil", st.name)
		}
	}
	if err := w.Verify(); err != nil {
		t.Fatalf("Verify after the refused steps: %v", err)
	}
}

// TestAddPageAfter checks that after places the new entry directly
// after the named page, not at the end.
func TestAddPageAfter(t *testing.T) {
	w := mustBegin(t, blankPath(t))
	entry := func(name string) func(string) ([]byte, error) {
		return func(relID string) ([]byte, error) {
			return emit.PageXML(emit.Page{Name: name, Width: 8.5, Height: 11, RelID: relID})
		}
	}
	if _, err := w.AddPage(entry("Wide"), ""); err != nil {
		t.Fatalf("first AddPage: %v", err)
	}
	addr, err := w.AddPage(entry("Middle"), "page0")
	if err != nil {
		t.Fatalf("AddPage after page0: %v", err)
	}
	if addr != "page2" {
		t.Errorf("AddPage after page0 = %q, want page2", addr)
	}
	if err := w.Verify(); err != nil {
		t.Fatalf("Verify: %v", err)
	}
	d, err := w.Core().Tree("/visio/pages/pages.xml")
	if err != nil {
		t.Fatalf("Tree(pages.xml): %v", err)
	}
	var ids []uint32
	for p := range d.Root().ElementsSeq() {
		if !isElem(p, vsd.ElemPage) {
			continue
		}
		if id, ok := shapeID(p); ok {
			ids = append(ids, id)
		}
	}
	if len(ids) != 3 || ids[0] != 0 || ids[1] != 2 || ids[2] != 1 {
		t.Errorf("pages order = %v, want [0 2 1] (page2 after page0)", ids)
	}
}

// TestAddPageV11 checks that a part in the 2011 spelling gets its new
// entry in that spelling.
func TestAddPageV11(t *testing.T) {
	w := mustBegin(t, vsdxtest.Fixture(t, "v11-ns2011"))
	addr, err := w.AddPage(func(relID string) ([]byte, error) {
		return emit.PageXML(emit.Page{Name: "Wide", Width: 8.5, Height: 11, RelID: relID})
	}, "")
	if err != nil {
		t.Fatalf("AddPage: %v", err)
	}
	if addr != "page1" {
		t.Errorf("AddPage = %q, want page1", addr)
	}
	if err := w.Verify(); err != nil {
		t.Fatalf("Verify: %v", err)
	}
	d, err := w.Core().Tree("/visio/pages/pages.xml")
	if err != nil {
		t.Fatalf("Tree(pages.xml): %v", err)
	}
	page1 := findPageElem(d.Root(), 1, 1)
	if page1 == nil {
		t.Fatal("no page1")
	}
	if page1.Space() != vsd.NSCore2011 {
		t.Errorf("page1 namespace = %q, want the 2011 core spelling", page1.Space())
	}
}

// TestScalingPages checks the scaling row: AddPage and RenamePage on
// a drawing of N and 4N pages (N = 1000, 4N = 4000 pages) allocates
// at most 5.5 times as much (laws.Scales) and the best-of-5 time ratio
// is at most 8. The measured run holds one workspace per size (setup
// builds it), so Begin's own cost is not measured: Begin on many pages
// is doc.Open's, not this chunk's.
func TestScalingPages(t *testing.T) {
	setup := func(n int) (func() error, error) {
		src := blankPath(t)
		w, err := Begin(src)
		if err != nil {
			return nil, err
		}
		for i := 1; i < n; i++ {
			name := "P" + strconv.Itoa(i)
			if _, err := w.AddPage(func(relID string) ([]byte, error) {
				return emit.PageXML(emit.Page{Name: name, Width: 8.5, Height: 11, RelID: relID})
			}, ""); err != nil {
				w.Tx().Rollback()
				return nil, err
			}
		}
		snap := filepath.Join(t.TempDir(), fmt.Sprintf("pages-%d.vsdx", n))
		if err := w.Tx().Snapshot(snap); err != nil {
			w.Tx().Rollback()
			return nil, err
		}
		w.Tx().Rollback()
		w, err = Begin(snap)
		if err != nil {
			return nil, err
		}
		t.Cleanup(func() { w.Tx().Rollback() })
		count := 0
		return func() error {
			count++
			name := "Extra" + strconv.Itoa(count)
			addr, err := w.AddPage(func(relID string) ([]byte, error) {
				return emit.PageXML(emit.Page{Name: name, Width: 8.5, Height: 11, RelID: relID})
			}, "")
			if err != nil {
				return err
			}
			if addr == "" {
				return errors.New("AddPage returned an empty address")
			}
			if err := w.RenamePage(addr, "Renamed"+strconv.Itoa(count)); err != nil {
				return err
			}
			return w.Verify()
		}, nil
	}
	laws.Scales(t, "vsdx AddPage", setup)
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
	t.Logf("vsdx AddPage: %d pages best time %s, %d pages best time %s: time ratio %.2f",
		laws.Small, costs[0].Time, laws.Large, costs[1].Time, tm)
	if tm > 8 {
		t.Errorf("vsdx AddPage time ratio %.2f for 4 times the input (budget 8): super-linear", tm)
	}
}
