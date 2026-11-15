package derive

import (
	"errors"
	"fmt"
	"math"
	"strconv"
)

// Pt is a point. Coordinates are in EMU unless stated otherwise.
type Pt struct{ X, Y float64 }

// Affine is a 2D affine map in the SVG matrix(a b c d e f) convention:
//
//	x' = A*x + C*y + E
//	y' = B*x + D*y + F
//
// The y axis points down, so a positive rotation is clockwise on screen,
// as in DrawingML.
type Affine struct{ A, B, C, D, E, F float64 }

// Identity is the identity map.
var Identity = Affine{A: 1, D: 1}

// Apply maps p.
func (m Affine) Apply(p Pt) Pt {
	return Pt{m.A*p.X + m.C*p.Y + m.E, m.B*p.X + m.D*p.Y + m.F}
}

// Vec maps the vector v (no translation).
func (m Affine) Vec(v Pt) Pt {
	return Pt{m.A*v.X + m.C*v.Y, m.B*v.X + m.D*v.Y}
}

// Then returns the map that applies m, then n.
func (m Affine) Then(n Affine) Affine {
	return Affine{
		A: n.A*m.A + n.C*m.B,
		B: n.B*m.A + n.D*m.B,
		C: n.A*m.C + n.C*m.D,
		D: n.B*m.C + n.D*m.D,
		E: n.A*m.E + n.C*m.F + n.E,
		F: n.B*m.E + n.D*m.F + n.F,
	}
}

// Invert returns the inverse map, and false if m is singular.
func (m Affine) Invert() (Affine, bool) {
	det := m.A*m.D - m.B*m.C
	if det == 0 || math.IsNaN(det) {
		return Affine{}, false
	}
	a, b, c, d := m.D/det, -m.B/det, -m.C/det, m.A/det
	return Affine{A: a, B: b, C: c, D: d, E: -(a*m.E + c*m.F), F: -(b*m.E + d*m.F)}, true
}

// translate returns a translation affine map.
func translate(x, y float64) Affine { return Affine{A: 1, D: 1, E: x, F: y} }

// scale returns a scaling affine map.
func scale(x, y float64) Affine { return Affine{A: x, D: y} }

// rotate returns a rotation affine map in degrees.
func rotate(deg float64) Affine {
	s, c := sincosDeg(deg)
	return Affine{A: c, B: s, C: -s, D: c}
}

// sincosDeg returns exact values at multiples of 90 degrees, so that
// right-angle rotations introduce no rounding.
func sincosDeg(deg float64) (s, c float64) {
	d := math.Mod(deg, 360)
	if d < 0 {
		d += 360
	}
	switch d {
	case 0:
		return 0, 1
	case 90:
		return 1, 0
	case 180:
		return 0, -1
	case 270:
		return -1, 0
	}
	return math.Sincos(d * math.Pi / 180)
}

// Xfrm is a DrawingML transform (a:xfrm): offset and extent in EMU,
// rotation in degrees clockwise, and flips.
type Xfrm struct {
	X, Y, W, H   float64
	Rot          float64
	FlipH, FlipV bool
}

// GroupXfrm is a group's transform: its own Xfrm plus the child
// coordinate space (a:chOff, a:chExt) that is mapped onto it.
type GroupXfrm struct {
	Xfrm
	ChX, ChY, ChW, ChH float64
}

// Matrix maps the shape's local coordinates (0..W, 0..H, before flips and
// rotation) to its parent's coordinate space. Flips and rotation are about
// the box centre: flips first, then rotation.
func (x Xfrm) Matrix() Affine {
	fx, fy := 1.0, 1.0
	if x.FlipH {
		fx = -1
	}
	if x.FlipV {
		fy = -1
	}
	cx, cy := x.X+x.W/2, x.Y+x.H/2
	return translate(-x.W/2, -x.H/2).Then(scale(fx, fy)).Then(rotate(x.Rot)).Then(translate(cx, cy))
}

// ErrDegenerateGroup is returned when a group's child extent is zero while
// its extent is not, so the child-to-group scale is undefined.
var ErrDegenerateGroup = errors.New("group child extent is zero but its extent is not")

// ChildMatrix maps the group's child coordinate space to the group's
// parent space: chOff/chExt onto off/ext, then the group's flips and
// rotation about its centre. A zero child extent with a zero extent scales
// by 1 on that axis, which is what an empty group frame implies.
func (g GroupXfrm) ChildMatrix() (Affine, error) {
	sx, err := axisScale(g.W, g.ChW)
	if err != nil {
		return Affine{}, err
	}
	sy, err := axisScale(g.H, g.ChH)
	if err != nil {
		return Affine{}, err
	}
	toLocal := translate(-g.ChX, -g.ChY).Then(scale(sx, sy))
	return toLocal.Then(g.Matrix()), nil
}

// axisScale returns the scale factor from child to extent, erroring on degenerate extents.
func axisScale(ext, ch float64) (float64, error) {
	if ch == 0 {
		if ext == 0 {
			return 1, nil
		}
		return 0, ErrDegenerateGroup
	}
	return ext / ch, nil
}

// Placed is an object's transform resolved into slide space.
type Placed struct {
	// Box is the unrotated box in slide space whose centre is the
	// object's centre: rotation and flips apply about that centre.
	Box Xfrm
	// M maps the object's local coordinates (0..w, 0..h of its stored
	// extent) to slide space.
	M Affine
	// Skew is how far, in degrees, the mapped axes are from perpendicular
	// when that is within SkewTolerance; Box is then the rectangle that
	// keeps the mapped x axis. Zero for an exact rectangle.
	Skew float64
}

// ErrSkew is returned when an object's image under its groups is not a
// rectangle (a rotated child under a non-uniformly scaled group), so it
// has no faithful box, rotation and flips.
var ErrSkew = errors.New("transform is not expressible as box, rotation and flips (skew)")

// SkewTolerance is the largest corner displacement, in EMU, between an
// object's true image under its groups and the rectangle reported for it.
// PowerPoint stores group extents as whole EMU, so nested rotated groups
// carry slightly non-uniform scales: in the corpus these give skews of
// 4e-6 to 0.045 degrees (corner displacement well under 0.5 pt), while
// genuinely skewed children are 2.7 degrees and more. Half a point
// separates the two and matches GlueTolerance.
const SkewTolerance = 0.5 * 12700

// Parent is the map from a group's child space to slide space, with the
// parity of the flips applied on the way. The slide itself is Root.
type Parent struct {
	M            Affine
	FlipH, FlipV bool
}

// Root is the slide's own coordinate space.
var Root = Parent{M: Identity}

// Child returns the parent map for the children of group g placed under p.
func (p Parent) Child(g GroupXfrm) (Parent, error) {
	m, err := g.ChildMatrix()
	if err != nil {
		return Parent{}, err
	}
	return Parent{M: m.Then(p.M), FlipH: p.FlipH != g.FlipH, FlipV: p.FlipV != g.FlipV}, nil
}

// Place resolves an object with local transform x under parent p. The
// slide-space flips are the parity of the object's and its groups' flips
// on each axis; the rotation is then unique.
func Place(x Xfrm, p Parent) (Placed, error) {
	m := x.Matrix().Then(p.M)
	box, skew, err := decompose(m, x.W, x.H, x.FlipV != p.FlipV)
	if err != nil {
		return Placed{M: m}, err
	}
	return Placed{Box: box, M: m, Skew: skew}, nil
}

// decompose writes the map m of a w x h local box as a box, rotation and
// flips in m's target space. flipV is the wanted vertical flip; flipH
// follows from the sign of the determinant (an odd number of reflections
// means exactly one flip). With both fixed the rotation is unique.
func decompose(m Affine, w, h float64, flipV bool) (Xfrm, float64, error) {
	c := m.Apply(Pt{w / 2, h / 2})
	// Unit axes of the local frame mapped through m.
	ax := m.Vec(Pt{1, 0})
	ay := m.Vec(Pt{0, 1})
	sxl, syl := math.Hypot(ax.X, ax.Y), math.Hypot(ay.X, ay.Y)
	if sxl == 0 || syl == 0 {
		return Xfrm{}, 0, ErrSkew
	}
	dot := (ax.X*ay.X + ax.Y*ay.Y) / (sxl * syl)
	dev := 0.0
	if math.Abs(dot) > 1e-12 {
		dev = math.Abs(90 - math.Acos(math.Max(-1, math.Min(1, dot)))*180/math.Pi)
		// Keeping the x axis, the far corner moves by H sin(dev).
		if h*syl*math.Abs(dot) > SkewTolerance {
			return Xfrm{}, dev, fmt.Errorf("%w: axes %s degrees from perpendicular", ErrSkew, strconv.FormatFloat(dev, 'g', 3, 64))
		}
	}
	det := ax.X*ay.Y - ax.Y*ay.X
	fh := (det < 0) != flipV
	// m's linear part is R(rot) * diag(sx*fhs, sy*fvs), so the mapped x
	// axis is fhs * sx * (cos rot, sin rot).
	fhs := 1.0
	if fh {
		fhs = -1
	}
	rot := normDeg(math.Atan2(ax.Y*fhs, ax.X*fhs) * 180 / math.Pi)
	W, H := w*sxl, h*syl
	return Xfrm{X: c.X - W/2, Y: c.Y - H/2, W: W, H: H, Rot: rot, FlipH: fh, FlipV: flipV}, dev, nil
}

// normDeg normalises an angle to [0, 360), snapping values within 1e-9 of
// a whole degree count so right angles stay exact.
func normDeg(d float64) float64 {
	d = math.Mod(d, 360)
	if d < 0 {
		d += 360
	}
	if r := math.Round(d); math.Abs(d-r) < 1e-9 {
		d = r
	}
	if d >= 360 {
		d -= 360
	}
	return d
}

// ErrInexpressible is returned by Unplace when the requested slide-space
// transform cannot be written in the group's child space with DrawingML's
// box, rotation and flips.
var ErrInexpressible = errors.New("transform not expressible in the group's coordinate space")

// Unplace inverts Place: given the slide-space transform want (as Place
// reports it) and the parent map, it returns the local Xfrm to store. It
// fails with ErrInexpressible when the result would need skew, which
// happens for a rotated object under a non-uniformly scaled rotated
// group, and when the parent map is singular.
func Unplace(want Xfrm, p Parent) (Xfrm, error) {
	inv, ok := p.M.Invert()
	if !ok {
		return Xfrm{}, ErrInexpressible
	}
	m := want.Matrix().Then(inv)
	// m maps want's local box (0..W, 0..H) into child space. The stored
	// local box has extent (w, h) with m = Matrix(local) * diag(w/W, h/H)
	// in local coordinates, so decompose with the requested extent.
	x, _, err := decompose(m, want.W, want.H, want.FlipV != p.FlipV)
	if err != nil {
		return Xfrm{}, ErrInexpressible
	}
	// decompose measured any skew in the child space, whose units the
	// group's scale distorts; measure it where it is shown, in slide space.
	if _, err := Place(x, p); err != nil {
		return Xfrm{}, ErrInexpressible
	}
	return x, nil
}
