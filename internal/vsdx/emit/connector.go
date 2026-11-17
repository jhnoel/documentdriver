package emit

import (
	"strconv"
	"strings"

	"github.com/jhnoel/documentdriver/internal/vsdx/formula"
	"github.com/jhnoel/documentdriver/internal/vsdx/vsd"
)

// End is one end of a new connector: its point in the parent's space
// (inches, y up) and, when glued, the target shape's ID and connection
// point number (1-based, Connections.Xn). Only point glue is written:
// it is the master-less glued form the corpus holds (apache-poi
// 60973.vsdx); whole-shape glue needs Visio's routing and is refused by
// the op.
type End struct {
	X, Y   float64
	Glued  bool
	Target uint32
	Point  int
}

// ConnectorXML returns a master-less 1-D Shape element with ID id: the
// transform cells of formula.OneD with their formulas
// ((BeginX+EndX)/2, SQRT(...), ATAN2(...), Width*0.5), BeginX/Y and
// EndX/Y (a glued end with F="PAR(PNT(Sheet.N!Connections.Xn,
// Sheet.N!Connections.Yn))"), a Geometry section with NoFill 1, MoveTo
// (0,0) and LineTo (Width,0) (X with F="Width*1"), styles 3, and the
// Connect rows of its glued ends (FromPart 9 or 12, ToPart 100+n-1).
// ok is false when the ends coincide. Chunk K4.
func ConnectorXML(id uint32, begin, end End) (shape []byte, connects [][]byte, ok bool) {
	pinX, pinY, width, angle, oneDOK := formula.OneD(begin.X, begin.Y, end.X, end.Y)
	if !oneDOK {
		return nil, nil, false
	}
	var b strings.Builder
	b.Grow(1024)
	b.WriteString("<Shape ID='")
	b.WriteString(strconv.FormatUint(uint64(id), 10))
	b.WriteString("' Type='Shape' LineStyle='3' FillStyle='3' TextStyle='3'>")
	writeFormulaCell(&b, vsd.CellPinX, vsd.FormatInches(pinX), "(BeginX+EndX)/2")
	writeFormulaCell(&b, vsd.CellPinY, vsd.FormatInches(pinY), "(BeginY+EndY)/2")
	writeFormulaCell(&b, vsd.CellWidth, vsd.FormatInches(width), "SQRT((EndX-BeginX)^2+(EndY-BeginY)^2)")
	writeCell(&b, vsd.CellHeight, vsd.FormatInches(0))
	writeFormulaCell(&b, vsd.CellLocPinX, vsd.FormatInches(width*0.5), "Width*0.5")
	writeFormulaCell(&b, vsd.CellLocPinY, vsd.FormatInches(0), "Height*0.5")
	writeFormulaCell(&b, vsd.CellAngle, vsd.FormatInches(angle), "ATAN2(EndY-BeginY,EndX-BeginX)")
	writeCell(&b, vsd.CellFlipX, zero)
	writeCell(&b, vsd.CellFlipY, zero)
	writeEndCell(&b, vsd.CellBeginX, begin.X, begin)
	writeEndCell(&b, vsd.CellBeginY, begin.Y, begin)
	writeEndCell(&b, vsd.CellEndX, end.X, end)
	writeEndCell(&b, vsd.CellEndY, end.Y, end)
	b.WriteString("<Section N='Geometry' IX='0'>")
	writeCell(&b, "NoFill", one)
	writeCell(&b, "NoLine", zero)
	writeCell(&b, "NoShow", zero)
	writeCell(&b, "NoSnap", zero)
	writeCell(&b, "NoQuickDrag", zero)
	writePointRowFormula(&b, "MoveTo", 1, zero, "Width*0", zero)
	writePointRowFormula(&b, "LineTo", 2, vsd.FormatInches(width), "Width*1", zero)
	b.WriteString("</Section></Shape>")
	s := []byte(b.String())
	if begin.Glued {
		connects = append(connects, connectXML(id, vsd.FromCellBegin, 9, begin))
	}
	if end.Glued {
		connects = append(connects, connectXML(id, vsd.FromCellEnd, 12, end))
	}
	return s, connects, true
}

// writeEndCell writes one BeginX/Y or EndX/Y cell: the stored value with
// the PAR(PNT(...)) glue formula when e.Glued, the value alone otherwise.
func writeEndCell(b *strings.Builder, name string, value float64, e End) {
	if e.Glued {
		writeFormulaCell(b, name, vsd.FormatInches(value), glueFormula(e))
		return
	}
	writeCell(b, name, vsd.FormatInches(value))
}

// glueFormula returns the point-glue formula of one end:
// PAR(PNT(Sheet.Target!Connections.XPoint,Sheet.Target!Connections.YPoint)).
func glueFormula(e End) string {
	target := strconv.FormatUint(uint64(e.Target), 10)
	point := strconv.Itoa(e.Point)
	return "PAR(PNT(Sheet." + target + "!Connections.X" + point + ",Sheet." + target + "!Connections.Y" + point + "))"
}

// writePointRowFormula writes one Geometry row: <Row T='typ' IX='ix'>
// with X xVal under formula xFormula and a plain Y yVal.
func writePointRowFormula(b *strings.Builder, typ string, ix int, xVal, xFormula, yVal string) {
	b.WriteString("<Row T='")
	b.WriteString(typ)
	b.WriteString("' IX='")
	b.WriteString(strconv.Itoa(ix))
	b.WriteString("'>")
	writeFormulaCell(b, "X", xVal, xFormula)
	writeCell(b, "Y", yVal)
	b.WriteString("</Row>")
}

// connectXML returns one Connect row for a glued end: the connector
// sheet id, fromCell (BeginX or EndX) and fromPart (9 or 12), to the
// glued target sheet id, its Connections.Xn point and ToPart 100+n-1.
func connectXML(id uint32, fromCell string, fromPart int, e End) []byte {
	var b strings.Builder
	b.Grow(128)
	b.WriteString("<Connect FromSheet='")
	b.WriteString(strconv.FormatUint(uint64(id), 10))
	b.WriteString("' FromCell='")
	b.WriteString(fromCell)
	b.WriteString("' FromPart='")
	b.WriteString(strconv.Itoa(fromPart))
	b.WriteString("' ToSheet='")
	b.WriteString(strconv.FormatUint(uint64(e.Target), 10))
	b.WriteString("' ToCell='Connections.X")
	b.WriteString(strconv.Itoa(e.Point))
	b.WriteString("' ToPart='")
	b.WriteString(strconv.Itoa(99 + e.Point))
	b.WriteString("'/>")
	return []byte(b.String())
}
