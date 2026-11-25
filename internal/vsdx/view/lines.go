package view

import (
	"math"
	"strconv"

	"github.com/jhnoel/documentdriver/internal/geom"
	"github.com/jhnoel/documentdriver/internal/render"
	"github.com/jhnoel/documentdriver/internal/vsdx/model"
	"github.com/jhnoel/documentdriver/internal/vsdx/vsd"
)

// dashPatterns are Visio's LinePattern values 2 to 23 as LibreOffice's
// import draws them (libvisio VSDContentCollector::_lineProperties):
// dash and gap lengths as multiples of the line weight.
var dashPatterns = map[int][]float64{
	2: {6, 3}, 3: {1, 3}, 4: {6, 3, 1, 3}, 5: {6, 3, 1, 3, 1, 3},
	6: {6, 3, 6, 3, 1, 3}, 7: {14, 2, 6, 2}, 8: {14, 2, 6, 2, 6, 2},
	9: {3, 2}, 10: {1, 2}, 11: {3, 2, 1, 2}, 12: {3, 2, 1, 2, 1, 2},
	13: {3, 2, 3, 2, 1, 2}, 14: {7, 2, 3, 2}, 15: {7, 2, 3, 2, 3, 2},
	16: {11, 5}, 17: {1, 5}, 18: {11, 5, 1, 5}, 19: {11, 5, 1, 5, 1, 5},
	20: {11, 5, 11, 5, 1, 5}, 21: {27, 5, 11, 5}, 22: {27, 5, 11, 5, 11, 5},
	23: {2, 2},
}

// defaultWeight is the line weight drawn when none is derived: 0.75
// pt, Visio's default line style weight (0.0104 in).
const defaultWeight = 0.75

// minDashUnit is the smallest weight dash lengths are scaled by, so a
// hairline's dashes stay visible.
const minDashUnit = 0.5

// strokes draws the outline of the object's paths that are not
// NoLine: dashed as its LinePattern, with arrowheads on a connector's
// or an open path's ends.
func (v *view) strokes(ob *model.Object, d *model.Drawing) {
	var polys []render.Poly
	for _, p := range d.Paths {
		if !p.NoLine {
			polys = append(polys, render.Flatten(pathOf(p), 0.1)...)
		}
	}
	if len(polys) == 0 {
		return
	}
	l := ob.Line
	if l == nil {
		v.note("line not derived: not drawn")
		return
	}
	if l.None {
		return
	}
	w := l.W
	if !l.HasW {
		w = defaultWeight
		v.note("line weight not derived: 0.75 pt drawn")
	}
	color := l.Color
	if color == "" {
		color = "#000000"
		v.note("line colour not derived: drawn black")
	}
	var head, tail *arrow
	if ob.Kind == model.Connector || ob.Geom == "polyline" || ob.Geom == "curve" {
		head = v.arrowOf(l.Begin, w)
		tail = v.arrowOf(l.End, w)
	}
	first, last := &polys[0], &polys[len(polys)-1]
	var heads []render.Item
	if head != nil && !first.Closed {
		var it render.Item
		first.Pts, it = head.place(reversed(first.Pts), color)
		first.Pts = reversed(first.Pts)
		heads = append(heads, it)
	}
	if tail != nil && !last.Closed {
		var it render.Item
		last.Pts, it = tail.place(last.Pts, color)
		heads = append(heads, it)
	}
	var pattern []float64
	if dash := l.Dash; dash != "" && dash != "1" {
		n, err := strconv.Atoi(dash)
		if p, ok := dashPatterns[n]; ok && err == nil {
			unit := math.Max(w, minDashUnit)
			for _, x := range p {
				pattern = append(pattern, x*unit)
			}
		} else {
			v.note("line pattern " + dash + " drawn solid")
		}
	}
	attrs := [][2]string{{"data-role", "line"}}
	if l.Dash != "" {
		attrs = append(attrs, [2]string{"data-dash", l.Dash})
	}
	v.add(render.Item{Kind: render.StrokeItem, Path: render.Dash(polys, pattern), Color: color, Width: w, Attrs: attrs})
	for _, it := range heads {
		v.add(it)
	}
}

// reversed returns pts in reverse order, in a new slice.
func reversed(pts []geom.Pt) []geom.Pt {
	out := make([]geom.Pt, len(pts))
	for i, p := range pts {
		out[len(pts)-1-i] = p
	}
	return out
}

// arrow is an arrowhead to draw: its index as stored, the marker
// outline in a 20 by 20 box with the tip at (10, 0) and the base at
// y 20 (libvisio's marker paths for 4 and 5), and its length in
// points.
type arrow struct {
	index  string
	length float64
	shape  []geom.Pt
	// back is how far behind the tip the line stops, as a fraction of
	// the length, so it ends inside the head.
	back float64
}

// arrowOf returns the arrowhead of a line end of weight w, nil for
// none. Its width is LibreOffice's for the weight (libvisio:
// 0.1/(w*w+1) + 2.54w inches for a weight of w inches, at least
// 0.05 in) and its length equals its width, as both markers' boxes
// are square.
func (v *view) arrowOf(e *model.LineEnd, w float64) *arrow {
	if e == nil || e.Arrow == "" || e.Arrow == "none" || e.Arrow == "0" {
		return nil
	}
	win := w / vsd.PointsPerInch
	width := math.Max(0.1/(win*win+1)+2.54*win, 0.05) * vsd.PointsPerInch
	a := &arrow{index: e.Arrow, length: width}
	switch e.Arrow {
	case "stealth":
		// m10 0-10 20q10,-5 20,0z: a quadratic base through (10, 15).
		a.shape = []geom.Pt{{X: 10, Y: 0}, {X: 0, Y: 20}}
		for i := 1; i < 8; i++ {
			t := float64(i) / 8
			u := 1 - t
			a.shape = append(a.shape, geom.Pt{X: u*u*0 + 2*u*t*10 + t*t*20, Y: u*u*20 + 2*u*t*15 + t*t*20})
		}
		a.shape = append(a.shape, geom.Pt{X: 20, Y: 20})
		a.back = 0.75
	default:
		if e.Arrow != "triangle" {
			v.note("arrow " + e.Arrow + " drawn as a triangle")
		}
		a.shape = []geom.Pt{{X: 10, Y: 0}, {X: 0, Y: 20}, {X: 20, Y: 20}}
		a.back = 0.9
	}
	return a
}

// place puts the arrowhead at the end of pts (its last point is the
// tip), aimed along the path over the head's length, and returns the
// path shortened so the line stops inside the head, with the head's
// fill item.
func (a *arrow) place(pts []geom.Pt, color string) ([]geom.Pt, render.Item) {
	tip := pts[len(pts)-1]
	base, _ := pointBack(pts, a.length)
	dx, dy := tip.X-base.X, tip.Y-base.Y
	l := math.Hypot(dx, dy)
	if l < 1e-9 {
		dx, dy, l = 1, 0, 1
	}
	ux, uy := dx/l, dy/l // along the line, toward the tip
	nx, ny := -uy, ux
	s := a.length / 20
	out := make([]geom.Pt, len(a.shape))
	for i, p := range a.shape {
		along := (20 - p.Y) * s // from the base toward the tip
		across := (p.X - 10) * s
		out[i] = geom.Pt{
			X: tip.X - ux*(a.length-along) + nx*across,
			Y: tip.Y - uy*(a.length-along) + ny*across,
		}
	}
	_, trimmed := pointBack(pts, a.length*a.back)
	it := render.Item{Kind: render.FillItem, Path: render.Polygon(out...), Color: color, Attrs: [][2]string{{"data-role", "arrowhead"}, {"data-arrow", a.index}}}
	return trimmed, it
}

// pointBack returns the point d back from the end of pts along it and
// pts cut there (at least two points). A path shorter than d returns
// its start and its first point twice.
func pointBack(pts []geom.Pt, d float64) (geom.Pt, []geom.Pt) {
	left := d
	for i := len(pts) - 1; i > 0; i-- {
		a, b := pts[i-1], pts[i]
		seg := math.Hypot(b.X-a.X, b.Y-a.Y)
		if seg >= left && seg > 0 {
			t := (seg - left) / seg
			p := geom.Pt{X: a.X + (b.X-a.X)*t, Y: a.Y + (b.Y-a.Y)*t}
			cut := append(append([]geom.Pt{}, pts[:i]...), p)
			return p, cut
		}
		left -= seg
	}
	return pts[0], []geom.Pt{pts[0], pts[0]}
}

// picture draws a picture's image in its box, cropped as read shows.
func (v *view) picture(ob *model.Object) {
	if ob.Box == nil {
		v.note("picture not drawn: box not derived")
		return
	}
	pic := ob.Picture
	if pic == nil || v.o.Media == nil {
		v.note("picture not drawn: image part not available")
		return
	}
	data, typ, ok := v.o.Media(pic.Image)
	if !ok {
		v.note("picture not drawn: " + pic.Image + " cannot be read")
		return
	}
	b := *ob.Box
	frame := geom.Scale(b.W, b.H).Then(b.Matrix())
	crop := [4]float64{0, 0, 1, 1}
	if pic.HasCrop {
		c := pic.Crop
		crop = [4]float64{c[0] / 100, c[1] / 100, 1 - (c[0]+c[2])/100, 1 - (c[1]+c[3])/100}
	}
	v.add(render.Item{Kind: render.ImageItem, Image: &render.Image{Frame: frame, Type: typ, Data: data, Crop: crop}, Attrs: [][2]string{{"data-role", "picture"}, {"data-image", pic.Image}}})
}

// text lays out a text block and draws it.
func (v *view) text(tb *model.TextBlock) {
	box := render.TextBox{Frame: tb.Frame, W: tb.W, H: tb.H, Margin: tb.Margin, VAlign: tb.VAlign}
	for _, p := range tb.Paras {
		rp := render.TextPara{Align: p.Align, Bullet: p.Bullet, LineSpacing: p.LineSpacing}
		for _, r := range p.Runs {
			cs := r.Style
			rp.Runs = append(rp.Runs, render.TextRun{Text: r.Text, Br: r.Br, Font: cs.Font, Size: cs.Size, Color: cs.Color,
				Bold: cs.Bold, Italic: cs.Italic, Underline: cs.Underline, Strike: cs.Strike})
		}
		box.Paras = append(box.Paras, rp)
	}
	items, notes := render.LayoutText(box)
	for _, n := range notes {
		v.note(n)
	}
	for _, it := range items {
		it.Attrs = append(it.Attrs, [2]string{"data-role", "text"})
		v.add(it)
	}
}
