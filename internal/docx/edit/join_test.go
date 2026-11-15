package edit

import (
	"testing"

	"github.com/jhnoel/documentdriver/internal/docx/model"
	core "github.com/jhnoel/documentdriver/internal/ooxml/edit"
)

func TestJoinParagraphsMovesIdentity(t *testing.T) {
	doc := `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<w:document` + nsDecl + w14Decl + `><w:body>` +
		`<w:p w14:paraId="11111111" w14:textId="0000000A"><w:r><w:t>text</w:t></w:r></w:p>` +
		`<w:p w14:paraId="22222222"><w:pPr><w:jc w:val="left"/></w:pPr></w:p>` +
		`<w:p w14:paraId="33333333"><w:r><w:t>kept</w:t></w:r></w:p>` +
		`<w:p><w:r><w:t>more</w:t></w:r></w:p><w:sectPr/></w:body></w:document>`
	w, _ := openIDs(t, doc, nil)
	ps := find(t, w, docPart, "p")
	h, err := w.Core().Range(docPart, "join", ps[0], ps[3])
	if err != nil {
		t.Fatal(err)
	}
	// The empty second paragraph absorbs the first: identity moves.
	keep, err := w.JoinParagraphs(h, ps[1], ps[0])
	if err != nil || keep != "11111111" {
		t.Fatalf("keep %s %v", keep, err)
	}
	if err := h.PutAfter(ps[0].Elements()[0], ps[1].Elements()[0]); err != nil {
		t.Fatal(err)
	}
	if err := h.RemoveNode(ps[0]); err != nil {
		t.Fatal(err)
	}
	if v := model.AttrOr(ps[1], "paraId"); v != "11111111" || model.AttrOr(ps[1], "textId") != "0000000A" {
		t.Fatalf("survivor ids %s %s", v, model.AttrOr(ps[1], "textId"))
	}
	reg := w.Registry()
	if p, ok := reg.Paragraph("11111111"); !ok || p != ps[1] {
		t.Fatal("surviving id does not resolve to the survivor")
	}
	if _, ok := reg.Paragraph("22222222"); ok {
		t.Fatal("the absorbed id still resolves")
	}
	if s, ok := reg.Joined("22222222"); !ok || s != "11111111" {
		t.Fatalf("Joined = %s %v", s, ok)
	}
	// A survivor with content keeps its own id; the unstored absorbed id
	// is recorded gone.
	aid, _ := w.ParaID(ps[3])
	keep, err = w.JoinParagraphs(h, ps[2], ps[3])
	if err != nil || keep != "33333333" {
		t.Fatalf("keep %s %v", keep, err)
	}
	if got := w.Joins(); len(got) != 2 || got[1] != (Join{Survivor: "33333333", Absorbed: aid.ID}) {
		t.Fatalf("Joins = %v", got)
	}
	if err := w.Verify(); err != nil {
		t.Fatal(err)
	}
}

func TestRunPropertiesPlacedInDeclarationOrder(t *testing.T) {
	doc := `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<w:document` + nsDecl + `><w:body>` +
		`<w:p><w:r><w:rPr><w:sz w:val="20"/><w:b/><x:y xmlns:x="urn:x"/></w:rPr><w:t>a</w:t></w:r></w:p>` +
		`<w:p><w:r><w:rPr><w:b/><w:sz w:val="20"/></w:rPr><w:t>b</w:t></w:r></w:p>` +
		`<w:sectPr/></w:body></w:document>`
	w, _ := openIDs(t, doc, nil)
	ps := find(t, w, docPart, "p")
	for _, p := range ps {
		h := w.Target(docPart, "p", p)
		if err := h.Ensure(core.Path{W("r"), W("rPr")}, W("i")); err != nil {
			t.Fatal(err)
		}
		if err := h.Ensure(core.Path{W("r"), W("rPr")}, W("b")); err != nil {
			t.Fatal(err) // kept as it is
		}
	}
	// The existing out-of-order pair and the unknown element stay as they
	// were; only the new w:i is placed.
	if got := string(ps[0].Find(wURI, "rPr").Bytes()); got != `<w:rPr><w:i/><w:sz w:val="20"/><w:b/><x:y xmlns:x="urn:x"/></w:rPr>` {
		t.Fatalf("out of order: %s", got)
	}
	if got := string(ps[1].Find(wURI, "rPr").Bytes()); got != `<w:rPr><w:b/><w:i/><w:sz w:val="20"/></w:rPr>` {
		t.Fatalf("in order: %s", got)
	}
	if err := w.Finish(); err != nil {
		t.Fatal(err)
	}
	if err := w.Verify(); err != nil {
		t.Fatal(err)
	}
}
