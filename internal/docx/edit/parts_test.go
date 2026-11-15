package edit

import (
	"testing"

	"github.com/jhnoel/documentdriver/internal/ooxml/ctypes"
)

// TestAddPart adds a header part named by PartName, related from the
// main document with the id RelID gives, and refuses a taken name.
func TestAddPart(t *testing.T) {
	w, _ := openIDs(t, idDoc, nil)
	name := w.PartName("/word", "header", ".xml")
	want, err := w.RelID(w.DocumentPart())
	if err != nil {
		t.Fatal(err)
	}
	const empty = `<w:hdr xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"/>`
	id, err := w.AddPart(ctypes.RelHeader, ctypes.CTWordprocessingmlHeader, name, empty)
	if err != nil || id != want {
		t.Fatalf("AddPart = %q, %v; want %q", id, err, want)
	}
	if next := w.PartName("/word", "header", ".xml"); next == name {
		t.Fatalf("PartName gives %s again once it is taken", name)
	}
	rels, err := w.Package().Rels(w.DocumentPart())
	if err != nil {
		t.Fatal(err)
	}
	if r, ok := rels.Get(id); !ok || "/word/"+r.Target != name {
		t.Fatalf("relationship %+v", r)
	}
	if _, err := w.AddPart(ctypes.RelHeader, ctypes.CTWordprocessingmlHeader, name, empty); err == nil {
		t.Fatal("a taken name was added again")
	}
	if err := w.Verify(); err != nil {
		t.Fatal(err)
	}
}

func TestRelTarget(t *testing.T) {
	for _, c := range []struct{ source, name, want string }{
		{"/word/document.xml", "/word/header1.xml", "header1.xml"},
		{"/word/document.xml", "/word/sub/x.xml", "sub/x.xml"},
		{"/word/document.xml", "/other/x.xml", "/other/x.xml"},
		{"/document.xml", "/x.xml", "x.xml"},
	} {
		if got := relTarget(c.source, c.name); got != c.want {
			t.Errorf("relTarget(%s, %s) = %s, want %s", c.source, c.name, got, c.want)
		}
	}
}
