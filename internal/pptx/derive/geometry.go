package derive

import (
	"errors"
	"fmt"
	"math"
	"strings"
)

// Site is a connection site in slide space.
type Site struct {
	Index int
	Pt
	// Ang is the site's outward direction in degrees, in slide space.
	Ang float64
}

// ErrNoSiteTable is returned for presets outside the connection-site table.
var ErrNoSiteTable = errors.New("preset not in the connection-site table")

// PresetSites returns the connection sites of preset prst with adjust
// values adj, for an object whose local box is w x h and whose local
// coordinates map to slide space through m. Site indices follow the order
// of the preset's cxnLst in ECMA-376 presetShapeDefinitions.xml.
func PresetSites(prst string, adj []Adjust, w, h float64, m Affine) ([]Site, error) {
	d := presetTable[prst]
	if d == nil || d.cxn == nil {
		return nil, ErrNoSiteTable
	}
	g, err := newGuides(w, h, d.av, adj, d.gd)
	if err != nil {
		return nil, err
	}
	return sites(g, d.cxn, m)
}

// CustomGeom is an a:custGeom as written (ReadCustomGeom): adjust and
// guide formulas, adjust handles, connection sites, the text rectangle
// and paths.
type CustomGeom struct {
	Av    []Adjust
	Gd    []Adjust
	Ah    []Handle
	Cxn   [][3]string // ang, x, y
	Rect  *[4]string  // l, t, r, b; nil when absent
	Paths []CustomPath
	// Which optional lists are written (present, even if empty).
	HasAv, HasGd, HasAh, HasCxn, HasPathLst bool
}

// CustomPath is one a:path of a custGeom.
type CustomPath struct {
	W, H   string // path coordinate space; "" means the shape's extent
	Fill   string // fill attribute as written, "" if absent
	Stroke string // stroke attribute as written, "" if absent
	Cmds   []CustomCmd
}

// CustomCmd is one path command: its local name and attribute or point
// references. For arcTo, Pts is empty and Arc holds wR, hR, stAng, swAng.
type CustomCmd struct {
	Op  string
	Pts [][2]string
	Arc [4]string
}

// guides builds evaluated guides from the custom geometry's adjusts.
func (c CustomGeom) guides(w, h float64) (*Guides, error) {
	var av, gd []guide
	for _, a := range c.Av {
		av = append(av, guide{a.Name, a.Fmla})
	}
	for _, a := range c.Gd {
		gd = append(gd, guide{a.Name, a.Fmla})
	}
	return newGuides(w, h, av, nil, gd)
}

// CustomSites returns the connection sites of a custom geometry. Sites are
// positioned in the shape's own coordinates, so path coordinate spaces do
// not apply.
func CustomSites(c CustomGeom, w, h float64, m Affine) ([]Site, error) {
	g, err := c.guides(w, h)
	if err != nil {
		return nil, err
	}
	var defs []cxnDef
	for _, s := range c.Cxn {
		defs = append(defs, cxnDef{s[0], s[1], s[2]})
	}
	return sites(g, defs, m)
}

// sites positions connection sites from guide definitions.
func sites(g *Guides, defs []cxnDef, m Affine) ([]Site, error) {
	out := make([]Site, 0, len(defs))
	for i, d := range defs {
		p, err := g.Point([2]string{d.x, d.y})
		if err != nil {
			return nil, fmt.Errorf("site %d: %w", i, err)
		}
		ang, err := g.Value(d.ang)
		if err != nil {
			return nil, fmt.Errorf("site %d: %w", i, err)
		}
		s, c := sincosDeg(ang / angleUnit)
		v := m.Vec(Pt{c, s})
		out = append(out, Site{Index: i, Pt: m.Apply(p), Ang: normDeg(math.Atan2(v.Y, v.X) * 180 / math.Pi)})
	}
	return out, nil
}

// ErrArc is returned when a path uses arcTo, which the SVG path subset
// (M L H V C Q Z) cannot express one-to-one.
var ErrArc = errors.New("path uses arcTo, which the M L H V C Q Z subset cannot express")

// CustomPathSVG renders a custom geometry's paths as one SVG path string.
// Coordinates are the shape's local coordinates (each path's own space
// w, h scaled onto the shape's extent w x h), multiplied by kx and ky; a
// caller passes the ratio of the slide-space box to the stored extent to
// get coordinates relative to the box. It fails with ErrArc for arcTo and
// for unknown commands.
func CustomPathSVG(c CustomGeom, w, h, kx, ky float64, num func(float64) string) (string, error) {
	paths, err := CustomPaths(c, w, h, kx, ky, num)
	if err != nil {
		return "", err
	}
	var parts []string
	for _, p := range paths {
		if p.D != "" {
			parts = append(parts, p.D)
		}
	}
	return strings.Join(parts, " "), nil
}

// Connector is a connector's geometry in slide space.
type Connector struct {
	From, To Pt
	// First is the direction of the first segment, "h" or "v", for elbow
	// connectors (bentConnector2-5); "" otherwise.
	First string
	// Elbows are the interior vertices of an elbow connector's path.
	Elbows []Pt
	// Curve is the path of a curved connector as cubic segments: start,
	// then three points per segment. Nil otherwise.
	Curve []Pt
}

// ErrNotConnector is returned for geometries outside the connector table.
var ErrNotConnector = errors.New("geometry is not a tabulated connector preset")

// ConnectorGeom evaluates a connector preset (line, straightConnector1,
// bentConnector2-5, curvedConnector2-5) for a local w x h box mapped to
// slide space by m. Endpoints are the path's first and last points.
func ConnectorGeom(prst string, adj []Adjust, w, h float64, m Affine) (Connector, error) {
	d := presetTable[prst]
	if d == nil || d.paths == nil {
		return Connector{}, ErrNotConnector
	}
	g, err := newGuides(w, h, d.av, adj, d.gd)
	if err != nil {
		return Connector{}, err
	}
	var pts []Pt
	var curve []Pt
	for _, p := range d.paths {
		for _, c := range p.cmds {
			for i, ref := range c.pts {
				pt, err := g.Point(ref)
				if err != nil {
					return Connector{}, err
				}
				sp := m.Apply(pt)
				curve = append(curve, sp)
				if i == len(c.pts)-1 {
					pts = append(pts, sp) // on-path point
				}
			}
		}
	}
	if len(pts) < 2 {
		return Connector{}, fmt.Errorf("%s: path has fewer than two points", prst)
	}
	out := Connector{From: pts[0], To: pts[len(pts)-1]}
	switch {
	case strings.HasPrefix(prst, "bentConnector"):
		// The first segment runs along the local x axis in every
		// bentConnector path; its slide direction is what rotation
		// encodes.
		v := m.Vec(Pt{1, 0})
		out.First = "h"
		if math.Abs(v.Y) > math.Abs(v.X) {
			out.First = "v"
		}
		out.Elbows = append([]Pt(nil), pts[1:len(pts)-1]...)
	case strings.HasPrefix(prst, "curvedConnector"):
		out.Curve = curve
	}
	return out, nil
}

// ConnectorXfrm inverts ConnectorGeom's endpoints: it returns the stored
// transform, in the parent's child space and before rounding, whose
// connector path runs from `from` to `to` in slide space under parent p.
// Every tabulated connector path starts at its local box's top-left and
// ends at its bottom-right, so the box, rotation and flips are all that
// place the endpoints.
//
// first is the wanted direction of an elbow connector's first segment in
// slide space, "h" or "v" ("" for straight and curved connectors, which
// do not care). cur is the existing stored transform, nil for a new
// connector. When cur's rotation and flips still express the endpoints
// (and first), they are kept and only the box changes. Otherwise the
// rotation is 0, or 90 degrees when first is "v", with the flips the
// endpoints need. Under a rotated group the rotation is chosen so that
// first holds in slide space. It fails with ErrInexpressible when the
// parent map is singular or when no such rotation gives first.
func ConnectorXfrm(from, to Pt, first string, cur *Xfrm, p Parent) (Xfrm, error) {
	inv, ok := p.M.Invert()
	if !ok {
		return Xfrm{}, ErrInexpressible
	}
	f, t := inv.Apply(from), inv.Apply(to)
	d := Pt{t.X - f.X, t.Y - f.Y}
	build := func(rot float64, fh, fv bool) Xfrm {
		s, c := sincosDeg(-rot)
		v := Pt{c*d.X - s*d.Y, s*d.X + c*d.Y} // R(-rot) d = (fx w, fy h)
		w, h := math.Abs(v.X), math.Abs(v.Y)
		if v.X != 0 {
			fh = v.X < 0
		}
		if v.Y != 0 {
			fv = v.Y < 0
		}
		cx, cy := (f.X+t.X)/2, (f.Y+t.Y)/2
		return Xfrm{X: cx - w/2, Y: cy - h/2, W: w, H: h, Rot: rot, FlipH: fh, FlipV: fv}
	}
	firstOf := func(rot float64) string {
		s, c := sincosDeg(rot)
		v := p.M.Vec(Pt{c, s})
		if math.Abs(v.Y) > math.Abs(v.X) {
			return "v"
		}
		return "h"
	}
	if cur != nil {
		x := build(cur.Rot, cur.FlipH, cur.FlipV)
		if x.FlipH == cur.FlipH && x.FlipV == cur.FlipV && (first == "" || firstOf(cur.Rot) == first) {
			return x, nil
		}
	}
	switch first {
	case "":
		return build(0, false, false), nil
	case "h", "v":
	default:
		return Xfrm{}, fmt.Errorf("first segment %q: want h or v", first)
	}
	for _, rot := range []float64{0, 90} {
		if firstOf(rot) == first {
			return build(rot, false, false), nil
		}
	}
	return Xfrm{}, fmt.Errorf("%w: no rotation gives a first segment %q under the group", ErrInexpressible, first)
}

// IsConnectorPreset reports whether prst is a tabulated connector preset.
func IsConnectorPreset(prst string) bool {
	d := presetTable[prst]
	return d != nil && d.paths != nil
}
