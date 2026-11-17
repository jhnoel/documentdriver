package model

// linkConnects reads the page's Connects into p.Connections and each
// connector's FromGlue and ToGlue, and adds the hazards: dangling-glue
// for a Connect naming a missing shape, stale-connector for a glued end
// farther than place.GlueTolerance from its connection site (point
// glue) or outside its target's box (whole-shape glue). Chunk M4.
func linkConnects(p *Page, byID map[uint32]*Object) {
	_, _ = p, byID
}
