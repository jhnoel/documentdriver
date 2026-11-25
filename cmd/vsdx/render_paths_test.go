package main

import (
	"bytes"
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// patternNames are the contract's fill pattern names, in the order the
// render test lays them out.
var patternNames = []string{"hatch_up", "grid", "cross_hatch", "hatch_down", "horizontal", "vertical", "dots", "horizontal_fine", "vertical_fine", "hatch_down_fine", "hatch_up_fine", "grid_fine", "cross_hatch_fine"}

// patternBox is the box of pattern i on the render test's page.
func patternBox(i int) [4]float64 {
	return [4]float64{36 + float64(i%5)*108, 36 + float64(i/5)*100, 90, 80}
}

// renderPathsPlan is the render test's plan: every named pattern on a
// rect (no text, so the ink is the pattern's), a hatch with a clear
// background over a red rect and one with a white background over
// another, and the four test shapes (bell curve, sine-like polyline,
// arc, closed cross-hatched trapezoid).
func renderPathsPlan(bell, sine, arc [][]float64) string {
	var ops []map[string]any
	for i, n := range patternNames {
		b := patternBox(i)
		ops = append(ops, map[string]any{"op": "object.add", "at": "page0", "geom": "rect", "box": b[:], "fill": map[string]any{"pattern": n}, "line": "none"})
	}
	for _, bg := range []any{"none", map[string]any{"hex": "#FFFFFF"}} {
		x := 36.0
		if bg != "none" {
			x = 144
		}
		ops = append(ops,
			map[string]any{"op": "object.add", "at": "page0", "geom": "rect", "box": []float64{x, 340, 90, 60}, "fill": map[string]any{"hex": "#FF0000"}, "line": "none"},
			map[string]any{"op": "object.add", "at": "page0", "geom": "rect", "box": []float64{x, 340, 90, 60}, "fill": map[string]any{"pattern": "hatch_up", "bg": bg}, "line": "none"})
	}
	ops = append(ops,
		map[string]any{"op": "object.add", "at": "page0", "geom": "curve", "points": bell, "line": map[string]any{"w": 1.5, "end": "triangle"}},
		map[string]any{"op": "object.add", "at": "page0", "geom": "polyline", "points": sine, "line": map[string]any{"w": 1.5}},
		map[string]any{"op": "object.add", "at": "page0", "geom": "curve", "points": arc, "line": map[string]any{"w": 1.5, "begin": "stealth", "end": "triangle"}},
		map[string]any{"op": "object.add", "at": "page0", "geom": "polyline", "closed": true, "points": [][]float64{{80, 760}, {220, 760}, {190, 680}, {110, 680}}, "fill": map[string]any{"pattern": "cross_hatch", "bg": "#FFFFFF"}, "line": map[string]any{"w": 1}})
	b, err := json.Marshal(map[string]any{"ops": ops})
	if err != nil {
		panic(err)
	}
	return string(b)
}

// TestRenderPatternsAndPaths renders, through LibreOffice, a drawing
// with every named fill pattern and the four test shapes, and checks
// the picture: each pattern rect is partly inked (a pattern, neither
// empty nor solid); hatch_up's strokes rise to the right and
// hatch_down's fall (LibreOffice's 45 and 315 degree hatches);
// a clear background shows the red rect beneath between the strokes
// and a white one hides it; the bell curve, the sine series and the
// arc are drawn through sample points of their own formulas and not
// at points away from them; the trapezoid's inside is hatched. With
// DD_RENDER_KEEP set to a directory the PNG and drawing are copied
// there for a person to look at. LibreOffice is a render check, not
// Visio: Visio draws the patterns from its own bitmaps.
func TestRenderPatternsAndPaths(t *testing.T) {
	needRenderer(t)
	bellY := func(x float64) float64 { z := (x - 192) / 36; return 560 - 120*math.Exp(-z*z/2) }
	sineY := func(x float64) float64 { return 480 - 30*math.Sin((x-340)/25) }
	var bell, sine, arc [][]float64
	for x := 72.0; x <= 312; x += 6 {
		bell = append(bell, []float64{x, bellY(x)})
	}
	for x := 340.0; x <= 590; x += 5 {
		sine = append(sine, []float64{x, sineY(x)})
	}
	for i := 0; i <= 8; i++ {
		a := float64(i) * math.Pi / 16
		arc = append(arc, []float64{400 + 80*math.Cos(a), 720 - 80*math.Sin(a)})
	}
	dir := t.TempDir()
	plan := filepath.Join(dir, "plan.json")
	if err := os.WriteFile(plan, []byte(renderPathsPlan(bell, sine, arc)), 0o600); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(dir, "paths.vsdx")
	var stdout, stderr bytes.Buffer
	if code := run([]string{"create", plan, "-o", file}, &stdout, &stderr); code != 0 {
		t.Fatalf("create: exit %d: %s", code, stderr.String())
	}
	out := filepath.Join(dir, "paths.png")
	stdout.Reset()
	if code := run([]string{"render", file, "--page", "page0", "--scale", "3", "-o", out}, &stdout, &stderr); code != 0 {
		t.Fatalf("render: exit %d: %s", code, stderr.String())
	}
	if keep := os.Getenv("DD_RENDER_KEEP"); keep != "" {
		for _, f := range []string{out, file} {
			b, err := os.ReadFile(f) //nolint:gosec // G304: a file the test just wrote
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(keep, filepath.Base(f)), b, 0o600); err != nil {
				t.Fatal(err)
			}
		}
	}
	w, _, at := decodeRenderPNG(t, out)
	step := 612 / float64(w)
	// dark is any inked pixel: LibreOffice draws thin diagonal
	// hatches antialiased grey.
	dark := func(x, y float64) bool {
		r, g, b := at(x, y)
		return r+g+b < 600
	}
	red := func(x, y float64) bool {
		r, g, b := at(x, y)
		return r > 200 && g < 80 && b < 80
	}
	white := func(x, y float64) bool {
		r, g, b := at(x, y)
		return r > 230 && g > 230 && b > 230
	}
	// share counts the pixels of the inner part of box (a 6 pt margin)
	// for which f holds, as a fraction.
	share := func(box [4]float64, f func(x, y float64) bool) float64 {
		n, hit := 0, 0
		for y := box[1] + 6; y < box[1]+box[3]-6; y += step {
			for x := box[0] + 6; x < box[0]+box[2]-6; x += step {
				n++
				if f(x, y) {
					hit++
				}
			}
		}
		return float64(hit) / float64(n)
	}
	// slant counts dark pixels whose neighbour one pixel up-right
	// (rising) or down-right (falling) is dark too.
	slant := func(box [4]float64, dy float64) float64 {
		return share(box, func(x, y float64) bool { return dark(x, y) && dark(x+step, y+dy*step) })
	}
	for i, n := range patternNames {
		b := patternBox(i)
		if ink := share(b, dark); ink < 0.03 || ink > 0.7 {
			t.Errorf("%s: %.0f%% of its inside is inked, want a pattern (3%% to 70%%)", n, ink*100)
		}
	}
	for _, tc := range []struct {
		name string
		i    int
		up   bool
	}{{"hatch_up", 0, true}, {"hatch_down", 3, false}, {"hatch_up_fine", 10, true}, {"hatch_down_fine", 9, false}} {
		b := patternBox(tc.i)
		rising, falling := slant(b, -1), slant(b, 1)
		if (rising > falling) != tc.up {
			t.Errorf("%s: rising %.3f falling %.3f, want it to %s", tc.name, rising, falling, map[bool]string{true: "rise", false: "fall"}[tc.up])
		}
	}
	clearBg := [4]float64{36, 340, 90, 60}
	if s := share(clearBg, red); s < 0.3 {
		t.Errorf("clear background: %.0f%% red, want the red rect to show between the strokes", s*100)
	}
	whiteBg := [4]float64{144, 340, 90, 60}
	if s := share(whiteBg, red); s > 0.01 {
		t.Errorf("white background: %.0f%% red, want it hidden", s*100)
	}
	if s := share(whiteBg, white); s < 0.3 {
		t.Errorf("white background: %.0f%% white", s*100)
	}
	// near reports whether a dark pixel lies within 2 pt of (x, y).
	near := func(x, y float64) bool {
		for dy := -2.0; dy <= 2; dy += step {
			for dx := -2.0; dx <= 2; dx += step {
				if dark(x+dx, y+dy) {
					return true
				}
			}
		}
		return false
	}
	for _, tc := range []struct {
		name string
		f    func(float64) float64
		xs   []float64
	}{
		{"bell", bellY, []float64{100, 150, 175, 192, 215, 250, 290}},
		{"sine", sineY, []float64{353, 380, 401, 430, 460, 505, 560}},
	} {
		for _, x := range tc.xs {
			if !near(x, tc.f(x)) {
				t.Errorf("%s: nothing drawn at (%v, %.1f)", tc.name, x, tc.f(x))
			}
			if near(x, tc.f(x)-15) && near(x, tc.f(x)+15) {
				t.Errorf("%s: ink 15 pt above and below (%v, %.1f): not a line", tc.name, x, tc.f(x))
			}
		}
	}
	for _, deg := range []float64{10, 33, 45, 60, 80} {
		a := deg * math.Pi / 180
		x, y := 400+80*math.Cos(a), 720-80*math.Sin(a)
		if !near(x, y) {
			t.Errorf("arc: nothing drawn at %v degrees (%.1f, %.1f)", deg, x, y)
		}
		if near(400+70*math.Cos(a), 720-70*math.Sin(a)) {
			t.Errorf("arc: ink 10 pt inside the arc at %v degrees", deg)
		}
	}
	if ink := share([4]float64{120, 700, 60, 50}, dark); ink < 0.03 || ink > 0.7 {
		t.Errorf("trapezoid: %.0f%% of its inside is inked, want a hatch", ink*100)
	}
	if !strings.Contains(stdout.String(), `"saved"`) {
		t.Errorf("render stdout = %q", stdout.String())
	}
}
