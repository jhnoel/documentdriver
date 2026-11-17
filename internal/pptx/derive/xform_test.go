package derive

import (
	"errors"
	"math"
	"testing"
)

const pt = 12700.0

func near(a, b float64) bool {
	return math.Abs(a-b) < 1e-6*math.Max(1, math.Max(math.Abs(a), math.Abs(b)))
}

func ptNear(p Pt, x, y float64) bool { return near(p.X, x*pt) && near(p.Y, y*pt) }

func xf(x, y, w, h float64) Xfrm { return Xfrm{X: x * pt, Y: y * pt, W: w * pt, H: h * pt} }

func boxEq(t *testing.T, name string, got Xfrm, x, y, w, h, rot float64, fh, fv bool) {
	t.Helper()
	if !near(got.X, x*pt) || !near(got.Y, y*pt) || !near(got.W, w*pt) || !near(got.H, h*pt) || !near(got.Rot, rot) || got.FlipH != fh || got.FlipV != fv {
		t.Errorf("%s: got box (%g %g %g %g) rot %g flips %v/%v, want (%g %g %g %g) rot %g flips %v/%v",
			name, got.X/pt, got.Y/pt, got.W/pt, got.H/pt, got.Rot, got.FlipH, got.FlipV, x, y, w, h, rot, fh, fv)
	}
}

func TestPlaceTopLevel(t *testing.T) {
	x := xf(100, 50, 40, 20)
	x.Rot, x.FlipH = 30, true
	p, err := Place(x, Root)
	if err != nil {
		t.Fatal(err)
	}
	boxEq(t, "top level", p.Box, 100, 50, 40, 20, 30, true, false)
	// The local origin of a flipped shape maps to its right edge before
	// rotation: centre (120, 60) plus R(30) * (20, -10).
	s, c := math.Sincos(30 * math.Pi / 180)
	want := Pt{X: 120 + 20*c + 10*s, Y: 60 + 20*s - 10*c}
	if got := p.M.Apply(Pt{}); !ptNear(got, want.X, want.Y) {
		t.Errorf("origin maps to %v, want %v pt", Pt{X: got.X / pt, Y: got.Y / pt}, want)
	}
}

func TestPlaceScaledGroup(t *testing.T) {
	// Child space 0..400 x 0..200 shown at 500,300 at half size.
	g := GroupXfrm{Xfrm: xf(500, 300, 200, 100), ChW: 400 * pt, ChH: 200 * pt}
	cp, err := Root.Child(g)
	if err != nil {
		t.Fatal(err)
	}
	p, err := Place(xf(100, 50, 100, 100), cp)
	if err != nil {
		t.Fatal(err)
	}
	boxEq(t, "scaled", p.Box, 550, 325, 50, 50, 0, false, false)
}

func TestPlaceFlippedGroup(t *testing.T) {
	g := GroupXfrm{Xfrm: xf(500, 300, 200, 100), ChW: 400 * pt, ChH: 200 * pt}
	g.FlipH = true
	cp, err := Root.Child(g)
	if err != nil {
		t.Fatal(err)
	}
	// The child at the left of child space lands at the right, flipped.
	p, err := Place(xf(0, 0, 100, 100), cp)
	if err != nil {
		t.Fatal(err)
	}
	boxEq(t, "flipped group", p.Box, 650, 300, 50, 50, 0, true, false)
	// A child flipped itself is unflipped on the slide.
	c := xf(0, 0, 100, 100)
	c.FlipH = true
	p, _ = Place(c, cp)
	boxEq(t, "double flip", p.Box, 650, 300, 50, 50, 0, false, false)
}

func TestPlaceRotatedGroup(t *testing.T) {
	// Group at 100,300 100x50 rotated 90 with identity child space.
	g := GroupXfrm{Xfrm: xf(100, 300, 100, 50), ChX: 100 * pt, ChY: 300 * pt, ChW: 100 * pt, ChH: 50 * pt}
	g.Rot = 90
	cp, _ := Root.Child(g)
	p, err := Place(xf(100, 300, 50, 50), cp)
	if err != nil {
		t.Fatal(err)
	}
	boxEq(t, "rotated group", p.Box, 125, 275, 50, 50, 90, false, false)
	// Rotations add through groups.
	c := xf(100, 300, 50, 50)
	c.Rot = 45
	p, _ = Place(c, cp)
	if !near(p.Box.Rot, 135) {
		t.Errorf("rotation 45 under 90 = %g, want 135", p.Box.Rot)
	}
}

func TestPlaceNested(t *testing.T) {
	// Depth 3: each level doubles the scale and shifts.
	parent := Root
	for i := 0; i < 3; i++ {
		g := GroupXfrm{Xfrm: xf(10, 10, 200, 200), ChW: 100 * pt, ChH: 100 * pt}
		var err error
		if parent, err = parent.Child(g); err != nil {
			t.Fatal(err)
		}
	}
	p, err := Place(xf(0, 0, 10, 10), parent)
	if err != nil {
		t.Fatal(err)
	}
	// Each level maps x -> 10 + 2x, so 0 -> 10 -> 30 -> 70; size 10 -> 80.
	boxEq(t, "nested", p.Box, 70, 70, 80, 80, 0, false, false)
}

func TestPlaceSkew(t *testing.T) {
	g := GroupXfrm{Xfrm: xf(0, 0, 200, 100), ChW: 100 * pt, ChH: 100 * pt}
	cp, _ := Root.Child(g)
	c := xf(10, 10, 50, 50)
	c.Rot = 30
	if _, err := Place(c, cp); !errors.Is(err, ErrSkew) {
		t.Errorf("rotated child under non-uniform scale: err %v, want ErrSkew", err)
	}
	// A right-angle rotation stays a rectangle with exchanged scales.
	c.Rot = 90
	p, err := Place(c, cp)
	if err != nil {
		t.Fatal(err)
	}
	// Centre (35,35) -> (70,35); local x axis (width 50) runs down at
	// scale 1, local y axis (height 50) runs left at scale 2.
	boxEq(t, "right angle", p.Box, 45, -15, 50, 100, 90, false, false)
	// A tiny skew (whole-EMU rounding) is accepted and reported.
	g2 := GroupXfrm{Xfrm: Xfrm{W: 1000001, H: 1000000}, ChW: 1000000, ChH: 1000000}
	cp2, _ := Root.Child(g2)
	c2 := Xfrm{W: 500000, H: 500000, Rot: 30}
	p2, err := Place(c2, cp2)
	if err != nil {
		t.Fatalf("tiny skew: %v", err)
	}
	if p2.Skew <= 0 || p2.Skew > 1e-3 {
		t.Errorf("tiny skew = %g degrees", p2.Skew)
	}
}

func TestDegenerateGroup(t *testing.T) {
	g := GroupXfrm{Xfrm: xf(0, 0, 10, 10)}
	if _, err := Root.Child(g); !errors.Is(err, ErrDegenerateGroup) {
		t.Errorf("err %v, want ErrDegenerateGroup", err)
	}
	// The slide's own spTree frame is all zeros: scale 1.
	if _, err := Root.Child(GroupXfrm{}); err != nil {
		t.Errorf("empty frame: %v", err)
	}
}

func TestUnplaceRoundTrip(t *testing.T) {
	groups := []GroupXfrm{
		{Xfrm: xf(500, 300, 200, 100), ChW: 400 * pt, ChH: 200 * pt},
		{Xfrm: Xfrm{X: 100 * pt, Y: 300 * pt, W: 100 * pt, H: 50 * pt, Rot: 90, FlipH: true}, ChX: 7 * pt, ChY: 3 * pt, ChW: 100 * pt, ChH: 50 * pt},
		{Xfrm: Xfrm{X: 10 * pt, Y: 20 * pt, W: 300 * pt, H: 300 * pt, Rot: 33, FlipV: true}, ChW: 150 * pt, ChH: 150 * pt},
	}
	children := []Xfrm{
		xf(10, 20, 30, 40),
		{X: 5 * pt, Y: 6 * pt, W: 70 * pt, H: 20 * pt, Rot: 17, FlipH: true},
		{X: 50 * pt, Y: 60 * pt, W: 10 * pt, H: 90 * pt, Rot: 270, FlipV: true},
	}
	for gi, g := range groups {
		cp, err := Root.Child(g)
		if err != nil {
			t.Fatal(err)
		}
		for ci, c := range children {
			p, err := Place(c, cp)
			if errors.Is(err, ErrSkew) {
				continue // group 0 scales non-uniformly
			}
			if err != nil {
				t.Fatal(err)
			}
			back, err := Unplace(p.Box, cp)
			if err != nil {
				t.Fatalf("group %d child %d: %v", gi, ci, err)
			}
			// The same slide-space result is what matters; the stored
			// encoding may trade a flip pair for a half turn.
			again, _ := Place(back, cp)
			for _, q := range []Pt{{X: 0, Y: 0}, {X: back.W, Y: 0}, {X: 0, Y: back.H}} {
				a := again.M.Apply(q)
				b := p.M.Apply(Pt{X: q.X * c.W / back.W, Y: q.Y * c.H / back.H})
				if math.Hypot(a.X-b.X, a.Y-b.Y) > 1e-3 {
					t.Errorf("group %d child %d: corner %v maps to %v, want %v", gi, ci, q, a, b)
				}
			}
		}
	}
}

func TestUnplaceInexpressible(t *testing.T) {
	g := GroupXfrm{Xfrm: xf(0, 0, 200, 100), ChW: 100 * pt, ChH: 100 * pt}
	cp, _ := Root.Child(g)
	want := xf(10, 10, 50, 50)
	want.Rot = 30
	if _, err := Unplace(want, cp); !errors.Is(err, ErrInexpressible) {
		t.Errorf("rotated box under non-uniform scale: err %v, want ErrInexpressible", err)
	}
	// Unrotated boxes are fine under any scale.
	want.Rot = 0
	got, err := Unplace(want, cp)
	if err != nil {
		t.Fatal(err)
	}
	boxEq(t, "unplaced", got, 5, 10, 25, 50, 0, false, false)
}
