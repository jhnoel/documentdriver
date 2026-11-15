package opc

import (
	"archive/zip"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeZip writes entries (name, body) in order, duplicates allowed.
func writeZip(t *testing.T, entries [][2]string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "p.zip")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	z := zip.NewWriter(f)
	for _, e := range entries {
		w, err := z.Create(e[0])
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write([]byte(e[1])); err != nil {
			t.Fatal(err)
		}
	}
	if err := z.Close(); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	return path
}

const leniencyTypes = `<?xml version="1.0" encoding="UTF-8"?><Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types"><Default Extension="xml" ContentType="application/xml"/></Types>`

// TestDuplicatePartNameFirstWins: a pre-existing duplicate entry does not
// stop opening, editing or saving; the first entry is the part, and the
// second is copied through untouched.
func TestDuplicatePartNameFirstWins(t *testing.T) {
	path := writeZip(t, [][2]string{
		{"[Content_Types].xml", leniencyTypes},
		{"a.xml", "<a>first</a>"},
		{"b.xml", "<b/>"},
		{"A.xml", "<a>second</a>"},
	})
	p := open(t, path)
	if n := p.Notes(); len(n) != 1 || !strings.Contains(n[0], "/A.xml") {
		t.Fatalf("Notes = %v", n)
	}
	pt, err := p.Part("/a.xml")
	if err != nil {
		t.Fatal(err)
	}
	if b, _ := pt.Original(); string(b) != "<a>first</a>" {
		t.Fatalf("part = %s", b)
	}
	b, err := p.Part("/b.xml")
	if err != nil {
		t.Fatal(err)
	}
	doc, err := b.Tree()
	if err != nil {
		t.Fatal(err)
	}
	if err := doc.Root().SetAttr("x", "1"); err != nil {
		t.Fatal(err)
	}
	if err := p.SaveAndClose(); err != nil {
		t.Fatal(err)
	}
	zr, err := zip.OpenReader(path)
	if err != nil {
		t.Fatal(err)
	}
	defer zr.Close()
	var names []string
	for _, f := range zr.File {
		names = append(names, f.Name)
	}
	if got := strings.Join(names, " "); got != "[Content_Types].xml a.xml b.xml A.xml" {
		t.Fatalf("saved entries %s", got)
	}
}

// TestContentTypesWrongRoot: a content types stream whose root is not
// Types opens; it declares nothing, and writing to it fails.
func TestContentTypesWrongRoot(t *testing.T) {
	path := writeZip(t, [][2]string{
		{"[Content_Types].xml", `<Wrong/>`},
		{"a.xml", "<a/>"},
	})
	p := open(t, path)
	ct, err := p.ContentTypes()
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := ct.Lookup("/a.xml"); ok {
		t.Fatal("a wrong root declared a type")
	}
	if err := p.RegistersTypes(); !errors.Is(err, ErrContentTypes) {
		t.Fatalf("RegistersTypes = %v", err)
	}
	if _, err := p.AddPart("/b.xml", []byte("<b/>"), "application/xml"); !errors.Is(err, ErrContentTypes) {
		t.Fatalf("AddPart = %v", err)
	}
	if p.Has("/b.xml") {
		t.Fatal("a refused AddPart added the part")
	}
	// An edit that adds no part saves, the stream copied through.
	a, err := p.Part("/a.xml")
	if err != nil {
		t.Fatal(err)
	}
	doc, err := a.Tree()
	if err != nil {
		t.Fatal(err)
	}
	if err := doc.Root().SetAttr("x", "1"); err != nil {
		t.Fatal(err)
	}
	if err := p.SaveAndClose(); err != nil {
		t.Fatal(err)
	}
	zr, err := zip.OpenReader(path)
	if err != nil {
		t.Fatal(err)
	}
	defer zr.Close()
	f, err := zr.Open("[Content_Types].xml")
	if err != nil {
		t.Fatal(err)
	}
	b := make([]byte, 64)
	nb, _ := f.Read(b)
	if string(b[:nb]) != "<Wrong/>" {
		t.Fatalf("content types now %q", b[:nb])
	}
	if n := p.Notes(); len(n) != 1 || !strings.Contains(n[0], "<Wrong>") {
		t.Fatalf("Notes = %v", n)
	}
}
