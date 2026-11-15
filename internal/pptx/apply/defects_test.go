package apply

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jhnoel/documentdriver/internal/cli"
	"github.com/jhnoel/documentdriver/internal/opc"
	"github.com/jhnoel/documentdriver/internal/pptx/model"
	"github.com/jhnoel/documentdriver/internal/pptx/view"
	"github.com/jhnoel/documentdriver/internal/testdeck"
)

// The tests here hold the rule for documents with defects
// (docs/contract.md, section 10): a defect the input already had never
// blocks a read, an edit elsewhere or the save.

func slideSource(t *testing.T) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(testdeck.Root(), "testdata", "decks", "minimal", "ppt", "slides", "slide1.xml"))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func readJSON(t *testing.T, path string) string {
	t.Helper()
	p, err := opc.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	d, err := model.OpenDeck(p)
	if err != nil {
		t.Fatal(err)
	}
	part, err := d.ResolveSlide("slide1")
	if err != nil {
		t.Fatal(err)
	}
	s, err := d.ReadSlide(part)
	if err != nil {
		t.Fatalf("read with a defect elsewhere: %v", err)
	}
	b, err := view.JSON(s, view.Options{})
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// TestUnreadableLayoutIsANote: a layout, master or theme that is not
// well-formed XML is left out with a note; the slide is still read, and
// an edit of an object on it still applies and saves.
func TestUnreadableLayoutIsANote(t *testing.T) {
	for _, part := range []string{"ppt/slideLayouts/slideLayout1.xml", "ppt/slideMasters/slideMaster1.xml", "ppt/theme/theme1.xml"} {
		path := testdeck.MinimalWith(t, map[string][]byte{part: []byte("<p:broken")})
		got := readJSON(t, path)
		if !strings.Contains(got, `"at":"slide1","reason":"`) || !strings.Contains(got, "unreadable") || !strings.Contains(got, `"id":"slide1/s2"`) {
			t.Errorf("%s: read does not note the unreadable part or lost the objects:\n%s", part, got)
		}
		res, err := Run(path, []byte(`{"ops": [{"op": "object.move", "at": "slide1/s2", "dx": 10}]}`), Options{})
		if err != nil {
			t.Errorf("%s: an edit elsewhere is blocked: %v", part, err)
			continue
		}
		if len(res.Changed) != 1 || res.Saved != path {
			t.Errorf("%s: changed %d, saved %q", part, len(res.Changed), res.Saved)
		}
	}
}

// TestInvalidExistingValueCanBeOverwritten: a value the file already has
// wrong (a rotation that is not a number, a preset that does not exist,
// a boolean that is neither) does not stop an operation from setting it,
// nor from changing something else on the same object.
func TestInvalidExistingValueCanBeOverwritten(t *testing.T) {
	src := slideSource(t)
	for _, c := range []struct{ name, old, bad, op, want string }{
		{"rot", `<a:xfrm><a:off x="914400" y="1828800"/>`, `<a:xfrm rot="sideways"><a:off x="914400" y="1828800"/>`,
			`{"op": "object.set", "at": "slide1/s2", "rot": 15}`, `rot="900000"`},
		{"flip", `<a:xfrm><a:off x="914400" y="1828800"/>`, `<a:xfrm flipH="maybe"><a:off x="914400" y="1828800"/>`,
			`{"op": "object.set", "at": "slide1/s2", "flip_h": true}`, `flipH="1"`},
		{"geom", `<a:prstGeom prst="rect">`, `<a:prstGeom prst="blob">`,
			`{"op": "object.set", "at": "slide1/s2", "geom": "ellipse"}`, `prst="ellipse"`},
		{"anchor", `<a:bodyPr anchor="ctr"/>`, `<a:bodyPr anchor="middle"/>`,
			`{"op": "object.set", "at": "slide1/s2", "text": {"anchor": "b"}}`, `anchor="b"`},
		{"elsewhere on the object", `<a:xfrm><a:off x="914400" y="1828800"/>`, `<a:xfrm rot="sideways"><a:off x="914400" y="1828800"/>`,
			`{"op": "object.set", "at": "slide1/s2", "name": "Renamed"}`, `name="Renamed"`},
		{"another object", `<a:xfrm><a:off x="914400" y="1828800"/>`, `<a:xfrm rot="sideways"><a:off x="914400" y="1828800"/>`,
			`{"op": "object.move", "at": "slide1/s3", "dx": 10}`, `rot="sideways"`},
	} {
		if !bytes.Contains(src, []byte(c.old)) {
			t.Fatalf("%s: the fixture lacks %s", c.name, c.old)
		}
		path := testdeck.MinimalWith(t, map[string][]byte{"ppt/slides/slide1.xml": bytes.Replace(src, []byte(c.old), []byte(c.bad), 1)})
		readJSON(t, path) // the read model takes the defect without failing
		if _, err := Run(path, []byte(`{"ops": [`+c.op+`]}`), Options{}); err != nil {
			t.Errorf("%s: %v", c.name, err)
			continue
		}
		p, err := opc.Open(path)
		if err != nil {
			t.Fatal(err)
		}
		pt, err := p.Part("/ppt/slides/slide1.xml")
		if err != nil {
			t.Fatal(err)
		}
		got, err := pt.Bytes()
		p.Close()
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Contains(got, []byte(c.want)) {
			t.Errorf("%s: the saved slide lacks %s", c.name, c.want)
		}
	}
}

// TestUnreadableTargetRefusesAtTheOp: an operation that needs a value of
// its own target that the file stores wrong is refused at that operation
// with unreadable_target, not at save time, and nothing is written.
func TestUnreadableTargetRefusesAtTheOp(t *testing.T) {
	src := slideSource(t)
	bad := bytes.Replace(src, []byte(`<a:xfrm><a:off x="914400" y="1828800"/>`), []byte(`<a:xfrm><a:off x="left" y="1828800"/>`), 1)
	path := testdeck.MinimalWith(t, map[string][]byte{"ppt/slides/slide1.xml": bad})
	before, _ := os.ReadFile(path)
	_, err := Run(path, []byte(`{"ops": [{"op": "object.move", "at": "slide1/s3", "dx": 1}, {"op": "object.move", "at": "slide1/s2", "dx": 10}]}`), Options{})
	var e *cli.Error
	if !errors.As(err, &e) || e.Code != cli.CodeUnreadableTarget || e.Path != "ops[1].at" || e.Detail == "" {
		t.Fatalf("got %v, want unreadable_target at ops[1].at with a detail", err)
	}
	if after, _ := os.ReadFile(path); !bytes.Equal(before, after) {
		t.Error("a refused plan wrote the deck")
	}
}
