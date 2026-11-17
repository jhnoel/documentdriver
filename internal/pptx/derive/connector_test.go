package derive

import (
	"errors"
	"math"
	"math/rand/v2"
	"testing"

	"github.com/jhnoel/documentdriver/internal/geom"
)

func connectorPresets() []string {
	var out []string
	for name, d := range presetTable {
		if d.paths != nil {
			out = append(out, name)
		}
	}
	return out
}

// TestConnectorPathsRunCornerToCorner checks the fact ConnectorXfrm rests
// on: every tabulated connector path starts at the local top-left and ends
// at the local bottom-right, whatever its adjust values.
func TestConnectorPathsRunCornerToCorner(t *testing.T) {
	ps := connectorPresets()
	if len(ps) < 10 {
		t.Fatalf("only %d connector presets", len(ps))
	}
	for _, prst := range ps {
		g, err := ConnectorGeom(prst, nil, 3000, 2000, Identity)
		if err != nil {
			t.Fatal(err)
		}
		if g.From != (Pt{X: 0, Y: 0}) || g.To != (Pt{X: 3000, Y: 2000}) {
			t.Errorf("%s: path runs %v -> %v", prst, g.From, g.To)
		}
	}
}

func testParents(t *testing.T) map[string]Parent {
	t.Helper()
	child := func(p Parent, g GroupXfrm) Parent {
		c, err := p.Child(g)
		if err != nil {
			t.Fatal(err)
		}
		return c
	}
	flipped := child(Root, GroupXfrm{Xfrm: Xfrm{X: 100, Y: 50, W: 2000, H: 1000, FlipH: true}, ChW: 4000, ChH: 2000})
	rot90 := child(Root, GroupXfrm{Xfrm: Xfrm{X: 500, Y: 500, W: 1000, H: 1000, Rot: 90}, ChX: 7, ChY: 3, ChW: 1000, ChH: 1000})
	rot33 := child(Root, GroupXfrm{Xfrm: Xfrm{X: 10, Y: 20, W: 3000, H: 1000, Rot: 33, FlipV: true}, ChW: 1500, ChH: 1500})
	return map[string]Parent{
		"slide":            Root,
		"flipped group":    flipped,
		"rotated 90 group": rot90,
		"rotated 33 group": rot33,
		"nested":           child(rot90, GroupXfrm{Xfrm: Xfrm{X: 0, Y: 0, W: 500, H: 500, Rot: 180, FlipH: true}, ChW: 500, ChH: 500}),
	}
}

func TestConnectorXfrmRoundTrip(t *testing.T) {
	rng := rand.New(rand.NewPCG(1, 2))
	for name, p := range testParents(t) {
		for range 200 {
			from := Pt{X: rng.Float64()*1e6 - 5e5, Y: rng.Float64()*1e6 - 5e5}
			to := Pt{X: rng.Float64()*1e6 - 5e5, Y: rng.Float64()*1e6 - 5e5}
			for _, c := range []struct{ prst, first string }{{"straightConnector1", ""}, {"bentConnector3", "h"}, {"bentConnector3", "v"}, {"curvedConnector3", ""}} {
				x, err := ConnectorXfrm(from, to, c.first, nil, p)
				if err != nil {
					t.Fatalf("%s %s: %v", name, c.prst, err)
				}
				g, err := ConnectorGeom(c.prst, nil, x.W, x.H, x.Matrix().Then(p.M))
				if err != nil {
					t.Fatal(err)
				}
				if math.Hypot(g.From.X-from.X, g.From.Y-from.Y) > 1e-6 || math.Hypot(g.To.X-to.X, g.To.Y-to.Y) > 1e-6 {
					t.Fatalf("%s %s: %v -> %v, want %v -> %v (xfrm %+v)", name, c.prst, g.From, g.To, from, to, x)
				}
				if c.first != "" && g.First != c.first {
					t.Fatalf("%s %s: first %s, want %s", name, c.prst, g.First, c.first)
				}
				if c.first == "" && x.Rot != 0 {
					t.Fatalf("%s: new straight connector rotated %v", name, x.Rot)
				}
			}
		}
	}
}

func TestConnectorXfrmKeepsExpressibleEncoding(t *testing.T) {
	// A connector stored rotated by 180 with both flips: endpoints in the
	// same quadrant keep that encoding.
	cur := Xfrm{X: 0, Y: 0, W: 1000, H: 500, Rot: 180, FlipH: true, FlipV: true}
	g, _ := ConnectorGeom("straightConnector1", nil, cur.W, cur.H, cur.Matrix())
	from, to := g.From, Pt{X: g.To.X + 300, Y: g.To.Y + 100}
	x, err := ConnectorXfrm(from, to, "", &cur, Root)
	if err != nil {
		t.Fatal(err)
	}
	if x.Rot != 180 || !x.FlipH || !x.FlipV || !near(x.W, 1300) || !near(x.H, 600) {
		t.Fatalf("encoding not kept: %+v", x)
	}
	// Endpoints that would need other flips fall back to rotation 0,
	// with the flips they need.
	x, err = ConnectorXfrm(to, Pt{X: to.X + 10, Y: to.Y - 10}, "", &cur, Root)
	if err != nil {
		t.Fatal(err)
	}
	if x.Rot != 0 || x.FlipH || !x.FlipV {
		t.Fatalf("no fallback: %+v", x)
	}
	// An elbow whose first segment changes falls back to 90 degrees.
	elbow := Xfrm{W: 1000, H: 500}
	x, err = ConnectorXfrm(Pt{X: 0, Y: 0}, Pt{X: 1000, Y: 500}, "v", &elbow, Root)
	if err != nil || x.Rot != 90 {
		t.Fatalf("first v: %+v, %v", x, err)
	}
}

func TestConnectorXfrmSingular(t *testing.T) {
	p := Parent{M: Affine{}}
	if _, err := ConnectorXfrm(Pt{}, Pt{X: 1, Y: 1}, "", nil, p); !errors.Is(err, ErrInexpressible) {
		t.Fatalf("err = %v", err)
	}
}

// FuzzUnplace checks the inverse law of the group transform: Unplace
// either fails with ErrInexpressible or returns a stored transform that
// Place maps back to the requested slide-space box, rotation and flips.
func FuzzUnplace(f *testing.F) {
	f.Add(0.0, false, false, 1.0, 1.0, 10.0, 20.0, 30.0, 40.0, 0.0, false, false)
	f.Add(90.0, true, false, 2.0, 2.0, 5.0, 6.0, 70.0, 20.0, 17.0, true, false)
	f.Add(33.0, false, true, 0.5, 0.5, 50.0, 60.0, 10.0, 90.0, 270.0, false, true)
	f.Add(45.0, false, false, 2.0, 1.0, 50.0, 60.0, 10.0, 90.0, 30.0, false, true)
	f.Fuzz(func(t *testing.T, grot float64, gfh, gfv bool, sx, sy, x, y, w, h, rot float64, fh, fv bool) {
		for _, v := range []float64{grot, sx, sy, x, y, w, h, rot} {
			if math.IsNaN(v) || math.IsInf(v, 0) || math.Abs(v) > 1e6 {
				return
			}
		}
		if sx <= 1e-3 || sy <= 1e-3 || w < 0 || h < 0 {
			return
		}
		g := GroupXfrm{Xfrm: Xfrm{X: 100 * pt, Y: 50 * pt, W: 400 * pt * sx, H: 200 * pt * sy, Rot: geom.NormDeg(grot), FlipH: gfh, FlipV: gfv}, ChX: 3 * pt, ChY: 7 * pt, ChW: 400 * pt, ChH: 200 * pt}
		p, err := Root.Child(g)
		if err != nil {
			return
		}
		want := Xfrm{X: x * pt, Y: y * pt, W: w * pt, H: h * pt, Rot: geom.NormDeg(rot), FlipH: fh, FlipV: fv}
		back, err := Unplace(want, p)
		if err != nil {
			if !errors.Is(err, ErrInexpressible) {
				t.Fatalf("Unplace err = %v", err)
			}
			return
		}
		got, err := Place(back, p)
		if err != nil {
			t.Fatalf("Place(Unplace(want)) failed: %v", err)
		}
		b := got.Box
		tol := 1e-6 * (1 + math.Abs(want.X) + math.Abs(want.Y) + want.W + want.H)
		if got.Skew > 0 {
			return // within SkewTolerance: the box keeps the mapped x axis, so it is near, not equal
		}
		if math.Abs(b.X-want.X) > tol || math.Abs(b.Y-want.Y) > tol || math.Abs(b.W-want.W) > tol || math.Abs(b.H-want.H) > tol {
			t.Fatalf("box %+v, want %+v", b, want)
		}
		if b.FlipH != want.FlipH || b.FlipV != want.FlipV {
			t.Fatalf("flips %v %v, want %v %v", b.FlipH, b.FlipV, want.FlipH, want.FlipV)
		}
		if d := math.Abs(math.Remainder(b.Rot-want.Rot, 360)); d > 1e-6 && want.W*want.H > 0 {
			t.Fatalf("rot %v, want %v", b.Rot, want.Rot)
		}
	})
}
