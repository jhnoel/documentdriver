package ops

import (
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"math"
	"slices"
	"strconv"
	"strings"

	"github.com/jhnoel/documentdriver/internal/pptx/derive"
	"github.com/jhnoel/documentdriver/internal/pptx/ops/edit"
	"github.com/jhnoel/documentdriver/internal/pptx/ops/prop"
	"github.com/jhnoel/documentdriver/internal/pptx/ops/text"
	"github.com/jhnoel/documentdriver/internal/pptx/show"
)

// Set is object.set: every field the plan writes is set through its
// property codec, and every other property keeps the value shown. A field
// written as shown is a no-op that touches no XML.
type Set struct {
	Slide string
	Units prop.Units
	Spec  SetSpec
}

// Name implements Op.
func (*Set) Name() string { return "object.set" }

// apply writes every field the plan set through its property codec.
func (o *Set) apply(t *Tx) error {
	h, err := t.target(o.Slide, o.Spec.Target)
	if err != nil {
		return err
	}
	s := o.Spec
	if err := s.fields().write(t, h, o.Slide, o.Units); err != nil {
		return fmt.Errorf("%s: %w", h.Ref(), err)
	}
	if s.HTML.Set {
		if err := text.SetWith(h, s.HTML.V, h.Rels()); err != nil {
			return fmt.Errorf("%s: html: %w", h.Ref(), err)
		}
	}
	if s.Text.Set {
		if err := writeBody(h, o.Units, s.Text.V); err != nil {
			return fmt.Errorf("%s: %w", h.Ref(), err)
		}
	}
	return nil
}

// writeBody sets a shape's text body properties, after its html (a
// shape without a text body gets one only from html); properties not
// given keep the value shown. A change of autofit mode drops the scales
// of the old a:normAutofit; a new a:normAutofit holds only the scales the
// plan gives (see "Text writes (M3)": PowerPoint applies the stored
// scale and does not recompute it on open, so none is invented).
func writeBody(h *edit.Handle, u prop.Units, b BodySpec) error {
	c := prop.Body(u)
	cur := c.Read(h, 0)
	shown, ok := cur.Get()
	if !ok {
		return fmt.Errorf("text: %w: %s; give html to create one", prop.ErrNoTextBody, cur.Reason())
	}
	vals, err := b.attrs(u)
	if err != nil {
		return err
	}
	want := prop.BodyVal{Attrs: map[string]string{}, Autofit: shown.Autofit}
	maps.Copy(want.Attrs, shown.Attrs)
	for _, a := range derive.BodyAttrs {
		if v, ok := vals[a]; ok {
			want.Attrs[a] = v
		}
	}
	if b.Autofit.Set && b.Autofit.V != shown.Autofit.Mode {
		want.Autofit = derive.Autofit{Mode: b.Autofit.V}
	}
	if v, ok := vals["fontScale"]; ok {
		want.Autofit.FontScale = v
	}
	if v, ok := vals["lnSpcReduction"]; ok {
		want.Autofit.LnSpcReduction = v
	}
	return prop.Set(h, c, want)
}

// fields are the properties object.add and object.set write, each
// written only when set.
type fields struct {
	Name Opt[string]
	Transform
	Geom   Opt[string]
	Adj    Opt[map[string]any]
	Fill   Opt[FillSpec]
	Line   Opt[LineSpec]
	Start  Opt[string]
	End    Opt[string]
	From   Opt[[2]float64]
	To     Opt[[2]float64]
	First  Opt[string]
	Elbows Opt[[][2]float64]
	// Path and Custom are the custom geometry, written after the
	// transform, to which paths are relative.
	Path   Opt[PathSpec]
	Custom Opt[CustomSpec]
	Pic    PictureFields
	// Body (the plan's text) is checked with the rest at compile, but
	// written after html by the operation, since only html creates a
	// text body.
	Body Opt[BodySpec]
	NonVisual
}

// fields returns the properties object.set writes, each only when set.
func (s *SetSpec) fields() fields {
	return fields{Name: s.Name, Transform: s.Transform, Geom: s.Geom, Adj: s.Adj, Fill: s.Fill, Line: s.Line,
		Start: s.Start, End: s.End, From: s.From, To: s.To, First: s.First, Elbows: s.Elbows, Body: s.Text, Path: s.Path, Custom: s.Custom,
		NonVisual: s.NonVisual, Pic: s.PictureFields}
}

// fields returns the properties object.add writes, each only when set.
func (s *AddSpec) fields() fields {
	return fields{Name: s.Name, Transform: s.Transform, Geom: s.Geom, Adj: s.Adj, Fill: s.Fill, Line: s.Line,
		Start: s.Start, End: s.End, From: s.From, To: s.To, First: s.First, Body: s.Text, Path: s.Path, Custom: s.Custom,
		NonVisual: s.NonVisual, Pic: s.PictureFields}
}

// writeNonVisual sets the non-visual properties given: descr, title,
// hidden, txBox.
func (n NonVisual) write(h *edit.Handle) error {
	for _, s := range []struct {
		attr string
		v    Opt[string]
	}{{"descr", n.Descr}, {"title", n.Title}} {
		if s.v.Set {
			if err := prop.Set(h, prop.CNvPrText(s.attr), s.v.V); err != nil {
				return err
			}
		}
	}
	if n.Hidden.Set {
		if err := prop.Set(h, prop.Hidden(), n.Hidden.V); err != nil {
			return err
		}
	}
	if n.TxBox.Set {
		if err := prop.Set(h, prop.TxBox(), n.TxBox.V); err != nil {
			return err
		}
	}
	return nil
}

// write sets the fields in a fixed order: name and the other non-visual
// properties, geometry, transform or endpoints (which depend on the
// geometry), glue, fill, line.
func (f fields) write(t *Tx, h *edit.Handle, slide string, u prop.Units) error {
	if f.Name.Set {
		if err := prop.Set(h, prop.Name(), f.Name.V); err != nil {
			return err
		}
	}
	if err := f.NonVisual.write(h); err != nil {
		return err
	}
	if f.Geom.Set || f.Adj.Set || f.Path.Set || f.Custom.Set {
		if err := writeGeom(h, f.Geom, f.Adj, f.Path.Set || f.Custom.Set); err != nil {
			return err
		}
	}
	ends := f.From.Set || f.To.Set || f.First.Set
	if ends && (f.Box.Set || f.Rot.Set || f.FlipH.Set || f.FlipV.Set) {
		return errors.New("give a connector's from and to, or its box, not both")
	}
	if err := setTransform(h, u, f.Transform); err != nil {
		return err
	}
	if ends {
		if err := writeEnds(h, u, f.From, f.To, f.First); err != nil {
			return err
		}
	}
	if f.Elbows.Set {
		if err := checkElbows(h, u, f.Elbows.V); err != nil {
			return err
		}
	}
	// Paths are relative to the box, so they follow the transform.
	if f.Path.Set {
		want, err := f.Path.V.value(u)
		if err != nil {
			return err
		}
		if err := prop.Set(h, prop.Path(u), want); err != nil {
			return err
		}
	}
	if f.Custom.Set {
		base, _ := prop.Custom(u).Read(h, 0).Get()
		want, err := f.Custom.V.value(u, base)
		if err != nil {
			return err
		}
		if err := prop.Set(h, prop.Custom(u), want); err != nil {
			return err
		}
	}
	for _, g := range []struct {
		el   string
		spec Opt[string]
	}{{"stCxn", f.Start}, {"endCxn", f.End}} {
		if !g.spec.Set {
			continue
		}
		want, err := t.glueEnd(slide, g.spec.V)
		if err != nil {
			return err
		}
		if err := prop.Set(h, prop.Glue(g.el), want); err != nil {
			return err
		}
	}
	if f.Fill.Set {
		want, err := fillValue(h, f.Fill.V)
		if err != nil {
			return err
		}
		if err := prop.Set(h, prop.Fill(), want); err != nil {
			return err
		}
	}
	if f.Line.Set {
		c := prop.Line(u)
		want, err := lineValue(h, c, u, f.Line.V)
		if err != nil {
			return err
		}
		if err := prop.Set(h, c, want); err != nil {
			return err
		}
	}
	if f.Pic.any() {
		if err := t.writePicture(h, f.Pic); err != nil {
			return err
		}
	}
	return nil
}

// writeGeom sets the preset and adjust values. Without geom, adj updates
// the current preset's values by name (others are kept); a new preset
// starts from the adjust values given, since the old preset's mean
// nothing to it.
func writeGeom(h *edit.Handle, geom Opt[string], adj Opt[map[string]any], custom bool) error {
	switch h.Kind() {
	case "grpSp":
		return errors.New("geom, path: a group has no geometry")
	case "cxnSp":
		if custom || geom.V == "custom" {
			return errors.New("path: a connector is drawn by its preset; draw an open path as a shape (kind shape, path, fill none)")
		}
	}
	c := prop.Geometry()
	cur, ok := c.Read(h, 0).Get()
	if geom.V == "custom" || (custom && !geom.Set) {
		// Custom geometry is written by path and custom, after the
		// transform; here only what they cannot say is checked.
		switch {
		case adj.Set:
			return errors.New("adj is for presets; a custom geometry's adjust values and guides are in custom (av, gd), read-only")
		case custom:
			return nil
		case ok && cur.Custom:
			return nil // written back as shown
		}
		return fmt.Errorf("geom custom: %w: path (the custom geometry's path)", ErrMissing)
	}
	if custom {
		return fmt.Errorf("path, custom: geom %s is a preset; give geom custom (or leave geom out) with a path", geom.V)
	}
	if ok && cur.Custom && !geom.Set {
		return errors.New("adj is for presets; a custom geometry's adjust values and guides are in custom (av, gd), read-only")
	}
	want := prop.GeomVal{}
	switch {
	case geom.Set:
		// A preset over a custom geometry replaces it: the explicit
		// switch from custGeom to prstGeom.
		want.Preset = geom.V
		if ok && cur.Preset == geom.V {
			want.Adj = slices.Clone(cur.Adj)
		}
	case ok:
		want = cur
		want.Adj = slices.Clone(cur.Adj)
	default:
		return fmt.Errorf("adj: %w: geom (the object has no geometry to adjust)", ErrMissing)
	}
	if adj.Set {
		names := slices.Sorted(maps.Keys(adj.V))
		for _, name := range names {
			fmla, err := adjFormula(name, adj.V[name])
			if err != nil {
				return err
			}
			if k := slices.IndexFunc(want.Adj, func(a derive.Adjust) bool { return a.Name == name }); k >= 0 {
				want.Adj[k].Fmla = fmla
			} else {
				want.Adj = append(want.Adj, derive.Adjust{Name: name, Fmla: fmla})
			}
		}
	}
	return prop.Set(h, c, want)
}

// adjFormula turns an adjust value as the views show it (a number for
// "val N", else the formula) into its formula.
func adjFormula(name string, v any) (string, error) {
	switch x := v.(type) {
	case float64:
		if x != math.Trunc(x) || math.Abs(x) > math.MaxInt32 {
			return "", fmt.Errorf("adj %s: %v is not a whole number", name, x)
		}
		return "val " + strconv.FormatInt(int64(x), 10), nil
	case string:
		if strings.TrimSpace(x) == "" {
			return "", fmt.Errorf("adj %s: empty formula", name)
		}
		return x, nil
	}
	return "", fmt.Errorf("adj %s: want a number or a formula", name)
}

// writeEnds sets a connector's endpoints; fields not given keep the
// value shown (an elbow connector keeps its first-segment direction).
func writeEnds(h *edit.Handle, u prop.Units, from, to Opt[[2]float64], first Opt[string]) error {
	c := prop.Endpoints(u)
	var want prop.Ends
	if cur, ok := c.Read(h, 0).Get(); ok {
		want = cur
	} else if !from.Set || !to.Set {
		return fmt.Errorf("%w: from and to (the endpoints are not derived: %s)", ErrMissing, c.Read(h, 0).Reason())
	}
	if from.Set {
		want.From = derive.Pt{X: u.ToEMU(from.V[0]), Y: u.ToEMU(from.V[1])}
	}
	if to.Set {
		want.To = derive.Pt{X: u.ToEMU(to.V[0]), Y: u.ToEMU(to.V[1])}
	}
	if first.Set {
		if first.V != "h" && first.V != "v" {
			return fmt.Errorf("first %q: want h or v", first.V)
		}
		want.First = first.V
	}
	if g, ok := prop.Geometry().Read(h, 0).Get(); ok && !strings.HasPrefix(g.Preset, "bentConnector") {
		if first.Set {
			return fmt.Errorf("first is for elbow connectors (bentConnector2-5), not %s", g.Preset)
		}
		want.First = ""
	}
	return prop.Set(h, c, want)
}

// checkElbows accepts elbows written back as shown; moving them is done
// through adj (the elbow positions as fractions of the box).
func checkElbows(h *edit.Handle, u prop.Units, want [][2]float64) error {
	g, ok := prop.Geometry().Read(h, 0).Get()
	if !ok || !strings.HasPrefix(g.Preset, "bentConnector") {
		return errors.New("elbows are for elbow connectors (bentConnector2-5)")
	}
	cur, err := elbows(h)
	if err != nil {
		return err
	}
	if len(cur) == len(want) {
		same := true
		for i, p := range cur {
			if u.Length(p.X) != derive.FormatNum(want[i][0]) || u.Length(p.Y) != derive.FormatNum(want[i][1]) {
				same = false
			}
		}
		if same {
			return nil
		}
	}
	return fmt.Errorf("elbows: %w; move elbows through adj (adj1..adj3, in 100000ths of the connector's box)", ErrNotImplemented)
}

// elbows reads the elbow points the views show.
func elbows(h *edit.Handle) ([]derive.Pt, error) {
	g, ok := prop.Geometry().Read(h, 0).Get()
	if !ok {
		return nil, errors.New("no geometry")
	}
	m, w, hh, err := prop.LocalFrame(h)
	if err != nil {
		return nil, err
	}
	c, err := derive.ConnectorGeom(g.Preset, g.Adj, w, hh, m)
	if err != nil {
		return nil, err
	}
	return c.Elbows, nil
}

// glueEnd parses a glue reference as the views show it ("s12:3",
// "$cache:1") or "none", resolving the object on the slide.
func (t *Tx) glueEnd(slide, s string) (*prop.GlueEnd, error) {
	if s == "none" {
		return nil, nil
	}
	i := strings.LastIndexByte(s, ':')
	if i < 0 {
		return nil, fmt.Errorf("glue %q: want object:site, such as s12:3 or $cache:1, or none", s)
	}
	site, err := strconv.Atoi(s[i+1:])
	if err != nil || site < 0 {
		return nil, fmt.Errorf("glue %q: site %q is not a site index", s, s[i+1:])
	}
	if raw, ok := strings.CutPrefix(s[:i], "#"); ok {
		// A glue id as the views show it when it names no single object
		// (dangling or shared): written as is.
		if raw == "" {
			return nil, fmt.Errorf("glue %q: empty id", s)
		}
		return &prop.GlueEnd{RawID: raw, Site: site}, nil
	}
	ref, err := t.resolve(slide, s[:i])
	if err != nil {
		return nil, fmt.Errorf("glue %q: %w", s, err)
	}
	raw, err := t.ws.GlueID(ref)
	if err != nil {
		return nil, err
	}
	return &prop.GlueEnd{RawID: raw, Site: site, Short: ref.Short()}, nil
}

// colorValue turns a plan colour into the value the views would show,
// with the displayed hex derived on the handle's slide. A hex given next
// to a reference must match the derived one. A system colour's displayed
// value is the lastClr PowerPoint caches beside it: the one prev (the
// colour shown now) carries when it is the same system colour, else the
// hex given when there are no modifiers (as text.set writes it), else
// none.
func colorValue(h *edit.Handle, cs *ColorSpec, prev *derive.ShownColor) (derive.ShownColor, error) {
	var c derive.Color
	kinds := 0
	for _, k := range []struct {
		set        bool
		kind, val  string
		comp       [3]int64
		components bool
	}{
		{cs.Scheme.Set, "scheme", cs.Scheme.V, [3]int64{}, false},
		{cs.RGB.Set, "srgb", strings.ToUpper(strings.TrimPrefix(cs.RGB.V, "#")), [3]int64{}, false},
		{cs.Sys.Set, "sys", cs.Sys.V, [3]int64{}, false},
		{cs.Preset.Set, "prst", cs.Preset.V, [3]int64{}, false},
		{cs.HSL.Set, "hsl", "", cs.HSL.V, true},
		{cs.ScRGB.Set, "scrgb", "", cs.ScRGB.V, true},
	} {
		if k.set {
			kinds++
			c = derive.Color{Kind: k.kind, Val: k.val, Comp: k.comp}
		}
	}
	hexOnly := false
	switch {
	case kinds > 1:
		return derive.ShownColor{}, errors.New("colour: give one of scheme, rgb, sys, preset, hsl, scrgb or hex")
	case kinds == 0 && cs.Hex.Set:
		if cs.Mods.Set {
			return derive.ShownColor{}, errors.New("colour: modifiers go with scheme or rgb, not hex (hex is the displayed value)")
		}
		hexOnly = true
		c = derive.Color{Kind: "srgb", Val: strings.ToUpper(strings.TrimPrefix(cs.Hex.V, "#"))}
	case kinds == 0:
		return derive.ShownColor{}, errors.New("colour: give scheme or hex")
	}
	if c.Kind == "srgb" && !validHex(c.Val) {
		return derive.ShownColor{}, fmt.Errorf("colour: %q is not six hex digits", c.Val)
	}
	if cs.Mods.Set {
		for _, raw := range cs.Mods.V {
			m, err := parseMod(raw)
			if err != nil {
				return derive.ShownColor{}, err
			}
			c.Mods = append(c.Mods, m)
		}
	}
	if c.Kind == "sys" {
		switch {
		case prev != nil && prev.Kind == "sys" && prev.Val == c.Val && prev.LastClr != "":
			c.LastClr = prev.LastClr
		case cs.Hex.Set && len(c.Mods) == 0:
			c.LastClr = strings.ToUpper(strings.TrimPrefix(cs.Hex.V, "#"))
			if !validHex(c.LastClr) {
				return derive.ShownColor{}, fmt.Errorf("colour: hex %q is not #RRGGBB", cs.Hex.V)
			}
		}
	}
	shown, err := prop.ShowColor(h, c)
	if err != nil {
		return shown, err
	}
	if cs.Hex.Set && !hexOnly && !strings.EqualFold(cs.Hex.V, shown.Hex) {
		return shown, fmt.Errorf("colour: hex %s does not match %s, which shows as %s; give the reference or the hex", cs.Hex.V, show.Color(&shown), orUnknown(shown.Hex))
	}
	return shown, nil
}

// orUnknown returns s, or "unresolved" when s is empty.
func orUnknown(s string) string {
	if s == "" {
		return "unresolved"
	}
	return s
}

// validHex reports whether s is exactly six uppercase hexadecimal digits.
func validHex(s string) bool {
	if len(s) != 6 {
		return false
	}
	for _, r := range s {
		if !(r >= '0' && r <= '9' || r >= 'A' && r <= 'F') {
			return false
		}
	}
	return true
}

// parseMod reads one modifier as the views show it: "inv", or
// {"lumMod": 75000}.
func parseMod(raw json.RawMessage) (derive.Mod, error) {
	var name string
	if json.Unmarshal(raw, &name) == nil {
		return derive.Mod{Name: name}, nil
	}
	var m map[string]json.RawMessage
	if err := json.Unmarshal(raw, &m); err != nil || len(m) != 1 {
		return derive.Mod{}, fmt.Errorf("colour modifier %s: want \"name\" or {\"name\": value}", raw)
	}
	for k, v := range m {
		var n json.Number
		if err := json.Unmarshal(v, &n); err == nil {
			if _, err := strconv.ParseInt(string(n), 10, 64); err != nil {
				return derive.Mod{}, fmt.Errorf("colour modifier %s: %s is not a whole number", k, n)
			}
			return derive.Mod{Name: k, Val: string(n)}, nil
		}
		var s string
		if err := json.Unmarshal(v, &s); err != nil {
			return derive.Mod{}, fmt.Errorf("colour modifier %s: want a number", k)
		}
		return derive.Mod{Name: k, Val: s}, nil
	}
	return derive.Mod{}, nil
}

// fillValue turns a plan fill into the value the views would show.
func fillValue(h *edit.Handle, f FillSpec) (derive.Fill, error) {
	if f.None {
		return derive.Fill{Type: "none"}, nil
	}
	if f.Color.Type.Set {
		// A fill shown by type only ({"type": "gradFill"}, or "solid"
		// without a readable colour): writable back unchanged only.
		return derive.Fill{Type: f.Color.Type.V}, nil
	}
	var prev *derive.ShownColor
	if cur, ok := prop.Fill().Read(h, 0).Get(); ok {
		prev = cur.Color
	}
	c, err := colorValue(h, f.Color, prev)
	if err != nil {
		return derive.Fill{}, fmt.Errorf("fill: %w", err)
	}
	return derive.Fill{Type: "solid", Color: &c}, nil
}

// lineValue applies a plan line to the line shown.
func lineValue(h *edit.Handle, c prop.Codec[derive.Line], u prop.Units, l LineSpec) (derive.Line, error) {
	if l.None {
		return derive.Line{None: true}, nil
	}
	cur, _ := c.Read(h, 0).Get()
	want := cur
	if l.Color != nil {
		want.None, want.Color, want.FillXML = false, nil, ""
		if l.Color.Type.Set {
			if cur.FillXML == "" || show.FillType(cur.FillXML) != l.Color.Type.V {
				return want, fmt.Errorf("line color: %w: %s line fills are not editable; give a colour", prop.ErrReadOnly, l.Color.Type.V)
			}
			want.FillXML = cur.FillXML
		} else {
			col, err := colorValue(h, l.Color, cur.Color)
			if err != nil {
				return want, fmt.Errorf("line color: %w", err)
			}
			want.Color = &col
		}
	}
	others := l.W.Set || l.Dash.Set || l.Cap.Set || l.Head.Set || l.Tail.Set
	if want.None && others {
		return want, errors.New("line: the line is none; give line.color to draw it")
	}
	if l.W.Set {
		if l.W.V < 0 {
			return want, errors.New("line w: negative")
		}
		w := u.ToEMU(l.W.V)
		want.W = &w
	}
	if l.Dash.Set {
		want.Dash = l.Dash.V
	}
	if l.Cap.Set {
		want.Cap = l.Cap.V
	}
	for _, e := range []struct {
		spec Opt[string]
		dst  **derive.Arrow
	}{{l.Head, &want.Head}, {l.Tail, &want.Tail}} {
		if !e.spec.Set {
			continue
		}
		a, err := parseArrow(e.spec.V)
		if err != nil {
			return want, err
		}
		*e.dst = a
	}
	return want, nil
}

// parseArrow reads a line end as the views show it: "triangle" or
// "triangle w=sm len=lg".
func parseArrow(s string) (*derive.Arrow, error) {
	f := strings.Fields(s)
	if len(f) == 0 {
		return nil, errors.New("arrow: empty")
	}
	a := &derive.Arrow{Type: f[0]}
	for _, x := range f[1:] {
		k, v, ok := strings.Cut(x, "=")
		switch {
		case ok && k == "w":
			a.W = v
		case ok && k == "len":
			a.Len = v
		default:
			return nil, fmt.Errorf("arrow %q: want type [w=sm|med|lg] [len=sm|med|lg]", s)
		}
	}
	return a, nil
}

// Templates for new objects, trimmed from a PowerPoint-authored deck: the
// LibreOffice test file sd/qa/unit/data/pptx/connectors.pptx (MPL-2.0;
// written by Microsoft Office PowerPoint 16.0000, listed powerpoint_native
// in $PPTX_CORPUS/manifest.jsonl, sha256 in the cn-lc1-square-circle task),
// slide1: the p:sp "Rectangle 1" (id 2) and the p:cxnSp "Connector: Elbow
// 5" (id 6). Kept: the elements CT_Shape and CT_Connector require (the
// non-visual properties with cNvPr, cNvSpPr or cNvCxnSpPr and nvPr, and an
// empty spPr). Removed: the a16:creationId extension, the transform,
// geometry, line, p:style, text body and glue, which the plan supplies
// through the property codecs; the id and name are set by object.add.
var (
	//go:embed fragments/sp.xml
	spTemplate []byte
	//go:embed fragments/cxnSp.xml
	cxnSpTemplate []byte
)

// The picture template, trimmed the same way from a PowerPoint 16 deck:
// ShapeCrawler's ShapeCrawler.Tests/Assets/059_crop-images.pptx (MIT;
// written by Microsoft Office PowerPoint 16.0000, listed powerpoint_native
// in $PPTX_CORPUS/manifest.jsonl, sha256 0fcc2722...c11e7), slide1, the
// p:pic "None" (id 7). Kept: what CT_Picture requires (p:nvPicPr with
// p:cNvPr, p:cNvPicPr and p:nvPr; p:blipFill; p:spPr) and the a:blip that
// holds the image. Removed: the a16:creationId extension, a:picLocks, the
// blip's r:embed and SVG extension, a:stretch with a:fillRect, the
// transform and the geometry, which the plan supplies through the
// picture codecs (locks, image, svg, fit, fillRect, box, geom).
//
//go:embed fragments/pic.xml
var picTemplate []byte

var templateNS = map[string]string{
	"p": derive.PML[0],
	"a": derive.DML[0],
}

// Add is object.add: a new shape or connector, in front of every other
// object on the slide, holding the PowerPoint template above plus what
// the plan specifies, written through the property codecs.
type Add struct {
	Slide string
	Units prop.Units
	Spec  AddSpec
}

// Name implements Op.
func (*Add) Name() string { return "object.add" }

// apply inserts a new object from a template and writes the specified fields.
func (o *Add) apply(t *Tx) error {
	s := o.Spec
	part, err := t.slidePart(o.Slide)
	if err != nil {
		return err
	}
	tmpl := spTemplate
	switch s.Kind {
	case "connector":
		tmpl = cxnSpTemplate
	case "picture":
		tmpl = picTemplate
	}
	at := edit.Front()
	if s.Parent.Set {
		g, err := t.writable(o.Slide, s.Parent.V)
		if err != nil {
			return fmt.Errorf("parent: %w", err)
		}
		if k, _ := t.ws.Kind(g); k != "grpSp" {
			return fmt.Errorf("parent: %s is not a group", g.Short())
		}
		at = edit.InFront(g)
	}
	ref, err := t.ws.InsertObject(part, tmpl, templateNS, at)
	if err != nil {
		return err
	}
	h, err := t.ws.Target(ref)
	if err != nil {
		return err
	}
	if err := t.bind(s.Alias, ref); err != nil {
		return err
	}
	if err := s.fields().write(t, h, o.Slide, o.Units); err != nil {
		return fmt.Errorf("new %s %s: %w", s.Kind, ref.Short(), err)
	}
	// The template has no text body: text.Set gives the shape a p:txBody
	// with an empty a:bodyPr through Ensure, then its paragraphs.
	if s.HTML.Set {
		if err := text.SetWith(h, s.HTML.V, h.Rels()); err != nil {
			return fmt.Errorf("new %s %s: html: %w", s.Kind, ref.Short(), err)
		}
	}
	if s.Text.Set {
		if err := writeBody(h, o.Units, s.Text.V); err != nil {
			return fmt.Errorf("new %s %s: %w", s.Kind, ref.Short(), err)
		}
	}
	return nil
}
