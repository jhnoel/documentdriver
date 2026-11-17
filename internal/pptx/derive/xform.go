package derive

import (
	"errors"

	"github.com/jhnoel/documentdriver/internal/geom"
)

// Pt is a point. Coordinates are in EMU unless stated otherwise.
type Pt = geom.Pt

// Affine is a 2D affine map in the SVG matrix convention (geom.Affine);
// the y axis points down, so a positive rotation is clockwise on screen,
// as in DrawingML.
type Affine = geom.Affine

// Identity is the identity map.
var Identity = geom.Identity

// Xfrm is a DrawingML transform (a:xfrm): offset and extent in EMU,
// rotation in degrees clockwise, and flips, as a geom.Box; its Matrix
// maps the shape's local coordinates to its parent's coordinate space.
type Xfrm = geom.Box

// GroupXfrm is a group's transform: its own Xfrm plus the child
// coordinate space (a:chOff, a:chExt) that is mapped onto it.
type GroupXfrm struct {
	Xfrm
	ChX, ChY, ChW, ChH float64
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
	toLocal := geom.Translate(-g.ChX, -g.ChY).Then(geom.Scale(sx, sy))
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
var ErrSkew = geom.ErrSkew

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
	box, skew, err := geom.Decompose(m, x.W, x.H, x.FlipV != p.FlipV, SkewTolerance)
	if err != nil {
		return Placed{M: m}, err
	}
	return Placed{Box: box, M: m, Skew: skew}, nil
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
	x, _, err := geom.Decompose(m, want.W, want.H, want.FlipV != p.FlipV, SkewTolerance)
	if err != nil {
		return Xfrm{}, ErrInexpressible
	}
	// Decompose measured any skew in the child space, whose units the
	// group's scale distorts; measure it where it is shown, in slide space.
	if _, err := Place(x, p); err != nil {
		return Xfrm{}, ErrInexpressible
	}
	return x, nil
}
