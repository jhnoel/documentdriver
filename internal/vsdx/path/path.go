// Package path holds the geometry forms of the master-less shapes vsdx
// writes (docs/contract.md §3, object.add geom): the unit-square rect,
// the Ellipse row, and the polyline and curve paths, whose points are
// stored in a shape's one Geometry section as fractions of its Width
// and Height (RelMoveTo, RelLineTo and RelCubBezTo rows, MS-VSDX
// visio.xsd lines 3507-3517). CurveSegs is the one definition of a
// curve through points, used by the writer to store a curve and by the
// reader to recognise one; Parse is the reader's recognition. Pure
// arithmetic over the tree, no I/O.
package path

import (
	"math"

	"github.com/jhnoel/documentdriver/internal/geom"
)

// The geom names of the forms, as plans write them and read shows them.
const (
	Rect     = "rect"
	Ellipse  = "ellipse"
	Polyline = "polyline"
	Curve    = "curve"
)

// Seg is one cubic Bézier segment of a curve: its two control points
// and its end point; it starts where the previous segment ends.
type Seg struct{ C1, C2, End geom.Pt }

// CurveSegs returns the cubic Bézier segments of the uniform
// Catmull-Rom spline through pts (tension 1/2: the tangent at a point
// is half the chord between its neighbours), which passes through
// every point. An open curve has len(pts)-1 segments and takes the
// tangent at each end from its one neighbour (the end point stands in
// for its missing neighbour); a closed curve has len(pts) segments,
// the last ending at pts[0], and wraps the neighbours around. Fewer
// than two points (three when closed) give no segments.
func CurveSegs(pts []geom.Pt, closed bool) []Seg {
	n := len(pts)
	if n < 2 || (closed && n < 3) {
		return nil
	}
	at := func(i int) geom.Pt {
		if closed {
			return pts[((i%n)+n)%n]
		}
		return pts[min(max(i, 0), n-1)]
	}
	count := n - 1
	if closed {
		count = n
	}
	segs := make([]Seg, count)
	for i := range count {
		p0, p1, p2, p3 := at(i-1), at(i), at(i+1), at(i+2)
		segs[i] = Seg{
			C1:  geom.Pt{X: p1.X + (p2.X-p0.X)/6, Y: p1.Y + (p2.Y-p0.Y)/6},
			C2:  geom.Pt{X: p2.X - (p3.X-p1.X)/6, Y: p2.Y - (p3.Y-p1.Y)/6},
			End: p2,
		}
	}
	return segs
}

// Tolerance is how far apart, in relative units, two stored numbers may
// be and still count as equal when a path is recognised: far below any
// visible difference, far above the rounding of a shortest-form float.
const Tolerance = 1e-9

// near reports whether a and b are within Tolerance in both axes.
func near(a, b geom.Pt) bool {
	return math.Abs(a.X-b.X) <= Tolerance && math.Abs(a.Y-b.Y) <= Tolerance
}

// Bounds returns the smallest axis-aligned box holding pts, as its
// minimum and maximum corners; pts must not be empty.
func Bounds(pts []geom.Pt) (lo, hi geom.Pt) {
	lo, hi = pts[0], pts[0]
	for _, p := range pts[1:] {
		lo.X, lo.Y = min(lo.X, p.X), min(lo.Y, p.Y)
		hi.X, hi.Y = max(hi.X, p.X), max(hi.Y, p.Y)
	}
	return lo, hi
}

// Normalize returns the points a path of kind stores for pts: when
// closed and the last point repeats the first (within Tolerance), the
// repeat is dropped, since a closed path returns to its start itself.
func Normalize(pts []geom.Pt, closed bool) []geom.Pt {
	if closed && len(pts) > 1 && near(pts[0], pts[len(pts)-1]) {
		return pts[:len(pts)-1]
	}
	return pts
}

// MinPoints is the fewest points an open (2) or closed (3) path takes.
func MinPoints(closed bool) int {
	if closed {
		return 3
	}
	return 2
}
