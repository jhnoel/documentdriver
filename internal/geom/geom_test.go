package geom

import (
	"errors"
	"math"
	"math/rand/v2"
	"testing"
)

// TestFormatNum checks the two-decimal view form.
func TestFormatNum(t *testing.T) {
	for in, want := range map[float64]string{0: "0", -0.001: "0", 1.5: "1.5", 2.005: "2", 72: "72", -3.14159: "-3.14", 0.126: "0.13"} {
		if got := FormatNum(in); got != want {
			t.Errorf("FormatNum(%v) = %q, want %q", in, got, want)
		}
	}
}

// TestSinCosDegExact checks that right angles give exact values.
func TestSinCosDegExact(t *testing.T) {
	for deg, want := range map[float64][2]float64{0: {0, 1}, 90: {1, 0}, 180: {0, -1}, 270: {-1, 0}, -90: {-1, 0}, 450: {1, 0}} {
		s, c := SinCosDeg(deg)
		if s != want[0] || c != want[1] {
			t.Errorf("SinCosDeg(%v) = %v, %v; want %v", deg, s, c, want)
		}
	}
}

// TestDecomposeRoundTrip is the property Box.Matrix then Decompose is the
// identity on boxes (rotation normalised to [0, 360)).
func TestDecomposeRoundTrip(t *testing.T) {
	rng := rand.New(rand.NewPCG(1, 2))
	for range 2000 {
		b := Box{X: rng.Float64()*1000 - 500, Y: rng.Float64()*1000 - 500, W: rng.Float64()*300 + 1, H: rng.Float64()*300 + 1,
			Rot: NormDeg(rng.Float64() * 720), FlipH: rng.IntN(2) == 0, FlipV: rng.IntN(2) == 0}
		got, skew, err := Decompose(b.Matrix(), b.W, b.H, b.FlipV, 1e-6)
		if err != nil || skew > 1e-6 {
			t.Fatalf("Decompose(%+v): %v (skew %v)", b, err, skew)
		}
		if got.FlipH != b.FlipH || got.FlipV != b.FlipV || math.Abs(got.X-b.X) > 1e-6 || math.Abs(got.Y-b.Y) > 1e-6 ||
			math.Abs(got.W-b.W) > 1e-6 || math.Abs(got.H-b.H) > 1e-6 || math.Abs(math.Remainder(got.Rot-b.Rot, 360)) > 1e-6 {
			t.Fatalf("Decompose(Matrix(%+v)) = %+v", b, got)
		}
	}
}

// TestDecomposeSkew checks that a sheared map is refused beyond the
// tolerance and accepted within it.
func TestDecomposeSkew(t *testing.T) {
	shear := Affine{A: 1, B: 0, C: 0.1, D: 1}
	if _, _, err := Decompose(shear, 10, 10, false, 0.5); !errors.Is(err, ErrSkew) {
		t.Errorf("shear 0.1 on a 10 box with tolerance 0.5: err %v, want ErrSkew", err)
	}
	if _, _, err := Decompose(shear, 10, 10, false, 2); err != nil {
		t.Errorf("shear 0.1 on a 10 box with tolerance 2: %v", err)
	}
}

// TestInvert checks m.Then(inverse) is the identity and a singular map
// is refused.
func TestInvert(t *testing.T) {
	m := Rotate(30).Then(Scale(2, 3)).Then(Translate(5, -7))
	inv, ok := m.Invert()
	if !ok {
		t.Fatal("Invert refused a regular map")
	}
	p := m.Then(inv).Apply(Pt{X: 3, Y: 4})
	if math.Abs(p.X-3) > 1e-9 || math.Abs(p.Y-4) > 1e-9 {
		t.Errorf("m then inverse maps (3,4) to %+v", p)
	}
	if _, ok := Scale(0, 1).Invert(); ok {
		t.Error("Invert accepted a singular map")
	}
}
