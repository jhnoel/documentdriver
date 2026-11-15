package edit

import (
	"testing"

	"github.com/jhnoel/documentdriver/internal/ooxml/tree"
)

func TestParseKnowsNamespaces(t *testing.T) {
	w, body := open(t)
	nodes, err := w.Parse(body, []byte(`<w:r><w:t>x</w:t></w:r>`), nil)
	if err != nil {
		t.Fatal(err)
	}
	if nodes[0].Space() != wURI || nodes[0].Elements()[0].Space() != wURI {
		t.Fatalf("detached Space = %q", nodes[0].Space())
	}
}

func TestPlaceAtParagraphContent(t *testing.T) {
	w, body := open(t)
	p := paras(body)[1] // pPr, r
	h := w.Target(docPart, "p", []*tree.Node{p})
	run := p.Elements()[1]
	n, err := w.Parse(p, []byte(`<w:ins w:id="1" w:author="a"><w:r><w:t>new </w:t></w:r></w:ins>`), nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := h.PlaceAt(p, n[0], run); err != nil {
		t.Fatal(err)
	}
	m, _ := w.Parse(p, []byte(`<w:r/>`), nil)
	if err := h.PlaceAt(p, m[0], nil); err != nil {
		t.Fatal(err)
	}
	if got := names(p); got != "w:pPr w:ins w:r w:r" {
		t.Fatalf("children %s", got)
	}
	bad, _ := w.Parse(p, []byte(`<w:r/>`), nil)
	if err := h.PlaceAt(p, bad[0], p.Elements()[0]); err == nil {
		t.Fatal("a run placed before the paragraph properties")
	}
	tbl, _ := w.Parse(p, []byte(`<w:tbl/>`), nil)
	if err := h.PlaceAt(p, tbl[0], nil); err == nil {
		t.Fatal("a table placed in a paragraph")
	}
	if !w.Created(n[0].Elements()[0]) {
		t.Fatal("placed content not recorded as created")
	}
	if err := w.Verify(); err != nil {
		t.Fatal(err)
	}
	d := w.Declared()
	if ts := d.Targets(docPart); len(ts) != 1 || ts[0] != p {
		t.Fatalf("Targets = %v", ts)
	}
}

func TestReplaceUnitWithAnother(t *testing.T) {
	w, body := open(t)
	ps := paras(body)
	h := w.Story(docPart, "body", body)
	tbl, err := w.Parse(body, []byte(`<w:tbl><w:tblPr/><w:tblGrid/></w:tbl>`), nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := h.ReplaceWith(ps[0], tbl[0]); err != nil {
		t.Fatal(err)
	}
	p, err := h.ReplaceWithNew(tbl[0], W("p"))
	if err != nil {
		t.Fatal(err)
	}
	if body.Elements()[0] != p {
		t.Fatal("replacement not in place")
	}
	// The body already has its one sectPr: replacing the second
	// paragraph with another is refused.
	sect, _ := w.Parse(body, []byte(`<w:sectPr/>`), nil)
	if err := h.ReplaceWith(ps[1], sect[0]); err == nil {
		t.Fatal("sectPr placed before the body's sectPr")
	}
	if err := w.Verify(); err != nil {
		t.Fatal(err)
	}
}
