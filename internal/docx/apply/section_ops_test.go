package apply

import (
	"encoding/json"
	"errors"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/jhnoel/documentdriver/internal/docx/math"
	"github.com/jhnoel/documentdriver/internal/docx/model"
	"github.com/jhnoel/documentdriver/internal/docx/read"
	"github.com/jhnoel/documentdriver/internal/docx/view"
	"github.com/jhnoel/documentdriver/internal/laws"
)

// sectionBody is a two-section body: the first ends at paragraph A with
// its own margins and a pending change by Bob; the body's final section
// has none.
const sectionBody = `<w:p w14:paraId="0000000A"><w:pPr><w:sectPr><w:pgMar w:top="1440" w:right="1440" w:bottom="1440" w:left="1440" w:header="720" w:footer="720" w:gutter="0"/>` +
	`<w:sectPrChange w:id="5" w:author="Bob" w:date="2020-01-01T00:00:00Z"><w:sectPr/></w:sectPrChange></w:sectPr></w:pPr><w:r><w:t>a</w:t></w:r></w:p>` +
	`<w:p w14:paraId="0000000B"><w:r><w:t>b</w:t></w:r></w:p>`

// TestSectionSetMargins: track records the margins before the change in
// a w:sectPrChange, direct writes none; a missing w:pgMar starts from
// Word's defaults; another author's pending change is refused in track
// mode only; margins already as given write nothing; a section past the
// last is not_found at the address.
func TestSectionSetMargins(t *testing.T) {
	op := `[{"op":"section.set_margins","at":"section:2","top":36,"gutter":9}]`
	path := writeDocx(t, sectionBody)
	if c := commit(t, path, opsText(t, path, "track", op)); c.Outcome != Committed {
		t.Fatalf("%+v", c.Diagnostic)
	}
	got := mainXML(t, path)
	want := `<w:sectPr><w:pgMar w:top="720" w:right="1440" w:bottom="1440" w:left="1440" w:header="720" w:footer="720" w:gutter="180"/><w:sectPrChange w:id="6" w:author="Ann" w:date="2026-01-01T00:00:00Z"><w:sectPr></w:sectPr></w:sectPrChange></w:sectPr></w:body>`
	if !strings.Contains(got, want) {
		t.Errorf("track: want %s in %s", want, got)
	}
	path = writeDocx(t, sectionBody)
	if c := commit(t, path, opsText(t, path, "direct", op)); c.Outcome != Committed {
		t.Fatalf("%+v", c.Diagnostic)
	}
	if got := mainXML(t, path); strings.Count(got, "sectPrChange") != 2 || !strings.Contains(got, `w:gutter="180"/></w:sectPr></w:body>`) {
		t.Errorf("direct: %s", got)
	}
	for _, c := range []struct {
		mode, ops, code, path string
	}{
		{"track", `[{"op":"section.set_margins","at":"section:1","top":36}]`, "operation_failed", "ops[0].at"},
		{"direct", `[{"op":"section.set_margins","at":"section:3","top":36}]`, "not_found", "ops[0].at"},
	} {
		path := writeDocx(t, sectionBody)
		r, _ := runText(path, opsText(t, path, c.mode, c.ops), Options{})
		if r.Outcome != Rejected || r.Diagnostic.Code != c.code || r.Diagnostic.Path != c.path {
			t.Errorf("%s %s: %+v", c.mode, c.ops, r.Diagnostic)
		}
	}
	path = writeDocx(t, sectionBody)
	if c := commit(t, path, opsText(t, path, "direct", `[{"op":"section.set_margins","at":"section:1","top":36}]`)); c.Outcome != Committed {
		t.Fatalf("direct beside a pending change: %+v", c.Diagnostic)
	}
	path = writeDocx(t, sectionBody)
	if c := commit(t, path, opsText(t, path, "direct", `[{"op":"section.set_margins","at":"section:1","top":72,"gutter":0}]`)); c.Outcome != Committed || len(c.Parts) != 0 {
		t.Errorf("margins as they are: %s %v", c.Outcome, c.Parts)
	}
}

// TestSectionSetLayout: a landscape A4 page with two columns of given
// widths in track mode, recorded in a w:sectPrChange; direct mode refuses
// a section with any pending change; widths that do not fit are refused
// at column_widths.
func TestSectionSetLayout(t *testing.T) {
	op := `[{"op":"section.set_layout","at":"section:2","page_size":"a4","orientation":"landscape","column_widths":[300,300],"column_spacing":18,"page_number_start":4}]`
	path := writeDocx(t, sectionBody)
	if c := commit(t, path, opsText(t, path, "track", op)); c.Outcome != Committed {
		t.Fatalf("%+v", c.Diagnostic)
	}
	got := mainXML(t, path)
	want := `<w:sectPr><w:pgSz w:w="16838" w:h="11906" w:orient="landscape"/><w:pgNumType w:start="4"/><w:cols w:num="2" w:space="360" w:equalWidth="0"><w:col w:w="6000" w:space="360"/><w:col w:w="6000" w:space="0"/></w:cols><w:sectPrChange w:id="6" w:author="Ann" w:date="2026-01-01T00:00:00Z"><w:sectPr></w:sectPr></w:sectPrChange></w:sectPr></w:body>`
	if !strings.Contains(got, want) {
		t.Errorf("track: want %s in %s", want, got)
	}
	for _, c := range []struct {
		mode, ops, code, path string
	}{
		{"direct", `[{"op":"section.set_layout","at":"section:1","break_type":"continuous"}]`, "operation_failed", "ops[0].at"},
		{"direct", `[{"op":"section.set_layout","at":"section:2","column_widths":[400,400]}]`, "operation_failed", "ops[0].column_widths"},
	} {
		path := writeDocx(t, sectionBody)
		r, _ := runText(path, opsText(t, path, c.mode, c.ops), Options{})
		if r.Outcome != Rejected || r.Diagnostic.Code != c.code || r.Diagnostic.Path != c.path {
			t.Errorf("%s %s: %+v", c.mode, c.ops, r.Diagnostic)
		}
	}
}

// readSectionIDs returns the section ids the document read of path
// shows, in order.
func readSectionIDs(t *testing.T, path string) []string {
	t.Helper()
	res, err := read.Read(mustRead(t, path), "document", "", read.Options{Equation: math.OMMLToLaTeX})
	if err != nil {
		t.Fatal(err)
	}
	s, err := read.Encode(res.Object(), false)
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		Sections []struct {
			ID string `json:"id"`
		} `json:"sections"`
	}
	if err := json.Unmarshal([]byte(s), &doc); err != nil {
		t.Fatal(err)
	}
	var ids []string
	for _, x := range doc.Sections {
		ids = append(ids, x.ID)
	}
	return ids
}

// TestSectionIDsFromRead takes every section id the document read shows
// and runs each section operation on it in direct mode: each id
// resolves (none is refused as not_found or malformed), and the op
// changes the section read lists under the same id.
func TestSectionIDsFromRead(t *testing.T) {
	// Two sections end at paragraphs sharing one stored id: read shows
	// the second as section:0000000A~2.
	dup := writeDocx(t, `<w:p w14:paraId="0000000A"><w:pPr><w:sectPr><w:pgMar w:top="1440" w:right="1440" w:bottom="1440" w:left="1440" w:header="720" w:footer="720" w:gutter="0"/></w:sectPr></w:pPr></w:p>`+
		`<w:p w14:paraId="0000000A"><w:pPr><w:sectPr/></w:pPr><w:r><w:t>b</w:t></w:r></w:p><w:p w14:paraId="0000000C"/>`)
	if got := readSectionIDs(t, dup); strings.Join(got, " ") != "section:0000000A section:0000000A~2 section:final" {
		t.Fatalf("read shows %v", got)
	}
	eachInput(t, append(sectionLawInputs(t), dup), func(t *testing.T, in string, _ int) {
		ids := readSectionIDs(t, in)
		if len(ids) == 0 {
			t.Errorf("%s: read shows no section", filepath.Base(in))
		}
		for i, id := range ids {
			for _, op := range []string{
				`{"op":"section.set_margins","at":"` + id + `","top":37}`,
				`{"op":"section.set_layout","at":"` + id + `","break_type":"oddPage"}`,
				`{"op":"section.set_first_page","at":"` + id + `","enabled":true}`,
			} {
				out := filepath.Join(t.TempDir(), "out.docx")
				err := runPlan(laws.Private(t, in), out, laws.Direct, "["+op+"]")
				var r refusal
				if errors.As(err, &r) && strings.Contains(r.d.Message, "pending property change") {
					continue // a section with a pending change refuses section.set_layout and set_first_page
				}
				if err != nil {
					t.Errorf("%s: %s: %v", filepath.Base(in), op, err)
					continue
				}
				if got := readSectionIDs(t, out); len(got) <= i || got[i] != id {
					t.Errorf("%s: %s: read shows sections %v after it", filepath.Base(in), op, got)
				}
				if in == dup && !changedSection(t, in, out, i) {
					t.Errorf("%s: %s changed another section", filepath.Base(in), op)
				}
			}
		}
	})
}

// TestSectionSetFirstPage: turning a section's distinct first page on
// writes w:titlePg w:val="1" and, tracked, records the change; turning
// it off writes w:val="0"; the mode it already has writes nothing.
func TestSectionSetFirstPage(t *testing.T) {
	path := writeDocx(t, sectionBody)
	if c := commit(t, path, opsText(t, path, "track", `[{"op":"section.set_first_page","at":"section:final","enabled":true}]`)); c.Outcome != Committed {
		t.Fatalf("%+v", c.Diagnostic)
	}
	if got := mainXML(t, path); !strings.Contains(got, `<w:sectPr><w:titlePg w:val="1"/><w:sectPrChange w:id="6" w:author="Ann" w:date="2026-01-01T00:00:00Z"><w:sectPr></w:sectPr></w:sectPrChange></w:sectPr></w:body>`) {
		t.Errorf("track: %s", got)
	}
	if r, _ := runText(path, opsText(t, path, "direct", `[{"op":"section.set_first_page","at":"section:0000000B","enabled":true}]`), Options{}); r.Outcome != Rejected || r.Diagnostic.Code != "not_found" {
		t.Errorf("a paragraph that ends no section: %s %+v", r.Outcome, r.Diagnostic)
	}
	if c := commit(t, path, opsText(t, path, "direct", `[{"op":"section.set_margins","at":"section:0000000a","top":12}]`)); c.Outcome != Committed || !strings.Contains(mainXML(t, path), `<w:pgMar w:top="240"`) {
		t.Errorf("by boundary paragraph: %s", mainXML(t, path))
	}
	path = writeDocx(t, sectionBody)
	if c := commit(t, path, opsText(t, path, "direct", `[{"op":"section.set_first_page","at":"section:final","enabled":false}]`)); c.Outcome != Committed || len(c.Parts) != 0 {
		t.Errorf("already off: %s %v", c.Outcome, c.Parts)
	}
}

// TestEvenOddHeaders: track mode refuses the setting (refused_in_mode);
// direct mode adds the settings part where the document has none, with
// its relationship and content type, and removing the setting removes
// the element.
func TestEvenOddHeaders(t *testing.T) {
	path := writeDocx(t, sectionBody)
	r, _ := runText(path, opsText(t, path, "track", `[{"op":"document.set_even_odd_headers","even_and_odd":true}]`), Options{})
	if r.Outcome != Rejected || r.Diagnostic.Code != "refused_in_mode" {
		t.Errorf("track: %+v", r.Diagnostic)
	}
	c := commit(t, path, opsText(t, path, "direct", `[{"op":"document.set_even_odd_headers","even_and_odd":true}]`))
	if c.Outcome != Committed {
		t.Fatalf("%+v", c.Diagnostic)
	}
	parts := zipEntries(t, mustRead(t, path))
	if got := string(parts["word/settings.xml"]); !strings.Contains(got, "<w:evenAndOddHeaders/>") {
		t.Errorf("settings: %s", got)
	}
	if !strings.Contains(string(parts["word/_rels/document.xml.rels"]), "relationships/settings") || !strings.Contains(string(parts["[Content_Types].xml"]), "/word/settings.xml") {
		t.Error("the settings part is not related and registered")
	}
	if c := commit(t, path, opsText(t, path, "direct", `[{"op":"document.set_even_odd_headers","even_and_odd":false}]`)); c.Outcome != Committed {
		t.Fatalf("%+v", c.Diagnostic)
	}
	if got := string(zipEntries(t, mustRead(t, path))["word/settings.xml"]); strings.Contains(got, "evenAndOdd") {
		t.Errorf("off: %s", got)
	}
}

// changedSection reports whether section i (0-based) is the only one
// whose w:sectPr differs between a and b.
func changedSection(t *testing.T, a, b string, i int) bool {
	t.Helper()
	sects := func(path string) []string {
		d, err := model.OpenDocument(mustRead(t, path))
		if err != nil {
			t.Fatal(err)
		}
		var out []string
		for _, s := range view.SectPrNodes(d.XML) {
			out = append(out, string(s.Bytes()))
		}
		return out
	}
	x, y := sects(a), sects(b)
	if len(x) != len(y) {
		return false
	}
	for k := range x {
		if (x[k] != y[k]) != (k == i) {
			return false
		}
	}
	return true
}

// TestConvertToFormatting: an insertion becomes underlined text, a
// deletion struck-through live text, a replaced word gets a plain space
// between its halves, a deletion inside a converted insertion goes, and
// a missing revision is not_found.
func TestConvertToFormatting(t *testing.T) {
	body := `<w:p w14:paraId="0000000A"><w:r><w:t xml:space="preserve">a </w:t></w:r>` +
		`<w:del w:id="1" w:author="Bob"><w:r><w:delText>old</w:delText></w:r></w:del>` +
		`<w:ins w:id="2" w:author="Bob"><w:r><w:t>new</w:t></w:r><w:del w:id="3" w:author="Bob"><w:r><w:delText>gone</w:delText></w:r></w:del></w:ins></w:p>`
	for _, mode := range []string{"track", "direct"} {
		path := writeDocx(t, body)
		if c := commit(t, path, opsText(t, path, mode, `[{"op":"revision.convert_to_formatting","all":true}]`)); c.Outcome != Committed {
			t.Fatalf("%+v", c.Diagnostic)
		}
		want := `<w:r><w:rPr><w:strike/></w:rPr><w:t>old</w:t></w:r><w:r><w:t xml:space="preserve"> </w:t></w:r><w:r><w:rPr><w:u w:val="single"/></w:rPr><w:t>new</w:t></w:r></w:p>`
		if got := mainXML(t, path); !strings.Contains(got, want) || strings.Contains(got, "gone") {
			t.Errorf("%s: want %s in %s", mode, want, got)
		}
	}
	path := writeDocx(t, body)
	r, _ := runText(path, opsText(t, path, "direct", `[{"op":"revision.convert_to_formatting","at":"revision:9"}]`), Options{})
	if r.Outcome != Rejected || r.Diagnostic.Code != "not_found" || r.Diagnostic.Path != "ops[0].at" {
		t.Errorf("missing revision: %+v", r.Diagnostic)
	}
}

// TestCommentThreadOps: a reply's markers and reference run go right
// after its parent's, threaded under the parent's paragraph id; resolving
// it writes w15:done; deleting the parent removes the thread and every
// anchor.
func TestCommentThreadOps(t *testing.T) {
	path := writeDocx(t, `<w:p w14:paraId="0000000A"><w:r><w:t>one two</w:t></w:r></w:p>`)
	steps := []string{
		`[{"op":"comment.add","at":"0000000A","select":"two","text":"Root"}]`,
		`[{"op":"comment.reply","at":"comment:0","text":"Reply"},{"op":"comment.set_status","at":"comment:0","status":"resolved"}]`,
	}
	for _, s := range steps {
		if c := commit(t, path, opsText(t, path, "direct", s)); c.Outcome != Committed {
			t.Fatalf("%s: %+v", s, c.Diagnostic)
		}
	}
	parts := zipEntries(t, mustRead(t, path))
	doc := string(parts["word/document.xml"])
	for _, want := range []string{`<w:commentRangeStart w:id="0"/><w:commentRangeStart w:id="1"/>`, `<w:commentRangeEnd w:id="0"/><w:commentRangeEnd w:id="1"/>`,
		`<w:commentReference w:id="0"/></w:r><w:r><w:rPr><w:rStyle w:val="CommentReference"/></w:rPr><w:commentReference w:id="1"/></w:r>`} {
		if !strings.Contains(doc, want) {
			t.Errorf("no %s in %s", want, doc)
		}
	}
	ex := string(parts["word/commentsExtended.xml"])
	if !regexp.MustCompile(`<w15:commentEx w15:paraId="([0-9A-F]{8})" w15:done="1"/><w15:commentEx w15:paraId="[0-9A-F]{8}" w15:paraIdParent="([0-9A-F]{8})" w15:done="0"/>`).MatchString(ex) {
		t.Errorf("commentsExtended: %s", ex)
	}
	if c := commit(t, path, opsText(t, path, "direct", `[{"op":"comment.delete","at":"comment:0"}]`)); c.Outcome != Committed {
		t.Fatalf("%+v", c.Diagnostic)
	}
	parts = zipEntries(t, mustRead(t, path))
	if doc := string(parts["word/document.xml"]); strings.Contains(doc, "comment") {
		t.Errorf("anchors left: %s", doc)
	}
	if c := string(parts["word/comments.xml"]); strings.Contains(c, "<w:comment ") {
		t.Errorf("records left: %s", c)
	}
}
