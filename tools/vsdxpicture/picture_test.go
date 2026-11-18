package vsdxpicture

import (
	"bytes"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jhnoel/documentdriver/internal/repo"
	"github.com/jhnoel/documentdriver/internal/vsdx/apply"
	"github.com/jhnoel/documentdriver/internal/vsdx/ops"
	"github.com/jhnoel/documentdriver/internal/vsdx/vsdxtest"
)

// writeImages writes a 40 x 20 PNG and a 30 x 30 JPEG into dir.
func writeImages(t *testing.T, dir string) (string, string) {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 40, 20))
	for x := range 40 {
		for y := range 20 {
			img.Set(x, y, color.RGBA{R: 200, G: uint8(x * 5), B: 40, A: 255})
		}
	}
	var pb, jb bytes.Buffer
	if err := png.Encode(&pb, img); err != nil {
		t.Fatal(err)
	}
	if err := jpeg.Encode(&jb, image.NewRGBA(image.Rect(0, 0, 30, 30)), nil); err != nil {
		t.Fatal(err)
	}
	p, j := filepath.Join(dir, "a.png"), filepath.Join(dir, "b.jpeg")
	if err := os.WriteFile(p, pb.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(j, jb.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}
	return p, j
}

// run applies plan to src, writing out.
func run(t *testing.T, src, out, plan string) {
	t.Helper()
	p, errs := ops.ParsePlan([]byte(plan))
	if len(errs) > 0 {
		t.Fatal(errs)
	}
	if _, err := apply.Run(src, p, apply.Options{Out: out}); err != nil {
		t.Fatalf("apply %s: %v", plan, err)
	}
}

// schemaKnownNew is the one error kind a picture adds that the schema
// reports on every Visio file's page Rel too: MS-VSDX's Rel_Type
// declares no attribute, so r:id on ForeignData/Rel (as on Page/Rel,
// which every drawing has) is reported.
const schemaKnownNew = "/PageContents/Shapes/Shape/ForeignData/Rel: 'r:id' attribute not allowed for element"

// TestPictureWritesValidate checks every picture write against the
// MS-VSDX XSD (the opt-in differential check): the input is a drawing
// with a master-less rect, the output adds PNG and JPEG pictures (pin,
// box, crop), replaces an image and deletes a picture. The output may
// add no error kind but schemaKnownNew. It skips only when uv or the
// schema cache is absent.
func TestPictureWritesValidate(t *testing.T) {
	if _, err := exec.LookPath("uv"); err != nil {
		t.Skip("uv not on PATH")
	}
	root, err := repo.Root()
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	pngPath, jpegPath := writeImages(t, dir)
	in := filepath.Join(dir, "in.vsdx")
	run(t, vsdxtest.Blank(t), in, `{"ops":[{"op":"object.add","at":"page0","geom":"rect","box":[300,300,72,72]}]}`)
	out := filepath.Join(dir, "out.vsdx")
	run(t, in, out, `{"ops":[
		{"op":"object.add","at":"page0","kind":"picture","image":"`+pngPath+`","pin":[100,100]},
		{"op":"object.add","at":"page0","kind":"picture","image":"`+jpegPath+`","box":[200,100,60,60],"crop":[10,5,10,15]},
		{"op":"object.add","at":"page0","kind":"picture","image":"`+pngPath+`","box":[10,400,40,20],"as":"p3"},
		{"op":"object.set","at":"$p3","image":"`+jpegPath+`"},
		{"op":"object.add","at":"page0","kind":"picture","image":"`+pngPath+`","box":[10,500,40,20],"as":"p4"},
		{"op":"object.delete","at":"$p4"}]}`)
	cmd := exec.Command("uv", "run", "--script", filepath.Join(root, "tools", "conformance", "vsdx-schema-check.py"), "--input", in, "--output", out) //nolint:gosec // G204: fixed script and test paths
	got, err := cmd.CombinedOutput()
	text := string(got)
	if strings.Contains(text, "no schema cache") {
		t.Skip("no MS-VSDX schema cache (make vsdx-schema)")
	}
	for _, line := range strings.Split(text, "\n") {
		if strings.HasPrefix(line, "NEW ") && !strings.HasSuffix(line, schemaKnownNew) {
			t.Errorf("schema: %s", line)
		}
	}
	if err != nil && !strings.Contains(text, schemaKnownNew) {
		t.Fatalf("schema check: %v\n%s", err, text)
	}
}

// TestPictureRendersInLibreOffice checks that LibreOffice imports and
// draws a drawing with a PNG picture vsdx added: vsdx render of the
// page succeeds and the picture's red is in the raster. LibreOffice is
// a render check, not proof Visio opens it (VE18). It skips only when
// LibreOffice is absent.
func TestPictureRendersInLibreOffice(t *testing.T) {
	soffice := os.Getenv("DD_SOFFICE")
	if soffice == "" {
		if p, err := exec.LookPath("soffice"); err == nil {
			soffice = p
		} else if _, err := os.Stat("/Applications/LibreOffice.app/Contents/MacOS/soffice"); err == nil {
			soffice = "/Applications/LibreOffice.app/Contents/MacOS/soffice"
		} else {
			t.Skip("LibreOffice not found")
		}
	}
	root, err := repo.Root()
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	pngPath, _ := writeImages(t, dir)
	out := filepath.Join(dir, "pic.vsdx")
	run(t, vsdxtest.Blank(t), out, `{"ops":[{"op":"object.add","at":"page0","kind":"picture","image":"`+pngPath+`","box":[100,100,200,100]}]}`)
	bin := filepath.Join(dir, "vsdx")
	build := exec.Command("go", "build", "-o", bin, "./cmd/vsdx") //nolint:gosec // G204: fixed arguments
	build.Dir = root
	if b, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build: %v %s", err, b)
	}
	pngOut := filepath.Join(dir, "page.png")
	r := exec.Command(bin, "render", out, "--page", "page0", "-o", pngOut, "--scale", "1") //nolint:gosec // G204: the built binary
	r.Env = append(os.Environ(), "DD_SOFFICE="+soffice)
	if b, err := r.CombinedOutput(); err != nil {
		if strings.Contains(string(b), "rasteriser") {
			t.Skip("no PDF rasteriser")
		}
		t.Fatalf("render: %v %s", err, b)
	}
	f, err := os.Open(pngOut)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	img, err := png.Decode(f)
	if err != nil {
		t.Fatal(err)
	}
	rr, g, b, _ := img.At(200, 150).RGBA()
	if !(rr>>8 > 150 && b>>8 < 100 && g>>8 < 230) {
		t.Errorf("render at the picture's centre = (%d,%d,%d), want the picture's red", rr>>8, g>>8, b>>8)
	}
}
