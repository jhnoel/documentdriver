package model

import (
	"github.com/jhnoel/documentdriver/internal/vsdx/inherit"
	"github.com/jhnoel/documentdriver/internal/vsdx/place"
)

// geometry derives the box (2-D, and 1-D with Full), the ends (1-D) and
// the sites (with Sites or Full) from the effective transform cells and
// Connection rows, through place.Box, place.Point and place.Site, and
// sets the box and ends origins. A cell that is absent or not a number
// omits the property with its reason. Chunk M2a.
func geometry(o *Object, r *inherit.Resolver, s *inherit.Shape, f place.Frame, pageH float64, opt Options) {
	_, _, _, _, _, _ = o, r, s, f, pageH, opt
}

// transformOf reads a shape's effective transform cells; ok is false
// (with the missing or unreadable cell named) when one cannot be
// derived. Chunk M2a.
func transformOf(r *inherit.Resolver, s *inherit.Shape) (t place.Transform, missing string, ok bool) {
	_, _ = r, s
	return place.Transform{}, "", false
}
