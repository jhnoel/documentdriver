package edit

import (
	"archive/zip"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jhnoel/documentdriver/internal/ooxml/ctypes"
	"github.com/jhnoel/documentdriver/internal/ooxml/schema"
	"github.com/jhnoel/documentdriver/internal/ooxml/tree"
	"github.com/jhnoel/documentdriver/internal/opc"
	"github.com/jhnoel/documentdriver/internal/plan"
)

// The core's tests run on WordprocessingML, a vocabulary the pptx layer
// never uses, to show that nothing in the core is PresentationML.

const wURI = "http://schemas.openxmlformats.org/wordprocessingml/2006/main"

var wNS = NS{URIs: []string{wURI, "http://purl.oclc.org/ooxml/wordprocessingml/main"}, Prefix: "w", Table: "w"}

func W(local string) Name { return wNS.Name(local) }

const docPart = "/word/document.xml"

const document = `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<w:document xmlns:w="` + wURI + `" xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships"><w:body>
  <w:p><w:r><w:t>one</w:t></w:r></w:p>
  <w:p><w:pPr><w:jc w:val="left"/></w:pPr><w:r><w:t>two</w:t></w:r></w:p>
  <w:sectPr/>
</w:body></w:document>`

// blocks are the units of a document for these tests: the body (the
// container) and its paragraphs and tables.
type blocks struct{}

func (blocks) HasUnits(doc *tree.Document) bool { return wNS.Is(doc.Root(), "document") }
func (blocks) IsUnit(e *tree.Node) bool {
	return wNS.Is(e, "body") || (wNS.Is(e, "p") || wNS.Is(e, "tbl")) && wNS.Is(e.Parent(), "body")
}
func (blocks) Describe(e *tree.Node) string { return e.Name() }

var wml = Format{
	Vocab: Vocab{wNS, REL},
	Units: blocks{},
	Type: func(e *tree.Node) (string, bool) {
		switch {
		case wNS.Is(e, "p"):
			return "w:CT_P", true
		case wNS.Is(e, "body"):
			return "w:CT_Body", true
		}
		return "", false
	},
	Bound: func(e *tree.Node) bool { return wNS.Is(e, "p") || wNS.Is(e, "tbl") },
}

// docx writes a minimal WordprocessingML package and returns its path.
func docx(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "doc.docx")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	z := zip.NewWriter(f)
	for name, body := range map[string]string{
		"[Content_Types].xml": `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>` +
			`<Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types">` +
			`<Default Extension="rels" ContentType="` + ctypes.CTPackageRelationships + `"/>` +
			`<Default Extension="xml" ContentType="application/xml"/>` +
			`<Override PartName="/word/document.xml" ContentType="` + ctypes.CTWordprocessingmlDocumentMain + `"/></Types>`,
		"_rels/.rels": `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>` +
			`<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">` +
			`<Relationship Id="rId1" Type="` + ctypes.RelOfficeDocument + `" Target="word/document.xml"/></Relationships>`,
		"word/document.xml": document,
	} {
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

// open returns a workspace over the test document and its body.
func open(t *testing.T) (*Workspace, *tree.Node) {
	t.Helper()
	pkg, err := opc.Open(docx(t))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { pkg.Close() })
	w := New(pkg, wml)
	doc, err := w.Tree(docPart)
	if err != nil {
		t.Fatal(err)
	}
	return w, doc.Root().Elements()[0]
}

func paras(body *tree.Node) []*tree.Node {
	var out []*tree.Node
	for _, e := range body.Elements() {
		if wNS.Is(e, "p") {
			out = append(out, e)
		}
	}
	return out
}

// text returns the content of e's w:t elements.
func text(e *tree.Node) string {
	var b strings.Builder
	e.Walk(func(n *tree.Node) bool {
		if wNS.Is(n, "t") {
			b.WriteString(n.Text())
		}
		return true
	})
	return b.String()
}

func names(e *tree.Node) string {
	var s []string
	for _, c := range e.Elements() {
		s = append(s, c.Name())
	}
	return strings.Join(s, " ")
}

func TestEnsureByWMLOrder(t *testing.T) {
	w, body := open(t)
	p := paras(body)[0]
	h := w.Target(docPart, "p1", []*tree.Node{p})
	if err := h.Ensure(nil, W("pPr")); err != nil {
		t.Fatal(err)
	}
	if err := h.Ensure(Path{W("pPr")}, W("jc")); err != nil {
		t.Fatal(err)
	}
	if err := h.Ensure(Path{W("pPr")}, W("keepNext")); err != nil { // before jc in CT_PPrBase
		t.Fatal(err)
	}
	if got := names(p); got != "w:pPr w:r" {
		t.Fatalf("p children %q", got)
	}
	if got := names(h.Find(Path{W("pPr")})); got != "w:keepNext w:jc" {
		t.Fatalf("pPr children %q", got)
	}
	if err := h.Ensure(nil, W("r")); !errors.Is(err, ErrRepeated) {
		t.Fatalf("Ensure of a repeated child: %v", err)
	}
	if err := h.Ensure(nil, W("tbl")); !errors.Is(err, ErrNotAChild) {
		t.Fatalf("Ensure of a table in a paragraph: %v", err)
	}
	if err := w.Verify(); err != nil {
		t.Fatal(err)
	}
}

func TestSetChecked(t *testing.T) {
	w, body := open(t)
	h := w.Target(docPart, "p2", []*tree.Node{paras(body)[1]})
	jc := Path{W("pPr"), W("jc")}
	val := Attr{NS: wNS, Local: "val"}
	if err := h.SetChecked(jc, val, "bogus"); !errors.Is(err, schema.ErrSchema) {
		t.Fatalf("bogus jc: %v", err)
	}
	if err := h.SetChecked(jc, val, "center"); err != nil {
		t.Fatal(err)
	}
	if got := string(h.Find(jc).Bytes()); got != `<w:jc w:val="center"/>` {
		t.Fatalf("jc = %s", got)
	}
	if err := w.Verify(); err != nil {
		t.Fatal(err)
	}
}

func TestUnitsInBody(t *testing.T) {
	w, body := open(t)
	// The front of the body's block slot is before w:sectPr.
	at, err := w.SlotEnd(body, "w:CT_Body", W("p"), true, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	nodes, err := w.Parse(body, []byte(`<w:p><w:r><w:t>three</w:t></w:r></w:p>`), map[string]string{"w": wURI})
	if err != nil || len(nodes) != 1 {
		t.Fatalf("parse: %v", err)
	}
	if err := w.Insert(docPart, at, nodes[0], []NodeAttr{{Node: nodes[0], Name: "w:rsidR", Value: "00AB12CD"}}); err != nil {
		t.Fatal(err)
	}
	if got := names(body); got != "w:p w:p w:p w:sectPr" {
		t.Fatalf("body %q", got)
	}
	// Move the new paragraph to the back, then delete the first.
	ps := paras(body)
	back, err := w.SlotEnd(body, "w:CT_Body", W("p"), false, ps[2], nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := w.Move(docPart, ps[2], back); err != nil {
		t.Fatal(err)
	}
	if err := w.Delete(docPart, ps[0]); err != nil {
		t.Fatal(err)
	}
	var texts []string
	for _, p := range paras(body) {
		texts = append(texts, text(p))
	}
	if got := strings.Join(texts, ","); got != "three,two" {
		t.Fatalf("paragraphs %q", got)
	}
	if err := w.Verify(); err != nil {
		t.Fatal(err)
	}
}

func TestInsertRefusesForeignAttribute(t *testing.T) {
	w, body := open(t)
	nodes, err := w.Parse(body, []byte(`<w:p/>`), map[string]string{"w": wURI})
	if err != nil {
		t.Fatal(err)
	}
	at, err := w.SlotEnd(body, "w:CT_Body", W("p"), true, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	before := string(body.Bytes())
	if err := w.Insert(docPart, at, nodes[0], []NodeAttr{{Node: paras(body)[0], Name: "x", Value: "1"}}); err == nil {
		t.Fatal("an attribute outside the unit was set")
	}
	if string(body.Bytes()) != before {
		t.Fatal("a refused insert changed the body")
	}
}

func TestReplaceRun(t *testing.T) {
	w, body := open(t)
	p := paras(body)[1]
	h := w.Target(docPart, "p2", []*tree.Node{p})
	if err := h.ReplaceRun(nil, W("r"), [][]byte{[]byte(`<w:r><w:t>2</w:t></w:r> <w:r><w:t>!</w:t></w:r>`)}); err != nil {
		t.Fatal(err)
	}
	if got := names(p); got != "w:pPr w:r w:r" || text(p) != "2!" {
		t.Fatalf("p %q %q", got, text(p))
	}
	for _, bad := range []string{`<w:p/>`, ``, `text`} {
		if err := h.ReplaceRun(nil, W("r"), [][]byte{[]byte(bad)}); err == nil {
			t.Errorf("fragment %q accepted", bad)
		}
	}
	if err := w.Verify(); err != nil {
		t.Fatal(err)
	}
}

func TestVerifyHoldsHandlesToTheirUnit(t *testing.T) {
	w, body := open(t)
	ps := paras(body)
	w.Target(docPart, "p1", []*tree.Node{ps[0]})
	peek := w.Peek(docPart, "p2", []*tree.Node{ps[1]})
	if err := peek.SetAttr(Path{W("pPr"), W("jc")}, "w:val", "right"); err != nil {
		t.Fatal(err)
	}
	if err := w.Verify(); !errors.Is(err, plan.ErrUndeclared) {
		t.Fatalf("Verify = %v", err)
	}
}

func TestPathBound(t *testing.T) {
	w, body := open(t)
	h := w.Target(docPart, "body", []*tree.Node{body})
	if err := h.SetAttr(Path{W("p")}, "x", "1"); err == nil || !strings.Contains(err.Error(), "another object") {
		t.Fatalf("path into a unit: %v", err)
	}
}

// reading is a test Reading.
type reading struct {
	v  string
	ok bool
}

func (r reading) Get() (string, bool) { return r.v, r.ok }
func (r reading) Reason() string      { return "absent" }

var errTest = errors.New("test: inexpressible")

func TestCodecSet(t *testing.T) {
	w, body := open(t)
	h := w.Target(docPart, "p2", []*tree.Node{paras(body)[1]})
	jc := Path{W("pPr"), W("jc")}
	writes := 0
	c := Codec[*Handle, string, reading]{
		Name: "align",
		Read: func(h *Handle, i int) reading {
			v, ok := h.FindIn(i, jc).AttrNS(wURI, "val")
			return reading{v, ok}
		},
		Write: func(h *Handle, v string) error {
			writes++
			return h.SetChecked(jc, Attr{NS: wNS, Local: "val"}, v)
		},
		Show: strings.ToLower,
	}
	if err := Set(h, c, "LEFT", errTest); err != nil || writes != 0 {
		t.Fatalf("identity: %v, %d writes", err, writes)
	}
	if err := Set(h, c, "center", errTest); err != nil || writes != 1 {
		t.Fatalf("set: %v, %d writes", err, writes)
	}
	c.Write = func(h *Handle, _ string) error { return h.SetChecked(jc, Attr{NS: wNS, Local: "val"}, "both") }
	if err := Set(h, c, "right", errTest); !errors.Is(err, errTest) {
		t.Fatalf("a value that does not read back: %v", err)
	}
}
