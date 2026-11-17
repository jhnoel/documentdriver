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
)

// StyleCategory returns the category of a shape-level cell, or of a
// section's cells (Character, Paragraph, Tabs are text). The table
// is designer-owned with provenance (MS-VSDX StyleSheet_Type and the
// ShapeSheet reference); a cell not on it is NoStyle and never comes
// from a style. Chunk V1 fills the table from the design note.
func StyleCategory(section, cell string) Category {
	_, _ = section, cell
	return NoStyle
}
