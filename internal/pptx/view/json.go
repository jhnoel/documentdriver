package view

import (
	"bytes"
	"encoding/json"
	"maps"
	"slices"
	"strconv"

	"github.com/jhnoel/documentdriver/internal/cli"
	"github.com/jhnoel/documentdriver/internal/pptx/derive"
	"github.com/jhnoel/documentdriver/internal/pptx/model"
	"github.com/jhnoel/documentdriver/internal/pptx/show"
)

// obj is a JSON object that keeps its keys in insertion order.
type obj struct {
	keys []string
	vals []any
}

// set appends one key and value to the object and returns it.
func (o *obj) set(k string, v any) *obj {
	o.keys = append(o.keys, k)
	o.vals = append(o.vals, v)
	return o
}

// MarshalJSON writes the keys in insertion order.
func (o *obj) MarshalJSON() ([]byte, error) {
	var b bytes.Buffer
	b.WriteByte('{')
	for i, k := range o.keys {
		if i > 0 {
			b.WriteByte(',')
		}
		kb, _ := json.Marshal(k)
		b.Write(kb)
		b.WriteByte(':')
		vb, err := marshal(o.vals[i])
		if err != nil {
			return nil, err
		}
		b.Write(vb)
	}
	b.WriteByte('}')
	return b.Bytes(), nil
}

// marshal encodes a value as JSON without HTML escaping.
func marshal(v any) ([]byte, error) {
	var b bytes.Buffer
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return nil, err
	}
	return bytes.TrimRight(b.Bytes(), "\n"), nil
}

// n is a number already formatted.
type n string

// MarshalJSON writes the number as is.
func (x n) MarshalJSON() ([]byte, error) { return []byte(x), nil }

// pt returns a point as two formatted numbers.
func (o Options) pt(p derive.Pt) []n { return []n{n(o.num(p.X)), n(o.num(p.Y))} }

// box returns a transform's x, y, width, and height as formatted numbers.
func (o Options) box(x *derive.Xfrm) []n {
	return []n{n(o.num(x.X)), n(o.num(x.Y)), n(o.num(x.W)), n(o.num(x.H))}
}

// JSON renders the slide as the JSON view.
func JSON(s *model.Slide, o Options) ([]byte, error) {
	o.Slide = s.Name
	root := &obj{}
	if o.Base != "" {
		root.set("base", o.Base)
	}
	root.set("slide", s.Name)
	root.set("units", o.unitName())
	if !slideOmits(s, "size") {
		root.set("size", []n{n(o.num(s.W)), n(o.num(s.H))})
	}
	colors := &obj{}
	for _, k := range slices.Sorted(maps.Keys(s.Colors)) {
		colors.set(k, s.Colors[k])
	}
	root.set("colors", colors)
	if len(s.Fonts) > 0 {
		fonts := &obj{}
		for _, k := range slices.Sorted(maps.Keys(s.Fonts)) {
			fonts.set(k, s.Fonts[k])
		}
		root.set("fonts", fonts)
	}
	objs := []any{}
	for _, ob := range selected(s, o) {
		objs = append(objs, jsonObject(ob, o))
	}
	root.set("objects", objs)
	hz := []any{}
	for _, h := range s.Hazards {
		hz = append(hz, (&obj{}).set("kind", h.Kind).set("id", o.addr(h.ID)).set("detail", h.Detail))
	}
	root.set("hazards", hz)
	root.set("notes", notes(s, o))
	return layout(root)
}

// ObjectJSON renders one object exactly as it appears in the JSON view's
// objects array; o.Slide is the object's slide.
func ObjectJSON(ob *model.Object, o Options) ([]byte, error) {
	return marshal(jsonObject(ob, o))
}

// layout writes the root object with one key per line and each element
// of the objects, hazards, notes and slides arrays on its own line,
// everything else compact.
func layout(root *obj) ([]byte, error) {
	var out bytes.Buffer
	out.WriteString("{")
	for i, k := range root.keys {
		if i > 0 {
			out.WriteString(",")
		}
		kb, _ := json.Marshal(k)
		out.WriteString("\n ")
		out.Write(kb)
		out.WriteString(": ")
		if arr, ok := root.vals[i].([]any); ok && (k == "objects" || k == "hazards" || k == "notes" || k == "slides") && len(arr) > 0 {
			out.WriteString("[")
			for j, v := range arr {
				if j > 0 {
					out.WriteString(",")
				}
				vb, err := marshal(v)
				if err != nil {
					return nil, err
				}
				out.WriteString("\n  ")
				out.Write(vb)
			}
			out.WriteString("\n ]")
			continue
		}
		vb, err := marshal(root.vals[i])
		if err != nil {
			return nil, err
		}
		out.Write(vb)
	}
	out.WriteString("\n}\n")
	return out.Bytes(), nil
}

// notes lists what the view left out or could not read, as {at, reason}:
// the slide's own omissions (a layout, master or theme that is missing
// or unreadable, the slide size), at the slide. An empty list means the
// slide was read in full. A property of one object that is not derived
// is left out of that object, with its reason in the object's "omitted"
// under --detail full.
func notes(s *model.Slide, _ Options) []any {
	out := []any{}
	for _, x := range s.Omitted {
		out = append(out, (&obj{}).set("at", s.Name).set("reason", x.Prop+": "+x.Reason))
	}
	return out
}

// omissions converts omissions into JSON objects with prop and reason.
func omissions(om []model.Omission) []any {
	var out []any
	for _, x := range om {
		out = append(out, (&obj{}).set("prop", x.Prop).set("reason", x.Reason))
	}
	return out
}

// jsonFill shows a fill as a JSON value.
func jsonFill(f *model.Fill, o Options) any { return show.Fill(f, o.full()) }

// jsonLine shows a line as a JSON value.
func jsonLine(l *model.Line, o Options) any { return show.Line(l, o.num, o.full()) }

// jsonObject converts an object into its JSON representation.
func jsonObject(ob *model.Object, o Options) *obj {
	j := &obj{}
	j.set("id", o.addr(ob.ID))
	j.set("kind", string(ob.Kind))
	if ob.Type != "" {
		j.set("type", ob.Type)
	}
	if ob.Name != "" {
		j.set("name", ob.Name)
	}
	if ob.Descr != "" {
		j.set("descr", ob.Descr)
	}
	if ob.Title != "" {
		j.set("title", ob.Title)
	}
	if ob.Parent != "" {
		j.set("parent", o.addr(ob.Parent))
	}
	if ob.Hidden {
		j.set("hidden", true)
	}
	if ob.TxBox {
		j.set("tx_box", true)
	}
	if ob.Ph != nil {
		ph := &obj{}
		if ob.Ph.Type != "" {
			ph.set("type", ob.Ph.Type)
		}
		if ob.Ph.Idx != "" {
			ph.set("idx", ob.Ph.Idx)
		}
		j.set("ph", ph)
	}
	if g := ob.Geom; g != nil {
		if g.Preset != "" {
			j.set("geom", g.Preset)
		} else {
			j.set("geom", "custom")
		}
		if len(g.Adj) > 0 {
			j.set("adj", show.Adj(g.Adj))
		}
		if len(g.Paths) > 0 {
			j.set("path", show.Paths(g.Paths, o.num))
		}
		if g.Parts != nil && o.full() {
			j.set("custom", show.Custom(g.Parts, o.num))
		}
	}
	if ob.Box != nil && (!isConnectorShape(ob) || o.full()) {
		j.set("box", o.box(ob.Box))
		if ob.Box.Rot != 0 {
			j.set("rot", n(rot(ob.Box.Rot)))
		}
		if ob.Box.FlipH {
			j.set("flip_h", true)
		}
		if ob.Box.FlipV {
			j.set("flip_v", true)
		}
	}
	if c := ob.Conn; c != nil && c.First != "?" {
		j.set("from", o.pt(c.From))
		j.set("to", o.pt(c.To))
		if c.First != "" {
			j.set("first", c.First)
		}
		if len(c.Elbows) > 0 {
			var e [][]n
			for _, p := range c.Elbows {
				e = append(e, o.pt(p))
			}
			j.set("elbows", e)
		}
	}
	if c := ob.Conn; c != nil {
		if c.Start != nil {
			j.set("from_glue", glueString(c.Start))
		}
		if c.End != nil {
			j.set("to_glue", glueString(c.End))
		}
	}
	if ob.Fill != nil {
		j.set("fill", jsonFill(ob.Fill, o))
	}
	if ob.Line != nil {
		j.set("line", jsonLine(ob.Line, o))
	}
	if ob.Image != "" {
		j.set("image", ob.Image)
	}
	if p := ob.Pic; p != nil {
		jsonPicture(j, p)
	}
	if ob.Text != nil {
		j.set("html", ob.Text.HTML)
	}
	if o.sites() && len(ob.Sites) > 0 {
		var ss [][]n
		for _, s := range ob.Sites {
			ss = append(ss, o.pt(s.Pt))
		}
		j.set("sites", ss)
	}
	if o.full() {
		j.set("z", ob.Z)
		if ob.Text != nil {
			j.set("text", bodyJSON(ob.Text, o))
		}
		if len(ob.Origin) > 0 {
			org := &obj{}
			for _, k := range slices.Sorted(maps.Keys(ob.Origin)) {
				org.set(cli.Snake(k), string(ob.Origin[k]))
			}
			j.set("origins", org)
		}
		if len(ob.Props) > 0 {
			p := &obj{}
			for _, x := range ob.Props {
				p.set(cli.Snake(x.Key), (&obj{}).set("value", x.Value).set("origin", string(x.Origin)))
			}
			j.set("props", p)
		}
		if len(ob.Omitted) > 0 {
			j.set("omitted", omissions(ob.Omitted))
		}
		if ob.Kind == model.Other && ob.XML() != nil {
			j.set("xml", string(ob.XML()))
		}
	}
	return j
}

// bodyJSON shows the text body properties (show.BodyJSON, shared with the
// body codec).
func bodyJSON(t *model.Text, o Options) json.RawMessage {
	return show.BodyJSON(show.Body{Attrs: t.Attrs, Autofit: t.Autofit}, o.num)
}

// jsonPicture adds a picture's SVG part, media facts, crop, fill mode,
// fill rectangle and locks, each only where the file says it (show
// formats them, as the picture codecs do).
func jsonPicture(j *obj, p *model.PicProps) {
	if p.SVG != "" {
		j.set("svg", p.SVG)
	}
	if p.Media != nil {
		j.set("media", show.Media(p.Media.Type, p.Media.Media))
	}
	if p.SVGMedia != nil {
		j.set("svg_media", show.Media(p.SVGMedia.Type, p.SVGMedia.Media))
	}
	if f := p.Fill; f != nil {
		if f.Crop != nil {
			j.set("crop", show.Rect(*f.Crop))
		}
		if f.Fit != "" {
			j.set("fit", f.Fit)
		}
		if f.FillRect != nil {
			j.set("fill_rect", show.Rect(*f.FillRect))
		}
	}
	if l := show.Locks(p.Locks); l != nil {
		j.set("locks", l)
	}
}

// insets shows the four body insets when all are derived.
func insets(t *model.Text, o Options) []n {
	var out []n
	for _, v := range t.Insets {
		if v == nil {
			return nil
		}
		out = append(out, n(o.num(*v)))
	}
	return out
}

// glueString is "s12:3", or "#17:3" when id 17 does not name exactly one
// object on the slide (dangling, or shared by several objects).
func glueString(g *model.Glue) string {
	id := g.ID
	if id == "" {
		id = "#" + g.RawID
	}
	return id + ":" + strconv.Itoa(g.Site)
}
