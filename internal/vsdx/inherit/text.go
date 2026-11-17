package inherit

import (
	"github.com/jhnoel/documentdriver/internal/ooxml/tree"
	"github.com/jhnoel/documentdriver/internal/vsdx/vsd"
)

// Text returns the Text element s shows and its origin: its own, or its
// master shape's when it has none (format notes section 4). nil when
// neither has one. Chunk H3.
func (r *Resolver) Text(s *Shape) (*tree.Node, Origin) {
	if s == nil {
		return nil, ""
	}
	if node := textChild(s); node != nil {
		return node, Local
	}
	if r == nil {
		return nil, ""
	}
	if master, ok := r.MasterShape(s); ok {
		if node := textChild(master); node != nil {
			return node, Master
		}
	}
	return nil, ""
}

// textChild returns the direct Text child of the shape's node, or nil
// when the shape has no sheet, no node or no Text child. Chunk H3.
func textChild(s *Shape) *tree.Node {
	if s == nil || s.Sheet == nil || s.Sheet.Node == nil {
		return nil
	}
	for child := range s.Sheet.Node.ElementsSeq() {
		if child.Local() == vsd.ElemText {
			return child
		}
	}
	return nil
}
