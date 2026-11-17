package derive

import (
	"fmt"
	"math"
	"slices"
	"strconv"
	"strings"
)

// guide is one shape guide: a name and its formula, as written.
type guide struct{ name, fmla string }

// cxnDef is one connection site of a preset: angle and position, as guide
// references.
type cxnDef struct{ ang, x, y string }

// pathCmd is one path command (moveTo, lnTo, cubicBezTo, quadBezTo, arcTo,
// close) with its points as guide references.
type pathCmd struct {
	op  string
	pts [][2]string
}

// pathDef is one path of a preset geometry, with its coordinate space.
type pathDef struct {
	w, h string
	cmds []pathCmd
}

// presetDef is the extract of one preset from presetShapeDefinitions.xml.
type presetDef struct {
	av    []guide
	gd    []guide
	cxn   []cxnDef
	paths []pathDef
}

// Adjust is an adjust value as stored in a shape's avLst: the guide name
// and its formula (normally "val N").
type Adjust struct {
	Name string
	Fmla string
}

// Guides holds evaluated shape guides for one shape size.
type Guides struct {
	w, h float64
	vals map[string]float64
}

// angle units: 60000ths of a degree.
const angleUnit = 60000.0

// NewGuides evaluates the builtin guides for a shape of width w and height
// h (any consistent unit, normally EMU), then the adjust values (defaults
// in av, replaced by the shape's own values in adj, matched by name), then
// the formulas in gd, in order.
func newGuides(w, h float64, av []guide, adj []Adjust, gd []guide) (*Guides, error) {
	g := &Guides{w: w, h: h, vals: map[string]float64{}}
	ss, ls := math.Min(w, h), math.Max(w, h)
	b := map[string]float64{
		"l": 0, "t": 0, "r": w, "b": h, "w": w, "h": h,
		"hc": w / 2, "vc": h / 2, "ls": ls, "ss": ss,
		"wd2": w / 2, "wd3": w / 3, "wd4": w / 4, "wd5": w / 5, "wd6": w / 6,
		"wd8": w / 8, "wd10": w / 10, "wd12": w / 12, "wd32": w / 32,
		"hd2": h / 2, "hd3": h / 3, "hd4": h / 4, "hd5": h / 5, "hd6": h / 6,
		"hd8": h / 8, "hd10": h / 10,
		"ssd2": ss / 2, "ssd4": ss / 4, "ssd6": ss / 6, "ssd8": ss / 8,
		"ssd16": ss / 16, "ssd32": ss / 32,
		"cd2": 10800000, "cd3": 7200000, "cd4": 5400000, "cd8": 2700000,
		"3cd4": 16200000, "3cd8": 8100000, "5cd8": 13500000, "7cd8": 18900000,
	}
	for k, v := range b {
		g.vals[k] = v
	}
	set := map[string]string{}
	for _, a := range adj {
		set[a.Name] = a.Fmla
	}
	for _, a := range av {
		f := a.fmla
		if s, ok := set[a.name]; ok {
			f = s
		}
		if err := g.define(a.name, f); err != nil {
			return nil, err
		}
	}
	for _, d := range gd {
		if err := g.define(d.name, d.fmla); err != nil {
			return nil, err
		}
	}
	return g, nil
}

// define evaluates a guide formula and stores its value.
func (g *Guides) define(name, fmla string) error {
	v, err := g.Eval(fmla)
	if err != nil {
		return fmt.Errorf("guide %s: %w", name, err)
	}
	g.vals[name] = v
	return nil
}

// Value returns a guide reference's value: a guide or builtin name, or an
// integer literal.
func (g *Guides) Value(ref string) (float64, error) {
	if v, ok := g.vals[ref]; ok {
		return v, nil
	}
	if f, err := strconv.ParseFloat(ref, 64); err == nil {
		return f, nil
	}
	return 0, fmt.Errorf("unknown guide %q", ref)
}

// Eval evaluates one formula of the DrawingML shape-guide language
// (ECMA-376 Part 1, 20.1.10.56 ST_GeomGuideFormula). Angles are in
// 60000ths of a degree.
func (g *Guides) Eval(fmla string) (float64, error) {
	f := strings.Fields(fmla)
	if len(f) == 0 {
		return 0, fmt.Errorf("empty formula")
	}
	arity := map[string]int{
		"*/": 3, "+-": 3, "+/": 3, "?:": 3, "abs": 1, "at2": 2, "cat2": 3,
		"cos": 2, "max": 2, "min": 2, "mod": 3, "pin": 3, "sat2": 3, "sin": 2,
		"sqrt": 1, "tan": 2, "val": 1,
	}
	n, ok := arity[f[0]]
	if !ok {
		return 0, fmt.Errorf("unknown operator %q", f[0])
	}
	if len(f)-1 != n {
		return 0, fmt.Errorf("%q: want %d arguments", fmla, n)
	}
	a := make([]float64, n)
	for i := range n {
		v, err := g.Value(f[i+1])
		if err != nil {
			return 0, err
		}
		a[i] = v
	}
	rad := func(x float64) float64 { return x / angleUnit * math.Pi / 180 }
	deg := func(r float64) float64 { return r * 180 / math.Pi * angleUnit }
	switch f[0] {
	case "*/":
		if a[2] == 0 {
			return 0, fmt.Errorf("%q: division by zero", fmla)
		}
		return a[0] * a[1] / a[2], nil
	case "+-":
		return a[0] + a[1] - a[2], nil
	case "+/":
		if a[2] == 0 {
			return 0, fmt.Errorf("%q: division by zero", fmla)
		}
		return (a[0] + a[1]) / a[2], nil
	case "?:":
		if a[0] > 0 {
			return a[1], nil
		}
		return a[2], nil
	case "abs":
		return math.Abs(a[0]), nil
	case "at2":
		return deg(math.Atan2(a[1], a[0])), nil
	case "cat2":
		return a[0] * math.Cos(math.Atan2(a[2], a[1])), nil
	case "cos":
		return a[0] * math.Cos(rad(a[1])), nil
	case "max":
		return math.Max(a[0], a[1]), nil
	case "min":
		return math.Min(a[0], a[1]), nil
	case "mod":
		return math.Sqrt(a[0]*a[0] + a[1]*a[1] + a[2]*a[2]), nil
	case "pin":
		switch {
		case a[1] < a[0]:
			return a[0], nil
		case a[1] > a[2]:
			return a[2], nil
		}
		return a[1], nil
	case "sat2":
		return a[0] * math.Sin(math.Atan2(a[2], a[1])), nil
	case "sin":
		return a[0] * math.Sin(rad(a[1])), nil
	case "sqrt":
		if a[0] < 0 {
			return 0, fmt.Errorf("%q: square root of negative", fmla)
		}
		return math.Sqrt(a[0]), nil
	case "tan":
		return a[0] * math.Tan(rad(a[1])), nil
	}
	return a[0], nil // val
}

// Point evaluates a pair of guide references.
func (g *Guides) Point(p [2]string) (Pt, error) {
	x, err := g.Value(p[0])
	if err != nil {
		return Pt{}, err
	}
	y, err := g.Value(p[1])
	if err != nil {
		return Pt{}, err
	}
	return Pt{X: x, Y: y}, nil
}

// HasSiteTable reports whether prst is in the connection-site table.
func HasSiteTable(prst string) bool {
	d := presetTable[prst]
	return d != nil && d.cxn != nil
}

// SitePresets lists the presets in the connection-site table.
func SitePresets() []string {
	var out []string
	for n, d := range presetTable {
		if d.cxn != nil {
			out = append(out, n)
		}
	}
	slices.Sort(out)
	return out
}
