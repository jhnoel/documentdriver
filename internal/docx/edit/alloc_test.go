package edit

import (
	"strings"
	"testing"

	"github.com/jhnoel/documentdriver/internal/docx/model"
)

const revHdr = `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<w:hdr` + nsDecl + `><w:p><w:ins w:id="5" w:author="a"><w:r><w:t>ab</w:t></w:r></w:ins>` +
	`<w:moveFromRangeStart w:id="9" w:name="m"/><w:bookmarkStart w:id="7" w:name="b"/></w:p></w:hdr>`

func TestRevisionIDs(t *testing.T) {
	w, _ := openIDs(t, idDoc, map[string]string{"word/header1.xml": revHdr})
	// The largest revision id counts move range markers: 9.
	for _, want := range []string{"10", "11"} {
		got, err := w.RevisionID(hdrPart)
		if err != nil || got != want {
			t.Fatalf("RevisionID = %s, %v; want %s", got, err, want)
		}
	}
	// A part without revisions starts at 1.
	if got, _ := w.RevisionID(docPart); got != "1" {
		t.Fatalf("document RevisionID = %s", got)
	}
}

func TestRenumberRevisions(t *testing.T) {
	w, _ := openIDs(t, idDoc, map[string]string{"word/header1.xml": revHdr})
	p := find(t, w, hdrPart, "p")[0]
	h := w.Target(hdrPart, "p", p)
	ins := p.Elements()[0]
	r := ins.Elements()[0]
	// Isolate "b": split the text, the run and the wrapper; the wrapper's
	// clone carries id 5 and must be renumbered, the original keeps it.
	if _, err := h.SplitText(r.Elements()[0], 1); err != nil {
		t.Fatal(err)
	}
	r2, err := h.SplitAt(r, 1, nil)
	if err != nil {
		t.Fatal(err)
	}
	ins2, err := h.SplitAt(ins, 1, nil)
	if err != nil {
		t.Fatal(err)
	}
	if ins2.Elements()[0] != r2 {
		t.Fatal("split moved the wrong run")
	}
	if err := w.RenumberRevisions(hdrPart); err != nil {
		t.Fatal(err)
	}
	if got := model.AttrOr(ins, "id"); got != "5" {
		t.Fatalf("original wrapper id %s", got)
	}
	if got := model.AttrOr(ins2, "id"); got != "10" {
		t.Fatalf("clone id %s, want 10", got)
	}
	// Numbered once: a second call changes nothing.
	if err := w.RenumberRevisions(hdrPart); err != nil {
		t.Fatal(err)
	}
	if got := model.AttrOr(ins2, "id"); got != "10" {
		t.Fatalf("renumbered twice: %s", got)
	}
	if err := w.Verify(); err != nil {
		t.Fatal(err)
	}
}

func TestInsertParaIDs(t *testing.T) {
	w, _ := openIDs(t, idDoc, nil)
	a, err := w.ParaIDs(docPart, 3, 0, 1)
	if err != nil {
		t.Fatal(err)
	}
	b, err := w.ParaIDs(docPart, 3, 0, 1)
	if err != nil || a[0] != b[0] {
		t.Fatalf("not deterministic: %v %v %v", a, b, err)
	}
	cells, err := w.ParaIDs(docPart, 3, 1, 4)
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	for _, p := range find(t, w, docPart, "p") {
		id, _ := w.ParaID(p)
		seen[id.ID] = true
	}
	for _, id := range append(a, cells...) {
		if seen[id] || !model.IsValidParaID(id) {
			t.Fatalf("id %s taken or invalid (%v)", id, seen)
		}
		seen[id] = true
	}
}

func TestOtherIDs(t *testing.T) {
	numbering := `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<w:numbering` + nsDecl + `><w:abstractNum w:abstractNumId="0"/><w:abstractNum w:abstractNumId="4"/><w:num w:numId="2"><w:abstractNumId w:val="4"/></w:num></w:numbering>`
	w, _ := openIDs(t, idDoc, map[string]string{
		"word/header1.xml":   revHdr,
		"word/numbering.xml": numbering,
		"word/_rels/document.xml.rels": `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>` +
			`<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">` +
			`<Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/header" Target="header1.xml"/>` +
			`<Relationship Id="rId7" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/numbering" Target="numbering.xml"/></Relationships>`,
	})
	if got, err := w.BookmarkID(); err != nil || got != "8" {
		t.Fatalf("BookmarkID = %s, %v", got, err)
	}
	num, ok, err := w.NumberingPart()
	if err != nil || !ok || num != "/word/numbering.xml" {
		t.Fatalf("NumberingPart = %s %v %v", num, ok, err)
	}
	if got, _ := w.NumID(num); got != "3" {
		t.Fatalf("NumID = %s", got)
	}
	if got, _ := w.AbstractNumID(num); got != "5" {
		t.Fatalf("AbstractNumID = %s", got)
	}
	if got, _ := w.RelID(docPart); got != "rId8" {
		t.Fatalf("RelID = %s", got)
	}
	if got := w.PartName("word", "header", ".xml"); got != "/word/header2.xml" {
		t.Fatalf("PartName = %s", got)
	}
	parts, err := w.StoryParts()
	if err != nil || strings.Join(parts, " ") != "/word/document.xml /word/header1.xml" {
		t.Fatalf("StoryParts = %v %v", parts, err)
	}
}
