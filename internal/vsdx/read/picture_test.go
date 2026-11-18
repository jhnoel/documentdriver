package read

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/jhnoel/documentdriver/internal/vsdx/vsdxtest"
)

// pictureObj returns object id of container in a corpus drawing.
func pictureObj(t *testing.T, rel, container, id string) map[string]json.RawMessage {
	t.Helper()
	dir := vsdxCorpus()
	if dir == "" {
		t.Skip("no vsdx corpus (set VSDX_CORPUS)")
	}
	input, err := os.ReadFile(filepath.Join(dir, "repos", filepath.FromSlash(rel)))
	if err != nil {
		t.Fatalf("corpus file: %v", err)
	}
	out, err := Page(input, "sha256:x", container, Options{})
	if err != nil {
		t.Fatal(err)
	}
	var page struct {
		Objects []map[string]json.RawMessage `json:"objects"`
	}
	if err := json.Unmarshal(out, &page); err != nil {
		t.Fatal(err)
	}
	for _, o := range page.Objects {
		if string(o["id"]) == `"`+id+`"` {
			return o
		}
	}
	t.Fatalf("%s not in %s", id, container)
	return nil
}

// TestPictureReadEMF is the row on 60973.vsdx: master24 (part
// masters/master22.xml) s5 is kind picture, image
// /visio/media/image3.emf (the masters.xml ID 24 Rel targets
// master22.xml), image/x-emf, with no px.
func TestPictureReadEMF(t *testing.T) {
	o := pictureObj(t, "apache-poi/test-data/diagram/60973.vsdx", "master24", "master24/s5")
	if string(o["kind"]) != `"picture"` || string(o["image"]) != `"/visio/media/image3.emf"` {
		t.Fatalf("kind %s image %s, want picture /visio/media/image3.emf", o["kind"], o["image"])
	}
	var m map[string]any
	if err := json.Unmarshal(o["media"], &m); err != nil {
		t.Fatal(err)
	}
	if m["type"] != "image/x-emf" || m["px"] != nil {
		t.Errorf("media = %v, want image/x-emf and no px", m)
	}
}

// TestPictureReadJPEGCrop is the row on fixture V18 (derived from a
// Visio-authored drawing, see its PROVENANCE.md): page0/s5 is
// /visio/media/image3.jpeg, 229,833 bytes, px [1142, 1600], and its Img
// cells give crop [2.27, 9.6, 4.03, 23.52].
func TestPictureReadJPEGCrop(t *testing.T) {
	input, err := os.ReadFile(vsdxtest.Fixture(t, "v18-jpeg"))
	if err != nil {
		t.Fatal(err)
	}
	out, err := Page(input, "sha256:x", "page0", Options{})
	if err != nil {
		t.Fatal(err)
	}
	var page struct {
		Objects []map[string]json.RawMessage `json:"objects"`
	}
	if err := json.Unmarshal(out, &page); err != nil || len(page.Objects) != 1 {
		t.Fatalf("page: %v, %d objects", err, len(page.Objects))
	}
	o := page.Objects[0]
	if string(o["id"]) != `"page0/s5"` || string(o["kind"]) != `"picture"` || string(o["image"]) != `"/visio/media/image3.jpeg"` {
		t.Fatalf("object = %s %s %s", o["id"], o["kind"], o["image"])
	}
	if got := string(o["media"]); got != `{"type":"image/jpeg","bytes":229833,"sha256":"d023efcced8e9b71cd8b4361229ee6f6055eb1f9332ed9d9587f5fc2c7b9207a","px":[1142,1600]}` {
		t.Errorf("media = %s", got)
	}
	if got := string(o["crop"]); got != `[2.27,9.6,4.03,23.52]` {
		t.Errorf("crop = %s, want [2.27,9.6,4.03,23.52]", got)
	}
}

// TestMediaExtract checks vsdx media's read side on V18: by picture
// address and by part name it writes the part's bytes (the hash read
// shows); a non-picture address, a non-media part and a page are
// not_found and write nothing.
func TestMediaExtract(t *testing.T) {
	input, err := os.ReadFile(vsdxtest.Fixture(t, "v18-jpeg"))
	if err != nil {
		t.Fatal(err)
	}
	for _, addr := range []string{"page0/s5", "/visio/media/image3.jpeg"} {
		var buf bytes.Buffer
		res, err := Media(input, "sha256:x", addr, &buf)
		if err != nil {
			t.Fatalf("%s: %v", addr, err)
		}
		if res.SHA256 != "d023efcced8e9b71cd8b4361229ee6f6055eb1f9332ed9d9587f5fc2c7b9207a" || res.Bytes != int64(buf.Len()) || buf.Len() != 229833 {
			t.Errorf("%s: %+v, %d bytes written", addr, res, buf.Len())
		}
	}
	for _, addr := range []string{"page0/s1", "/visio/pages/page1.xml", "page0"} {
		var buf bytes.Buffer
		if _, err := Media(input, "sha256:x", addr, &buf); err == nil || buf.Len() != 0 {
			t.Errorf("%s: want not_found and nothing written, got %v, %d bytes", addr, err, buf.Len())
		}
	}
}
