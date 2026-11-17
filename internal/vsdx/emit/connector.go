package emit

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
	_, _, _ = id, begin, end
	return nil, nil, false
}
