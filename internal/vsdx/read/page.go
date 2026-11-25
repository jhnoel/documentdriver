package read

import (
	"bytes"
	"encoding/json"
	"strings"

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

// patternJSON is a pattern fill as read shows it (docs/contract.md §3,
// Pattern fills): the foreground colour, the pattern's name or stored
// number, and the background, a colour or "none" (left out when not
// derived).
type patternJSON struct {
	Hex     string `json:"hex"`
	Pattern string `json:"pattern"`
	Bg      any    `json:"bg,omitempty"`
}

// lineJSON is a line as read shows it.
type lineJSON struct {
	W     json.Number  `json:"w,omitempty"`
	Color *hexJSON     `json:"color,omitempty"`
	Dash  string       `json:"dash,omitempty"`
	Begin *lineEndJSON `json:"begin,omitempty"`
	End   *lineEndJSON `json:"end,omitempty"`
}

// lineEndJSON is one arrowhead as read shows it (docs/contract.md §3).
type lineEndJSON struct {
	Arrow string      `json:"arrow"`
	Size  json.Number `json:"size,omitempty"`
}

// mediaJSON is a picture's media facts as read shows them (§8).
type mediaJSON struct {
	Type   string `json:"type"`
	Bytes  int    `json:"bytes"`
	SHA256 string `json:"sha256"`
	Px     []int  `json:"px,omitempty"`
}

// shadowJSON is a shadow as read shows it (docs/contract.md §8).
type shadowJSON struct {
	Color        *hexJSON      `json:"color,omitempty"`
	Transparency json.Number   `json:"transparency,omitempty"`
	Offset       []json.Number `json:"offset,omitempty"`
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
	Value  any    `json:"value,omitzero"` // absent, never null, when there is none (§4)
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
	ID        string            `json:"id"`
	Kind      string            `json:"kind"`
	Type      string            `json:"type,omitempty"`
	Name      string            `json:"name,omitempty"`
	NameU     string            `json:"name_u,omitempty"`
	Master    string            `json:"master,omitempty"`
	Parent    string            `json:"parent,omitempty"`
	Hidden    bool              `json:"hidden,omitempty"`
	Box       []json.Number     `json:"box,omitempty"`
	Rot       json.Number       `json:"rot,omitempty"`
	FlipH     bool              `json:"flip_h,omitempty"`
	FlipV     bool              `json:"flip_v,omitempty"`
	Geom      string            `json:"geom,omitempty"`
	Closed    bool              `json:"closed,omitempty"`
	Points    [][]json.Number   `json:"points,omitempty"`
	From      []json.Number     `json:"from,omitempty"`
	To        []json.Number     `json:"to,omitempty"`
	FromGlue  string            `json:"from_glue,omitempty"`
	ToGlue    string            `json:"to_glue,omitempty"`
	Glued     []gluedJSON       `json:"glued,omitempty"`
	Fill      any               `json:"fill,omitempty"`
	Line      any               `json:"line,omitempty"`
	Shadow    *shadowJSON       `json:"shadow,omitempty"`
	TextBlock *textBlockJSON    `json:"text_block,omitempty"`
	Image     string            `json:"image,omitempty"`
	Media     *mediaJSON        `json:"media,omitempty"`
	Crop      []json.Number     `json:"crop,omitempty"`
	HTML      string            `json:"html,omitempty"`
	Text      string            `json:"text,omitempty"`
	Props     []propJSON        `json:"props,omitempty"`
	Layers    []string          `json:"layers,omitempty"`
	Sites     [][]json.Number   `json:"sites,omitempty"`
	Z         *int              `json:"z,omitempty"`
	Origins   map[string]string `json:"origins,omitempty"`
	Cells     []cellJSON        `json:"cells,omitempty"`
	Omitted   []omissionJSON    `json:"omitted,omitempty"`
}

// Page reads one page (or master, for an address master<N>) as JSON in
// the layout pptx read --slide uses: base, page, name, units, size,
// orientation, background, background_page, layers, objects,
// connections, hazards, notes. Chunk R2. Page is the bytes.Buffer
// wrapper over WritePage, for tests and goldens; the CLI streams
// through WritePage itself (chunk RM3).
func Page(input []byte, base, address string, o Options) ([]byte, error) {
	var buf bytes.Buffer
	if err := WritePage(&buf, input, base, address, o); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
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

// storedMaster returns the master address the shape element itself
// stores, "" when it stores none: the StoredMaster the walk set, so
// it needs no node (Page releases the nodes before marshalling).
func storedMaster(obj *model.Object) string {
	if obj == nil {
		return ""
	}
	return obj.StoredMaster
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
	if p.Pattern != "" {
		pj := patternJSON{Hex: p.Hex, Pattern: p.Pattern}
		switch {
		case p.BgNone:
			pj.Bg = "none"
		case p.BgHex != "":
			pj.Bg = hexJSON{Hex: p.BgHex}
		}
		return pj
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
	lj.Begin, lj.End = lineEndJSONOf(l.Begin), lineEndJSONOf(l.End)
	if lj.W == "" && lj.Color == nil && lj.Dash == "" && lj.Begin == nil && lj.End == nil {
		return nil
	}
	return lj
}

// lineEndJSONOf shows one arrowhead, nil when there is none.
func lineEndJSONOf(e *model.LineEnd) *lineEndJSON {
	if e == nil {
		return nil
	}
	j := &lineEndJSON{Arrow: e.Arrow}
	if e.HasSize {
		j.Size = json.Number(geom.FormatNum(e.Size))
		if e.Size == 0 {
			j.Size = "0"
		}
	}
	return j
}

// shadowJSONOf shows a shadow, nil when there is none.
func shadowJSONOf(s *model.Shadow, in bool) *shadowJSON {
	if s == nil {
		return nil
	}
	j := &shadowJSON{}
	if s.HasColor {
		j.Color = &hexJSON{Hex: s.Color}
	}
	if s.HasTrans {
		j.Transparency = json.Number(geom.FormatNum(s.Transparency))
		if s.Transparency == 0 {
			j.Transparency = "0"
		}
	}
	if s.HasOffset {
		j.Offset = []json.Number{lengthNum(s.Offset[0], in), lengthNum(s.Offset[1], in)}
	}
	return j
}

// textBlockJSON is an object's text block as read shows it.
type textBlockJSON struct {
	Box        []json.Number `json:"box,omitempty"`
	Angle      json.Number   `json:"angle,omitempty"`
	VAlign     string        `json:"valign,omitempty"`
	Margins    []json.Number `json:"margins,omitempty"`
	Background any           `json:"background,omitempty"`
}

// textBgJSON is a text background colour: hex, and its transparency
// in percent when not 0.
type textBgJSON struct {
	Hex          string      `json:"hex"`
	Transparency json.Number `json:"transparency,omitempty"`
}

// textBlockJSONOf shows a text block: box (the block's place on the
// page) and angle (when not 0) in the selected units, valign, margins
// and background "none" or {hex, transparency}, each when derived.
func textBlockJSONOf(tb *model.TextBlockInfo, in bool) *textBlockJSON {
	j := &textBlockJSON{}
	if tb.HasBox {
		b := tb.Box
		j.Box = boxJSON(&b, in)
		if b.Rot != 0 {
			j.Angle = json.Number(geom.FormatNum(b.Rot))
		}
	}
	if tb.HasVAlign {
		j.VAlign = tb.VAlign
	}
	if tb.HasMargins {
		for _, m := range tb.Margins {
			n := lengthNum(m, in)
			if m == 0 {
				n = "0"
			}
			j.Margins = append(j.Margins, n)
		}
	}
	switch {
	case !tb.HasBg:
	case tb.BgNone:
		j.Background = "none"
	default:
		bg := textBgJSON{Hex: tb.BgHex}
		if tb.BgTrans != 0 {
			bg.Transparency = json.Number(geom.FormatNum(tb.BgTrans))
		}
		j.Background = bg
	}
	return j
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
	j.Geom, j.Closed = obj.Geom, obj.Closed
	for i := range obj.Points {
		j.Points = append(j.Points, pointJSON(&obj.Points[i], in))
	}
	j.From, j.To = pointJSON(obj.From, in), pointJSON(obj.To, in)
	for _, g := range obj.Glued {
		j.Glued = append(j.Glued, gluedJSON{Cell: g.Cell, To: g.To})
	}
	j.Fill = fillJSON(obj.Fill)
	j.Line = lineJSONOf(obj.Line, in)
	j.Shadow = shadowJSONOf(obj.Shadow, in)
	if tb := obj.TextBlock; tb != nil && tb.Local {
		j.TextBlock = textBlockJSONOf(tb, in)
	}
	if pic := obj.Picture; pic != nil {
		j.Image = pic.Image
		j.Media = &mediaJSON{Type: pic.Type, Bytes: pic.Bytes, SHA256: pic.SHA256}
		if pic.HasPx {
			j.Media.Px = []int{pic.Px[0], pic.Px[1]}
		}
		if pic.HasCrop {
			for _, v := range pic.Crop {
				j.Crop = append(j.Crop, json.Number(geom.FormatNum(v)))
			}
		}
	}
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
