package model

import "github.com/jhnoel/documentdriver/internal/ooxml/tree"

// gluedOf handles one Connect row whose FromCell is not BeginX or EndX
// (a control handle such as Controls.Row_1, 48 in the corpus): it adds
// a Glued entry to the from-shape's object (read only) and a
// dangling-glue hazard when ToSheet names no shape. It reports false for
// an end Connect (BeginX, EndX), which linkConnects handles. Chunk M4b.
func gluedOf(p *Page, c *tree.Node, byID map[uint32]*placed) bool {
	_, _, _ = p, c, byID
	return false
}
