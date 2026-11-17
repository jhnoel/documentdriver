package model

import (
	"github.com/jhnoel/documentdriver/internal/vsdx/inherit"
	"github.com/jhnoel/documentdriver/internal/vsdx/place"
)

// fillObject derives one object's values: geometry (box, ends, sites),
// paint (fill, line), text (html, plain text) and shape data, each
// omitted with a reason when it cannot be derived.
func fillObject(o *Object, r *inherit.Resolver, s *inherit.Shape, f place.Frame, pageH float64, opt Options) {
	geometry(o, r, s, f, pageH, opt)
	paint(o, r, s)
	textOf(o, r, s)
	var propsOmitted []Omission
	o.Props, propsOmitted = readProps(o.ID, r, s)
	o.Omitted = append(o.Omitted, propsOmitted...)
}
