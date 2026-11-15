// Package show formats property values exactly as the JSON view shows
// them. The view and the property codecs (internal/pptx/ops/prop) both use it,
// so a value shown on read and the value a codec compares on write are
// the same string. It sees derive's values only, never the read model.
package show

import (
	"bytes"
	"encoding/json"
	"strconv"
	"strings"

	"github.com/jhnoel/documentdriver/internal/pptx/derive"
)

// obj writes a compact JSON object with keys in insertion order.
type obj struct {
	b    bytes.Buffer
	keys int
}

// raw appends one key and raw JSON value to the object and returns it.
func (o *obj) raw(k string, v []byte) *obj {
	if o.keys == 0 {
		o.b.WriteByte('{')
	} else {
		o.b.WriteByte(',')
	}
	o.keys++
	o.b.Write(str(k))
	o.b.WriteByte(':')
	o.b.Write(v)
	return o
}

// str appends one string key and value to the object and returns it.
func (o *obj) str(k, v string) *obj { return o.raw(k, str(v)) }

// done returns the object as JSON, or an empty object when nothing was written.
func (o *obj) done() json.RawMessage {
	if o.keys == 0 {
		return json.RawMessage("{}")
	}
	o.b.WriteByte('}')
	return json.RawMessage(o.b.Bytes())
}

// str encodes a JSON string without HTML escaping, as the views do.
func str(s string) []byte {
	var b bytes.Buffer
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(s)
	return bytes.TrimRight(b.Bytes(), "\n")
}

// Num keeps an integer modifier value as a JSON number, quoting anything
// else (such as the "50%" transitional form).
func Num(s string) []byte {
	for i, r := range s {
		if !(r >= '0' && r <= '9' || r == '-' && i == 0) {
			return str(s)
		}
	}
	if s == "" || s == "-" {
		return []byte(`""`)
	}
	return []byte(s)
}

// Color shows a colour: {"scheme": "accent1", "mods": [...], "hex":
// "#..."} or {"hex": "#..."}. A nil colour is JSON null.
func Color(c *derive.ShownColor) json.RawMessage {
	if c == nil {
		return json.RawMessage("null")
	}
	j := &obj{}
	switch c.Kind {
	case "srgb":
		// The displayed hex stands for a plain sRGB colour; without one
		// (no theme to resolve against) the stored value is shown.
		if len(c.Mods) > 0 || c.Hex == "" {
			j.str("rgb", c.Val)
		}
	case "scheme":
		j.str("scheme", c.Val)
	case "sys":
		j.str("sys", c.Val)
	case "prst":
		j.str("preset", c.Val)
	case "hsl", "scrgb":
		j.raw(c.Kind, []byte("["+strconv.FormatInt(c.Comp[0], 10)+","+strconv.FormatInt(c.Comp[1], 10)+","+strconv.FormatInt(c.Comp[2], 10)+"]"))
	}
	if len(c.Mods) > 0 {
		var m bytes.Buffer
		m.WriteByte('[')
		for i, x := range c.Mods {
			if i > 0 {
				m.WriteByte(',')
			}
			if x.Val == "" {
				m.Write(str(x.Name))
			} else {
				m.Write((&obj{}).raw(x.Name, Num(x.Val)).done())
			}
		}
		m.WriteByte(']')
		j.raw("mods", m.Bytes())
	}
	if c.Hex != "" {
		j.str("hex", c.Hex)
	}
	return j.done()
}

// Fill shows a fill: "none", a colour, {"type": "solid"} for a solid fill
// without a colour, or {"type": "gradFill"} (with "xml" when full) for
// fills shown as raw XML.
func Fill(f *derive.Fill, full bool) json.RawMessage {
	switch f.Type {
	case "none":
		return json.RawMessage(`"none"`)
	case "solid":
		if f.Color == nil {
			return (&obj{}).str("type", "solid").done()
		}
		return Color(f.Color)
	}
	j := (&obj{}).str("type", f.Type)
	if full {
		j.str("xml", f.XML)
	}
	return j.done()
}

// Line shows an outline: "none", or {"w", "color", "dash", "cap", "head",
// "tail"} with the fields that were derived. num formats a length in EMU
// in the view's units.
func Line(l *derive.Line, num func(float64) string, full bool) json.RawMessage {
	if l.None {
		return json.RawMessage(`"none"`)
	}
	j := &obj{}
	if l.W != nil {
		j.raw("w", []byte(num(*l.W)))
	}
	switch {
	case l.Color != nil:
		j.raw("color", Color(l.Color))
	case l.FillXML != "":
		c := (&obj{}).str("type", FillType(l.FillXML))
		if full {
			c.str("xml", l.FillXML)
		}
		j.raw("color", c.done())
	}
	if l.Dash != "" {
		j.str("dash", l.Dash)
	}
	if l.Cap != "" {
		j.str("cap", l.Cap)
	}
	if l.Head != nil {
		j.str("head", Arrow(l.Head))
	}
	if l.Tail != nil {
		j.str("tail", Arrow(l.Tail))
	}
	return j.done()
}

// Arrow is the compact arrow form shared by both views: the type, plus
// " w=.. len=.." when written.
func Arrow(a *derive.Arrow) string {
	s := a.Type
	if a.W != "" {
		s += " w=" + a.W
	}
	if a.Len != "" {
		s += " len=" + a.Len
	}
	return s
}

// FillType extracts the local element name of a raw fill.
func FillType(xml string) string {
	s := xml
	if i := strings.IndexByte(s, '<'); i >= 0 {
		s = s[i+1:]
	}
	end := len(s)
	for i, r := range s {
		if r == ' ' || r == '>' || r == '/' || r == '\t' || r == '\n' || r == '\r' {
			end = i
			break
		}
	}
	s = s[:end]
	if i := strings.IndexByte(s, ':'); i >= 0 {
		s = s[i+1:]
	}
	return s
}

// AdjValue shows an adjust formula: the number for "val N", else the
// formula as written.
func AdjValue(f string) string {
	if v, ok := strings.CutPrefix(f, "val "); ok {
		if _, err := strconv.ParseFloat(v, 64); err == nil {
			return v
		}
	}
	return f
}

// Adj shows adjust values as the view's "adj" object: numbers for "val N",
// formulas as strings.
func Adj(adj []derive.Adjust) json.RawMessage {
	j := &obj{}
	for _, a := range adj {
		v := AdjValue(a.Fmla)
		if v == a.Fmla {
			j.str(a.Name, v)
		} else {
			j.raw(a.Name, []byte(v))
		}
	}
	return j.done()
}

// Geom shows a geometry as the view's "geom" value: the preset name, or
// "custom".
func Geom(g derive.Geom) string {
	if g.Custom {
		return "custom"
	}
	return g.Preset
}

// PathUnits converts a path whose numbers are EMU (as the read model and
// derive.CustomPaths give them) into the units num formats.
func PathUnits(p string, num func(float64) string) string {
	f := strings.Fields(p)
	for i, s := range f {
		if v, err := strconv.ParseFloat(s, 64); err == nil {
			f[i] = num(v)
		}
	}
	return strings.Join(f, " ")
}

// Paths shows a custom geometry's paths as the view's "path" value: one
// string when there is a single path with the default fill and stroke,
// else an array of {"d", "fill", "stroke"} objects with the modes that
// differ from the defaults (fill norm, stroke true). Numbers are EMU,
// converted by num.
func Paths(paths []derive.ShownPath, num func(float64) string) json.RawMessage {
	if len(paths) == 1 && paths[0].Default() {
		return str(PathUnits(paths[0].D, num))
	}
	var b bytes.Buffer
	b.WriteByte('[')
	for i, p := range paths {
		if i > 0 {
			b.WriteByte(',')
		}
		j := (&obj{}).str("d", PathUnits(p.D, num))
		if p.Fill != "" {
			j.str("fill", p.Fill)
		}
		if p.NoStroke {
			j.raw("stroke", []byte("false"))
		}
		b.Write(j.done())
	}
	b.WriteByte(']')
	return b.Bytes()
}

// Ref shows a custom geometry coordinate: a guide name as a string, or a
// literal (EMU) as a number in num's units.
func Ref(r derive.ShownRef, num func(float64) string) json.RawMessage {
	if r.Name != "" {
		return str(r.Name)
	}
	return json.RawMessage(num(r.V))
}

// Custom shows the non-path part of a custom geometry as the view's
// "custom" object (--detail full): "av" and "gd" as [name, formula]
// pairs, "ah" as handles ({"ahXY": {attributes}, "pos": [x, y]}, all as
// written), "cxn" as [angle, x, y] sites and "rect" as [l, t, r, b], with
// literal coordinates in num's units relative to the box and literal
// angles in degrees. A list's key is present exactly when the list is
// written, even empty.
func Custom(c *derive.ShownCustom, num func(float64) string) json.RawMessage {
	j := &obj{}
	list := func(n int, item func(i int, b *bytes.Buffer)) []byte {
		var b bytes.Buffer
		b.WriteByte('[')
		for i := range n {
			if i > 0 {
				b.WriteByte(',')
			}
			item(i, &b)
		}
		b.WriteByte(']')
		return b.Bytes()
	}
	pairs := func(gs []derive.Adjust) []byte {
		return list(len(gs), func(i int, b *bytes.Buffer) {
			b.WriteByte('[')
			b.Write(str(gs[i].Name))
			b.WriteByte(',')
			b.Write(str(gs[i].Fmla))
			b.WriteByte(']')
		})
	}
	if c.HasAv {
		j.raw("av", pairs(c.Av))
	}
	if c.HasGd {
		j.raw("gd", pairs(c.Gd))
	}
	if c.HasAh {
		j.raw("ah", list(len(c.Ah), func(i int, b *bytes.Buffer) {
			h := c.Ah[i]
			attrs := &obj{}
			for _, a := range h.Attrs {
				attrs.str(a[0], a[1])
			}
			pos := "[" + string(str(h.Pos[0])) + "," + string(str(h.Pos[1])) + "]"
			b.Write((&obj{}).raw(h.Kind, attrs.done()).raw("pos", []byte(pos)).done())
		}))
	}
	deg := func(v float64) string { return derive.FormatNum(v) }
	if c.HasCxn {
		j.raw("cxn", list(len(c.Cxn), func(i int, b *bytes.Buffer) {
			s := c.Cxn[i]
			b.WriteByte('[')
			b.Write(Ref(s[0], deg))
			b.WriteByte(',')
			b.Write(Ref(s[1], num))
			b.WriteByte(',')
			b.Write(Ref(s[2], num))
			b.WriteByte(']')
		}))
	}
	if c.Rect != nil {
		j.raw("rect", list(4, func(i int, b *bytes.Buffer) { b.Write(Ref(c.Rect[i], num)) }))
	}
	return j.done()
}
