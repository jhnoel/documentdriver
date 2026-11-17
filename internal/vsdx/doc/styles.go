package doc

import "github.com/jhnoel/documentdriver/internal/ooxml/tree"

// StyleSheet is one StyleSheet of document.xml: its ID, names, the
// parent style IDs of its three categories (absent parents are
// HasLine etc. false) and its node, whose cells and sections the
// inheritance resolver reads.
type StyleSheet struct {
	ID                        uint32
	Name, NameU               string
	LineStyle, FillStyle      uint32
	TextStyle                 uint32
	HasLine, HasFill, HasText bool
	Node                      *tree.Node
}

// readStyles reads document.xml's StyleSheets and FaceNames. A
// StyleSheet whose ID repeats is left out with a note. Chunk D3.
func (d *Document) readStyles(root *tree.Node) {
	_ = root
}

// readCustom sets Recalc from docProps/custom.xml's RecalcDocument
// property (found through the package relationships). Chunk D3.
func (d *Document) readCustom() {}
