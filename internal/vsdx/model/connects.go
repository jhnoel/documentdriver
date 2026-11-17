package model

import (
	"github.com/jhnoel/documentdriver/internal/ooxml/tree"
	"github.com/jhnoel/documentdriver/internal/vsdx/place"
)

// placed is what the walk (M1) keeps of each shape for the glue pass:
// its object, the frame it sits in, its transform (ok false when not
// derived) and its sites.
type placed struct {
	obj   *Object
	frame place.Frame
	t     place.Transform
	hasT  bool
	sites []Site
}

// linkConnects reads the page's Connects element (nil when none) into
// p.Connections, each connector's FromGlue and ToGlue, and each other
// shape's Glued (non-end FromCells), and adds the hazards:
// dangling-glue for a Connect naming a missing shape, stale-connector
// for a glued end farther than place.GlueTolerance (math.Hypot in view
// points) from its site (point glue) or outside its target's box
// (whole-shape glue). A Connect naming no FromSheet is a note. Chunk M4.
func linkConnects(p *Page, connects *tree.Node, byID map[uint32]*placed, pageH float64) {
	_ = pageH
	if connects != nil && gluedOf(p, connects, byID) {
		return
	}
}
