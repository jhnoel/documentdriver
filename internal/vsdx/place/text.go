package place

import (
	"errors"
	"math"

	"github.com/jhnoel/documentdriver/internal/geom"
)

// pointsPerInch converts the shape sheet's inches to view points.
const pointsPerInch = 72

// TextCells are a shape's text transform cells in its local inches
// (TxtWidth, TxtHeight, TxtPinX, TxtPinY, TxtLocPinX, TxtLocPinY) and
// TxtAngle in radians.
type TextCells struct {
	W, H, PinX, PinY, LocPinX, LocPinY, Angle float64
}

// DefaultText returns the text cells Visio's default formulas give a
// shape of transform t: the block is the shape's box, pinned at its
// centre, at no angle.
func DefaultText(t Transform) TextCells {
	return TextCells{W: t.Width, H: t.Height, PinX: t.Width / 2, PinY: t.Height / 2, LocPinX: t.Width / 2, LocPinY: t.Height / 2}
}

// TextFrame returns the map from a text block's own space (points, y
// down, origin at its top-left corner) to the view, and the block's
// width and height in points, for cells c of a shape of transform t in
// frame f on a page pageHeight inches high. A flipped shape does not
// mirror its text, so a frame that would is reflected about the
// block's vertical centre line.
func TextFrame(c TextCells, t Transform, f Frame, pageHeight float64) (geom.Affine, float64, float64) {
	w, h := c.W*pointsPerInch, c.H*pointsPerInch
	toBlock := geom.Scale(1.0/pointsPerInch, -1.0/pointsPerInch).Then(geom.Translate(0, c.H))
	local := geom.Translate(-c.LocPinX, -c.LocPinY).Then(geom.Rotate(c.Angle * 180 / math.Pi)).Then(geom.Translate(c.PinX, c.PinY))
	frame := toBlock.Then(local).Then(t.Local()).Then(f.M).Then(View(pageHeight))
	if frame.A*frame.D-frame.B*frame.C < 0 {
		frame = geom.Scale(-1, 1).Then(geom.Translate(w, 0)).Then(frame)
	}
	return frame, w, h
}

// TextBox returns the text block's place on the page as read shows it
// (TextFrame): its rectangle before rotation, centred where the
// block's centre lands, its size through the frame, and Rot the angle
// of its baseline on the page, as an object's rot is measured.
func TextBox(c TextCells, t Transform, f Frame, pageHeight float64) geom.Box {
	frame, w, h := TextFrame(c, t, f, pageHeight)
	ctr := frame.Apply(geom.Pt{X: w / 2, Y: h / 2})
	pw := math.Hypot(frame.A, frame.B) * w
	ph := math.Hypot(frame.C, frame.D) * h
	return geom.Box{X: ctr.X - pw/2, Y: ctr.Y - ph/2, W: pw, H: ph, Rot: geom.NormDeg(math.Atan2(frame.B, frame.A) * 180 / math.Pi)}
}

// ErrTextBox reports a text box that cannot be placed: a shape whose
// transform has no inverse, or a size that is not positive and finite.
var ErrTextBox = errors.New("place: the text box cannot be placed on this shape")

// TextCellsFor returns the cells that put a shape's text block at box
// b on the page (TextBox's form: the rectangle before rotation, Rot
// the baseline's angle in degrees), pinned at its centre: the inverse
// of TextBox, so TextBox(TextCellsFor(b)) is b.
func TextCellsFor(b geom.Box, t Transform, f Frame, pageHeight float64) (TextCells, error) {
	m := t.Local().Then(f.M).Then(View(pageHeight))
	det := m.A*m.D - m.B*m.C
	if !(math.Abs(det) > 1e-12) || !(b.W > 0) || !(b.H > 0) || math.IsInf(b.W, 0) || math.IsInf(b.H, 0) {
		return TextCells{}, ErrTextBox
	}
	inv, ok := m.Invert()
	if !ok {
		return TextCells{}, ErrTextBox
	}
	pin := inv.Apply(geom.Pt{X: b.X + b.W/2, Y: b.Y + b.H/2})
	// The baseline's direction on the page; a block the frame would
	// mirror is reflected (TextFrame), so its x axis runs the other
	// way through m.
	rad := b.Rot * math.Pi / 180
	d := geom.Pt{X: math.Cos(rad), Y: math.Sin(rad)}
	if det > 0 {
		d = geom.Pt{X: -d.X, Y: -d.Y}
	}
	v := inv.Vec(d)
	angle := math.Atan2(v.Y, v.X)
	if math.Abs(angle) < 1e-12 {
		angle = 0
	}
	// Lengths: the page size of one local inch along the block's axes.
	ax := m.Vec(geom.Pt{X: math.Cos(angle), Y: math.Sin(angle)})
	ay := m.Vec(geom.Pt{X: -math.Sin(angle), Y: math.Cos(angle)})
	sx, sy := math.Hypot(ax.X, ax.Y), math.Hypot(ay.X, ay.Y)
	w, h := b.W/sx, b.H/sy
	return TextCells{W: w, H: h, PinX: pin.X, PinY: pin.Y, LocPinX: w / 2, LocPinY: h / 2, Angle: angle}, nil
}
