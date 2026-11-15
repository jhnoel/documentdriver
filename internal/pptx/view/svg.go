package view

import (
	"fmt"
	"maps"
	"slices"
	"strconv"
	"strings"

	"github.com/jhnoel/documentdriver/internal/cli"
	"github.com/jhnoel/documentdriver/internal/pptx/derive"
	"github.com/jhnoel/documentdriver/internal/pptx/model"
	"github.com/jhnoel/documentdriver/internal/pptx/show"
)

// svgw accumulates SVG output.
type svgw struct {
	b      strings.Builder
	o      Options
	arrows map[string]*model.Arrow
}

// esc escapes s for an attribute value, writing tab, newline and carriage
// return as character references so that attribute-value normalisation
// keeps them.
func esc(s string) string {
	r := strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", `"`, "&quot;", "\t", "&#9;", "\n", "&#10;", "\r", "&#13;")
	return r.Replace(s)
}

// escText escapes s for character data, keeping carriage returns, which
// XML parsers otherwise fold into newlines.
func escText(s string) string {
	r := strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", "\r", "&#13;")
	return r.Replace(s)
}

// attr writes name="value" with the value escaped.
func (w *svgw) attr(name, value string) {
	w.b.WriteString(" " + name + `="` + esc(value) + `"`)
}

// jsonAttr writes a data attribute holding JSON.
func (w *svgw) jsonAttr(name string, v any) {
	b, _ := marshal(v)
	w.attr(name, string(b))
}

// SVG renders the slide as the SVG view: the same objects, ids and values
// as the JSON view. Coordinates are in the selected units; scheme colours
// are CSS variables on :root.
func SVG(s *model.Slide, o Options) ([]byte, error) {
	o.Slide = s.Name
	w := &svgw{o: o, arrows: map[string]*model.Arrow{}}
	sel := selected(s, o)
	body := &svgw{o: o, arrows: w.arrows}
	inSel := map[string]bool{}
	for _, ob := range sel {
		inSel[ob.ID] = true
	}
	byID := map[string]*model.Object{}
	for _, ob := range s.Objects {
		byID[ob.ID] = ob
	}
	// shownParent is the nearest selected ancestor.
	shownParent := func(ob *model.Object) string {
		for p := byID[ob.Parent]; p != nil; p = byID[p.Parent] {
			if inSel[p.ID] {
				return p.ID
			}
			if p.Parent == "" {
				break
			}
		}
		return ""
	}
	children := map[string][]*model.Object{}
	var top []*model.Object
	for _, ob := range sel {
		if sp := shownParent(ob); sp != "" {
			children[sp] = append(children[sp], ob)
		} else {
			top = append(top, ob)
		}
	}
	var emit func(ob *model.Object, nestParent string)
	emit = func(ob *model.Object, nestParent string) {
		body.object(ob, nestParent, func() {
			for _, c := range children[ob.ID] {
				emit(c, ob.ID)
			}
		})
	}
	for _, ob := range top {
		emit(ob, "")
	}

	w.b.WriteString(`<svg xmlns="http://www.w3.org/2000/svg"`)
	if o.Base != "" {
		w.attr("data-base", o.Base)
	}
	w.attr("data-slide", s.Name)
	w.attr("data-units", o.unitName())
	if !slideOmits(s, "size") {
		w.attr("viewBox", "0 0 "+o.num(s.W)+" "+o.num(s.H))
	}
	hz := []any{}
	for _, h := range s.Hazards {
		hz = append(hz, (&obj{}).set("kind", h.Kind).set("id", o.addr(h.ID)).set("detail", h.Detail))
	}
	w.jsonAttr("data-hazards", hz)
	w.jsonAttr("data-notes", notes(s, o))
	w.b.WriteString(">\n")
	var css []string
	for _, k := range slices.Sorted(maps.Keys(s.Colors)) {
		css = append(css, "--"+k+": "+s.Colors[k])
	}
	for _, k := range slices.Sorted(maps.Keys(s.Fonts)) {
		css = append(css, "--"+strings.TrimPrefix(k, "+")+": '"+strings.ReplaceAll(s.Fonts[k], "'", `\'`)+"'")
	}
	w.b.WriteString("  <style>:root { " + escText(strings.Join(css, "; ")) + " }</style>\n")
	if len(w.arrows) > 0 {
		w.b.WriteString("  <defs>\n")
		for _, k := range slices.Sorted(maps.Keys(w.arrows)) {
			w.marker(k, w.arrows[k])
		}
		w.b.WriteString("  </defs>\n")
	}
	w.b.WriteString(body.b.String())
	w.b.WriteString("</svg>\n")
	return []byte(w.b.String()), nil
}

// marker writes an arrowhead marker definition. The shapes are
// approximate; the marker id carries the arrow as written.
func (w *svgw) marker(id string, a *model.Arrow) {
	w.b.WriteString(`    <marker`)
	w.attr("id", id)
	w.b.WriteString(` viewBox="0 0 10 10" refX="5" refY="5" markerWidth="6" markerHeight="6" orient="auto-start-reverse">`)
	switch a.Type {
	case "oval":
		w.b.WriteString(`<circle cx="5" cy="5" r="4"/>`)
	case "diamond":
		w.b.WriteString(`<path d="M 5 0 L 10 5 L 5 10 L 0 5 Z"/>`)
	case "arrow":
		w.b.WriteString(`<path d="M 0 0 L 10 5 L 0 10" fill="none" stroke="black"/>`)
	case "stealth":
		w.b.WriteString(`<path d="M 0 0 L 10 5 L 0 10 L 3 5 Z"/>`)
	default:
		w.b.WriteString(`<path d="M 0 0 L 10 5 L 0 10 Z"/>`)
	}
	w.b.WriteString("</marker>\n")
}

// markerID names a marker for an arrow: the type, plus -w-len when
// written ("triangle", "triangle-sm-lg"; a missing part is "x").
func markerID(a *model.Arrow) string {
	if a.W == "" && a.Len == "" {
		return a.Type
	}
	w, l := a.W, a.Len
	if w == "" {
		w = "x"
	}
	if l == "" {
		l = "x"
	}
	return a.Type + "-" + w + "-" + l
}

// paint returns the SVG paint for a colour and, when the CSS value does
// not carry the colour as written, the data attribute value that does.
func paint(c *model.Color) (value, data string) {
	switch {
	case c.Kind == "scheme" && len(c.Mods) == 0:
		return "var(--" + c.Val + ")", ""
	case c.Kind == "srgb" && len(c.Mods) == 0:
		return "#" + c.Val, ""
	}
	v := c.Hex
	if v == "" {
		v = "none"
	}
	return v, model.ColorString(c)
}

// fillAttrs writes fill attributes for an object's geometry element.
func (w *svgw) fillAttrs(f *model.Fill) {
	switch {
	case f == nil:
		w.attr("fill", "none")
		w.attr("data-fill", "?")
	case f.Type == "none":
		w.attr("fill", "none")
	case f.Type == "solid" && f.Color != nil:
		v, d := paint(f.Color)
		w.attr("fill", v)
		if d != "" {
			w.attr("data-fill", d)
		}
		if f.Color.Alpha < 1 {
			w.attr("fill-opacity", derive.FormatNum(f.Color.Alpha))
		}
	default:
		w.attr("fill", "none")
		w.attr("data-fill", f.Type)
		if w.o.full() && f.XML != "" {
			w.attr("data-fill-xml", f.XML)
		}
	}
}

// strokeAttrs writes outline attributes.
func (w *svgw) strokeAttrs(l *model.Line) {
	if l == nil {
		w.attr("stroke", "none")
		w.attr("data-stroke", "?")
		return
	}
	if l.None {
		w.attr("stroke", "none")
		return
	}
	switch {
	case l.Color != nil:
		v, d := paint(l.Color)
		w.attr("stroke", v)
		if d != "" {
			w.attr("data-stroke", d)
		}
	case l.FillXML != "":
		w.attr("stroke", "none")
		w.attr("data-stroke", show.FillType(l.FillXML))
		if w.o.full() {
			w.attr("data-stroke-xml", l.FillXML)
		}
	default:
		w.attr("stroke", "none")
		w.attr("data-stroke", "?")
	}
	if l.W != nil {
		w.attr("stroke-width", w.o.num(*l.W))
	}
	if l.Dash != "" {
		w.attr("data-dash", l.Dash)
		if l.Dash != "solid" {
			w.attr("stroke-dasharray", "4 2")
		}
	}
	if l.Cap != "" {
		w.attr("stroke-linecap", map[string]string{"rnd": "round", "sq": "square", "flat": "butt"}[l.Cap])
		w.attr("data-cap", l.Cap)
	}
	if l.Head != nil {
		id := markerID(l.Head)
		w.arrows[id] = l.Head
		if l.Head.Type != "none" {
			w.attr("marker-start", "url(#"+id+")")
		}
		w.attr("data-head", arrowString(l.Head))
	}
	if l.Tail != nil {
		id := markerID(l.Tail)
		w.arrows[id] = l.Tail
		if l.Tail.Type != "none" {
			w.attr("marker-end", "url(#"+id+")")
		}
		w.attr("data-tail", arrowString(l.Tail))
	}
}

// common writes the data attributes every object element carries.
func (w *svgw) common(ob *model.Object, nestParent string) {
	w.attr("id", w.o.addr(ob.ID))
	w.attr("data-kind", string(ob.Kind))
	if ob.Type != "" {
		w.attr("data-type", ob.Type)
	}
	if ob.Name != "" {
		w.attr("data-name", ob.Name)
	}
	if ob.Descr != "" {
		w.attr("data-descr", ob.Descr)
	}
	if ob.Title != "" {
		w.attr("data-title", ob.Title)
	}
	if ob.Parent != nestParent {
		w.attr("data-parent", w.o.addr(ob.Parent))
	}
	if ob.Hidden {
		w.attr("data-hidden", "1")
		w.attr("visibility", "hidden")
	}
	if ob.TxBox {
		w.attr("data-tx-box", "1")
	}
	if ob.Ph != nil {
		w.attr("data-ph", ob.Ph.Type+":"+ob.Ph.Idx)
	}
	if g := ob.Geom; g != nil {
		if g.Preset != "" {
			w.attr("data-geom", g.Preset)
		} else {
			w.attr("data-geom", "custom")
		}
		if len(g.Adj) > 0 {
			var parts []string
			for _, a := range g.Adj {
				parts = append(parts, a.Name+"="+adjValue(a.Fmla))
			}
			w.attr("data-adj", strings.Join(parts, ";"))
		}
	}
	if c := ob.Conn; c != nil {
		if c.Start != nil {
			w.attr("data-from-glue", glueString(c.Start))
		}
		if c.End != nil {
			w.attr("data-to-glue", glueString(c.End))
		}
		if c.First != "" && c.First != "?" {
			w.attr("data-first", c.First)
		}
	}
	if ob.Image != "" {
		w.attr("data-image", ob.Image)
	}
	if p := ob.Pic; p != nil {
		w.pictureAttrs(p)
	}
	if w.o.full() {
		w.attr("data-z", fmt.Sprint(ob.Z))
		if len(ob.Origin) > 0 {
			org := &obj{}
			for _, k := range slices.Sorted(maps.Keys(ob.Origin)) {
				org.set(cli.Snake(k), string(ob.Origin[k]))
			}
			w.jsonAttr("data-origins", org)
		}
		if len(ob.Props) > 0 {
			p := &obj{}
			for _, x := range ob.Props {
				p.set(cli.Snake(x.Key), (&obj{}).set("value", x.Value).set("origin", string(x.Origin)))
			}
			w.jsonAttr("data-props", p)
		}
		if len(ob.Omitted) > 0 {
			w.jsonAttr("data-omitted", omissions(ob.Omitted))
		}
	}
}

// pictureAttrs writes a picture's facts beyond its image part as data
// attributes, in the JSON view's values: data-svg, data-media and
// data-svg-media (JSON), data-crop and data-fill-rect ("l t r b" in
// percent), data-fit and data-locks (JSON).
func (w *svgw) pictureAttrs(p *model.PicProps) {
	if p.SVG != "" {
		w.attr("data-svg", p.SVG)
	}
	if p.Media != nil {
		w.attr("data-media", string(show.Media(p.Media.Type, p.Media.Media)))
	}
	if p.SVGMedia != nil {
		w.attr("data-svg-media", string(show.Media(p.SVGMedia.Type, p.SVGMedia.Media)))
	}
	rect := func(r [4]int64) string {
		return show.Percent(r[0]) + " " + show.Percent(r[1]) + " " + show.Percent(r[2]) + " " + show.Percent(r[3])
	}
	if f := p.Fill; f != nil {
		if f.Crop != nil {
			w.attr("data-crop", rect(*f.Crop))
		}
		if f.Fit != "" {
			w.attr("data-fit", f.Fit)
		}
		if f.FillRect != nil {
			w.attr("data-fill-rect", rect(*f.FillRect))
		}
	}
	if l := show.Locks(p.Locks); l != nil {
		w.attr("data-locks", string(l))
	}
}

// pictureImage draws a picture's image: a nested svg whose viewport is
// the box (inset by the stretch's fill rectangle) and whose viewBox is
// the cropped part of the image in fractions of the image, holding the
// image stretched over the unit square, so the crop clips it. Rotation
// and flips go on an enclosing g. A tiled fill is drawn stretched;
// data-fit is authoritative.
func (w *svgw) pictureImage(ob *model.Object, b *derive.Xfrm) {
	o := w.o
	x, y, bw, bh := b.X, b.Y, b.W, b.H
	vx, vy, vw, vh := 0.0, 0.0, 1.0, 1.0
	if f := ob.Pic.Fill; f != nil {
		if r := f.FillRect; r != nil {
			x += bw * float64(r[0]) / 100000
			y += bh * float64(r[1]) / 100000
			bw *= 1 - float64(r[0]+r[2])/100000
			bh *= 1 - float64(r[1]+r[3])/100000
		}
		if c := f.Crop; c != nil && float64(c[0]+c[2]) < 100000 && float64(c[1]+c[3]) < 100000 {
			vx, vy = float64(c[0])/100000, float64(c[1])/100000
			vw, vh = 1-float64(c[0]+c[2])/100000, 1-float64(c[1]+c[3])/100000
		}
	}
	frac := func(f float64) string { return strconv.FormatFloat(f, 'g', 6, 64) }
	w.b.WriteString("    <g")
	w.transform(b, true)
	w.b.WriteString("><svg")
	w.attr("x", o.num(x))
	w.attr("y", o.num(y))
	w.attr("width", o.num(max(bw, 0)))
	w.attr("height", o.num(max(bh, 0)))
	w.attr("viewBox", frac(vx)+" "+frac(vy)+" "+frac(vw)+" "+frac(vh))
	w.attr("preserveAspectRatio", "none")
	w.b.WriteString(`><image width="1" height="1" preserveAspectRatio="none"`)
	if ob.Image != "" {
		w.attr("href", ob.Image)
	}
	w.b.WriteString("/></svg></g>\n")
}

// rotation returns the rotate() transform about the box centre, or "".
func (w *svgw) rotation(b *derive.Xfrm) string {
	if b.Rot == 0 {
		return ""
	}
	cx, cy := b.X+b.W/2, b.Y+b.H/2
	return "rotate(" + rot(b.Rot) + " " + w.o.num(cx) + " " + w.o.num(cy) + ")"
}

// flip returns the reflection about the box centre, or "".
func (w *svgw) flip(b *derive.Xfrm) string {
	if !b.FlipH && !b.FlipV {
		return ""
	}
	a, d, e, f := "1", "1", "0", "0"
	if b.FlipH {
		a, e = "-1", w.o.num(2*b.X+b.W)
	}
	if b.FlipV {
		d, f = "-1", w.o.num(2*b.Y+b.H)
	}
	return "matrix(" + a + " 0 0 " + d + " " + e + " " + f + ")"
}

// transform writes the rotation and then the flip of a geometry element;
// the flip is omitted for text, which flips do not mirror.
func (w *svgw) transform(b *derive.Xfrm, withFlip bool) {
	var t []string
	if r := w.rotation(b); r != "" {
		t = append(t, r)
	}
	if withFlip {
		if f := w.flip(b); f != "" {
			t = append(t, f)
		}
	}
	if len(t) > 0 {
		w.attr("transform", strings.Join(t, " "))
	}
}

// rectAttrs writes the x, y, width, and height attributes of a box.
func (w *svgw) rectAttrs(b *derive.Xfrm) {
	w.attr("x", w.o.num(b.X))
	w.attr("y", w.o.num(b.Y))
	w.attr("width", w.o.num(b.W))
	w.attr("height", w.o.num(b.H))
}

// connElem opens the element that draws a connector path: path for
// curved connectors (endpoints in data-from/data-to), polyline when there
// are elbows, else line.
func (w *svgw) connElem(c *model.Conn, indent string) {
	o := w.o
	switch {
	case c.Curve != nil:
		w.b.WriteString(indent + "<path")
		var d strings.Builder
		d.WriteString("M " + o.num(c.Curve[0].X) + " " + o.num(c.Curve[0].Y))
		for i := 1; i+2 < len(c.Curve); i += 3 {
			d.WriteString(" C")
			for _, p := range c.Curve[i : i+3] {
				d.WriteString(" " + o.num(p.X) + " " + o.num(p.Y))
			}
		}
		w.attr("d", d.String())
		w.attr("data-from", o.num(c.From.X)+" "+o.num(c.From.Y))
		w.attr("data-to", o.num(c.To.X)+" "+o.num(c.To.Y))
	case len(c.Elbows) > 0:
		w.b.WriteString(indent + "<polyline")
		pts := []string{o.num(c.From.X) + "," + o.num(c.From.Y)}
		for _, p := range c.Elbows {
			pts = append(pts, o.num(p.X)+","+o.num(p.Y))
		}
		pts = append(pts, o.num(c.To.X)+","+o.num(c.To.Y))
		w.attr("points", strings.Join(pts, " "))
	default:
		w.b.WriteString(indent + "<line")
		w.attr("x1", o.num(c.From.X))
		w.attr("y1", o.num(c.From.Y))
		w.attr("x2", o.num(c.To.X))
		w.attr("y2", o.num(c.To.Y))
	}
}

// object writes one SVG object, calling children to write any nested objects.
func (w *svgw) object(ob *model.Object, nestParent string, children func()) {
	o := w.o
	b := ob.Box
	// Connectors with derived endpoints are a single element.
	if ob.Kind == model.Connector && ob.Conn != nil && ob.Conn.First != "?" {
		c := ob.Conn
		w.connElem(c, "  ")
		w.common(ob, nestParent)
		if o.full() && b != nil {
			w.attr("data-box", o.num(b.X)+" "+o.num(b.Y)+" "+o.num(b.W)+" "+o.num(b.H))
			if b.Rot != 0 {
				w.attr("data-rot", rot(b.Rot))
			}
			if f := flipName(b); f != "" {
				w.attr("data-flip", f)
			}
		}
		if ob.Fill != nil {
			w.fillAttrs(ob.Fill)
		} else {
			w.attr("fill", "none")
		}
		w.strokeAttrs(ob.Line)
		w.b.WriteString("/>\n")
		return
	}

	w.b.WriteString("  <g")
	w.common(ob, nestParent)
	if b != nil && b.Rot != 0 && (ob.Kind == model.Group || (ob.Kind == model.Connector && o.full())) {
		w.attr("data-rot", rot(b.Rot))
	}
	if b != nil && (ob.Kind == model.Group || (ob.Kind == model.Connector && o.full())) {
		w.attr("data-box", o.num(b.X)+" "+o.num(b.Y)+" "+o.num(b.W)+" "+o.num(b.H))
		if f := flipName(b); f != "" {
			w.attr("data-flip", f)
		}
	}
	if ob.Kind == model.Group && ob.Fill != nil {
		w.fillAttrs(ob.Fill)
	}
	if ob.Kind == model.Connector {
		w.strokeAttrs(ob.Line)
	}
	w.b.WriteString(">\n")
	if b != nil && ob.Kind != model.Group && ob.Kind != model.Connector {
		geom := ""
		if ob.Geom != nil {
			geom = ob.Geom.Preset
		}
		custom := false
		switch {
		case ob.Kind == model.Picture:
			w.pictureImage(ob, b)
			w.b.WriteString("    <rect")
			w.rectAttrs(b)
			w.transform(b, true)
			if ob.Geom != nil && ob.Geom.Path != "" {
				w.attr("data-path", pathUnits(ob.Geom.Path, o))
				w.customAttrs(ob.Geom)
			}
			w.attr("fill", "none")
		case ob.Geom != nil && ob.Geom.Custom && ob.Geom.Path != "":
			custom = true
			w.b.WriteString("    <g")
			w.transform(b, false)
			w.b.WriteString("><svg")
			w.rectAttrs(b)
			w.attr("viewBox", "0 0 "+o.num(b.W)+" "+o.num(b.H))
			w.attr("preserveAspectRatio", "none")
			w.attr("overflow", "visible")
			w.b.WriteString("><path")
			w.attr("d", pathUnits(ob.Geom.Path, o))
			w.customAttrs(ob.Geom)
			w.flipLocal(b)
		case geom == "ellipse":
			w.b.WriteString("    <ellipse")
			w.attr("cx", o.num(b.X+b.W/2))
			w.attr("cy", o.num(b.Y+b.H/2))
			w.attr("rx", o.num(b.W/2))
			w.attr("ry", o.num(b.H/2))
			// The box as stored; cx/cy/rx/ry are rounded separately.
			w.attr("data-box", o.num(b.X)+" "+o.num(b.Y)+" "+o.num(b.W)+" "+o.num(b.H))
			w.transform(b, true)
		case ob.Conn != nil && ob.Conn.First != "?":
			// A shape with a connector preset: drawn by its path.
			w.connElem(ob.Conn, "    ")
			w.attr("data-box", o.num(b.X)+" "+o.num(b.Y)+" "+o.num(b.W)+" "+o.num(b.H))
			if b.Rot != 0 {
				w.attr("data-rot", rot(b.Rot))
			}
			if f := flipName(b); f != "" {
				w.attr("data-flip", f)
			}
		default:
			w.b.WriteString("    <rect")
			w.rectAttrs(b)
			w.transform(b, true)
			if ob.Geom != nil && ob.Geom.Custom {
				// A custom geometry whose path is empty or not shown.
				if len(ob.Geom.Paths) > 0 {
					w.attr("data-path", pathUnits(ob.Geom.Path, o))
				}
				w.customAttrs(ob.Geom)
			}
		}
		if ob.Kind != model.Picture {
			w.fillAttrs(ob.Fill)
		}
		w.strokeAttrs(ob.Line)
		w.b.WriteString("/>")
		if custom {
			w.b.WriteString("</svg></g>")
		}
		w.b.WriteString("\n")
	} else if b == nil && ob.Kind != model.Group && ob.Kind != model.Connector {
		// No box: the paint still has to be carried.
		w.b.WriteString("    <rect")
		w.attr("data-nobox", "1")
		if ob.Kind != model.Picture {
			w.fillAttrs(ob.Fill)
		}
		w.strokeAttrs(ob.Line)
		w.b.WriteString("/>\n")
	}
	if o.sites() {
		for _, s := range ob.Sites {
			w.b.WriteString("    <circle")
			w.attr("data-site", fmt.Sprint(s.Index))
			w.attr("cx", o.num(s.X))
			w.attr("cy", o.num(s.Y))
			w.b.WriteString(` r="1.5" fill="red"/>` + "\n")
		}
	}
	if ob.Text != nil {
		w.b.WriteString("    <foreignObject")
		if b != nil {
			w.rectAttrs(b)
			w.transform(b, false)
		}
		if o.full() {
			if ob.Text.Anchor != "" {
				w.attr("data-anchor", ob.Text.Anchor)
			}
			if ob.Text.Wrap != "" {
				w.attr("data-wrap", ob.Text.Wrap)
			}
			if ins := insets(ob.Text, o); ins != nil {
				var s []string
				for _, v := range ins {
					s = append(s, string(v))
				}
				w.attr("data-insets", strings.Join(s, " "))
			}
			// The other body properties, as the JSON view's text object
			// shows them.
			rest := map[string]string{}
			for k, v := range ob.Text.Attrs {
				switch k {
				case "anchor", "wrap", "lIns", "tIns", "rIns", "bIns":
				default:
					rest[k] = v
				}
			}
			w.attr("data-body", string(show.BodyJSON(show.Body{Attrs: rest, Autofit: ob.Text.Autofit}, o.num)))
		}
		w.b.WriteString(">" + ob.Text.HTML + "</foreignObject>\n")
	}
	if o.full() && ob.Kind == model.Other && ob.XML() != nil {
		w.b.WriteString("    <desc>" + escText(string(ob.XML())) + "</desc>\n")
	}
	children()
	w.b.WriteString("  </g>\n")
}

// customAttrs writes what the path element's d does not carry of a
// custom geometry: the paths one by one with their fill and stroke modes
// (data-paths, the JSON view's "path" array) when they are not a single
// default path, and in full detail the other lists (data-custom, the JSON
// view's "custom").
func (w *svgw) customAttrs(g *model.Geometry) {
	if len(g.Paths) > 0 && (len(g.Paths) != 1 || !g.Paths[0].Default()) {
		w.attr("data-paths", string(show.Paths(g.Paths, w.o.num)))
	}
	if g.Parts != nil && w.o.full() {
		w.attr("data-custom", string(show.Custom(g.Parts, w.o.num)))
	}
}

// flipLocal writes a flip in a path's local coordinates.
func (w *svgw) flipLocal(x *derive.Xfrm) {
	if !x.FlipH && !x.FlipV {
		return
	}
	a, d, e, f := "1", "1", "0", "0"
	if x.FlipH {
		a, e = "-1", w.o.num(x.W)
	}
	if x.FlipV {
		d, f = "-1", w.o.num(x.H)
	}
	w.attr("transform", "matrix("+a+" 0 0 "+d+" "+e+" "+f+")")
}

// pathUnits returns the path, which the model holds in EMU, in the
// selected units.
func pathUnits(p string, o Options) string {
	if o.emu() {
		return p
	}
	return show.PathUnits(p, o.num)
}

// SlideList renders the slide list as JSON: the deck's base hash (when
// given), then the slides in presentation order.
func SlideList(list []model.SlideInfo, base string) ([]byte, error) {
	root := &obj{}
	if base != "" {
		root.set("base", base)
	}
	out := []any{}
	for i, s := range list {
		out = append(out, (&obj{}).set("index", i+1).set("slide", s.Name).set("part", s.Part).set("title", s.Title).set("objects", s.Objects))
	}
	root.set("slides", out)
	root.set("notes", []any{})
	return layout(root)
}
