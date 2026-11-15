package edit

import (
	"errors"
	"testing"

	"github.com/jhnoel/documentdriver/internal/docx/model"
	core "github.com/jhnoel/documentdriver/internal/ooxml/edit"
)

func TestRegistryTablesSurviveShifts(t *testing.T) {
	w := openWS(t, nil)
	reg := w.Registry()
	cell, at, err := reg.Table("table:1/row:1/cell:1")
	if err != nil || at != "table:1/row:1/cell:1" {
		t.Fatalf("Table = %v %s %v", cell, at, err)
	}
	// Insert a table before the first: positions shift, handles do not.
	body := find(t, w, docPart, "body")[0]
	nodes, err := w.Core().Parse(body, []byte(`<w:tbl><w:tblPr/><w:tblGrid/><w:tr><w:tc><w:p/></w:tc></w:tr></w:tbl>`), map[string]string{"w": wURI})
	if err != nil {
		t.Fatal(err)
	}
	if err := w.Core().Insert(docPart, core.Before(find(t, w, docPart, "tbl")[0]), nodes[0], nil); err != nil {
		t.Fatal(err)
	}
	if err := reg.Refresh(); err != nil {
		t.Fatal(err)
	}
	got, at, err := reg.Table("table:1/row:1/cell:1")
	if err != nil || got != cell || at != "table:2/row:1/cell:1" {
		t.Fatalf("after insert: %v %s %v", got == cell, at, err)
	}
	if err := reg.BindTable("t", nodes[0]); err != nil {
		t.Fatal(err)
	}
	if c, at, err := reg.Table("$t/row:1/cell:1"); err != nil || at != "table:1/row:1/cell:1" || c != nodes[0].Elements()[2].Elements()[0] {
		t.Fatalf("$t cell: %s %v", at, err)
	}
	if err := reg.BindTable("t", nodes[0]); !errors.Is(err, ErrDuplicateName) {
		t.Fatalf("rebinding = %v", err)
	}
	if _, _, err := reg.Table("table:9"); !errors.Is(err, ErrUnknownHandle) {
		t.Fatalf("unknown = %v", err)
	}
	if err := w.Core().Delete(docPart, nodes[0]); err != nil {
		t.Fatal(err)
	}
	if err := reg.Refresh(); err != nil {
		t.Fatal(err)
	}
	if _, _, err := reg.Table("$t"); !errors.Is(err, ErrDeletedHandle) {
		t.Fatalf("deleted = %v", err)
	}
}

func TestRegistryParagraphsSectionsAliases(t *testing.T) {
	doc := `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<w:document` + nsDecl + w14Decl + `><w:body>` +
		`<w:p w14:paraId="11111111"><w:pPr><w:sectPr/></w:pPr><w:r><w:t>one</w:t></w:r><w:del w:id="1"><w:r><w:delText>gone</w:delText></w:r></w:del></w:p>` +
		`<w:p w14:paraId="22222222"><w:r><w:t>two</w:t></w:r></w:p><w:sectPr/></w:body></w:document>`
	w, _ := openIDs(t, doc, nil)
	reg := w.Registry()
	p, ok := reg.Paragraph("11111111")
	if !ok {
		t.Fatal("paragraph not found")
	}
	if s, _ := reg.BaseText(p); s != "one" {
		t.Fatalf("BaseText = %q", s)
	}
	h := w.Target(docPart, "p", p)
	if err := h.SetText(p.Find(wURI, "t"), "changed"); err != nil {
		t.Fatal(err)
	}
	if s, _ := reg.BaseText(p); s != "one" || model.ParagraphText(p, model.Current) != "changed" {
		t.Fatalf("BaseText after edit = %q", s)
	}
	if s, err := reg.Section("section:11111111"); err != nil || s.Parent().Parent() != p {
		t.Fatalf("Section = %v", err)
	}
	if s, err := reg.Section("section:final"); err != nil || s.Parent() != find(t, w, docPart, "body")[0] {
		t.Fatalf("final Section = %v", err)
	}
	if _, err := reg.Section("section:22222222"); !errors.Is(err, ErrNoSection) {
		t.Fatalf("Section of a paragraph without one = %v", err)
	}
	if err := reg.DefineAlias("a", "22222222"); err != nil {
		t.Fatal(err)
	}
	if err := reg.DefineGroup("a", nil); !errors.Is(err, ErrDuplicateName) {
		t.Fatalf("group over alias = %v", err)
	}
	if v, err := reg.Alias("a"); err != nil || v != "22222222" {
		t.Fatalf("Alias = %s %v", v, err)
	}
	if _, err := reg.Alias("b"); !errors.Is(err, ErrUnknownAlias) {
		t.Fatalf("unknown alias = %v", err)
	}
}
