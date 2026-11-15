package main

import (
	"testing"

	"github.com/jhnoel/documentdriver/internal/pptx/render"
	"github.com/jhnoel/documentdriver/internal/testdeck"
)

func TestMarksFromModel(t *testing.T) {
	deck := testdeck.Diagram(t)
	marks, err := slideMarks(deck, "/ppt/slides/slide1.xml", nil)
	if err != nil {
		t.Fatal(err)
	}
	by := map[string]render.Mark{}
	for _, m := range marks {
		by[m.ID] = m
	}
	// Group children are marked: s22 is the ellipse inside the flipped group.
	for _, id := range []string{"s10", "s11", "s12", "s21", "s22", "s23", "s26"} {
		if _, ok := by[id]; !ok {
			t.Errorf("no mark for %s", id)
		}
	}
	if api := by["s10"]; len(api.Sites) != 4 || len(api.Path) != 0 {
		t.Errorf("API mark = %+v", api)
	}
	// Ellipse sites are the eight preset sites, diagonals included.
	if got := len(by["s11"].Sites); got != 8 {
		t.Errorf("ellipse sites = %d, want 8", got)
	}
	// Straight connector s12 is glued at both ends: the ends are the
	// derived from/to points, also the ends of its path.
	c := by["s12"]
	if len(c.Ends) != 2 || len(c.Path) < 2 || c.Ends[0] != c.Path[0] || c.Ends[1] != c.Path[len(c.Path)-1] {
		t.Errorf("connector s12 = %+v", c)
	}
	// Elbow s13 is glued at the start only; s61 only at the end.
	if e := by["s13"].Ends; len(e) != 1 || e[0] != by["s13"].Path[0] {
		t.Errorf("s13 ends = %v", e)
	}
	if e := by["s61"].Ends; len(e) != 1 {
		t.Errorf("s61 ends = %v", e)
	}
	// The connector inside the group ends on its neighbours' sites.
	in := by["s23"]
	if len(in.Ends) != 2 || in.Ends[0] != by["s21"].Sites[3] || in.Ends[1] != by["s22"].Sites[2] {
		t.Errorf("s23 ends %v, sites %v / %v", in.Ends, by["s21"].Sites[3], by["s22"].Sites[2])
	}
	// The rotated group's child carries the slide-space rotation.
	if by["s26"].Rot == 0 {
		t.Errorf("s26 rotation lost: %+v", by["s26"])
	}
	// ids keeps the object and its descendants only.
	sub, err := slideMarks(deck, "/ppt/slides/slide1.xml", []string{"s20"})
	if err != nil {
		t.Fatal(err)
	}
	if len(sub) != 5 {
		t.Errorf("ids s20: %d marks, want the group and its four children", len(sub))
	}
}
