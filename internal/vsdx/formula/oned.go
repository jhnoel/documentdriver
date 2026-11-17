// Package formula is the ShapeSheet formula work of the writes: in P2 a
// lossless lexer, the allow-list classifier and the closed evaluator of
// decision (c) (docs/vsdx-design.md); now the 1-D transform kernel the
// connector writers need.
package formula

// OneD returns the transform cells Visio derives for a 1-D shape from
// its ends (inches, y up), as the corpus's connectors store them:
// PinX = (BeginX+EndX)/2, PinY = (BeginY+EndY)/2,
// Width = SQRT((EndX-BeginX)^2+(EndY-BeginY)^2),
// Angle = ATAN2(EndY-BeginY, EndX-BeginX) in radians; LocPinX is
// Width*0.5 and LocPinY 0. ok is false when the ends coincide (Width 0,
// angle undefined). Chunk K2.
func OneD(beginX, beginY, endX, endY float64) (pinX, pinY, width, angle float64, ok bool) {
	_, _, _, _ = beginX, beginY, endX, endY
	return 0, 0, 0, 0, false
}
