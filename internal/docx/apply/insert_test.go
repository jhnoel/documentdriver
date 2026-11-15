package apply

import (
	"os"
	"regexp"
	"strings"
	"testing"

	"github.com/jhnoel/documentdriver/internal/docx/model"
	"github.com/jhnoel/documentdriver/internal/laws"
)

// insertBody is a heading-free body with an anchor whose properties a new
// paragraph inherits, and a table.
const insertBody = `<w:p w14:paraId="0000000A"><w:pPr><w:jc w:val="center"/><w:rPr><w:b/></w:rPr></w:pPr><w:r><w:t>a</w:t></w:r></w:p>` +
	`<w:tbl><w:tblPr/><w:tblGrid><w:gridCol w:w="100"/></w:tblGrid><w:tr><w:tc><w:p w14:paraId="0000000B"><w:r><w:t>c</w:t></w:r></w:p></w:tc></w:tr></w:tbl>` +
	`<w:p w14:paraId="0000000C"/>`

// paraIDs lists the w14:paraId values of a main document part in order.
var paraIDRE = regexp.MustCompile(`<w:p w14:paraId="([0-9A-F]{8})"`)

func paraIDs(xml string) []string {
	var out []string
	for _, m := range paraIDRE.FindAllStringSubmatch(xml, -1) {
		out = append(out, m[1])
	}
	return out
}

// TestInsertParagraph: track records the content in w:ins and the new
// mark as inserted, direct writes them plain; the new paragraph inherits
// the anchor's base properties (not its mark's run properties), takes a
// fresh id, and aliases address it and a list's members.
func TestInsertParagraph(t *testing.T) {
	op := `[{"op":"paragraph.insert","at":"0000000A","position":"after","html":"x <b>bold</b><br/>y","as":"n"},` +
		`{"op":"paragraph.insert","at":"$n","position":"after","html":["one","two <equation display=\"inline\">x^2</equation>","<a href=\"https://e.com\">link</a>"],"as":"g"},` +
		`{"op":"paragraph.format","at":"$g_2","alignment":"right"}]`
	for _, mode := range []string{"track", "direct"} {
		path := writeDocx(t, insertBody)
		c := commit(t, path, opsText(t, path, mode, op))
		if c.Outcome != Committed {
			t.Fatalf("%s: %+v", mode, c.Diagnostic)
		}
		got := mainXML(t, path)
		ids := paraIDs(got)
		if len(ids) != 7 || ids[0] != "0000000A" || ids[5] != "0000000B" {
			t.Fatalf("%s: paragraphs %v", mode, ids)
		}
		if c.Aliases["n"] != ids[1] || c.Aliases["g_1"] != ids[2] || c.Aliases["g_3"] != ids[4] {
			t.Errorf("%s: aliases %v, ids %v", mode, c.Aliases, ids)
		}
		for _, want := range []string{
			`<w:r><w:rPr><w:b/></w:rPr><w:t>bold</w:t><w:br/></w:r>`,
			`<m:oMath xmlns:m=`,
			`<w:hyperlink r:id="rId1">`,
			`<w:jc w:val="right"/>`,
		} {
			if !strings.Contains(got, want) {
				t.Errorf("%s: no %s in %s", mode, want, got)
			}
		}
		if strings.Count(got, `<w:jc w:val="center"/>`) < 4 {
			t.Errorf("%s: new paragraphs do not inherit the anchor's alignment: %s", mode, got)
		}
		tracked := strings.Contains(got, `<w:ins w:id=`)
		if tracked != (mode == "track") {
			t.Errorf("%s: tracked %v: %s", mode, tracked, got)
		}
		if mode == "track" && !strings.Contains(got, `<w:hyperlink r:id="rId1"><w:ins `) {
			t.Errorf("track: the link's runs are not inside its own w:ins: %s", got)
		}
	}
}

// TestInsertParagraphPlaces: before a table, after a table, in a cell,
// and two inserts after one anchor keep the plan's order.
func TestInsertParagraphPlaces(t *testing.T) {
	for _, c := range []struct {
		name, ops string
		texts     []string
	}{
		{"before table", `[{"op":"paragraph.insert","at":"table:1","position":"before","html":"T"}]`, []string{"a", "T", "c", ""}},
		{"after table", `[{"op":"paragraph.insert","at":"table:1","position":"after","html":"T"}]`, []string{"a", "c", "T", ""}},
		{"in a cell", `[{"op":"paragraph.insert","at":"0000000B","position":"after","html":"T"}]`, []string{"a", "c", "T", ""}},
		{"order", `[{"op":"paragraph.insert","at":"0000000A","position":"after","html":"1"},{"op":"paragraph.insert","at":"0000000A","position":"after","html":"2"}]`, []string{"a", "1", "2", "c", ""}},
		{"group anchor", `[{"op":"paragraph.insert","at":"0000000C","position":"before","html":["1","2"],"as":"g"},{"op":"paragraph.insert","at":"$g","position":"after","html":"3"},{"op":"paragraph.insert","at":"$g","position":"before","html":"0"}]`, []string{"a", "c", "0", "1", "2", "3", ""}},
	} {
		path := writeDocx(t, insertBody)
		r := commit(t, path, opsText(t, path, "direct", c.ops))
		if r.Outcome != Committed {
			t.Fatalf("%s: %+v", c.name, r.Diagnostic)
		}
		b, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		d, err := model.OpenDocument(b)
		if err != nil {
			t.Fatal(err)
		}
		var texts []string
		for _, p := range d.XML.AllParagraphs() {
			texts = append(texts, model.ParagraphText(p, model.Current))
		}
		if strings.Join(texts, "|") != strings.Join(c.texts, "|") {
			t.Errorf("%s: %q, want %q", c.name, texts, c.texts)
		}
	}
}

// TestInsertParagraphRefusals: each refusal names its code and field and
// leaves the file as it was.
func TestInsertParagraphRefusals(t *testing.T) {
	for _, c := range []struct {
		ops, code, path string
	}{
		{`[{"op":"paragraph.insert","at":"0000000F","position":"after","html":"x"}]`, "not_found", "ops[0].at"},
		{`[{"op":"paragraph.insert","at":"$nope","position":"after","html":"x"}]`, "not_found", "ops[0].at"},
		{`[{"op":"paragraph.insert","at":"table:9","position":"after","html":"x"}]`, "not_found", "ops[0].at"},
		{`[{"op":"paragraph.insert","at":"0000000A","position":"after","html":"x","style":"Nope"}]`, "not_found", "ops[0].style"},
		{`[{"op":"paragraph.insert","at":"0000000A","position":"after","html":"<table><tr><td>x</td></tr></table>"}]`, "invalid_operation", "ops[0].html"},
		{`[{"op":"paragraph.insert","at":"0000000A","position":"after","html":["a","<ins>b</ins>"]}]`, "invalid_operation", "ops[0].html[1]"},
		{`[{"op":"paragraph.insert","at":"0000000A","position":"after","html":"a <a href=\"https://e.com\">b</a>"}]`, "invalid_operation", "ops[0].html"},
		{`[{"op":"paragraph.insert","at":"0000000A","position":"after","html":"<p>a</p><p>b</p>"}]`, "invalid_operation", "ops[0].html"},
		{`[{"op":"paragraph.insert","at":"0000000A","position":"after","html":"a","as":"x"},{"op":"paragraph.insert","at":"0000000A","position":"after","html":"b","as":"x"}]`, "invalid_operation", "ops[1].as"},
		{`[{"op":"paragraph.insert","at":"0000000A","position":"after","html":"a","as":"g_1"},{"op":"paragraph.insert","at":"0000000A","position":"after","html":["b","c"],"as":"g"}]`, "invalid_operation", "ops[1].as"},
		{`[{"op":"paragraph.insert","at":"0000000A","position":"after","html":["b","c"],"as":"g"},{"op":"paragraph.format","at":"$g","alignment":"right"}]`, "invalid_operation", "ops[1].at"},
	} {
		for _, mode := range []string{"track", "direct"} {
			path := writeDocx(t, insertBody)
			before, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			r, err := runText(path, opsText(t, path, mode, c.ops), Options{})
			if err != nil {
				t.Fatal(err)
			}
			if r.Outcome != Rejected || r.Diagnostic.Code != c.code || r.Diagnostic.Path != c.path {
				t.Errorf("%s %s: %s %+v, want %s at %s", c.ops, mode, r.Outcome, r.Diagnostic, c.code, c.path)
			}
			if after, _ := os.ReadFile(path); string(after) != string(before) { //nolint:errcheck // compared
				t.Errorf("%s %s: a refused plan wrote the file", c.ops, mode)
			}
		}
	}
}

// insertSuite is paragraph.insert's change-mode law (AcceptAuthored):
// after and before the first body paragraph, formatted content, a list
// with an equation.
func insertSuite(t *testing.T, in string) *laws.ModeSuite {
	b, err := os.ReadFile(in) //nolint:gosec // G304: a law input
	if err != nil {
		t.Fatal(err)
	}
	d, err := model.OpenDocument(b)
	if err != nil || d.XML.Body() == nil {
		return nil
	}
	ps := model.ParagraphsIn(d.XML.Body())
	if len(ps) == 0 {
		return nil
	}
	id := d.ParaIDs().IDs[ps[0].Node].ID
	op := `[{"op":"paragraph.insert","at":"` + id + `","position":"after","html":"New <b>bold</b> <i>text</i><br/>line","as":"n"},` +
		`{"op":"paragraph.insert","at":"` + id + `","position":"before","html":["first","with <equation display=\"inline\">a+b</equation>"]},` +
		`{"op":"paragraph.insert","at":"$n","position":"after","html":""}]`
	s := settleSuite("all", "accept")
	s.Name = "paragraph.insert"
	s.Class = laws.AcceptAuthored
	s.Apply = func(src, dst, mode string) error { return runPlan(src, dst, mode, op) }
	return s
}

// TestInsertParagraphLaws runs the change-mode law on every law input.
func TestInsertParagraphLaws(t *testing.T) {
	eachInput(t, lawInputs(t), func(t *testing.T, in string, _ int) {
		if s := insertSuite(t, in); s != nil {
			s.Check(t, in)
		}
	})
}

// TestInsertParagraphGate: a new paragraph that inherits its anchor's
// invalid property copies a fault the input has; the located gate counts
// a copy of an input element's fault as pre-existing, so the plan saves,
// and so does an edit beside the fault.
func TestInsertParagraphGate(t *testing.T) {
	body := `<w:p w14:paraId="0000000A"><w:pPr><w:jc w:val="bogus"/></w:pPr><w:r><w:t>a</w:t></w:r></w:p>` +
		`<w:p w14:paraId="0000000B"><w:r><w:t>b</w:t></w:r></w:p>`
	for _, mode := range []string{"track", "direct"} {
		for _, at := range []string{"0000000A", "0000000B"} {
			path := writeDocx(t, body)
			ops := `[{"op":"paragraph.format","at":"0000000B","alignment":"right"},{"op":"paragraph.insert","at":"` + at + `","position":"after","html":"x"}]`
			if c := commit(t, path, opsText(t, path, mode, ops)); c.Outcome != Committed {
				t.Errorf("%s after %s: %+v", mode, at, c.Diagnostic)
			}
			if at == "0000000A" && strings.Count(mainXML(t, path), `w:val="bogus"`) != 2 {
				t.Errorf("%s: the anchor's properties were not inherited", mode)
			}
		}
	}
}
