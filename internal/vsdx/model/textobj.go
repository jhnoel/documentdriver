package model

import (
	"github.com/jhnoel/documentdriver/internal/vsdx/inherit"
	"github.com/jhnoel/documentdriver/internal/vsdx/text"
)

// textOf sets HTML, Text and HasText from the Text the shape shows
// (inherit.Text), parsed by text.Parse, formatted by formats and
// rendered by text.Render, with the html origin. Chunk M2c.
func textOf(o *Object, r *inherit.Resolver, s *inherit.Shape) {
	if o == nil {
		return
	}
	o.HTML, o.Text, o.HasText = "", "", false
	delete(o.Origins, "html")
	node, origin := r.Text(s)
	if node == nil {
		return
	}
	body, _ := text.Parse(node)
	owner := s
	if origin == inherit.Master {
		if master, ok := r.MasterShape(s); ok {
			owner = master
		}
	}
	o.HTML = text.Render(body, formats(r, owner), o.ID)
	o.Text = text.Plain(body)
	o.HasText = true
	setOrigin(o, "html", origin)
}
