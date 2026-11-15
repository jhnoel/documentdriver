package create

import (
	"bytes"
	"encoding/base64"
	"errors"
	"image"
	"image/color"
	"image/gif"
	"image/jpeg"
	"image/png"
	"regexp"
	"strings"
	"testing"

	"github.com/jhnoel/documentdriver/internal/opc"
)

// pngURL is a data: URL of a w x h PNG.
func pngURL(t *testing.T, w, h int) string {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	img.Set(0, 0, color.RGBA{R: 255, A: 255})
	var b bytes.Buffer
	if err := png.Encode(&b, img); err != nil {
		t.Fatal(err)
	}
	return "data:image/png;base64," + base64.StdEncoding.EncodeToString(b.Bytes())
}

// TestCreateImages: an <img> with a data: URL is an inline picture of
// the size the HTML gives (or the image's own, or scaled to its aspect
// from one side), the same image twice is one media part, a picture in a
// header has its own relationship, and read shows each with its size and
// alt; read's markup with the asset's data_url in src reads the same.
func TestCreateImages(t *testing.T) {
	src := pngURL(t, 40, 20)
	in := `<header><p><img src="` + src + `" alt="logo"></p></header>` +
		`<p>a <img src="` + src + `" width="80" height="30" alt="wide"> b <img src="` + src + `"> <img src="` + src + `" width="10"></p>` +
		`<table><tr><td><img src="` + src + `" height="5"></td></tr></table>`
	out := created(t, in)
	m := markupOf(t, out)
	for _, want := range []string{`width="80" height="30" alt="wide"`, `width="40" height="20"`, `width="10" height="5"`, `alt="logo"`} {
		if !strings.Contains(m, want) {
			t.Errorf("no %s in\n%s", want, m)
		}
	}
	pkg, err := opc.OpenBytes(out, opc.DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	var media []string
	for _, name := range pkg.Names() {
		if strings.HasPrefix(name, "/word/media/") {
			media = append(media, name)
		}
	}
	if len(media) != 1 || media[0] != "/word/media/image1.png" {
		t.Errorf("media parts %v, want one", media)
	}
	// read shows src as an asset address; with the data_url put back,
	// its markup makes a document that reads the same.
	back := assetSrc.ReplaceAllString(m, `src="`+src+`"`)
	if again := markupOf(t, created(t, back)); sameContent(again) != sameContent(m) {
		t.Errorf("create(read(doc)) reads differently:\n%s\n%s", m, again)
	}
}

// TestCreateImageFormats: JPEG and GIF build as PNG does, each its own
// media part with its own extension.
func TestCreateImageFormats(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 3, 2))
	var j, g bytes.Buffer
	if err := jpeg.Encode(&j, img, nil); err != nil {
		t.Fatal(err)
	}
	if err := gif.Encode(&g, img, nil); err != nil {
		t.Fatal(err)
	}
	out := created(t, `<p><img src="data:image/jpeg;base64,`+base64.StdEncoding.EncodeToString(j.Bytes())+`"><img src="data:image/gif;base64,`+base64.StdEncoding.EncodeToString(g.Bytes())+`"></p>`)
	pkg, err := opc.OpenBytes(out, opc.DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"/word/media/image1.jpeg", "/word/media/image2.gif"} {
		if !pkg.Has(name) {
			t.Errorf("no %s", name)
		}
	}
	if m := markupOf(t, out); strings.Count(m, `width="3" height="2"`) != 2 {
		t.Errorf("want two 3x2 images:\n%s", m)
	}
}

// assetSrc is the src read writes for an image: its asset address.
var assetSrc = regexp.MustCompile(`src="assets/[^"]*"`)

// TestCreateImageRefusals: read's asset address, a URL, a format other
// than PNG, JPEG or GIF and bytes that are not the image are refused at
// html<img>, the asset address with a pointer to read --kind assets.
func TestCreateImageRefusals(t *testing.T) {
	for _, c := range []struct{ in, msg string }{
		{`<p><img src="assets/i1.png" data-ref="0A1B2C3D/opaque:1" data-image-part="word/media/image1.png" width="10" height="10"></p>`, "--kind assets"},
		{`<p><img src="https://example.com/a.png"></p>`, "data:"},
		{`<p><img src="data:image/svg+xml;base64,PHN2Zy8+"></p>`, "PNG, JPEG or GIF"},
		{`<p><img src="data:image/png;base64,iVBORw0KGgo="></p>`, "not a image/png image"},
		{`<p><img src="data:image/png;base64,***"></p>`, "base64"},
	} {
		_, err := Build(c.in)
		var e *Error
		if !errors.As(err, &e) || e.Code != "invalid_operation" || e.Path != "html<img>" || !strings.Contains(e.Msg, c.msg) {
			t.Errorf("%s: %v (%+v), want invalid_operation at html<img> (%s)", c.in, err, e, c.msg)
		}
	}
}
