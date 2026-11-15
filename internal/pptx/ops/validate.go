package ops

import (
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"

	"github.com/jhnoel/documentdriver/internal/cli"
	"github.com/jhnoel/documentdriver/internal/pptx/derive"
	"github.com/jhnoel/documentdriver/internal/pptx/ops/edit"
	"github.com/jhnoel/documentdriver/internal/pptx/ops/prop"
	"github.com/jhnoel/documentdriver/internal/pptx/ops/text"
)

// checkFields checks, before the deck is opened, every value object.add
// and object.set would write against the schema type of the attribute
// it is written to (edit.CheckAttr, edit.CheckColor: tables generated
// from the pinned schemas), so that a value outside the schema fails the
// plan naming the field, the value and what the type allows, and nothing
// is written. Values the tool's own vocabulary carries ("custom" geometry
// and dashes, fills shown by type, first, glue syntax) are checked as far
// as they can be without the deck. The HTML is parsed here too.
func checkFields(f fields, u prop.Units, html Opt[string]) error {
	if f.Path.Set {
		if _, err := f.Path.V.value(u); err != nil {
			return err
		}
	}
	if f.Custom.Set {
		if _, err := f.Custom.V.value(u, prop.CustomVal{}); err != nil {
			return err
		}
	}
	if f.Geom.Set && f.Geom.V != "custom" {
		if err := edit.CheckAttr("a:CT_PresetGeometry2D", "prst", f.Geom.V); err != nil {
			return fmt.Errorf("geom: %w", err)
		}
	}
	if f.Adj.Set {
		for name, v := range f.Adj.V {
			fmla, err := adjFormula(name, v)
			if err != nil {
				return err
			}
			if err := edit.CheckAttr("a:CT_GeomGuide", "name", name); err != nil {
				return fmt.Errorf("adj %s: %w", name, err)
			}
			if err := edit.CheckAttr("a:CT_GeomGuide", "fmla", fmla); err != nil {
				return fmt.Errorf("adj %s: %w", name, err)
			}
		}
	}
	if f.Box.Set {
		b := f.Box.V
		for i, c := range []struct{ typ, attr, name string }{
			{"a:CT_Point2D", "x", "x"}, {"a:CT_Point2D", "y", "y"},
			{"a:CT_PositiveSize2D", "cx", "width"}, {"a:CT_PositiveSize2D", "cy", "height"},
		} {
			if err := checkLength(c.typ, c.attr, u, b[i]); err != nil {
				return fmt.Errorf("box %s %s: %w", c.name, derive.FormatNum(b[i]), err)
			}
		}
	}
	for _, e := range []struct {
		name string
		pt   Opt[[2]float64]
	}{{"from", f.From}, {"to", f.To}} {
		if !e.pt.Set {
			continue
		}
		for i, attr := range []string{"x", "y"} {
			if err := checkLength("a:CT_Point2D", attr, u, e.pt.V[i]); err != nil {
				return fmt.Errorf("%s %s: %w", e.name, attr, err)
			}
		}
	}
	if f.First.Set && f.First.V != "h" && f.First.V != "v" {
		return fmt.Errorf("first %q: want h or v", f.First.V)
	}
	for _, g := range []struct {
		name string
		spec Opt[string]
	}{{"from_glue", f.Start}, {"to_glue", f.End}} {
		if g.spec.Set {
			if err := checkGlue(g.spec.V); err != nil {
				return fmt.Errorf("%s: %w", g.name, err)
			}
		}
	}
	if f.Fill.Set && !f.Fill.V.None && !f.Fill.V.Color.Type.Set {
		if err := checkColor(f.Fill.V.Color); err != nil {
			return fmt.Errorf("fill: %w", err)
		}
	}
	if f.Line.Set {
		if err := checkLine(f.Line.V, u); err != nil {
			return fmt.Errorf("line %w", err)
		}
	}
	if f.Body.Set {
		if err := checkBody(f.Body.V, u); err != nil {
			return err
		}
	}
	for _, s := range []struct {
		attr string
		v    Opt[string]
	}{{"descr", f.Descr}, {"title", f.Title}} {
		if s.v.Set {
			if err := edit.CheckAttr("a:CT_NonVisualDrawingProps", s.attr, s.v.V); err != nil {
				return fmt.Errorf("%s: %w", s.attr, err)
			}
		}
	}
	if err := checkPicture(f.Pic); err != nil {
		return err
	}
	if html.Set {
		if _, err := text.Parse(html.V); err != nil {
			return fmt.Errorf("html: %w", err)
		}
	}
	return nil
}

// checkBody checks a plan's text body properties: each stored value
// against its a:bodyPr attribute's type (a:ST_TextAnchoringType,
// a:ST_TextWrappingType, a:ST_Coordinate32, a:ST_TextVerticalType,
// a:ST_Angle, a:ST_TextColumnCount, a:ST_PositiveCoordinate32,
// xsd:boolean, a:ST_TextVertOverflowType, a:ST_TextHorzOverflowType),
// the autofit mode, and the scales against a:CT_TextNormalAutofit's
// types, which need autofit shrink.
func checkBody(b BodySpec, u prop.Units) error {
	vals, err := b.attrs(u)
	if err != nil {
		return err
	}
	for _, a := range derive.BodyAttrs {
		v, ok := vals[a]
		if !ok {
			continue
		}
		if err := edit.CheckAttr("a:CT_TextBodyProperties", a, v); err != nil {
			return fmt.Errorf("text %s: %w", cli.Snake(a), err)
		}
	}
	if b.Autofit.Set {
		if _, ok := derive.AutofitElems[b.Autofit.V]; !ok {
			return fmt.Errorf("text autofit %q: want none, shape (resize the shape to fit) or shrink (shrink text on overflow)", b.Autofit.V)
		}
	}
	for _, s := range []string{"fontScale", "lnSpcReduction"} {
		v, ok := vals[s]
		if !ok {
			continue
		}
		if b.Autofit.Set && b.Autofit.V != "shrink" {
			return fmt.Errorf("text %s: only with autofit shrink", cli.Snake(s))
		}
		if err := edit.CheckAttr("a:CT_TextNormalAutofit", s, v); err != nil {
			return fmt.Errorf("text %s: %w", cli.Snake(s), err)
		}
	}
	return nil
}

// lockName returns the a:picLocks attribute a plan's lock key names
// (no_change_aspect is noChangeAspect), checked against the schema.
func lockName(key string) (string, bool) {
	name := cli.Camel(key)
	if cli.Snake(name) != key {
		return "", false
	}
	typ, ok := edit.AttrType("a:CT_PictureLocking", name)
	return name, ok && typ == "xsd:boolean"
}

// checkPicture checks a plan's picture fields: crop and fill rectangle
// sides in a:ST_Percentage once rounded to 1000ths of a percent, the fill
// mode's name, the lock flags against a:CT_PictureLocking, and that image
// and svg name something. Whether a file or part is an image of the
// right format is checked when the operation runs, against the deck.
func checkPicture(p PictureFields) error {
	for _, r := range []struct {
		name string
		v    Opt[[4]float64]
	}{{"crop", p.Crop}, {"fill_rect", p.FillRect}} {
		if !r.v.Set {
			continue
		}
		pct, err := percents(r.v.V)
		if err != nil {
			return fmt.Errorf("%s: %w", r.name, err)
		}
		for i, side := range derive.RectSides {
			if err := edit.CheckAttr("a:CT_RelativeRect", side, strconv.FormatInt(pct[i], 10)); err != nil {
				return fmt.Errorf("%s %s %s: %w", r.name, side, derive.FormatNum(r.v.V[i]), err)
			}
		}
	}
	if p.Fit.Set && p.Fit.V != "stretch" && p.Fit.V != "tile" {
		return fmt.Errorf("fit %q: want stretch (or tile, writable back unchanged only)", p.Fit.V)
	}
	if p.Locks.Set {
		for n := range p.Locks.V {
			if _, ok := lockName(n); !ok {
				return fmt.Errorf("locks: %q is not an a:picLocks flag (a:CT_PictureLocking)", n)
			}
		}
	}
	for _, s := range []struct {
		name string
		v    Opt[string]
	}{{"image", p.Image}, {"svg", p.SVG}} {
		if s.v.Set && strings.TrimSpace(s.v.V) == "" {
			return fmt.Errorf("%s: empty; give a file or a part of the deck", s.name)
		}
	}
	if p.Image.Set && p.Image.V == "none" {
		return errors.New(`image: a picture needs an image; "none" is only for svg`)
	}
	return nil
}

// percents rounds [l, t, r, b] in percent to whole 1000ths of a percent,
// the unit a:ST_Percentage stores.
func percents(v [4]float64) ([4]int64, error) {
	var out [4]int64
	for i, x := range v {
		y := math.Round(x * 1000)
		if math.IsNaN(y) || math.Abs(y) > math.MaxInt32 {
			return out, fmt.Errorf("%s is out of range", derive.FormatNum(x))
		}
		out[i] = int64(y)
	}
	return out, nil
}

// checkLength checks a length in the plan's units, rounded to whole EMU
// as it would be stored, against the schema type of attr.
func checkLength(ctype, attr string, u prop.Units, v float64) error {
	x := u.ToEMU(v)
	if x > math.MaxInt64/2 || x < math.MinInt64/2 {
		return fmt.Errorf("%w: %s EMU is out of range", edit.ErrSchema, strconv.FormatFloat(x, 'g', -1, 64))
	}
	return edit.CheckAttr(ctype, attr, strconv.FormatInt(int64(derive.RoundEMU(x)), 10))
}

// checkGlue checks a glue reference's syntax and site index; the object
// it names resolves when the operation runs.
func checkGlue(s string) error {
	if s == "none" {
		return nil
	}
	i := strings.LastIndexByte(s, ':')
	if i < 0 {
		return fmt.Errorf("glue %q: want object:site, such as s12:3 or $cache:1, or none", s)
	}
	if err := edit.CheckAttr("a:CT_Connection", "idx", s[i+1:]); err != nil {
		return fmt.Errorf("glue %q: site: %w", s, err)
	}
	if raw, ok := strings.CutPrefix(s[:i], "#"); ok {
		if err := edit.CheckAttr("a:CT_Connection", "id", raw); err != nil {
			return fmt.Errorf("glue %q: id: %w", s, err)
		}
	}
	return nil
}

// checkColor checks a plan colour: one kind, its value, components and
// modifiers in the schema.
func checkColor(cs *ColorSpec) error {
	var c derive.Color
	kinds := 0
	set := func(ok bool, kind, val string, comp [3]int64) {
		if ok {
			kinds++
			c = derive.Color{Kind: kind, Val: val, Comp: comp}
		}
	}
	set(cs.Scheme.Set, "scheme", cs.Scheme.V, [3]int64{})
	set(cs.RGB.Set, "srgb", strings.TrimPrefix(cs.RGB.V, "#"), [3]int64{})
	set(cs.Sys.Set, "sys", cs.Sys.V, [3]int64{})
	set(cs.Preset.Set, "prst", cs.Preset.V, [3]int64{})
	set(cs.HSL.Set, "hsl", "", cs.HSL.V)
	set(cs.ScRGB.Set, "scrgb", "", cs.ScRGB.V)
	switch {
	case kinds > 1:
		return errors.New("colour: give one of scheme, rgb, sys, preset, hsl, scrgb or hex")
	case kinds == 0 && !cs.Hex.Set:
		return errors.New("colour: give scheme or hex")
	}
	if cs.Hex.Set && !validHex(strings.ToUpper(strings.TrimPrefix(cs.Hex.V, "#"))) {
		return fmt.Errorf("colour: hex %q is not #RRGGBB", cs.Hex.V)
	}
	if kinds == 0 {
		if cs.Mods.Set {
			return errors.New("colour: modifiers go with scheme or rgb, not hex (hex is the displayed value)")
		}
		return nil
	}
	if cs.Mods.Set {
		for _, raw := range cs.Mods.V {
			m, err := parseMod(raw)
			if err != nil {
				return err
			}
			c.Mods = append(c.Mods, m)
		}
	}
	if err := edit.CheckSlideColor(c); err != nil {
		return fmt.Errorf("colour: %w", err)
	}
	return nil
}

// checkLine checks a plan outline's width, cap, dash, arrows and colour.
func checkLine(l LineSpec, u prop.Units) error {
	if l.None {
		return nil
	}
	if l.W.Set {
		if err := checkLength("a:CT_LineProperties", "w", u, l.W.V); err != nil {
			return fmt.Errorf("w %s: %w", derive.FormatNum(l.W.V), err)
		}
	}
	if l.Cap.Set {
		if err := edit.CheckAttr("a:CT_LineProperties", "cap", l.Cap.V); err != nil {
			return fmt.Errorf("cap: %w", err)
		}
	}
	if l.Dash.Set && l.Dash.V != "custom" {
		if err := edit.CheckAttr("a:CT_PresetLineDashProperties", "val", l.Dash.V); err != nil {
			return fmt.Errorf("dash: %w", err)
		}
	}
	for _, e := range []struct {
		name string
		spec Opt[string]
	}{{"head", l.Head}, {"tail", l.Tail}} {
		if !e.spec.Set {
			continue
		}
		a, err := parseArrow(e.spec.V)
		if err != nil {
			return fmt.Errorf("%s: %w", e.name, err)
		}
		for _, x := range []struct{ attr, v string }{{"type", a.Type}, {"w", a.W}, {"len", a.Len}} {
			if x.v == "" {
				continue
			}
			if err := edit.CheckAttr("a:CT_LineEndProperties", x.attr, x.v); err != nil {
				return fmt.Errorf("%s %s: %w", e.name, x.attr, err)
			}
		}
	}
	if l.Color != nil && !l.Color.Type.Set {
		if err := checkColor(l.Color); err != nil {
			return fmt.Errorf("color: %w", err)
		}
	}
	return nil
}
