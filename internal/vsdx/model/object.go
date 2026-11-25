package model

import (
	"github.com/jhnoel/documentdriver/internal/vsdx/inherit"
	"github.com/jhnoel/documentdriver/internal/vsdx/place"
)

// fillObject derives one object's values: geometry (box, ends, sites,
// the geom form and path),
// paint (fill, line), text (html, plain text) and shape data, each
// omitted with a reason when it cannot be derived.
func fillObject(o *Object, r *inherit.Resolver, s *inherit.Shape, f place.Frame, pageH float64, opt Options) {
	geometry(o, r, s, f, pageH, opt)
	pathOf(o, r, s, f, pageH)
	paint(o, r, s)
	textOf(o, r, s)
	if opt.Draw {
		drawingOf(o, r, s, f, pageH)
	}
	var propsOmitted []Omission
	o.Props, propsOmitted = readProps(o.ID, r, s)
	o.Omitted = append(o.Omitted, propsOmitted...)
}
