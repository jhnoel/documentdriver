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
	CellFillBkgnd   = "FillBkgnd"
	// CellFillBkgndTrans is the background's transparency, a fraction
	// from 0 (opaque) to 1 (clear).
	CellFillBkgndTrans = "FillBkgndTrans"
	CellLineColor      = "LineColor"
	CellLineWeight     = "LineWeight"
	CellLinePattern    = "LinePattern"

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

// Text block and text layout cells the drawing view reads
// (model.Options.Draw) and the text_block field writes: the text
// block's transform in the shape's local space, its margins, its
// vertical alignment, its background colour and transparency, the
// Character and Paragraph cells of a row, and a Geometry section's
// flags.
const (
	CellTxtPinX        = "TxtPinX"
	CellTxtPinY        = "TxtPinY"
	CellTxtWidth       = "TxtWidth"
	CellTxtHeight      = "TxtHeight"
	CellTxtLocPinX     = "TxtLocPinX"
	CellTxtLocPinY     = "TxtLocPinY"
	CellTxtAngle       = "TxtAngle"
	CellLeftMargin     = "LeftMargin"
	CellRightMargin    = "RightMargin"
	CellTopMargin      = "TopMargin"
	CellBottomMargin   = "BottomMargin"
	CellVerticalAlign  = "VerticalAlign"
	CellTextBkgnd      = "TextBkgnd"
	CellTextBkgndTrans = "TextBkgndTrans"
	CellCharFont       = "Font"
	CellCharSize       = "Size"
	CellCharColor      = "Color"
	CellCharStyle      = "Style"
	CellCharStrike     = "Strikethru"
	CellParaHorzAlign  = "HorzAlign"
	CellParaBullet     = "Bullet"
	CellParaBulletStr  = "BulletStr"
	CellParaSpLine     = "SpLine"
	CellGeomNoFill     = "NoFill"
	CellGeomNoLine     = "NoLine"
	CellGeomNoShow     = "NoShow"
)

// ArrowNames maps the stored arrow indexes the contract names
// (docs/contract.md §3, Line ends) to their names; ArrowIndex is the
// reverse. An index not in the table is shown as its stored string.
var (
	ArrowNames = map[string]string{"0": "none", "4": "triangle", "5": "stealth"}
	ArrowIndex = map[string]string{"none": "0", "triangle": "4", "stealth": "5"}
)

// FillPatternNames maps the stored FillPattern numbers the contract
// names (docs/contract.md §3, Pattern fills) to their names;
// FillPatternIndex is the reverse. A pattern from FillPatternMin to
// FillPatternMax not in the table is shown as its stored number. The
// numbers are the Fill dialog's pattern list in order (0 transparent,
// 1 solid, 2 "Dark upward diagonal", ...), and LibreOffice's import
// (libvisio) draws each as the hatch the name says, except dots (8)
// and cross_hatch_fine (24), which it draws as a dense triple hatch.
// That Visio draws them so is Visio unconfirmed (VE19).
var (
	FillPatternNames = map[string]string{
		"2": "hatch_up", "3": "grid", "4": "cross_hatch", "5": "hatch_down",
		"6": "horizontal", "7": "vertical", "8": "dots",
		"19": "horizontal_fine", "20": "vertical_fine", "21": "hatch_down_fine",
		"22": "hatch_up_fine", "23": "grid_fine", "24": "cross_hatch_fine",
	}
	FillPatternIndex = map[string]string{
		"hatch_up": "2", "grid": "3", "cross_hatch": "4", "hatch_down": "5",
		"horizontal": "6", "vertical": "7", "dots": "8",
		"horizontal_fine": "19", "vertical_fine": "20", "hatch_down_fine": "21",
		"hatch_up_fine": "22", "grid_fine": "23", "cross_hatch_fine": "24",
	}
)

// FillPatternMin and FillPatternMax bound the stored FillPattern
// numbers that are patterns (MS-VSDX FillPattern cell: 0 none, 1
// solid, 2 to 24 patterns, 25 to 40 gradients).
const (
	FillPatternMin = 2
	FillPatternMax = 24
)
