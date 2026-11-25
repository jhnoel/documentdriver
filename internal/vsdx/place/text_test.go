package place

import (
	"math"
	"testing"

	"github.com/jhnoel/documentdriver/internal/geom"
)

// textRand is a small xorshift generator for the property test.
type textRand uint64

// float returns a value in [lo, hi).
func (r *textRand) float(lo, hi float64) float64 {
	x := uint64(*r)
	x ^= x << 13
	x ^= x >> 7
	x ^= x << 17
	*r = textRand(x)
	return lo + (hi-lo)*float64(x>>11)/float64(1<<53)
}

// TestTextCellsForProperty checks that TextBox inverts TextCellsFor on
// random shapes (sizes, pins, angles, flips) in random group frames:
// the box read shows is the box written, to 1e-6 pt and degrees.
func TestTextCellsForProperty(t *testing.T) {
	r := textRand(0x2545f4914f6cdd1d)
	for i := range 5000 {
		tr := Transform{PinX: r.float(0, 10), PinY: r.float(0, 10), Width: r.float(0.1, 5), Height: r.float(0, 3),
			Angle: r.float(-4, 4), FlipX: r.float(0, 1) < 0.3, FlipY: r.float(0, 1) < 0.3}
		tr.LocPinX, tr.LocPinY = tr.Width*r.float(0, 1), tr.Height*r.float(0, 1)
		f := Page
		if i%3 == 0 {
			g := Transform{PinX: r.float(0, 8), PinY: r.float(0, 8), Width: 4, Height: 3, LocPinX: 2, LocPinY: 1.5, Angle: r.float(-3, 3), FlipX: i%2 == 0}
			f = Page.Child(g)
		}
		want := geom.Box{X: r.float(-100, 700), Y: r.float(-100, 900), W: r.float(1, 300), H: r.float(1, 100), Rot: geom.NormDeg(r.float(-360, 360))}
		c, err := TextCellsFor(want, tr, f, 11)
		if err != nil {
			t.Fatalf("case %d: %v", i, err)
		}
		got := TextBox(c, tr, f, 11)
		dRot := math.Abs(math.Remainder(got.Rot-want.Rot, 360))
		if math.Abs(got.X-want.X) > 1e-6 || math.Abs(got.Y-want.Y) > 1e-6 || math.Abs(got.W-want.W) > 1e-6 || math.Abs(got.H-want.H) > 1e-6 || dRot > 1e-6 {
			t.Fatalf("case %d: box %+v, want %+v (cells %+v)", i, got, want, c)
		}
	}
}

// TestTextBoxDefault checks the default text block of an unrotated
// 144 x 72 pt shape whose box is [72, 72, 144, 72] on an 11 in page:
// the block is the shape's box at angle 0.
func TestTextBoxDefault(t *testing.T) {
	tr := Transform{PinX: 2, PinY: 11 - 1.5, Width: 2, Height: 1, LocPinX: 1, LocPinY: 0.5}
	got := TextBox(DefaultText(tr), tr, Page, 11)
	if math.Abs(got.X-72) > 1e-9 || math.Abs(got.Y-72) > 1e-9 || math.Abs(got.W-144) > 1e-9 || math.Abs(got.H-72) > 1e-9 || got.Rot != 0 {
		t.Errorf("default box %+v, want [72 72 144 72] at 0", got)
	}
	if _, err := TextCellsFor(geom.Box{W: 0, H: 1}, tr, Page, 11); err == nil {
		t.Error("a zero-width box should not place")
	}
}
