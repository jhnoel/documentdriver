package model

import (
	"github.com/jhnoel/documentdriver/internal/vsdx/inherit"
	"github.com/jhnoel/documentdriver/internal/vsdx/place"
)

// fillObject derives one object's box or ends, fill, line, text and
// layers from its effective cells, each omitted with a reason when it
// cannot be derived (a non-numeric cell, a themed value with no cached
// colour, a skewed box). Chunk M2.
func fillObject(o *Object, r *inherit.Resolver, s *inherit.Shape, f place.Frame, pageH float64, opt Options) {
	_, _, _, _, _, _ = o, r, s, f, pageH, opt
}
