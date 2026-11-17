package model

import "github.com/jhnoel/documentdriver/internal/vsdx/inherit"

// textOf sets HTML, Text and HasText from the Text the shape shows
// (inherit.Text), parsed by text.Parse, formatted by formats and
// rendered by text.Render, with the html origin. Chunk M2c.
func textOf(o *Object, r *inherit.Resolver, s *inherit.Shape) {
	_, _, _ = o, r, s
	_ = formats
}
