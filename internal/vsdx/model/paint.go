package model

import "github.com/jhnoel/documentdriver/internal/vsdx/inherit"

// paint derives fill and line: FillPattern 0 is fill "none", otherwise
// FillForegnd through doc.Document.Color; LinePattern 0 is line "none",
// otherwise LineWeight in points, LineColor through Color and
// LinePattern as Dash. "Themed" and unresolvable colours omit the
// colour with the reason. Chunk M2b.
func paint(o *Object, r *inherit.Resolver, s *inherit.Shape) {
	_, _, _ = o, r, s
}
