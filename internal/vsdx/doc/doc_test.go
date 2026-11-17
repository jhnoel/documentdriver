package doc

import (
	"testing"

	"github.com/jhnoel/documentdriver/internal/opc"
	"github.com/jhnoel/documentdriver/internal/testdeck"
	"github.com/jhnoel/documentdriver/internal/vsdx/vsdxtest"
)

// openFixture opens the vsdx fixture name and returns its Document.
func openFixture(t *testing.T, name string) *Document {
	t.Helper()
	p, err := opc.Open(vsdxtest.Fixture(t, name))
	if err != nil {
		t.Fatalf("opc.Open(%s): %v", name, err)
	}
	t.Cleanup(func() {
		if err := p.Close(); err != nil {
			t.Errorf("close %s: %v", name, err)
		}
	})
	d, err := Open(p)
	if err != nil {
		t.Fatalf("Open(%s): %v", name, err)
	}
	return d
}

// pageFields are the PageEntry fields a row names, without the PageSheet
// node, for comparing one fixture's pages with another's.
type pageFields struct {
	ID         uint32
	Name       string
	NameU      string
	Background bool
	BackPage   uint32
	HasBack    bool
	Part       string
	HasSheet   bool
}

// fieldsOf returns p's comparable fields, with HasSheet for its PageSheet.
func fieldsOf(p PageEntry) pageFields {
	return pageFields{ID: p.ID, Name: p.Name, NameU: p.NameU, Background: p.Background,
		BackPage: p.BackPage, HasBack: p.HasBack, Part: p.Part, HasSheet: p.PageSheet != nil}
}

// wantPages is the Pages row of V1 (also V11's and V12a's).
var wantPages = []pageFields{{ID: 0, Name: "Page-1", NameU: "Page-1", Part: "/visio/pages/page1.xml", HasSheet: true}}

// checkPages fails when d's pages do not equal want in fields and order.
func checkPages(t *testing.T, d *Document, want []pageFields) {
	t.Helper()
	if len(d.Pages) != len(want) {
		t.Fatalf("Pages = %v, want %v", d.Pages, want)
	}
	for i, p := range d.Pages {
		if got := fieldsOf(p); got != want[i] {
			t.Errorf("Pages[%d] = %+v, want %+v", i, got, want[i])
		}
	}
}

// TestOpenV1 checks the V1 row: Kind Drawing, Part
// "/visio/document.xml", one page and no masters or notes.
func TestOpenV1(t *testing.T) {
	d := openFixture(t, "v1-minimal")
	if d.Kind != Drawing {
		t.Errorf("Kind = %v, want Drawing", d.Kind)
	}
	if d.Macro {
		t.Error("Macro = true, want false")
	}
	if d.Part != "/visio/document.xml" {
		t.Errorf("Part = %q, want /visio/document.xml", d.Part)
	}
	checkPages(t, d, wantPages)
	if len(d.Masters) != 0 {
		t.Errorf("Masters = %v, want none", d.Masters)
	}
	if len(d.Notes) != 0 {
		t.Errorf("Notes = %v, want none", d.Notes)
	}
}

// TestOpenV2 checks the V2 row: masters 2 "Process" and 3 "Pair" at
// their master parts, found by Master.
func TestOpenV2(t *testing.T) {
	d := openFixture(t, "v2-master")
	want := []MasterEntry{
		{ID: 2, Name: "Process", NameU: "Process", UniqueID: "{00000000-0000-0000-0000-000000000002}", BaseID: "{00000000-0000-0000-0000-0000000000B2}", Part: "/visio/masters/master1.xml"},
		{ID: 3, Name: "Pair", NameU: "Pair", UniqueID: "{00000000-0000-0000-0000-000000000003}", BaseID: "{00000000-0000-0000-0000-0000000000B3}", Part: "/visio/masters/master2.xml"},
	}
	if len(d.Masters) != len(want) {
		t.Fatalf("Masters = %v, want %v", d.Masters, want)
	}
	for i, m := range d.Masters {
		if m != want[i] {
			t.Errorf("Masters[%d] = %+v, want %+v", i, m, want[i])
		}
	}
	if m, ok := d.Master(3); !ok || m.Name != "Pair" {
		t.Errorf("Master(3) = %v, %v, want Pair", m, ok)
	}
	if _, ok := d.Master(99); ok {
		t.Error("Master(99) found, want none")
	}
}

// TestOpenV8Pages checks the V8 row: page IDs 0, 1, 3 in pages.xml
// order, page 0's BackPage 3 and page 3's Background flag.
func TestOpenV8Pages(t *testing.T) {
	d := openFixture(t, "v8-pages")
	var ids []uint32
	for _, p := range d.Pages {
		ids = append(ids, p.ID)
	}
	want := []uint32{0, 1, 3}
	if len(ids) != len(want) {
		t.Fatalf("page IDs = %v, want %v", ids, want)
	}
	for i, id := range ids {
		if id != want[i] {
			t.Errorf("page IDs = %v, want %v", ids, want)
		}
	}
	p0, ok := d.Page(0)
	if !ok || p0.BackPage != 3 || !p0.HasBack {
		t.Errorf("Page(0) = %+v, %v, want BackPage 3 HasBack", p0, ok)
	}
	p3, ok := d.Page(3)
	if !ok || !p3.Background {
		t.Errorf("Page(3) = %+v, %v, want Background", p3, ok)
	}
}

// TestOpenV10Defects checks the V10 rows: page 4 has Part "" with a
// note at "page4", and the two pages with ID 5 are both kept, with
// Page(5) returning the first.
func TestOpenV10Defects(t *testing.T) {
	d := openFixture(t, "v10-defects")
	if len(d.Pages) != 5 {
		t.Fatalf("Pages = %v, want 5 entries", d.Pages)
	}
	if p := d.Pages[2]; p.ID != 4 || p.Part != "" {
		t.Errorf("Pages[2] = %+v, want ID 4 with no part", p)
	}
	found := false
	for _, n := range d.Notes {
		if n.At == "page4" {
			found = true
		}
	}
	if !found {
		t.Errorf("Notes = %v, want a note at page4", d.Notes)
	}
	if p, ok := d.Page(5); !ok || p.Name != "Twin" || p.Part != "/visio/pages/page4.xml" {
		t.Errorf("Page(5) = %+v, %v, want the first Twin", p, ok)
	}
	if p := d.Pages[4]; p.ID != 5 || p.Name != "Twin-2" || p.Part != "/visio/pages/page5.xml" {
		t.Errorf("Pages[4] = %+v, want the second page with ID 5 (page5~2)", p)
	}
	if _, ok := d.Page(99); ok {
		t.Error("Page(99) found, want none")
	}
}

// TestOpenV11 checks the V11 row: the 2011/1/core namespace reads the
// same Pages as V1.
func TestOpenV11(t *testing.T) {
	d := openFixture(t, "v11-ns2011")
	if d.Kind != Drawing || d.Part != "/visio/document.xml" {
		t.Errorf("Kind, Part = %v, %q, want Drawing, /visio/document.xml", d.Kind, d.Part)
	}
	checkPages(t, d, wantPages)
	if len(d.Notes) != 0 {
		t.Errorf("Notes = %v, want none", d.Notes)
	}
}

// TestOpenV12 checks the V12 rows: the .vsdm is a Drawing with Macro
// true and V1's pages, and the stencil is Kind Stencil with no pages.
func TestOpenV12(t *testing.T) {
	d := openFixture(t, "v12-vsdm")
	if d.Kind != Drawing || !d.Macro {
		t.Errorf("vsdm Kind, Macro = %v, %v, want Drawing, true", d.Kind, d.Macro)
	}
	checkPages(t, d, wantPages)

	s := openFixture(t, "v12-stencil")
	if s.Kind != Stencil {
		t.Errorf("stencil Kind = %v, want Stencil", s.Kind)
	}
	if s.Macro {
		t.Error("stencil Macro = true, want false")
	}
	if len(s.Pages) != 0 {
		t.Errorf("stencil Pages = %v, want none", s.Pages)
	}
	if len(s.Masters) != 1 || s.Masters[0].ID != 2 || s.Masters[0].Name != "Box" ||
		s.Masters[0].Part != "/visio/masters/master1.xml" {
		t.Errorf("stencil Masters = %v, want master 2 Box at /visio/masters/master1.xml", s.Masters)
	}
}

// TestOpenPPTX checks that a pptx (testdeck.Minimal) is refused: it has
// no Visio document relationship.
func TestOpenPPTX(t *testing.T) {
	p, err := opc.Open(testdeck.Minimal(t))
	if err != nil {
		t.Fatalf("opc.Open(testdeck.Minimal): %v", err)
	}
	defer func() {
		if err := p.Close(); err != nil {
			t.Errorf("close: %v", err)
		}
	}()
	if _, err := Open(p); err == nil {
		t.Error("Open(testdeck.Minimal) succeeded, want an error")
	}
}
