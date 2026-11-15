package apply

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jhnoel/documentdriver/internal/docx/model"
	"github.com/jhnoel/documentdriver/internal/docx/ops"
	"github.com/jhnoel/documentdriver/internal/docx/view"
	"github.com/jhnoel/documentdriver/internal/laws"
	"github.com/jhnoel/documentdriver/internal/repo"
)

// TestInsertParagraphAsRead: a paragraph's markup as read shows it,
// sent back as paragraph.insert's html, makes a paragraph that reads the
// same: the block element's tag, style and paragraph CSS are kept, its
// id and the other derived attributes are ignored.
func TestInsertParagraphAsRead(t *testing.T) {
	root, err := repo.Root()
	if err != nil {
		t.Fatal(err)
	}
	src := filepath.Join(root, "tools", "oracle", "testdata", "write", "mixed.docx")
	in, err := os.ReadFile(src) //nolint:gosec // G304: a fixture
	if err != nil {
		t.Fatal(err)
	}
	d, err := model.OpenDocument(in)
	if err != nil {
		t.Fatal(err)
	}
	// A heading with bold runs, a plain paragraph, a centred one, an
	// inline equation and a line break.
	ids := []string{"10000001", "10000002", "1000000A", "1000000B", "1000000C"}
	markup, err := view.AffectedMarkup(d, ids, viewOpt)
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range ids {
		m := markup[id]
		if m == "" {
			t.Fatalf("no markup for %s", id)
		}
		dst := filepath.Join(t.TempDir(), "mixed.docx")
		if err := laws.CopyFile(src, dst); err != nil {
			t.Fatal(err)
		}
		spec := &ops.InsertParagraph{At: ops.InsertAnchor(id), Position: "after", HTML: ops.ParagraphContent{Items: []string{m}}, Alias: ops.Ptr("n")}
		p, dg := ops.ParsePlanJSON(planOf(t, dst, "direct", spec))
		if dg != nil {
			t.Fatalf("%s: %s", id, dg.Message)
		}
		r, err := Run(dst, p, Options{Commit: p.PreviewKey(), Now: "2026-01-01T00:00:00Z", View: viewOpt})
		if err != nil || r.Outcome != Committed {
			t.Fatalf("%s: %v %+v\n%s", id, err, r.Diagnostic, m)
		}
		newID := r.Aliases["n"]
		got := strings.ReplaceAll(r.Markup[newID], newID, id)
		if got != m {
			t.Errorf("%s: inserted back reads\n%s\nwant\n%s", id, got, m)
		}
	}
}

// TestParagraphHTMLForms: bare inline content is the content alone; one
// block element is the paragraph; two blocks, or inline content with a
// block in it, are refused at the field with one clear message.
func TestParagraphHTMLForms(t *testing.T) {
	path := writeDocx(t, `<w:p w14:paraId="0000000A"><w:pPr><w:jc w:val="right"/></w:pPr><w:r><w:t>a</w:t></w:r></w:p>`)
	for _, c := range []struct {
		html       string
		code, path string
		want       string
	}{
		{html: `plain <b>bold</b>`, want: `<w:jc w:val="right"/>`},                   // inherits the anchor's properties
		{html: `<p style="text-align:center">x</p>`, want: `<w:jc w:val="center"/>`}, // the block's own
		{html: `<p id="0000000A" data-break-del="3">x</p>`, want: `<w:p w14:paraId`}, // derived attributes ignored
		{html: `<p>a</p><p>b</p>`, code: "invalid_operation", path: "ops[0].html", want: "one paragraph per html value; use the list form for several"},
		{html: `x <p>y</p>`, code: "invalid_operation", path: "ops[0].html", want: "one paragraph per html value"},
		{html: `a</p>`, code: "invalid_operation", path: "ops[0].html", want: "one paragraph per html value"},
		{html: `<h1 data-style="Quote">x</h1>`, code: "invalid_operation", path: "ops[0].html", want: "takes its style from the tag"},
	} {
		spec := &ops.InsertParagraph{At: "0000000A", Position: "after", HTML: ops.ParagraphContent{Items: []string{c.html}}}
		r, err := runText(path, planOf(t, path, "direct", spec), Options{View: viewOpt})
		if err != nil {
			t.Fatal(err)
		}
		if c.code != "" {
			if r.Outcome != Rejected || r.Diagnostic.Code != c.code || r.Diagnostic.Path != c.path || !strings.Contains(r.Diagnostic.Message, c.want) {
				t.Errorf("%s: %s %+v", c.html, r.Outcome, r.Diagnostic)
			}
			continue
		}
		if r.Outcome != Previewed {
			t.Fatalf("%s: %s %+v", c.html, r.Outcome, r.Diagnostic)
		}
		doc := string(zipEntries(t, r.Snapshot)["word/document.xml"])
		second := doc[strings.LastIndex(doc, "<w:p "):]
		if !strings.Contains(second, c.want) {
			t.Errorf("%s: the new paragraph is %s, want %s in it", c.html, second, c.want)
		}
	}
	// The list form takes one paragraph per item, refused by its index.
	spec := &ops.InsertParagraph{At: "0000000A", Position: "after", HTML: ops.ParagraphContent{List: true, Items: []string{"<p>one</p>", "<p>two</p><p>three</p>"}}}
	r, err := runText(path, planOf(t, path, "direct", spec), Options{View: viewOpt})
	if err != nil || r.Diagnostic == nil || r.Diagnostic.Path != "ops[0].html[1]" {
		t.Errorf("list form: %v %+v", err, r.Diagnostic)
	}
}
