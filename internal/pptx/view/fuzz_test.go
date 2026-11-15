package view

import (
	"archive/zip"
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jhnoel/documentdriver/internal/ooxml/tree"
	"github.com/jhnoel/documentdriver/internal/opc"
	"github.com/jhnoel/documentdriver/internal/pptx/model"
	"github.com/jhnoel/documentdriver/internal/testdeck"
)

// FuzzRead mutates the slide XML of the diagram fixture (groups, glued
// connectors, placeholders, mc:AlternateContent, custom geometry) and
// runs what `pptx read` runs: the slide list, the read model, and the
// JSON and SVG views under every option set. Any of them may refuse; none
// may panic or hang.
func FuzzRead(f *testing.F) {
	base, err := os.ReadFile(testdeck.Diagram(f))
	if err != nil {
		f.Fatal(err)
	}
	zr, err := zip.NewReader(bytes.NewReader(base), int64(len(base)))
	if err != nil {
		f.Fatal(err)
	}
	for _, path := range []string{
		filepath.Join(testdeck.Root(), "testdata", "decks", "diagram", "ppt", "slides", "slide1.xml"),
		filepath.Join(testdeck.Root(), "testdata", "decks", "minimal", "ppt", "slides", "slide1.xml"),
	} {
		b, err := os.ReadFile(path)
		if err != nil {
			f.Fatal(err)
		}
		f.Add(b)
	}
	deck := filepath.Join(f.TempDir(), "fuzz.pptx")
	f.Fuzz(func(t *testing.T, slide []byte) {
		if err := os.WriteFile(deck, withSlide(t, zr, slide), 0o644); err != nil {
			t.Fatal(err)
		}
		p, err := opc.Open(deck)
		if err != nil {
			t.Fatal(err)
		}
		defer p.Close()
		d, err := model.OpenDeck(p)
		if err != nil {
			t.Fatal(err)
		}
		if list, err := d.List(); err == nil {
			if _, err := SlideList(list, ""); err != nil {
				t.Fatalf("SlideList: %v", err)
			}
		}
		s, err := d.ReadSlide("/ppt/slides/slide1.xml")
		if err != nil {
			return
		}
		for _, o := range allOptions {
			// Errors are valid outcomes for fuzzed input; only panics fail.
			if _, err := JSON(s, o); err != nil {
				continue
			}
			if _, err := SVG(s, o); err != nil {
				continue
			}
		}
	})
}

// FuzzReadEdits is FuzzRead with structural mutations, which reach the
// read model far more often than byte flips (those mostly break
// well-formedness): prog is a list of edits on the diagram fixture's slide,
// three bytes each (kind, element, other element or attribute), and vals
// holds "|"-separated attribute values the edits draw from. Edits delete
// an element, duplicate it in place, copy it into another element (groups
// into shapes, shapes into text, a group into itself), or set one of its
// attributes. The result is read as FuzzRead reads it.
func FuzzReadEdits(f *testing.F) {
	base, err := os.ReadFile(testdeck.Diagram(f))
	if err != nil {
		f.Fatal(err)
	}
	zr, err := zip.NewReader(bytes.NewReader(base), int64(len(base)))
	if err != nil {
		f.Fatal(err)
	}
	src, err := os.ReadFile(filepath.Join(testdeck.Root(), "testdata", "decks", "diagram", "ppt", "slides", "slide1.xml"))
	if err != nil {
		f.Fatal(err)
	}
	f.Add([]byte{0, 10, 0}, "")
	f.Add([]byte{1, 5, 0, 2, 7, 3}, "")
	f.Add([]byte{3, 12, 0, 3, 20, 1, 3, 30, 2}, "-1|0|99999999999999|ffffff|-5400000|x")
	f.Add([]byte{2, 4, 4, 3, 6, 0}, "0|1|21600000")
	deck := filepath.Join(f.TempDir(), "fuzz.pptx")
	f.Fuzz(func(t *testing.T, prog []byte, vals string) {
		slide := src
		for i := 0; i+2 < len(prog) && i < 3*8; i += 3 {
			next, ok := editSlide(slide, prog[i], int(prog[i+1]), int(prog[i+2]), vals)
			if !ok {
				return
			}
			slide = next
		}
		if err := os.WriteFile(deck, withSlide(t, zr, slide), 0o644); err != nil {
			t.Fatal(err)
		}
		p, err := opc.Open(deck)
		if err != nil {
			t.Fatal(err)
		}
		defer p.Close()
		d, err := model.OpenDeck(p)
		if err != nil {
			t.Fatal(err)
		}
		if list, err := d.List(); err == nil {
			if _, err := SlideList(list, ""); err != nil {
				t.Fatalf("SlideList: %v", err)
			}
		}
		s, err := d.ReadSlide("/ppt/slides/slide1.xml")
		if err != nil {
			return
		}
		for _, o := range allOptions {
			// Errors are valid outcomes for fuzzed input; only panics fail.
			if _, err := JSON(s, o); err != nil {
				continue
			}
			if _, err := SVG(s, o); err != nil {
				continue
			}
		}
	})
}

// editSlide applies one structural edit to well-formed slide bytes by
// splicing element spans (the tree's mutation API is reserved for the
// engine). It reports false when the edit does not apply or the result
// is not well formed.
func editSlide(slide []byte, kind byte, i, j int, vals string) ([]byte, bool) {
	doc, err := tree.Parse(slide)
	if err != nil {
		return nil, false
	}
	var els []*tree.Node
	doc.Root().Walk(func(n *tree.Node) bool {
		if n.Kind() == tree.ElementNode {
			els = append(els, n)
		}
		return true
	})
	e := els[i%len(els)]
	es, ee, _ := e.Span()
	splice := func(at, end int, ins []byte) []byte {
		out := append([]byte(nil), slide[:at]...)
		out = append(out, ins...)
		return append(out, slide[end:]...)
	}
	var out []byte
	switch kind % 4 {
	case 0: // delete
		if e == doc.Root() {
			return nil, false
		}
		out = splice(es, ee, nil)
	case 1: // duplicate in place
		out = splice(ee, ee, slide[es:ee])
	case 2: // copy into another element, before its end tag
		g := els[j%len(els)]
		end := g.EndTag()
		if end == nil {
			return nil, false
		}
		_, ge, _ := g.Span()
		at := ge - len(end)
		out = splice(at, at, slide[es:ee])
	case 3: // set an attribute
		attrs := e.Attrs()
		if len(attrs) == 0 {
			return nil, false
		}
		vs := strings.Split(vals, "|")
		v := vs[j%len(vs)]
		a := attrs[j%len(attrs)]
		tag := e.StartTag()
		k := bytes.Index(tag, []byte(a.Name+"="))
		if k < 0 {
			return nil, false
		}
		vs0 := k + len(a.Name) + 1
		for vs0 < len(tag) && tag[vs0] != '"' && tag[vs0] != '\'' {
			vs0++
		}
		if vs0 >= len(tag) {
			return nil, false
		}
		q := tag[vs0]
		ve := bytes.IndexByte(tag[vs0+1:], q)
		if ve < 0 {
			return nil, false
		}
		out = splice(es+vs0+1, es+vs0+1+ve, xmlEscape(v))
	}
	if tree.Check(out) != nil {
		return nil, false
	}
	return out, true
}

// xmlEscape returns v escaped for an attribute value in either quote.
func xmlEscape(v string) []byte {
	var w bytes.Buffer
	for _, r := range v {
		switch r {
		case '<':
			w.WriteString("&lt;")
		case '&':
			w.WriteString("&amp;")
		case '"':
			w.WriteString("&quot;")
		case '\'':
			w.WriteString("&apos;")
		default:
			w.WriteRune(r)
		}
	}
	return w.Bytes()
}

// withSlide returns the zip zr with ppt/slides/slide1.xml replaced by
// slide (stored), every other member copied compressed.
func withSlide(t *testing.T, zr *zip.Reader, slide []byte) []byte {
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for _, zf := range zr.File {
		if zf.Name != "ppt/slides/slide1.xml" {
			if err := zw.Copy(zf); err != nil {
				t.Fatal(err)
			}
			continue
		}
		w, err := zw.CreateHeader(&zip.FileHeader{Name: zf.Name, Method: zip.Store})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := io.Copy(w, bytes.NewReader(slide)); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}
