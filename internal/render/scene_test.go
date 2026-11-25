package render

import (
	"bytes"
	"math"
	"strings"
	"testing"

	"github.com/jhnoel/documentdriver/internal/geom"
	"github.com/jhnoel/documentdriver/internal/ooxml/tree"
)

// TestFlattenQuarterCircle checks that a cubic quarter circle of
// radius 100 flattens to points within the tolerance of the circle
// (the cubic itself is within 0.03 of it).
func TestFlattenQuarterCircle(t *testing.T) {
	k := 4.0 / 3 * math.Tan(math.Pi/8) * 100
	p := Path{}.MoveTo(geom.Pt{X: 100}).CubicTo(geom.Pt{X: 100, Y: k}, geom.Pt{X: k, Y: 100}, geom.Pt{Y: 100})
	polys := Flatten(p, 0.1)
	if len(polys) != 1 || len(polys[0].Pts) < 8 {
		t.Fatalf("Flatten = %d polys, %d points; want 1 poly of at least 8", len(polys), len(polys[0].Pts))
	}
	for _, pt := range polys[0].Pts {
		if d := math.Abs(math.Hypot(pt.X, pt.Y) - 100); d > 0.15 {
			t.Errorf("point %v is %.3f from the circle, want <= 0.15", pt, d)
		}
	}
}

// TestDashLengths checks a 100 pt line dashed 6 on, 3 off: 12 dashes
// starting at 0, each 6 long except the last (100 - 11*9 = 1).
func TestDashLengths(t *testing.T) {
	p := Dash([]Poly{{Pts: []geom.Pt{{}, {X: 100}}}}, []float64{6, 3})
	var dashes [][2]float64
	for i := 0; i+1 < len(p); i += 2 {
		if p[i].Op != 'M' || p[i+1].Op != 'L' {
			t.Fatalf("segment %d: ops %c %c, want M L", i, p[i].Op, p[i+1].Op)
		}
		dashes = append(dashes, [2]float64{p[i].P[0].X, p[i+1].P[0].X})
	}
	if len(dashes) != 12 {
		t.Fatalf("got %d dashes, want 12: %v", len(dashes), dashes)
	}
	for i, d := range dashes {
		want := [2]float64{float64(i) * 9, math.Min(float64(i)*9+6, 100)}
		if math.Abs(d[0]-want[0]) > 1e-9 || math.Abs(d[1]-want[1]) > 1e-9 {
			t.Errorf("dash %d = %v, want %v", i, d, want)
		}
	}
	if whole := Dash([]Poly{{Pts: []geom.Pt{{}, {X: 100}}}}, nil); len(whole) != 2 {
		t.Errorf("Dash with no pattern = %d segments, want the line whole (2)", len(whole))
	}
}

// rect returns the closed polyline of a rectangle.
func rect(x, y, w, h float64) []Poly {
	return []Poly{{Pts: []geom.Pt{{X: x, Y: y}, {X: x + w, Y: y}, {X: x + w, Y: y + h}, {X: x, Y: y + h}}, Closed: true}}
}

// TestHatchRect checks horizontal hatching of a 90 by 80 rectangle at
// the origin, 7.2 apart: the 11 lines y = 7.2k for k = 1..11 (the
// edges themselves are not drawn), each from x 0 to 90.
func TestHatchRect(t *testing.T) {
	p, cut := Hatch(rect(0, 0, 90, 80), 0, 7.2)
	if cut {
		t.Fatal("cut")
	}
	if len(p) != 22 {
		t.Fatalf("got %d segments, want 22 (11 lines)", len(p))
	}
	for i := 0; i < len(p); i += 2 {
		a, b := p[i].P[0], p[i+1].P[0]
		y := 7.2 * float64(i/2+1)
		if math.Abs(a.Y-y) > 1e-9 || math.Abs(b.Y-y) > 1e-9 || math.Abs(math.Min(a.X, b.X)) > 1e-9 || math.Abs(math.Max(a.X, b.X)-90) > 1e-9 {
			t.Errorf("line %d = %v to %v, want y %.1f from 0 to 90", i/2, a, b, y)
		}
	}
}

// TestHatchConcave checks that a line through both arms of a U shape
// is clipped to two pieces, never across the gap, and that diagonal
// hatch endpoints lie on the outline.
func TestHatchConcave(t *testing.T) {
	u := []Poly{{Pts: []geom.Pt{{}, {X: 30}, {X: 30, Y: 60}, {X: 60, Y: 60}, {X: 60}, {X: 90}, {X: 90, Y: 90}, {Y: 90}}, Closed: true}}
	p, _ := Hatch(u, 0, 10)
	for i := 0; i < len(p); i += 2 {
		a, b := p[i].P[0], p[i+1].P[0]
		mid := lerpPt(a, b, 0.5)
		if !Inside(u, mid) {
			t.Errorf("hatch piece %v-%v has its middle outside the shape", a, b)
		}
		if a.Y < 60 && (math.Abs(b.X-a.X) > 30+1e-9) {
			t.Errorf("hatch piece %v-%v crosses the gap", a, b)
		}
	}
	d, _ := Hatch(rect(0, 0, 50, 50), -45, 5)
	if len(d) == 0 {
		t.Fatal("no diagonal hatch")
	}
	for _, s := range d {
		pt := s.P[0]
		onEdge := math.Abs(pt.X) < 1e-9 || math.Abs(pt.Y) < 1e-9 || math.Abs(pt.X-50) < 1e-9 || math.Abs(pt.Y-50) < 1e-9
		if !onEdge {
			t.Errorf("diagonal hatch end %v is not on the rectangle's outline", pt)
		}
	}
}

// TestDotsInside checks that every dot of a triangle's stipple has its
// centre inside the triangle and that there are some.
func TestDotsInside(t *testing.T) {
	tri := []Poly{{Pts: []geom.Pt{{X: 10, Y: 10}, {X: 110, Y: 10}, {X: 60, Y: 90}}, Closed: true}}
	p, cut := Dots(tri, 3.6, 1)
	if cut || len(p) == 0 {
		t.Fatalf("Dots = %d segments, cut %v", len(p), cut)
	}
	for i := 0; i+4 < len(p); i += 5 {
		c := lerpPt(p[i].P[0], p[i+2].P[0], 0.5)
		if !Inside(tri, c) {
			t.Errorf("dot at %v is outside", c)
		}
	}
}

// TestLayoutWrapAndAlign checks wrapping against the width tables and
// the three vertical alignments: no line wider than the box less its
// margins (by the tables), lines in order, centred text anchored in
// the middle, and top, middle and bottom moving the first baseline
// down.
func TestLayoutWrapAndAlign(t *testing.T) {
	text := "The quick brown fox jumps over the lazy dog and keeps on running far away"
	box := TextBox{Frame: geom.Identity, W: 120, H: 200, Margin: [4]float64{4, 4, 4, 4}, Paras: []TextPara{{Align: "center", Runs: []TextRun{{Text: text, Size: 12}}}}}
	baseline := map[string]float64{}
	for _, va := range []string{"top", "middle", "bottom"} {
		box.VAlign = va
		items, notes := LayoutText(box)
		if len(notes) > 0 {
			t.Fatalf("notes %v", notes)
		}
		var words []string
		prevY := -1.0
		for _, it := range items {
			if it.Kind != TextItem {
				continue
			}
			l := it.Text
			if l.Anchor != "middle" || l.X != 60 {
				t.Errorf("%s: line anchored %s at %v, want middle at 60", va, l.Anchor, l.X)
			}
			w := 0.0
			for _, sp := range l.Spans {
				w += TextWidth(sp.Family, sp.Bold, sp.Italic, sp.Size, sp.Text)
				words = append(words, strings.Fields(sp.Text)...)
			}
			if w > 112+1e-9 {
				t.Errorf("%s: line %q is %.2f wide, want <= 112", va, l.Spans[0].Text, w)
			}
			if l.Y <= prevY {
				t.Errorf("%s: baselines not increasing", va)
			}
			if prevY < 0 {
				baseline[va] = l.Y
			}
			prevY = l.Y
		}
		if got := strings.Join(words, " "); got != text {
			t.Errorf("%s: laid out %q, want %q", va, got, text)
		}
	}
	if !(baseline["top"] < baseline["middle"] && baseline["middle"] < baseline["bottom"]) {
		t.Errorf("first baselines %v: want top < middle < bottom", baseline)
	}
	if baseline["top"] < 4 || baseline["top"] > 4+12*1.2 {
		t.Errorf("top baseline %.2f, want within the first line under the 4 pt margin", baseline["top"])
	}
}

// TestLayoutLongWord checks that a word wider than the box is cut
// between characters onto several lines, none wider than the box.
func TestLayoutLongWord(t *testing.T) {
	box := TextBox{Frame: geom.Identity, W: 40, H: 100, Paras: []TextPara{{Align: "left", Runs: []TextRun{{Text: strings.Repeat("W", 20)}}}}}
	items, _ := LayoutText(box)
	n := 0
	for _, it := range items {
		if it.Kind == TextItem {
			n++
			if w := TextWidth(it.Text.Spans[0].Family, false, false, 12, it.Text.Spans[0].Text); w > 40+1e-9 {
				t.Errorf("line %q is %.1f wide, want <= 40", it.Text.Spans[0].Text, w)
			}
		}
	}
	if n < 5 {
		t.Errorf("%d lines, want the word cut onto at least 5", n)
	}
}

// TestRasteriseFillAndStroke checks coverage: a filled 10 by 10
// square at (10.25, 10.25) at scale 2 is solid inside, white outside and
// half-covered on its edge pixels; a 2 pt horizontal stroke covers
// exactly the four pixel rows under it.
func TestRasteriseFillAndStroke(t *testing.T) {
	s := &Scene{W: 40, H: 40, Background: "#FFFFFF", Groups: []Group{{Items: []Item{
		{Kind: FillItem, Path: Polygon(geom.Pt{X: 10.25, Y: 10.25}, geom.Pt{X: 20.25, Y: 10.25}, geom.Pt{X: 20.25, Y: 20.25}, geom.Pt{X: 10.25, Y: 20.25}), Color: "#000000"},
		{Kind: StrokeItem, Path: Path{}.MoveTo(geom.Pt{X: 5, Y: 30}).LineTo(geom.Pt{X: 35, Y: 30}), Color: "#000000", Width: 2},
	}}}}
	img, notes, err := Rasterise(s, 2)
	if err != nil || len(notes) > 0 {
		t.Fatalf("Rasterise: %v %v", err, notes)
	}
	gray := func(x, y int) int { return int(img.RGBAAt(x, y).R) }
	if g := gray(30, 30); g != 0 {
		t.Errorf("inside the square: %d, want 0", g)
	}
	if g := gray(5, 5); g != 255 {
		t.Errorf("outside: %d, want 255", g)
	}
	if g := gray(20, 30); g < 120 || g > 135 {
		t.Errorf("left edge pixel (half covered): %d, want about 128", g)
	}
	for y := 56; y < 64; y++ {
		g := gray(40, y)
		want := 255
		if y >= 58 && y < 62 {
			want = 0
		}
		if g != want {
			t.Errorf("stroke row %d: %d, want %d", y, g, want)
		}
	}
}

// TestRasteriseOverlapNoCancel checks that a polyline doubling back on
// itself strokes solid (the segments' outlines add, not cancel).
func TestRasteriseOverlapNoCancel(t *testing.T) {
	s := &Scene{W: 20, H: 20, Background: "#FFFFFF", Groups: []Group{{Items: []Item{
		{Kind: StrokeItem, Path: Path{}.MoveTo(geom.Pt{X: 2, Y: 10}).LineTo(geom.Pt{X: 18, Y: 10}).LineTo(geom.Pt{X: 4, Y: 10}), Color: "#000000", Width: 4},
	}}}}
	img, _, err := Rasterise(s, 1)
	if err != nil {
		t.Fatal(err)
	}
	if g := img.RGBAAt(10, 10).R; g != 0 {
		t.Errorf("doubled-back stroke at its middle: %d, want 0", g)
	}
}

// xorshift is a small deterministic generator for the property test
// (the determinism rule keeps math/rand out of the repository).
type xorshift uint64

// next returns the next value.
func (x *xorshift) next() uint64 {
	*x ^= *x << 13
	*x ^= *x >> 7
	*x ^= *x << 17
	return uint64(*x)
}

// float returns a value in [lo, hi).
func (x *xorshift) float(lo, hi float64) float64 {
	return lo + (hi-lo)*float64(x.next()%1_000_000)/1_000_000
}

// TestSceneProperty checks, over random scenes (paths of every op,
// strokes, text with markup characters, controls and non-ASCII,
// attributes with quotes, images of junk bytes), that WriteSVG writes
// a well-formed document whose root is svg and that Rasterise never
// fails or panics.
func TestSceneProperty(t *testing.T) {
	r := xorshift(0x9e3779b97f4a7c15)
	texts := []string{"a<b>&c", "\"quoted' \x01\x7f", "日本語", "tab\there\nnew", "￾￿é", ""}
	for i := range 200 {
		s := &Scene{W: r.float(1, 300), H: r.float(1, 300), Background: "#FFFFFF", Attrs: [][2]string{{"data-x", `<"&'>`}}}
		for g := range 1 + int(r.next()%4) {
			grp := Group{ID: "page0/s" + string(rune('0'+g)), Attrs: [][2]string{{"data-kind", "a\"b"}}}
			for range 1 + int(r.next()%5) {
				var p Path
				for range 1 + int(r.next()%6) {
					pt := func() geom.Pt { return geom.Pt{X: r.float(-50, 350), Y: r.float(-50, 350)} }
					switch r.next() % 4 {
					case 0:
						p = p.MoveTo(pt())
					case 1:
						p = p.LineTo(pt())
					case 2:
						p = p.CubicTo(pt(), pt(), pt())
					default:
						p = p.Close()
					}
				}
				switch r.next() % 4 {
				case 0:
					grp.Items = append(grp.Items, Item{Kind: FillItem, Path: p, Color: "#123456", Opacity: r.float(0, 1)})
				case 1:
					grp.Items = append(grp.Items, Item{Kind: StrokeItem, Path: p, Color: "#ABCDEF", Width: r.float(0, 5)})
				case 2:
					grp.Items = append(grp.Items, Item{Kind: TextItem, Text: &TextLine{Frame: geom.Rotate(r.float(0, 360)), X: r.float(0, 100), Y: r.float(0, 100), Anchor: "middle",
						Spans: []Span{{Text: texts[r.next()%uint64(len(texts))], Family: Family("Odd 'Face'"), Size: r.float(1, 30), Color: "#000000", Bold: true}, {Text: texts[r.next()%uint64(len(texts))], Family: Family(""), Size: 8, Color: "#FF0000", Italic: true}}}})
				default:
					grp.Items = append(grp.Items, Item{Kind: ImageItem, Image: &Image{Frame: geom.Scale(r.float(1, 50), r.float(1, 50)), Type: "image/png", Data: []byte("not a png"), Crop: [4]float64{0.1, 0.1, 0.5, 0.5}}})
				}
			}
			s.Groups = append(s.Groups, grp)
		}
		var buf bytes.Buffer
		if err := WriteSVG(&buf, s); err != nil {
			t.Fatal(err)
		}
		d, err := tree.Parse(buf.Bytes())
		if err != nil {
			t.Fatalf("scene %d: SVG not well formed: %v\n%s", i, err, buf.String())
		}
		if d.Root() == nil || d.Root().Local() != "svg" {
			t.Fatalf("scene %d: root is not svg", i)
		}
		if _, _, err := Rasterise(s, r.float(0.2, 3)); err != nil {
			t.Fatalf("scene %d: Rasterise: %v", i, err)
		}
	}
}
