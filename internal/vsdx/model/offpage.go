package model

import (
	"github.com/jhnoel/documentdriver/internal/geom"
	"github.com/jhnoel/documentdriver/internal/vsdx/vsd"
)

// offPage adds an off-page hazard for every top-level object of a page
// whose box (or, for a 1-D shape, both ends) lies wholly outside the
// page's [0, 0, W, H] in view points (the contract draft's off-page
// hazard; page.set reports it when it moves the page edge past
// objects, read reports what the file already holds). Detail:
// "box [x,y,w,h] lies outside the page [0,0,W,H]" with geom.FormatNum
// numbers. Pages whose size is not derived, masters, and objects
// without a box or ends add nothing. Chunk OPF.
func offPage(p *Page) {
	if p == nil || !p.HasSize {
		return
	}
	if addr, err := vsd.Parse(p.ID); err == nil && addr.Kind == vsd.KindMaster {
		return
	}
	pageW := p.W * vsd.PointsPerInch
	pageH := p.H * vsd.PointsPerInch
	if !(pageW > 0) || !(pageH > 0) {
		return
	}
	for _, o := range p.Objects {
		if o == nil || o.Parent != "" {
			continue
		}
		if o.From != nil && o.To != nil {
			x, y, w, h := endsBox(o.From, o.To)
			if whollyOutside(x, y, w, h, pageW, pageH) {
				p.Hazards = append(p.Hazards, Hazard{Kind: "off-page", ID: o.ID, Detail: offPageDetail(x, y, w, h, pageW, pageH)})
			}
			continue
		}
		if o.Kind == Connector || o.From != nil || o.To != nil || o.Box == nil {
			continue
		}
		b := o.Box
		if whollyOutside(b.X, b.Y, b.W, b.H, pageW, pageH) {
			p.Hazards = append(p.Hazards, Hazard{Kind: "off-page", ID: o.ID, Detail: offPageDetail(b.X, b.Y, b.W, b.H, pageW, pageH)})
		}
	}
}

// whollyOutside reports whether the box [x,y,w,h] in view points lies
// wholly outside the page [0,0,pageW,pageH]: its right or bottom edge
// is at or left of the page's left or top edge, or its left or top
// edge is at or right of the page's right or bottom edge. A box that
// only partly lies outside overlaps the page and is not wholly outside.
// Chunk OPF.
func whollyOutside(x, y, w, h, pageW, pageH float64) bool {
	return x+w <= 0 || x >= pageW || y+h <= 0 || y >= pageH
}

// endsBox returns the bounding box, in view points, of the 1-D ends
// from and to: the smallest [x,y,w,h] holding both ends, so the ends
// lie wholly outside the page exactly when their bounding box does.
// Chunk OPF.
func endsBox(from, to *geom.Pt) (float64, float64, float64, float64) {
	x, y := from.X, from.Y
	if to.X < x {
		x = to.X
	}
	if to.Y < y {
		y = to.Y
	}
	return x, y, endsBoxW(from.X, to.X), endsBoxW(from.Y, to.Y)
}

// endsBoxW returns the distance between a and b on one axis.
// Chunk OPF.
func endsBoxW(a, b float64) float64 {
	if a < b {
		return b - a
	}
	return a - b
}

// offPageDetail returns the off-page hazard detail for the box
// [x,y,w,h] in view points on a page [0,0,pageW,pageH] in view points.
// Chunk OPF.
func offPageDetail(x, y, w, h, pageW, pageH float64) string {
	return "box [" + geom.FormatNum(x) + "," + geom.FormatNum(y) + "," + geom.FormatNum(w) + "," + geom.FormatNum(h) +
		"] lies outside the page [0,0," + geom.FormatNum(pageW) + "," + geom.FormatNum(pageH) + "]"
}
