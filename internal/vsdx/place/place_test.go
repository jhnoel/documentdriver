package place

import (
	"errors"
	"math"
	"math/rand/v2"
	"testing"

	"github.com/jhnoel/documentdriver/internal/geom"
)

// boxCase is one row of the P1 table: a shape transform and frame, and
// the view box wanted (points, y down, page height 11 inches).
type boxCase struct {
	name         string
	frame        Frame
	t            Transform
	x, y, w, h   float64
	rot          float64
	flipH, flipV bool
}

// TestPlaceRows checks the P1 table rows (inches/radians in, points out).
func TestPlaceRows(t *testing.T) {
	row1 := Transform{PinX: 2, PinY: 9, Width: 1, Height: 0.5, LocPinX: 0.5, LocPinY: 0.25}
	group := Transform{PinX: 4, PinY: 5, Width: 2, Height: 2, LocPinX: 1, LocPinY: 1}
	child := Transform{PinX: 0.5, PinY: 0.5, Width: 1, Height: 1, LocPinX: 0.5, LocPinY: 0.5}
	groupFlipY := Transform{PinX: 6, PinY: 5, Width: 1, Height: 1, LocPinX: 0.5, LocPinY: 0.5, FlipY: true}
	childFlip := Transform{PinX: 0.5, PinY: 0.25, Width: 0.5, Height: 0.5, LocPinX: 0.25, LocPinY: 0.25}
	s10 := Transform{PinX: 4, PinY: 5, Width: 2, Height: 2, LocPinX: 1, LocPinY: 1}
	s12 := Transform{PinX: 1.5, PinY: 1.5, Width: 1, Height: 1, LocPinX: 0.5, LocPinY: 0.5, Angle: 0.5235987755987756}
	s13 := Transform{PinX: 0.5, PinY: 0.5, Width: 0.5, Height: 0.5, LocPinX: 0.25, LocPinY: 0.25}
	s12Frame := Page.Child(s10).Child(s12)

	cases := []boxCase{
		{name: "row 1", frame: Page, t: row1, x: 108, y: 126, w: 72, h: 36},
		{name: "row 2", frame: Page, t: Transform{PinX: 2, PinY: 9, Width: 1, Height: 0.5, LocPinX: 0.5, LocPinY: 0.25, Angle: math.Pi / 2}, x: 108, y: 126, w: 72, h: 36, rot: 270},
		{name: "row 3", frame: Page, t: Transform{PinX: 2, PinY: 9, Width: 1, Height: 0.5, LocPinX: 0.5, LocPinY: 0.25, FlipX: true}, x: 108, y: 126, w: 72, h: 36, flipH: true},
		{name: "row 4", frame: Page, t: Transform{PinX: 2, PinY: 9, Width: 1, Height: 0.5, Angle: math.Pi / 2}, x: 90, y: 90, w: 72, h: 36, rot: 270},
		{name: "row 5", frame: Page.Child(group), t: child, x: 216, y: 432, w: 72, h: 72},
		{name: "row 8", frame: Page.Child(groupFlipY), t: childFlip, x: 414, y: 396, w: 36, h: 36, flipV: true},
		{name: "row 13", frame: s12Frame, t: s13, x: 306, y: 378, w: 36, h: 36, rot: 330},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := Box(tc.frame, tc.t, 11)
			if err != nil {
				t.Fatalf("Box: %v", err)
			}
			tol := 1e-6
			if tc.name == "row 13" {
				tol = 1e-6
			}
			for _, f := range []struct {
				name string
				got  float64
				want float64
			}{
				{"X", got.X, tc.x}, {"Y", got.Y, tc.y}, {"W", got.W, tc.w}, {"H", got.H, tc.h},
			} {
				if math.Abs(f.got-f.want) > tol {
					t.Errorf("%s = %v, want %v", f.name, f.got, f.want)
				}
			}
			rotTol := 1e-6
			if tc.name == "row 9" {
				rotTol = 1e-9
			}
			if math.Abs(got.Rot-tc.rot) > rotTol {
				t.Errorf("Rot = %v, want %v", got.Rot, tc.rot)
			}
			if got.FlipH != tc.flipH || got.FlipV != tc.flipV {
				t.Errorf("flips = (%v, %v), want (%v, %v)", got.FlipH, got.FlipV, tc.flipH, tc.flipV)
			}
		})
	}
}

// TestPlaceRow9 checks row 9: a negative angle's rotation, within 1e-9.
func TestPlaceRow9(t *testing.T) {
	got, err := Box(Page, Transform{PinX: 2, PinY: 9, Width: 1, Height: 0.5, LocPinX: 0.5, LocPinY: 0.25, Angle: -0.5235987755982988}, 11)
	if err != nil {
		t.Fatalf("Box: %v", err)
	}
	if math.Abs(got.Rot-30) > 1e-9 {
		t.Errorf("Rot = %v, want 30 within 1e-9", got.Rot)
	}
}

// TestPlacePoints checks rows 6, 7 and 10.
func TestPlacePoints(t *testing.T) {
	if got, want := Point(Page, 1, 10, 11), (geom.Pt{X: 72, Y: 72}); got != want {
		t.Errorf("row 6: Point = %v, want %v", got, want)
	}
	row1 := Transform{PinX: 2, PinY: 9, Width: 1, Height: 0.5, LocPinX: 0.5, LocPinY: 0.25}
	if got, want := Site(Page, row1, 0.5, 0.5, 11), (geom.Pt{X: 144, Y: 126}); got != want {
		t.Errorf("row 7: Site = %v, want %v", got, want)
	}
	group := Transform{PinX: 4, PinY: 5, Width: 2, Height: 2, LocPinX: 1, LocPinY: 1}
	if got, want := Point(Page.Child(group), 0, 0, 11), (geom.Pt{X: 216, Y: 504}); got != want {
		t.Errorf("row 10: Point = %v, want %v", got, want)
	}
}

// TestPlaceRow12Skew checks row 12: a sheared frame under a rotated
// child skews by more than 0.5 pt and fails with geom.ErrSkew.
func TestPlaceRow12Skew(t *testing.T) {
	f := Frame{M: geom.Affine{A: 1, C: 0.3, D: 1}}
	child := Transform{PinX: 1, PinY: 1, Width: 2, Height: 2, LocPinX: 1, LocPinY: 1, Angle: math.Pi / 4}
	if _, err := Box(f, child, 11); !errors.Is(err, geom.ErrSkew) {
		t.Errorf("Box: err = %v, want geom.ErrSkew", err)
	}
}

// randomTransform draws one random transform from rng.
func randomTransform(rng *rand.Rand) Transform {
	w := 0.1 + 2.9*rng.Float64()
	h := 0.1 + 2.9*rng.Float64()
	return Transform{
		PinX: -5 + 20*rng.Float64(), PinY: -5 + 20*rng.Float64(),
		Width: w, Height: h,
		LocPinX: rng.Float64() * w, LocPinY: rng.Float64() * h,
		Angle: -math.Pi + 2*math.Pi*rng.Float64(),
		FlipX: rng.Float64() < 0.5, FlipY: rng.Float64() < 0.5,
	}
}

// naivePoint maps a local point through the chain of transforms with the
// stored formula Pin + R(Angle) * diag(fx, fy) * (p - LocPin) at each
// level, then to the view, without using place or geom.
func naivePoint(chain []Transform, x, y, pageHeight float64) geom.Pt {
	// chain[0] is the outermost group and the last entry the shape, so
	// the shape's transform applies first.
	for i := len(chain) - 1; i >= 0; i-- {
		t := chain[i]
		fx, fy := 1.0, 1.0
		if t.FlipX {
			fx = -1
		}
		if t.FlipY {
			fy = -1
		}
		dx, dy := (x-t.LocPinX)*fx, (y-t.LocPinY)*fy
		s, c := math.Sin(t.Angle), math.Cos(t.Angle)
		x, y = t.PinX+c*dx-s*dy, t.PinY+s*dx+c*dy
	}
	return geom.Pt{X: 72 * x, Y: 72 * (pageHeight - y)}
}

// TestPlaceRow11Property checks row 11: over 5,000 random transforms
// nested 1 to 4 deep, Box.Matrix corners equal the naive per-corner
// mapping within 1e-6 pt when unskewed.
func TestPlaceRow11Property(t *testing.T) {
	rng := rand.New(rand.NewPCG(1, 2))
	for i := 0; i < 5000; i++ {
		depth := 1 + rng.IntN(4)
		chain := make([]Transform, depth)
		for j := range chain {
			chain[j] = randomTransform(rng)
		}
		f := Page
		for _, g := range chain[:depth-1] {
			f = f.Child(g)
		}
		shape := chain[depth-1]
		box, err := Box(f, shape, 11)
		if err != nil {
			t.Fatalf("case %d chain %v: Box: %v", i, chain, err)
		}
		// Box.Matrix reads view-oriented local coordinates in points,
		// whose corner (x, y) is the shape's (x/72, Height-y/72).
		for _, q := range []geom.Pt{{X: 0, Y: 0}, {X: 72 * shape.Width, Y: 0}, {X: 0, Y: 72 * shape.Height}, {X: 72 * shape.Width, Y: 72 * shape.Height}} {
			got := box.Matrix().Apply(q)
			want := naivePoint(chain, q.X/72, shape.Height-q.Y/72, 11)
			if math.Abs(got.X-want.X) > 1e-6 || math.Abs(got.Y-want.Y) > 1e-6 {
				t.Fatalf("case %d chain %v corner %v: Box.Matrix = %v, naive = %v", i, chain, q, got, want)
			}
		}
	}
}
