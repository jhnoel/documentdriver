package path

import (
	"strconv"

	"github.com/jhnoel/documentdriver/internal/geom"
	"github.com/jhnoel/documentdriver/internal/ooxml/tree"
	"github.com/jhnoel/documentdriver/internal/vsdx/vsd"
)

// Geometry is a shape's geometry in one of the forms vsdx writes: its
// Kind (Rect, Ellipse, Polyline, Curve), whether a path is Closed, and
// a path's Points in relative units (fractions of Width and Height,
// y up from the shape's lower-left corner), without the closing repeat.
// A polyline or curve may have several paths, one Geometry section
// each: Paths holds them all in section order, and Closed and Points
// are the first.
type Geometry struct {
	Kind   string
	Closed bool
	Points []geom.Pt
	Paths  []Subpath
}

// Subpath is one path of a polyline or curve: whether it is closed and
// its points in relative units, without the closing repeat.
type Subpath struct {
	Closed bool
	Points []geom.Pt
}

// ellipseFormulas are the formulas of the Ellipse row emit writes, by
// cell: the centre, the right point (A, B) and the top point (C, D).
var ellipseFormulas = map[string]string{
	"X": "Width*0.5", "Y": "Height*0.5", "A": "Width*1",
	"B": "Height*0.5", "C": "Width*0.5", "D": "Height*1",
}

// Parse recognises the geometry a shape element stores locally, and
// reports false when it is not exactly one of the forms vsdx writes:
// a Geometry section, not deleted, NoLine and NoShow 0 or absent, and
// rows of one form with plain numeric cells (no formulas), or, for a
// polyline or curve, several such sections, IX 0, 1, 2... in order,
// each one path of the same kind (a hatch of disjoint lines):
//   - Rect: NoFill 0, RelMoveTo (0,0), RelLineTo (1,0) (1,1) (0,1) (0,0);
//   - Ellipse: NoFill 0, one Ellipse row with emit's Width*/Height*
//     formulas;
//   - Polyline: RelMoveTo then RelLineTo rows, at least one;
//   - Curve: RelMoveTo then RelCubBezTo rows whose control points are
//     CurveSegs of the rows' end points.
//
// A path with NoFill 1 is open and every row's point is shown; one with
// NoFill 0 is closed and must end at its start (the repeat is dropped).
// The walk is over the shape's own children and one section's rows,
// never a list of them. A master instance's geometry is the master's,
// so callers ask only of master-less shapes.
func Parse(shape *tree.Node) (Geometry, bool) {
	if shape == nil {
		return Geometry{}, false
	}
	var out Geometry
	n := 0
	for c := range shape.ElementsSeq() {
		if c.Local() != vsd.ElemSection {
			continue
		}
		if name, _ := c.Attr("N"); name != vsd.SectionGeometry {
			continue
		}
		// Sections are stored in IX order, 0, 1, 2..., as emit writes
		// them.
		if ix, _ := c.Attr("IX"); ix != strconv.Itoa(n) {
			return Geometry{}, false
		}
		g, ok := parseSection(c)
		if !ok {
			return Geometry{}, false
		}
		switch {
		case n == 0:
			out = g
		case out.Kind != g.Kind || (g.Kind != Polyline && g.Kind != Curve):
			// Several sections are one polyline or curve's paths.
			return Geometry{}, false
		}
		if g.Kind == Polyline || g.Kind == Curve {
			out.Paths = append(out.Paths, Subpath{Closed: g.Closed, Points: g.Points})
		}
		n++
	}
	if n == 0 {
		return Geometry{}, false
	}
	return out, true
}

// parseSection recognises one Geometry section (Parse's forms).
func parseSection(section *tree.Node) (Geometry, bool) {
	if v, ok := section.Attr("Del"); ok && v == "1" {
		return Geometry{}, false
	}
	noFill := false
	var rows []row
	for c := range section.ElementsSeq() {
		switch c.Local() {
		case vsd.ElemCell:
			name, _ := c.Attr("N")
			v, ok := plainNumber(c)
			switch {
			case !ok:
				return Geometry{}, false
			case name == "NoFill":
				noFill = v == 1
				if v != 0 && v != 1 {
					return Geometry{}, false
				}
			case name == "NoLine" || name == "NoShow":
				if v != 0 {
					return Geometry{}, false
				}
			}
		case vsd.ElemRow:
			r, ok := readRow(c)
			if !ok {
				return Geometry{}, false
			}
			rows = append(rows, r)
		}
	}
	return recognise(rows, noFill)
}

// row is one Geometry row: its type, its point (X, Y), its other cells
// A to D, which cells it stores, and its node for the Ellipse check.
type row struct {
	typ  string
	pt   geom.Pt
	abcd [4]float64
	has  [6]bool // X, Y, A, B, C, D
	node *tree.Node
}

// rowCells are the cell names a row may store, in row.has order.
var rowCells = [6]string{"X", "Y", "A", "B", "C", "D"}

// readRow reads one Row element: its T and its cells X, Y, A to D.
// An Ellipse row's cells are checked by recognise (they carry
// formulas); any other row's cells must be plain numbers, and a cell
// of another name, a deleted row or a row without T is refused.
func readRow(n *tree.Node) (row, bool) {
	typ, ok := n.Attr("T")
	if !ok {
		return row{}, false
	}
	if v, ok := n.Attr("Del"); ok && v == "1" {
		return row{}, false
	}
	r := row{typ: typ, node: n}
	if typ == "Ellipse" {
		return r, true
	}
	for c := range n.ElementsSeq() {
		if c.Local() != vsd.ElemCell {
			continue
		}
		name, _ := c.Attr("N")
		v, ok := plainNumber(c)
		if !ok {
			return row{}, false
		}
		i := cellIndex(name)
		if i < 0 {
			return row{}, false
		}
		r.has[i] = true
		switch i {
		case 0:
			r.pt.X = v
		case 1:
			r.pt.Y = v
		default:
			r.abcd[i-2] = v
		}
	}
	return r, r.has[0] && r.has[1]
}

// cellIndex returns the index of name in rowCells, -1 when absent.
func cellIndex(name string) int {
	for i, c := range rowCells {
		if c == name {
			return i
		}
	}
	return -1
}

// plainNumber returns a cell's V as a number, reporting false when it
// has a formula or its value is not a number.
func plainNumber(c *tree.Node) (float64, bool) {
	if _, hasF := c.Attr("F"); hasF {
		return 0, false
	}
	v, ok := c.Attr("V")
	if !ok {
		return 0, false
	}
	return vsd.Number(v)
}

// recognise names the form of a section's rows (see Parse).
func recognise(rows []row, noFill bool) (Geometry, bool) {
	if len(rows) == 1 && rows[0].typ == "Ellipse" {
		return Geometry{Kind: Ellipse}, !noFill && ellipseForm(rows[0].node)
	}
	if len(rows) < 2 || rows[0].typ != "RelMoveTo" || rows[0].has[2] {
		return Geometry{}, false
	}
	kind := ""
	switch rows[1].typ {
	case "RelLineTo":
		kind = Polyline
	case "RelCubBezTo":
		kind = Curve
	default:
		return Geometry{}, false
	}
	pts := make([]geom.Pt, len(rows))
	for i, r := range rows {
		pts[i] = r.pt
		if i == 0 {
			continue
		}
		if kind == Polyline && (r.typ != "RelLineTo" || r.has[2] || r.has[3] || r.has[4] || r.has[5]) {
			return Geometry{}, false
		}
		if kind == Curve && (r.typ != "RelCubBezTo" || !(r.has[2] && r.has[3] && r.has[4] && r.has[5])) {
			return Geometry{}, false
		}
	}
	closed := !noFill
	if closed {
		if !near(pts[0], pts[len(pts)-1]) {
			return Geometry{}, false
		}
		pts = pts[:len(pts)-1]
	}
	if len(pts) < MinPoints(closed) {
		return Geometry{}, false
	}
	if kind == Curve {
		for i, s := range CurveSegs(pts, closed) {
			r := rows[i+1]
			if !near(s.C1, geom.Pt{X: r.abcd[0], Y: r.abcd[1]}) || !near(s.C2, geom.Pt{X: r.abcd[2], Y: r.abcd[3]}) {
				return Geometry{}, false
			}
		}
	}
	if kind == Polyline && closed && isUnitSquare(pts) {
		return Geometry{Kind: Rect}, true
	}
	return Geometry{Kind: kind, Closed: closed, Points: pts}, true
}

// unitSquare is the rect form's corners after its RelMoveTo, in order.
var unitSquare = [4]geom.Pt{{X: 0, Y: 0}, {X: 1, Y: 0}, {X: 1, Y: 1}, {X: 0, Y: 1}}

// isUnitSquare reports whether pts are the rect form's corners.
func isUnitSquare(pts []geom.Pt) bool {
	if len(pts) != len(unitSquare) {
		return false
	}
	for i, p := range pts {
		if !near(p, unitSquare[i]) {
			return false
		}
	}
	return true
}

// ellipseForm reports whether an Ellipse row stores exactly emit's
// formulas for its six cells, and no other cell.
func ellipseForm(n *tree.Node) bool {
	seen := 0
	for c := range n.ElementsSeq() {
		if c.Local() != vsd.ElemCell {
			continue
		}
		name, _ := c.Attr("N")
		want, ok := ellipseFormulas[name]
		if f, _ := c.Attr("F"); !ok || f != want {
			return false
		}
		seen++
	}
	return seen == len(ellipseFormulas)
}
