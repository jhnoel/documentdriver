package revise

import (
	"archive/zip"
	"bytes"
	"errors"
	"testing"

	docxedit "github.com/jhnoel/documentdriver/internal/docx/edit"
	"github.com/jhnoel/documentdriver/internal/docx/model"
	"github.com/jhnoel/documentdriver/internal/ooxml/ctypes"
	"github.com/jhnoel/documentdriver/internal/ooxml/edit"
	"github.com/jhnoel/documentdriver/internal/ooxml/tree"
	"github.com/jhnoel/documentdriver/internal/opc"
)

// TestFinishSettlesThroughEachHandle: an operation that records
// property changes through two regions (one per paragraph) finishes in
// both modes: each authored revision is numbered or accepted through the
// handle it was authored with, not the first one's.
func TestFinishSettlesThroughEachHandle(t *testing.T) {
	body := `<w:p><w:pPr><w:jc w:val="left"/></w:pPr><w:r><w:t>a</w:t></w:r></w:p>` +
		`<w:p><w:pPr><w:jc w:val="left"/></w:pPr><w:r><w:t>b</w:t></w:r></w:p>`
	for _, mode := range []Mode{Track, Direct} {
		ws := workspaceOf(t, body)
		part := ws.DocumentPart()
		doc, err := ws.Tree(part)
		if err != nil {
			t.Fatal(err)
		}
		r := New("Ann", clock, mode)
		for _, p := range model.ParagraphsIn((&model.XML{Doc: doc}).Body()) {
			h, err := ws.Core().Range(part, "paragraph", p.Node, p.Node)
			if err != nil {
				t.Fatal(err)
			}
			ppr := model.ChildW(p.Node, "pPr")
			jc := model.ChildW(ppr, "jc")
			if _, err := r.Property(h, ppr, func() error { return h.SetNodeAttr(jc, "w:val", "center") }); err != nil {
				t.Fatal(err)
			}
		}
		n, err := r.Finish()
		if err != nil || n != 2 {
			t.Fatalf("mode %v: Finish = %d, %v; want 2 revisions", mode, n, err)
		}
		if err := ws.Verify(); err != nil {
			t.Fatalf("mode %v: %v", mode, err)
		}
	}
}

// workspaceOf opens an edit workspace on a package whose main document's
// body is body.
func workspaceOf(t *testing.T, body string) *docxedit.Workspace {
	t.Helper()
	var b bytes.Buffer
	z := zip.NewWriter(&b)
	for _, p := range []struct{ name, body string }{
		{"[Content_Types].xml", `<Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types">` +
			`<Default Extension="rels" ContentType="` + ctypes.CTPackageRelationships + `"/>` +
			`<Override PartName="/word/document.xml" ContentType="` + ctypes.CTWordprocessingmlDocumentMain + `"/></Types>`},
		{"_rels/.rels", `<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">` +
			`<Relationship Id="rId1" Type="` + ctypes.RelOfficeDocument + `" Target="word/document.xml"/></Relationships>`},
		{"word/document.xml", head + body + `</w:body></w:document>`},
	} {
		w, err := z.Create(p.name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write([]byte(p.body)); err != nil {
			t.Fatal(err)
		}
	}
	if err := z.Close(); err != nil {
		t.Fatal(err)
	}
	pkg, err := opc.OpenBytes(b.Bytes(), opc.DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	ws, err := docxedit.New(pkg, docxedit.Options{})
	if err != nil {
		t.Fatal(err)
	}
	return ws
}

// TestRangeOwnsWhatItPlaces: a paragraph a range places among its
// members is the range's (an operation may then edit and settle inside
// it), while a paragraph outside it stays refused, also one an earlier
// operation created.
func TestRangeOwnsWhatItPlaces(t *testing.T) {
	body := `<w:p><w:r><w:t>a</w:t></w:r></w:p><w:p><w:r><w:t>b</w:t></w:r></w:p>`
	for _, mode := range []Mode{Track, Direct} {
		ws := workspaceOf(t, body)
		part := ws.DocumentPart()
		doc, err := ws.Tree(part)
		if err != nil {
			t.Fatal(err)
		}
		x := &model.XML{Doc: doc}
		ps := model.ParagraphsIn(x.Body())
		first, second := ps[0].Node, ps[1].Node
		// An earlier operation's paragraph, placed before the range opens.
		earlier := newParagraph(t, ws, x.Body())
		if err := ws.Core().Story(part, "body", x.Body()).PlaceAt(x.Body(), earlier, second); err != nil {
			t.Fatal(err)
		}
		h, err := ws.Core().Range(part, "paragraph", first, first)
		if err != nil {
			t.Fatal(err)
		}
		placed := newParagraph(t, ws, x.Body())
		if err := h.PlaceAt(x.Body(), placed, earlier); err != nil {
			t.Fatal(err)
		}
		r := New("Ann", clock, mode)
		ppr, err := h.EnsureIn(placed, docxedit.W("pPr"))
		if err != nil {
			t.Fatalf("mode %v: the placed paragraph is not the range's: %v", mode, err)
		}
		if _, err := r.Property(h, ppr, func() error {
			jc, err := h.EnsureIn(ppr, docxedit.W("jc"))
			if err != nil {
				return err
			}
			return h.SetNodeAttr(jc, "w:val", "center")
		}); err != nil {
			t.Fatal(err)
		}
		if _, err := r.Finish(); err != nil {
			t.Fatalf("mode %v: %v", mode, err)
		}
		for _, outside := range []*tree.Node{second, earlier} {
			if _, err := h.EnsureIn(outside, docxedit.W("pPr")); !errors.Is(err, edit.ErrOutside) {
				t.Errorf("mode %v: a paragraph outside the range was edited through it: %v", mode, err)
			}
		}
	}
}

// newParagraph parses a detached empty paragraph for placement in body.
func newParagraph(t *testing.T, ws *docxedit.Workspace, body *tree.Node) *tree.Node {
	t.Helper()
	nodes, err := ws.Core().Parse(body, []byte(`<w:p/>`), map[string]string{"w": model.NSW})
	if err != nil || len(nodes) != 1 {
		t.Fatalf("parse: %v", err)
	}
	return nodes[0]
}
