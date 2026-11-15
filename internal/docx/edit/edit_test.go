package edit

import (
	"archive/zip"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/jhnoel/documentdriver/internal/ooxml/ctypes"
	core "github.com/jhnoel/documentdriver/internal/ooxml/edit"
	"github.com/jhnoel/documentdriver/internal/ooxml/tree"
	"github.com/jhnoel/documentdriver/internal/opc"
	"github.com/jhnoel/documentdriver/internal/plan"
)

const wURI = "http://schemas.openxmlformats.org/wordprocessingml/2006/main"

const nsDecl = ` xmlns:w="` + wURI + `" xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships"`

const docXML = `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<w:document` + nsDecl + `><w:body>` +
	`<w:p><w:pPr><w:sectPr><w:pgSz w:w="11906"/></w:sectPr></w:pPr><w:r><w:t>one</w:t></w:r></w:p>` +
	`<w:tbl><w:tblPr><w:tblW w:w="0"/></w:tblPr><w:tblGrid><w:gridCol/></w:tblGrid>` +
	`<w:tr><w:tc><w:tcPr/><w:p><w:r><w:t>cell</w:t></w:r></w:p></w:tc></w:tr>` +
	`<w:sdt><w:sdtPr/><w:sdtContent><w:tr><w:tc><w:p/></w:tc></w:tr></w:sdtContent></w:sdt></w:tbl>` +
	`<w:sdt><w:sdtPr/><w:sdtContent><w:p><w:sdt><w:sdtContent><w:r><w:t>in</w:t></w:r></w:sdtContent></w:sdt></w:p></w:sdtContent></w:sdt>` +
	`<w:sectPr><w:sectPrChange w:id="1"><w:sectPr/></w:sectPrChange></w:sectPr>` +
	`</w:body></w:document>`

const hdrXML = `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<w:hdr` + nsDecl + `><w:p><w:r><w:t>head</w:t></w:r></w:p></w:hdr>`

const docPart = "/word/document.xml"
const hdrPart = "/word/header1.xml"

// writeDocx writes a package of the given parts (zip names) with content
// types and package relationships, and returns its path.
func writeDocx(t *testing.T, parts map[string]string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "doc.docx")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	z := zip.NewWriter(f)
	all := map[string]string{
		"[Content_Types].xml": `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>` +
			`<Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types">` +
			`<Default Extension="rels" ContentType="` + ctypes.CTPackageRelationships + `"/>` +
			`<Default Extension="xml" ContentType="application/xml"/>` +
			`<Override PartName="/word/document.xml" ContentType="` + ctypes.CTWordprocessingmlDocumentMain + `"/>` +
			`<Override PartName="/word/header1.xml" ContentType="` + ctypes.CTWordprocessingmlHeader + `"/></Types>`,
		"_rels/.rels": `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>` +
			`<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">` +
			`<Relationship Id="rId1" Type="` + ctypes.RelOfficeDocument + `" Target="word/document.xml"/></Relationships>`,
		"word/_rels/document.xml.rels": `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>` +
			`<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">` +
			`<Relationship Id="rId1" Type="` + ctypes.RelHeader + `" Target="header1.xml"/></Relationships>`,
		"word/document.xml": docXML,
		"word/header1.xml":  hdrXML,
	}
	for k, v := range parts {
		all[k] = v
	}
	for name, body := range all {
		w, err := z.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write([]byte(body)); err != nil {
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

func openWS(t *testing.T, parts map[string]string) *Workspace {
	t.Helper()
	pkg, err := opc.Open(writeDocx(t, parts))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { pkg.Close() })
	w, err := New(pkg, Options{})
	if err != nil {
		t.Fatal(err)
	}
	return w
}

// find returns the elements of part named local (in document order).
func find(t *testing.T, w *Workspace, part, local string) []*tree.Node {
	t.Helper()
	doc, err := w.Tree(part)
	if err != nil {
		t.Fatal(err)
	}
	return doc.Root().FindAll(wURI, local)
}

func TestUnits(t *testing.T) {
	w := openWS(t, nil)
	if w.DocumentPart() != docPart {
		t.Fatalf("DocumentPart = %s", w.DocumentPart())
	}
	doc, _ := w.Tree(docPart)
	if !Units().HasUnits(doc) {
		t.Fatal("document has no units")
	}
	count := map[string]int{}
	doc.Root().Walk(func(n *tree.Node) bool {
		if IsUnit(n) {
			count[n.Local()]++
		}
		return true
	})
	// sdt: the block one and the row one, not the run one inside a
	// paragraph; sdtContent likewise. sectPr: in pPr and the body's, not
	// the one in w:sectPrChange.
	want := map[string]int{"body": 1, "p": 4, "tbl": 1, "tr": 2, "tc": 2, "sdt": 2, "sdtContent": 2, "sectPr": 2}
	for k, v := range want {
		if count[k] != v {
			t.Errorf("%s units = %d, want %d", k, count[k], v)
		}
	}
	for k := range count {
		if _, ok := want[k]; !ok {
			t.Errorf("unexpected unit %s", k)
		}
	}
	hdr, _ := w.Tree(hdrPart)
	if !Units().HasUnits(hdr) {
		t.Fatal("header has no units")
	}
}

func TestType(t *testing.T) {
	w := openWS(t, nil)
	sdts := find(t, w, docPart, "sdt")
	for _, c := range []struct {
		e    *tree.Node
		want string
	}{
		{find(t, w, docPart, "p")[0], "w:CT_P"},
		{find(t, w, docPart, "r")[0], "w:CT_R"},
		{find(t, w, docPart, "pPr")[0], "w:CT_PPr"},
		{find(t, w, docPart, "pgSz")[0], "w:CT_PageSz"},
		{find(t, w, docPart, "tblPr")[0], "w:CT_TblPr"},
		{find(t, w, docPart, "tr")[0], "w:CT_Row"},
		{find(t, w, docPart, "tcPr")[0], "w:CT_TcPr"},
		{sdts[0], "w:CT_SdtRow"},
		{sdts[1], "w:CT_SdtBlock"},
		{sdts[2], "w:CT_SdtRun"},
		{find(t, w, docPart, "body")[0], "w:CT_Body"},
		{find(t, w, hdrPart, "p")[0].Parent(), "w:CT_HdrFtr"},
	} {
		got, ok := Type(c.e)
		if !ok || got != c.want {
			t.Errorf("Type(%s) = %q, %v; want %s", c.e.Name(), got, ok, c.want)
		}
	}
	if _, ok := Type(find(t, w, docPart, "tbl")[0]); ok {
		t.Error("w:tbl reported tabulated")
	}
}

func TestVerifyUnits(t *testing.T) {
	ins := func(w *Workspace, part string, c *tree.Node, typ string) error {
		nodes, err := w.Core().Parse(c, []byte(`<w:p><w:r><w:t>new</w:t></w:r></w:p>`), map[string]string{"w": wURI})
		if err != nil {
			return err
		}
		pos, err := w.Core().SlotEnd(c, typ, W("p"), true, nil, nil)
		if err != nil {
			return err
		}
		return w.Core().Insert(part, pos, nodes[0], nil)
	}
	setText := func(w *Workspace, part string, p *tree.Node, s string) error {
		h := w.Target(part, "p", p)
		return h.SetText(p.Find(wURI, "t"), s)
	}
	cases := []struct {
		name string
		edit func(*Workspace) error
		ok   bool
	}{
		{"targeted paragraph", func(w *Workspace) error {
			return setText(w, docPart, find(t, w, docPart, "p")[0], "x")
		}, true},
		{"paragraph in a cell, cell targeted", func(w *Workspace) error {
			tc := find(t, w, docPart, "tc")[0]
			w.Target(docPart, "tc", tc)
			p := tc.Find(wURI, "p")
			h := w.Core().Peek(docPart, "p", []*tree.Node{p})
			return h.SetText(p.Find(wURI, "t"), "x")
		}, false},
		{"insert into a cell", func(w *Workspace) error {
			return ins(w, docPart, find(t, w, docPart, "tc")[0], "w:CT_Tc")
		}, true},
		{"insert into a block content control", func(w *Workspace) error {
			return ins(w, docPart, find(t, w, docPart, "sdtContent")[1], "w:CT_SdtContentBlock")
		}, true},
		{"section in a paragraph is its own unit", func(w *Workspace) error {
			p := find(t, w, docPart, "p")[0]
			h := w.Target(docPart, "p", p)
			return h.SetAttr(core.Path{W("pPr"), W("sectPr"), W("pgSz")}, "w:w", "1")
		}, false},
		{"section targeted", func(w *Workspace) error {
			s := find(t, w, docPart, "sectPr")[0]
			h := w.Target(docPart, "section", s)
			return h.SetAttr(core.Path{W("pgSz")}, "w:w", "1")
		}, true},
		{"header paragraph targeted", func(w *Workspace) error {
			return setText(w, hdrPart, find(t, w, hdrPart, "p")[0], "x")
		}, true},
		{"header paragraph through a peek", func(w *Workspace) error {
			p := find(t, w, hdrPart, "p")[0]
			w.Core().DeclarePart(hdrPart)
			h := w.Core().Peek(hdrPart, "p", []*tree.Node{p})
			return h.SetText(p.Find(wURI, "t"), "x")
		}, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			w := openWS(t, nil)
			if err := c.edit(w); err != nil {
				if !c.ok {
					return // refused before the verifier: the bound held
				}
				t.Fatal(err)
			}
			err := w.Verify()
			if c.ok && err != nil {
				t.Fatalf("Verify = %v", err)
			}
			if !c.ok && !errors.Is(err, plan.ErrUndeclared) {
				t.Fatalf("Verify = %v, want ErrUndeclared", err)
			}
		})
	}
}

// TestStrictDocumentOpens: an ISO Strict document opens in the edit
// workspace and its units are recognised (WML holds both URIs).
func TestStrictDocumentOpens(t *testing.T) {
	strict := `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<w:document xmlns:w="http://purl.oclc.org/ooxml/wordprocessingml/main"><w:body><w:p><w:r><w:t>s</w:t></w:r></w:p><w:sectPr/></w:body></w:document>`
	w := openWS(t, map[string]string{"word/document.xml": strict})
	doc, err := w.Tree(docPart)
	if err != nil {
		t.Fatal(err)
	}
	if !Units().HasUnits(doc) {
		t.Fatal("strict document has no units")
	}
	p := doc.Root().Elements()[0].Elements()[0]
	if !IsUnit(p) {
		t.Fatal("strict paragraph is not a unit")
	}
	if typ, ok := Type(p); !ok || typ != "w:CT_P" {
		t.Fatalf("Type = %s %v", typ, ok)
	}
}
