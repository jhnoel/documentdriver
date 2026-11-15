package prop

import (
	"errors"
	"fmt"
	"slices"
	"strconv"

	"github.com/jhnoel/documentdriver/internal/ooxml/tree"
	"github.com/jhnoel/documentdriver/internal/pptx/derive"
	"github.com/jhnoel/documentdriver/internal/pptx/ops/edit"
	"github.com/jhnoel/documentdriver/internal/pptx/show"
)

// ErrNoProperty is returned when an object kind has no such property.
var ErrNoProperty = errors.New("prop: the object has no such property")

// lastOmission returns the reason of the last omission of prop in n.
func lastOmission(n *derive.Notes, prop, def string) string {
	for _, v := range slices.Backward(n.Omitted) {
		if v.Prop == prop {
			return v.Reason
		}
	}
	return def
}

// propsPath returns the path of branch i's shape properties: p:spPr, or
// p:grpSpPr for a group.
func propsPath(h *edit.Handle, i int) edit.Path {
	if h.Elem(i).Local() == "grpSp" {
		return edit.Path{edit.P("grpSpPr")}
	}
	return edit.Path{edit.P("spPr")}
}

// Fill returns the fill codec of shapes and groups. Its read half is
// derive.ColorCtx.ReadFill over the inheritance chain, the style and the
// enclosing groups' fills, as in the read model. A write stores the fill
// on the object itself (a local override; p:style is kept): a:noFill, or
// a:solidFill with the colour as given (scheme references stay
// references). Gradient, pattern and picture fills are shown but not
// writable: replacing one takes an explicit "none" or solid fill.
func Fill() Codec[derive.Fill] {
	return Codec[derive.Fill]{
		Name:  "fill",
		Read:  readFill,
		Write: writeFill,
		Show:  func(f derive.Fill) string { return string(show.Fill(&f, false)) },
	}
}

// readFill reads branch i's fill through its inheritance chain and enclosing groups.
func readFill(h *edit.Handle, i int) derive.Val[derive.Fill] {
	kind := h.Elem(i).Local()
	if kind != "sp" && kind != "grpSp" && kind != "cxnSp" {
		return derive.Missing[derive.Fill]("a " + kind + " has no fill")
	}
	cc, err := h.ColorCtx()
	if err != nil {
		return derive.Missing[derive.Fill](err.Error())
	}
	if kind == "cxnSp" {
		// A connector's own fill only, as in the read model: connector
		// paths are never filled, so a style's fill reference shows
		// nothing and no fill is no value.
		spPr := derive.Child(h.Elem(i), derive.PML, "spPr")
		if derive.FillElem(spPr) == nil {
			return derive.Missing[derive.Fill]("the connector has no fill of its own")
		}
		var n derive.Notes
		f := cc.ReadFill([]derive.Link{{Elem: spPr, Origin: derive.FromSlide}}, nil, nil, false, &n)
		if f == nil {
			return derive.Missing[derive.Fill](lastOmission(&n, "fill", "no fill"))
		}
		return derive.Got(*f, n.Origin["fill"])
	}
	// The enclosing groups' fills, outermost first, for a:grpFill.
	var gfill *derive.Fill
	groups := h.Groups(i)
	for _, group := range slices.Backward(groups) {
		gfill = cc.ReadFill([]derive.Link{{Elem: derive.Child(group, derive.PML, "grpSpPr"), Origin: derive.FromSlide}}, nil, gfill, true, nil)
	}
	var n derive.Notes
	var f *derive.Fill
	if kind == "grpSp" {
		f = cc.ReadFill([]derive.Link{{Elem: derive.Child(h.Elem(i), derive.PML, "grpSpPr"), Origin: derive.FromSlide}}, nil, gfill, true, &n)
	} else {
		chain, err := h.Chain(i)
		if err != nil {
			return derive.Missing[derive.Fill](err.Error())
		}
		f = cc.ReadFill(derive.SpPrChain(chain), derive.ObjectStyle(chain), gfill, false, &n)
	}
	if f == nil {
		return derive.Missing[derive.Fill](lastOmission(&n, "fill", "no fill"))
	}
	return derive.Got(*f, n.Origin["fill"])
}

// writeFill writes the wanted none or solid fill onto the object.
func writeFill(h *edit.Handle, f derive.Fill) error {
	kind := h.Kind()
	switch {
	case kind == "cxnSp" && f.Type != "none":
		// Connectors draw no fill (design: connectors have no fill); the
		// a:noFill PowerPoint and pdraw write on them is settable.
		return fmt.Errorf("%w: a connector's fill is never drawn; only \"none\" can be set", ErrNoProperty)
	case kind != "sp" && kind != "grpSp" && kind != "cxnSp":
		return fmt.Errorf("%w: a %s has no fill", ErrNoProperty, kind)
	}
	paths := make([]edit.Path, h.Branches())
	for i := range paths {
		if h.Elem(i).Local() != kind {
			return fmt.Errorf("%w: branches of different kinds", edit.ErrBranch)
		}
		paths[i] = propsPath(h, i)
	}
	pp := paths[0]
	switch f.Type {
	case "none":
		return h.Ensure(pp, edit.A("noFill"))
	case "solid":
		if f.Color == nil {
			return errors.New("a solid fill needs a colour")
		}
		if err := h.Ensure(pp, edit.A("solidFill")); err != nil {
			return err
		}
		return writeColor(h, append(append(edit.Path{}, pp...), edit.A("solidFill")), f.Color.Color)
	}
	return fmt.Errorf("%w: %s fills are shown as raw XML and not editable; write \"none\" or a solid colour to replace one", ErrReadOnly, f.Type)
}

// colorElems maps colour kinds to their elements: all six DrawingML
// colour types.
var colorElems = map[string]string{
	"scheme": "schemeClr", "srgb": "srgbClr", "sys": "sysClr",
	"prst": "prstClr", "hsl": "hslClr", "scrgb": "scrgbClr",
}

// colorAttrs returns the attributes colour c is written with and those
// its element must not keep: val (with a system colour's lastClr, the
// displayed value PowerPoint caches there, removed when c has none), or
// the hsl and scRGB components.
func colorAttrs(c derive.Color) (set [][2]string, unset []string) {
	n := func(v int64) string { return strconv.FormatInt(v, 10) }
	switch c.Kind {
	case "hsl":
		return [][2]string{{"hue", n(c.Comp[0])}, {"sat", n(c.Comp[1])}, {"lum", n(c.Comp[2])}}, nil
	case "scrgb":
		return [][2]string{{"r", n(c.Comp[0])}, {"g", n(c.Comp[1])}, {"b", n(c.Comp[2])}}, nil
	case "sys":
		if c.LastClr != "" {
			return [][2]string{{"val", c.Val}, {"lastClr", c.LastClr}}, nil
		}
		return [][2]string{{"val", c.Val}}, []string{"lastClr"}
	}
	return [][2]string{{"val", c.Val}}, nil
}

// writeColor makes the element at parent hold colour c: the colour
// element of c's kind (replacing another kind; one of the same kind is
// kept, so the file's reference type stays unless c changes it) with its
// value, and its modifiers exactly as given, in order. Modifiers already
// as given are left alone. The six colour types are handled alike; their
// modifiers are placed by each type's generated child order.
func writeColor(h *edit.Handle, parent edit.Path, c derive.Color) error {
	name, ok := colorElems[c.Kind]
	if !ok {
		return fmt.Errorf("%w: %s colours", ErrReadOnly, c.Kind)
	}
	if err := edit.CheckSlideColor(c); err != nil {
		return err
	}
	if err := h.Ensure(parent, edit.A(name)); err != nil {
		return err
	}
	el := append(append(edit.Path{}, parent...), edit.A(name))
	set, unset := colorAttrs(c)
	for _, a := range set {
		if err := h.SetAttr(el, a[0], a[1]); err != nil {
			return err
		}
	}
	for _, a := range unset {
		if err := h.RemoveAttr(el, a); err != nil {
			return err
		}
	}
	for i := range h.Branches() {
		cur, _ := derive.ParseColor(h.FindIn(i, el))
		if slices.Equal(cur.Mods, c.Mods) {
			continue
		}
		if err := h.RemoveChildren(el, func(*tree.Node) bool { return true }); err != nil {
			return err
		}
		for _, m := range c.Mods {
			var attrs [][2]string
			if m.Val != "" {
				attrs = [][2]string{{"val", m.Val}}
			}
			if err := h.AppendEach(el, edit.A(m.Name), attrs); err != nil {
				return err
			}
		}
		break
	}
	return nil
}

// ShowColor completes a colour given in a plan with the displayed hex the
// views would show for it on the handle's slide ("" when the slide has no
// theme or the colour does not resolve, as on read).
func ShowColor(h *edit.Handle, c derive.Color) (derive.ShownColor, error) {
	out := derive.ShownColor{Color: c, Alpha: 1}
	cc, err := h.ColorCtx()
	if err != nil {
		return out, err
	}
	if cc.Scheme == nil {
		return out, nil
	}
	if res, err := cc.Scheme.Resolve(c, cc.Map, nil); err == nil {
		out.Hex, out.Alpha = "#"+res.Hex, res.Alpha
	}
	return out, nil
}

// Line returns the outline codec of shapes, connectors and pictures,
// shown in units u. Its read half is derive.ColorCtx.ReadLine (each
// property through the object's a:ln, its placeholders' and the style's
// lnRef), as in the read model. A write compares property by property
// and writes only those that differ from what is shown, as local
// overrides in the object's own a:ln; p:style is kept, so properties not
// written still come from the style.
func Line(u Units) Codec[derive.Line] {
	num := func(emu float64) string { return u.Length(emu) }
	return Codec[derive.Line]{
		Name:  "line",
		Read:  readLine,
		Write: func(h *edit.Handle, l derive.Line) error { return writeLine(h, l, num) },
		Show:  func(l derive.Line) string { return string(show.Line(&l, num, false)) },
	}
}

// readLine reads branch i's outline through its inheritance chain and style.
func readLine(h *edit.Handle, i int) derive.Val[derive.Line] {
	switch kind := h.Elem(i).Local(); kind {
	case "sp", "cxnSp", "pic":
	default:
		return derive.Missing[derive.Line]("a " + kind + " has no outline")
	}
	cc, err := h.ColorCtx()
	if err != nil {
		return derive.Missing[derive.Line](err.Error())
	}
	chain, err := h.Chain(i)
	if err != nil {
		return derive.Missing[derive.Line](err.Error())
	}
	var n derive.Notes
	l := cc.ReadLine(derive.SpPrChain(chain), derive.ObjectStyle(chain), &n)
	if l == nil {
		return derive.Missing[derive.Line](lastOmission(&n, "line", "no outline"))
	}
	return derive.Got(*l, n.Origin["line.color"])
}

var (
	pathLn = edit.Path{edit.P("spPr"), edit.A("ln")}
)

// lnChild returns the path to the named child of a:ln.
func lnChild(local string) edit.Path {
	return append(append(edit.Path{}, pathLn...), edit.A(local))
}

// colorShow shows a line's colour choice in one string.
func colorShow(l derive.Line) string {
	switch {
	case l.None:
		return "none"
	case l.Color != nil:
		return string(show.Color(l.Color))
	case l.FillXML != "":
		return "type=" + show.FillType(l.FillXML)
	}
	return ""
}

// writeLine writes the wanted outline properties onto the object.
func writeLine(h *edit.Handle, want derive.Line, num func(float64) string) error {
	switch h.Kind() {
	case "sp", "cxnSp", "pic":
	default:
		return fmt.Errorf("%w: a %s has no outline", ErrNoProperty, h.Kind())
	}
	var cur derive.Line
	if v, ok := readLine(h, 0).Get(); ok {
		cur = v
	}
	if err := h.Ensure(pathSpPr, edit.A("ln")); err != nil {
		return err
	}
	if colorShow(want) != colorShow(cur) {
		switch {
		case want.None:
			if err := h.Ensure(pathLn, edit.A("noFill")); err != nil {
				return err
			}
		case want.Color != nil:
			if err := h.Ensure(pathLn, edit.A("solidFill")); err != nil {
				return err
			}
			if err := writeColor(h, lnChild("solidFill"), want.Color.Color); err != nil {
				return err
			}
		case want.FillXML != "":
			return fmt.Errorf("%w: %s line fills are shown as raw XML and not editable", ErrReadOnly, show.FillType(want.FillXML))
		default:
			return errors.New("line colour: nothing to write")
		}
	}
	if want.None {
		return nil // nothing else of a line without a fill is shown
	}
	if want.W != nil && (cur.W == nil || num(*cur.W) != num(*want.W)) {
		w := strconv.FormatInt(int64(derive.RoundEMU(*want.W)), 10)
		if err := edit.CheckAttr("a:CT_LineProperties", "w", w); err != nil {
			return fmt.Errorf("line width %s: %w", num(*want.W), err)
		}
		if err := h.SetAttr(pathLn, "w", w); err != nil {
			return err
		}
	}
	if want.Cap != cur.Cap {
		if want.Cap == "" {
			return errors.New("line cap: cannot unset an inherited value")
		}
		if err := edit.CheckAttr("a:CT_LineProperties", "cap", want.Cap); err != nil {
			return fmt.Errorf("line cap: %w", err)
		}
		if err := h.SetAttr(pathLn, "cap", want.Cap); err != nil {
			return err
		}
	}
	if want.Dash != cur.Dash {
		switch want.Dash {
		case "":
			return errors.New("line dash: cannot unset an inherited value")
		case "custom":
			return fmt.Errorf("%w: custom dashes", ErrReadOnly)
		}
		if err := edit.CheckAttr("a:CT_PresetLineDashProperties", "val", want.Dash); err != nil {
			return fmt.Errorf("line dash: %w", err)
		}
		if err := h.Ensure(pathLn, edit.A("prstDash")); err != nil {
			return err
		}
		if err := h.SetAttr(lnChild("prstDash"), "val", want.Dash); err != nil {
			return err
		}
	}
	for _, e := range []struct {
		el        string
		want, cur *derive.Arrow
	}{{"headEnd", want.Head, cur.Head}, {"tailEnd", want.Tail, cur.Tail}} {
		if e.want == nil {
			if e.cur != nil {
				return fmt.Errorf("line %s: cannot unset an inherited value", e.el)
			}
			continue
		}
		if e.cur != nil && show.Arrow(e.want) == show.Arrow(e.cur) {
			continue
		}
		for _, a := range [][2]string{{"type", e.want.Type}, {"w", e.want.W}, {"len", e.want.Len}} {
			if a[1] == "" && a[0] != "type" {
				continue
			}
			if err := edit.CheckAttr("a:CT_LineEndProperties", a[0], a[1]); err != nil {
				return fmt.Errorf("line %s %s: %w", e.el, a[0], err)
			}
		}
		if err := h.Ensure(pathLn, edit.A(e.el)); err != nil {
			return err
		}
		p := lnChild(e.el)
		if err := h.SetAttr(p, "type", e.want.Type); err != nil {
			return err
		}
		for _, a := range []struct{ name, v string }{{"w", e.want.W}, {"len", e.want.Len}} {
			var err error
			if a.v == "" {
				err = h.RemoveAttr(p, a.name)
			} else {
				err = h.SetAttr(p, a.name, a.v)
			}
			if err != nil {
				return err
			}
		}
	}
	return nil
}
