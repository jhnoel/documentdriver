package model

import (
	"slices"
	"testing"

	"github.com/jhnoel/documentdriver/internal/ooxml/tree"
	"github.com/jhnoel/documentdriver/internal/vsdx/place"
	"github.com/jhnoel/documentdriver/internal/vsdx/vsd"
)

// TestGluedV5 checks the V5 row: s6's Glued is
// [{Cell "Controls.Row_1", To "s2"}] and there is no Connections entry
// for s6.
func TestGluedV5(t *testing.T) {
	d, r := openFixture(t, "v5-glue")
	p := buildPageOf(t, d, r, 0)
	want := []Glued{{Cell: "Controls.Row_1", To: "s2"}}
	if got := byID(t, p, "page0/s6").Glued; !slices.Equal(got, want) {
		t.Errorf("s6 Glued = %+v, want %+v", got, want)
	}
	for _, conn := range p.Connections {
		if conn.Connector == "page0/s6" {
			t.Errorf("Connections has an entry for s6: %+v, want none", conn)
		}
	}
}

// TestGluedDangling checks row 2: V5 page0/s6 glued to shape 99
// gives Glued To "#99" and the dangling-glue hazard
// "Controls.Row_1 names shape 99, which is not on the page", with no
// Connections entry.
func TestGluedDangling(t *testing.T) {
	d, r := openFixture(t, "v5-glue")
	built := buildPageOf(t, d, r, 0)
	from := byID(t, built, "page0/s6")
	from.Glued = nil
	p := &Page{ID: "page0", Objects: []*Object{from}}
	doc, err := tree.ParseString("<Connects><Connect FromSheet='6' FromCell='Controls.Row_1' ToSheet='99' ToCell='PinX'/></Connects>")
	if err != nil {
		t.Fatalf("ParseString: %v", err)
	}
	byIDMap := map[uint32]*placed{6: {obj: from, s: shapeFor(from, 0, false), frame: place.Page}}
	linkConnects(p, r, doc.Root(), byIDMap, built.H)
	want := []Glued{{Cell: "Controls.Row_1", To: "#99"}}
	if !slices.Equal(from.Glued, want) {
		t.Errorf("Glued = %+v, want %+v", from.Glued, want)
	}
	wantH := []Hazard{{Kind: "dangling-glue", ID: "page0/s6", Detail: "Controls.Row_1 names shape 99, which is not on the page"}}
	if !slices.Equal(p.Hazards, wantH) {
		t.Errorf("Hazards = %+v, want %+v", p.Hazards, wantH)
	}
	if len(p.Connections) != 0 {
		t.Errorf("Connections = %+v, want none", p.Connections)
	}
}

// TestGluedEndUntouched checks the end row: an end Connect (BeginX,
// and EndX) reports false and leaves the page untouched.
func TestGluedEndUntouched(t *testing.T) {
	from := &Object{ID: "page0/s3", ShapeID: 3}
	to := &Object{ID: "page0/s1", ShapeID: 1}
	byID := map[uint32]*placed{
		1: {obj: to, frame: place.Page},
		3: {obj: from, frame: place.Page},
	}
	for _, cell := range []string{vsd.FromCellBegin, vsd.FromCellEnd} {
		d, err := tree.ParseString("<Connects><Connect FromSheet='3' FromCell='" + cell + "' ToSheet='1' ToCell='PinX'/></Connects>")
		if err != nil {
			t.Fatalf("ParseString: %v", err)
		}
		p := &Page{ID: "page0", Objects: []*Object{to, from}}
		for child := range d.Root().ElementsSeq() {
			if gluedOf(p, child, byID) {
				t.Errorf("gluedOf(%s) = true, want false for an end Connect", cell)
			}
		}
		if len(from.Glued) != 0 || len(p.Hazards) != 0 || len(p.Notes) != 0 || len(p.Connections) != 0 {
			t.Errorf("after gluedOf(%s): Glued = %+v, Hazards = %+v, Notes = %+v, Connections = %+v, want all untouched",
				cell, from.Glued, p.Hazards, p.Notes, p.Connections)
		}
	}
}
