package render

import (
	"errors"
	"image"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jhnoel/documentdriver/internal/opc"
	"github.com/jhnoel/documentdriver/internal/testdeck"
)

func needRenderer(t *testing.T) *Renderer {
	t.Helper()
	r, err := New()
	if errors.Is(err, ErrNoSoffice) || errors.Is(err, ErrNoRasteriser) {
		t.Skip(err)
	}
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { r.Close() })
	return r
}

const slideCT = "application/vnd.openxmlformats-officedocument.presentationml.slide+xml"

// twoSlides returns the minimal deck with a second slide whose API box sits
// at (360pt, 288pt) instead of (72pt, 144pt). Slide 1 is hidden when
// hideFirst is set.
func twoSlides(t *testing.T, hideFirst bool) string {
	t.Helper()
	path := testdeck.Minimal(t)
	pkg, err := opc.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer pkg.Close()
	s1, err := pkg.Part("/ppt/slides/slide1.xml")
	if err != nil {
		t.Fatal(err)
	}
	b, err := s1.Bytes()
	if err != nil {
		t.Fatal(err)
	}
	moved := strings.Replace(string(b), `<a:off x="914400" y="1828800"/>`, `<a:off x="4572000" y="3657600"/>`, 1)
	if moved == string(b) {
		t.Fatal("fixture changed: API offset not found")
	}
	if _, err := pkg.AddPart("/ppt/slides/slide2.xml", []byte(moved), slideCT); err != nil {
		t.Fatal(err)
	}
	rels1, _ := pkg.Rels("/ppt/slides/slide1.xml")
	rels2, err := pkg.Rels("/ppt/slides/slide2.xml")
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range rels1.All() {
		if _, err := rels2.Add(r.Type, r.Target, r.External()); err != nil {
			t.Fatal(err)
		}
	}
	pres, _ := pkg.Rels("/ppt/presentation.xml")
	rid, err := pres.Add(nsR+"/slide", "slides/slide2.xml", false)
	if err != nil {
		t.Fatal(err)
	}
	pp, _ := pkg.Part("/ppt/presentation.xml")
	doc, err := pp.Tree()
	if err != nil {
		t.Fatal(err)
	}
	lst := doc.Root().Child(nsP, "sldIdLst")
	frag := `<p:sldId id="257" r:id="` + rid + `"/>`
	if _, err := lst.InsertFragment(len(lst.Children()), []byte(frag), map[string]string{"p": nsP, "r": nsR}); err != nil {
		t.Fatal(err)
	}
	if hideFirst {
		d1, err := s1.Tree()
		if err != nil {
			t.Fatal(err)
		}
		if err := d1.Root().SetAttr("show", "0"); err != nil {
			t.Fatal(err)
		}
	}
	out := filepath.Join(t.TempDir(), "two.pptx")
	if err := pkg.SaveAs(out); err != nil {
		t.Fatal(err)
	}
	return out
}

// isBackground reports whether the pixel is (near) white.
func isBackground(img *image.RGBA, x, y int) bool {
	c := img.RGBAAt(x, y)
	return c.R > 245 && c.G > 245 && c.B > 245
}

// pt returns the pixel at a point in slide space.
func pt(res *Result, x, y float64) (int, int) {
	b := res.Image.Bounds()
	return int(x / res.Info.W * float64(b.Dx())), int(y / res.Info.H * float64(b.Dy()))
}

func TestSlideHiddenAndOrder(t *testing.T) {
	r := needRenderer(t)
	deck := twoSlides(t, true)

	// The hidden first slide still renders, at the original position.
	res, err := r.Slide(deck, "slide1", Options{})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Info.Hidden || res.Info.Hiddens != 1 || res.Info.Index != 0 || res.Info.Count != 2 {
		t.Errorf("info = %+v", res.Info)
	}
	if res.Info.W != 960 || res.Info.H != 540 {
		t.Errorf("slide size = %gx%g, want 960x540", res.Info.W, res.Info.H)
	}
	b := res.Image.Bounds()
	if b.Dx() < 1440 || b.Dx() > 1442 || b.Dy() != 810 {
		t.Errorf("image = %dx%d, want about 1440x810 at scale 1.5", b.Dx(), b.Dy())
	}
	// Known shape: the API box (72,144,180,90 pt) is filled with accent1.
	x, y := pt(res, 72+90, 144+45)
	if isBackground(res.Image, x, y) {
		t.Errorf("API box interior at (%d,%d) is background", x, y)
	}
	x, y = pt(res, 360+90, 288+45)
	if !isBackground(res.Image, x, y) {
		t.Errorf("slide 1 has paint where only slide 2 has a box")
	}

	// Slide 2 is page 2 even though slide 1 is hidden.
	res2, err := r.Slide(deck, "/ppt/slides/slide2.xml", Options{Scale: 1})
	if err != nil {
		t.Fatal(err)
	}
	if res2.Info.Hidden || res2.Info.Index != 1 {
		t.Errorf("info = %+v", res2.Info)
	}
	if b := res2.Image.Bounds(); b.Dx() < 960 || b.Dx() > 962 {
		t.Errorf("scale 1 width = %d", b.Dx())
	}
	x, y = pt(res2, 360+90, 288+45)
	if isBackground(res2.Image, x, y) {
		t.Errorf("slide 2 API box interior at (%d,%d) is background", x, y)
	}
	x, y = pt(res2, 72+90, 144+45)
	if !isBackground(res2.Image, x, y) {
		t.Errorf("slide 2 has paint at slide 1's box position")
	}
	if r.Conversions() != 1 {
		t.Errorf("%d conversions for one deck, want 1", r.Conversions())
	}
}

func TestSlideWorkDir(t *testing.T) {
	r := needRenderer(t)
	deck := testdeck.Minimal(t)
	if _, err := r.Slide(deck, "slide1", Options{}); err != nil {
		t.Fatal(err)
	}
	// Slide converted the deck once, under the name deck.pptx, so the
	// cached PDF sits beside the deck.pptx copy in the work directory.
	pdf, err := r.Convert(deck, "deck.pptx", "", DefaultTimeout)
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Base(pdf) != "deck.pdf" {
		t.Errorf("pdf = %s, want base deck.pdf", pdf)
	}
	work := filepath.Dir(pdf)
	for _, want := range []string{"deck.pptx", "deck.pdf"} {
		if _, err := os.Stat(filepath.Join(work, want)); err != nil {
			t.Errorf("work directory lacks %s: %v", want, err)
		}
	}
	if r.Conversions() != 1 {
		t.Errorf("Conversions() = %d, want 1", r.Conversions())
	}
}

func TestSlideErrors(t *testing.T) {
	r := needRenderer(t)
	deck := testdeck.Minimal(t)
	if _, err := r.Slide(deck, "slide9", Options{}); err == nil || !strings.Contains(err.Error(), "not in the presentation") {
		t.Errorf("unknown slide: %v", err)
	}
	bad := filepath.Join(t.TempDir(), "bad.pptx")
	if err := os.WriteFile(bad, []byte("not a zip"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Slide(bad, "slide1", Options{}); err == nil {
		t.Error("garbage deck rendered")
	}
	if _, err := r.Slide(deck, "slide1", Options{Timeout: time.Millisecond}); err == nil || !strings.Contains(err.Error(), "timed out") {
		t.Errorf("timeout: %v", err)
	}
}

func TestLocateNoSoffice(t *testing.T) {
	pkg, err := opc.Open(testdeck.Minimal(t))
	if err != nil {
		t.Fatal(err)
	}
	defer pkg.Close()
	info, err := Locate(pkg, "slide1")
	if err != nil {
		t.Fatal(err)
	}
	if info.Part != "/ppt/slides/slide1.xml" || info.W != 960 || info.H != 540 || info.Hidden {
		t.Errorf("info = %+v", info)
	}
}
