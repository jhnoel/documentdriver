package render

import (
	"math"
	"slices"

	"github.com/jhnoel/documentdriver/internal/geom"
)

// Poly is one flattened subpath: its points and whether it is closed.
type Poly struct {
	Pts    []geom.Pt
	Closed bool
}

// maxCurveSteps bounds the pieces one cubic is cut into.
const maxCurveSteps = 256

// Flatten turns p into polylines, cutting each cubic into straight
// pieces no more than about tol from the curve (tol in the path's
// units). A subpath without a move starts at the last point.
func Flatten(p Path, tol float64) []Poly {
	if tol <= 0 {
		tol = 0.1
	}
	var out []Poly
	var cur []geom.Pt
	var last geom.Pt
	flush := func(closed bool) {
		if len(cur) > 1 || (closed && len(cur) > 0) {
			out = append(out, Poly{Pts: cur, Closed: closed})
		}
		cur = nil
	}
	for _, s := range p {
		switch s.Op {
		case 'M':
			flush(false)
			cur = []geom.Pt{s.P[0]}
			last = s.P[0]
		case 'L':
			if cur == nil {
				cur = []geom.Pt{last}
			}
			cur = append(cur, s.P[0])
			last = s.P[0]
		case 'C':
			if cur == nil {
				cur = []geom.Pt{last}
			}
			cur = appendCubic(cur, last, s.P[0], s.P[1], s.P[2], tol)
			last = s.P[2]
		case 'Z':
			if len(cur) > 0 {
				first := cur[0]
				flush(true)
				last = first
			}
		}
	}
	flush(false)
	return out
}

// appendCubic appends the flattened cubic p0..p3 (without p0) to pts.
func appendCubic(pts []geom.Pt, p0, p1, p2, p3 geom.Pt, tol float64) []geom.Pt {
	// The control polygon's deviation bounds the curve's: n pieces
	// keep the error under tol (Wang's formula).
	dx := math.Max(math.Abs(p0.X-2*p1.X+p2.X), math.Abs(p1.X-2*p2.X+p3.X))
	dy := math.Max(math.Abs(p0.Y-2*p1.Y+p2.Y), math.Abs(p1.Y-2*p2.Y+p3.Y))
	d := math.Hypot(dx, dy)
	n := 1
	if d > 0 && !math.IsNaN(d) && !math.IsInf(d, 0) {
		n = int(math.Ceil(math.Sqrt(3 * d / (4 * tol))))
	}
	n = min(max(n, 1), maxCurveSteps)
	for i := 1; i <= n; i++ {
		t := float64(i) / float64(n)
		u := 1 - t
		a, b, c, e := u*u*u, 3*u*u*t, 3*u*t*t, t*t*t
		pts = append(pts, geom.Pt{
			X: a*p0.X + b*p1.X + c*p2.X + e*p3.X,
			Y: a*p0.Y + b*p1.Y + c*p2.Y + e*p3.Y,
		})
	}
	return pts
}

// Dash cuts the polylines into the "on" pieces of pattern (lengths in
// points, alternating on and off, starting on; an odd count repeats,
// as SVG's stroke-dasharray does) and returns them as one path of open
// subpaths. An empty pattern, or one of zero length, returns the
// polylines whole. The pattern restarts at each subpath.
func Dash(polys []Poly, pattern []float64) Path {
	var out Path
	if len(pattern)%2 == 1 {
		pattern = append(slices.Clip(pattern), pattern...)
	}
	total := 0.0
	for _, v := range pattern {
		total += v
	}
	for _, pl := range polys {
		pts := pl.Pts
		if pl.Closed && len(pts) > 0 {
			pts = append(slices.Clip(pts), pts[0])
		}
		if len(pattern) == 0 || !(total > 0) {
			for i, p := range pts {
				if i == 0 {
					out = out.MoveTo(p)
				} else {
					out = out.LineTo(p)
				}
			}
			continue
		}
		idx, left, on := 0, pattern[0], true
		drawing := false
		for i := 1; i < len(pts); i++ {
			a, b := pts[i-1], pts[i]
			seg := math.Hypot(b.X-a.X, b.Y-a.Y)
			pos := 0.0
			for seg-pos > 1e-9 {
				step := math.Min(left, seg-pos)
				p0 := lerpPt(a, b, pos/seg)
				p1 := lerpPt(a, b, (pos+step)/seg)
				if on {
					if !drawing {
						out = out.MoveTo(p0)
						drawing = true
					}
					out = out.LineTo(p1)
				}
				pos += step
				left -= step
				if left <= 1e-9 {
					idx = (idx + 1) % len(pattern)
					left, on = pattern[idx], !on
					drawing = false
				}
			}
		}
	}
	return out
}

// lerpPt returns a + (b-a)*t.
func lerpPt(a, b geom.Pt, t float64) geom.Pt {
	return geom.Pt{X: a.X + (b.X-a.X)*t, Y: a.Y + (b.Y-a.Y)*t}
}
