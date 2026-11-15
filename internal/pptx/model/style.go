package model

import (
	"maps"
	"slices"
	"strconv"
	"strings"

	"github.com/jhnoel/documentdriver/internal/ooxml/tree"
	"github.com/jhnoel/documentdriver/internal/pptx/derive"
)

// merge copies a derive read's notes onto the object, in order.
func (o *Object) merge(n *derive.Notes) {
	for _, x := range n.Omitted {
		o.omit(x.Prop, x.Reason)
	}
	for _, k := range slices.Sorted(maps.Keys(n.Origin)) {
		o.setOrigin(k, n.Origin[k])
	}
}

// color resolves a colour element (derive.ColorCtx.ShowColor, the read
// half the property codecs share). Failures are recorded against prop.
func (r *reader) color(o *Object, prop string, el *tree.Node, phClr *derive.Color) *Color {
	var n derive.Notes
	c := r.cc.ShowColor(prop, el, phClr, &n)
	o.merge(&n)
	return c
}

// fill resolves an object's fill (derive.ColorCtx.ReadFill, shared with
// the fill codec). chain holds spPr elements.
func (r *reader) fill(o *Object, chain []link, style *tree.Node, gfill *Fill, group bool) *Fill {
	var n derive.Notes
	f := r.cc.ReadFill(links(chain), style, gfill, group, &n)
	o.merge(&n)
	return f
}

// line resolves each outline property separately (derive.ColorCtx.ReadLine,
// shared with the line codecs).
func (r *reader) line(o *Object, chain []link, style *tree.Node) *Line {
	var n derive.Notes
	l := r.cc.ReadLine(links(chain), style, &n)
	o.merge(&n)
	return l
}

// links maps the reader chain to derive links.
func links(chain []link) []derive.Link {
	out := make([]derive.Link, len(chain))
	for i, l := range chain {
		out[i] = derive.Link{Elem: l.sp, Origin: l.origin}
	}
	return out
}

// ColorString is a compact form of a colour shared by both views and the
// full-detail properties: "#RRGGBB" for sRGB, else the reference and its
// modifiers followed by the displayed hex, as in "accent1 lumMod=75000
// #2F5597" or "sys:windowText #000000". An unresolved hex is "#?".
func ColorString(c *Color) string {
	if c == nil {
		return ""
	}
	var parts []string
	switch c.Kind {
	case "srgb":
		if len(c.Mods) == 0 {
			return "#" + c.Val
		}
		parts = append(parts, "srgb:"+c.Val)
	case "scheme":
		parts = append(parts, c.Val)
	case "hsl", "scrgb":
		parts = append(parts, c.Kind+":"+strconv.FormatInt(c.Comp[0], 10)+","+strconv.FormatInt(c.Comp[1], 10)+","+strconv.FormatInt(c.Comp[2], 10))
	default:
		parts = append(parts, c.Kind+":"+c.Val)
	}
	for _, m := range c.Mods {
		if m.Val == "" {
			parts = append(parts, m.Name)
		} else {
			parts = append(parts, m.Name+"="+m.Val)
		}
	}
	h := c.Hex
	if h == "" {
		h = "#?"
	}
	parts = append(parts, h)
	return strings.Join(parts, " ")
}
