// Package formula is the ShapeSheet formula work of the writes: in P2 a
// lossless lexer, the allow-list classifier and the closed evaluator of
// decision (c) (docs/vsdx-design.md); now the 1-D transform kernel the
// connector writers need.
package formula

import "math"

// OneD returns the transform cells Visio derives for a 1-D shape from
// its ends (inches, y up), as the corpus's connectors store them:
// PinX = (BeginX+EndX)/2, PinY = (BeginY+EndY)/2,
// Width = SQRT((EndX-BeginX)^2+(EndY-BeginY)^2),
// Angle = ATAN2(EndY-BeginY, EndX-BeginX) in radians; LocPinX is
// Width*0.5 and LocPinY 0. ok is false when the ends coincide (Width 0,
// angle undefined), when any end is NaN or infinite, or when a derived
// cell is not finite: a pin or the width overflowing, or the width the
// stored SQRT(dx^2+dy^2) formula evaluates overflowing though the true
// length (math.Hypot, computed without that intermediate overflow) is
// finite, as at ends (0,0) and (1e308,1e308). Chunk K2, revised K2F.
func OneD(beginX, beginY, endX, endY float64) (pinX, pinY, width, angle float64, ok bool) {
	if math.IsNaN(beginX) || math.IsNaN(beginY) || math.IsNaN(endX) || math.IsNaN(endY) ||
		math.IsInf(beginX, 0) || math.IsInf(beginY, 0) || math.IsInf(endX, 0) || math.IsInf(endY, 0) {
		return 0, 0, 0, 0, false
	}
	dx := endX - beginX
	dy := endY - beginY
	pinX = (beginX + endX) / 2
	pinY = (beginY + endY) / 2
	width = math.Hypot(dx, dy)
	if width == 0 {
		return pinX, pinY, 0, 0, false
	}
	angle = math.Atan2(dy, dx)
	if math.IsNaN(pinX) || math.IsNaN(pinY) || math.IsNaN(width) || math.IsNaN(angle) ||
		math.IsInf(pinX, 0) || math.IsInf(pinY, 0) || math.IsInf(width, 0) || math.IsInf(angle, 0) ||
		math.IsInf(dx*dx+dy*dy, 0) {
		return 0, 0, 0, 0, false
	}
	return pinX, pinY, width, angle, true
}
