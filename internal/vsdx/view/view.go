// Package view is the drawing view of a vsdx page: the read model's
// objects (built with model.Options.Draw, so their effective geometry
// and text are resolved through masters, styles and the theme) turned
// into a render.Scene, which render.WriteSVG writes as read --format
// svg and render.Rasterise draws as render's PNG without LibreOffice.
// It never reads the file itself: everything comes from the model.
// What it cannot draw, or draws only approximately, is a note.
package view

import (
	"slices"
	"strconv"

	"github.com/jhnoel/documentdriver/internal/geom"
	"github.com/jhnoel/documentdriver/internal/render"
	"github.com/jhnoel/documentdriver/internal/vsdx/doc"
	"github.com/jhnoel/documentdriver/internal/vsdx/model"
	"github.com/jhnoel/documentdriver/internal/vsdx/vsd"
)

// Media returns the bytes and content type of a media part, false
// when it cannot be read.
type Media func(part string) (data []byte, typ string, ok bool)

// Options are the view's inputs beyond the page.
type Options struct {
	// Media reads a picture's image part; nil draws no pictures.
	Media Media
}

// Limits lists what the view never draws or draws only approximately,
// whatever the page holds; help and the docs show it.
var Limits = []string{
	"text is wrapped and drawn with the embedded Carlito, Liberation Sans and Liberation Serif (metric-compatible with Calibri, Arial and Times New Roman); other faces are drawn as Arial, so a line may break a word early or late; a character those lack comes from DejaVu Sans, and one no embedded font has is left out with a note; glyphs are unhinted and not kerned",
	"dashes follow LibreOffice's LinePattern table, multiples of the line weight",
	"arrowheads are drawn at LibreOffice's size for the line weight; BeginArrowSize and EndArrowSize are not applied; only triangle (4) and stealth (5) have their own shape, other arrows are drawn as triangles",
	"patterns 9 to 18, which the contract does not name, are drawn as dots",
	"PolylineTo, NURBSTo and spline rows are drawn as straight lines to their end points",
	"a gradient fill (FillPattern 25 to 40) is drawn solid in its foreground colour; fill and line transparency, rounded corners, line caps and joins are not applied",
	"a fill, line or text value read cannot derive (a Themed cell) is not drawn, with a note; text of no derived size is 12 pt, black",
	"text: tabs, indents, paragraph spacing, superscripts and the text block's own flips are not applied; a field shows its cached text",
	"pictures other than PNG, JPEG and GIF are embedded in the SVG but not drawn in the PNG",
}

// view collects one page's scene and notes.
type view struct {
	o     Options
	scene *render.Scene
	notes []doc.Note
	cur   *render.Group
	at    string
}

// Build returns the scene of page p (built with model.Options.Draw)
// and the notes on what it leaves out: the page's own read notes and
// the drawing's.
func Build(p *model.Page, o Options) (*render.Scene, []doc.Note) {
	v := &view{o: o, scene: &render.Scene{
		W:          p.W * vsd.PointsPerInch,
		H:          p.H * vsd.PointsPerInch,
		Background: "#FFFFFF",
	}}
	v.notes = append(v.notes, p.Notes...)
	visible := map[string]bool{}
	for _, l := range p.Layers {
		visible[l.ID] = l.Visible
	}
	for _, ob := range p.Objects {
		if len(ob.Layers) > 0 && !slices.ContainsFunc(ob.Layers, func(id string) bool { return visible[id] }) {
			continue
		}
		v.object(ob)
	}
	return v.scene, v.notes
}

// note records a note on the current object.
func (v *view) note(reason string) {
	v.notes = append(v.notes, doc.Note{At: v.at, Reason: reason})
}

// add appends an item to the current object's group.
func (v *view) add(it render.Item) { v.cur.Items = append(v.cur.Items, it) }

// object draws one object into its own group.
func (v *view) object(ob *model.Object) {
	v.at = ob.ID
	g := render.Group{ID: ob.ID, Attrs: [][2]string{{"data-kind", string(ob.Kind)}}}
	if ob.Master != "" {
		g.Attrs = append(g.Attrs, [2]string{"data-master", ob.Master})
	}
	v.scene.Groups = append(v.scene.Groups, g)
	v.cur = &v.scene.Groups[len(v.scene.Groups)-1]
	d := ob.Draw
	if d == nil {
		v.note("not drawn: no drawing built")
		return
	}
	for _, n := range d.Notes {
		v.note(n)
	}
	if ob.Kind == model.Other {
		v.note("not drawn: an object of kind other (OLE, ink or a guide)")
		return
	}
	if ob.Kind == model.Picture {
		v.picture(ob)
	} else {
		v.fills(ob, d)
	}
	v.strokes(ob, d)
	if d.Text != nil {
		v.text(d.Text, ob.TextBlock)
	}
}

// pathOf converts a model path to a render path, closing it when it
// is closed.
func pathOf(p model.DrawPath) render.Path {
	out := make(render.Path, 0, len(p.Segs)+1)
	for _, s := range p.Segs {
		out = append(out, render.Seg{Op: s.Op, P: s.P})
	}
	if p.Closed {
		out = out.Close()
	}
	return out
}

// fills draws the shadow, background and fill of the object's closed
// paths that are not NoFill.
func (v *view) fills(ob *model.Object, d *model.Drawing) {
	var area render.Path
	for _, p := range d.Paths {
		if p.Closed && !p.NoFill {
			area = append(area, pathOf(p)...)
		}
	}
	if len(area) == 0 {
		return
	}
	f := ob.Fill
	if f == nil {
		v.note("fill not derived: not drawn")
		return
	}
	if f.None {
		return
	}
	if sh := ob.Shadow; sh != nil && sh.HasColor && sh.HasOffset {
		op := 1.0
		if sh.HasTrans {
			op = 1 - sh.Transparency/100
		}
		if op > 0 {
			v.add(render.Item{Kind: render.FillItem, Path: offset(area, sh.Offset), Color: sh.Color, Opacity: op, Attrs: [][2]string{{"data-role", "shadow"}}})
		}
	}
	if f.Pattern == "" {
		if f.Hex == "" {
			v.note("fill colour not derived: not drawn")
			return
		}
		v.add(render.Item{Kind: render.FillItem, Path: area, Color: f.Hex, Attrs: [][2]string{{"data-role", "fill"}}})
		return
	}
	switch {
	case f.BgHex != "":
		v.add(render.Item{Kind: render.FillItem, Path: area, Color: f.BgHex, Attrs: [][2]string{{"data-role", "pattern-bg"}}})
	case !f.BgNone:
		v.note("pattern background not derived: drawn clear")
	}
	fg := f.Hex
	if fg == "" {
		fg = "#000000"
		v.note("pattern colour not derived: drawn black")
	}
	v.pattern(f.Pattern, area, fg)
}

// offset returns p moved by d.
func offset(p render.Path, d [2]float64) render.Path {
	out := make(render.Path, len(p))
	for i, s := range p {
		out[i].Op = s.Op
		for j := range s.P {
			out[i].P[j] = geom.Pt{X: s.P[j].X + d[0], Y: s.P[j].Y + d[1]}
		}
	}
	return out
}

// hatchSpec is how a FillPattern is drawn: hatch line angles (degrees
// clockwise from x, y down) at spacing points, or dots at spacing.
type hatchSpec struct {
	angles  []float64
	spacing float64
	dots    bool
}

// coarse and fine are LibreOffice's hatch spacings for Visio's
// patterns (libvisio): 0.1 in for 2 to 7, 0.05 in for the rest.
const (
	coarse     = 7.2
	fine       = 3.6
	hatchWidth = 0.5
	dotSize    = 1.0
)

// patternSpecs maps the FillPattern numbers to their drawing, by the
// contract's names (docs/contract.md §3, Pattern fills): hatch_up
// rises to the right, hatch_down falls, grid is horizontal and
// vertical, cross_hatch both diagonals; the _fine siblings at the
// fine spacing.
var patternSpecs = map[int]hatchSpec{
	2:  {angles: []float64{-45}, spacing: coarse},
	3:  {angles: []float64{0, 90}, spacing: coarse},
	4:  {angles: []float64{-45, 45}, spacing: coarse},
	5:  {angles: []float64{45}, spacing: coarse},
	6:  {angles: []float64{0}, spacing: coarse},
	7:  {angles: []float64{90}, spacing: coarse},
	8:  {dots: true, spacing: fine},
	19: {angles: []float64{0}, spacing: fine},
	20: {angles: []float64{90}, spacing: fine},
	21: {angles: []float64{45}, spacing: fine},
	22: {angles: []float64{-45}, spacing: fine},
	23: {angles: []float64{0, 90}, spacing: fine},
	24: {angles: []float64{-45, 45}, spacing: fine},
}

// pattern draws a pattern fill's foreground over area: hatch lines or
// dots computed and clipped here, as plain strokes and fills.
func (v *view) pattern(name string, area render.Path, fg string) {
	n := 0
	if ix, ok := vsd.FillPatternIndex[name]; ok {
		n, _ = strconv.Atoi(ix)
	} else if k, err := strconv.Atoi(name); err == nil {
		n = k
	}
	spec, ok := patternSpecs[n]
	if !ok {
		if n < vsd.FillPatternMin || n > vsd.FillPatternMax {
			v.note("fill pattern " + name + " not drawn")
			return
		}
		spec = hatchSpec{dots: true, spacing: fine * (1 + float64(n-9)/4)}
		v.note("fill pattern " + name + " drawn as dots (approximate)")
	}
	polys := render.Flatten(area, 0.1)
	for i := range polys {
		polys[i].Closed = true
	}
	attrs := [][2]string{{"data-role", "pattern"}, {"data-pattern", name}}
	if spec.dots {
		p, cut := render.Dots(polys, spec.spacing, dotSize)
		if cut {
			v.note("pattern dots cut at the bound")
		}
		v.add(render.Item{Kind: render.FillItem, Path: p, Color: fg, Attrs: attrs})
		return
	}
	var all render.Path
	for _, a := range spec.angles {
		p, cut := render.Hatch(polys, a, spec.spacing)
		if cut {
			v.note("pattern lines cut at the bound")
		}
		all = append(all, p...)
	}
	v.add(render.Item{Kind: render.StrokeItem, Path: all, Color: fg, Width: hatchWidth, Attrs: attrs})
}
