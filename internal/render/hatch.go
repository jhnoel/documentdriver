package render

import (
	"math"
	"slices"

	"github.com/jhnoel/documentdriver/internal/geom"
)

// MaxHatchMarks bounds the lines or dots one hatched area draws; an
// area that would need more is drawn with this many, and Hatch reports
// that it was cut. maxHatchWork bounds the line-edge tests (Hatch) and
// grid cells (Dots) one area costs, so a huge or many-edged area is
// cut rather than slow.
const (
	MaxHatchMarks = 20000
	maxHatchWork  = 100_000_000
)

// Hatch returns the parallel lines of a hatch clipped to area (closed
// polylines, non-zero winding): lines at angle degrees (clockwise from
// the x axis, y down), spacing apart, through the page origin so that
// neighbouring areas line up, as a path of open two-point subpaths.
// cut is true when MaxHatchMarks stopped it.
func Hatch(area []Poly, angle, spacing float64) (p Path, cut bool) {
	if !(spacing > 0) || len(area) == 0 {
		return nil, false
	}
	s, c := geom.SinCosDeg(angle)
	d := geom.Pt{X: c, Y: s}  // along the lines
	n := geom.Pt{X: -s, Y: c} // across them
	lo, hi := math.Inf(1), math.Inf(-1)
	for _, pl := range area {
		for _, pt := range pl.Pts {
			v := pt.X*n.X + pt.Y*n.Y
			lo, hi = math.Min(lo, v), math.Max(hi, v)
		}
	}
	if math.IsInf(lo, 0) || math.IsNaN(lo) || math.IsNaN(hi) {
		return nil, false
	}
	edges := 0
	for _, pl := range area {
		edges += len(pl.Pts)
	}
	first, last := math.Ceil(lo/spacing), math.Floor(hi/spacing)
	limit := float64(MaxHatchMarks)
	if edges > 0 {
		limit = math.Min(limit, math.Floor(maxHatchWork/float64(edges)))
	}
	if last-first+1 > limit {
		last, cut = first+limit-1, true
	}
	type cross struct {
		t float64
		w int
	}
	var xs []cross
	for k := first; k <= last; k++ {
		off := k * spacing
		if off-lo < 1e-9 || hi-off < 1e-9 {
			continue // a line along the outline's extreme edge
		}
		xs = xs[:0]
		for _, pl := range area {
			m := len(pl.Pts)
			for i := range m {
				a, b := pl.Pts[i], pl.Pts[(i+1)%m]
				va, vb := a.X*n.X+a.Y*n.Y-off, b.X*n.X+b.Y*n.Y-off
				// Half-open rule: an edge counts when it crosses from
				// one side to the other, a vertex on the line counted
				// once.
				if (va < 0) == (vb < 0) {
					continue
				}
				f := va / (va - vb)
				pt := lerpPt(a, b, f)
				w := 1
				if vb < va {
					w = -1
				}
				xs = append(xs, cross{t: pt.X*d.X + pt.Y*d.Y, w: w})
			}
		}
		slices.SortFunc(xs, func(a, b cross) int {
			switch {
			case a.t < b.t:
				return -1
			case a.t > b.t:
				return 1
			}
			return 0
		})
		wind, t0 := 0, 0.0
		base := geom.Pt{X: n.X * off, Y: n.Y * off}
		for _, x := range xs {
			before := wind
			wind += x.w
			switch {
			case before == 0 && wind != 0:
				t0 = x.t
			case before != 0 && wind == 0 && x.t-t0 > 1e-9:
				p = p.MoveTo(geom.Pt{X: base.X + d.X*t0, Y: base.Y + d.Y*t0})
				p = p.LineTo(geom.Pt{X: base.X + d.X*x.t, Y: base.Y + d.Y*x.t})
			}
		}
	}
	return p, cut
}

// Inside reports whether pt is inside area (closed polylines,
// non-zero winding).
func Inside(area []Poly, pt geom.Pt) bool {
	wind := 0
	for _, pl := range area {
		m := len(pl.Pts)
		for i := range m {
			a, b := pl.Pts[i], pl.Pts[(i+1)%m]
			if (a.Y <= pt.Y) == (b.Y <= pt.Y) {
				continue
			}
			x := a.X + (pt.Y-a.Y)/(b.Y-a.Y)*(b.X-a.X)
			if x > pt.X {
				if b.Y > a.Y {
					wind++
				} else {
					wind--
				}
			}
		}
	}
	return wind != 0
}

// Dots returns square dots of side size on a grid spacing apart
// (aligned to the page origin, alternate rows offset by half a
// spacing) whose centres are inside area, as one fill path. cut is
// true when MaxHatchMarks stopped it.
func Dots(area []Poly, spacing, size float64) (p Path, cut bool) {
	if !(spacing > 0) || !(size > 0) {
		return nil, false
	}
	x0, y0, x1, y1 := math.Inf(1), math.Inf(1), math.Inf(-1), math.Inf(-1)
	for _, pl := range area {
		for _, pt := range pl.Pts {
			x0, y0 = math.Min(x0, pt.X), math.Min(y0, pt.Y)
			x1, y1 = math.Max(x1, pt.X), math.Max(y1, pt.Y)
		}
	}
	if math.IsInf(x0, 0) || math.IsNaN(x0+y0+x1+y1) {
		return nil, false
	}
	edges := 0
	for _, pl := range area {
		edges += len(pl.Pts)
	}
	if (x1-x0)/spacing*(y1-y0)/spacing*float64(max(edges, 1)) > maxHatchWork {
		return nil, true
	}
	count, h := 0, size/2
	for j := math.Floor(y0 / spacing); j*spacing <= y1; j++ {
		shift := 0.0
		if int64(j)%2 != 0 {
			shift = spacing / 2
		}
		for i := math.Floor(x0/spacing) - 1; i*spacing+shift <= x1; i++ {
			c := geom.Pt{X: i*spacing + shift, Y: j * spacing}
			if c.X < x0 || !Inside(area, c) {
				continue
			}
			if count == MaxHatchMarks {
				return p, true
			}
			count++
			p = append(p, Polygon(
				geom.Pt{X: c.X - h, Y: c.Y - h}, geom.Pt{X: c.X + h, Y: c.Y - h},
				geom.Pt{X: c.X + h, Y: c.Y + h}, geom.Pt{X: c.X - h, Y: c.Y + h})...)
		}
	}
	return p, false
}
