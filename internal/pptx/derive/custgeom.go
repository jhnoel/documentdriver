package derive

import (
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"

	"github.com/jhnoel/documentdriver/internal/ooxml/tree"
)

// Handle is one adjust handle of a custom geometry (a:ahXY or a:ahPolar)
// as written: its kind, its attributes in written order and its position
// as guide references.
type Handle struct {
	Kind  string // ahXY or ahPolar
	Attrs [][2]string
	Pos   [2]string
}

// ReadCustomGeom reads an a:custGeom as written: its adjust values,
// guides, handles, connection sites, text rectangle and paths. Has*
// report which of the optional lists are present (an empty list and an
// absent one are different bytes, though neither has a value).
func ReadCustomGeom(cg *tree.Node) CustomGeom {
	var c CustomGeom
	if cg == nil {
		return c
	}
	if av := Child(cg, DML, "avLst"); av != nil {
		c.HasAv = true
		c.Av = ReadAdj(av)
	}
	if gd := Child(cg, DML, "gdLst"); gd != nil {
		c.HasGd = true
		c.Gd = ReadAdj(gd)
	}
	if ah := Child(cg, DML, "ahLst"); ah != nil {
		c.HasAh = true
		for h := range ah.ElementsSeq() {
			x := Handle{Kind: h.Local()}
			for _, a := range h.Attrs() {
				x.Attrs = append(x.Attrs, [2]string{a.Name, a.Value})
			}
			if pos := Child(h, DML, "pos"); pos != nil {
				x.Pos[0], _ = pos.Attr("x")
				x.Pos[1], _ = pos.Attr("y")
			}
			c.Ah = append(c.Ah, x)
		}
	}
	if cl := Child(cg, DML, "cxnLst"); cl != nil {
		c.HasCxn = true
		for cx := range cl.ElementsSeq() {
			ang, _ := cx.Attr("ang")
			pos := Child(cx, DML, "pos")
			if pos == nil {
				continue
			}
			x, _ := pos.Attr("x")
			y, _ := pos.Attr("y")
			c.Cxn = append(c.Cxn, [3]string{ang, x, y})
		}
	}
	if r := Child(cg, DML, "rect"); r != nil {
		var v [4]string
		for i, a := range []string{"l", "t", "r", "b"} {
			v[i], _ = r.Attr(a)
		}
		c.Rect = &v
	}
	if pl := Child(cg, DML, "pathLst"); pl != nil {
		c.HasPathLst = true
		for p := range pl.ElementsSeq() {
			var cp CustomPath
			cp.W, _ = p.Attr("w")
			cp.H, _ = p.Attr("h")
			cp.Fill, _ = p.Attr("fill")
			cp.Stroke, _ = p.Attr("stroke")
			for cmd := range p.ElementsSeq() {
				cc := CustomCmd{Op: cmd.Local()}
				if cc.Op == "arcTo" {
					for i, a := range []string{"wR", "hR", "stAng", "swAng"} {
						cc.Arc[i], _ = cmd.Attr(a)
					}
				}
				for pt := range cmd.ElementsSeq() {
					x, _ := pt.Attr("x")
					y, _ := pt.Attr("y")
					cc.Pts = append(cc.Pts, [2]string{x, y})
				}
				cp.Cmds = append(cp.Cmds, cc)
			}
			c.Paths = append(c.Paths, cp)
		}
	}
	return c
}

// ShownPath is one a:path as the views show it.
type ShownPath struct {
	// D is the path in the SVG subset (M L C Q Z), absolute, numbers
	// formatted by the caller's num: the views give EMU relative to the
	// slide-space box's top-left, before rotation and flips.
	D string
	// Fill is the path's fill mode (ST_PathFillMode) when it is not norm,
	// the default; "" for norm, written or not.
	Fill string
	// NoStroke is set when the path is not stroked (stroke="0").
	NoStroke bool
}

// Default reports whether the path has the default fill and stroke, so
// that the views can show it as a bare string.
func (p ShownPath) Default() bool { return p.Fill == "" && !p.NoStroke }

// CustomPaths evaluates a custom geometry's paths for display, one
// ShownPath per a:path. Coordinates are the shape's local coordinates
// (each path's own space w, h scaled onto the shape's extent w x h),
// multiplied by kx and ky (the ratio of the slide-space box to the stored
// extent). It fails with ErrArc for arcTo, for unknown commands and for
// unreadable stroke values.
func CustomPaths(c CustomGeom, w, h, kx, ky float64, num func(float64) string) ([]ShownPath, error) {
	g, err := c.guides(w, h)
	if err != nil {
		return nil, err
	}
	out := make([]ShownPath, 0, len(c.Paths))
	for pi, p := range c.Paths {
		sx, sy, err := pathScale(g, p, w, h)
		if err != nil {
			return nil, err
		}
		var b strings.Builder
		for _, cmd := range p.Cmds {
			var op string
			switch cmd.Op {
			case "moveTo":
				op = "M"
			case "lnTo":
				op = "L"
			case "cubicBezTo":
				op = "C"
			case "quadBezTo":
				op = "Q"
			case "close":
				op = "Z"
			case "arcTo":
				return nil, ErrArc
			default:
				return nil, fmt.Errorf("path %d: unknown command %q", pi, cmd.Op)
			}
			if b.Len() > 0 {
				b.WriteByte(' ')
			}
			b.WriteString(op)
			for _, ref := range cmd.Pts {
				pt, err := g.Point(ref)
				if err != nil {
					return nil, err
				}
				fmt.Fprintf(&b, " %s %s", num(pt.X*sx*kx), num(pt.Y*sy*ky))
			}
		}
		sp := ShownPath{D: b.String()}
		if p.Fill != "norm" {
			sp.Fill = p.Fill
		}
		if p.Stroke != "" {
			v, err := strconv.ParseBool(p.Stroke)
			if err != nil || (p.Stroke != "0" && p.Stroke != "1" && p.Stroke != "true" && p.Stroke != "false") {
				return nil, fmt.Errorf("path %d: stroke %q is not a boolean", pi, p.Stroke)
			}
			sp.NoStroke = !v
		}
		out = append(out, sp)
	}
	return out, nil
}

// pathScale returns the scale from a path's own coordinate space (its w
// and h, the shape's extent when absent or zero) to the shape's extent.
func pathScale(g *Guides, p CustomPath, w, h float64) (float64, float64, error) {
	sx, sy := 1.0, 1.0
	if p.W != "" {
		pw, err := g.Value(p.W)
		if err != nil {
			return 0, 0, err
		}
		if pw != 0 {
			sx = w / pw
		}
	}
	if p.H != "" {
		ph, err := g.Value(p.H)
		if err != nil {
			return 0, 0, err
		}
		if ph != 0 {
			sy = h / ph
		}
	}
	return sx, sy, nil
}

// PathUsesGuides reports whether any point of the custom geometry's paths,
// or a path's extent, refers to a guide by name rather than a literal.
func (c CustomGeom) PathUsesGuides() bool {
	lit := func(s string) bool {
		_, err := strconv.ParseInt(strings.TrimSpace(s), 10, 64)
		return s == "" || err == nil
	}
	for _, p := range c.Paths {
		if !lit(p.W) || !lit(p.H) {
			return true
		}
		for _, cmd := range p.Cmds {
			for _, pt := range cmd.Pts {
				if !lit(pt[0]) || !lit(pt[1]) {
					return true
				}
			}
		}
	}
	return false
}

// ShownRef is a coordinate or angle of a custom geometry as the views
// show it: a guide name as written, or a literal (EMU relative to the
// box, scaled like the path; or degrees for an angle).
type ShownRef struct {
	Name string
	V    float64
}

// ShownCustom is the non-path part of a custom geometry as the views show
// it (--detail full): adjust values and guides as written, handles as
// written, connection sites and the text rectangle with literal
// coordinates scaled like the path. Has* report which lists are present.
type ShownCustom struct {
	Av, Gd                      []Adjust
	HasAv, HasGd, HasAh, HasCxn bool
	Ah                          []Handle
	Cxn                         [][3]ShownRef // ang, x, y
	Rect                        *[4]ShownRef
}

// ShowCustom evaluates what the views show of a custom geometry besides
// its paths, for a shape whose box is the stored extent scaled by kx, ky.
// Literal coordinates of sites and the text rectangle are in the shape's
// own coordinates (not a path's), so they scale by kx, ky alone; literal
// site angles are shown in degrees.
func ShowCustom(c CustomGeom, kx, ky float64) (ShownCustom, error) {
	s := ShownCustom{Av: c.Av, Gd: c.Gd, HasAv: c.HasAv, HasGd: c.HasGd, HasAh: c.HasAh, HasCxn: c.HasCxn, Ah: c.Ah}
	coord := func(v string, k float64) (ShownRef, error) {
		v = strings.TrimSpace(v)
		if v == "" {
			return ShownRef{}, errors.New("empty coordinate")
		}
		if n, err := strconv.ParseInt(v, 10, 64); err == nil {
			return ShownRef{V: float64(n) * k}, nil
		}
		if _, err := strconv.ParseFloat(v, 64); err == nil {
			return ShownRef{}, fmt.Errorf("coordinate %q is not a whole number", v)
		}
		return ShownRef{Name: v}, nil
	}
	angle := func(v string) (ShownRef, error) {
		v = strings.TrimSpace(v)
		if n, err := strconv.ParseInt(v, 10, 64); err == nil {
			return ShownRef{V: float64(n) / AngleUnit}, nil
		}
		if v == "" {
			return ShownRef{}, errors.New("empty angle")
		}
		return ShownRef{Name: v}, nil
	}
	for i, x := range c.Cxn {
		a, err := angle(x[0])
		if err != nil {
			return s, fmt.Errorf("site %d: %w", i, err)
		}
		px, err := coord(x[1], kx)
		if err != nil {
			return s, fmt.Errorf("site %d: %w", i, err)
		}
		py, err := coord(x[2], ky)
		if err != nil {
			return s, fmt.Errorf("site %d: %w", i, err)
		}
		s.Cxn = append(s.Cxn, [3]ShownRef{a, px, py})
	}
	if c.Rect != nil {
		var r [4]ShownRef
		for i, v := range c.Rect {
			k := kx
			if i%2 == 1 {
				k = ky
			}
			var err error
			if r[i], err = coord(v, k); err != nil {
				return s, fmt.Errorf("text rectangle: %w", err)
			}
		}
		s.Rect = &r
	}
	return s, nil
}

// PathCmd is one command of a parsed path in the SVG subset, absolute:
// M (moveTo), L (lnTo), C (cubicBezTo), Q (quadBezTo) or Z (close),
// with its points.
type PathCmd struct {
	Op  byte
	Pts []Pt
}

// ErrPathSyntax is wrapped by ParsePath for paths outside the subset.
var ErrPathSyntax = errors.New("path is not in the SVG path subset (M L H V C Q Z)")

// ParsePath parses a path in the SVG subset M L H V C Q Z, absolute
// (upper case) and relative (lower case), with numbers separated by
// spaces or commas and implicit repeats (extra pairs after M are lines,
// as in SVG). H and V become lines from the current point. Each command
// maps to one DrawingML path command; arcs (A) are refused because
// DrawingML's arcTo has different semantics, and so are the smooth
// curves S and T, which have no DrawingML counterpart.
func ParsePath(s string) ([]PathCmd, error) {
	p := &pathParser{s: s}
	var out []PathCmd
	var cur, start Pt
	var op byte
	started := false
	for {
		p.skip()
		if p.i >= len(p.s) {
			break
		}
		c := p.s[p.i]
		if isPathLetter(c) {
			p.i++
			op = c
		} else if op == 0 {
			return nil, fmt.Errorf("%w: %q: a path starts with M", ErrPathSyntax, s)
		} else if op == 'Z' || op == 'z' {
			return nil, fmt.Errorf("%w: number after Z at %d", ErrPathSyntax, p.i)
		}
		// op holds for repeated argument groups; a number here repeats it.
		switch op {
		case 'A', 'a':
			return nil, fmt.Errorf("%w: arcs (A) are not in the subset, because DrawingML's arcTo has different semantics; approximate each arc with cubic curves (C)", ErrPathSyntax)
		case 'S', 's', 'T', 't':
			return nil, fmt.Errorf("%w: smooth curves (%c) are not in the subset; give the control points with C or Q", ErrPathSyntax, op)
		}
		if !started && op != 'M' && op != 'm' {
			return nil, fmt.Errorf("%w: %q: a path starts with M", ErrPathSyntax, s)
		}
		rel := op >= 'a'
		up := op &^ 0x20
		base := func() Pt {
			if rel {
				return cur
			}
			return Pt{}
		}
		switch up {
		case 'Z':
			out = append(out, PathCmd{Op: 'Z'})
			cur = start
			continue
		case 'M', 'L':
			pt, err := p.pair()
			if err != nil {
				return nil, err
			}
			b := base()
			pt = Pt{b.X + pt.X, b.Y + pt.Y}
			if up == 'M' {
				out = append(out, PathCmd{Op: 'M', Pts: []Pt{pt}})
				start = pt
				started = true
				// Further pairs are lines (SVG's implicit lineto).
				if rel {
					op = 'l'
				} else {
					op = 'L'
				}
			} else {
				out = append(out, PathCmd{Op: 'L', Pts: []Pt{pt}})
			}
			cur = pt
		case 'H', 'V':
			v, err := p.num()
			if err != nil {
				return nil, err
			}
			pt := cur
			switch {
			case up == 'H' && rel:
				pt.X += v
			case up == 'H':
				pt.X = v
			case rel:
				pt.Y += v
			default:
				pt.Y = v
			}
			out = append(out, PathCmd{Op: 'L', Pts: []Pt{pt}})
			cur = pt
		case 'C', 'Q':
			n := 3
			if up == 'Q' {
				n = 2
			}
			b := base()
			pts := make([]Pt, n)
			for k := range pts {
				pt, err := p.pair()
				if err != nil {
					return nil, err
				}
				pts[k] = Pt{b.X + pt.X, b.Y + pt.Y}
			}
			out = append(out, PathCmd{Op: up, Pts: pts})
			cur = pts[n-1]
		default:
			return nil, fmt.Errorf("%w: unknown command %q", ErrPathSyntax, string(op))
		}
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("%w: empty path", ErrPathSyntax)
	}
	// Relative commands add up; a sum can overflow to infinity.
	for _, c := range out {
		for _, pt := range c.Pts {
			if math.IsInf(pt.X, 0) || math.IsInf(pt.Y, 0) {
				return nil, fmt.Errorf("%w: coordinates overflow", ErrPathSyntax)
			}
		}
	}
	return out, nil
}

// isPathLetter reports whether c is an alphabetic path command letter.
func isPathLetter(c byte) bool {
	return (c >= 'A' && c <= 'Z' || c >= 'a' && c <= 'z') && c != 'e' && c != 'E'
}

type pathParser struct {
	s string
	i int
}

// skip advances past separators in the path string.
func (p *pathParser) skip() {
	for p.i < len(p.s) && (p.s[p.i] == ' ' || p.s[p.i] == ',' || p.s[p.i] == '\t' || p.s[p.i] == '\n' || p.s[p.i] == '\r') {
		p.i++
	}
}

// num parses the next number from the path string.
func (p *pathParser) num() (float64, error) {
	p.skip()
	j := p.i
	if j < len(p.s) && (p.s[j] == '+' || p.s[j] == '-') {
		j++
	}
	digits := 0
	for j < len(p.s) && p.s[j] >= '0' && p.s[j] <= '9' {
		j++
		digits++
	}
	if j < len(p.s) && p.s[j] == '.' {
		j++
		for j < len(p.s) && p.s[j] >= '0' && p.s[j] <= '9' {
			j++
			digits++
		}
	}
	if digits == 0 {
		if p.i >= len(p.s) {
			return 0, fmt.Errorf("%w: path ends where a number is expected", ErrPathSyntax)
		}
		return 0, fmt.Errorf("%w: expected a number at %d, found %q", ErrPathSyntax, p.i, string(p.s[p.i]))
	}
	if j < len(p.s) && (p.s[j] == 'e' || p.s[j] == 'E') {
		k := j + 1
		if k < len(p.s) && (p.s[k] == '+' || p.s[k] == '-') {
			k++
		}
		if k < len(p.s) && p.s[k] >= '0' && p.s[k] <= '9' {
			for k < len(p.s) && p.s[k] >= '0' && p.s[k] <= '9' {
				k++
			}
			j = k
		}
	}
	v, err := strconv.ParseFloat(p.s[p.i:j], 64)
	if err != nil || math.IsInf(v, 0) || math.IsNaN(v) {
		return 0, fmt.Errorf("%w: bad number %q", ErrPathSyntax, p.s[p.i:j])
	}
	p.i = j
	return v + 0, nil // -0 reads as 0, as absolute M, L, C and Q (0 + -0) already did
}

// pair parses the next x,y point pair.
func (p *pathParser) pair() (Pt, error) {
	x, err := p.num()
	if err != nil {
		return Pt{}, err
	}
	y, err := p.num()
	if err != nil {
		return Pt{}, err
	}
	return Pt{x, y}, nil
}

// FormatPath writes parsed commands in the form the views show: absolute
// commands separated by spaces, numbers formatted by num.
func FormatPath(cmds []PathCmd, num func(float64) string) string {
	var b strings.Builder
	for _, c := range cmds {
		if b.Len() > 0 {
			b.WriteByte(' ')
		}
		b.WriteByte(c.Op)
		for _, p := range c.Pts {
			b.WriteString(" " + num(p.X) + " " + num(p.Y))
		}
	}
	return b.String()
}

// AxisScales returns how much m stretches the local x and y axes: the
// ratio of a placed object's slide-space box to its stored extent, also
// when an extent is zero. Custom paths, sites and text rectangles are
// shown scaled by them.
func AxisScales(m Affine) (kx, ky float64) {
	return math.Hypot(m.A, m.B), math.Hypot(m.C, m.D)
}
