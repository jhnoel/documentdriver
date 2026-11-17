package vsd

// Category is which style attribute of a shape governs a cell when the
// shape and its master leave it out (format notes section 2,
// inheritance step 3).
type Category uint8

// Style categories.
const (
	NoStyle Category = iota
	LineCategory
	FillCategory
	TextCategory
	// PageCategory is a page-sheet cell a style sheet may hold
	// (PrintPageOrientation: 55 of 57 corpus documents store it only in
	// StyleSheet 0). Which style attribute governs it is not confirmed;
	// the resolver tries the line, fill and text chains in that order
	// (every corpus page sheet uses style 0 for all three).
	PageCategory
)

// StyleCategory returns the category of a shape-level cell, or of a
// section's cells (Character, Paragraph, Tabs are text). The table
// is designer-owned with provenance (MS-VSDX StyleSheet_Type and the
// ShapeSheet reference); a cell not on it is NoStyle and never comes
// from a style. Chunk V1 fills the table from the design note.
func StyleCategory(section, cell string) Category {
	switch section {
	case SectionCharacter, SectionParagraph, "Tabs":
		return TextCategory
	case "":
	default:
		return NoStyle
	}
	switch cell {
	case "LineWeight", "LineColor", "LinePattern", "Rounding", "EndArrowSize",
		"BeginArrow", "EndArrow", "LineCap", "BeginArrowSize", "LineColorTrans",
		"CompoundType":
		return LineCategory
	case "FillForegnd", "FillBkgnd", "FillPattern", "ShdwForegnd", "ShdwBkgnd",
		"ShdwPattern", "FillForegndTrans", "FillBkgndTrans", "ShdwForegndTrans",
		"ShdwBkgndTrans", "ShapeShdwType", "ShapeShdwOffsetX", "ShapeShdwOffsetY",
		"ShapeShdwObliqueAngle", "ShapeShdwScaleFactor", "ShapeShdwBlur",
		"ShapeShdwShow":
		return FillCategory
	case "LeftMargin", "RightMargin", "TopMargin", "BottomMargin", "VerticalAlign",
		"TextBkgnd", "DefaultTabStop", "TextDirection", "TextBkgndTrans":
		return TextCategory
	case CellOrientation:
		return PageCategory
	}
	return NoStyle
}
