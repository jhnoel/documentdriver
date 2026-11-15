package derive

import (
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"

	"github.com/jhnoel/documentdriver/internal/ooxml/tree"
)

// NS is the DrawingML main namespace.
const NS = "http://schemas.openxmlformats.org/drawingml/2006/main"

// Mod is one colour modifier as written: its element name and integer
// value ("" for modifiers without one, such as inv or gray).
type Mod struct {
	Name string
	Val  string
}

// Color is a DrawingML colour as written: one of srgbClr, schemeClr,
// sysClr, prstClr, hslClr or scrgbClr, with its modifiers in order.
type Color struct {
	Kind string // srgb, scheme, sys, prst, hsl, scrgb
	// Val is the hex value (srgb), scheme name (scheme), system colour
	// name (sys) or preset name (prst).
	Val string
	// LastClr is a system colour's last computed value, if written.
	LastClr string
	// Comp holds hsl (hue, sat, lum) or scrgb (r, g, b) components.
	Comp [3]int64
	Mods []Mod
	// Invalid says why a component could not be read ("" if all were);
	// such a colour does not resolve.
	Invalid string
}

var colorKinds = map[string]string{
	"srgbClr": "srgb", "schemeClr": "scheme", "sysClr": "sys",
	"prstClr": "prst", "hslClr": "hsl", "scrgbClr": "scrgb",
}

// ColorElement returns the local name of the element of a colour kind
// (srgb is srgbClr), "" for an unknown kind.
func ColorElement(kind string) string {
	for el, k := range colorKinds {
		if k == kind {
			return el
		}
	}
	return ""
}

// ColorComponents returns the component attributes of an hsl or scrgb
// colour, in Comp order; ok is false for other kinds.
func ColorComponents(kind string) (names [3]string, ok bool) {
	switch kind {
	case "hsl":
		return [3]string{"hue", "sat", "lum"}, true
	case "scrgb":
		return [3]string{"r", "g", "b"}, true
	}
	return names, false
}

// ColorElem returns the first colour element child of n (as in a:solidFill
// or a fillRef), or nil.
func ColorElem(n *tree.Node) *tree.Node {
	if n == nil {
		return nil
	}
	for c := range n.ElementsSeq() {
		if _, ok := colorKinds[c.Local()]; ok && In(c, DML) {
			return c
		}
	}
	return nil
}

// ParseColor reads a colour element. It returns false for elements that
// are not colours.
func ParseColor(n *tree.Node) (Color, bool) {
	if !In(n, DML) {
		return Color{}, false
	}
	kind, ok := colorKinds[n.Local()]
	if !ok {
		return Color{}, false
	}
	c := Color{Kind: kind}
	switch kind {
	case "srgb", "scheme", "sys", "prst":
		c.Val, _ = n.Attr("val")
		if kind == "srgb" {
			c.Val = strings.ToUpper(c.Val)
		}
		if kind == "sys" {
			c.LastClr, _ = n.Attr("lastClr")
		}
	case "hsl", "scrgb":
		names, _ := ColorComponents(kind)
		for i, a := range names {
			v, err := attrPct(n, a)
			if err != nil {
				c.Invalid = err.Error()
				break
			}
			c.Comp[i] = v
		}
	}
	for m := range n.ElementsSeq() {
		v, _ := m.Attr("val")
		c.Mods = append(c.Mods, Mod{Name: m.Local(), Val: v})
	}
	return c, true
}

// attrPct reads a required integer or percentage component attribute.
func attrPct(n *tree.Node, name string) (int64, error) {
	s, ok := n.Attr(name)
	if !ok {
		return 0, &AttrError{Elem: n.Name(), Attr: name, Err: ErrAbsent}
	}
	v, err := parsePct(s)
	if err != nil {
		return 0, &AttrError{Elem: n.Name(), Attr: name, Value: s, Err: errors.New("not an integer or percentage")}
	}
	return v, nil
}

// parsePct parses an integer value, accepting the "50%" form of the
// transitional schema as 50000.
func parsePct(s string) (int64, error) {
	if strings.HasSuffix(s, "%") {
		f, err := strconv.ParseFloat(strings.TrimSuffix(s, "%"), 64)
		return int64(math.Round(f * 1000)), err
	}
	return strconv.ParseInt(s, 10, 64)
}

// Scheme is a theme's colour scheme (a:clrScheme), by slot name (dk1, lt1,
// dk2, lt2, accent1-6, hlink, folHlink).
type Scheme map[string]Color

// ClrMap is a colour map (p:clrMap or an override): logical names such as
// bg1 and tx1 mapped to scheme slots.
type ClrMap map[string]string

// ParseClrMap reads the attributes of a p:clrMap or a:overrideClrMapping.
func ParseClrMap(n *tree.Node) ClrMap {
	m := ClrMap{}
	if n == nil {
		return m
	}
	for _, a := range n.Attrs() {
		if !strings.Contains(a.Name, ":") && a.Name != "xmlns" {
			m[a.Name] = a.Value
		}
	}
	return m
}

// ParseScheme reads an a:clrScheme.
func ParseScheme(n *tree.Node) Scheme {
	s := Scheme{}
	if n == nil {
		return s
	}
	for slot := range n.ElementsSeq() {
		if c, ok := ParseColor(ColorElem(slot)); ok {
			s[slot.Local()] = c
		}
	}
	return s
}

// Errors from colour resolution.
var (
	ErrUnresolvedScheme = errors.New("scheme colour not in the colour map or theme")
	ErrSysNoLastClr     = errors.New("system colour without lastClr")
	ErrUnknownPreset    = errors.New("unknown preset colour")
	ErrPhClr            = errors.New("phClr outside a style reference")
)

// Slot maps a scheme colour name through the colour map to a theme slot:
// bg1 -> lt1 and so on. Names the map does not mention are slots already.
func (m ClrMap) Slot(name string) string {
	if s, ok := m[name]; ok {
		return s
	}
	return name
}

// Resolved is a colour resolved for display.
type Resolved struct {
	Hex   string  // RRGGBB, approximate when modifiers apply
	Alpha float64 // 1 is opaque
}

// Resolve computes the displayed colour of c. phClr, if not nil, is the
// colour that replaces schemeClr phClr (the colour of the style reference
// being resolved). Modifiers are applied in order with the method of
// LibreOffice's oox import (color.cxx): luminance and saturation in HSL,
// tint and shade in linear RGB with gamma 2.3. PowerPoint's own arithmetic
// is not documented, so the hex is approximate.
func (s Scheme) Resolve(c Color, m ClrMap, phClr *Color) (Resolved, error) {
	base, mods, err := s.base(c, m, phClr, 0)
	if err != nil {
		return Resolved{}, err
	}
	st := colorState{mode: modeRGB, c: base, alpha: 100000}
	for _, md := range mods {
		st.apply(md)
	}
	st.toRGB()
	return Resolved{Hex: fmt.Sprintf("%02X%02X%02X", st.c[0], st.c[1], st.c[2]), Alpha: float64(st.alpha) / 100000}, nil
}

// base returns c's base colour as RGB components and the modifiers to
// apply, following scheme and placeholder references.
func (s Scheme) base(c Color, m ClrMap, phClr *Color, depth int) ([3]int64, []Mod, error) {
	if depth > 4 {
		return [3]int64{}, nil, ErrUnresolvedScheme
	}
	if c.Invalid != "" {
		return [3]int64{}, nil, errors.New(c.Invalid)
	}
	switch c.Kind {
	case "srgb":
		v, err := parseHex(c.Val)
		return v, c.Mods, err
	case "sys":
		if c.LastClr == "" {
			return [3]int64{}, nil, ErrSysNoLastClr
		}
		v, err := parseHex(c.LastClr)
		return v, c.Mods, err
	case "prst":
		h, ok := presetColors[c.Val]
		if !ok {
			return [3]int64{}, nil, fmt.Errorf("%w %q", ErrUnknownPreset, c.Val)
		}
		v, err := parseHex(h)
		return v, c.Mods, err
	case "hsl":
		st := colorState{mode: modeHSL, c: c.Comp}
		st.toRGB()
		return st.c, c.Mods, nil
	case "scrgb":
		st := colorState{mode: modeCRGB, c: c.Comp}
		st.toRGB()
		return st.c, c.Mods, nil
	case "scheme":
		if c.Val == "phClr" {
			if phClr == nil {
				return [3]int64{}, nil, ErrPhClr
			}
			v, mods, err := s.base(*phClr, m, nil, depth+1)
			return v, append(append([]Mod(nil), mods...), c.Mods...), err
		}
		slot := m.Slot(c.Val)
		t, ok := s[slot]
		if !ok || t.Kind == "scheme" {
			return [3]int64{}, nil, fmt.Errorf("%w: %s", ErrUnresolvedScheme, c.Val)
		}
		v, mods, err := s.base(t, m, nil, depth+1)
		return v, append(append([]Mod(nil), mods...), c.Mods...), err
	}
	return [3]int64{}, nil, fmt.Errorf("unknown colour kind %q", c.Kind)
}

// parseHex parses a 6-digit hex colour into RGB components.
func parseHex(h string) ([3]int64, error) {
	if len(h) != 6 {
		return [3]int64{}, fmt.Errorf("bad hex colour %q", h)
	}
	v, err := strconv.ParseUint(h, 16, 32)
	if err != nil {
		return [3]int64{}, fmt.Errorf("bad hex colour %q", h)
	}
	return [3]int64{int64(v >> 16 & 0xff), int64(v >> 8 & 0xff), int64(v & 0xff)}, nil
}

type colorMode int

const (
	modeRGB  colorMode = iota // 0..255
	modeCRGB                  // linear 0..100000
	modeHSL                   // hue 0..21600000, sat and lum 0..100000
)

const (
	maxPercent = 100000
	maxDegree  = 21600000
	decGamma   = 2.3
	incGamma   = 1 / decGamma
)

type colorState struct {
	mode  colorMode
	c     [3]int64
	alpha int64
}

// gamma applies exponent g to a percent value.
func gamma(v int64, g float64) int64 {
	return int64(math.Pow(float64(v)/maxPercent, g)*maxPercent + 0.5)
}

// clamp limits v to the range lo..hi.
func clamp(v, lo, hi int64) int64 { return max(lo, min(v, hi)) }

// toRGB converts the colour state to sRGB 0..255.
func (s *colorState) toRGB() {
	switch s.mode {
	case modeCRGB:
		for i := range s.c {
			s.c[i] = gamma(clamp(s.c[i], 0, maxPercent), incGamma) * 255 / maxPercent
		}
	case modeHSL:
		h, sat, lum := s.c[0], s.c[1], s.c[2]
		var r, g, b float64
		if sat == 0 || lum == maxPercent {
			r = float64(lum) / maxPercent
			g, b = r, r
		} else if lum > 0 {
			hue := float64(h) / maxDegree * 6
			switch {
			case hue <= 1:
				r, g = 1, hue
			case hue <= 2:
				r, g = 2-hue, 1
			case hue <= 3:
				g, b = 1, hue-2
			case hue <= 4:
				g, b = 4-hue, 1
			case hue <= 5:
				r, b = hue-4, 1
			default:
				r, b = 1, 6-hue
			}
			fs := float64(sat) / maxPercent
			r, g, b = (r-0.5)*fs+0.5, (g-0.5)*fs+0.5, (b-0.5)*fs+0.5
			fl := 2*float64(lum)/maxPercent - 1
			if fl < 0 {
				k := fl + 1
				r, g, b = r*k, g*k, b*k
			} else if fl > 0 {
				k := 1 - fl
				r, g, b = 1-(1-r)*k, 1-(1-g)*k, 1-(1-b)*k
			}
		}
		s.c = [3]int64{int64(r*255 + 0.5), int64(g*255 + 0.5), int64(b*255 + 0.5)}
	}
	s.mode = modeRGB
	for i := range s.c {
		s.c[i] = clamp(s.c[i], 0, 255)
	}
}

// toCRGB converts the colour state to linear CRGB.
func (s *colorState) toCRGB() {
	if s.mode == modeHSL {
		s.toRGB()
	}
	if s.mode == modeRGB {
		for i := range s.c {
			s.c[i] = gamma(s.c[i]*maxPercent/255, decGamma)
		}
	}
	s.mode = modeCRGB
}

// toHSL converts the colour state to HSL components.
func (s *colorState) toHSL() {
	if s.mode == modeCRGB {
		s.toRGB()
	}
	if s.mode != modeRGB {
		return
	}
	r, g, b := float64(s.c[0])/255, float64(s.c[1])/255, float64(s.c[2])/255
	mn, mx := math.Min(r, math.Min(g, b)), math.Max(r, math.Max(g, b))
	d := mx - mn
	var h int64
	switch {
	case d == 0:
		h = 0
	case mx == r:
		h = int64(((g-b)/d*60+360)*60000+0.5) % maxDegree
	case mx == g:
		h = int64(((b-r)/d*60+120)*60000 + 0.5)
	default:
		h = int64(((r-g)/d*60+240)*60000 + 0.5)
	}
	l := int64((mn+mx)/2*maxPercent + 0.5)
	var sat int64
	switch {
	case l == 0 || l == maxPercent:
		sat = 0
	case l <= 50000:
		sat = int64(d/(mn+mx)*maxPercent + 0.5)
	default:
		sat = int64(d/(2-mx-mn)*maxPercent + 0.5)
	}
	s.c = [3]int64{h, sat, l}
	s.mode = modeHSL
}

// apply applies one colour modifier to the state.
func (s *colorState) apply(m Mod) {
	v, err := parsePct(m.Val)
	hasVal := err == nil
	set := func(i int, mx int64) {
		if hasVal && v >= 0 && v <= mx {
			s.c[i] = v
		}
	}
	mod := func(i int, mx int64) {
		if hasVal {
			s.c[i] = clamp(int64(float64(s.c[i])*float64(v)/maxPercent), 0, mx)
		}
	}
	off := func(i int, mx int64) {
		if hasVal {
			s.c[i] = clamp(s.c[i]+v, 0, mx)
		}
	}
	grayAtEnds := func() {
		if s.c[2] == 0 || s.c[2] == maxPercent {
			s.c[1] = 0
		}
	}
	switch m.Name {
	case "red", "green", "blue", "redMod", "greenMod", "blueMod", "redOff", "greenOff", "blueOff":
		s.toCRGB()
		i := map[byte]int{'r': 0, 'g': 1, 'b': 2}[m.Name[0]]
		switch {
		case strings.HasSuffix(m.Name, "Mod"):
			mod(i, maxPercent)
		case strings.HasSuffix(m.Name, "Off"):
			off(i, maxPercent)
		default:
			set(i, maxPercent)
		}
	case "hue":
		s.toHSL()
		set(0, maxDegree)
	case "hueMod":
		s.toHSL()
		mod(0, maxDegree)
	case "hueOff":
		s.toHSL()
		off(0, maxDegree)
	case "sat":
		s.toHSL()
		set(1, maxPercent)
	case "satMod":
		s.toHSL()
		mod(1, maxPercent)
	case "satOff":
		s.toHSL()
		off(1, maxPercent)
	case "lum":
		s.toHSL()
		set(2, maxPercent)
		grayAtEnds()
	case "lumMod":
		s.toHSL()
		mod(2, maxPercent)
		grayAtEnds()
	case "lumOff":
		s.toHSL()
		off(2, maxPercent)
		grayAtEnds()
	case "shade":
		s.toCRGB()
		if hasVal && v >= 0 && v <= maxPercent {
			f := float64(v) / maxPercent
			for i := range s.c {
				s.c[i] = int64(float64(s.c[i]) * f)
			}
		}
	case "tint":
		s.toCRGB()
		if hasVal && v >= 0 && v <= maxPercent {
			f := float64(v) / maxPercent
			for i := range s.c {
				s.c[i] = int64(maxPercent - float64(maxPercent-s.c[i])*f)
			}
		}
	case "gray":
		s.toRGB()
		g := (s.c[0]*22 + s.c[1]*72 + s.c[2]*6) / 100
		s.c = [3]int64{g, g, g}
	case "comp":
		s.toHSL()
		s.c[0] = (s.c[0] + 180*60000) % maxDegree
	case "inv":
		s.toCRGB()
		for i := range s.c {
			s.c[i] = maxPercent - s.c[i]
		}
	case "gamma":
		s.toCRGB()
		for i := range s.c {
			s.c[i] = gamma(s.c[i], incGamma)
		}
	case "invGamma":
		s.toCRGB()
		for i := range s.c {
			s.c[i] = gamma(s.c[i], decGamma)
		}
	case "alpha":
		if hasVal {
			s.alpha = clamp(v, 0, maxPercent)
		}
	case "alphaMod":
		if hasVal {
			s.alpha = clamp(int64(float64(s.alpha)*float64(v)/maxPercent), 0, maxPercent)
		}
	case "alphaOff":
		if hasVal {
			s.alpha = clamp(s.alpha+v, 0, maxPercent)
		}
	}
}
