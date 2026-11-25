package path_test

import (
	"math"
	"math/rand/v2"
	"strings"
	"testing"

	"github.com/jhnoel/documentdriver/internal/geom"
	"github.com/jhnoel/documentdriver/internal/ooxml/tree"
	"github.com/jhnoel/documentdriver/internal/vsdx/emit"
	"github.com/jhnoel/documentdriver/internal/vsdx/path"
)

// TestCurveSegsThroughPoints checks the curve's defining properties:
// an open curve of n points has n-1 segments ending at points 1..n-1,
// a closed one n segments ending back at point 0, and the tangent at
// an inner point is continuous (the control points on both sides are
// mirror images about it), so the curve is smooth through it.
func TestCurveSegsThroughPoints(t *testing.T) {
	pts := []geom.Pt{{X: 0, Y: 0}, {X: 1, Y: 2}, {X: 3, Y: 1}, {X: 4, Y: 4}}
	for _, closed := range []bool{false, true} {
		segs := path.CurveSegs(pts, closed)
		want := len(pts) - 1
		if closed {
			want = len(pts)
		}
		if len(segs) != want {
			t.Fatalf("closed=%v: %d segments, want %d", closed, len(segs), want)
		}
		for i, s := range segs {
			end := pts[(i+1)%len(pts)]
			if s.End != end {
				t.Errorf("closed=%v: segment %d ends at %v, want %v", closed, i, s.End, end)
			}
		}
		for i := 0; i+1 < len(segs); i++ {
			p := segs[i].End
			in, out := segs[i].C2, segs[i+1].C1
			if math.Abs((p.X-in.X)-(out.X-p.X)) > 1e-12 || math.Abs((p.Y-in.Y)-(out.Y-p.Y)) > 1e-12 {
				t.Errorf("closed=%v: tangent breaks at point %d: in %v out %v", closed, i+1, in, out)
			}
		}
	}
	if path.CurveSegs(pts[:1], false) != nil || path.CurveSegs(pts[:2], true) != nil {
		t.Errorf("CurveSegs with too few points returned segments")
	}
}

// TestCurveSegsLine checks a row derived by hand: through three
// collinear, evenly spaced points the curve is the straight line, with
// control points at thirds ((0,0) -> (1,0): c1 (1/6, 0) from the
// duplicated end tangent (p2-p0)/6 = (1,0)/6, c2 = p2 - (p3-p1)/6 =
// 1 - 2/6).
func TestCurveSegsLine(t *testing.T) {
	segs := path.CurveSegs([]geom.Pt{{X: 0, Y: 0}, {X: 1, Y: 0}, {X: 2, Y: 0}}, false)
	want := path.Seg{C1: geom.Pt{X: 1.0 / 6}, C2: geom.Pt{X: 1 - 2.0/6}, End: geom.Pt{X: 1}}
	got := segs[0]
	for _, d := range []float64{got.C1.X - want.C1.X, got.C1.Y - want.C1.Y, got.C2.X - want.C2.X, got.C2.Y - want.C2.Y, got.End.X - want.End.X, got.End.Y - want.End.Y} {
		if math.Abs(d) > 1e-15 {
			t.Errorf("segment 0 = %+v, want %+v", got, want)
			break
		}
	}
}

// emitParse writes a shape of kind through emit.ShapeXML and parses its
// geometry back.
func emitParse(t *testing.T, kind string, pts []geom.Pt, closed bool) (path.Geometry, bool) {
	t.Helper()
	b, err := emit.ShapeXML(emit.Shape{ID: 1, Geom: kind, Width: 2, Height: 1, PinX: 1, PinY: 1, LocPinX: 1, LocPinY: 0.5, Style: 3, Points: pts, Closed: closed})
	if err != nil {
		t.Fatalf("ShapeXML(%s) error = %v", kind, err)
	}
	doc, err := tree.Parse(b)
	if err != nil {
		t.Fatalf("parse %s: %v", b, err)
	}
	return path.Parse(doc.Root())
}

// TestParseEmitted checks that each form emit writes is recognised:
// rect and ellipse by kind, polylines and curves, open and closed,
// with their points (a closed path without its repeat).
func TestParseEmitted(t *testing.T) {
	pts := []geom.Pt{{X: 0, Y: 0}, {X: 0.5, Y: 1}, {X: 1, Y: 0.25}}
	for _, kind := range []string{path.Rect, path.Ellipse} {
		g, ok := emitParse(t, kind, nil, false)
		if !ok || g.Kind != kind {
			t.Errorf("Parse(%s) = %+v, %v", kind, g, ok)
		}
	}
	for _, kind := range []string{path.Polyline, path.Curve} {
		for _, closed := range []bool{false, true} {
			g, ok := emitParse(t, kind, pts, closed)
			if !ok || g.Kind != kind || g.Closed != closed || len(g.Points) != len(pts) {
				t.Fatalf("Parse(%s closed=%v) = %+v, %v", kind, closed, g, ok)
			}
			for i, p := range g.Points {
				if p != pts[i] {
					t.Errorf("Parse(%s closed=%v) point %d = %v, want %v", kind, closed, i, p, pts[i])
				}
			}
		}
	}
}

// TestParseRoundTripProperty checks, on random point sets, that a path
// emit writes parses back to exactly its points, kind and closedness.
func TestParseRoundTripProperty(t *testing.T) {
	rng := rand.New(rand.NewPCG(1, 2))
	for trial := range 200 {
		n := 3 + rng.IntN(30)
		pts := make([]geom.Pt, n)
		for i := range pts {
			pts[i] = geom.Pt{X: rng.Float64(), Y: rng.Float64()}
		}
		kind := path.Polyline
		if trial%2 == 1 {
			kind = path.Curve
		}
		closed := trial%4 >= 2
		g, ok := emitParse(t, kind, pts, closed)
		if !ok || g.Kind != kind || g.Closed != closed || len(g.Points) != n {
			t.Fatalf("trial %d: Parse = %+v, %v", trial, g, ok)
		}
		for i, p := range g.Points {
			if p != pts[i] {
				t.Fatalf("trial %d: point %d = %v, want %v", trial, i, p, pts[i])
			}
		}
	}
}

// TestParseRefuses checks the forms Parse does not claim, each a
// mutation of an emitted curve or polyline: a formula on a row cell, a
// second Geometry section, a closed fill on a path that does not
// return to its start, control points that are not the curve's, a
// deleted row, a hidden line, mixed row types and a lone move.
func TestParseRefuses(t *testing.T) {
	pts := []geom.Pt{{X: 0, Y: 0}, {X: 0.5, Y: 1}, {X: 1, Y: 0.25}}
	emitted := func(kind string, closed bool) string {
		b, err := emit.ShapeXML(emit.Shape{ID: 1, Geom: kind, Width: 2, Height: 1, PinX: 1, PinY: 1, LocPinX: 1, LocPinY: 0.5, Style: 3, Points: pts, Closed: closed})
		if err != nil {
			t.Fatal(err)
		}
		return string(b)
	}
	curve, line := emitted(path.Curve, false), emitted(path.Polyline, false)
	cases := []struct{ name, xml string }{
		{"formula", strings.Replace(line, "<Cell N='X' V='0.5'/>", "<Cell N='X' V='0.5' F='Width*0.25'/>", 1)},
		{"two sections", strings.Replace(line, "</Shape>", "<Section N='Geometry' IX='1'><Row T='RelMoveTo' IX='1'><Cell N='X' V='0'/><Cell N='Y' V='0'/></Row></Section></Shape>", 1)},
		{"filled open", strings.Replace(line, "<Cell N='NoFill' V='1'/>", "<Cell N='NoFill' V='0'/>", 1)},
		{"foreign controls", strings.Replace(curve, "<Cell N='A' V='", "<Cell N='A' V='9", 1)},
		{"deleted row", strings.Replace(line, "<Row T='RelLineTo' IX='2'>", "<Row T='RelLineTo' IX='2' Del='1'>", 1)},
		{"no line", strings.Replace(line, "<Cell N='NoLine' V='0'/>", "<Cell N='NoLine' V='1'/>", 1)},
		{"mixed rows", strings.Replace(line, "<Row T='RelLineTo' IX='3'>", "<Row T='LineTo' IX='3'>", 1)},
		{"lone move", "<Shape ID='1'><Section N='Geometry' IX='0'><Row T='RelMoveTo' IX='1'><Cell N='X' V='0'/><Cell N='Y' V='0'/></Row></Section></Shape>"},
	}
	for _, tc := range cases {
		doc, err := tree.ParseString(tc.xml)
		if err != nil {
			t.Fatalf("%s: %v", tc.name, err)
		}
		if g, ok := path.Parse(doc.Root()); ok {
			t.Errorf("%s: Parse = %+v, true; want false", tc.name, g)
		}
	}
}
