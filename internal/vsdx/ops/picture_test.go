package ops_test

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jhnoel/documentdriver/internal/vsdx/ops"
	"github.com/jhnoel/documentdriver/internal/vsdx/vsdxtest"
)

// pngFile writes a w x h PNG (its bytes vary with seed) and returns its path.
func pngFile(t *testing.T, w, h int, seed uint8) string {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	img.Pix[0], img.Pix[3] = seed, 255
	var b bytes.Buffer
	if err := png.Encode(&b, img); err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(t.TempDir(), "i.png")
	if err := os.WriteFile(p, b.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

// mediaParts lists the /visio/media/ members of path.
func mediaParts(t *testing.T, path string) []string {
	t.Helper()
	z, err := zip.OpenReader(path)
	if err != nil {
		t.Fatal(err)
	}
	defer z.Close()
	var out []string
	for _, f := range z.File {
		if strings.HasPrefix(f.Name, "visio/media/") {
			out = append(out, f.Name)
		}
	}
	return out
}

// TestPictureAddReadBack checks the Pictures rows: pin alone is the
// pixel size at 96 per inch (96 x 48 px is 72 x 36 pt); box is taken as
// given; crop reads back; two pictures of the same bytes share one
// media part; sending read's image and crop back writes nothing.
func TestPictureAddReadBack(t *testing.T) {
	img := pngFile(t, 96, 48, 1)
	out, _ := refusalOut(t, vsdxtest.Blank(t), `{"ops":[{"op":"object.add","at":"page0","kind":"picture","image":"`+img+`","pin":[200,200]},{"op":"object.add","at":"page0","kind":"picture","image":"`+img+`","box":[10,10,50,50],"crop":[10,0,10,20]}]}`)
	s1, s2 := readObj(t, out, "page0/s1"), readObj(t, out, "page0/s2")
	if string(s1["kind"]) != `"picture"` || string(s1["box"]) != `[164,182,72,36]` || string(s1["image"]) != `"/visio/media/image1.png"` {
		t.Errorf("s1 = kind %s box %s image %s", s1["kind"], s1["box"], s1["image"])
	}
	if string(s2["image"]) != `"/visio/media/image1.png"` || string(s2["crop"]) != `[10,0,10,20]` || string(s2["box"]) != `[10,10,50,50]` {
		t.Errorf("s2 = image %s crop %s box %s", s2["image"], s2["crop"], s2["box"])
	}
	if got := mediaParts(t, out); len(got) != 1 {
		t.Errorf("media parts = %v, want one shared part", got)
	}
	if _, changed := refusalOut(t, out, `{"ops":[{"op":"object.set","at":"page0/s2","image":"/visio/media/image1.png","crop":[10,0,10,20]},{"op":"object.set","at":"page0/s1","image":"`+img+`"}]}`); len(changed) != 0 {
		t.Errorf("identity: changed = %v", changed)
	}
}

// TestPictureReplaceAndDeleteRelease checks that a replaced or deleted
// picture releases its media: the page relationship goes when no
// shape uses it and the part when nothing targets it.
func TestPictureReplaceAndDeleteRelease(t *testing.T) {
	a, b := pngFile(t, 10, 10, 1), pngFile(t, 10, 10, 2)
	out, _ := refusalOut(t, vsdxtest.Blank(t), `{"ops":[{"op":"object.add","at":"page0","kind":"picture","image":"`+a+`","box":[0,0,10,10]},{"op":"object.add","at":"page0","kind":"picture","image":"`+b+`","box":[20,0,10,10]}]}`)
	replaced, _ := refusalOut(t, out, `{"ops":[{"op":"object.set","at":"page0/s1","image":"`+b+`"}]}`)
	if got := mediaParts(t, replaced); len(got) != 1 || got[0] != "visio/media/image2.png" {
		t.Errorf("after replace: media = %v, want only image2.png", got)
	}
	deleted, _ := refusalOut(t, replaced, `{"ops":[{"op":"object.delete","at":"page0/s1"}]}`)
	if got := mediaParts(t, deleted); len(got) != 1 {
		t.Errorf("after deleting one user: media = %v, want image2.png kept", got)
	}
	gone, _ := refusalOut(t, deleted, `{"ops":[{"op":"object.delete","at":"page0/s2"}]}`)
	if got := mediaParts(t, gone); len(got) != 0 {
		t.Errorf("after deleting the last user: media = %v, want none", got)
	}
}

// TestPictureRefusals checks the refusals: file faults come back
// together before the document opens; image or crop on a shape that is
// not a picture; a value naming both a part and a file; fields a
// picture does not take.
func TestPictureRefusals(t *testing.T) {
	notImage := filepath.Join(t.TempDir(), "x.png")
	if err := os.WriteFile(notImage, []byte("not an image"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, errs := ops.ParsePlan([]byte(`{"ops":[{"op":"object.add","at":"page0","kind":"picture","image":"/no/such.png","box":[0,0,1,1]},{"op":"object.add","at":"page0","kind":"picture","image":"` + notImage + `","box":[0,0,1,1]},{"op":"object.add","at":"page0","kind":"picture","image":"` + notImage + `","box":[0,0,1,1],"crop":[60,0,50,0]}]}`))
	b, _ := json.Marshal(errs)
	for _, want := range []string{`"code":"io","path":"ops[0].image"`, `"code":"invalid_operation","path":"ops[1].image"`, `"path":"ops[2].crop"`} {
		if !strings.Contains(string(b), want) {
			t.Errorf("errors %s lack %s (all faults together, before the drawing is read)", b, want)
		}
	}
	img := pngFile(t, 10, 10, 1)
	out, _ := refusalOut(t, vsdxtest.Blank(t), `{"ops":[{"op":"object.add","at":"page0","geom":"rect","box":[0,0,10,10]},{"op":"object.add","at":"page0","kind":"picture","image":"`+img+`","box":[20,0,10,10]}]}`)
	expectRefusal(t, out, `{"ops":[{"op":"object.set","at":"page0/s1","image":"`+img+`"}]}`, "image", "is not a picture")
	expectRefusal(t, out, `{"ops":[{"op":"object.set","at":"page0/s1","crop":[1,1,1,1]}]}`, "crop", "is not a picture")
	if _, errs := ops.ParsePlan([]byte(`{"ops":[{"op":"object.add","at":"page0","kind":"picture","image":"x.png","box":[0,0,1,1],"fill":"none"}]}`)); len(errs) != 1 || errs[0].Path != "ops[0].fill" {
		t.Errorf("fill on a picture: %v, want a fault at ops[0].fill", errs)
	}
}

// TestPictureAddNoneStyle is Fable's must-fix row: an added picture
// takes the drawing's "None" style sheet (ID 2 in the blank drawing,
// as V18 shape 5 and 60973 master24/s5 store LineStyle, FillStyle and
// TextStyle '2'), so it reads back with fill "none" and line "none"
// and has no connection points (the samples store none).
func TestPictureAddNoneStyle(t *testing.T) {
	img := pngFile(t, 10, 10, 1)
	out, _ := refusalOut(t, vsdxtest.Blank(t), `{"ops":[{"op":"object.add","at":"page0","kind":"picture","image":"`+img+`","box":[0,0,10,10]}]}`)
	o := readObj(t, out, "page0/s1")
	if string(o["fill"]) != `"none"` || string(o["line"]) != `"none"` {
		t.Errorf("picture fill %s line %s, want \"none\" and \"none\"", o["fill"], o["line"])
	}
	page := pagePart(t, out)
	if !strings.Contains(page, "LineStyle='2' FillStyle='2' TextStyle='2'") {
		t.Errorf("picture style attributes not the None sheet's 2: %s", page)
	}
	if strings.Contains(page, "N='Connection'") {
		t.Error("an added picture stores connection points; the samples have none")
	}
	if strings.Contains(page, `"`) {
		t.Errorf("page1.xml mixes quote styles: %s", page)
	}
}

// TestPictureAddNoNoneStyleFallback is the fallback row (designer's
// ruling): a drawing with no "None" style sheet gives the picture its
// default style and stores LinePattern 0 and FillPattern 0 locally, so
// it still reads fill "none" and line "none".
func TestPictureAddNoNoneStyleFallback(t *testing.T) {
	src := withoutNoneStyle(t, vsdxtest.Blank(t))
	img := pngFile(t, 10, 10, 1)
	out, _ := refusalOut(t, src, `{"ops":[{"op":"object.add","at":"page0","kind":"picture","image":"`+img+`","box":[0,0,10,10]}]}`)
	o := readObj(t, out, "page0/s1")
	if string(o["fill"]) != `"none"` || string(o["line"]) != `"none"` {
		t.Errorf("fallback picture fill %s line %s, want \"none\" and \"none\"", o["fill"], o["line"])
	}
}

// TestPictureFaultsWithPlanFaults checks that image-file faults come
// back together with the plan's other document-independent faults
// (ParsePlan), not after them.
func TestPictureFaultsWithPlanFaults(t *testing.T) {
	_, errs := ops.ParsePlan([]byte(`{"ops":[{"op":"object.add","at":"page0","kind":"picture","image":"/no/such.png","box":[0,0,1,1]},{"op":"object.set","at":"bogus","name":"x"}]}`))
	var paths []string
	for _, e := range errs {
		paths = append(paths, e.Path)
	}
	joined := strings.Join(paths, " ")
	if !strings.Contains(joined, "ops[0].image") || !strings.Contains(joined, "ops[1].at") {
		t.Errorf("errors at %v, want ops[0].image and ops[1].at together", paths)
	}
}

// pagePart returns visio/pages/page1.xml of path.
func pagePart(t *testing.T, path string) string {
	t.Helper()
	z, err := zip.OpenReader(path)
	if err != nil {
		t.Fatal(err)
	}
	defer z.Close()
	for _, f := range z.File {
		if f.Name == "visio/pages/page1.xml" {
			rc, err := f.Open()
			if err != nil {
				t.Fatal(err)
			}
			var b bytes.Buffer
			_, _ = b.ReadFrom(rc)
			rc.Close()
			return b.String()
		}
	}
	t.Fatal("no page1.xml")
	return ""
}

// withoutNoneStyle copies drawing src without its "None" style sheet
// (ID 2 in the blank drawing, which nothing else in it uses).
func withoutNoneStyle(t *testing.T, src string) string {
	t.Helper()
	z, err := zip.OpenReader(src)
	if err != nil {
		t.Fatal(err)
	}
	defer z.Close()
	var buf bytes.Buffer
	w := zip.NewWriter(&buf)
	for _, f := range z.File {
		rc, err := f.Open()
		if err != nil {
			t.Fatal(err)
		}
		var b bytes.Buffer
		_, _ = b.ReadFrom(rc)
		rc.Close()
		data := b.Bytes()
		if f.Name == "visio/document.xml" {
			s := string(data)
			i := strings.Index(s, "<StyleSheet ID='2'")
			j := strings.Index(s[i:], "</StyleSheet>")
			if i < 0 || j < 0 {
				t.Fatal("blank document.xml has no StyleSheet ID 2")
			}
			data = []byte(s[:i] + s[i+j+len("</StyleSheet>"):])
		}
		fw, err := w.Create(f.Name)
		if err != nil {
			t.Fatal(err)
		}
		_, _ = fw.Write(data)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(t.TempDir(), "nonone.vsdx")
	if err := os.WriteFile(out, buf.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}
	return out
}
