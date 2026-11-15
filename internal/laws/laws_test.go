package laws

import (
	"archive/zip"
	"errors"
	"fmt"
	"math/rand/v2"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/jhnoel/documentdriver/internal/ooxml/ctypes"
	"github.com/jhnoel/documentdriver/internal/ooxml/edit"
	"github.com/jhnoel/documentdriver/internal/ooxml/tree"
	"github.com/jhnoel/documentdriver/internal/opc"
	"github.com/jhnoel/documentdriver/internal/plan"
)

// The harness's own tests run a law suite for paragraph alignment
// (w:jc) on a small WordprocessingML format built directly on the edit
// core and plan transactions: the shape a docx law suite takes.

const wURI = "http://schemas.openxmlformats.org/wordprocessingml/2006/main"

var wNS = edit.NS{URIs: []string{wURI, "http://purl.oclc.org/ooxml/wordprocessingml/main"}, Prefix: "w", Table: "w"}

const docPart = "/word/document.xml"

// blocks are the units of a document: the body and its paragraphs.
type blocks struct{}

func (blocks) HasUnits(doc *tree.Document) bool { return wNS.Is(doc.Root(), "document") }
func (blocks) IsUnit(e *tree.Node) bool {
	return wNS.Is(e, "body") || wNS.Is(e, "p") && wNS.Is(e.Parent(), "body")
}
func (blocks) Describe(e *tree.Node) string { return e.Name() }

var wml = edit.Format{
	Vocab: edit.Vocab{wNS, edit.REL},
	Units: blocks{},
	Type: func(e *tree.Node) (string, bool) {
		if wNS.Is(e, "p") {
			return "w:CT_P", true
		}
		return "", false
	},
	Bound: func(e *tree.Node) bool { return wNS.Is(e, "p") },
}

// docx writes a WordprocessingML package with the given paragraphs.
func docx(t *testing.T, paras ...string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "doc.docx")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	z := zip.NewWriter(f)
	body := `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>` + "\n" +
		`<w:document xmlns:w="` + wURI + `"><w:body>` + strings.Join(paras, "\n") + `<w:sectPr/></w:body></w:document>`
	for name, b := range map[string]string{
		"[Content_Types].xml": `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>` +
			`<Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types">` +
			`<Default Extension="rels" ContentType="` + ctypes.CTPackageRelationships + `"/>` +
			`<Default Extension="xml" ContentType="application/xml"/>` +
			`<Override PartName="/word/document.xml" ContentType="` + ctypes.CTWordprocessingmlDocumentMain + `"/></Types>`,
		"_rels/.rels": `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>` +
			`<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">` +
			`<Relationship Id="rId1" Type="` + ctypes.RelOfficeDocument + `" Target="word/document.xml"/></Relationships>`,
		"word/document.xml": body,
	} {
		w, err := z.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write([]byte(b)); err != nil {
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

// paragraphs returns the body's paragraphs.
func paragraphs(doc *tree.Document) []*tree.Node {
	var out []*tree.Node
	for _, e := range doc.Root().Elements()[0].Elements() {
		if wNS.Is(e, "p") {
			out = append(out, e)
		}
	}
	return out
}

var jc = edit.Path{wNS.Name("pPr"), wNS.Name("jc")}

// align reads a paragraph's alignment as shown: its w:jc value, or "".
func align(p *tree.Node) string {
	for _, c := range p.Elements() {
		if wNS.Is(c, "pPr") {
			for _, cc := range c.Elements() {
				if wNS.Is(cc, "jc") {
					v, _ := cc.AttrNS(wURI, "val")
					return v
				}
			}
		}
	}
	return ""
}

// op is one operation of the test format.
type op func(w *edit.Workspace, doc *tree.Document) error

// setAlign sets paragraph k's alignment through the core.
func setAlign(k int, v string) op {
	return func(w *edit.Workspace, doc *tree.Document) error {
		ps := paragraphs(doc)
		if k >= len(ps) {
			return fmt.Errorf("no paragraph %d", k)
		}
		if align(ps[k]) == v {
			return nil // the round-trip rule
		}
		h := w.Target(docPart, "p"+strconv.Itoa(k), []*tree.Node{ps[k]})
		if err := h.Ensure(nil, wNS.Name("pPr")); err != nil {
			return err
		}
		if err := h.Ensure(edit.Path{wNS.Name("pPr")}, wNS.Name("jc")); err != nil {
			return err
		}
		return h.SetChecked(jc, edit.Attr{NS: wNS, Local: "val"}, v)
	}
}

// apply runs ops in one plan transaction.
func apply(src, dst string, ops []Op) error {
	var w *edit.Workspace
	tx, err := plan.Begin(src, func(p *opc.Package) plan.Workspace { w = edit.New(p, wml); return w })
	if err != nil {
		return err
	}
	doc, err := w.Tree(docPart)
	if err != nil {
		tx.Rollback()
		return err
	}
	for i, o := range ops {
		if err := tx.Run(strconv.Itoa(i), func() error { return o.(op)(w, doc) }); err != nil {
			tx.Rollback()
			return err
		}
	}
	if dst == src {
		return tx.Commit()
	}
	return tx.CommitTo(dst)
}

func read(path string) ([]Value[string], error) {
	ps, err := Parts(path)
	if err != nil {
		return nil, err
	}
	doc, err := tree.Parse(ps[docPart])
	if err != nil {
		return nil, err
	}
	var out []Value[string]
	for k, p := range paragraphs(doc) {
		out = append(out, Value[string]{Target{docPart, "p" + strconv.Itoa(k)}, align(p)})
	}
	return out, nil
}

func index(t Target) int {
	k, err := strconv.Atoi(strings.TrimPrefix(t.Addr, "p"))
	if err != nil {
		panic(err)
	}
	return k
}

var aligns = []string{"left", "center", "right", "both"}

func alignSuite() *Suite[string] {
	return &Suite[string]{
		Name: "align",
		Read: read,
		Show: func(v string) string { return v },
		Set:  func(t Target, v string) Op { return setAlign(index(t), v) },
		Vary: func(rng *rand.Rand, _ Target, cur string) string {
			for {
				if v := aligns[rng.IntN(len(aligns))]; v != cur {
					return v
				}
			}
		},
		Apply:   apply,
		Refused: func(error) bool { return false },
		Units:   blocks{},
		Owns: func(t Target, e *tree.Node) bool {
			k := 0
			for _, c := range e.Parent().Elements() {
				if c == e {
					return k == index(t)
				}
				if wNS.Is(c, "p") {
					k++
				}
			}
			return false
		},
		Fail: func() Op { return op(func(*edit.Workspace, *tree.Document) error { return errors.New("injected") }) },
		Undo: func(_ *rand.Rand, t Target) (Op, Op) {
			return setAlign(index(t), "both"), setAlign(index(t), "left")
		},
	}
}

func TestSuiteOnWordprocessingML(t *testing.T) {
	src := docx(t,
		`<w:p><w:pPr><w:jc w:val="left"/></w:pPr><w:r><w:t>one</w:t></w:r></w:p>`,
		`<w:p><w:pPr><w:jc w:val="left"/></w:pPr><w:r><w:t>two</w:t></w:r></w:p>`,
		`<w:p><w:pPr><w:jc w:val="left"/></w:pPr><w:r><w:t>three</w:t></w:r></w:p>`)
	var rep Report
	alignSuite().Check(t, src, 1, &rep)
	t.Log(rep)
	if rep.IdentityPlans != 1 || rep.InverseOK != 3 || rep.AtomicPlans == 0 || rep.AtomicStages == 0 || rep.UndoPlans != 3 || rep.SequencePairs != 1 {
		t.Fatalf("laws exercised too little: %v", rep)
	}
}

func TestUntargetedCatchesNeighbours(t *testing.T) {
	a := docx(t, `<w:p><w:r><w:t>one</w:t></w:r></w:p>`, `<w:p><w:r><w:t>two</w:t></w:r></w:p>`)
	b := docx(t, `<w:p><w:r><w:t>ONE</w:t></w:r></w:p>`, `<w:p><w:r><w:t>two</w:t></w:r></w:p>`)
	first := func(e *tree.Node) bool { return wNS.Is(e, "p") && strings.Contains(string(e.Bytes()), "one") }
	second := func(e *tree.Node) bool { return wNS.Is(e, "p") && strings.Contains(string(e.Bytes()), "two") }
	if err := Untargeted(a, b, docPart, blocks{}, first); err != nil {
		t.Fatalf("the target's own change was refused: %v", err)
	}
	if err := Untargeted(a, b, docPart, blocks{}, second); err == nil || !strings.Contains(err.Error(), "untargeted w:p changed") {
		t.Fatalf("a neighbour's change went unnoticed: %v", err)
	}
	c := docx(t, `<w:p><w:r><w:t>one</w:t></w:r></w:p>`, `<w:p><w:r><w:t>two</w:t></w:r></w:p>`, `<w:p/>`)
	if err := Untargeted(a, c, docPart, blocks{}, first); err == nil {
		t.Fatal("an added unit went unnoticed")
	}
}

func TestSnapshotSeesWrites(t *testing.T) {
	path := Private(t, docx(t, `<w:p/>`))
	before := snapshot(t, path)
	if err := apply(path, path, []Op{setAlign(0, "center")}); err != nil {
		t.Fatal(err)
	}
	if before.same(path) == nil {
		t.Fatal("a write went unnoticed")
	}
	before.restore(t, path)
	if err := before.same(path); err != nil {
		t.Fatalf("restore: %v", err)
	}
	if err := os.WriteFile(filepath.Join(filepath.Dir(path), "tmp"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if before.same(path) == nil {
		t.Fatal("a stray temporary file went unnoticed")
	}
}
