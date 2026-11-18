package model

import (
	"math"
	"testing"

	"github.com/jhnoel/documentdriver/internal/geom"
	"github.com/jhnoel/documentdriver/internal/ooxml/tree"
	"github.com/jhnoel/documentdriver/internal/vsdx/inherit"
	"github.com/jhnoel/documentdriver/internal/vsdx/place"
	"github.com/jhnoel/documentdriver/internal/vsdx/sheet"
	"github.com/jhnoel/documentdriver/internal/vsdx/vsd"
)

// buildH builds fixture's page 0 with the page height the rows assume
// (11 inches) rather than the page sheet's (chunk M6): build's walk and
// geometry are production code, only p.H is preset. It fails the test
// on any error.
func buildH(t *testing.T, fixture string, opt Options) (*Page, *inherit.Resolver) {
	t.Helper()
	d, r := openFixture(t, fixture)
	e, ok := d.Page(0)
	if !ok {
		t.Fatalf("Page(0) not found in %s", fixture)
	}
	contents, err := d.PageContents(e)
	if err != nil {
		t.Fatalf("PageContents(%s): %v", fixture, err)
	}
	p := &Page{H: 11}
	got, err := build(p, d, r, contents, vsd.PageID(e.ID, pageDup(d, e)), e.Part, opt)
	if err != nil {
		t.Fatalf("build(%s): %v", fixture, err)
	}
	return got, r
}

// checkBox fails the test when o has no box or its box is not want
// (points, within 1e-6) with rotation rot and flips flipH and flipV.
func checkBox(t *testing.T, o *Object, want [4]float64, rot float64, flipH, flipV bool) {
	t.Helper()
	if o.Box == nil {
		t.Fatalf("%s: no box, omitted %v", o.ID, o.Omitted)
	}
	got := [4]float64{o.Box.X, o.Box.Y, o.Box.W, o.Box.H}
	for i := range got {
		if math.Abs(got[i]-want[i]) > 1e-6 {
			t.Errorf("%s: box = %v, want %v", o.ID, got, want)
			break
		}
	}
	if math.Abs(o.Box.Rot-rot) > 1e-6 {
		t.Errorf("%s: rot = %v, want %v", o.ID, o.Box.Rot, rot)
	}
	if o.Box.FlipH != flipH || o.Box.FlipV != flipV {
		t.Errorf("%s: flips = (%v, %v), want (%v, %v)", o.ID, o.Box.FlipH, o.Box.FlipV, flipH, flipV)
	}
}

// checkPt fails the test when got is not want (points, within 1e-6).
func checkPt(t *testing.T, what string, got geom.Pt, want geom.Pt) {
	t.Helper()
	if math.Abs(got.X-want.X) > 1e-6 || math.Abs(got.Y-want.Y) > 1e-6 {
		t.Errorf("%s = (%v, %v), want (%v, %v)", what, got.X, got.Y, want.X, want.Y)
	}
}

// omittedReason returns the reason recorded for prop on o, "" when none.
func omittedReason(o *Object, prop string) string {
	for _, x := range o.Omitted {
		if x.Prop == prop {
			return x.Reason
		}
	}
	return ""
}

// shapeFor builds the inherit.Shape of o as the walk does, carrying
// the master the enclosing group instance carries when o stores no
// Master of its own.
func shapeFor(o *Object, master uint32, hasMaster bool) *inherit.Shape {
	return readShapeHead(o.Node, master, hasMaster).s
}

// geomFresh derives o's geometry through geometry in frame f with the
// fresh object it returns, for origin checks build's Full branch
// (chunk M7) would replace.
func geomFresh(o *Object, r *inherit.Resolver, s *inherit.Shape, f place.Frame, opt Options) *Object {
	fresh := &Object{ID: o.ID, Kind: o.Kind}
	geometry(fresh, r, s, f, 11, opt)
	return fresh
}

// TestGeometryV1 checks the V1 row: s1 Box [108,126,72,36], box origin
// local.
func TestGeometryV1(t *testing.T) {
	p, r := buildH(t, "v1-minimal", Options{})
	o := byID(t, p, "page0/s1")
	checkBox(t, o, [4]float64{108, 126, 72, 36}, 0, false, false)
	fresh := geomFresh(o, r, shapeFor(o, 0, false), place.Page, Options{Full: true})
	checkBox(t, fresh, [4]float64{108, 126, 72, 36}, 0, false, false)
	if got := fresh.Origins["box"]; got != inherit.Local {
		t.Errorf("box origin = %q, want local", got)
	}
}

// TestGeometryV2 checks the V2 row: s1 [108,117,72,54] origin master;
// s2 [252,117,72,54] (its Width origin inherited); s3
// [360,324,144,72]; s4 [360,324,72,72]; s5 [432,324,72,72].
func TestGeometryV2(t *testing.T) {
	p, r := buildH(t, "v2-master", Options{})
	for id, want := range map[string][4]float64{
		"page0/s1": {108, 117, 72, 54}, "page0/s2": {252, 117, 72, 54},
		"page0/s3": {360, 324, 144, 72}, "page0/s4": {360, 324, 72, 72},
		"page0/s5": {432, 324, 72, 72},
	} {
		checkBox(t, byID(t, p, id), want, 0, false, false)
	}
	// Origins through geometry: s1's box is master (all but PinX and
	// PinY come from master2); s2's Width is inherited (F="Inh"), so
	// its box takes the most inherited of its cells, master.
	s1 := byID(t, p, "page0/s1")
	if got := geomFresh(s1, r, shapeFor(s1, 0, false), place.Page, Options{Full: true}).Origins["box"]; got != inherit.Master {
		t.Errorf("s1 box origin = %q, want master", got)
	}
	s2 := byID(t, p, "page0/s2")
	s2Shape := shapeFor(s2, 0, false)
	if _, origin, ok := r.Cell(s2Shape, sheet.ShapeCell(vsd.CellWidth)); !ok || origin != inherit.Inherited {
		t.Errorf("s2 Width origin = %q, %v, want inherited", origin, ok)
	}
	if got := geomFresh(s2, r, s2Shape, place.Page, Options{Full: true}).Origins["box"]; got != inherit.Master {
		t.Errorf("s2 box origin = %q, want master (most inherited of its cells)", got)
	}
	// s4 and s5 sit in s3, the master3 group instance: their frame is
	// s3's child frame and their master is the group's (3).
	s3 := byID(t, p, "page0/s3")
	t3, _, ok := transformOf(r, shapeFor(s3, 0, false))
	if !ok {
		t.Fatalf("s3 transform not derived")
	}
	frame := place.Page.Child(t3)
	for id, want := range map[string][4]float64{
		"page0/s4": {360, 324, 72, 72}, "page0/s5": {432, 324, 72, 72},
	} {
		o := byID(t, p, id)
		fresh := geomFresh(o, r, shapeFor(o, 3, true), frame, Options{Full: true})
		checkBox(t, fresh, want, 0, false, false)
		if got := fresh.Origins["box"]; got != inherit.Master {
			t.Errorf("%s box origin = %q, want master", id, got)
		}
	}
}

// TestGeometryV5Ends checks the V5 rows: s3 From (180,144) To
// (324,144) with no Box unless Full; s5 From (216,216) To (216,288).
func TestGeometryV5Ends(t *testing.T) {
	p, r := buildH(t, "v5-glue", Options{})
	s3 := byID(t, p, "page0/s3")
	if s3.From == nil || s3.To == nil {
		t.Fatalf("s3: from or to missing, omitted %v", s3.Omitted)
	}
	checkPt(t, "s3 From", *s3.From, geom.Pt{X: 180, Y: 144})
	checkPt(t, "s3 To", *s3.To, geom.Pt{X: 324, Y: 144})
	if s3.Box != nil {
		t.Errorf("s3: box = %v in the default view, want no Box unless Full", s3.Box)
	}
	s5 := byID(t, p, "page0/s5")
	if s5.From == nil || s5.To == nil {
		t.Fatalf("s5: from or to missing, omitted %v", s5.Omitted)
	}
	checkPt(t, "s5 From", *s5.From, geom.Pt{X: 216, Y: 216})
	checkPt(t, "s5 To", *s5.To, geom.Pt{X: 216, Y: 288})
	// With Full the connector's box appears (its stored transform) and
	// the ends carry their origins.
	fresh := geomFresh(s3, r, shapeFor(s3, 0, false), place.Page, Options{Full: true})
	checkBox(t, fresh, [4]float64{180, 144, 144, 0}, 0, false, false)
	if fresh.Origins["from"] != inherit.Local || fresh.Origins["to"] != inherit.Local {
		t.Errorf("s3 ends origins = %v, want local", fresh.Origins)
	}
}

// TestGeometryV5Sites checks the V5 row: s1 Sites (with Sites)
// [{1, (180,144)}, {2, (108,144)}].
func TestGeometryV5Sites(t *testing.T) {
	p, _ := buildH(t, "v5-glue", Options{})
	if o := byID(t, p, "page0/s1"); len(o.Sites) != 0 {
		t.Errorf("s1: default sites = %v, want none", o.Sites)
	}
	p, _ = buildH(t, "v5-glue", Options{Sites: true})
	sites := byID(t, p, "page0/s1").Sites
	want := []Site{{N: 1, Pt: geom.Pt{X: 180, Y: 144}}, {N: 2, Pt: geom.Pt{X: 108, Y: 144}}}
	if len(sites) != len(want) {
		t.Fatalf("s1 sites = %v, want %v", sites, want)
	}
	for i := range want {
		if sites[i].N != want[i].N {
			t.Errorf("sites[%d].N = %d, want %d", i, sites[i].N, want[i].N)
		}
		checkPt(t, "s1 site", sites[i].Pt, want[i].Pt)
	}
	// Full also derives the sites.
	p, _ = buildH(t, "v5-glue", Options{Full: true})
	if got := byID(t, p, "page0/s1").Sites; len(got) != 2 {
		t.Errorf("s1 sites with Full = %v, want 2", got)
	}
}

// TestGeometryV4 checks the V4 row: s11 [216,432,72,72], s13
// [306,378,36,36] rot 330.
func TestGeometryV4(t *testing.T) {
	p, _ := buildH(t, "v4-groups", Options{})
	checkBox(t, byID(t, p, "page0/s11"), [4]float64{216, 432, 72, 72}, 0, false, false)
	checkBox(t, byID(t, p, "page0/s13"), [4]float64{306, 378, 36, 36}, 330, false, false)
}

// TestGeometryV9 checks the V9 row: s1 [90,90,72,36] rot 270, s2
// flip_h at [252,126,72,36], s3 rot 30 at [396,126,72,36].
func TestGeometryV9(t *testing.T) {
	p, _ := buildH(t, "v9-rotation", Options{})
	checkBox(t, byID(t, p, "page0/s1"), [4]float64{90, 90, 72, 36}, 270, false, false)
	checkBox(t, byID(t, p, "page0/s2"), [4]float64{252, 126, 72, 36}, 0, true, false)
	checkBox(t, byID(t, p, "page0/s3"), [4]float64{396, 126, 72, 36}, 30, false, false)
}

// TestGeometryNoPinX checks the row: a shape without PinX has its box
// omitted with the reason "PinX not stored".
func TestGeometryNoPinX(t *testing.T) {
	d, r := openDeep(t, 1)
	e, ok := d.Page(0)
	if !ok {
		t.Fatalf("Page(0) not found")
	}
	contents, err := d.PageContents(e)
	if err != nil {
		t.Fatalf("PageContents: %v", err)
	}
	p := &Page{H: 11}
	got, err := build(p, d, r, contents, "page0", "", Options{})
	if err != nil {
		t.Fatalf("build deep: %v", err)
	}
	if len(got.Objects) == 0 {
		t.Fatal("deep: no objects")
	}
	o := got.Objects[0]
	// Through geometry directly: fillObject assigns readProps'
	// omissions over geometry's (chunk M3 owns that merge).
	fresh := geomFresh(o, r, shapeFor(o, 0, false), place.Page, Options{})
	if fresh.Box != nil {
		t.Errorf("deep s1: box = %v, want omitted without PinX", fresh.Box)
	}
	if reason := omittedReason(fresh, "box"); reason != "PinX not stored" {
		t.Errorf("deep s1: box omission = %q, want %q", reason, "PinX not stored")
	}
	// transformOf names PinX for a shape with no cells at all.
	if _, missing, ok := transformOf(r, &inherit.Shape{Sheet: &sheet.Sheet{}}); ok || missing != "PinX" {
		t.Errorf("transformOf empty shape: missing = %q, ok = %v, want PinX, false", missing, ok)
	}
}

// TestTransformOf checks transformOf on V1 s1 and on V10 s8, which
// stores only PinX and PinY: the first cell it cannot derive is Width.
func TestTransformOf(t *testing.T) {
	p, r := buildH(t, "v1-minimal", Options{})
	o := byID(t, p, "page0/s1")
	got, missing, ok := transformOf(r, shapeFor(o, 0, false))
	want := place.Transform{PinX: 2, PinY: 9, Width: 1, Height: 0.5, LocPinX: 0.5, LocPinY: 0.25}
	if !ok || missing != "" || got != want {
		t.Errorf("transformOf V1 s1 = %+v, %q, %v, want %+v, \"\", true", got, missing, ok, want)
	}
	p10, r10 := buildH(t, "v10-defects", Options{})
	s8 := byID(t, p10, "page0/s8")
	if _, missing, ok := transformOf(r10, shapeFor(s8, 0, false)); ok || missing != "Width" {
		t.Errorf("transformOf V10 s8: missing = %q, ok = %v, want Width, false", missing, ok)
	}
}

// TestGeometryNotANumber checks that a transform cell stored as a
// non-number omits the box naming the cell.
func TestGeometryNotANumber(t *testing.T) {
	doc, err := tree.ParseString(`<Shape ID='1'><Cell N='PinX' V='Themed'/></Shape>`)
	if err != nil {
		t.Fatalf("ParseString: %v", err)
	}
	sh, _ := sheet.Index(doc.Root())
	s := &inherit.Shape{Sheet: sh}
	if _, missing, ok := transformOf(nil, s); ok || missing != "PinX" {
		t.Errorf("transformOf Themed PinX: missing = %q, ok = %v, want PinX, false", missing, ok)
	}
	o := &Object{ID: "page0/s1", Kind: Shape}
	geometry(o, nil, s, place.Page, 11, Options{})
	if o.Box != nil {
		t.Errorf("box = %v, want omitted for a non-number PinX", o.Box)
	}
	if reason := omittedReason(o, "box"); reason != "PinX not a number" {
		t.Errorf("box omission = %q, want %q", reason, "PinX not a number")
	}
}

// TestGeometrySkew checks that a rotated shape under a sheared frame
// has its box omitted, while its ends do not depend on the box.
func TestGeometrySkew(t *testing.T) {
	p, r := buildH(t, "v9-rotation", Options{})
	o := byID(t, p, "page0/s1")
	sheared := place.Frame{M: geom.Affine{A: 1, C: 0.3, D: 1}}
	fresh := geomFresh(o, r, shapeFor(o, 0, false), sheared, Options{})
	if fresh.Box != nil {
		t.Errorf("box = %v, want omitted under a sheared frame (skew)", fresh.Box)
	}
	if reason := omittedReason(fresh, "box"); reason == "" {
		t.Errorf("box omission empty, want a skew reason, omitted %v", fresh.Omitted)
	}
}
