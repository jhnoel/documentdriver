// Package emit writes the XML of the things the writes create: a
// master-less 2-D shape (rect, ellipse), a master-less straight
// connector with its Connect rows, a Text element with its Character
// and Paragraph rows, a page entry and an empty page. Every function is
// pure (values in, bytes out) and escapes through xmlscan; the forms
// are those of the Visio-authored corpus files (docs/vsdx-design.md,
// Create). The writes insert the bytes through the edit layer; create's
// template uses the page forms.
package emit

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
}

// ShapeXML returns the Shape element: transform cells (LocPinX and
// LocPinY with formulas Width*k, Height*k and their values), fill and
// line cells when set, a Connection section of four points (X1 top
// centre, X2 right middle, X3 bottom centre, X4 left middle, with
// Width*/Height* formulas), and one Geometry section: rect as
// RelMoveTo (0,0), RelLineTo (1,0) (1,1) (0,1) (0,0), ellipse as one
// Ellipse row (centre W/2,H/2, A,B the right point, C,D the top point,
// with formulas). Numbers through vsd.FormatInches. Chunk K3.
func ShapeXML(s Shape) ([]byte, error) {
	_ = s
	return nil, nil
}
