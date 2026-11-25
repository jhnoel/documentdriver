package model

import (
	"cmp"
	"math"
	"slices"

	"github.com/jhnoel/documentdriver/internal/geom"
	"github.com/jhnoel/documentdriver/internal/vsdx/inherit"
	"github.com/jhnoel/documentdriver/internal/vsdx/place"
	"github.com/jhnoel/documentdriver/internal/vsdx/sheet"
	"github.com/jhnoel/documentdriver/internal/vsdx/vsd"
)

// Drawing is what the drawing view (read --format svg, render without
// LibreOffice) needs of an object beyond what read shows: its
// effective geometry sections as paths in the view (points, y down)
// and its text laid into its text block. Built only with
// Options.Draw. Notes says what the drawing approximates or leaves
// out of this object.
type Drawing struct {
	Paths []DrawPath
	Text  *TextBlock
	Notes []string
}

// DrawPath is one Geometry section: its segments in the view, whether
// it ends where it starts (Closed, so it can be filled), and its
// NoFill and NoLine cells.
type DrawPath struct {
	Segs           []Seg
	Closed         bool
	NoFill, NoLine bool
}

// Seg is one path segment in the view: Op 'M' (move to P[0]), 'L'
// (line to P[0]) or 'C' (cubic Bézier through controls P[0], P[1] to
// P[2]).
type Seg struct {
	Op byte
	P  [3]geom.Pt
}

// End returns the point a segment ends at.
func (s Seg) End() geom.Pt {
	if s.Op == 'C' {
		return s.P[2]
	}
	return s.P[0]
}

// maxDrawRows bounds the geometry rows one object draws; a shape with
// more is drawn up to the bound, with a note.
const maxDrawRows = 1 << 12

// drawingOf sets o.Draw from the shape's effective Geometry sections
// (inherit.Rows, so a master instance draws its master's geometry)
// and its text block. Rows are drawn in section IX then row IX order;
// a value is the cell's cached V (the value Visio last computed), never
// a formula evaluated here. A shape whose transform cannot be read
// draws nothing, with a note.
func drawingOf(o *Object, r *inherit.Resolver, s *inherit.Shape, f place.Frame, pageH float64) {
	if o == nil || s == nil {
		return
	}
	d := &Drawing{}
	o.Draw = d
	t, missing, ok := transformOf(r, s)
	if !ok {
		d.Notes = append(d.Notes, "not drawn: "+missing+" not derived")
		return
	}
	m := t.Local().Then(f.M).Then(place.View(pageH))
	rows := r.Rows(s, vsd.SectionGeometry)
	if len(rows) > maxDrawRows {
		rows = rows[:maxDrawRows]
		d.Notes = append(d.Notes, "geometry cut at 4096 rows")
	}
	keys := make([]sheet.Key, 0, len(rows))
	for _, row := range rows {
		if row.Key.RowIX >= 0 {
			keys = append(keys, row.Key)
		}
	}
	slices.SortStableFunc(keys, func(a, b sheet.Key) int {
		if c := cmp.Compare(a.SectionIX, b.SectionIX); c != 0 {
			return c
		}
		return cmp.Compare(a.RowIX, b.RowIX)
	})
	g := geomWalker{r: r, s: s, t: t, m: m, d: d}
	if ms, ok := r.MasterShape(s); ok {
		g.master = ms.Sheet
	}
	for i := 0; i < len(keys); {
		j := i
		for j < len(keys) && keys[j].SectionIX == keys[i].SectionIX {
			j++
		}
		g.section(keys[i].SectionIX, keys[i:j])
		i = j
	}
	textBlockOf(d, r, s, t, f, pageH)
}

// geomWalker draws the rows of one shape's Geometry sections.
type geomWalker struct {
	r      *inherit.Resolver
	s      *inherit.Shape
	master *sheet.Sheet
	t      place.Transform
	m      geom.Affine
	d      *Drawing
	cur    geom.Pt // current point, local inches
	has    bool
	seg    []Seg
}

// cell returns the effective number of a row cell, reporting false
// when it is absent or not a number.
func (g *geomWalker) cell(k sheet.Key, name string) (float64, bool) {
	c, _, ok := g.r.Cell(g.s, k.WithCell(name))
	if !ok {
		return 0, false
	}
	return vsd.Number(c.V)
}

// flag returns a section-level boolean cell (NoFill, NoLine, NoShow),
// false when absent.
func (g *geomWalker) flag(secIX int, name string) bool {
	c, _, ok := g.r.Cell(g.s, sheet.Key{Section: vsd.SectionGeometry, SectionIX: secIX, RowIX: -1, Cell: name})
	if !ok {
		return false
	}
	if n, ok := vsd.Number(c.V); ok {
		return n != 0
	}
	b, _ := vsd.Bool(c.V)
	return b
}

// rowCellNames are the cells a Geometry row may store, searched for a
// stored cell whose row element carries the row's type.
var rowCellNames = [...]string{"X", "Y", "A", "B", "C", "D", "E"}

// rowType returns a Geometry row's T: from the stored row element of
// any of its cells, on the shape's own sheet first and then its
// master shape's.
func (g *geomWalker) rowType(k sheet.Key) string {
	for _, sh := range [2]*sheet.Sheet{g.s.Sheet, g.master} {
		if sh == nil {
			continue
		}
		for _, name := range rowCellNames {
			c, ok := sh.Cell(k.WithCell(name))
			if !ok || c.Node == nil || c.Node.Parent() == nil {
				continue
			}
			if t, ok := c.Node.Parent().Attr("T"); ok {
				return t
			}
		}
	}
	return ""
}

// section draws one Geometry section's rows (sorted by IX) into a
// DrawPath. A hidden section (NoShow) draws nothing.
func (g *geomWalker) section(secIX int, keys []sheet.Key) {
	if g.flag(secIX, vsd.CellGeomNoShow) {
		return
	}
	g.seg, g.has = nil, false
	for _, k := range keys {
		g.row(k, g.rowType(k))
	}
	if len(g.seg) < 2 {
		return
	}
	start, end := g.seg[0].P[0], g.seg[len(g.seg)-1].End()
	closed := math.Abs(end.X-start.X) < 1e-6 && math.Abs(end.Y-start.Y) < 1e-6
	g.d.Paths = append(g.d.Paths, DrawPath{
		Segs:   g.seg,
		Closed: closed,
		NoFill: g.flag(secIX, vsd.CellGeomNoFill),
		NoLine: g.flag(secIX, vsd.CellGeomNoLine),
	})
}

// moveTo starts a subpath at p (local inches).
func (g *geomWalker) moveTo(p geom.Pt) {
	g.seg = append(g.seg, Seg{Op: 'M', P: [3]geom.Pt{g.m.Apply(p)}})
	g.cur, g.has = p, true
}

// lineTo draws a line from the current point to p, starting a subpath
// at the origin when there is no current point.
func (g *geomWalker) lineTo(p geom.Pt) {
	if !g.has {
		g.moveTo(geom.Pt{})
	}
	g.seg = append(g.seg, Seg{Op: 'L', P: [3]geom.Pt{g.m.Apply(p)}})
	g.cur = p
}

// cubicTo draws a cubic Bézier from the current point.
func (g *geomWalker) cubicTo(c1, c2, p geom.Pt) {
	if !g.has {
		g.moveTo(geom.Pt{})
	}
	g.seg = append(g.seg, Seg{Op: 'C', P: [3]geom.Pt{g.m.Apply(c1), g.m.Apply(c2), g.m.Apply(p)}})
	g.cur = p
}

// note records an approximation once per object.
func (g *geomWalker) note(s string) {
	if !slices.Contains(g.d.Notes, s) {
		g.d.Notes = append(g.d.Notes, s)
	}
}

// pair returns two row cells as a point, scaled by the shape's size
// when rel, reporting whether both are derived.
func (g *geomWalker) pair(k sheet.Key, xName, yName string, rel bool) (geom.Pt, bool) {
	x, okX := g.cell(k, xName)
	y, okY := g.cell(k, yName)
	if rel {
		x, y = x*g.t.Width, y*g.t.Height
	}
	return geom.Pt{X: x, Y: y}, okX && okY
}

// row draws one Geometry row of type typ.
func (g *geomWalker) row(k sheet.Key, typ string) {
	rel := len(typ) > 3 && typ[:3] == "Rel"
	switch typ {
	case "MoveTo", "RelMoveTo":
		if p, ok := g.pair(k, "X", "Y", rel); ok {
			g.moveTo(p)
			return
		}
	case "LineTo", "RelLineTo":
		if p, ok := g.pair(k, "X", "Y", rel); ok {
			g.lineTo(p)
			return
		}
	case "RelCubBezTo":
		p, ok1 := g.pair(k, "X", "Y", true)
		c1, ok2 := g.pair(k, "A", "B", true)
		c2, ok3 := g.pair(k, "C", "D", true)
		if ok1 && ok2 && ok3 {
			g.cubicTo(c1, c2, p)
			return
		}
	case "RelQuadBezTo":
		p, ok1 := g.pair(k, "X", "Y", true)
		c, ok2 := g.pair(k, "A", "B", true)
		if ok1 && ok2 {
			p0 := g.cur
			g.cubicTo(lerp(p0, c, 2.0/3), lerp(p, c, 2.0/3), p)
			return
		}
	case "ArcTo":
		p, ok1 := g.pair(k, "X", "Y", false)
		bow, ok2 := g.cell(k, "A")
		if ok1 && ok2 {
			g.arcTo(p, bow)
			return
		}
	case "EllipticalArcTo", "RelEllipticalArcTo":
		p, ok1 := g.pair(k, "X", "Y", rel)
		c, ok2 := g.pair(k, "A", "B", rel)
		angle, ok3 := g.cell(k, "C")
		ratio, ok4 := g.cell(k, "D")
		if ok1 && ok2 && ok3 && ok4 {
			g.ellipticalArcTo(p, c, angle, ratio)
			return
		}
	case "Ellipse":
		c, ok1 := g.pair(k, "X", "Y", false)
		a, ok2 := g.pair(k, "A", "B", false)
		b, ok3 := g.pair(k, "C", "D", false)
		if ok1 && ok2 && ok3 {
			g.ellipse(c, a, b)
			return
		}
	case "PolylineTo", "NURBSTo", "SplineStart", "SplineKnot":
		// The intermediate points live in a formula (POLYLINE, NURBS)
		// or a spline whose knots are not evaluated here: the row is
		// drawn as a straight line to its end point.
		if p, ok := g.pair(k, "X", "Y", false); ok {
			g.lineTo(p)
			g.note(typ + " drawn as a straight line to its end point")
			return
		}
	case "InfiniteLine":
		g.note("InfiniteLine not drawn")
		return
	case "":
		g.note("geometry row without a type not drawn")
		return
	default:
		g.note(typ + " row not drawn")
		return
	}
	g.note(typ + " row with a cell not derived not drawn")
}

// lerp returns a + (b-a)*t.
func lerp(a, b geom.Pt, t float64) geom.Pt {
	return geom.Pt{X: a.X + (b.X-a.X)*t, Y: a.Y + (b.Y-a.Y)*t}
}

// arcTo draws Visio's ArcTo: a circular arc from the current point to
// p whose midpoint is bow from the chord's midpoint, to the right of
// the direction of travel (y up) for a positive bow, as LibreOffice's
// import draws it (a negative bow sweeps clockwise in the y-down view).
func (g *geomWalker) arcTo(p geom.Pt, bow float64) {
	p0 := g.cur
	dx, dy := p.X-p0.X, p.Y-p0.Y
	chord := math.Hypot(dx, dy)
	if bow == 0 || chord == 0 {
		g.lineTo(p)
		return
	}
	mid := geom.Pt{X: (p0.X + p.X) / 2, Y: (p0.Y + p.Y) / 2}
	nx, ny := dy/chord, -dx/chord // right-hand normal in a y-up space
	g.arc3(geom.Pt{X: mid.X + nx*bow, Y: mid.Y + ny*bow}, p)
}

// ellipticalArcTo draws Visio's EllipticalArcTo: an arc of the ellipse
// whose major axis is at angle (radians, counter-clockwise) and whose
// major to minor ratio is ratio, from the current point through c to
// p. The ellipse is mapped to a circle, the arc drawn there and mapped
// back.
func (g *geomWalker) ellipticalArcTo(p, c geom.Pt, angle, ratio float64) {
	if !(ratio > 0) || math.IsInf(ratio, 0) {
		g.lineTo(p)
		g.note("EllipticalArcTo with a ratio that is not positive drawn as a line")
		return
	}
	toCircle := geom.Rotate(-angle * 180 / math.Pi).Then(geom.Scale(1/ratio, 1))
	back, ok := toCircle.Invert()
	if !ok {
		g.lineTo(p)
		return
	}
	saved := g.m
	g.m = back.Then(saved)
	g.cur = toCircle.Apply(g.cur)
	g.arc3(toCircle.Apply(c), toCircle.Apply(p))
	g.m = saved
	g.cur = p
}

// arc3 draws the circular arc from the current point through c to p as
// cubic Béziers (at most a quarter turn each); three collinear points
// draw a line.
func (g *geomWalker) arc3(c, p geom.Pt) {
	ax, ay := g.cur.X, g.cur.Y
	bx, by := c.X, c.Y
	cx, cy := p.X, p.Y
	den := 2 * (ax*(by-cy) + bx*(cy-ay) + cx*(ay-by))
	if math.Abs(den) < 1e-12 {
		g.lineTo(p)
		return
	}
	a2, b2, c2 := ax*ax+ay*ay, bx*bx+by*by, cx*cx+cy*cy
	ox := (a2*(by-cy) + b2*(cy-ay) + c2*(ay-by)) / den
	oy := (a2*(cx-bx) + b2*(ax-cx) + c2*(bx-ax)) / den
	rad := math.Hypot(ax-ox, ay-oy)
	t0 := math.Atan2(ay-oy, ax-ox)
	tm := math.Atan2(by-oy, bx-ox)
	t1 := math.Atan2(cy-oy, cx-ox)
	sweep := ccwTurn(t0, t1)
	if ccwTurn(t0, tm) > sweep {
		sweep -= 2 * math.Pi // clockwise through c
	}
	g.circleArc(geom.Pt{X: ox, Y: oy}, rad, t0, sweep, p)
}

// ccwTurn returns the counter-clockwise turn from angle a to angle b,
// in [0, 2π).
func ccwTurn(a, b float64) float64 {
	d := math.Mod(b-a, 2*math.Pi)
	if d < 0 {
		d += 2 * math.Pi
	}
	return d
}

// circleArc draws the arc of the circle of radius rad about o from
// angle t0 through sweep radians as cubic Béziers of at most a quarter
// turn, ending exactly at end.
func (g *geomWalker) circleArc(o geom.Pt, rad, t0, sweep float64, end geom.Pt) {
	n := max(int(math.Ceil(math.Abs(sweep)/(math.Pi/2)-1e-9)), 1)
	step := sweep / float64(n)
	k := 4.0 / 3 * math.Tan(step/4)
	at := func(t float64) (geom.Pt, geom.Pt) {
		s, c := math.Sincos(t)
		return geom.Pt{X: o.X + rad*c, Y: o.Y + rad*s}, geom.Pt{X: -rad * s, Y: rad * c}
	}
	for i := range n {
		pa, da := at(t0 + step*float64(i))
		pb, db := at(t0 + step*float64(i+1))
		if i == n-1 {
			pb = end
		}
		g.cubicTo(geom.Pt{X: pa.X + k*da.X, Y: pa.Y + k*da.Y}, geom.Pt{X: pb.X - k*db.X, Y: pb.Y - k*db.Y}, pb)
	}
}

// ellipse draws Visio's Ellipse row: centre c and the ends a and b of
// two conjugate semi-diameters, as four cubic Béziers (exact under the
// affine map from the unit circle).
func (g *geomWalker) ellipse(c, a, b geom.Pt) {
	m := geom.Affine{A: a.X - c.X, B: a.Y - c.Y, C: b.X - c.X, D: b.Y - c.Y, E: c.X, F: c.Y}
	saved := g.m
	g.m = m.Then(saved)
	g.moveTo(geom.Pt{X: 1})
	g.circleArc(geom.Pt{}, 1, 0, 2*math.Pi, geom.Pt{X: 1})
	g.m = saved
	g.cur, g.has = a, true
}

// shapeNumber returns the effective number of a shape-level cell.
func shapeNumber(r *inherit.Resolver, s *inherit.Shape, name string) (float64, bool) {
	c, _, ok := r.Cell(s, sheet.ShapeCell(name))
	if !ok {
		return 0, false
	}
	return vsd.Number(c.V)
}
