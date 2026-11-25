package emit

import (
	"fmt"
	"math"
	"strconv"
	"strings"

	"github.com/jhnoel/documentdriver/internal/geom"
	"github.com/jhnoel/documentdriver/internal/vsdx/path"
	"github.com/jhnoel/documentdriver/internal/vsdx/vsd"
)

// checkPath refuses a path with too few points for closed or with a
// point that is not finite.
func checkPath(pts []geom.Pt, closed bool) error {
	if len(pts) < path.MinPoints(closed) {
		return fmt.Errorf("vsdx: emit ShapeXML: a path needs at least %d points, got %d", path.MinPoints(closed), len(pts))
	}
	for i, p := range pts {
		if math.IsNaN(p.X) || math.IsInf(p.X, 0) || math.IsNaN(p.Y) || math.IsInf(p.Y, 0) {
			return fmt.Errorf("vsdx: emit ShapeXML: point %d is not finite", i)
		}
	}
	return nil
}

// writePathGeometry writes Geometry section ix of a polyline or curve
// (one section per path) in the form path.Parse recognises: NoFill 1
// when open and 0 when closed, NoLine, NoShow, NoSnap and NoQuickDrag 0, a RelMoveTo at the
// first point, then for a polyline one RelLineTo per further point and
// for a curve one RelCubBezTo per path.CurveSegs segment (X, Y the end,
// A, B the first control point, C, D the second: MS-VSDX RelCubBezTo,
// visio.xsd line 3767); a closed path ends with a row back at the
// first point. Every cell is a plain value of relative units.
func writePathGeometry(b *strings.Builder, kind string, pts []geom.Pt, closed bool, ix int) {
	b.WriteString("<Section N='Geometry' IX='" + strconv.Itoa(ix) + "'>")
	if closed {
		writeCell(b, "NoFill", zero)
	} else {
		writeCell(b, "NoFill", one)
	}
	writeCell(b, "NoLine", zero)
	writeCell(b, "NoShow", zero)
	writeCell(b, "NoSnap", zero)
	writeCell(b, "NoQuickDrag", zero)
	writeRelRow(b, "RelMoveTo", 1, pts[0], nil)
	row := 2
	if kind == path.Curve {
		for _, s := range path.CurveSegs(pts, closed) {
			writeRelRow(b, "RelCubBezTo", row, s.End, &s)
			row++
		}
	} else {
		for _, p := range pts[1:] {
			writeRelRow(b, "RelLineTo", row, p, nil)
			row++
		}
		if closed {
			writeRelRow(b, "RelLineTo", row, pts[0], nil)
		}
	}
	b.WriteString("</Section>")
}

// writeRelRow writes one relative row at IX ix: X and Y of p, and with
// seg its control points as A, B, C, D.
func writeRelRow(b *strings.Builder, typ string, ix int, p geom.Pt, seg *path.Seg) {
	b.WriteString("<Row T='")
	b.WriteString(typ)
	b.WriteString("' IX='")
	b.WriteString(strconv.Itoa(ix))
	b.WriteString("'>")
	writeCell(b, "X", vsd.FormatInches(p.X))
	writeCell(b, "Y", vsd.FormatInches(p.Y))
	if seg != nil {
		writeCell(b, "A", vsd.FormatInches(seg.C1.X))
		writeCell(b, "B", vsd.FormatInches(seg.C1.Y))
		writeCell(b, "C", vsd.FormatInches(seg.C2.X))
		writeCell(b, "D", vsd.FormatInches(seg.C2.Y))
	}
	b.WriteString("</Row>")
}
