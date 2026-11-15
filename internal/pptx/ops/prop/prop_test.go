package prop

import (
	"bytes"
	"errors"
	"testing"

	"github.com/jhnoel/documentdriver/internal/opc"
	"github.com/jhnoel/documentdriver/internal/pptx/derive"
	"github.com/jhnoel/documentdriver/internal/pptx/ops/edit"
	"github.com/jhnoel/documentdriver/internal/testdeck"
)

const slide1 = "/ppt/slides/slide1.xml"

func open(t *testing.T, path string) (*opc.Package, *edit.Workspace) {
	t.Helper()
	p, err := opc.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { p.Close() })
	return p, edit.New(p)
}

func handle(t *testing.T, w *edit.Workspace, addr string) *edit.Handle {
	t.Helper()
	r, err := w.Resolve(addr)
	if err != nil {
		t.Fatal(err)
	}
	h, err := w.Target(r)
	if err != nil {
		t.Fatal(err)
	}
	return h
}

func changed(t *testing.T, p *opc.Package) []string {
	t.Helper()
	c, err := p.ChangedParts()
	if err != nil {
		t.Fatal(err)
	}
	return c
}

var diagramIDs = []string{"10", "11", "12", "13", "14", "20", "21", "22", "23", "24", "25", "26", "30", "31", "40", "40~2", "50", "51", "60", "61", "70", "71"}

func TestIdentityWriteTouchesNothing(t *testing.T) {
	p, w := open(t, testdeck.Diagram(t))
	c := Transform(Pt)
	for _, id := range append(diagramIDs, "2", "3") {
		h := handle(t, w, "slide1#"+id)
		cur, ok := c.Read(h, 0).Get()
		if !ok {
			t.Logf("s%s: %s", id, c.Read(h, 0).Reason())
			continue
		}
		// The value shown, written back in the shown units (rounded to
		// 0.01 pt, so up to 63 EMU away), is a no-op.
		shown := cur
		for _, v := range []*float64{&shown.X, &shown.Y, &shown.W, &shown.H} {
			*v = float64(int64(*v/derive.EMUPerPoint*100+0.5)) / 100 * derive.EMUPerPoint
		}
		if err := Set(h, c, shown); err != nil {
			t.Fatalf("s%s: %v", id, err)
		}
	}
	if got := changed(t, p); len(got) != 0 {
		t.Fatalf("identity writes changed %v", got)
	}
}

func TestSetReadsBack(t *testing.T) {
	cases := []struct {
		id   string
		want derive.Xfrm
	}{
		{"10", derive.Xfrm{X: 72 * 12700, Y: 36 * 12700, W: 100 * 12700, H: 50 * 12700}},
		{"10", derive.Xfrm{X: 72 * 12700, Y: 36 * 12700, W: 100 * 12700, H: 50 * 12700, Rot: 33.33, FlipH: true}},
		{"21", derive.Xfrm{X: 500 * 12700, Y: 300 * 12700, W: 40 * 12700, H: 20 * 12700, Rot: 12.5}},   // under a flipped, scaled group
		{"26", derive.Xfrm{X: 100 * 12700, Y: 300 * 12700, W: 40 * 12700, H: 20 * 12700, FlipV: true}}, // under a rotated group
		{"30", derive.Xfrm{X: 700 * 12700, Y: 50 * 12700, W: 30 * 12700, H: 30 * 12700}},               // both mc branches
		{"20", derive.Xfrm{X: 400 * 12700, Y: 250 * 12700, W: 300 * 12700, H: 100 * 12700, FlipH: true}},
	}
	for _, cs := range cases {
		p, w := open(t, testdeck.Diagram(t))
		c := Transform(Pt)
		h := handle(t, w, "slide1#"+cs.id)
		if err := Set(h, c, cs.want); err != nil {
			t.Fatalf("s%s: %v", cs.id, err)
		}
		for i := range h.Branches() {
			got, _ := c.Read(h, i).Get()
			if c.Show(got) != c.Show(cs.want) {
				t.Errorf("s%s branch %d: %s, want %s", cs.id, i, c.Show(got), c.Show(cs.want))
			}
		}
		if err := w.Verify(); err != nil {
			t.Errorf("s%s: %v", cs.id, err)
		}
		if got := changed(t, p); len(got) != 1 || got[0] != slide1 {
			t.Errorf("s%s: changed %v", cs.id, got)
		}
	}
}

func TestSetInexpressibleFails(t *testing.T) {
	_, w := open(t, testdeck.Diagram(t))
	// Make group s20 scale its children non-uniformly (1 x 0.5).
	if _, err := w.RawSetAttr(slide1, "//p:cNvPr[@id='20']", "name", "x"); err != nil {
		t.Fatal(err)
	}
	if _, err := w.RawSetAttr(slide1, "/p:sld/p:cSld/p:spTree/p:grpSp[1]/p:grpSpPr/a:xfrm/a:chExt", "cx", "2540000"); err != nil {
		t.Fatal(err)
	}
	c := Transform(Pt)
	h := handle(t, w, "slide1#21")
	cur, ok := c.Read(h, 0).Get()
	if !ok {
		t.Fatal(c.Read(h, 0).Reason())
	}
	before := h.Elem(0).Bytes()
	cur.Rot = 30 // a rotated box under a non-uniform scale is a skew
	if err := Set(h, c, cur); !errors.Is(err, ErrInexpressible) {
		t.Fatalf("err = %v", err)
	}
	if !bytes.Equal(before, h.Elem(0).Bytes()) {
		t.Fatal("failed write changed the object")
	}
}

func TestMoveAndBackIsByteIdentical(t *testing.T) {
	p, w := open(t, testdeck.Diagram(t))
	c := Transform(Pt)
	for _, id := range []string{"10", "21", "26", "30", "20", "13"} {
		h := handle(t, w, "slide1#"+id)
		if err := Move(h, c, 12.5*12700, -3*12700); err != nil {
			t.Fatalf("s%s: %v", id, err)
		}
		if err := Move(h, c, -12.5*12700, 3*12700); err != nil {
			t.Fatalf("s%s back: %v", id, err)
		}
	}
	pt, _ := p.Part(slide1)
	cur, _ := pt.Bytes()
	orig, _ := pt.Original()
	if !bytes.Equal(cur, orig) {
		t.Fatal("move and back changed bytes")
	}
}

func TestPlaceholderWithoutTransform(t *testing.T) {
	_, w := open(t, testdeck.Diagram(t))
	c := Transform(Pt)
	h := handle(t, w, "slide1#2")
	cur, ok := c.Read(h, 0).Get()
	if !ok {
		t.Fatal("inherited box not read")
	}
	cur.X += 12700
	if err := Set(h, c, cur); err != nil {
		t.Fatal(err)
	}
	got, _ := c.Read(h, 0).Get()
	if c.Show(got) != c.Show(cur) || c.Read(h, 0).Origin() != derive.FromSlide {
		t.Fatalf("read back %s from %s", c.Show(got), c.Read(h, 0).Origin())
	}
	if err := w.Verify(); err != nil {
		t.Fatal(err)
	}
	// The transform is inserted first in p:spPr, as the schema orders it.
	spPr := h.Find(edit.Path{edit.P("spPr")})
	if first := spPr.Elements()[0]; first.Name() != "a:xfrm" || len(first.Elements()) != 2 {
		t.Fatalf("spPr = %s", spPr.Bytes())
	}
	// Moving the other placeholder inserts its transform too, moved.
	h3 := handle(t, w, "slide1#3")
	before, _ := c.Read(h3, 0).Get()
	if err := Move(h3, c, 0, -6*12700); err != nil {
		t.Fatal(err)
	}
	after, _ := c.Read(h3, 0).Get()
	if after.Y != before.Y-6*12700 || after.X != before.X {
		t.Fatalf("moved %v -> %v", before, after)
	}
}
