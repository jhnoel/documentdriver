package coverage

import (
	"archive/zip"
	"os"
	"path/filepath"
	"testing"

	"github.com/jhnoel/documentdriver/internal/ooxml/tree"
	"github.com/jhnoel/documentdriver/internal/testdeck"
)

// outsideSlide is a slide with elements in a namespace the universe has no
// item for, at slide level and inside a shape. Counting one made
// counter.outside call itself until the stack overflowed (every corpus
// deck has one).
const outsideSlide = `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<p:sld xmlns:a="http://schemas.openxmlformats.org/drawingml/2006/main" xmlns:p="http://schemas.openxmlformats.org/presentationml/2006/main" xmlns:x="urn:example:outside">
<p:cSld><p:spTree><p:nvGrpSpPr><p:cNvPr id="1" name=""/><p:cNvGrpSpPr/><p:nvPr/></p:nvGrpSpPr><p:grpSpPr/>
<p:sp><p:nvSpPr><p:cNvPr id="2" name="A"/><p:cNvSpPr/><p:nvPr/></p:nvSpPr><p:spPr/><x:unknown/></p:sp>
</p:spTree></p:cSld><x:unknown/></p:sld>`

func writeZip(t *testing.T, path string, members map[string]string) {
	t.Helper()
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	zw := zip.NewWriter(f)
	for name, body := range members {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write([]byte(body)); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
}

// TestCountDeepGroups counts a slide nested as deep as the tokenizer
// allows: counter.walk recurses per element and again per object in a
// group, bounded by tree.MaxDepth.
func TestCountDeepGroups(t *testing.T) {
	u := loadUniverse(t)
	deck := filepath.Join(t.TempDir(), "deep.pptx")
	writeZip(t, deck, map[string]string{"ppt/slides/slide1.xml": string(testdeck.DeepGroupsSlide(tree.MaxDepth))})
	c, err := countDecks(u, []string{deck}, "", false)
	if err != nil {
		t.Fatal(err)
	}
	if c.Decks != 1 || c.Objects != tree.MaxDepth-8+1 {
		t.Fatalf("decks=%d unreadable=%d objects=%d", c.Decks, c.Unreadable, c.Objects)
	}
}

// TestCountOutside is the regression test for the stack overflow in
// tools/coverage --corpus: an element outside the universe is recorded,
// in the corpus count and (inside objects only) in the profile count.
func TestCountOutside(t *testing.T) {
	u := loadUniverse(t)
	deck := filepath.Join(t.TempDir(), "outside.pptx")
	writeZip(t, deck, map[string]string{"ppt/slides/slide1.xml": outsideSlide})
	for _, objOnly := range []bool{false, true} {
		c, err := countDecks(u, []string{deck}, "", objOnly)
		if err != nil {
			t.Fatal(err)
		}
		if c.Decks != 1 || c.Unreadable != 0 {
			t.Fatalf("objOnly=%v: decks=%d unreadable=%d", objOnly, c.Decks, c.Unreadable)
		}
		if got := c.Outside["p:sp/x:unknown"]; got != 1 {
			t.Errorf("objOnly=%v: outside p:sp/x:unknown = %d, want 1 (%v)", objOnly, got, c.Outside)
		}
		want := int64(1)
		if objOnly {
			want = 0
		}
		if got := c.Outside["p:sld/x:unknown"]; got != want {
			t.Errorf("objOnly=%v: outside p:sld/x:unknown = %d, want %d (%v)", objOnly, got, want, c.Outside)
		}
	}
}
