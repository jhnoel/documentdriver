// Package geom is the plane geometry the formats share: points, affine
// maps in the SVG matrix convention, and the box, rotation and flips a
// view shows for a placed object. It knows no format: pptx stores EMU and
// DrawingML transforms, vsdx stores inches and ShapeSheet pins, and both
// build an Affine from what they store and show a Box decomposed from it.
// The y axis points down, as on screen.
package geom

import (
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
)

// Pt is a point. The unit is the caller's.
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

// Translate returns a translation affine map.
func Translate(x, y float64) Affine { return Affine{A: 1, D: 1, E: x, F: y} }

// Scale returns a scaling affine map.
func Scale(x, y float64) Affine { return Affine{A: x, D: y} }

// Rotate returns a rotation affine map in degrees, clockwise on screen.
func Rotate(deg float64) Affine {
	s, c := SinCosDeg(deg)
	return Affine{A: c, B: s, C: -s, D: c}
}

// SinCosDeg returns the sine and cosine of deg degrees, exact at
// multiples of 90 degrees, so that right-angle rotations introduce no
// rounding.
func SinCosDeg(deg float64) (s, c float64) {
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

// NormDeg normalises an angle to [0, 360), snapping values within 1e-9 of
// a whole degree count so right angles stay exact.
func NormDeg(d float64) float64 {
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

// FormatNum formats a number with at most two decimals, trimming zeros.
// It is the one formatting rule of the views' lengths and of the codecs
// that write them, so a value shown can be compared with a value written
// in the same form.
func FormatNum(f float64) string {
	s := strconv.FormatFloat(f, 'f', 2, 64)
	s = strings.TrimRight(strings.TrimRight(s, "0"), ".")
	if s == "-0" {
		s = "0"
	}
	return s
}

// Box is an object's frame as the views show it: the unrotated box (left,
// top, width, height), the rotation in degrees clockwise about the box
// centre, and the flips, applied first, about that centre.
type Box struct {
	X, Y, W, H   float64
	Rot          float64
	FlipH, FlipV bool
}

// Matrix maps the box's local coordinates (0..W, 0..H, before flips and
// rotation) to the space the box is in. Flips and rotation are about the
// box centre: flips first, then rotation.
func (x Box) Matrix() Affine {
	fx, fy := 1.0, 1.0
	if x.FlipH {
		fx = -1
	}
	if x.FlipV {
		fy = -1
	}
	cx, cy := x.X+x.W/2, x.Y+x.H/2
	return Translate(-x.W/2, -x.H/2).Then(Scale(fx, fy)).Then(Rotate(x.Rot)).Then(Translate(cx, cy))
}

// ErrSkew is returned when an object's image under its groups is not a
// rectangle (a rotated child under a non-uniformly scaled group), so it
// has no faithful box, rotation and flips.
var ErrSkew = errors.New("transform is not expressible as box, rotation and flips (skew)")

// Decompose writes the map m of a w x h local box as a box, rotation and
// flips in m's target space. flipV is the wanted vertical flip; flipH
// follows from the sign of the determinant (an odd number of reflections
// means exactly one flip). With both fixed the rotation is unique. It
// also returns how far, in degrees, the mapped axes are from
// perpendicular; when the far corner of the box moves by more than tol
// (in the target space's unit) because of that, it fails with ErrSkew.
func Decompose(m Affine, w, h float64, flipV bool, tol float64) (Box, float64, error) {
	c := m.Apply(Pt{w / 2, h / 2})
	// Unit axes of the local frame mapped through m.
	ax := m.Vec(Pt{1, 0})
	ay := m.Vec(Pt{0, 1})
	sxl, syl := math.Hypot(ax.X, ax.Y), math.Hypot(ay.X, ay.Y)
	if sxl == 0 || syl == 0 {
		return Box{}, 0, ErrSkew
	}
	dot := (ax.X*ay.X + ax.Y*ay.Y) / (sxl * syl)
	dev := 0.0
	if math.Abs(dot) > 1e-12 {
		dev = math.Abs(90 - math.Acos(math.Max(-1, math.Min(1, dot)))*180/math.Pi)
		// Keeping the x axis, the far corner moves by H sin(dev).
		if h*syl*math.Abs(dot) > tol {
			return Box{}, dev, fmt.Errorf("%w: axes %s degrees from perpendicular", ErrSkew, strconv.FormatFloat(dev, 'g', 3, 64))
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
	rot := NormDeg(math.Atan2(ax.Y*fhs, ax.X*fhs) * 180 / math.Pi)
	W, H := w*sxl, h*syl
	return Box{X: c.X - W/2, Y: c.Y - H/2, W: W, H: H, Rot: rot, FlipH: fh, FlipV: flipV}, dev, nil
}
