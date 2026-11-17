// Package place turns ShapeSheet transforms into the view's frame: page
// space with the origin at the top-left corner, y down, in points
// (docs/vsdx-design.md, decision b). Visio stores inches with y up from
// the page's lower-left corner; a shape's local point p maps to its
// parent as Pin + R(Angle) * diag(fx, fy) * (p - LocPin), Angle in
// radians counter-clockwise and the flips about the pin, before the
// rotation (format notes section 3). Group children compose with their
// groups. The box the view shows is geom.Decompose of the composed map,
// as pptx shows a group child. Pure arithmetic, no tree.
package place

import (
	"github.com/jhnoel/documentdriver/internal/geom"
	"github.com/jhnoel/documentdriver/internal/vsdx/vsd"
)

// SkewTolerance is the largest corner displacement, in points, between
// a shape's true image under its groups and the box reported for it;
// half a point, as pptx (derive.SkewTolerance) and GlueTolerance.
const SkewTolerance = 0.5

// GlueTolerance is how far, in points, a glued end may sit from its
// site before read reports stale-connector (as pptx).
const GlueTolerance = 0.5

// Transform is a shape's effective transform cells in stored units
// (inches, radians).
type Transform struct {
	PinX, PinY, Width, Height, LocPinX, LocPinY, Angle float64
	FlipX, FlipY                                       bool
}

// Local returns the map from the shape's local space (inches, y up,
// 0..Width x 0..Height) to its parent's space (inches, y up). Chunk P1.
func (t Transform) Local() geom.Affine {
	fx, fy := 1.0, 1.0
	if t.FlipX {
		fx = -1
	}
	if t.FlipY {
		fy = -1
	}
	// vsd.Degrees returns the negated angle (the view's clockwise
	// degrees), so negating it again gives the counter-clockwise angle
	// in degrees that geom.Rotate lays out in a y-up space.
	return geom.Translate(-t.LocPinX, -t.LocPinY).
		Then(geom.Scale(fx, fy)).
		Then(geom.Rotate(-vsd.Degrees(t.Angle))).
		Then(geom.Translate(t.PinX, t.PinY))
}

// Frame is a parent space placed in the page: the map from it to page
// space (inches, y up), and the parity of the flips on the way.
type Frame struct {
	M            geom.Affine
	FlipX, FlipY bool
}

// Page is the page's own space.
var Page = Frame{M: geom.Identity}

// Child returns the frame of the children of group g placed in f.
// Chunk P1.
func (f Frame) Child(g Transform) Frame {
	return Frame{
		M:     g.Local().Then(f.M),
		FlipX: f.FlipX != g.FlipX,
		FlipY: f.FlipY != g.FlipY,
	}
}

// View is the frame of the view: page inches, y up, to points, y down,
// for a page pageHeight inches high: (x, y) -> (72x, 72(pageHeight-y)).
// Chunk P1.
func View(pageHeight float64) geom.Affine {
	return geom.Scale(72, -72).Then(geom.Translate(0, 72*pageHeight))
}

// Box returns the view box of a shape with transform t in frame f on a
// page pageHeight inches high: lengths in points, rotation in degrees
// clockwise, the flips the parity of the shape's and its groups'.
// It fails with geom.ErrSkew past SkewTolerance. Chunk P1.
func Box(f Frame, t Transform, pageHeight float64) (geom.Box, error) {
	// geom.Decompose reads a box in view-oriented local coordinates
	// (y down), while a shape's local space is y up: its corner (x, y)
	// is the shape's (x, Height-y). Compose that turn with the shape's
	// own map to the view so the decomposition's flips are the stored
	// flips' parity and its rotation is degrees clockwise.
	turn := geom.Scale(1, -1).Then(geom.Translate(0, t.Height))
	m := turn.Then(t.Local()).Then(f.M).Then(View(pageHeight))
	box, _, err := geom.Decompose(m, t.Width, t.Height, t.FlipY != f.FlipY, SkewTolerance)
	return box, err
}

// Point returns where a point of f's space (inches, y up; a 1-D shape's
// BeginX, BeginY are in its parent's space) is in the view (points, y
// down). Chunk P1.
func Point(f Frame, x, y, pageHeight float64) geom.Pt {
	return f.M.Then(View(pageHeight)).Apply(geom.Pt{X: x, Y: y})
}

// Site returns where a point of the shape's local space (a Connection
// row's X, Y) is in the view. Chunk P1.
func Site(f Frame, t Transform, x, y, pageHeight float64) geom.Pt {
	return t.Local().Then(f.M).Then(View(pageHeight)).Apply(geom.Pt{X: x, Y: y})
}
