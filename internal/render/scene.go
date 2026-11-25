package render

import "github.com/jhnoel/documentdriver/internal/geom"

// Scene is a page as plain drawing items, in points with y down: what
// a format's drawing view draws, written as SVG by SVG and rasterised
// by Rasterise, so the two always draw the same marks. Every item is
// drawn with the most basic means a rasteriser has: filled paths and
// solid strokes of straight and cubic segments, text runs and images;
// dashes, arrowheads, hatches and text decorations are already
// expanded into such items, because common SVG rasterisers (MuPDF,
// behind PyMuPDF, among them) ignore stroke-dasharray, markers,
// clipping and pattern paint.
type Scene struct {
	W, H float64
	// Background is the page colour "#RRGGBB" ("" for none).
	Background string
	// Attrs are data attributes of the root element (name, value),
	// in order.
	Attrs  [][2]string
	Groups []Group
}

// Group is one object's items, painted in order.
type Group struct {
	ID    string
	Attrs [][2]string
	Items []Item
}

// ItemKind is what an item draws.
type ItemKind uint8

// Item kinds: a filled path, a stroked path, a line of text and an
// image.
const (
	FillItem ItemKind = iota + 1
	StrokeItem
	TextItem
	ImageItem
)

// Item is one drawing mark.
type Item struct {
	Kind ItemKind
	// Path is the outline to fill (non-zero winding) or the lines to
	// stroke; a stroke has butt caps and round joins.
	Path Path
	// Color is "#RRGGBB"; Opacity in (0, 1] (0 is read as 1).
	Color   string
	Opacity float64
	// Width is a stroke's width in points.
	Width float64
	// Attrs are data attributes of the item's element.
	Attrs [][2]string
	Text  *TextLine
	Image *Image
}

// Path is a sequence of segments; each subpath starts with a move.
type Path []Seg

// Seg is one segment: Op 'M' (move to P[0]), 'L' (line to P[0]), 'C'
// (cubic Bézier through P[0], P[1] to P[2]) or 'Z' (close the
// subpath).
type Seg struct {
	Op byte
	P  [3]geom.Pt
}

// TextLine is one line of text: Frame maps its space (points, y down)
// to the page; X and Y are the anchor and the baseline in that space,
// Anchor "start", "middle" or "end"; Spans are its runs in order.
type TextLine struct {
	Frame  geom.Affine
	X, Y   float64
	Anchor string
	Spans  []Span
}

// Span is a run of text in one style: Family is the CSS font family
// list, Size in points, Color "#RRGGBB".
type Span struct {
	Text         string
	Family       string
	Size         float64
	Color        string
	Bold, Italic bool
}

// Image is a picture: Frame maps the unit square (0..1, y down) to
// the page; Data the encoded image of media type Type; Crop the part
// of the image shown, as fractions [x, y, w, h] of it.
type Image struct {
	Frame geom.Affine
	Type  string
	Data  []byte
	Crop  [4]float64
}

// MoveTo appends a move.
func (p Path) MoveTo(pt geom.Pt) Path { return append(p, Seg{Op: 'M', P: [3]geom.Pt{pt}}) }

// LineTo appends a line.
func (p Path) LineTo(pt geom.Pt) Path { return append(p, Seg{Op: 'L', P: [3]geom.Pt{pt}}) }

// CubicTo appends a cubic Bézier.
func (p Path) CubicTo(c1, c2, pt geom.Pt) Path {
	return append(p, Seg{Op: 'C', P: [3]geom.Pt{c1, c2, pt}})
}

// Close appends a close.
func (p Path) Close() Path { return append(p, Seg{Op: 'Z'}) }

// Polygon returns the closed path through pts.
func Polygon(pts ...geom.Pt) Path {
	var p Path
	for i, pt := range pts {
		if i == 0 {
			p = p.MoveTo(pt)
		} else {
			p = p.LineTo(pt)
		}
	}
	return p.Close()
}
