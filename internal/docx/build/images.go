package build

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"image"
	"image/gif"
	"image/jpeg"
	"image/png"
	"strconv"
	"strings"

	"github.com/jhnoel/documentdriver/internal/docx/html"
	"github.com/jhnoel/documentdriver/internal/ooxml/ctypes"
)

// Images (create): an <img> whose src is a data: URL of a PNG, JPEG or
// GIF becomes an inline DrawingML picture, as Word writes one: its bytes
// a media part the caller adds (Options.Image), its size the HTML's width
// and height in CSS pixels (read's unit: 9525 EMU), or the image's own
// at 96 pixels per inch, scaled to keep its aspect when only one is given.

// The DrawingML namespaces of an inline picture (transitional, as the
// template is).
const (
	nsWP  = "http://schemas.openxmlformats.org/drawingml/2006/wordprocessingDrawing"
	nsA   = "http://schemas.openxmlformats.org/drawingml/2006/main"
	nsPic = "http://schemas.openxmlformats.org/drawingml/2006/picture"
)

// emuPerPx is the EMU of one CSS pixel (read's emuToPx).
const emuPerPx = 9525

// maxImagePx bounds a picture's width and height in pixels (about 26
// metres at 96 per inch; Word's own limit is far smaller).
const maxImagePx = 100000

// Image is a picture decoded from a data: URL.
type Image struct {
	// ContentType and Ext name the media part's type and extension.
	ContentType, Ext string
	// Data are the image's bytes.
	Data []byte
	// Width and Height are its own size in pixels.
	Width, Height int
}

// imageFormats are the formats a data: URL may carry: its media type, the
// part's extension and content type, and the header decoder.
var imageFormats = map[string]struct {
	ext, ct string
	config  func([]byte) (image.Config, error)
}{
	ctypes.CTImagePng:  {"png", ctypes.CTImagePng, func(b []byte) (image.Config, error) { return png.DecodeConfig(bytes.NewReader(b)) }},
	ctypes.CTImageJpeg: {"jpeg", ctypes.CTImageJpeg, func(b []byte) (image.Config, error) { return jpeg.DecodeConfig(bytes.NewReader(b)) }},
	ctypes.CTImageGif:  {"gif", ctypes.CTImageGif, func(b []byte) (image.Config, error) { return gif.DecodeConfig(bytes.NewReader(b)) }},
}

// DecodeImage reads a data: URL (data:<mime>;base64,<payload>) of a PNG,
// JPEG or GIF; anything else, or bytes that are not that format, is
// refused as content (an ElementError for img).
func DecodeImage(src string) (Image, error) {
	rest, ok := strings.CutPrefix(src, "data:")
	meta, payload, ok2 := strings.Cut(rest, ",")
	mime, enc, ok3 := strings.Cut(meta, ";")
	if !ok || !ok2 || !ok3 || enc != "base64" {
		return Image{}, imgErr("an image's src is a data: URL, data:<mime>;base64,<bytes>")
	}
	f, ok := imageFormats[strings.ToLower(mime)]
	if !ok {
		return Image{}, imgErr("an image is PNG, JPEG or GIF (data:image/png, image/jpeg or image/gif); got %q", mime)
	}
	data, err := base64.StdEncoding.DecodeString(payload)
	if err != nil {
		return Image{}, imgErr("an image's data: URL is not valid base64: %v", err)
	}
	cfg, err := f.config(data)
	if err != nil {
		return Image{}, imgErr("an image's bytes are not a %s image: %v", mime, err)
	}
	if cfg.Width < 1 || cfg.Height < 1 || cfg.Width > maxImagePx || cfg.Height > maxImagePx {
		return Image{}, imgErr("an image is 1 to %d pixels wide and high", maxImagePx)
	}
	return Image{ContentType: f.ct, Ext: f.ext, Data: data, Width: cfg.Width, Height: cfg.Height}, nil
}

// Picture is what Options.Image gives a placed image: the relationship id
// of its media part from the part being built, a drawing object id unique
// in the document, and its own size in pixels.
type Picture struct {
	RID           string
	ID            int
	Width, Height int
}

// imgErr refuses an image.
func imgErr(format string, args ...any) error {
	return &ElementError{Element: "img", Msg: fmt.Sprintf(format, args...)}
}

// extent is the picture's size in EMU: the HTML's width and height, one
// of them scaled from the image's aspect, or its own size.
func extent(a html.Atom, p Picture) (cx, cy int64, err error) {
	w, h := int64(p.Width), int64(p.Height)
	switch {
	case a.Width.OK && a.Height.OK:
		w, h = int64(a.Width.V), int64(a.Height.V)
	case a.Width.OK:
		w, h = int64(a.Width.V), max(1, (int64(a.Width.V)*h+w/2)/w)
	case a.Height.OK:
		w, h = max(1, (int64(a.Height.V)*w+h/2)/h), int64(a.Height.V)
	}
	if w < 1 || h < 1 || w > maxImagePx || h > maxImagePx {
		return 0, 0, imgErr("an image's width and height are 1 to %d pixels", maxImagePx)
	}
	return w * emuPerPx, h * emuPerPx, nil
}

// drawing is the w:drawing of an inline picture.
func drawing(a html.Atom, p Picture, cx, cy int64) string {
	id := strconv.Itoa(p.ID)
	x, y := strconv.FormatInt(cx, 10), strconv.FormatInt(cy, 10)
	descr := ""
	if a.Alt.OK && a.Alt.V != "" {
		descr = ` descr="` + html.EscapeAttr(a.Alt.V) + `"`
	}
	name := "Picture " + id
	return `<w:drawing><wp:inline xmlns:wp="` + nsWP + `" distT="0" distB="0" distL="0" distR="0">` +
		`<wp:extent cx="` + x + `" cy="` + y + `"/><wp:effectExtent l="0" t="0" r="0" b="0"/>` +
		`<wp:docPr id="` + id + `" name="` + name + `"` + descr + `/>` +
		`<wp:cNvGraphicFramePr><a:graphicFrameLocks xmlns:a="` + nsA + `" noChangeAspect="1"/></wp:cNvGraphicFramePr>` +
		`<a:graphic xmlns:a="` + nsA + `"><a:graphicData uri="` + nsPic + `">` +
		`<pic:pic xmlns:pic="` + nsPic + `"><pic:nvPicPr><pic:cNvPr id="` + id + `" name="` + name + `"` + descr + `/><pic:cNvPicPr/></pic:nvPicPr>` +
		`<pic:blipFill><a:blip r:embed="` + p.RID + `"/><a:stretch><a:fillRect/></a:stretch></pic:blipFill>` +
		`<pic:spPr><a:xfrm><a:off x="0" y="0"/><a:ext cx="` + x + `" cy="` + y + `"/></a:xfrm><a:prstGeom prst="rect"><a:avLst/></a:prstGeom></pic:spPr>` +
		`</pic:pic></a:graphicData></a:graphic></wp:inline></w:drawing>`
}
