// Package emit writes the XML of the things the writes create: a
// master-less 2-D shape (rect, ellipse, polyline, curve), a master-less straight
// connector with its Connect rows, a Text element with its Character
// and Paragraph rows, a page entry and an empty page. Every function is
// pure (values in, bytes out) and escapes through xmlscan; the forms
// are those of the Visio-authored corpus files (docs/vsdx-design.md,
// Create). The writes insert the bytes through the edit layer; create's
// template uses the page forms.
package emit

import (
	"fmt"
	"math"
	"strconv"
	"strings"

	"github.com/jhnoel/documentdriver/internal/geom"
	"github.com/jhnoel/documentdriver/internal/ooxml/xmlscan"
	"github.com/jhnoel/documentdriver/internal/vsdx/path"
	"github.com/jhnoel/documentdriver/internal/vsdx/vsd"
)

// Shape is a new master-less 2-D shape: its ID, optional name, kind
// ("rect" or "ellipse"), stored transform (inches, radians), fill and
// line ("" keeps the style's) and the style IDs (3, Normal, as the
// corpus's master-less shapes use).
type Shape struct {
	ID               uint32
	Name             string
	Geom             string
	PinX, PinY       float64
	Width, Height    float64
	LocPinX, LocPinY float64
	Angle            float64
	FlipX, FlipY     bool
	FillHex, LineHex string
	LineWeight       float64 // inches; 0 keeps the style's
	Style            uint32
	// Points and Closed are a polyline's or curve's path in relative
	// units (fractions of Width and Height, y up), without the closing
	// repeat; unused by rect and ellipse.
	Points []geom.Pt
	Closed bool
	// Paths, when set, are a polyline's or curve's several paths, one
	// Geometry section each (IX 0, 1, ...), in place of Points and
	// Closed.
	Paths []path.Subpath
}

// ShapeXML returns the Shape element. For a polyline or curve it is
// the transform, fill and line cells and the path's Geometry section
// (writePathGeometry), with no Connection section. Otherwise it is:
// transform cells (LocPinX and
// LocPinY with formulas Width*k, Height*k and their values), fill and
// line cells when set, a Connection section of four points (X1 top
// centre, X2 right middle, X3 bottom centre, X4 left middle, with
// Width*/Height* formulas), and one Geometry section: rect as
// RelMoveTo (0,0), RelLineTo (1,0) (1,1) (0,1) (0,0), ellipse as one
// Ellipse row (centre W/2,H/2, A,B the right point, C,D the top point,
// with formulas). Numbers through vsd.FormatInches. Chunk K3.
// It refuses a non-finite PinX, PinY, Width, Height, LocPinX, LocPinY,
// Angle or LineWeight, and a negative LineWeight, naming the field;
// a zero LineWeight keeps the style's. Chunk K3F.
func ShapeXML(s Shape) ([]byte, error) {
	isPath := s.Geom == path.Polyline || s.Geom == path.Curve
	if s.Geom != path.Rect && s.Geom != path.Ellipse && !isPath {
		return nil, fmt.Errorf("vsdx: emit ShapeXML: unknown geom %q (want rect, ellipse, polyline or curve)", s.Geom)
	}
	if isPath {
		if len(s.Paths) == 0 {
			s.Paths = []path.Subpath{{Closed: s.Closed, Points: s.Points}}
		}
		for _, p := range s.Paths {
			if err := checkPath(p.Points, p.Closed); err != nil {
				return nil, err
			}
		}
	}
	if math.IsNaN(s.PinX) || math.IsInf(s.PinX, 0) {
		return nil, fmt.Errorf("vsdx: emit ShapeXML: PinX %v is not finite", s.PinX)
	}
	if math.IsNaN(s.PinY) || math.IsInf(s.PinY, 0) {
		return nil, fmt.Errorf("vsdx: emit ShapeXML: PinY %v is not finite", s.PinY)
	}
	if math.IsNaN(s.Width) || math.IsInf(s.Width, 0) {
		return nil, fmt.Errorf("vsdx: emit ShapeXML: Width %v is not finite", s.Width)
	}
	if math.IsNaN(s.Height) || math.IsInf(s.Height, 0) {
		return nil, fmt.Errorf("vsdx: emit ShapeXML: Height %v is not finite", s.Height)
	}
	if math.IsNaN(s.LocPinX) || math.IsInf(s.LocPinX, 0) {
		return nil, fmt.Errorf("vsdx: emit ShapeXML: LocPinX %v is not finite", s.LocPinX)
	}
	if math.IsNaN(s.LocPinY) || math.IsInf(s.LocPinY, 0) {
		return nil, fmt.Errorf("vsdx: emit ShapeXML: LocPinY %v is not finite", s.LocPinY)
	}
	if math.IsNaN(s.Angle) || math.IsInf(s.Angle, 0) {
		return nil, fmt.Errorf("vsdx: emit ShapeXML: Angle %v is not finite", s.Angle)
	}
	if math.IsNaN(s.LineWeight) || math.IsInf(s.LineWeight, 0) {
		return nil, fmt.Errorf("vsdx: emit ShapeXML: LineWeight %v is not finite", s.LineWeight)
	}
	if s.LineWeight < 0 {
		return nil, fmt.Errorf("vsdx: emit ShapeXML: LineWeight %v is negative", s.LineWeight)
	}
	if !(s.Width > 0) {
		return nil, fmt.Errorf("vsdx: emit ShapeXML: Width must be positive, got %v", s.Width)
	}
	if !(s.Height > 0) {
		return nil, fmt.Errorf("vsdx: emit ShapeXML: Height must be positive, got %v", s.Height)
	}
	var b strings.Builder
	b.Grow(1024)
	b.WriteString("<Shape ID='")
	b.WriteString(strconv.FormatUint(uint64(s.ID), 10))
	b.WriteByte('\'')
	if s.Name != "" {
		name := xmlscan.EscapeAttr(s.Name, '\'')
		b.WriteString(" NameU='")
		b.WriteString(name)
		b.WriteString("' Name='")
		b.WriteString(name)
		b.WriteByte('\'')
	}
	b.WriteString(" Type='Shape'")
	if s.Style != 0 {
		style := strconv.FormatUint(uint64(s.Style), 10)
		b.WriteString(" LineStyle='")
		b.WriteString(style)
		b.WriteString("' FillStyle='")
		b.WriteString(style)
		b.WriteString("' TextStyle='")
		b.WriteString(style)
		b.WriteByte('\'')
	}
	b.WriteByte('>')
	writeCell(&b, vsd.CellPinX, vsd.FormatInches(s.PinX))
	writeCell(&b, vsd.CellPinY, vsd.FormatInches(s.PinY))
	writeCell(&b, vsd.CellWidth, vsd.FormatInches(s.Width))
	writeCell(&b, vsd.CellHeight, vsd.FormatInches(s.Height))
	writeFormulaCell(&b, vsd.CellLocPinX, vsd.FormatInches(s.LocPinX), "Width*"+vsd.FormatInches(s.LocPinX/s.Width))
	writeFormulaCell(&b, vsd.CellLocPinY, vsd.FormatInches(s.LocPinY), "Height*"+vsd.FormatInches(s.LocPinY/s.Height))
	writeCell(&b, vsd.CellAngle, vsd.FormatInches(s.Angle))
	writeCell(&b, vsd.CellFlipX, flipBool(s.FlipX))
	writeCell(&b, vsd.CellFlipY, flipBool(s.FlipY))
	if s.FillHex != "" {
		writeCell(&b, vsd.CellFillForegnd, xmlscan.EscapeAttr(s.FillHex, '\''))
		writeCell(&b, vsd.CellFillPattern, one)
	}
	if s.LineHex != "" {
		writeCell(&b, vsd.CellLineColor, xmlscan.EscapeAttr(s.LineHex, '\''))
	}
	if s.LineWeight != 0 {
		writeCell(&b, vsd.CellLineWeight, vsd.FormatInches(s.LineWeight))
	}
	if isPath {
		for ix, p := range s.Paths {
			writePathGeometry(&b, s.Geom, p.Points, p.Closed, ix)
		}
		b.WriteString("</Shape>")
		return []byte(b.String()), nil
	}
	b.WriteString("<Section N='Connection'>")
	writeConnectionRow(&b, 0, s.Width*0.5, "Width*0.5", s.Height, "Height*1")
	writeConnectionRow(&b, 1, s.Width, "Width*1", s.Height*0.5, "Height*0.5")
	writeConnectionRow(&b, 2, s.Width*0.5, "Width*0.5", 0, "Height*0")
	writeConnectionRow(&b, 3, 0, "Width*0", s.Height*0.5, "Height*0.5")
	b.WriteString("</Section>")
	b.WriteString("<Section N='Geometry' IX='0'>")
	writeCell(&b, "NoFill", zero)
	writeCell(&b, "NoLine", zero)
	writeCell(&b, "NoShow", zero)
	writeCell(&b, "NoSnap", zero)
	writeCell(&b, "NoQuickDrag", zero)
	if s.Geom == "rect" {
		writePointRow(&b, "RelMoveTo", 1, zero, zero)
		writePointRow(&b, "RelLineTo", 2, one, zero)
		writePointRow(&b, "RelLineTo", 3, one, one)
		writePointRow(&b, "RelLineTo", 4, zero, one)
		writePointRow(&b, "RelLineTo", 5, zero, zero)
	} else {
		b.WriteString("<Row T='Ellipse' IX='1'>")
		writeFormulaCell(&b, "X", vsd.FormatInches(s.Width*0.5), "Width*0.5")
		writeFormulaCell(&b, "Y", vsd.FormatInches(s.Height*0.5), "Height*0.5")
		writeFormulaCell(&b, "A", vsd.FormatInches(s.Width), "Width*1")
		writeFormulaCell(&b, "B", vsd.FormatInches(s.Height*0.5), "Height*0.5")
		writeFormulaCell(&b, "C", vsd.FormatInches(s.Width*0.5), "Width*0.5")
		writeFormulaCell(&b, "D", vsd.FormatInches(s.Height), "Height*1")
		b.WriteString("</Row>")
	}
	b.WriteString("</Section></Shape>")
	return []byte(b.String()), nil
}

// zero and one are the stored boolean and unit-square values ("0", "1").
const (
	zero = "0"
	one  = "1"
)

// writeCell writes one value cell: <Cell N='name' V='value'/>.
func writeCell(b *strings.Builder, name, value string) {
	b.WriteString("<Cell N='")
	b.WriteString(name)
	b.WriteString("' V='")
	b.WriteString(value)
	b.WriteString("'/>")
}

// writeFormulaCell writes one cell with a formula:
// <Cell N='name' V='value' F='formula'/>.
func writeFormulaCell(b *strings.Builder, name, value, formula string) {
	b.WriteString("<Cell N='")
	b.WriteString(name)
	b.WriteString("' V='")
	b.WriteString(value)
	b.WriteString("' F='")
	b.WriteString(formula)
	b.WriteString("'/>")
}

// writeConnectionRow writes one Connection row at IX ix: the point at
// (xVal, yVal) with the Width*/Height* formulas xFormula and yFormula.
func writeConnectionRow(b *strings.Builder, ix int, xVal float64, xFormula string, yVal float64, yFormula string) {
	b.WriteString("<Row T='Connection' IX='")
	b.WriteString(strconv.Itoa(ix))
	b.WriteString("'>")
	writeFormulaCell(b, "X", vsd.FormatInches(xVal), xFormula)
	writeFormulaCell(b, "Y", vsd.FormatInches(yVal), yFormula)
	b.WriteString("</Row>")
}

// writePointRow writes one unit-square corner row: <Row T='typ' IX='ix'>
// with unit cells X xVal and Y yVal.
func writePointRow(b *strings.Builder, typ string, ix int, xVal, yVal string) {
	b.WriteString("<Row T='")
	b.WriteString(typ)
	b.WriteString("' IX='")
	b.WriteString(strconv.Itoa(ix))
	b.WriteString("'>")
	writeCell(b, "X", xVal)
	writeCell(b, "Y", yVal)
	b.WriteString("</Row>")
}

// flipBool writes a flip flag as Visio stores it ("0" or "1").
func flipBool(flip bool) string {
	if flip {
		return one
	}
	return zero
}
