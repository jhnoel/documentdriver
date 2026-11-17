package place

import (
	"math"

	"github.com/jhnoel/documentdriver/internal/geom"
)

// Unplace inverts Box: given the view box want (points, y down, as Box
// reports it) of a shape in frame f on a page pageHeight inches high,
// and the shape's LocPin as fractions of its size (locX = LocPinX/Width;
// 0.5 for a new shape), it returns the transform to store (inches,
// radians, flips relative to the frame's). It fails with
// geom.ErrSkew when the frame cannot hold the box without skew. Chunk K1.
//
// The box's own map (want.Matrix, box-local points to view points) is
// carried back through the view and the frame to frame inches, and
// decomposed there: the decomposed box is the shape in its parent
// space. That decomposition's vertical flip is the complement of the
// stored FlipY, because a box's local y runs down the view while a
// shape's local y runs up (see Box), and its horizontal flip, rotation
// (in degrees in the frame's y-up space, normalised by Decompose to
// [0, 360)) and size are the stored ones; the stored angle is that
// rotation in radians, wrapped to (-pi, pi] as Visio's ATAN2 yields
// it. The pin is where the LocPin point (locX, 1-locY of the box, the
// y turned round for the same reason) lands in the frame. Skew is
// measured in frame inches converted to points along the frame's x
// axis, and then again where the box is shown, in view points, as
// derive.Unplace does in pptx: the frame's scale distorts the first
// measure.
func Unplace(f Frame, want geom.Box, pageHeight, locX, locY float64) (Transform, error) {
	if math.IsNaN(want.X) || math.IsNaN(want.Y) || math.IsNaN(want.W) || math.IsNaN(want.H) ||
		math.IsNaN(want.Rot) || math.IsNaN(pageHeight) || math.IsNaN(locX) || math.IsNaN(locY) ||
		math.IsInf(want.X, 0) || math.IsInf(want.Y, 0) || math.IsInf(want.W, 0) || math.IsInf(want.H, 0) ||
		math.IsInf(want.Rot, 0) || math.IsInf(pageHeight, 0) || math.IsInf(locX, 0) || math.IsInf(locY, 0) ||
		want.W < 0 || want.H < 0 {
		return Transform{}, geom.ErrSkew
	}
	view := View(pageHeight)
	viewInv, ok := view.Invert()
	if !ok {
		return Transform{}, geom.ErrSkew
	}
	frameInv, ok := f.M.Invert()
	if !ok {
		return Transform{}, geom.ErrSkew
	}
	m := want.Matrix().Then(viewInv).Then(frameInv)
	xAxis := f.M.Then(view).Vec(geom.Pt{X: 1})
	scale := math.Hypot(xAxis.X, xAxis.Y)
	if scale == 0 {
		return Transform{}, geom.ErrSkew
	}
	inFrame, _, err := geom.Decompose(m, want.W, want.H, want.FlipV == f.FlipY, SkewTolerance/scale)
	if err != nil {
		return Transform{}, err
	}
	pin := m.Apply(geom.Pt{X: locX * want.W, Y: (1 - locY) * want.H})
	angle := inFrame.Rot * math.Pi / 180
	if angle > math.Pi {
		angle -= 2 * math.Pi
	}
	t := Transform{
		PinX: pin.X, PinY: pin.Y,
		Width: inFrame.W, Height: inFrame.H,
		LocPinX: locX * inFrame.W, LocPinY: locY * inFrame.H,
		Angle: angle,
		FlipX: inFrame.FlipH, FlipY: !inFrame.FlipV,
	}
	if t.Width != 0 && t.Height != 0 {
		shown := inFrame.Matrix().Then(f.M).Then(view)
		if _, _, err := geom.Decompose(shown, t.Width, t.Height, want.FlipV, SkewTolerance); err != nil {
			return Transform{}, err
		}
	}
	return t, nil
}
