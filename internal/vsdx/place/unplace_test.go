package place

import (
	"errors"
	"math"
	"math/rand/v2"
	"testing"

	"github.com/jhnoel/documentdriver/internal/geom"
)

// TestUnplace checks Unplace against the chunk K1 rows verbatim: the
// first two entries are the K1 spec rows, the rest are the inverses of
// the P1 rows 1, 2, 4, 5 (child frame) and 8 (page height 11 in,
// boxes in view points y down).
func TestUnplace(t *testing.T) {
	group := Transform{PinX: 4, PinY: 5, Width: 2, Height: 2, LocPinX: 1, LocPinY: 1}
	groupFlipY := Transform{PinX: 6, PinY: 5, Width: 1, Height: 1, LocPinX: 0.5, LocPinY: 0.5, FlipY: true}
	rows := []struct {
		name       string
		frame      Frame
		want       geom.Box
		locX, locY float64
		got        Transform
	}{
		{"k1 row 1", Page, geom.Box{X: 72, Y: 72, W: 144, H: 72}, 0.5, 0.5,
			Transform{PinX: 2, PinY: 9.5, Width: 2, Height: 1, LocPinX: 1, LocPinY: 0.5}},
		{"k1 row 2", Page, geom.Box{X: 0, Y: 0, W: 72, H: 72}, 0.5, 0.5,
			Transform{PinX: 0.5, PinY: 10.5, Width: 1, Height: 1, LocPinX: 0.5, LocPinY: 0.5}},
		{"p1 row 1", Page, geom.Box{X: 108, Y: 126, W: 72, H: 36}, 0.5, 0.5,
			Transform{PinX: 2, PinY: 9, Width: 1, Height: 0.5, LocPinX: 0.5, LocPinY: 0.25}},
		{"p1 row 2", Page, geom.Box{X: 108, Y: 126, W: 72, H: 36, Rot: 270}, 0.5, 0.5,
			Transform{PinX: 2, PinY: 9, Width: 1, Height: 0.5, LocPinX: 0.5, LocPinY: 0.25, Angle: math.Pi / 2}},
		{"p1 row 4", Page, geom.Box{X: 90, Y: 90, W: 72, H: 36, Rot: 270}, 0, 0,
			Transform{PinX: 2, PinY: 9, Width: 1, Height: 0.5, Angle: math.Pi / 2}},
		{"p1 row 5", Page.Child(group), geom.Box{X: 216, Y: 432, W: 72, H: 72}, 0.5, 0.5,
			Transform{PinX: 0.5, PinY: 0.5, Width: 1, Height: 1, LocPinX: 0.5, LocPinY: 0.5}},
		{"p1 row 8", Page.Child(groupFlipY), geom.Box{X: 414, Y: 396, W: 36, H: 36, FlipV: true}, 0.5, 0.5,
			Transform{PinX: 0.5, PinY: 0.25, Width: 0.5, Height: 0.5, LocPinX: 0.25, LocPinY: 0.25}},
	}
	for _, r := range rows {
		got, err := Unplace(r.frame, r.want, 11, r.locX, r.locY)
		if err != nil {
			t.Errorf("%s: Unplace err = %v", r.name, err)
			continue
		}
		unplaceCheckTransform(t, r.name, got, r.got, 1e-9)
	}
}

// TestUnplaceSkew checks that a rotated box under a non-uniformly
// scaled frame, and a singular frame, fail with geom.ErrSkew.
func TestUnplaceSkew(t *testing.T) {
	frame := Frame{M: geom.Scale(2, 1)}
	want := geom.Box{X: 10, Y: 10, W: 50, H: 50, Rot: 30}
	if _, err := Unplace(frame, want, 11, 0.5, 0.5); !errors.Is(err, geom.ErrSkew) {
		t.Errorf("rotated under scale: err = %v, want geom.ErrSkew", err)
	}
	if _, err := Unplace(Frame{}, want, 11, 0.5, 0.5); !errors.Is(err, geom.ErrSkew) {
		t.Errorf("singular frame: err = %v, want geom.ErrSkew", err)
	}
}

// TestUnplaceRoundTrip is the K1 property: for 5,000 random transforms
// in random frames 1 to 3 deep, Unplace(Box(t)) == t within 1e-9, the
// LocPin fractions taken from t. It reuses randomTransform from
// place_test.go, whose draws are isometries, so no case skews.
func TestUnplaceRoundTrip(t *testing.T) {
	rng := rand.New(rand.NewPCG(1, 2))
	for i := 0; i < 5000; i++ {
		f := Page
		for depth := 1 + rng.IntN(3); depth > 0; depth-- {
			f = f.Child(randomTransform(rng))
		}
		tr := randomTransform(rng)
		box, err := Box(f, tr, 11)
		if err != nil {
			t.Fatalf("iter %d: Box err = %v", i, err)
		}
		got, err := Unplace(f, box, 11, tr.LocPinX/tr.Width, tr.LocPinY/tr.Height)
		if err != nil {
			t.Fatalf("iter %d: Unplace err = %v", i, err)
		}
		unplaceCheckTransform(t, "round trip", got, tr, 1e-9)
		if t.Failed() {
			t.Fatalf("iter %d: frame %+v, transform %+v, box %+v, got %+v", i, f, tr, box, got)
		}
	}
}

// unplaceCheckTransform reports any field of got further than tol from
// want, the angle modulo 2pi (pi and -pi are the same rotation).
func unplaceCheckTransform(t *testing.T, name string, got, want Transform, tol float64) {
	t.Helper()
	for _, f := range []struct {
		field     string
		got, want float64
	}{
		{"PinX", got.PinX, want.PinX},
		{"PinY", got.PinY, want.PinY},
		{"Width", got.Width, want.Width},
		{"Height", got.Height, want.Height},
		{"LocPinX", got.LocPinX, want.LocPinX},
		{"LocPinY", got.LocPinY, want.LocPinY},
	} {
		if math.Abs(f.got-f.want) > tol {
			t.Errorf("%s: %s = %v, want %v", name, f.field, f.got, f.want)
		}
	}
	dAngle := got.Angle - want.Angle
	if dAngle > math.Pi {
		dAngle -= 2 * math.Pi
	} else if dAngle <= -math.Pi {
		dAngle += 2 * math.Pi
	}
	if math.Abs(dAngle) > tol {
		t.Errorf("%s: Angle = %v, want %v", name, got.Angle, want.Angle)
	}
	if got.FlipX != want.FlipX || got.FlipY != want.FlipY {
		t.Errorf("%s: flips = %v/%v, want %v/%v", name, got.FlipX, got.FlipY, want.FlipX, want.FlipY)
	}
}
