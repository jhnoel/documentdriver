package place

import (
	"github.com/jhnoel/documentdriver/internal/geom"
	"github.com/jhnoel/documentdriver/internal/vsdx/vsd"
)

// Unplace inverts Box: given the view box want (points, y down, as Box
// reports it) of a shape in frame f on a page pageHeight inches high,
// and the shape's LocPin as fractions of its size (locX = LocPinX/Width;
// 0.5 for a new shape), it returns the transform to store (inches,
// radians, flips relative to the frame's). It fails with
// geom.ErrSkew when the frame cannot hold the box without skew. Chunk K1.
func Unplace(f Frame, want geom.Box, pageHeight, locX, locY float64) (Transform, error) {
	_, _, _, _, _ = f, want, pageHeight, locX, locY
	return Transform{}, vsd.ErrNotImplemented
}
