package ops

import (
	"encoding/json"
	"fmt"
	"io"
	"math"
	"strconv"

	"github.com/jhnoel/documentdriver/internal/cli"
)

// Opt is an optional plan field that distinguishes absent from zero: Set
// is true only when the field was written. JSON null is rejected rather
// than read as absent or zero.
type Opt[T any] struct {
	V   T
	Set bool
}

// Some returns a set Opt.
func Some[T any](v T) Opt[T] { return Opt[T]{V: v, Set: true} }

// Slot implements cli.Optional: the place the plan decoder decodes into.
func (o *Opt[T]) Slot() any { return &o.V }

// MarkSet implements cli.Optional.
func (o *Opt[T]) MarkSet() { o.Set = true }

// Plan is a decoded plan: the units of its lengths and its operations in
// order. Every operation names the slide it works on in its address.
type Plan struct {
	// Base is the plan's staleness guard ("" for none): the hash of the
	// deck the plan was written for.
	Base string
	// Units is "pt" (the default) or "emu", as in the views.
	Units string
	Ops   []PlanOp
	// slides are the slides the operations address, set by Compile.
	slides []string
}

// PlanOp is one decoded operation: one of the *Spec types below.
type PlanOp interface{ opName() string }

// OpDef is one entry of the operation table: the name, a constructor of
// its spec (whose struct tags give the plan fields) and its reference
// text. The table drives decoding and `pptx help ops`.
type OpDef struct {
	Name    string
	New     func() PlanOp
	Summary string
	// Pending operations are in the plan grammar but not implemented.
	Pending bool
}

// Ops is the operation table.
var Ops = []OpDef{
	{Name: "object.add", New: func() PlanOp { return &AddSpec{} },
		Summary: "Add a shape, connector or picture to the slide or group `at`, in front. kind is shape, connector or picture. A shape takes geom (a preset) or path (a freeform) and box; a connector takes geom (a connector preset), from and to, first (h or v) for elbow presets, and from_glue and to_glue (\"s12:3\", \"$name:1\" or \"none\"); a picture takes image and box. name is required (\"\" allowed)."},
	{Name: "object.set", New: func() PlanOp { return &SetSpec{} },
		Summary: "Set properties of the object `at`. Every field given is set; every field left out keeps its value. Values take the form `pptx read` shows."},
	{Name: "object.move", New: func() PlanOp { return &MoveSpec{} },
		Summary: "Translate the object by dx, dy, applied to the exact stored position."},
	{Name: "object.delete", New: func() PlanOp { return &DeleteSpec{} }, Summary: "Delete the object."},
	{Name: "object.duplicate", New: func() PlanOp { return &DuplicateSpec{} },
		Summary: "Copy the object in front of the original, offset by dx, dy."},
	{Name: "object.reorder", New: func() PlanOp { return &ReorderSpec{} },
		Summary: "Change the paint order: to is front or back, or the address of another object with position before or after."},
	{Name: "text.set", New: func() PlanOp { return &TextSetSpec{} },
		Summary: "Make the object's text body show html (\"\" is one empty paragraph). Paragraphs and runs the html leaves unchanged keep their bytes."},
	{Name: "text.replace", New: func() PlanOp { return &TextReplaceSpec{} },
		Summary: "Replace select (not empty) by with (\"\" deletes) in the object's text, keeping the formatting of the run where each match starts. One match needs nothing more; several need occurrence (1-based) or all."},
	{Name: "group.create", New: func() PlanOp { return &GroupCreateSpec{} },
		Summary: "Group the objects `at` (a list, all in one container). name is required (\"\" allowed)."},
	{Name: "group.ungroup", New: func() PlanOp { return &GroupUngroupSpec{} }, Summary: "Dissolve the group, keeping its children where they are."},
	{Name: "group.fit", New: func() PlanOp { return &GroupFitSpec{} }, Summary: "Fit the group's frame to its children."},
	{Name: "slide.add", New: func() PlanOp { return &SlideAddSpec{} }, Pending: true},
	{Name: "slide.delete", New: func() PlanOp { return &SlideDeleteSpec{} }, Pending: true},
	{Name: "slide.duplicate", New: func() PlanOp { return &SlideDuplicateSpec{} }, Pending: true},
	{Name: "xml.set", New: func() PlanOp { return &XMLSetSpec{} }, Pending: true},
}

// LookupOp returns the table entry named name.
func LookupOp(name string) (*OpDef, bool) {
	for i := range Ops {
		if Ops[i].Name == name {
			return &Ops[i], true
		}
	}
	return nil, false
}

// DecodePlan reads a plan (ParsePlan).
func DecodePlan(r io.Reader) (*Plan, error) {
	b, err := io.ReadAll(r)
	if err != nil {
		return nil, cli.Errorf(cli.CodeIO, "", "read the plan: %v", err)
	}
	return ParsePlan(b)
}

// ParsePlan decodes a plan's text with the shared plan decoder
// (internal/cli): unknown fields anywhere, unknown operations, null
// values and duplicate keys are errors, each with the path of the value
// it is about. The error is a cli.Errors holding every fault found.
func ParsePlan(text []byte) (*Plan, error) {
	env, d := cli.ParsePlan(text, "units")
	if env == nil {
		return nil, d.Err()
	}
	p := &Plan{Units: "pt", Base: env.Base}
	if u := env.Settings["units"]; u != nil {
		d.Value(u, "units", &p.Units)
		if u.Kind == cli.String && p.Units != "pt" && p.Units != "emu" {
			d.Fail(cli.CodeInvalidPlan, "units", u, "units is pt or emu, not %q", p.Units)
		}
	}
	for i, op := range env.Ops {
		if env.Names[i] == "" {
			continue
		}
		def, ok := LookupOp(env.Names[i])
		if !ok {
			d.UnknownOp(i, op, "pptx")
			continue
		}
		spec := def.New()
		d.Struct(op, cli.Index("ops", i), spec, "op")
		p.Ops = append(p.Ops, spec)
	}
	if err := d.Err(); err != nil {
		return nil, err
	}
	return p, nil
}

// Transform holds the transform fields of object.add and object.set, in
// slide space and the plan's units: box [x, y, w, h], rotation in
// degrees, flips.
type Transform struct {
	Box   Opt[[4]float64] `json:"box"`
	Rot   Opt[float64]    `json:"rot"`
	FlipH Opt[bool]       `json:"flip_h"`
	FlipV Opt[bool]       `json:"flip_v"`
}

// ColorSpec is a colour in the views' form: {"scheme": "accent1"} or
// {"hex": "#FF0000"}, optionally with modifiers ("mods": [{"lumMod":
// 75000}, "inv"]); {"rgb": "FF0000", "mods": [...]} for an sRGB colour
// with modifiers; {"sys": ...} and {"preset": ...} as read. A "hex" next
// to a reference is the displayed value the views derive: it may be given
// back, and must then match. Type is a non-solid line fill shown as
// {"type": "gradFill"}, writable only unchanged.
type ColorSpec struct {
	Scheme Opt[string]            `json:"scheme"`
	RGB    Opt[string]            `json:"rgb"`
	Sys    Opt[string]            `json:"sys"`
	Preset Opt[string]            `json:"preset"`
	HSL    Opt[[3]int64]          `json:"hsl"`
	ScRGB  Opt[[3]int64]          `json:"scrgb"`
	Mods   Opt[[]json.RawMessage] `json:"mods"`
	Hex    Opt[string]            `json:"hex"`
	Type   Opt[string]            `json:"type"`
}

// FillSpec is a fill in the views' form: "none", a colour (a solid fill),
// or {"type": "gradFill"} for a fill shown as raw XML, writable only
// unchanged.
type FillSpec struct {
	None  bool
	Color *ColorSpec
}

// DecodePlan implements cli.PlanDecoder: "none" or a colour object, with
// the path of any fault inside it.
func (f *FillSpec) DecodePlan(v *cli.Value, path string, d *cli.Decoder) {
	if v.Kind == cli.String {
		if v.Str != "none" {
			d.Fail(cli.CodeInvalidPlan, path, v, "fill %q: want \"none\" or a colour", v.Str)
			return
		}
		f.None = true
		return
	}
	f.Color = &ColorSpec{}
	d.Struct(v, path, f.Color)
}

// LineSpec is an outline in the views' form: "none", or the properties to
// set: w in the plan's units, color, dash and cap as DrawingML names them,
// head and tail as "triangle" or "triangle w=sm len=lg". Properties left
// out keep the value shown.
type LineSpec struct {
	None  bool
	W     Opt[float64]
	Color *ColorSpec
	Dash  Opt[string]
	Cap   Opt[string]
	Head  Opt[string]
	Tail  Opt[string]
}

// DecodePlan implements cli.PlanDecoder: "none" or an object of the
// properties to set, with the path of any fault inside it.
func (l *LineSpec) DecodePlan(v *cli.Value, path string, d *cli.Decoder) {
	if v.Kind == cli.String {
		if v.Str != "none" {
			d.Fail(cli.CodeInvalidPlan, path, v, "line %q: want \"none\" or an object", v.Str)
			return
		}
		l.None = true
		return
	}
	var w struct {
		W     Opt[float64] `json:"w"`
		Color *ColorSpec   `json:"color"`
		Dash  Opt[string]  `json:"dash"`
		Cap   Opt[string]  `json:"cap"`
		Head  Opt[string]  `json:"head"`
		Tail  Opt[string]  `json:"tail"`
	}
	d.Struct(v, path, &w)
	*l = LineSpec{W: w.W, Color: w.Color, Dash: w.Dash, Cap: w.Cap, Head: w.Head, Tail: w.Tail}
}

// BodySpec is a shape's text body properties in the views' form (`text`
// in --detail full): anchor, wrap, vert, vertOverflow and horzOverflow as
// DrawingML names them, insets [left, top, right, bottom] and spcCol in
// the plan's units, rot in degrees, numCol a count, the flags as
// booleans, autofit "none", "shape" (a:spAutoFit) or "shrink"
// (a:normAutofit) with fontScale and lnSpcReduction in percent.
// Properties left out keep the value shown.
type BodySpec struct {
	Anchor           Opt[string]     `json:"anchor"`
	Wrap             Opt[string]     `json:"wrap"`
	Insets           Opt[[4]float64] `json:"insets"`
	Autofit          Opt[string]     `json:"autofit"`
	FontScale        Opt[float64]    `json:"font_scale"`
	LnSpcReduction   Opt[float64]    `json:"ln_spc_reduction"`
	Vert             Opt[string]     `json:"vert"`
	Rot              Opt[float64]    `json:"rot"`
	NumCol           Opt[int64]      `json:"num_col"`
	SpcCol           Opt[float64]    `json:"spc_col"`
	RtlCol           Opt[bool]       `json:"rtl_col"`
	AnchorCtr        Opt[bool]       `json:"anchor_ctr"`
	Upright          Opt[bool]       `json:"upright"`
	VertOverflow     Opt[string]     `json:"vert_overflow"`
	HorzOverflow     Opt[string]     `json:"horz_overflow"`
	CompatLnSpc      Opt[bool]       `json:"compat_ln_spc"`
	SpcFirstLastPara Opt[bool]       `json:"spc_first_last_para"`
	FromWordArt      Opt[bool]       `json:"from_word_art"`
	ForceAA          Opt[bool]       `json:"force_aa"`
}

// attrs returns the stored a:bodyPr attribute values the spec gives,
// in derive.BodyAttrs order (the insets and lengths rounded to whole EMU
// in units u), and the stored autofit scales. Values are checked by the
// caller against the schema.
func (b BodySpec) attrs(u interface{ ToEMU(float64) float64 }) (map[string]string, error) {
	out := map[string]string{}
	str := func(k string, v Opt[string]) {
		if v.Set {
			out[k] = v.V
		}
	}
	flag := func(k string, v Opt[bool]) {
		if v.Set {
			out[k] = map[bool]string{true: "1", false: "0"}[v.V]
		}
	}
	str("anchor", b.Anchor)
	str("wrap", b.Wrap)
	if b.Insets.Set {
		for i, k := range []string{"lIns", "tIns", "rIns", "bIns"} {
			out[k] = emuString(u.ToEMU(b.Insets.V[i]))
		}
	}
	str("vert", b.Vert)
	if b.Rot.Set {
		x := math.Round(b.Rot.V * 60000)
		if math.Abs(x) > math.MaxInt32 {
			return nil, fmt.Errorf("text rot %v: out of range", b.Rot.V)
		}
		out["rot"] = strconv.FormatInt(int64(x), 10)
	}
	if b.NumCol.Set {
		out["numCol"] = strconv.FormatInt(b.NumCol.V, 10)
	}
	if b.SpcCol.Set {
		out["spcCol"] = emuString(u.ToEMU(b.SpcCol.V))
	}
	flag("rtlCol", b.RtlCol)
	flag("anchorCtr", b.AnchorCtr)
	flag("upright", b.Upright)
	str("vertOverflow", b.VertOverflow)
	str("horzOverflow", b.HorzOverflow)
	flag("compatLnSpc", b.CompatLnSpc)
	flag("spcFirstLastPara", b.SpcFirstLastPara)
	flag("fromWordArt", b.FromWordArt)
	flag("forceAA", b.ForceAA)
	for _, s := range []struct {
		k string
		v Opt[float64]
	}{{"fontScale", b.FontScale}, {"lnSpcReduction", b.LnSpcReduction}} {
		if s.v.Set {
			x := math.Round(s.v.V * 1000)
			if math.Abs(x) > math.MaxInt32 {
				return nil, fmt.Errorf("text %s %v: out of range", s.k, s.v.V)
			}
			out[s.k] = strconv.FormatInt(int64(x), 10)
		}
	}
	return out, nil
}

// emuString rounds an EMU length to whole EMU, out of range values
// becoming a string the schema check refuses.
func emuString(x float64) string {
	if x > math.MaxInt64/2 || x < math.MinInt64/2 {
		return strconv.FormatFloat(x, 'g', -1, 64)
	}
	return strconv.FormatInt(int64(math.Round(x)), 10)
}

// AddSpec is object.add.
type AddSpec struct {
	// At is the slide ("slide3") or group ("slide3/s40", "$name") to add
	// into, in front.
	At    string        `json:"at"`
	Alias Opt[string]   `json:"as"`
	Kind  string        `json:"kind" enum:"shape,connector,picture"`
	Name  Opt[string]   `json:"name" plan:"needed" doc:"Every object has a name; \"\" is allowed."`
	Geom  Opt[string]   `json:"geom"`
	Path  Opt[PathSpec] `json:"path"`
	Transform
	Custom Opt[CustomSpec]     `json:"custom"`
	Parent Opt[string]         `json:"-"` // the group to add into, from At (Compile)
	Fill   Opt[FillSpec]       `json:"fill"`
	Line   Opt[LineSpec]       `json:"line"`
	HTML   Opt[string]         `json:"html"`
	Text   Opt[BodySpec]       `json:"text"`
	Start  Opt[string]         `json:"from_glue"` // glue: "s12:3" or "$name:1"
	End    Opt[string]         `json:"to_glue"`
	From   Opt[[2]float64]     `json:"from"`
	To     Opt[[2]float64]     `json:"to"`
	First  Opt[string]         `json:"first"`
	Adj    Opt[map[string]any] `json:"adj"`
	PictureFields
	NonVisual
}

// NonVisual holds the non-visual properties of object.add and object.set
// in the views' form: descr (alternative text) and title as strings (""
// removes them), hidden as a boolean, and txBox (a shape that is
// PowerPoint's text box) as a boolean.
type NonVisual struct {
	Descr  Opt[string] `json:"descr"`
	Title  Opt[string] `json:"title"`
	Hidden Opt[bool]   `json:"hidden"`
	TxBox  Opt[bool]   `json:"tx_box"`
}

// PictureFields are a picture's fields on object.add and object.set, in
// the views' form. image is the image part as shown (a part of the deck)
// or a local PNG, JPEG or GIF file; svg likewise for an SVG file, whose
// raster fallback is image, or "none"; crop and fillRect are [l, t, r, b]
// in percent; fit is stretch (or tile, writable back unchanged only);
// locks sets the a:picLocks flags given, true or false, keeping the
// others.
type PictureFields struct {
	Image    Opt[string]          `json:"image"`
	SVG      Opt[string]          `json:"svg"`
	Crop     Opt[[4]float64]      `json:"crop"`
	Fit      Opt[string]          `json:"fit"`
	FillRect Opt[[4]float64]      `json:"fill_rect"`
	Locks    Opt[map[string]bool] `json:"locks"`
}

// any reports whether any picture field is given.
func (p PictureFields) any() bool {
	return p.Image.Set || p.SVG.Set || p.Crop.Set || p.Fit.Set || p.FillRect.Set || p.Locks.Set
}

// SetSpec is object.set: every written field is set, every absent field
// is left as it is. Fields take the form the views show them in.
type SetSpec struct {
	Target string      `json:"at"`
	Name   Opt[string] `json:"name"`
	Transform
	Geom   Opt[string]         `json:"geom"`
	Adj    Opt[map[string]any] `json:"adj"`
	Path   Opt[PathSpec]       `json:"path"`
	Custom Opt[CustomSpec]     `json:"custom"`
	Fill   Opt[FillSpec]       `json:"fill"`
	Line   Opt[LineSpec]       `json:"line"`
	HTML   Opt[string]         `json:"html"`
	Text   Opt[BodySpec]       `json:"text"`
	Start  Opt[string]         `json:"from_glue"`
	End    Opt[string]         `json:"to_glue"`
	From   Opt[[2]float64]     `json:"from"`
	To     Opt[[2]float64]     `json:"to"`
	First  Opt[string]         `json:"first"`
	Elbows Opt[[][2]float64]   `json:"elbows"`
	PictureFields
	NonVisual
}

// MoveSpec is object.move: translate by dx, dy applied to the exact
// stored EMU.
type MoveSpec struct {
	Target string       `json:"at"`
	DX     Opt[float64] `json:"dx"`
	DY     Opt[float64] `json:"dy"`
}

// DeleteSpec is object.delete.
type DeleteSpec struct {
	Target string `json:"at"`
}

// DuplicateSpec is object.duplicate.
type DuplicateSpec struct {
	Target string       `json:"at"`
	Alias  Opt[string]  `json:"as"`
	DX     Opt[float64] `json:"dx"`
	DY     Opt[float64] `json:"dy"`
}

// ReorderSpec is object.reorder: to is front or back, or the address of
// another object with position before or after.
type ReorderSpec struct {
	Target string      `json:"at"`
	To     string      `json:"to" doc:"front, back, or the address of another object on the slide."`
	Pos    Opt[string] `json:"position" enum:"before,after" doc:"Required when to is an address."`
	Ref    Opt[string] `json:"-"` // the object To addresses, from To (Compile)
}

// GroupCreateSpec is group.create.
type GroupCreateSpec struct {
	Targets []string    `json:"at"`
	Alias   Opt[string] `json:"as"`
	Name    Opt[string] `json:"name" plan:"needed"`
}

// GroupUngroupSpec is group.ungroup.
type GroupUngroupSpec struct {
	Target string `json:"at"`
}

// GroupFitSpec is group.fit.
type GroupFitSpec struct {
	Target string `json:"at"`
}

// TextSetSpec is text.set: the object's text body becomes html, in the
// grammar the read view emits (internal/pptx/ops/text). "" is a body with one
// empty paragraph; the field itself is required.
type TextSetSpec struct {
	Target string      `json:"at"`
	HTML   Opt[string] `json:"html" plan:"needed"`
}

// TextReplaceSpec is text.replace: select (non-empty) replaced by with
// ("" deletes), in the run where each match starts. More than one match
// needs occurrence (1-based) or all.
type TextReplaceSpec struct {
	Target string      `json:"at"`
	Find   Opt[string] `json:"select" plan:"needed" doc:"Exact text, not empty."`
	With   Opt[string] `json:"with" plan:"needed" doc:"Plain text; \"\" deletes."`
	All    Opt[bool]   `json:"all" doc:"Replace every match. Not with occurrence."`
	Occ    Opt[int]    `json:"occurrence" doc:"Which match, counting from 1."`
}

// SlideAddSpec is slide.add.
type SlideAddSpec struct {
	Layout Opt[string] `json:"layout"`
	After  Opt[string] `json:"after"`
}

// SlideDeleteSpec is slide.delete.
type SlideDeleteSpec struct {
	Slide string `json:"slide"`
}

// SlideDuplicateSpec is slide.duplicate.
type SlideDuplicateSpec struct {
	Slide string `json:"slide"`
}

// XMLSetSpec is xml.set.
type XMLSetSpec struct {
	Target string `json:"at"`
	XML    string `json:"xml"`
}

// opName returns "object.add" for an AddSpec operation.
func (*AddSpec) opName() string { return "object.add" }

// opName returns "object.set" for a SetSpec operation.
func (*SetSpec) opName() string { return "object.set" }

// opName returns "object.move" for a MoveSpec operation.
func (*MoveSpec) opName() string { return "object.move" }

// opName returns "object.delete" for a DeleteSpec operation.
func (*DeleteSpec) opName() string { return "object.delete" }

// opName returns "object.duplicate" for a DuplicateSpec operation.
func (*DuplicateSpec) opName() string { return "object.duplicate" }

// opName returns "object.reorder" for a ReorderSpec operation.
func (*ReorderSpec) opName() string { return "object.reorder" }

// opName returns "group.create" for a GroupCreateSpec operation.
func (*GroupCreateSpec) opName() string { return "group.create" }

// opName returns "group.ungroup" for a GroupUngroupSpec operation.
func (*GroupUngroupSpec) opName() string { return "group.ungroup" }

// opName returns "group.fit" for a GroupFitSpec operation.
func (*GroupFitSpec) opName() string { return "group.fit" }

// opName returns "text.set" for a TextSetSpec operation.
func (*TextSetSpec) opName() string { return "text.set" }

// opName returns "text.replace" for a TextReplaceSpec operation.
func (*TextReplaceSpec) opName() string { return "text.replace" }

// opName returns "slide.add" for a SlideAddSpec operation.
func (*SlideAddSpec) opName() string { return "slide.add" }

// opName returns "slide.delete" for a SlideDeleteSpec operation.
func (*SlideDeleteSpec) opName() string { return "slide.delete" }

// opName returns "slide.duplicate" for a SlideDuplicateSpec operation.
func (*SlideDuplicateSpec) opName() string { return "slide.duplicate" }

// opName returns "xml.set" for an XMLSetSpec operation.
func (*XMLSetSpec) opName() string { return "xml.set" }
