package render

import (
	"image"
	"image/color"
	"math"
	"strconv"
)

// Mark is one object to annotate. All coordinates are in points in the
// rendered page's coordinate system (origin at its top-left, y down), after
// any group transform has been applied by the caller.
type Mark struct {
	ID    string       // label shown by the ids overlay
	Box   [4]float64   // x, y, w, h of the unrotated box
	Path  [][2]float64 // if set, the ids overlay draws this polyline (a connector) instead of the box
	Rot   float64      // clockwise rotation in degrees about the box centre
	Sites [][2]float64 // connection sites; the printed number is the slice index
	Ends  [][2]float64 // glued connector ends
}

// Overlay selects what Draw paints.
type Overlay struct {
	IDs   bool // box outlines and an id tag at each box's top-left
	Sites bool // a dot and index number at each connection site
	Glue  bool // a ring around each glued connector end
}

// KeyEntry maps a number drawn on the image to an object id. Draw returns
// a key when the page is too dense for id text.
type KeyEntry struct {
	N  int
	ID string
}

// denseThreshold is the object count above which tags show numbers.
const denseThreshold = 24

var (
	colBox   = color.RGBA{230, 0, 200, 255}
	colTagBG = color.RGBA{255, 235, 0, 255}
	colTagFG = color.RGBA{0, 0, 0, 255}
	colSite  = color.RGBA{0, 110, 255, 255}
	colWhite = color.RGBA{255, 255, 255, 255}
	colGlue  = color.RGBA{255, 90, 0, 255}
)

// Draw paints the overlays onto img, which must show the whole page of
// width by height points. The point-to-pixel factor is taken from the
// image size, so it holds whatever the rasteriser rounded to. It returns
// the number-to-id key when ids were drawn as numbers, else nil.
func Draw(img *image.RGBA, width, height float64, marks []Mark, ov Overlay) []KeyEntry {
	b := img.Bounds()
	sx := float64(b.Dx()) / width
	sy := float64(b.Dy()) / height
	px := func(x, y float64) (float64, float64) { return float64(b.Min.X) + x*sx, float64(b.Min.Y) + y*sy }
	var key []KeyEntry
	if ov.IDs {
		key = drawIDs(img, px, sx, marks)
	}
	if ov.Sites {
		for _, m := range marks {
			for i, s := range m.Sites {
				x, y := px(s[0], s[1])
				disc(img, x, y, 4, colWhite)
				disc(img, x, y, 3, colSite)
				drawTag(img, int(x)+5, int(y)-16, strconv.Itoa(i), colSite, colWhite)
			}
		}
	}
	if ov.Glue {
		for _, m := range marks {
			for _, e := range m.Ends {
				x, y := px(e[0], e[1])
				annulus(img, x, y, 5.5, 9, colWhite)
				annulus(img, x, y, 6.5, 8, colGlue)
			}
		}
	}
	return key
}

// drawIDs draws shape outlines and ID tags for marks, returning the key entries.
func drawIDs(img *image.RGBA, px func(x, y float64) (float64, float64), _ float64, marks []Mark) []KeyEntry {
	type tag struct {
		x, y int
		id   string
	}
	var tags []tag
	for _, m := range marks {
		if len(m.Path) > 0 {
			for i := 1; i < len(m.Path); i++ {
				x0, y0 := px(m.Path[i-1][0], m.Path[i-1][1])
				x1, y1 := px(m.Path[i][0], m.Path[i][1])
				line(img, x0, y0, x1, y1, colBox)
			}
			tx, ty := px(m.Path[0][0], m.Path[0][1])
			tags = append(tags, tag{int(tx), int(ty), m.ID})
			continue
		}
		x, y, w, h := m.Box[0], m.Box[1], m.Box[2], m.Box[3]
		cx, cy := x+w/2, y+h/2
		corners := [4][2]float64{{x, y}, {x + w, y}, {x + w, y + h}, {x, y + h}}
		rad := m.Rot * math.Pi / 180
		sin, cos := math.Sincos(rad)
		var pts [4][2]float64
		for i, c := range corners {
			dx, dy := c[0]-cx, c[1]-cy
			pts[i][0], pts[i][1] = px(cx+dx*cos-dy*sin, cy+dx*sin+dy*cos)
		}
		for i := range pts {
			j := (i + 1) % 4
			line(img, pts[i][0], pts[i][1], pts[j][0], pts[j][1], colBox)
		}
		tx, ty := px(x, y) // top-left of the unrotated box; the rotated one for non-zero Rot
		if m.Rot != 0 {
			tx, ty = pts[0][0], pts[0][1]
		}
		tags = append(tags, tag{int(tx), int(ty), m.ID})
	}
	// Decide between id text and numbers: numbers when dense or when two
	// id tags would overlap.
	numbered := len(tags) > denseThreshold
	if !numbered {
		var rects []image.Rectangle
		for _, t := range tags {
			r := tagRect(t.x, t.y, t.id)
			for _, o := range rects {
				if r.Overlaps(o) {
					numbered = true
				}
			}
			rects = append(rects, r)
		}
	}
	var key []KeyEntry
	for i, t := range tags {
		if numbered {
			n := i + 1
			key = append(key, KeyEntry{N: n, ID: t.id})
			drawTag(img, t.x, t.y, strconv.Itoa(n), colTagBG, colTagFG)
		} else {
			drawTag(img, t.x, t.y, t.id, colTagBG, colTagFG)
		}
	}
	return key
}

// tagRect returns the rectangle bounding a tag of text s at x, y.
func tagRect(x, y int, s string) image.Rectangle {
	w := len(s)*fontAdvance + 4
	h := fontHeight + 2
	return image.Rect(x, y, x+w, y+h)
}

// drawTag paints text on a filled box with a 1px dark border, nudged to
// stay inside the image.
func drawTag(img *image.RGBA, x, y int, s string, bg, fg color.RGBA) {
	r := tagRect(x, y, s)
	b := img.Bounds()
	if r.Max.X > b.Max.X {
		r = r.Add(image.Pt(b.Max.X-r.Max.X, 0))
	}
	if r.Max.Y > b.Max.Y {
		r = r.Add(image.Pt(0, b.Max.Y-r.Max.Y))
	}
	if r.Min.X < b.Min.X {
		r = r.Add(image.Pt(b.Min.X-r.Min.X, 0))
	}
	if r.Min.Y < b.Min.Y {
		r = r.Add(image.Pt(0, b.Min.Y-r.Min.Y))
	}
	fill(img, r, colTagFG)
	fill(img, r.Inset(1), bg)
	drawText(img, r.Min.X+2, r.Min.Y+1, s, fg)
}

// fill paints every pixel of rectangle r in the image with colour c.
func fill(img *image.RGBA, r image.Rectangle, c color.RGBA) {
	r = r.Intersect(img.Bounds())
	for y := r.Min.Y; y < r.Max.Y; y++ {
		for x := r.Min.X; x < r.Max.X; x++ {
			img.SetRGBA(x, y, c)
		}
	}
}

// set paints pixel x, y with colour c if it lies inside the image.
func set(img *image.RGBA, x, y int, c color.RGBA) {
	if image.Pt(x, y).In(img.Bounds()) {
		img.SetRGBA(x, y, c)
	}
}

// line draws a straight line from x0, y0 to x1, y1 in colour c.
func line(img *image.RGBA, x0, y0, x1, y1 float64, c color.RGBA) {
	n := int(math.Max(math.Abs(x1-x0), math.Abs(y1-y0))) + 1
	for i := 0; i <= n; i++ {
		t := float64(i) / float64(n)
		set(img, int(math.Round(x0+(x1-x0)*t)), int(math.Round(y0+(y1-y0)*t)), c)
	}
}

// disc paints a filled disc of radius r centred at cx, cy.
func disc(img *image.RGBA, cx, cy, r float64, c color.RGBA) { annulus(img, cx, cy, 0, r, c) }

// annulus paints the pixels whose distance from (cx, cy) is within
// [inner, outer].
func annulus(img *image.RGBA, cx, cy, inner, outer float64, c color.RGBA) {
	for y := int(cy - outer - 1); y <= int(cy+outer+1); y++ {
		for x := int(cx - outer - 1); x <= int(cx+outer+1); x++ {
			d := math.Hypot(float64(x)+0.5-cx, float64(y)+0.5-cy)
			if d <= outer && d >= inner {
				set(img, x, y, c)
			}
		}
	}
}
