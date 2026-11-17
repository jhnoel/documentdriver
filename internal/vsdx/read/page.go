package read

import (
	"encoding/json"
	"strconv"
	"strings"

	"github.com/jhnoel/documentdriver/internal/cli"
	"github.com/jhnoel/documentdriver/internal/geom"
	"github.com/jhnoel/documentdriver/internal/vsdx/model"
	"github.com/jhnoel/documentdriver/internal/vsdx/vsd"
)

// pageJSON is one page as read shows it, with keys in the golden layout order.
type pageJSON struct {
	Base             string           `json:"base"`
	Page             string           `json:"page"`
	Name             string           `json:"name"`
	Units            string           `json:"units"`
	Size             []json.Number    `json:"size,omitempty"`
	Orientation      string           `json:"orientation,omitempty"`
	PrintOrientation string           `json:"print_orientation,omitempty"`
	Background       bool             `json:"background,omitempty"`
	BackgroundPage   string           `json:"background_page,omitempty"`
	Layers           []layerJSON      `json:"layers"`
	Objects          []objectJSON     `json:"objects"`
	Connections      []connectionJSON `json:"connections"`
	Hazards          []hazardJSON     `json:"hazards"`
	Notes            []Note           `json:"notes"`
}

// layerJSON is one page layer as read shows it.
type layerJSON struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Index   int    `json:"index"`
	Visible bool   `json:"visible"`
	Print   bool   `json:"print"`
	Lock    bool   `json:"lock,omitempty"`
}

// connectionJSON is one connector's glue as read shows it.
type connectionJSON struct {
	Connector string `json:"connector"`
	From      string `json:"from,omitempty"`
	To        string `json:"to,omitempty"`
	FromGlue  string `json:"from_glue,omitempty"`
	ToGlue    string `json:"to_glue,omitempty"`
}

// hazardJSON is one page hazard as read shows it.
type hazardJSON struct {
	Kind   string `json:"kind"`
	ID     string `json:"id"`
	Detail string `json:"detail"`
}

// pageHeadJSON is a page result up to and including its layers:
// the head of the streamed page output, marshalled on its own so
// the objects can be written one by one into the same buffer.
type pageHeadJSON struct {
	Base             string        `json:"base"`
	Page             string        `json:"page"`
	Name             string        `json:"name"`
	Units            string        `json:"units"`
	Size             []json.Number `json:"size,omitempty"`
	Orientation      string        `json:"orientation,omitempty"`
	PrintOrientation string        `json:"print_orientation,omitempty"`
	Background       bool          `json:"background,omitempty"`
	BackgroundPage   string        `json:"background_page,omitempty"`
	Layers           []layerJSON   `json:"layers"`
}

// pageTailJSON is a page result's connections, hazards and notes:
// the tail of the streamed page output, marshalled on its own.
type pageTailJSON struct {
	Connections []connectionJSON `json:"connections"`
	Hazards     []hazardJSON     `json:"hazards"`
	Notes       []Note           `json:"notes"`
}

// hexJSON is a colour as read shows it.
type hexJSON struct {
	Hex string `json:"hex"`
}

// lineJSON is a line as read shows it.
type lineJSON struct {
	W     json.Number `json:"w,omitempty"`
	Color *hexJSON    `json:"color,omitempty"`
	Dash  string      `json:"dash,omitempty"`
}

// gluedJSON is one non-end Connect row as read shows it.
type gluedJSON struct {
	Cell string `json:"cell"`
	To   string `json:"to"`
}

// dateJSON is a date prop value as read shows it.
type dateJSON struct {
	Date string `json:"date"`
}

// propJSON is one shape data row as read shows it.
type propJSON struct {
	ID     string `json:"id"`
	Label  string `json:"label"`
	Type   string `json:"type"`
	Value  any    `json:"value"`
	Format string `json:"format,omitempty"`
	Unit   string `json:"unit,omitempty"`
	Hidden bool   `json:"hidden,omitempty"`
	Linked bool   `json:"linked,omitempty"`
}

// cellJSON is one stored cell as --detail full shows it.
type cellJSON struct {
	N string `json:"n"`
	V string `json:"v"`
	U string `json:"u,omitempty"`
	F string `json:"f,omitempty"`
}

// omissionJSON is one omitted property as --detail full shows it.
type omissionJSON struct {
	Prop   string `json:"prop"`
	Reason string `json:"reason"`
}

// objectJSON is one object as read shows it, with keys in the golden layout order.
type objectJSON struct {
	ID       string            `json:"id"`
	Kind     string            `json:"kind"`
	Type     string            `json:"type,omitempty"`
	Name     string            `json:"name,omitempty"`
	NameU    string            `json:"name_u,omitempty"`
	Master   string            `json:"master,omitempty"`
	Parent   string            `json:"parent,omitempty"`
	Hidden   bool              `json:"hidden,omitempty"`
	Box      []json.Number     `json:"box,omitempty"`
	Rot      json.Number       `json:"rot,omitempty"`
	FlipH    bool              `json:"flip_h,omitempty"`
	FlipV    bool              `json:"flip_v,omitempty"`
	From     []json.Number     `json:"from,omitempty"`
	To       []json.Number     `json:"to,omitempty"`
	FromGlue string            `json:"from_glue,omitempty"`
	ToGlue   string            `json:"to_glue,omitempty"`
	Glued    []gluedJSON       `json:"glued,omitempty"`
	Fill     any               `json:"fill,omitempty"`
	Line     any               `json:"line,omitempty"`
	HTML     string            `json:"html,omitempty"`
	Text     string            `json:"text,omitempty"`
	Props    []propJSON        `json:"props,omitempty"`
	Layers   []string          `json:"layers,omitempty"`
	Sites    [][]json.Number   `json:"sites,omitempty"`
	Z        *int              `json:"z,omitempty"`
	Origins  map[string]string `json:"origins,omitempty"`
	Cells    []cellJSON        `json:"cells,omitempty"`
	Omitted  []omissionJSON    `json:"omitted,omitempty"`
}

// Page reads one page (or master, for an address master<N>) as JSON in
// the layout pptx read --slide uses: base, page, name, units, size,
// orientation, background, background_page, layers, objects,
// connections, hazards, notes. Chunk R2.
func Page(input []byte, base, address string, o Options) ([]byte, error) {
	in := o.Units == "in"
	units := "pt"
	if in {
		units = "in"
	}
	full := o.Detail == "full"
	mo := model.Options{Sites: o.Detail == "sites" || full, Full: full}
	a, err := vsd.Parse(address)
	if err != nil {
		return nil, cli.Errorf(cli.CodeNotFound, "", "%s", err)
	}
	if a.Kind != vsd.KindPage && a.Kind != vsd.KindMaster {
		return nil, cli.Errorf(cli.CodeNotFound, "", "no page %q in the drawing's page list", address)
	}
	pkg, d, r, err := openDoc(input)
	if err != nil {
		return nil, err
	}
	defer pkg.Close()
	var p *model.Page
	if a.Kind == vsd.KindMaster {
		m, ok := d.Master(a.Container)
		if !ok {
			return nil, cli.Errorf(cli.CodeNotFound, "--master", "no master %s: not found", address)
		}
		p, err = model.BuildMaster(d, r, m, mo)
		if err != nil {
			return nil, &cli.Error{Code: cli.CodeUnreadableTarget, Path: "--master", Message: "the master cannot be read", Detail: err.Error(), Cause: err}
		}
	} else {
		e, ok := pageEntry(d, a.Container, a.PageDup)
		if !ok {
			return nil, cli.Errorf(cli.CodeNotFound, "--page", "no page %q in the drawing's page list", address)
		}
		p, err = model.BuildPage(d, r, e, mo)
		if err != nil {
			return nil, &cli.Error{Code: cli.CodeUnreadableTarget, Path: "--page", Message: "the page cannot be read", Detail: err.Error(), Cause: err}
		}
	}
	head := pageHeadJSON{Base: base, Page: p.ID, Name: p.Name, Units: units,
		Orientation: p.Orientation, PrintOrientation: p.PrintOrientation,
		Background: p.Background, BackgroundPage: p.BackPage,
		Layers: []layerJSON{}}
	if p.HasSize {
		if in {
			head.Size = []json.Number{json.Number(vsd.FormatInches(p.W)), json.Number(vsd.FormatInches(p.H))}
		} else {
			head.Size = []json.Number{json.Number(geom.FormatNum(p.W * vsd.PointsPerInch)), json.Number(geom.FormatNum(p.H * vsd.PointsPerInch))}
		}
	}
	for _, l := range p.Layers {
		head.Layers = append(head.Layers, layerJSON{ID: l.ID, Name: l.Name, Index: l.Index, Visible: l.Visible, Print: l.Print, Lock: l.Lock})
	}
	tail := pageTailJSON{Connections: []connectionJSON{}, Hazards: []hazardJSON{}, Notes: []Note{}}
	for _, c := range p.Connections {
		tail.Connections = append(tail.Connections, connectionJSON{Connector: c.Connector, From: c.From, To: c.To, FromGlue: c.FromGlue, ToGlue: c.ToGlue})
	}
	for _, h := range p.Hazards {
		tail.Hazards = append(tail.Hazards, hazardJSON{Kind: h.Kind, ID: h.ID, Detail: h.Detail})
	}
	for _, n := range d.Notes {
		tail.Notes = append(tail.Notes, Note{At: n.At, Reason: n.Reason})
	}
	for _, n := range p.Notes {
		tail.Notes = append(tail.Notes, Note{At: n.At, Reason: n.Reason})
	}
	for _, n := range r.Notes() {
		tail.Notes = append(tail.Notes, Note{At: n.At, Reason: n.Reason})
	}
	return marshalPage(head, tail, selectObjects(p, o), in, full)
}

// marshalPage writes a page result into one buffer: the marshalled
// head (without its closing brace), then the objects one by one,
// then the marshalled tail (without its opening brace). No slice of
// per-object copies is ever held: each object is marshalled and
// appended before the next is converted, so the live set is the
// model plus the output buffer.
func marshalPage(head pageHeadJSON, tail pageTailJSON, objects []*model.Object, in, full bool) ([]byte, error) {
	headB, err := cli.Marshal(head)
	if err != nil {
		return nil, err
	}
	tailB, err := cli.Marshal(tail)
	if err != nil {
		return nil, err
	}
	out := make([]byte, 0, len(headB)+len(tailB)+len(objects)*384)
	out = append(out, headB[:len(headB)-1]...)
	out = append(out, `,"objects":[`...)
	for i, obj := range objects {
		if i > 0 {
			out = append(out, ',')
		}
		b, err := cli.Marshal(objectJSONOf(obj, in, full))
		if err != nil {
			return nil, err
		}
		out = append(out, b...)
	}
	out = append(out, ']', ',')
	out = append(out, tailB[1:]...)
	return append(out, '\n'), nil
}

// lengthNum formats a length in view points in the selected units.
func lengthNum(pts float64, in bool) json.Number {
	if in {
		return json.Number(vsd.FormatInches(pts / vsd.PointsPerInch))
	}
	return json.Number(geom.FormatNum(pts))
}

// boxJSON formats a view box in the selected units.
func boxJSON(b *geom.Box, in bool) []json.Number {
	if b == nil {
		return nil
	}
	return []json.Number{lengthNum(b.X, in), lengthNum(b.Y, in), lengthNum(b.W, in), lengthNum(b.H, in)}
}

// pointJSON formats a view point in the selected units.
func pointJSON(p *geom.Pt, in bool) []json.Number {
	if p == nil {
		return nil
	}
	return []json.Number{lengthNum(p.X, in), lengthNum(p.Y, in)}
}

// storedMaster returns the master address the shape element itself stores,
// "" when it stores none.
func storedMaster(obj *model.Object) string {
	if obj.Node == nil {
		return ""
	}
	v, ok := obj.Node.Attr("Master")
	if !ok {
		return ""
	}
	u, err := strconv.ParseUint(v, 10, 32)
	if err != nil {
		return ""
	}
	return vsd.MasterID(uint32(u))
}

// fillJSON shows a fill as read shows it.
func fillJSON(p *model.Paint) any {
	if p == nil {
		return nil
	}
	if p.None {
		return "none"
	}
	if p.Hex == "" {
		return nil
	}
	return hexJSON{Hex: p.Hex}
}

// lineJSONOf shows a line as read shows it.
func lineJSONOf(l *model.Line, in bool) any {
	if l == nil {
		return nil
	}
	if l.None {
		return "none"
	}
	var lj lineJSON
	if l.HasW {
		lj.W = lengthNum(l.W, in)
	}
	if l.Color != "" {
		lj.Color = &hexJSON{Hex: l.Color}
	}
	lj.Dash = l.Dash
	if lj.W == "" && lj.Color == nil && lj.Dash == "" {
		return nil
	}
	return lj
}

// propValue shows a shape data value as read shows it.
func propValue(v any) any {
	switch t := v.(type) {
	case float64:
		return json.Number(vsd.FormatInches(t))
	case bool:
		return t
	case string:
		return t
	case map[string]string:
		return dateJSON{Date: t["date"]}
	default:
		return t
	}
}

// objectJSONOf converts one model object into its read form.
func objectJSONOf(obj *model.Object, in, full bool) objectJSON {
	j := objectJSON{ID: obj.ID, Kind: string(obj.Kind), Name: obj.Name, Parent: obj.Parent,
		FromGlue: obj.FromGlue, ToGlue: obj.ToGlue}
	if obj.Kind == model.Other {
		j.Type = obj.Type
	}
	if obj.NameU != "" && obj.NameU != obj.Name {
		j.NameU = obj.NameU
	}
	j.Master = storedMaster(obj)
	j.Box = boxJSON(obj.Box, in)
	if obj.Box != nil {
		if obj.Box.Rot != 0 {
			j.Rot = json.Number(geom.FormatNum(obj.Box.Rot))
		}
		j.FlipH, j.FlipV = obj.Box.FlipH, obj.Box.FlipV
	}
	j.From, j.To = pointJSON(obj.From, in), pointJSON(obj.To, in)
	for _, g := range obj.Glued {
		j.Glued = append(j.Glued, gluedJSON{Cell: g.Cell, To: g.To})
	}
	j.Fill = fillJSON(obj.Fill)
	j.Line = lineJSONOf(obj.Line, in)
	if obj.HasText {
		j.HTML, j.Text = obj.HTML, obj.Text
	}
	for _, pr := range obj.Props {
		j.Props = append(j.Props, propJSON{ID: pr.ID, Label: pr.Label, Type: pr.Type, Value: propValue(pr.Value), Format: pr.Format, Unit: pr.Unit, Hidden: pr.Hidden, Linked: pr.Linked})
	}
	j.Layers = obj.Layers
	for _, s := range obj.Sites {
		j.Sites = append(j.Sites, []json.Number{lengthNum(s.Pt.X, in), lengthNum(s.Pt.Y, in)})
	}
	if full {
		z := obj.Z
		j.Z = &z
		if len(obj.Origins) > 0 {
			j.Origins = make(map[string]string, len(obj.Origins))
			for k, v := range obj.Origins {
				j.Origins[k] = string(v)
			}
		}
		for _, c := range obj.Cells {
			j.Cells = append(j.Cells, cellJSON{N: c.Key, V: c.V, U: c.U, F: c.F})
		}
		for _, om := range obj.Omitted {
			j.Omitted = append(j.Omitted, omissionJSON{Prop: om.Prop, Reason: om.Reason})
		}
	}
	return j
}

// selectObjects returns the objects of p kept by o.IDs and o.Region, in paint order.
func selectObjects(p *model.Page, o Options) []*model.Object {
	if len(o.IDs) == 0 && o.Region == nil {
		return p.Objects
	}
	byID := make(map[string]*model.Object, len(p.Objects))
	for _, obj := range p.Objects {
		byID[obj.ID] = obj
	}
	ids := make(map[string]bool, len(o.IDs))
	for _, id := range o.IDs {
		ids[id] = true
	}
	var out []*model.Object
	for _, obj := range p.Objects {
		if len(o.IDs) > 0 && !inIDs(obj, ids, byID) {
			continue
		}
		if o.Region != nil && !intersects(obj, o) {
			continue
		}
		out = append(out, obj)
	}
	return out
}

// shortID returns the part of address after the last slash, the short id.
func shortID(id string) string {
	if i := strings.LastIndex(id, "/"); i >= 0 {
		return id[i+1:]
	}
	return id
}

// inIDs reports whether obj or any of its ancestors is in ids.
func inIDs(obj *model.Object, ids map[string]bool, byID map[string]*model.Object) bool {
	seen := map[string]bool{}
	for cur := obj; cur != nil; {
		if ids[cur.ID] || ids[shortID(cur.ID)] {
			return true
		}
		if cur.Parent == "" || seen[cur.Parent] {
			return false
		}
		seen[cur.ID] = true
		cur = byID[cur.Parent]
	}
	return false
}

// intersects reports whether an object's box intersects the selected region.
func intersects(obj *model.Object, o Options) bool {
	if obj.Box == nil || o.Region == nil {
		return false
	}
	in := o.Units == "in"
	x, y, w, h := obj.Box.X, obj.Box.Y, obj.Box.W, obj.Box.H
	if in {
		x, y, w, h = x/vsd.PointsPerInch, y/vsd.PointsPerInch, w/vsd.PointsPerInch, h/vsd.PointsPerInch
	}
	r := o.Region
	return x+w >= (*r)[0] && x <= (*r)[0]+(*r)[2] && y+h >= (*r)[1] && y <= (*r)[1]+(*r)[3]
}
