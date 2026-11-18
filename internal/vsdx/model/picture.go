package model

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"image"
	_ "image/jpeg" // DecodeConfig for JPEG headers
	_ "image/png"  // DecodeConfig for PNG headers
	"io"
	"strings"

	"github.com/jhnoel/documentdriver/internal/ooxml/ctypes"
	"github.com/jhnoel/documentdriver/internal/ooxml/tree"
	"github.com/jhnoel/documentdriver/internal/vsdx/doc"
	"github.com/jhnoel/documentdriver/internal/vsdx/inherit"
	"github.com/jhnoel/documentdriver/internal/vsdx/sheet"
	"github.com/jhnoel/documentdriver/internal/vsdx/vsd"
)

// PictureInfo is a picture's image facts (docs/contract.md §8): the media
// part, its content type, size, SHA-256 and pixel size (HasPx), and
// the crop [l, t, r, b] in percent (HasCrop).
type PictureInfo struct {
	Image   string
	Type    string
	Bytes   int
	SHA256  string
	Px      [2]int
	HasPx   bool
	Crop    [4]float64
	HasCrop bool
}

// mediaFacts is the cached facts of one media part.
type mediaFacts struct {
	typ    string
	bytes  int
	sha    string
	px     [2]int
	hasPx  bool
	broken string
}

// RelID returns the r:id of a ForeignData element's Rel child.
func RelID(foreign *tree.Node) string {
	if foreign == nil {
		return ""
	}
	for c := range foreign.ElementsSeq() {
		if c.Local() == vsd.ElemRel {
			for name, v := range c.AttrsSeq() {
				if name == "id" || strings.HasSuffix(name, ":id") {
					return v
				}
			}
		}
	}
	return ""
}

// pictureOf fills o.Picture from the ForeignData of a picture shape
// stored in part: the relationship's target media part and its facts
// (cached per part in cache), and the crop from the Img cells. A
// missing relationship or part is a note on the page.
func pictureOf(o *Object, p *Page, d *doc.Document, r *inherit.Resolver, s *inherit.Shape, part string, foreign *tree.Node, cache map[string]*mediaFacts) {
	id := RelID(foreign)
	target := ""
	if id != "" && part != "" && d != nil && d.Pkg != nil {
		if rels, err := d.Pkg.Rels(part); err == nil {
			if rel, ok := rels.Get(id); ok && !rel.External() {
				target, _ = rels.TargetPart(rel)
			}
		}
	}
	if target == "" || !d.Pkg.Has(target) {
		p.Notes = append(p.Notes, doc.Note{At: o.ID, Reason: "picture image relationship or part is missing"})
		return
	}
	f := cache[target]
	if f == nil {
		f = readMedia(d, target)
		cache[target] = f
	}
	pic := &PictureInfo{Image: target, Type: f.typ, Bytes: f.bytes, SHA256: f.sha, Px: f.px, HasPx: f.hasPx}
	if !f.hasPx {
		o.Omitted = append(o.Omitted, Omission{Prop: "media.px", Reason: "pixel size not derived for " + f.typ})
	}
	cropOf(o, pic, r, s)
	o.Picture = pic
}

// readMedia reads one media part's content type, size, hash and, for
// PNG and JPEG, its header's pixel size.
func readMedia(d *doc.Document, name string) *mediaFacts {
	f := &mediaFacts{}
	pt, err := d.Pkg.Part(name)
	if err != nil {
		f.broken = err.Error()
		return f
	}
	f.typ, _ = pt.ContentType()
	h := sha256.New()
	head := &HeadWriter{Max: HeaderPrefix}
	n, err := pt.WriteTo(io.MultiWriter(h, head))
	if err != nil {
		f.broken = err.Error()
		return f
	}
	f.bytes = int(n)
	f.sha = hex.EncodeToString(h.Sum(nil))
	f.px, f.hasPx = HeaderPx(f.typ, head.B)
	return f
}

// HeaderPrefix is how much of a media part is kept for the header
// decode: a PNG IHDR is in the first bytes and a JPEG SOF within it in
// practice; a part is hashed whole but never held.
const HeaderPrefix = 256 << 10

// HeadWriter keeps the first Max bytes written to it in B.
type HeadWriter struct {
	B   []byte
	Max int
}

// Write keeps what fits under Max and reports all bytes written.
func (p *HeadWriter) Write(b []byte) (int, error) {
	if room := p.Max - len(p.B); room > 0 {
		p.B = append(p.B, b[:min(room, len(b))]...)
	}
	return len(b), nil
}

// HeaderPx returns the pixel size a PNG or JPEG header in head
// declares, and false for any other type or a header that does not
// decode.
func HeaderPx(typ string, head []byte) ([2]int, bool) {
	if typ != ctypes.CTImagePng && typ != ctypes.CTImageJpeg {
		return [2]int{}, false
	}
	cfg, _, err := image.DecodeConfig(bytes.NewReader(head))
	if err != nil {
		return [2]int{}, false
	}
	return [2]int{cfg.Width, cfg.Height}, true
}

// cropOf derives a picture's crop from ImgOffsetX/Y and ImgWidth/Height
// when they are in the forms §3 writes (ImgWidth Width*a, ImgOffsetX
// ImgWidth*b, likewise for the height): l = -100b, r = 100 - 100/a - l;
// b and t likewise. No Img cells shows no crop; other forms omit crop.
func cropOf(o *Object, pic *PictureInfo, r *inherit.Resolver, s *inherit.Shape) {
	get := func(name string) (string, bool) {
		c, _, ok := r.Cell(s, sheet.ShapeCell(name))
		return c.F, ok
	}
	wf, okW := get("ImgWidth")
	hf, okH := get("ImgHeight")
	xf, okX := get("ImgOffsetX")
	yf, okY := get("ImgOffsetY")
	if !okW && !okH && !okX && !okY {
		return
	}
	a, ok1 := factor(wf, "Width")
	c, ok2 := factor(hf, "Height")
	bx, ok3 := factor(xf, "ImgWidth")
	by, ok4 := factor(yf, "ImgHeight")
	if !ok1 || !ok2 || !ok3 || !ok4 || a <= 0 || c <= 0 {
		o.Omitted = append(o.Omitted, Omission{Prop: "crop", Reason: "Img cells not in the crop form"})
		return
	}
	l, bottom := -bx*100, -by*100
	right, top := 100-100/a-l, 100-100/c-bottom
	crop := [4]float64{round2(l), round2(top), round2(right), round2(bottom)}
	if crop == [4]float64{} {
		return
	}
	pic.Crop, pic.HasCrop = crop, true
}

// factor parses "Name*k" (spaces allowed) and returns k.
func factor(f, name string) (float64, bool) {
	f = strings.ReplaceAll(f, " ", "")
	rest, ok := strings.CutPrefix(f, name+"*")
	if !ok {
		return 0, false
	}
	rest = strings.TrimSuffix(strings.TrimPrefix(rest, "("), ")")
	return vsd.Number(rest)
}

// round2 rounds to two decimals, as read's percentages show.
func round2(v float64) float64 {
	if v < 0 && v > -0.005 {
		return 0
	}
	n := v * 100
	if n < 0 {
		return float64(int64(n-0.5)) / 100
	}
	return float64(int64(n+0.5)) / 100
}
