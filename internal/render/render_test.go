package render

import (
	"errors"
	"image"
	"image/color"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jhnoel/documentdriver/internal/opc"
	"github.com/jhnoel/documentdriver/internal/repo"
	"github.com/jhnoel/documentdriver/internal/testdeck"
)

func needRenderer(t *testing.T) *Renderer {
	t.Helper()
	r, err := New("")
	if errors.Is(err, ErrNoSoffice) || errors.Is(err, ErrNoRasteriser) {
		t.Skip(err)
	}
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { r.Close() })
	return r
}

// vsdxFixture builds testdata/vsdx/v1-minimal (with the shared common
// members) into a fresh file in t.TempDir and returns its path.
func vsdxFixture(t *testing.T) string {
	t.Helper()
	root, err := repo.Root()
	if err != nil {
		t.Fatal(err)
	}
	base := filepath.Join(root, "testdata", "vsdx")
	pairs := [][2]string{
		{"common/document.xml", "visio/document.xml"},
		{"common/app.xml", "docProps/app.xml"},
		{"v1-minimal/[Content_Types].xml", "[Content_Types].xml"},
		{"v1-minimal/_rels/.rels", "_rels/.rels"},
		{"v1-minimal/visio/_rels/document.xml.rels", "visio/_rels/document.xml.rels"},
		{"v1-minimal/visio/pages/_rels/pages.xml.rels", "visio/pages/_rels/pages.xml.rels"},
		{"v1-minimal/visio/pages/page1.xml", "visio/pages/page1.xml"},
		{"v1-minimal/visio/pages/pages.xml", "visio/pages/pages.xml"},
	}
	var members []opc.ZipMember
	for _, p := range pairs {
		b, err := os.ReadFile(filepath.Join(base, filepath.FromSlash(p[0])))
		if err != nil {
			t.Fatal(err)
		}
		members = append(members, opc.ZipMember{Name: p[1], Body: string(b)})
	}
	data, err := opc.BuildZip(members...)
	if err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(t.TempDir(), "source.vsdx")
	if err := os.WriteFile(out, data, 0o644); err != nil {
		t.Fatal(err)
	}
	return out
}

func TestConvertBadNames(t *testing.T) {
	file := filepath.Join(t.TempDir(), "input.bin")
	if err := os.WriteFile(file, []byte("bytes"), 0o644); err != nil {
		t.Fatal(err)
	}
	r := &Renderer{dir: t.TempDir(), pdf: map[string]string{}}
	for _, name := range []string{"a/b.pptx", "deck"} {
		if _, err := r.Convert(file, name, "", DefaultTimeout); err == nil {
			t.Errorf("Convert(name %q) succeeded, want error", name)
		} else if !strings.Contains(err.Error(), name) {
			t.Errorf("Convert(name %q) error %q does not name it", name, err)
		}
	}
	if r.Conversions() != 0 {
		t.Errorf("Conversions() = %d after rejected names, want 0", r.Conversions())
	}
}

func TestConvertNamesWorkFiles(t *testing.T) {
	for _, tc := range []struct{ name, pdf string }{
		{"deck.pptx", "deck.pdf"},
		{"drawing.vsdx", "drawing.pdf"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := needRenderer(t)
			file := testdeck.Minimal(t)
			if tc.name == "drawing.vsdx" {
				file = vsdxFixture(t)
			}
			pdf, err := r.Convert(file, tc.name, "", DefaultTimeout)
			if err != nil {
				t.Fatal(err)
			}
			if filepath.Base(pdf) != tc.pdf {
				t.Errorf("pdf = %s, want base %s", pdf, tc.pdf)
			}
			work := filepath.Dir(pdf)
			for _, want := range []string{tc.name, tc.pdf} {
				if _, err := os.Stat(filepath.Join(work, want)); err != nil {
					t.Errorf("work directory lacks %s: %v", want, err)
				}
			}
			if r.Conversions() != 1 {
				t.Errorf("Conversions() = %d, want 1", r.Conversions())
			}
			if _, err := r.Convert(file, tc.name, "", DefaultTimeout); err != nil {
				t.Fatal(err)
			}
			if r.Conversions() != 1 {
				t.Errorf("Conversions() = %d after the same file again, want 1", r.Conversions())
			}
		})
	}
}

// TestCropToPage checks CropToPage: the union raster is cut to one
// page's own size at the scale, keeping the top-left content, and a
// page that already fills the raster, or a page with no positive
// finite size or scale, leaves the raster unchanged.
func TestCropToPage(t *testing.T) {
	// pattern holds a distinct colour at each of 8 by 6 pixels.
	pattern := func() *image.RGBA {
		img := image.NewRGBA(image.Rect(0, 0, 8, 6))
		for y := range 6 {
			for x := range 8 {
				img.SetRGBA(x, y, color.RGBA{uint8(x), uint8(y), 0, 255})
			}
		}
		return img
	}
	for _, tc := range []struct {
		w, h, scale   float64
		width, height int
	}{
		{w: 5, h: 4, scale: 1, width: 5, height: 4},
		{w: 5, h: 2, scale: 2, width: 8, height: 4}, // at the raster, clamped
		{w: 2.5, h: 1.5, scale: 2, width: 5, height: 3},
		{w: 3.6, h: 3.4, scale: 1, width: 4, height: 3}, // rounded to a pixel
		{w: 8, h: 6, scale: 1, width: 8, height: 6},     // already its size
		{w: 100, h: 100, scale: 1, width: 8, height: 6}, // larger than the raster, clamped
		{w: 0, h: 4, scale: 1, width: 8, height: 6},
		{w: -5, h: 4, scale: 1, width: 8, height: 6},
		{w: 5, h: 4, scale: 0, width: 8, height: 6},
		{w: 5, h: 4, scale: -1, width: 8, height: 6},
		{w: math.NaN(), h: 4, scale: 1, width: 8, height: 6},
		{w: 5, h: math.Inf(1), scale: 1, width: 8, height: 6},
		{w: 5, h: 4, scale: math.Inf(1), width: 8, height: 6},
	} {
		img := pattern()
		got := CropToPage(img, tc.w, tc.h, tc.scale)
		if got.Bounds().Dx() != tc.width || got.Bounds().Dy() != tc.height {
			t.Errorf("CropToPage(%v, %v, %v) = %d x %d, want %d x %d",
				tc.w, tc.h, tc.scale, got.Bounds().Dx(), got.Bounds().Dy(), tc.width, tc.height)
			continue
		}
		for y := 0; y < tc.height; y++ {
			for x := 0; x < tc.width; x++ {
				if got.RGBAAt(x, y) != img.RGBAAt(x, y) {
					t.Fatalf("CropToPage(%v, %v, %v) pixel %d,%d = %v, want %v (top-left content kept)",
						tc.w, tc.h, tc.scale, x, y, got.RGBAAt(x, y), img.RGBAAt(x, y))
				}
			}
		}
	}
	if CropToPage(nil, 5, 4, 1) != nil {
		t.Error("CropToPage(nil) is not nil")
	}
}
