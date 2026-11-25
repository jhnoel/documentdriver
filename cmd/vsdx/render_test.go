package main

import (
	"bytes"
	"errors"
	"fmt"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jhnoel/documentdriver/internal/cli"
	"github.com/jhnoel/documentdriver/internal/laws"
	"github.com/jhnoel/documentdriver/internal/render"
	"github.com/jhnoel/documentdriver/internal/vsdx/read"
	"github.com/jhnoel/documentdriver/internal/vsdx/vsdxtest"
)

// needRenderer returns a renderer, skipping the test when LibreOffice
// or a rasteriser is missing.
func needRenderer(t *testing.T) *render.Renderer {
	t.Helper()
	renderer, err := render.New(os.Getenv("DD_SOFFICE"))
	if errors.Is(err, render.ErrNoSoffice) || errors.Is(err, render.ErrNoRasteriser) {
		t.Skip(err)
	}
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := renderer.Close(); err != nil {
			t.Errorf("close renderer: %v", err)
		}
	})
	return renderer
}

// decodeRenderPNG decodes the PNG at path and returns its size and a
// function reading the colour at a point of the 612 x 792 page.
func decodeRenderPNG(t *testing.T, path string) (int, int, func(x, y float64) (uint32, uint32, uint32)) {
	t.Helper()
	file, err := os.Open(path) //nolint:gosec // G304: a picture the test just wrote
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	img, err := png.Decode(file)
	if err != nil {
		t.Fatal(err)
	}
	bounds := img.Bounds()
	return bounds.Dx(), bounds.Dy(), func(x, y float64) (uint32, uint32, uint32) {
		r, g, b, _ := img.At(int(x*float64(bounds.Dx())/612), int(y*float64(bounds.Dy())/792)).RGBA()
		return r >> 8, g >> 8, b >> 8
	}
}

// TestRenderV1 checks the RN2 V1 row: page0 renders to a PNG of
// 612 x 792 pt at --scale 1 (within a pixel), and the result says so.
func TestRenderV1(t *testing.T) {
	needRenderer(t)
	out := filepath.Join(t.TempDir(), "page0.png")
	var stdout, stderr bytes.Buffer
	args := []string{"render", vsdxtest.Fixture(t, "v1-minimal"), "--page", "page0", "--scale", "1", "-o", out}
	if code := run(args, &stdout, &stderr); code != 0 {
		t.Fatalf("render: exit %d: %s", code, stderr.String())
	}
	for _, want := range []string{`"saved":"` + out + `"`, `"px":[612,792]`, `"size":[612,792]`, `"scale":1`, `"key":[]`} {
		if !strings.Contains(stdout.String(), want) {
			t.Errorf("stdout = %q, want it to contain %s", stdout.String(), want)
		}
	}
	w, h, _ := decodeRenderPNG(t, out)
	if w < 611 || w > 613 || h < 791 || h > 793 {
		t.Errorf("PNG = %d x %d, want 612 x 792 within 1 px", w, h)
	}
}

// TestRenderV17 checks the RN3 V17 row: at --scale 1, page1 (11 x 8.5
// in) renders to a PNG of 792 x 612 px (within 1 px) and page0
// (8.5 x 11 in) to 612 x 792, each the page's own size and not the
// union of all pages, and --overlay ids on page0 puts s1's tag inside
// s1's box [108,378,72,36] at that scale.
func TestRenderV17(t *testing.T) {
	needRenderer(t)
	file := vsdxtest.Fixture(t, "v17-orientation")
	input := fixtureBytes(t, "v17-orientation")
	marks, err := pageMarks(input, "page0", nil, false)
	if err != nil {
		t.Fatalf("pageMarks(V17 page0): %v", err)
	}
	if len(marks) != 1 || marks[0].ID != "page0/s1" || marks[0].Box != [4]float64{108, 378, 72, 36} {
		t.Fatalf("marks = %+v, want one mark for page0/s1 with box [108 378 72 36]", marks)
	}
	for _, tc := range []struct {
		page string
		w, h int
		px   string
		size string
	}{
		{page: "page1", w: 792, h: 612, px: `"px":[792,612]`, size: `"size":[792,612]`},
		{page: "page0", w: 612, h: 792, px: `"px":[612,792]`, size: `"size":[612,792]`},
	} {
		out := filepath.Join(t.TempDir(), tc.page+".png")
		var stdout, stderr bytes.Buffer
		args := []string{"render", file, "--page", tc.page, "--scale", "1", "-o", out}
		if code := run(args, &stdout, &stderr); code != 0 {
			t.Fatalf("render %s: exit %d: %s", tc.page, code, stderr.String())
		}
		for _, want := range []string{tc.px, tc.size, `"scale":1`} {
			if !strings.Contains(stdout.String(), want) {
				t.Errorf("render %s stdout = %q, want it to contain %s", tc.page, stdout.String(), want)
			}
		}
		w, h, _ := decodeRenderPNG(t, out)
		if w < tc.w-1 || w > tc.w+1 || h < tc.h-1 || h > tc.h+1 {
			t.Errorf("render %s PNG = %d x %d, want %d x %d within 1 px", tc.page, w, h, tc.w, tc.h)
		}
	}
	out := filepath.Join(t.TempDir(), "over.png")
	var stdout, stderr bytes.Buffer
	args := []string{"render", file, "--page", "page0", "--overlay", "ids", "--scale", "1", "-o", out}
	if code := run(args, &stdout, &stderr); code != 0 {
		t.Fatalf("render overlay: exit %d: %s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), `"px":[612,792]`) {
		t.Errorf("render overlay stdout = %q, want px [612,792]", stdout.String())
	}
	decodeOverlayTag(t, out, [4]float64{108, 378, 72, 36})
}

// decodeOverlayTag checks that the overlay tag (its yellow background,
// 255,235,0) of a render with --overlay ids is drawn entirely inside
// box at scale 1, so the overlay maps with the page's own size, not
// the union's.
func decodeOverlayTag(t *testing.T, path string, box [4]float64) {
	t.Helper()
	file, err := os.Open(path) //nolint:gosec // G304: a picture the test just wrote
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	img, err := png.Decode(file)
	if err != nil {
		t.Fatal(err)
	}
	bounds := img.Bounds()
	found := false
	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			r, g, b, _ := img.At(x, y).RGBA()
			if r>>8 == 255 && g>>8 == 235 && b>>8 == 0 {
				found = true
				fx, fy := float64(x-bounds.Min.X), float64(y-bounds.Min.Y)
				if fx < box[0] || fy < box[1] || fx >= box[0]+box[2] || fy >= box[1]+box[3] {
					t.Errorf("tag at %v,%v is outside s1's box %v, want it inside", fx, fy, box)
					return
				}
			}
		}
	}
	if !found {
		t.Errorf("no tag background (255,235,0) found in %s, want s1's tag inside its box %v", path, box)
	}
}

// TestRenderV8 checks the RN2 V8 row by measuring LibreOffice's PDF:
// page0 is PDF page index 0 (its shape on its background), page1 is
// index 1 (blank) and the background page3 is index 2 (its footer on
// its own), of 3 PDF pages, and each renders through the command.
func TestRenderV8(t *testing.T) {
	renderer := needRenderer(t)
	file := vsdxtest.Fixture(t, "v8-pages")
	input, err := os.ReadFile(file) //nolint:gosec // G304: a fixture the test just built
	if err != nil {
		t.Fatal(err)
	}
	pdf, err := renderer.Convert(file, "drawing.vsdx", "", render.DefaultTimeout)
	if err != nil {
		t.Fatal(err)
	}
	if n := render.CountPages(pdf); n != 3 {
		t.Fatalf("CountPages = %d, want 3 PDF pages for V8's 3 pages", n)
	}
	blank := map[string]bool{}
	for _, tc := range []struct {
		page  string
		index int
		want  bool // whether the rasterised PDF page is blank
	}{
		{"page0", 0, false},
		{"page1", 1, true},
		{"page3", 2, false},
	} {
		info, err := read.Locate(input, tc.page)
		if err != nil {
			t.Fatalf("Locate(%s): %v", tc.page, err)
		}
		if info.Index != tc.index {
			t.Errorf("Locate(%s).Index = %d, want PDF page index %d", tc.page, info.Index, tc.index)
		}
		img, err := renderer.Rasterise(pdf, info.Index+1, 72, render.DefaultTimeout)
		if err != nil {
			t.Fatalf("Rasterise(%s): %v", tc.page, err)
		}
		blank[tc.page] = isBlank(img)
		if blank[tc.page] != tc.want {
			t.Errorf("rasterised PDF page %d for %s blank = %v, want %v", info.Index+1, tc.page, blank[tc.page], tc.want)
		}
	}
	for _, page := range []string{"page0", "page1", "page3"} {
		out := filepath.Join(t.TempDir(), page+".png")
		var stdout, stderr bytes.Buffer
		if code := run([]string{"render", file, "--page", page, "--scale", "1", "-o", out}, &stdout, &stderr); code != 0 {
			t.Fatalf("render %s: exit %d: %s", page, code, stderr.String())
		}
		if w, h, _ := decodeRenderPNG(t, out); w < 611 || w > 613 || h < 791 || h > 793 {
			t.Errorf("render %s PNG = %d x %d, want 612 x 792 within 1 px", page, w, h)
		}
	}
}

// isBlank reports whether img is all white.
func isBlank(img image.Image) bool {
	bounds := img.Bounds()
	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			r, g, b, _ := img.At(x, y).RGBA()
			if r>>8 != 255 || g>>8 != 255 || b>>8 != 255 {
				return false
			}
		}
	}
	return true
}

// TestRenderOverlay checks the RN2 overlay row: --overlay ids draws
// one mark per object, with the outline on V1 shape's box top edge at
// page0/s1's box [108,126,72,36].
func TestRenderOverlay(t *testing.T) {
	input := fixtureBytes(t, "v1-minimal")
	marks, err := pageMarks(input, "page0", nil, false)
	if err != nil {
		t.Fatalf("pageMarks(V1): %v", err)
	}
	if len(marks) != 1 || marks[0].ID != "page0/s1" {
		t.Fatalf("marks = %+v, want one mark for page0/s1", marks)
	}
	if marks[0].Box != [4]float64{108, 126, 72, 36} {
		t.Errorf("mark box = %v, want [108 126 72 36]", marks[0].Box)
	}
	v5, err := pageMarks(fixtureBytes(t, "v5-glue"), "page0", nil, true)
	if err != nil {
		t.Fatalf("pageMarks(V5): %v", err)
	}
	if len(v5) != 6 {
		t.Errorf("V5 marks = %d, want one per object (6)", len(v5))
	}
	if got := v5[2]; len(got.Path) != 2 || len(got.Ends) != 2 {
		t.Errorf("connector mark = %+v, want a path and two glue ends", got)
	}
	if len(v5[0].Sites) != 2 {
		t.Errorf("shape mark sites = %v, want two connection sites", v5[0].Sites)
	}
	needRenderer(t)
	out := filepath.Join(t.TempDir(), "over.png")
	var stdout, stderr bytes.Buffer
	args := []string{"render", vsdxtest.Fixture(t, "v1-minimal"), "--page", "page0", "--overlay", "ids", "--scale", "1", "-o", out}
	if code := run(args, &stdout, &stderr); code != 0 {
		t.Fatalf("render overlay: exit %d: %s", code, stderr.String())
	}
	_, _, at := decodeRenderPNG(t, out)
	if r, g, b := at(108+36, 126+36); r != 230 || g != 0 || b != 200 {
		t.Errorf("overlay outline at the box's bottom edge = %d,%d,%d, want 230,0,200", r, g, b)
	}
}

// fixtureBytes returns the bytes of the vsdx fixture name.
func fixtureBytes(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(vsdxtest.Fixture(t, name))
	if err != nil {
		t.Fatalf("read fixture %s: %v", name, err)
	}
	return b
}

// TestRenderNotFound checks the RN2 page7 row: render --page page7
// fails with exit 1, a not_found error at --page.
func TestRenderNotFound(t *testing.T) {
	var stdout, stderr bytes.Buffer
	args := []string{"render", vsdxtest.Fixture(t, "v1-minimal"), "--page", "page7", "-o", filepath.Join(t.TempDir(), "x.png")}
	if code := run(args, &stdout, &stderr); code != 1 {
		t.Fatalf("render page7: exit %d, want 1: %s", code, stderr.String())
	}
	for _, want := range []string{`"code":"not_found"`, `"path":"--page"`} {
		if !strings.Contains(stderr.String(), want) {
			t.Errorf("stderr = %q, want it to contain %s", stderr.String(), want)
		}
	}
}

// TestRenderUsage checks the RN2 usage rows: without --page or -o,
// with a bad page, overlay or scale, render fails with exit 2.
func TestRenderUsage(t *testing.T) {
	for _, tc := range []struct {
		name string
		args []string
	}{
		{"no page", []string{"render", "x.vsdx", "-o", "x.png"}},
		{"bad page", []string{"render", "x.vsdx", "--page", "slide1", "-o", "x.png"}},
		{"no out", []string{"render", "x.vsdx", "--page", "page0"}},
		{"bad overlay", []string{"render", "x.vsdx", "--page", "page0", "--overlay", "bogus", "-o", "x.png"}},
		{"bad scale", []string{"render", "x.vsdx", "--page", "page0", "--scale", "0", "-o", "x.png"}},
		{"scale inf", []string{"render", "x.vsdx", "--page", "page0", "--scale", "Inf", "-o", "x.png"}},
		{"scale nan", []string{"render", "x.vsdx", "--page", "page0", "--scale", "NaN", "-o", "x.png"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			if code := run(tc.args, &stdout, &stderr); code != 2 {
				t.Errorf("exit %d, want 2: %s", code, stderr.String())
			}
			if !strings.Contains(stderr.String(), `"code":"usage"`) {
				t.Errorf("stderr = %q, want a usage error", stderr.String())
			}
		})
	}
}

// TestRenderPageUsage checks the CC10 row: render --page slide1
// is usage at --page with the want hint.
func TestRenderPageUsage(t *testing.T) {
	var stdout, stderr bytes.Buffer
	args := []string{"render", vsdxtest.Fixture(t, "v1-minimal"), "--page", "slide1", "-o", filepath.Join(t.TempDir(), "x.png")}
	if code := run(args, &stdout, &stderr); code != 2 {
		t.Fatalf("render slide1: exit %d, want 2: %s", code, stderr.String())
	}
	for _, want := range []string{`"code":"usage"`, `"path":"--page"`, `want page<id>, as read lists them`} {
		if !strings.Contains(stderr.String(), want) {
			t.Errorf("stderr = %q, want it to contain %s", stderr.String(), want)
		}
	}
}

// TestScalingMarks checks the scaling row: pageMarks on Generated(1000)
// and Generated(4000) (N = 1000, 4N = 4000 shapes) allocates at most
// 5.5 times as much (laws.Scales) and the laws.TimeScales time ratio is at
// most 8.
func TestScalingMarks(t *testing.T) {
	setup := func(n int) (func() error, error) {
		data, err := os.ReadFile(vsdxtest.Generated(t, n))
		if err != nil {
			return nil, err
		}
		return func() error {
			marks, err := pageMarks(data, "page0", nil, true)
			if err != nil {
				return err
			}
			if want := n + n/2; len(marks) != want {
				return fmt.Errorf("Generated(%d): %d marks, want %d", n, len(marks), want)
			}
			return nil
		}, nil
	}
	laws.Scales(t, "vsdx render marks", setup)
	laws.TimeScales(t, "vsdx render marks", setup)
}

// TestRenderUnreadable checks the RNF rows: render of v10-defects
// page2 and page4 (whose parts cannot be read) fails with exit 1,
// unreadable_target at --page, whatever the overlay, and writes no
// file. It runs without LibreOffice: the check is before it.
func TestRenderUnreadable(t *testing.T) {
	file := vsdxtest.Fixture(t, "v10-defects")
	for _, tc := range []struct {
		name string
		page string
		args []string
	}{
		{"plain page2", "page2", nil},
		{"overlay page2", "page2", []string{"--overlay", "ids"}},
		{"plain page4", "page4", nil},
		{"overlay page4", "page4", []string{"--overlay", "ids,sites,glue"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			out := filepath.Join(t.TempDir(), "x.png")
			var stdout, stderr bytes.Buffer
			args := []string{"render", file, "--page", tc.page}
			args = append(args, tc.args...)
			args = append(args, "-o", out)
			if code := run(args, &stdout, &stderr); code != 1 {
				t.Fatalf("render %s: exit %d, want 1: %s", tc.page, code, stderr.String())
			}
			for _, want := range []string{`"code":"unreadable_target"`, `"path":"--page"`} {
				if !strings.Contains(stderr.String(), want) {
					t.Errorf("stderr = %q, want it to contain %s", stderr.String(), want)
				}
			}
			if stdout.Len() != 0 {
				t.Errorf("stdout = %q, want empty on failure", stdout.String())
			}
			if _, err := os.Stat(out); !os.IsNotExist(err) {
				t.Errorf("output file exists after a refused render: stat err = %v", err)
			}
		})
	}
}

// TestRenderNoSoffice checks the row: render --renderer libreoffice
// finds soffice from DD_SOFFICE (then PATH), as xlsx --recalc does;
// when LibreOffice cannot run (here DD_SOFFICE names a missing file)
// it is operation_failed, exit 1, and writes nothing. (The default,
// --renderer auto, draws with the svg renderer instead:
// TestRenderAutoWithoutSoffice.)
func TestRenderNoSoffice(t *testing.T) {
	t.Setenv("DD_SOFFICE", filepath.Join(t.TempDir(), "no-soffice"))
	out := filepath.Join(t.TempDir(), "x.png")
	var stdout, stderr bytes.Buffer
	args := []string{"render", vsdxtest.Fixture(t, "v1-minimal"), "--page", "page0", "--renderer", "libreoffice", "-o", out}
	if code := run(args, &stdout, &stderr); code != 1 {
		t.Fatalf("render without soffice: exit %d, want 1: %s", code, stderr.String())
	}
	if !strings.Contains(stderr.String(), `"code":"operation_failed"`) || !strings.Contains(stderr.String(), `"path":"-o"`) {
		t.Errorf("stderr = %q, want operation_failed", stderr.String())
	}
	if _, err := os.Stat(out); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("render without soffice wrote %s", out)
	}
}

// TestRenderErrNoSofficeIsOperationFailed checks that a missing
// LibreOffice or rasteriser maps to operation_failed with the
// DD_SOFFICE hint.
func TestRenderErrNoSofficeIsOperationFailed(t *testing.T) {
	for _, err := range []error{render.ErrNoSoffice, render.ErrNoRasteriser} {
		ce := rendererError(err)
		if ce.Code != cli.CodeOperationFailed || ce.Path != "-o" || !strings.Contains(ce.Hint, "DD_SOFFICE") {
			t.Errorf("rendererError(%v) = %s %q, want operation_failed with a DD_SOFFICE hint", err, ce.Code, ce.Hint)
		}
	}
}
