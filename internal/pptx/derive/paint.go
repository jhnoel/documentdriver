package derive

import (
	"strconv"

	"github.com/jhnoel/documentdriver/internal/ooxml/tree"
)

// Notes collects what a read derived about one object besides its
// values: the origin of each property and the properties it had to omit,
// in the order found. The read model copies them onto its object; the
// property codecs read the values and ignore the notes.
type Notes struct {
	Omitted []Omit
	Origin  map[string]Origin
}

// Omit is one omitted property and the reason.
type Omit struct {
	Prop   string
	Reason string
}

// omit records an omitted property with its reason.
func (n *Notes) omit(prop, reason string) {
	if n != nil {
		n.Omitted = append(n.Omitted, Omit{prop, reason})
	}
}

// set records the origin of a property.
func (n *Notes) set(prop string, o Origin) {
	if n == nil {
		return
	}
	if n.Origin == nil {
		n.Origin = map[string]Origin{}
	}
	n.Origin[prop] = o
}

// unset removes the origin of a property.
func (n *Notes) unset(prop string) {
	if n != nil {
		delete(n.Origin, prop)
	}
}

// ShownColor is a colour as written plus its approximate displayed value.
type ShownColor struct {
	Color
	// Hex is the displayed RRGGBB ("" if it cannot be resolved), Alpha the
	// opacity (1 opaque).
	Hex   string
	Alpha float64
}

// Fill is a shape fill as the views show it.
type Fill struct {
	// Type is none, solid, or the element name of a fill shown as raw XML
	// (gradFill, blipFill, pattFill).
	Type  string
	Color *ShownColor
	// XML is the raw fill element for gradFill, blipFill and pattFill.
	XML string
}

// Line is a shape outline as the views show it. Nil fields were not
// derived.
type Line struct {
	W     *float64 // EMU
	None  bool     // a:noFill
	Color *ShownColor
	// FillXML is a non-solid line fill (gradFill, pattFill) as raw XML.
	FillXML    string
	Dash       string // preset dash, or "custom"
	Cap        string // rnd, sq, flat
	Head, Tail *Arrow
}

// Arrow is a line end.
type Arrow struct {
	Type string // none, triangle, stealth, diamond, oval, arrow
	W    string // sm, med, lg ("" if not written)
	Len  string
}

// ColorCtx resolves colours on one slide: the theme's colour scheme (nil
// when no theme is reachable), the colour map in force after the slide's
// and layout's overrides, and the theme's format scheme for style
// references.
type ColorCtx struct {
	Scheme Scheme
	Map    ClrMap
	Fmt    *tree.Node
}

// SlideColorCtx builds the colour context of a slide from its root, its
// layout's root, its master's root and its theme's root (any may be nil):
// the master's p:clrMap, replaced by the slide's p:clrMapOvr
// overrideClrMapping, or when the slide has none (and does not say
// masterClrMapping), by the layout's.
func SlideColorCtx(slide, layout, master, theme *tree.Node) ColorCtx {
	c := ColorCtx{Map: ClrMap{}}
	if master != nil {
		c.Map = ParseClrMap(Child(master, PML, "clrMap"))
	}
	for _, root := range []*tree.Node{slide, layout} {
		if root == nil {
			continue
		}
		if o := Path(root, PML, "clrMapOvr"); o != nil {
			if ov := Child(o, DML, "overrideClrMapping"); ov != nil {
				c.Map = ParseClrMap(ov)
				break
			}
			if root == slide && Child(o, DML, "masterClrMapping") != nil {
				break
			}
		}
	}
	if theme != nil {
		te := Child(theme, DML, "themeElements")
		c.Scheme = ParseScheme(Child(te, DML, "clrScheme"))
		c.Fmt = Child(te, DML, "fmtScheme")
	}
	return c
}

// ShowColor resolves a colour element for display. phClr is the style
// reference colour for schemeClr phClr. Failures are noted against prop.
func (c ColorCtx) ShowColor(prop string, el *tree.Node, phClr *Color, n *Notes) *ShownColor {
	col, ok := ParseColor(el)
	if !ok {
		n.omit(prop, "no colour element")
		return nil
	}
	out := &ShownColor{Color: col, Alpha: 1}
	if c.Scheme == nil {
		n.omit(prop+".hex", "no theme")
		return out
	}
	res, err := c.Scheme.Resolve(col, c.Map, phClr)
	if err != nil {
		n.omit(prop+".hex", err.Error())
		return out
	}
	out.Hex, out.Alpha = "#"+res.Hex, res.Alpha
	if col.Kind == "scheme" && col.Val == "phClr" && phClr != nil {
		// Show the reference colour with the style's modifiers appended,
		// which is the colour as written once phClr is substituted.
		sub := *phClr
		sub.Mods = append(append([]Mod(nil), phClr.Mods...), col.Mods...)
		out.Color = sub
	}
	return out
}

// FillNames are the local names of the fill choice.
var FillNames = map[string]bool{"noFill": true, "solidFill": true, "gradFill": true, "blipFill": true, "pattFill": true, "grpFill": true}

// FillElem returns the fill choice element among n's children.
func FillElem(n *tree.Node) *tree.Node {
	if n == nil {
		return nil
	}
	for c := range n.ElementsSeq() {
		if FillNames[c.Local()] && In(c, DML) {
			return c
		}
	}
	return nil
}

// FillFrom builds a fill from a fill choice element.
func (c ColorCtx) FillFrom(prop string, f *tree.Node, phClr *Color, n *Notes) *Fill {
	switch f.Local() {
	case "noFill":
		return &Fill{Type: "none"}
	case "solidFill":
		return &Fill{Type: "solid", Color: c.ShowColor(prop+".color", ColorElem(f), phClr, n)}
	}
	return &Fill{Type: f.Local(), XML: string(f.Bytes())}
}

// StyleEntry returns the theme format-scheme entry a style reference
// points to: list is fillStyleLst, lnStyleLst or effectStyleLst, and idx
// 1001+ selects bgFillStyleLst for fills.
func (c ColorCtx) StyleEntry(list string, idx int) *tree.Node {
	if c.Fmt == nil || idx <= 0 {
		return nil
	}
	if list == "fillStyleLst" && idx >= 1001 {
		list, idx = "bgFillStyleLst", idx-1000
	}
	l := Child(c.Fmt, DML, list)
	if l == nil {
		return nil
	}
	els := l.Elements()
	if idx > len(els) {
		return nil
	}
	return els[idx-1]
}

// RefIdx reads a style reference's idx.
func RefIdx(ref *tree.Node) (int, bool) {
	if ref == nil {
		return 0, false
	}
	s, ok := ref.Attr("idx")
	if !ok {
		return 0, false
	}
	i, err := strconv.Atoi(s)
	return i, err == nil
}

// ObjectStyle returns the p:style that applies to an object: its own, else
// the first in its placeholder chain.
func ObjectStyle(chain []Link) *tree.Node {
	if len(chain) == 0 {
		return nil
	}
	if s := Child(chain[0].Elem, PML, "style"); s != nil {
		return s
	}
	for _, l := range chain[1:] {
		if s := Child(l.Elem, PML, "style"); s != nil {
			return s
		}
	}
	return nil
}

// SpPrChain maps an object chain to the chain of its shape properties
// (p:spPr; nil where a link has none).
func SpPrChain(chain []Link) []Link {
	out := make([]Link, 0, len(chain))
	for _, l := range chain {
		out = append(out, Link{Elem: Child(l.Elem, PML, "spPr"), Origin: l.Origin})
	}
	return out
}

// ReadFill resolves a fill through the shape-property chain (the
// object's own spPr, then its placeholders'), then the style's fillRef.
// gfill is the enclosing group's fill for a:grpFill (nil if not derived);
// group is set for a group's own fill, which has no style. Origins and
// omissions are noted against "fill".
func (c ColorCtx) ReadFill(chain []Link, style *tree.Node, gfill *Fill, group bool, n *Notes) *Fill {
	for _, l := range chain {
		f := FillElem(l.Elem)
		if f == nil {
			continue
		}
		if f.Local() == "grpFill" {
			if gfill == nil {
				n.omit("fill", "a:grpFill but the enclosing group's fill is not derived")
				return nil
			}
			n.set("fill", FromGroup)
			cp := *gfill
			return &cp
		}
		n.set("fill", l.Origin)
		return c.FillFrom("fill", f, nil, n)
	}
	if group {
		return nil // a group without a fill has none to give its children
	}
	if ref := Child(style, DML, "fillRef"); ref != nil {
		idx, ok := RefIdx(ref)
		if ok {
			n.set("fill", FromStyle)
			if idx == 0 {
				return &Fill{Type: "none"}
			}
			ph, _ := ParseColor(ColorElem(ref))
			e := c.StyleEntry("fillStyleLst", idx)
			if e == nil {
				n.omit("fill", "style fillRef idx "+strconv.Itoa(idx)+" not in the theme's format scheme")
				n.unset("fill")
				return nil
			}
			return c.FillFrom("fill", e, &ph, n)
		}
	}
	n.omit("fill", "not specified on the object, its placeholders or a style")
	return nil
}

// lineSrc is one source of outline properties.
type lineSrc struct {
	ln     *tree.Node
	origin Origin
	ph     *Color
	none   bool // lnRef idx 0
}

// ReadLine resolves each outline property separately through the
// shape-property chain's a:ln elements, then the style's lnRef. Origins
// and omissions are noted against "line" and "line.*".
func (c ColorCtx) ReadLine(chain []Link, style *tree.Node, n *Notes) *Line {
	var srcs []lineSrc
	for _, l := range chain {
		if ln := Child(l.Elem, DML, "ln"); ln != nil {
			srcs = append(srcs, lineSrc{ln: ln, origin: l.Origin})
		}
	}
	if ref := Child(style, DML, "lnRef"); ref != nil {
		if idx, ok := RefIdx(ref); ok {
			ph, _ := ParseColor(ColorElem(ref))
			if idx == 0 {
				srcs = append(srcs, lineSrc{origin: FromStyle, none: true})
			} else if e := c.StyleEntry("lnStyleLst", idx); e != nil {
				srcs = append(srcs, lineSrc{ln: e, origin: FromStyle, ph: &ph})
			} else {
				n.omit("line", "style lnRef idx "+strconv.Itoa(idx)+" not in the theme's format scheme")
			}
		}
	}
	if len(srcs) == 0 {
		n.omit("line", "not specified on the object, its placeholders or a style")
		return nil
	}
	ln := &Line{}
	found := false
	// Fill (colour).
	for _, s := range srcs {
		if s.none {
			ln.None = true
			n.set("line.color", s.origin)
			found = true
			break
		}
		f := FillElem(s.ln)
		if f == nil {
			continue
		}
		n.set("line.color", s.origin)
		found = true
		switch f.Local() {
		case "noFill":
			ln.None = true
		case "solidFill":
			ln.Color = c.ShowColor("line.color", ColorElem(f), s.ph, n)
		default:
			ln.FillXML = string(f.Bytes())
		}
		break
	}
	if !found {
		n.omit("line.color", "not specified on the object, its placeholders or a style")
	}
	attr := func(prop, name string, set func(string)) {
		for _, s := range srcs {
			if s.ln == nil {
				continue
			}
			if v, ok := s.ln.Attr(name); ok {
				set(v)
				n.set(prop, s.origin)
				return
			}
		}
		if !ln.None {
			n.omit(prop, "not specified")
		}
	}
	attr("line.w", "w", func(v string) {
		i, err := strconv.ParseInt(v, 10, 64) // ST_LineWidth is an integer
		if err != nil {
			n.omit("line.w", "a:ln@w="+strconv.Quote(v)+" is not an integer")
			return
		}
		f := float64(i)
		ln.W = &f
	})
	attr("line.cap", "cap", func(v string) { ln.Cap = v })
	child := func(prop string, names []string, set func(*tree.Node)) {
		for _, s := range srcs {
			if s.ln == nil {
				continue
			}
			for _, nm := range names {
				if ch := Child(s.ln, DML, nm); ch != nil {
					set(ch)
					n.set(prop, s.origin)
					return
				}
			}
		}
	}
	child("line.dash", []string{"prstDash", "custDash"}, func(ch *tree.Node) {
		if ch.Local() == "custDash" {
			ln.Dash = "custom"
			return
		}
		ln.Dash, _ = ch.Attr("val")
	})
	for _, end := range []struct {
		prop, el string
		dst      **Arrow
	}{{"line.head", "headEnd", &ln.Head}, {"line.tail", "tailEnd", &ln.Tail}} {
		child(end.prop, []string{end.el}, func(ch *tree.Node) {
			a := &Arrow{}
			a.Type, _ = ch.Attr("type")
			a.W, _ = ch.Attr("w")
			a.Len, _ = ch.Attr("len")
			if a.Type == "" {
				a.Type = "none" // ST_LineEndType default
			}
			*end.dst = a
		})
	}
	return ln
}

// Geom is a preset geometry with its adjust values as stored, or a custom
// geometry.
type Geom struct {
	Preset string // "" for custGeom
	Adj    []Adjust
	// Custom is set for custGeom; Elem is then the a:custGeom element.
	Custom bool
	Elem   *tree.Node
}

// ReadGeom finds an object's geometry through its chain (the object, then
// its placeholders): the first a:prstGeom or a:custGeom in a p:spPr.
func ReadGeom(chain []Link) (Geom, Origin, bool) {
	for _, l := range chain {
		spPr := Child(l.Elem, PML, "spPr")
		if pg := Child(spPr, DML, "prstGeom"); pg != nil {
			g := Geom{Elem: pg}
			g.Preset, _ = pg.Attr("prst")
			g.Adj = ReadAdj(Child(pg, DML, "avLst"))
			return g, l.Origin, true
		}
		if cg := Child(spPr, DML, "custGeom"); cg != nil {
			return Geom{Custom: true, Elem: cg}, l.Origin, true
		}
	}
	return Geom{}, "", false
}

// ReadAdj reads a guide list (avLst, gdLst) as written.
func ReadAdj(av *tree.Node) []Adjust {
	if av == nil {
		return nil
	}
	var out []Adjust
	for gd := range av.ElementsSeq() {
		n, _ := gd.Attr("name")
		f, _ := gd.Attr("fmla")
		out = append(out, Adjust{Name: n, Fmla: f})
	}
	return out
}
