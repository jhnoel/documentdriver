package inherit

import "github.com/jhnoel/documentdriver/internal/ooxml/tree"

// Text returns the Text element s shows and its origin: its own, or its
// master shape's when it has none (format notes section 4). nil when
// neither has one. Chunk H3.
func (r *Resolver) Text(s *Shape) (*tree.Node, Origin) {
	_ = s
	return nil, ""
}
