package model

import (
	"slices"
	"strings"
	"testing"

	"github.com/jhnoel/documentdriver/internal/vsdx/doc"
	"github.com/jhnoel/documentdriver/internal/vsdx/inherit"
)

// TestPageSheetV1 checks the V1 row: page0 W 8.5, H 11, Orientation
// portrait, PrintOrientation portrait (StyleSheet 0).
func TestPageSheetV1(t *testing.T) {
	d, r := openFixture(t, "v1-minimal")
	e, ok := d.Page(0)
	if !ok {
		t.Fatalf("Page(0) not found, Pages = %v", d.Pages)
	}
	p := PageSheet(d, r, e)
	if p.ID != "page0" {
		t.Errorf("ID = %q, want page0", p.ID)
	}
	if p.Name != "Page-1" || p.NameU != "Page-1" {
		t.Errorf("Name, NameU = %q, %q, want Page-1, Page-1", p.Name, p.NameU)
	}
	if p.Background {
		t.Error("Background = true, want false")
	}
	if p.BackPage != "" {
		t.Errorf("BackPage = %q, want empty", p.BackPage)
	}
	if !p.HasSize || p.W != 8.5 || p.H != 11 {
		t.Errorf("HasSize, W, H = %v, %v, %v, want true, 8.5, 11", p.HasSize, p.W, p.H)
	}
	if p.Orientation != "portrait" {
		t.Errorf("Orientation = %q, want portrait", p.Orientation)
	}
	if p.PrintOrientation != "portrait" {
		t.Errorf("PrintOrientation = %q, want portrait (StyleSheet 0)", p.PrintOrientation)
	}
	if len(p.Layers) != 0 {
		t.Errorf("Layers = %v, want none", p.Layers)
	}
	if len(p.Notes) != 0 {
		t.Errorf("Notes = %v, want none", p.Notes)
	}
}

// TestPageSheetV17 checks the V17 row: page0 portrait/portrait,
// page1 landscape/landscape, page2 landscape/portrait, page3
// Orientation "" / portrait.
func TestPageSheetV17(t *testing.T) {
	d, r := openFixture(t, "v17-orientation")
	type row struct {
		id          uint32
		w, h        float64
		orientation string
		print       string
	}
	want := []row{
		{id: 0, w: 8.5, h: 11, orientation: "portrait", print: "portrait"},
		{id: 1, w: 11, h: 8.5, orientation: "landscape", print: "landscape"},
		{id: 2, w: 11, h: 8.5, orientation: "landscape", print: "portrait"},
		{id: 3, w: 8, h: 8, orientation: "", print: "portrait"},
	}
	for _, tc := range want {
		e, ok := d.Page(tc.id)
		if !ok {
			t.Fatalf("Page(%d) not found, Pages = %v", tc.id, d.Pages)
		}
		p := PageSheet(d, r, e)
		if !p.HasSize || p.W != tc.w || p.H != tc.h {
			t.Errorf("page%d HasSize, W, H = %v, %v, %v, want true, %v, %v", tc.id, p.HasSize, p.W, p.H, tc.w, tc.h)
		}
		if p.Orientation != tc.orientation {
			t.Errorf("page%d Orientation = %q, want %q", tc.id, p.Orientation, tc.orientation)
		}
		if p.PrintOrientation != tc.print {
			t.Errorf("page%d PrintOrientation = %q, want %q", tc.id, p.PrintOrientation, tc.print)
		}
	}
}

// TestPageSheetV8Layers checks the V8 row: Layers [Flowchart Index 0
// Visible Print, Connector Index 1 not Visible Print].
func TestPageSheetV8Layers(t *testing.T) {
	d, r := openFixture(t, "v8-pages")
	e, ok := d.Page(0)
	if !ok {
		t.Fatalf("Page(0) not found, Pages = %v", d.Pages)
	}
	p := PageSheet(d, r, e)
	if p.BackPage != "page3" {
		t.Errorf("BackPage = %q, want page3", p.BackPage)
	}
	if p.Background {
		t.Error("Background = true, want false")
	}
	want := []Layer{
		{ID: "page0/layer:Flowchart", Name: "Flowchart", Index: 0, Visible: true, Print: true, Lock: false},
		{ID: "page0/layer:Connector", Name: "Connector", Index: 1, Visible: false, Print: true, Lock: false},
	}
	if !slices.Equal(p.Layers, want) {
		t.Errorf("Layers = %+v, want %+v", p.Layers, want)
	}
	bg, ok := d.Page(3)
	if !ok {
		t.Fatalf("Page(3) not found, Pages = %v", d.Pages)
	}
	if bp := PageSheet(d, r, bg); !bp.Background {
		t.Error("page3 Background = false, want true")
	}
}

// TestLayerMembers checks the layerMembers rows: "0;1" gives both,
// "" gives none, "0;7" gives Flowchart plus a note.
func TestLayerMembers(t *testing.T) {
	d, r := openFixture(t, "v8-pages")
	e, ok := d.Page(0)
	if !ok {
		t.Fatalf("Page(0) not found, Pages = %v", d.Pages)
	}
	type row struct {
		value string
		want  []string
		notes int
	}
	cases := []row{
		{value: "0;1", want: []string{"page0/layer:Flowchart", "page0/layer:Connector"}, notes: 0},
		{value: "", want: nil, notes: 0},
		{value: "0;7", want: []string{"page0/layer:Flowchart"}, notes: 1},
	}
	for _, tc := range cases {
		p := PageSheet(d, r, e)
		before := len(p.Notes)
		got := layerMembers(p, tc.value)
		if !slices.Equal(got, tc.want) {
			t.Errorf("layerMembers(%q) = %v, want %v", tc.value, got, tc.want)
		}
		if added := len(p.Notes) - before; added != tc.notes {
			t.Errorf("layerMembers(%q) added %d notes, want %d (notes %v)", tc.value, added, tc.notes, p.Notes)
		}
		for _, n := range p.Notes[before:] {
			if n.At != "page0" {
				t.Errorf("layerMembers(%q) note At = %q, want page0", tc.value, n.At)
			}
			if !strings.Contains(n.Reason, "7") {
				t.Errorf("layerMembers(%q) note Reason = %q, want it to name 7", tc.value, n.Reason)
			}
		}
	}
}

// TestPageSheetV13ScaleNote checks the V13 row: a drawing-scale note
// on the scaled page.
func TestPageSheetV13ScaleNote(t *testing.T) {
	d, r := openFixture(t, "v13-scale")
	e, ok := d.Page(0)
	if !ok {
		t.Fatalf("Page(0) not found, Pages = %v", d.Pages)
	}
	p := PageSheet(d, r, e)
	found := false
	for _, n := range p.Notes {
		if n.At == "page0" && n.Reason == "drawing scale 1 in = 10 ft: lengths are stored drawing units" {
			found = true
		}
	}
	if !found {
		t.Errorf("Notes = %v, want the drawing-scale note at page0", p.Notes)
	}
	if !p.HasSize || p.W != 8.5 || p.H != 11 || p.Orientation != "portrait" {
		t.Errorf("HasSize, W, H, Orientation = %v, %v, %v, %q, want true, 8.5, 11, portrait", p.HasSize, p.W, p.H, p.Orientation)
	}
}

// TestPageSheetObjectsNil checks that PageSheet(V1) has Objects nil:
// it holds only page-sheet facts, without shapes. Chunk M6.
func TestPageSheetObjectsNil(t *testing.T) {
	d, r := openFixture(t, "v1-minimal")
	e, ok := d.Page(0)
	if !ok {
		t.Fatalf("Page(0) not found, Pages = %v", d.Pages)
	}
	p := PageSheet(d, r, e)
	if p.Objects != nil {
		t.Errorf("Objects = %v, want nil", p.Objects)
	}
	if p.Connections != nil {
		t.Errorf("Connections = %v, want nil", p.Connections)
	}
}

// TestBuildPageV8ObjectLayers checks the V8 object layers: page0/s1
// LayerMember "0;1" gives both layer addresses.
func TestBuildPageV8ObjectLayers(t *testing.T) {
	d, r := openFixture(t, "v8-pages")
	p := buildPageOf(t, d, r, 0)
	o := byID(t, p, "page0/s1")
	want := []string{"page0/layer:Flowchart", "page0/layer:Connector"}
	if !slices.Equal(o.Layers, want) {
		t.Errorf("page0/s1 Layers = %v, want %v", o.Layers, want)
	}
}

// pageSheetOf returns the PageSheet of page id of d through r, failing
// the test when there is none. It is the Pagesheet twin of
// buildPageOf for tests that need only page-sheet facts.
func pageSheetOf(t *testing.T, d *doc.Document, r *inherit.Resolver, id uint32) *Page {
	t.Helper()
	e, ok := d.Page(id)
	if !ok {
		t.Fatalf("Page(%d) not found, Pages = %v", id, d.Pages)
	}
	return PageSheet(d, r, e)
}

// TestPageSheetOf checks the helper above against V1.
func TestPageSheetOf(t *testing.T) {
	d, r := openFixture(t, "v1-minimal")
	if p := pageSheetOf(t, d, r, 0); p.ID != "page0" || p.W != 8.5 {
		t.Errorf("pageSheetOf(0) = %+v, want page0 with W 8.5", p)
	}
}
