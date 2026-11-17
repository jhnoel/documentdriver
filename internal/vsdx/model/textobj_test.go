package model

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/jhnoel/documentdriver/internal/laws"
	"github.com/jhnoel/documentdriver/internal/repo"
	"github.com/jhnoel/documentdriver/internal/vsdx/inherit"
)

// textObjGolden reads the designer-authored golden at
// testdata/vsdx/<name> from the repository root, for the exact-bytes
// V7 row.
func textObjGolden(t *testing.T, name string) string {
	t.Helper()
	root, err := repo.Root()
	if err != nil {
		t.Fatalf("repo root: %v", err)
	}
	body, err := os.ReadFile(filepath.Join(root, "testdata", "vsdx", name)) //nolint:gosec // G304: testdata path from the repo root and a fixed name, never from a document
	if err != nil {
		t.Fatalf("read golden %s: %v", name, err)
	}
	return string(body)
}

// checkText fails the test when o does not show html and plain with
// the html origin origin, and HasText true.
func checkText(t *testing.T, o *Object, html, plain string, origin inherit.Origin) {
	t.Helper()
	if !o.HasText {
		t.Errorf("%s HasText = false, want true", o.ID)
	}
	if o.HTML != html {
		t.Errorf("%s HTML = %q, want %q", o.ID, o.HTML, html)
	}
	if o.Text != plain {
		t.Errorf("%s Text = %q, want %q", o.ID, o.Text, plain)
	}
	if got := o.Origins["html"]; got != origin {
		t.Errorf("%s html origin = %q, want %q", o.ID, got, origin)
	}
}

// TestTextV1 checks the V1 row: s1 HTML `<p><b>Hello</b></p>`, Text
// "Hello", html origin local.
func TestTextV1(t *testing.T) {
	d, r := openFixture(t, "v1-minimal")
	p := buildPageOf(t, d, r, 0)
	checkText(t, byID(t, p, "page0/s1"), "<p><b>Hello</b></p>", "Hello", inherit.Local)
}

// TestTextV2 checks the V2 rows: s1 `<p><i>Process</i></p>` origin
// master; s2 `<p>Review</p>` local (the master rows are not shown
// under its own text); s4 `<p>Left</p>` master; s5 `<p>Right</p>`
// local; s3 HasText false with no html.
func TestTextV2(t *testing.T) {
	d, r := openFixture(t, "v2-master")
	p := buildPageOf(t, d, r, 0)
	checkText(t, byID(t, p, "page0/s1"), "<p><i>Process</i></p>", "Process", inherit.Master)
	checkText(t, byID(t, p, "page0/s2"), "<p>Review</p>", "Review", inherit.Local)
	checkText(t, byID(t, p, "page0/s4"), "<p>Left</p>", "Left", inherit.Master)
	checkText(t, byID(t, p, "page0/s5"), "<p>Right</p>", "Right", inherit.Local)
	if o := byID(t, p, "page0/s3"); o.HasText || o.HTML != "" || o.Text != "" || o.Origins["html"] != "" {
		t.Errorf("s3 = HasText %v, HTML %q, Text %q, origins %v, want no text", o.HasText, o.HTML, o.Text, o.Origins)
	}
}

// TestTextV7 checks the V7 row: s1 html is the golden file
// byte-for-byte, Text "Title red\nitem\ntwo\tend 3", html origin
// local; formats as testdata/vsdx/v7-text PROVENANCE.md.
func TestTextV7(t *testing.T) {
	d, r := openFixture(t, "v7-text")
	p := buildPageOf(t, d, r, 0)
	o := byID(t, p, "page0/s1")
	if want := textObjGolden(t, "golden/v7-page0.html"); o.HTML != want {
		t.Errorf("s1 HTML = %q, want golden %q", o.HTML, want)
	}
	checkText(t, o, o.HTML, "Title red\nitem\ntwo\tend 3", inherit.Local)
}

// TestTextNil checks that textOf with a nil object, resolver or
// shape never panics and leaves a textless object alone.
func TestTextNil(t *testing.T) {
	textOf(nil, nil, nil)
	o := &Object{ID: "page0/s1"}
	textOf(o, nil, nil)
	if o.HasText || o.HTML != "" || o.Text != "" {
		t.Errorf("textOf(o, nil, nil) = HasText %v, HTML %q, Text %q, want no text", o.HasText, o.HTML, o.Text)
	}
}

// TestScalingTextOf checks the scaling row: textOf over N and 4N
// shapes (N = 1000) allocates at most 5.5 times as much (laws.Scales)
// and the laws.TimeScales time ratio is at most 8, linear in the shapes.
func TestScalingTextOf(t *testing.T) {
	setup := func(n int) (func() error, error) {
		d, r := openFixture(t, "v1-minimal")
		e, ok := d.Page(0)
		if !ok {
			return nil, fmt.Errorf("v1-minimal: no page 0")
		}
		p, err := BuildPage(d, r, e, Options{})
		if err != nil {
			return nil, fmt.Errorf("BuildPage(v1-minimal): %w", err)
		}
		shape := shapeFor(p.Objects[0], 0, false)
		many := make([]*inherit.Shape, n)
		for i := range many {
			many[i] = shape
		}
		return func() error {
			for _, s := range many {
				o := &Object{ID: "page0/s1"}
				textOf(o, r, s)
				if o.HTML != "<p><b>Hello</b></p>" || o.Text != "Hello" {
					return fmt.Errorf("textOf = HTML %q, Text %q, want <p><b>Hello</b></p>, Hello", o.HTML, o.Text)
				}
			}
			return nil
		}, nil
	}
	laws.Scales(t, "vsdx model textOf", setup)
	laws.TimeScales(t, "vsdx model textOf", setup)
}
