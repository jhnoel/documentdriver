package vsd

// Element local names of the Visio parts (MS-VSDX section 2.3; format
// notes sections 2 to 8).
const (
	ElemVisioDocument   = "VisioDocument"
	ElemPages           = "Pages"
	ElemPage            = "Page"
	ElemPageSheet       = "PageSheet"
	ElemMasters         = "Masters"
	ElemMaster          = "Master"
	ElemPageContents    = "PageContents"
	ElemMasterContents  = "MasterContents"
	ElemShapes          = "Shapes"
	ElemShape           = "Shape"
	ElemCell            = "Cell"
	ElemSection         = "Section"
	ElemRow             = "Row"
	ElemText            = "Text"
	ElemConnects        = "Connects"
	ElemConnect         = "Connect"
	ElemStyleSheets     = "StyleSheets"
	ElemStyleSheet      = "StyleSheet"
	ElemFaceNames       = "FaceNames"
	ElemForeignData     = "ForeignData"
	ElemRel             = "Rel"
	ElemCharMark        = "cp"
	ElemParaMark        = "pp"
	ElemTabMark         = "tp"
	ElemField           = "fld"
	FormulaInherited    = "Inh"
	FormulaNone         = "No Formula"
	ValueThemed         = "Themed"
	ShapeTypeGroup      = "Group"
	ShapeTypeForeign    = "Foreign"
	ShapeTypeGuide      = "Guide"
	SectionProperty     = "Property"
	SectionCharacter    = "Character"
	SectionParagraph    = "Paragraph"
	SectionConnection   = "Connection"
	SectionLayer        = "Layer"
	SectionGeometry     = "Geometry"
	SectionField        = "Field"
	ToCellPin           = "PinX"
	ToCellConnectionPfx = "Connections.X"
	FromCellBegin       = "BeginX"
	FromCellEnd         = "EndX"
)

// Shape-level cell names the reader shows (format notes section 3).
const (
	CellPinX        = "PinX"
	CellPinY        = "PinY"
	CellWidth       = "Width"
	CellHeight      = "Height"
	CellLocPinX     = "LocPinX"
	CellLocPinY     = "LocPinY"
	CellAngle       = "Angle"
	CellFlipX       = "FlipX"
	CellFlipY       = "FlipY"
	CellBeginX      = "BeginX"
	CellBeginY      = "BeginY"
	CellEndX        = "EndX"
	CellEndY        = "EndY"
	CellPageWidth   = "PageWidth"
	CellPageHeight  = "PageHeight"
	CellOrientation = "PrintPageOrientation"
	CellLayerMember = "LayerMember"
	CellFillForegnd = "FillForegnd"
	CellFillPattern = "FillPattern"
	CellLineColor   = "LineColor"
	CellLineWeight  = "LineWeight"
	CellLinePattern = "LinePattern"

	CellBeginArrow       = "BeginArrow"
	CellEndArrow         = "EndArrow"
	CellBeginArrowSize   = "BeginArrowSize"
	CellEndArrowSize     = "EndArrowSize"
	CellShdwPattern      = "ShdwPattern"
	CellShdwForegnd      = "ShdwForegnd"
	CellShdwForegndTrans = "ShdwForegndTrans"
	CellShapeShdwType    = "ShapeShdwType"
	CellShapeShdwOffsetX = "ShapeShdwOffsetX"
	CellShapeShdwOffsetY = "ShapeShdwOffsetY"
	CellShapeShdwBlur    = "ShapeShdwBlur"
	CellShapeShdwOblique = "ShapeShdwObliqueAngle"
	CellShapeShdwScale   = "ShapeShdwScaleFactor"
)

// ArrowNames maps the stored arrow indexes the contract names
// (docs/contract.md §3, Line ends) to their names; ArrowIndex is the
// reverse. An index not in the table is shown as its stored string.
var (
	ArrowNames = map[string]string{"0": "none", "4": "triangle", "5": "stealth"}
	ArrowIndex = map[string]string{"none": "0", "triangle": "4", "stealth": "5"}
)
