package apply

import (
	"archive/zip"
	"bytes"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/jhnoel/documentdriver/internal/docx/model"
	"github.com/jhnoel/documentdriver/internal/docx/ops"
	"github.com/jhnoel/documentdriver/internal/ooxml/ctypes"
)

// headerDocx writes a package whose main document's paragraphs have no
// paraIds and whose header holds a tracked insertion.
func headerDocx(t *testing.T) string {
	t.Helper()
	const decl = `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>`
	parts := []struct{ name, body string }{
		{"[Content_Types].xml", decl + `<Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types">` +
			`<Default Extension="rels" ContentType="` + ctypes.CTPackageRelationships + `"/>` +
			`<Default Extension="xml" ContentType="application/xml"/>` +
			`<Override PartName="/word/document.xml" ContentType="` + ctypes.CTWordprocessingmlDocumentMain + `"/>` +
			`<Override PartName="/word/header1.xml" ContentType="` + ctypes.CTWordprocessingmlHeader + `"/></Types>`},
		{"_rels/.rels", decl + `<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">` +
			`<Relationship Id="rId1" Type="` + ctypes.RelOfficeDocument + `" Target="word/document.xml"/></Relationships>`},
		{"word/_rels/document.xml.rels", decl + `<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">` +
			`<Relationship Id="rId1" Type="` + ctypes.RelHeader + `" Target="header1.xml"/></Relationships>`},
		{"word/document.xml", decl + `<w:document` + nsDecl + ` xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships"><w:body>` +
			`<w:p><w:r><w:t>one</w:t></w:r></w:p><w:p><w:r><w:t>two</w:t></w:r></w:p>` +
			`<w:sectPr><w:headerReference w:type="default" r:id="rId1"/></w:sectPr></w:body></w:document>`},
		{"word/header1.xml", decl + `<w:hdr` + nsDecl + `><w:p w14:paraId="00000010"><w:ins w:id="1" w:author="Bob" w:date="2020-01-01T00:00:00Z"><w:r><w:t>head</w:t></w:r></w:ins></w:p></w:hdr>`},
	}
	var b bytes.Buffer
	z := zip.NewWriter(&b)
	for _, p := range parts {
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
	path := filepath.Join(t.TempDir(), "doc.docx")
	if err := os.WriteFile(path, b.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

// mainAddresses are the main document's paragraph addresses and how many of
// them are not stored.
func mainAddresses(t *testing.T, b []byte) ([]string, int) {
	t.Helper()
	d, err := model.OpenDocument(b)
	if err != nil {
		t.Fatal(err)
	}
	idx := d.ParaIDs()
	var out []string
	repairs := 0
	for _, p := range d.XML.AllParagraphs() {
		out = append(out, idx.IDs[p].ID)
		if idx.Repaired[p] {
			repairs++
		}
	}
	return out, repairs
}

// TestHeaderOnlyEditStoresParaIDs: a plan that changes only a header (it
// accepts the header's tracked insertion) still writes the main
// document's missing paragraph ids, so read on the output gives the
// addresses read gave the input and needs no repair.
func TestHeaderOnlyEditStoresParaIDs(t *testing.T) {
	path := headerDocx(t)
	in, err := os.ReadFile(path) //nolint:gosec // G304: the test's own file
	if err != nil {
		t.Fatal(err)
	}
	before, repairs := mainAddresses(t, in)
	if repairs != 2 {
		t.Fatalf("input: %d repairs needed, want 2", repairs)
	}
	for _, mode := range []string{"track", "direct"} {
		out := filepath.Join(t.TempDir(), mode+".docx")
		text := planOf(t, path, mode, settle("all", "accept"))
		p, d := ops.ParsePlanJSON(text)
		if d != nil {
			t.Fatalf("%s: %s", mode, d.Message)
		}
		r, err := Run(path, p, Options{Commit: p.PreviewKey(), Out: out, Now: "2026-01-01T00:00:00Z", View: viewOpt})
		if err != nil {
			t.Fatal(err)
		}
		if r.Outcome != Committed {
			t.Fatalf("%s: %s %+v", mode, r.Outcome, r.Diagnostic)
		}
		if !slices.Contains(r.Parts, "/word/header1.xml") {
			t.Fatalf("%s: parts %v, want the header changed", mode, r.Parts)
		}
		b, err := os.ReadFile(out) //nolint:gosec // G304: the test's own output
		if err != nil {
			t.Fatal(err)
		}
		after, repairs := mainAddresses(t, b)
		if repairs != 0 || !slices.Equal(after, before) {
			t.Errorf("%s: output addresses %v (%d repairs needed), want %v stored", mode, after, repairs, before)
		}
	}
}

// TestHeaderParagraphChanged: a changed header paragraph with a stored
// paraId is in the result's changed paragraphs under that address, with
// its header's markup, where read shows it as the paragraph's id.
func TestHeaderParagraphChanged(t *testing.T) {
	path := headerDocx(t)
	p, d := ops.ParsePlanJSON(planOf(t, path, "direct", settle("all", "accept")))
	if d != nil {
		t.Fatal(d.Message)
	}
	r, err := Run(path, p, Options{Commit: p.PreviewKey(), Out: filepath.Join(t.TempDir(), "out.docx"), Now: "2026-01-01T00:00:00Z", View: viewOpt})
	if err != nil || r.Outcome != Committed {
		t.Fatalf("%v %+v", err, r)
	}
	if !slices.Contains(r.Changed, "00000010") {
		t.Fatalf("changed %v, want the header paragraph 00000010", r.Changed)
	}
	if m := r.Markup["00000010"]; !strings.Contains(m, `<header>`) || !strings.Contains(m, `<p id="00000010">head</p>`) {
		t.Errorf("markup %q, want the header with the paragraph's id", m)
	}
}
