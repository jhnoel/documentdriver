package render

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	_ "image/gif"  // decode GIF pictures
	_ "image/jpeg" // decode JPEG pictures
	_ "image/png"  // decode PNG pictures
	"math"
	"slices"
	"strings"
	"unicode"

	"github.com/jhnoel/documentdriver/internal/geom"
	"github.com/jhnoel/documentdriver/internal/render/ttf"
)

// MaxRasterPixels bounds the pixels Rasterise allocates (about 160 MB
// of RGBA, and at most as much again for one fill's coverage); a
// larger page at the scale asked is refused. A letter page fits at
// scale 9.
const MaxRasterPixels = 40_000_000

// maxImagePixels bounds the pixels of one embedded image Rasterise
// decodes; a larger image is drawn as its outline, with a note.
const maxImagePixels = 50_000_000

// Rasterise draws the scene at scale pixels per point into a new image,
// the pure-Go counterpart of WriteSVG: the same items, anti-aliased
// with non-zero fills, butt-capped round-joined strokes, images
// resampled from their decoded pixels (PNG, JPEG and GIF), and text
// filled from the outlines of the embedded fonts (package fonts: the
// metric-compatible open faces of Calibri, Arial and Times, and DejaVu
// Sans for what they lack), unhinted. Notes say what it could not
// draw.
func Rasterise(s *Scene, scale float64) (*image.RGBA, []string, error) {
	if !(scale > 0) || math.IsInf(scale, 0) {
		return nil, nil, fmt.Errorf("render: scale %v is not positive", scale)
	}
	pw, ph := math.Ceil(s.W*scale-1e-9), math.Ceil(s.H*scale-1e-9)
	if !(pw >= 1) || !(ph >= 1) || pw*ph > MaxRasterPixels {
		return nil, nil, fmt.Errorf("render: a %.0f by %.0f pixel picture is out of bounds (at most %d pixels)", pw, ph, MaxRasterPixels)
	}
	img := image.NewRGBA(image.Rect(0, 0, int(pw), int(ph)))
	c := &canvas{img: img, scale: scale}
	if s.Background != "" {
		bg := parseHex(s.Background)
		for i := 0; i < len(img.Pix); i += 4 {
			img.Pix[i], img.Pix[i+1], img.Pix[i+2], img.Pix[i+3] = bg.R, bg.G, bg.B, 255
		}
	}
	var notes []string
	for gi := range s.Groups {
		g := &s.Groups[gi]
		for ii := range g.Items {
			if n := c.item(&g.Items[ii]); n != "" {
				notes = append(notes, g.ID+": "+n)
			}
		}
	}
	return img, notes, nil
}

// canvas is an image being drawn at scale pixels per point.
type canvas struct {
	img    *image.RGBA
	scale  float64
	acc    []float32
	glyphs map[glyphKey]Path
}

// parseHex parses "#RRGGBB", black when it does not parse.
func parseHex(s string) color.RGBA {
	if len(s) != 7 || s[0] != '#' {
		return color.RGBA{A: 255}
	}
	var v [3]uint8
	for i := range 3 {
		hi, ok1 := hexDigit(s[1+2*i])
		lo, ok2 := hexDigit(s[2+2*i])
		if !ok1 || !ok2 {
			return color.RGBA{A: 255}
		}
		v[i] = hi<<4 | lo
	}
	return color.RGBA{R: v[0], G: v[1], B: v[2], A: 255}
}

// hexDigit returns the value of a hexadecimal digit.
func hexDigit(b byte) (uint8, bool) {
	switch {
	case b >= '0' && b <= '9':
		return b - '0', true
	case b >= 'a' && b <= 'f':
		return b - 'a' + 10, true
	case b >= 'A' && b <= 'F':
		return b - 'A' + 10, true
	}
	return 0, false
}

// item draws one item, returning a note when it could not.
func (c *canvas) item(it *Item) string {
	op := it.Opacity
	if !(op > 0) || op > 1 {
		op = 1
	}
	switch it.Kind {
	case FillItem:
		polys := Flatten(c.toPx(it.Path), 0.2)
		for i := range polys {
			polys[i].Closed = true
		}
		c.fill(polys, parseHex(it.Color), op)
	case StrokeItem:
		w := it.Width * c.scale
		if !(w > 0) {
			return ""
		}
		c.fill(strokePolys(Flatten(c.toPx(it.Path), 0.2), w), parseHex(it.Color), op)
	case TextItem:
		return c.text(it.Text)
	case ImageItem:
		return c.image(it.Image)
	}
	return ""
}

// toPx maps a path from points to pixels.
func (c *canvas) toPx(p Path) Path {
	out := make(Path, len(p))
	for i, s := range p {
		out[i].Op = s.Op
		for j := range s.P {
			out[i].P[j] = geom.Pt{X: s.P[j].X * c.scale, Y: s.P[j].Y * c.scale}
		}
	}
	return out
}

// strokePolys returns the outline of a stroke of width w (pixels)
// along the polylines: one quadrilateral per segment (butt caps) and a
// disc at every inner vertex (round joins), all wound the same way so
// that their overlaps add up rather than cancel.
func strokePolys(polys []Poly, w float64) []Poly {
	var out []Poly
	h := w / 2
	n := max(int(math.Ceil(w*1.5)), 8)
	n = min(n, 64)
	for _, pl := range polys {
		pts := pl.Pts
		if pl.Closed && len(pts) > 0 {
			pts = append(pts[:len(pts):len(pts)], pts[0])
		}
		for i := 1; i < len(pts); i++ {
			a, b := pts[i-1], pts[i]
			dx, dy := b.X-a.X, b.Y-a.Y
			l := math.Hypot(dx, dy)
			if !(l > 1e-9) {
				continue
			}
			nx, ny := -dy/l*h, dx/l*h
			out = append(out, oriented(Poly{Pts: []geom.Pt{
				{X: a.X + nx, Y: a.Y + ny}, {X: b.X + nx, Y: b.Y + ny},
				{X: b.X - nx, Y: b.Y - ny}, {X: a.X - nx, Y: a.Y - ny},
			}, Closed: true}))
			if i < len(pts)-1 || pl.Closed {
				disc := make([]geom.Pt, n)
				for k := range n {
					s, c := math.Sincos(2 * math.Pi * float64(k) / float64(n))
					disc[k] = geom.Pt{X: b.X + h*c, Y: b.Y + h*s}
				}
				out = append(out, oriented(Poly{Pts: disc, Closed: true}))
			}
		}
	}
	return out
}

// oriented returns p wound with positive signed area.
func oriented(p Poly) Poly {
	a := 0.0
	for i := range p.Pts {
		q, r := p.Pts[i], p.Pts[(i+1)%len(p.Pts)]
		a += q.X*r.Y - r.X*q.Y
	}
	if a < 0 {
		for i, j := 0, len(p.Pts)-1; i < j; i, j = i+1, j-1 {
			p.Pts[i], p.Pts[j] = p.Pts[j], p.Pts[i]
		}
	}
	return p
}

// fill paints the area of polys (pixels, every one closed; non-zero
// winding, coverage clamped to one) in col at opacity op, with
// anti-aliased edges from signed-area accumulation.
func (c *canvas) fill(polys []Poly, col color.RGBA, op float64) {
	b := c.img.Bounds()
	x0, y0, x1, y1 := math.Inf(1), math.Inf(1), math.Inf(-1), math.Inf(-1)
	for _, pl := range polys {
		for _, p := range pl.Pts {
			if math.IsNaN(p.X) || math.IsNaN(p.Y) {
				return
			}
			x0, y0 = math.Min(x0, p.X), math.Min(y0, p.Y)
			x1, y1 = math.Max(x1, p.X), math.Max(y1, p.Y)
		}
	}
	bx0 := int(math.Max(math.Floor(x0), 0))
	by0 := int(math.Max(math.Floor(y0), 0))
	bx1 := int(math.Min(math.Ceil(x1), float64(b.Dx())))
	by1 := int(math.Min(math.Ceil(y1), float64(b.Dy())))
	if bx1 <= bx0 || by1 <= by0 {
		return
	}
	w, h := bx1-bx0+2, by1-by0
	if cap(c.acc) < w*h {
		c.acc = make([]float32, w*h)
	}
	acc := c.acc[:w*h]
	clear(acc)
	ox, oy := float64(bx0), float64(by0)
	for _, pl := range polys {
		m := len(pl.Pts)
		for i := range m {
			a, e := pl.Pts[i], pl.Pts[(i+1)%m]
			accLine(acc, w, h, a.X-ox, a.Y-oy, e.X-ox, e.Y-oy)
		}
	}
	for y := range h {
		sum := float32(0)
		row := acc[y*w : y*w+w]
		pix := c.img.Pix[(by0+y)*c.img.Stride:]
		for x := range bx1 - bx0 {
			sum += row[x]
			cov := float64(sum)
			if cov < 0 {
				cov = -cov
			}
			if cov > 1 {
				cov = 1
			}
			if cov < 1.0/512 {
				continue
			}
			blend(pix[(bx0+x)*4:], col, cov*op)
		}
	}
}

// blend composites col at alpha a over the pixel p (RGBA bytes).
func blend(p []uint8, col color.RGBA, a float64) {
	ia := 1 - a
	p[0] = uint8(float64(col.R)*a + float64(p[0])*ia + 0.5)
	p[1] = uint8(float64(col.G)*a + float64(p[1])*ia + 0.5)
	p[2] = uint8(float64(col.B)*a + float64(p[2])*ia + 0.5)
	p[3] = uint8(255*a + float64(p[3])*ia + 0.5)
}

// accLine accumulates the signed area one edge from (x0, y0) to
// (x1, y1) adds to each cell of a w by h grid (the font-rs method: a
// prefix sum along each row gives the winding coverage). x is clamped
// to the grid, which keeps the winding of the rows it crosses; rows
// outside the grid are skipped.
func accLine(acc []float32, w, h int, x0, y0, x1, y1 float64) {
	if y0 == y1 {
		return
	}
	dir := 1.0
	if y0 > y1 {
		dir = -1
		x0, y0, x1, y1 = x1, y1, x0, y0
	}
	clampX := func(x float64) float64 { return math.Min(math.Max(x, 0), float64(w-2)) }
	dxdy := (x1 - x0) / (y1 - y0)
	ys := math.Max(y0, 0)
	ye := math.Min(y1, float64(h))
	if ys >= ye {
		return
	}
	x := x0 + dxdy*(ys-y0)
	for yi := int(math.Floor(ys)); yi < h && float64(yi) < ye; yi++ {
		top := math.Max(float64(yi), ys)
		bot := math.Min(float64(yi+1), ye)
		dy := bot - top
		if dy <= 0 {
			continue
		}
		xn := x + dxdy*dy
		d := float32(dy * dir)
		xa, xb := clampX(x), clampX(xn)
		if xa > xb {
			xa, xb = xb, xa
		}
		line := acc[yi*w : yi*w+w]
		x0f := math.Floor(xa)
		x0i := int(x0f)
		x1i := int(math.Ceil(xb))
		if x1i <= x0i+1 {
			xm := float32(0.5*(xa+xb) - x0f)
			line[x0i] += d - d*xm
			line[x0i+1] += d * xm
		} else {
			s := 1 / (xb - xa)
			fa := xa - x0f
			a0 := 0.5 * s * (1 - fa) * (1 - fa)
			fb := xb - float64(x1i) + 1
			am := 0.5 * s * fb * fb
			line[x0i] += d * float32(a0)
			if x1i == x0i+2 {
				line[x0i+1] += d * float32(1-a0-am)
			} else {
				a1 := s * (1.5 - fa)
				line[x0i+1] += d * float32(a1-a0)
				for xi := x0i + 2; xi < x1i-1; xi++ {
					line[xi] += d * float32(s)
				}
				a2 := a1 + float64(x1i-x0i-3)*s
				line[x1i-1] += d * float32(1-a2-am)
			}
			line[x1i] += d * float32(am)
		}
		x = xn
	}
}

// glyphKey names one glyph of one embedded font.
type glyphKey struct {
	font  *ttf.Font
	glyph int
}

// text fills a text line's glyph outlines (glyphFor: each run's face,
// weight and slant, falling back by coverage), each at its advance,
// and returns a note naming the characters no embedded font has,
// which are left out.
func (c *canvas) text(t *TextLine) string {
	if t == nil {
		return ""
	}
	starts := spanStarts(t)
	toPx := t.Frame.Then(geom.Scale(c.scale, c.scale))
	var missing []rune
	for si, sp := range t.Spans {
		x := starts[si]
		col := parseHex(sp.Color)
		class := faceClass(sp.Family)
		var polys []Poly
		for _, r := range sp.Text {
			if r == '\t' {
				x += RuneWidth(sp.Family, sp.Bold, sp.Italic, r) * sp.Size
				continue
			}
			f, g := glyphFor(class, sp.Bold, sp.Italic, r)
			if f == nil {
				if !unicode.IsSpace(r) && !slices.Contains(missing, r) && len(missing) < 16 {
					missing = append(missing, r)
				}
				continue
			}
			k := 1 / float64(f.UnitsPerEm()) * sp.Size
			m := geom.Affine{A: k, D: -k, E: x, F: t.Y}.Then(toPx)
			polys = append(polys, c.glyph(glyphKey{f, g}, m)...)
			x += float64(f.Advance(g)) * k
		}
		c.fill(polys, col, 1)
	}
	if len(missing) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("text: no embedded font has")
	for _, r := range missing {
		fmt.Fprintf(&b, " U+%04X", r)
	}
	b.WriteString(" (left out)")
	return b.String()
}

// glyph returns the outline of glyph k placed by m (design units to
// pixels) as flattened polygons, decoding each glyph once per canvas.
func (c *canvas) glyph(k glyphKey, m geom.Affine) []Poly {
	if c.glyphs == nil {
		c.glyphs = map[glyphKey]Path{}
	}
	p, ok := c.glyphs[k]
	if !ok {
		contours, err := k.font.Outline(k.glyph)
		if err == nil {
			p = contourPath(contours)
		}
		c.glyphs[k] = p
	}
	placed := make(Path, len(p))
	for i, s := range p {
		placed[i] = Seg{Op: s.Op, P: [3]geom.Pt{m.Apply(s.P[0]), m.Apply(s.P[1]), m.Apply(s.P[2])}}
	}
	return Flatten(placed, 0.1)
}

// contourPath converts TrueType contours (quadratic, with implied
// on-curve points between two control points) to a closed path in
// design units, each quadratic as the equal cubic.
func contourPath(cs []ttf.Contour) Path {
	var p Path
	for _, c := range cs {
		n := len(c)
		if n == 0 {
			continue
		}
		pt := func(q ttf.Point) geom.Pt { return geom.Pt{X: q.X, Y: q.Y} }
		mid := func(a, b ttf.Point) ttf.Point { return ttf.Point{X: (a.X + b.X) / 2, Y: (a.Y + b.Y) / 2, On: true} }
		// Start at an on-curve point (or the midpoint of the first two
		// control points).
		first := -1
		for i, q := range c {
			if q.On {
				first = i
				break
			}
		}
		var start ttf.Point
		if first >= 0 {
			start = c[first]
		} else {
			first, start = 0, mid(c[0], c[1%n])
		}
		p = p.MoveTo(pt(start))
		cur := start
		var ctrl *ttf.Point
		for i := 1; i <= n; i++ {
			q := c[(first+i)%n]
			if i == n && first < 0 {
				q = start
			}
			switch {
			case q.On && ctrl == nil:
				p = p.LineTo(pt(q))
				cur = q
			case q.On:
				p = quadTo(p, cur, *ctrl, q)
				cur, ctrl = q, nil
			case ctrl == nil:
				cc := q
				ctrl = &cc
			default:
				m := mid(*ctrl, q)
				p = quadTo(p, cur, *ctrl, m)
				cur = m
				cc := q
				ctrl = &cc
			}
		}
		if ctrl != nil {
			p = quadTo(p, cur, *ctrl, start)
		}
		p = append(p, Seg{Op: 'Z'})
	}
	return p
}

// quadTo appends the quadratic from a through control q to b, as the
// equal cubic.
func quadTo(p Path, a, q, b ttf.Point) Path {
	c1 := geom.Pt{X: a.X + 2*(q.X-a.X)/3, Y: a.Y + 2*(q.Y-a.Y)/3}
	c2 := geom.Pt{X: b.X + 2*(q.X-b.X)/3, Y: b.Y + 2*(q.Y-b.Y)/3}
	return append(p, Seg{Op: 'C', P: [3]geom.Pt{c1, c2, {X: b.X, Y: b.Y}}})
}

// image draws an image item by sampling the decoded pixels at each
// covered pixel's centre (nearest neighbour), returning a note when
// it cannot decode it.
func (c *canvas) image(im *Image) string {
	if im == nil {
		return ""
	}
	cfg, _, err := image.DecodeConfig(bytes.NewReader(im.Data))
	if err != nil {
		return "picture of type " + im.Type + " not drawn (not PNG, JPEG or GIF)"
	}
	if cfg.Width <= 0 || cfg.Height <= 0 || cfg.Width*cfg.Height > maxImagePixels {
		return "picture too large to draw"
	}
	src, _, err := image.Decode(bytes.NewReader(im.Data))
	if err != nil {
		return "picture not drawn: " + err.Error()
	}
	toPx := im.Frame.Then(geom.Scale(c.scale, c.scale))
	inv, ok := toPx.Invert()
	if !ok {
		return ""
	}
	crop := im.Crop
	if crop[2] <= 0 || crop[3] <= 0 {
		crop = [4]float64{0, 0, 1, 1}
	}
	sb := src.Bounds()
	b := c.img.Bounds()
	x0, y0, x1, y1 := math.Inf(1), math.Inf(1), math.Inf(-1), math.Inf(-1)
	for _, u := range [4]geom.Pt{{}, {X: 1}, {X: 1, Y: 1}, {Y: 1}} {
		p := toPx.Apply(u)
		x0, y0 = math.Min(x0, p.X), math.Min(y0, p.Y)
		x1, y1 = math.Max(x1, p.X), math.Max(y1, p.Y)
	}
	for py := max(int(math.Floor(y0)), b.Min.Y); py < min(int(math.Ceil(y1)), b.Max.Y); py++ {
		for px := max(int(math.Floor(x0)), b.Min.X); px < min(int(math.Ceil(x1)), b.Max.X); px++ {
			u := inv.Apply(geom.Pt{X: float64(px) + 0.5, Y: float64(py) + 0.5})
			if u.X < 0 || u.X >= 1 || u.Y < 0 || u.Y >= 1 {
				continue
			}
			sx := sb.Min.X + int((crop[0]+u.X*crop[2])*float64(sb.Dx()))
			sy := sb.Min.Y + int((crop[1]+u.Y*crop[3])*float64(sb.Dy()))
			if sx < sb.Min.X || sx >= sb.Max.X || sy < sb.Min.Y || sy >= sb.Max.Y {
				continue
			}
			n, ok := color.NRGBAModel.Convert(src.At(sx, sy)).(color.NRGBA)
			if !ok || n.A == 0 {
				continue
			}
			blend(c.img.Pix[py*c.img.Stride+px*4:], color.RGBA{R: n.R, G: n.G, B: n.B, A: 255}, float64(n.A)/255)
		}
	}
	return ""
}
