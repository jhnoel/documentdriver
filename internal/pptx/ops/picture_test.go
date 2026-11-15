package ops

import (
	"errors"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jhnoel/documentdriver/internal/opc"
	"github.com/jhnoel/documentdriver/internal/pptx/ops/prop"
	"github.com/jhnoel/documentdriver/internal/testdeck"
)

// picFiles writes a 3x2 PNG and an SVG authored here.
func picFiles(t *testing.T) (pngPath, svgPath string) {
	t.Helper()
	dir := t.TempDir()
	img := image.NewRGBA(image.Rect(0, 0, 3, 2))
	img.Set(1, 1, color.RGBA{0xFF, 0, 0, 0xFF})
	pngPath = filepath.Join(dir, "x.png")
	f, err := os.Create(pngPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := png.Encode(f, img); err != nil {
		t.Fatal(err)
	}
	f.Close()
	svgPath = filepath.Join(dir, "x.svg")
	if err := os.WriteFile(svgPath, []byte(`<svg xmlns="http://www.w3.org/2000/svg" width="3" height="2"/>`), 0o644); err != nil {
		t.Fatal(err)
	}
	return pngPath, svgPath
}

// TestAddPictureWritesWhatPowerPointWrites: a picture added with an SVG
// holds the template, the image relationship, the SVG extension exactly
// as PowerPoint 16 writes it, and nothing the plan did not give.
func TestAddPictureWritesWhatPowerPointWrites(t *testing.T) {
	pngPath, svgPath := picFiles(t)
	path := testdeck.Minimal(t)
	if err := Apply(path, compile(t, `{"ops": [
	  {"op": "object.add", "at": "slide1", "kind": "picture", "name": "P", "box": [10, 20, 30, 40],
	   "image": "`+pngPath+`", "svg": "`+svgPath+`"}]}`)...); err != nil {
		t.Fatal(err)
	}
	x := slideXML(t, path)
	want := `<p:pic><p:nvPicPr><p:cNvPr id="6" name="P"/><p:cNvPicPr/><p:nvPr/></p:nvPicPr><p:blipFill><a:blip r:embed="rId3"><a:extLst><a:ext uri="{96DAC541-7B7A-43D3-8B79-37D633B846F1}"><asvg:svgBlip xmlns:asvg="http://schemas.microsoft.com/office/drawing/2016/SVG/main" r:embed="rId4"/></a:ext></a:extLst></a:blip></p:blipFill><p:spPr><a:xfrm><a:off x="127000" y="254000"/><a:ext cx="381000" cy="508000"/></a:xfrm></p:spPr></p:pic>`
	if !strings.Contains(x, want) {
		t.Fatalf("slide holds:\n%s\nwant:\n%s", x, want)
	}
	p, err := opc.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	rels, _ := p.Rels("/ppt/slides/slide1.xml")
	for id, target := range map[string]string{"rId3": "../media/image2.png", "rId4": "../media/image3.svg"} {
		r, ok := rels.Get(id)
		if !ok || r.Target != target || r.Type != "http://schemas.openxmlformats.org/officeDocument/2006/relationships/image" {
			t.Errorf("%s = %+v, want an image relationship to %s", id, r, target)
		}
	}
	ct, _ := p.ContentTypes()
	if v, ok := ct.Default("svg"); !ok || v != "image/svg+xml" {
		t.Errorf("svg Default = %q", v)
	}
	if _, ok := ct.Override("/ppt/media/image2.png"); ok {
		t.Error("an Override for a part its extension's Default covers")
	}
}

// TestPictureRefusals: plans that would need a choice the tool does not
// make, or a value it cannot store, fail and write nothing.
func TestPictureRefusals(t *testing.T) {
	pngPath, svgPath := picFiles(t)
	path := testdeck.Minimal(t)
	if err := Apply(path, compile(t, `{"ops": [
	  {"op": "object.set", "at": "slide1/s5", "svg": "`+svgPath+`"}]}`)...); err != nil {
		t.Fatal(err)
	}
	before, _ := os.ReadFile(path)
	for _, c := range []struct {
		op   string
		want error
		msg  string
	}{
		{`{"op": "object.set", "at": "slide1/s5", "image": "` + pngPath + `"}`, nil, "give svg too"},
		{`{"op": "object.set", "at": "slide1/s5", "image": "` + svgPath + `", "svg": "none"}`, nil, "give it as svg"},
		{`{"op": "object.set", "at": "slide1/s5", "svg": "` + pngPath + `"}`, nil, "not an SVG"},
		{`{"op": "object.set", "at": "slide1/s5", "fit": "tile"}`, prop.ErrReadOnly, "tile"},
		{`{"op": "object.set", "at": "slide1/s2", "crop": [1, 1, 1, 1]}`, nil, "for pictures"},
		{`{"op": "object.set", "at": "slide1/s5", "image": "/no/such/file.png", "svg": "none"}`, nil, "not a readable file"},
	} {
		err := Apply(path, compile(t, `{"ops": [`+c.op+`]}`)...)
		if err == nil || c.want != nil && !errors.Is(err, c.want) || !strings.Contains(err.Error(), c.msg) {
			t.Errorf("%s: err = %v, want %v naming %q", c.op, err, c.want, c.msg)
		}
		if after, _ := os.ReadFile(path); string(after) != string(before) {
			t.Fatalf("%s: the refused plan changed the file", c.op)
		}
	}
	for _, c := range []struct{ op, msg string }{
		{`{"op": "object.set", "at": "slide1/s5", "locks": {"noSuchLock": true}}`, "noSuchLock"},
		{`{"op": "object.set", "at": "slide1/s5", "crop": [3000000, 0, 0, 0]}`, "crop"},
		{`{"op": "object.set", "at": "slide1/s5", "fit": "zoom"}`, "fit"},
		{`{"op": "object.add", "at": "slide1", "kind": "shape", "name": "x", "geom": "rect", "box": [1,2,3,4], "image": "a.png"}`, "for pictures"},
		{`{"op": "object.add", "at": "slide1", "kind": "picture", "name": "x", "box": [1,2,3,4], "image": "a.png", "fill": "none"}`, "fill"},
	} {
		p, err := DecodePlan(strings.NewReader(`{"ops": [` + c.op + `]}`))
		if err != nil {
			t.Fatal(err)
		}
		if _, err := p.Compile(); err == nil || !strings.Contains(err.Error(), c.msg) {
			t.Errorf("%s: compile err = %v, want one naming %q", c.op, err, c.msg)
		}
	}
}

func TestRelPaths(t *testing.T) {
	for _, c := range [][3]string{
		{"/ppt/slides/slide1.xml", "/ppt/media/image3.png", "../media/image3.png"},
		{"/ppt/slides/slide1.xml", "/ppt/slides/x.png", "x.png"},
		{"/slide.xml", "/ppt/media/a.png", "ppt/media/a.png"},
	} {
		if got := relTarget(c[0], c[1]); got != c[2] {
			t.Errorf("relTarget(%s, %s) = %s, want %s", c[0], c[1], got, c[2])
		}
	}
	for name, want := range map[string]string{"/ppt/slides/_rels/slide1.xml.rels": "/ppt/slides/slide1.xml", "/_rels/.rels": "/"} {
		if got, ok := relsSource(name); !ok || got != want {
			t.Errorf("relsSource(%s) = %s, %v", name, got, ok)
		}
	}
	if _, ok := relsSource("/ppt/slides/slide1.xml"); ok {
		t.Error("a slide is not a relationships part")
	}
}
