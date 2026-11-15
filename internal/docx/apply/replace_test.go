package apply

import (
	"encoding/json"
	"strings"
	"testing"
)

// replaceBody holds a paragraph with a run-level symbol between text, a
// simple field and an equation, and a plain one.
const replaceBody = `<w:p w14:paraId="0000000A"><w:r><w:rPr><w:b/></w:rPr><w:t>a</w:t><w:sym w:font="Symbol" w:char="F020"/><w:t>b</w:t></w:r>` +
	`<w:fldSimple w:instr="AUTHOR"><w:r><w:t>Ada</w:t></w:r></w:fldSimple>` +
	`<m:oMath xmlns:m="http://schemas.openxmlformats.org/officeDocument/2006/math"><m:r><m:t>x</m:t></m:r></m:oMath></w:p>` +
	`<w:p w14:paraId="0000000B"><w:r><w:rPr><w:i/></w:rPr><w:t>plain</w:t></w:r></w:p>`

func replaceOp(at, html string, del ...string) string {
	s := `[{"op":"paragraph.replace","at":"` + at + `","html":` + jsonString(html)
	if len(del) > 0 {
		s += `,"delete":["` + strings.Join(del, `","`) + `"]`
	}
	return s + `}]`
}

func jsonString(s string) string {
	b, err := json.Marshal(s)
	if err != nil {
		panic(err)
	}
	return string(b)
}

// TestReplaceParagraph: kept placeholders and equations stay byte for
// byte where the html puts them, bare text takes the shared run
// formatting, delete deletes, and leaving content out is refused.
func TestReplaceParagraph(t *testing.T) {
	keep := `x<opaque data-ref="0000000A/opaque:1"></opaque>y<span data-field="AUTHOR" data-ref="0000000A/opaque:2">Ada</span><equation display="inline" data-ref="0000000A/equation:1">x</equation>z`
	for _, mode := range []string{"track", "direct"} {
		path := writeDocx(t, replaceBody)
		c := commit(t, path, opsText(t, path, mode, replaceOp("0000000A", keep)))
		if c.Outcome != Committed {
			t.Fatalf("%s: %+v", mode, c.Diagnostic)
		}
		got := mainXML(t, path)
		for _, want := range []string{`<w:sym w:font="Symbol" w:char="F020"/>`, `<w:fldSimple w:instr="AUTHOR"><w:r><w:t>Ada</w:t></w:r></w:fldSimple>`, `<m:t>x</m:t>`} {
			if strings.Count(got, want) != 1 {
				t.Errorf("%s: %s kept %d times: %s", mode, want, strings.Count(got, want), got)
			}
		}
		if mode == "direct" && !strings.Contains(got, `<w:r><w:t>x</w:t></w:r><w:r><w:rPr><w:b/></w:rPr><w:sym`) {
			t.Errorf("direct: the symbol is not in its own run: %s", got)
		}
		if mode == "track" && (!strings.Contains(got, `<w:del `) || !strings.Contains(got, `<w:ins `)) {
			t.Errorf("track: no revisions: %s", got)
		}
	}
	// Reordering is direct only.
	moved := `<equation display="inline" data-ref="0000000A/equation:1">x</equation>q<opaque data-ref="0000000A/opaque:1"></opaque>`
	path := writeDocx(t, replaceBody)
	c := commit(t, path, opsText(t, path, "direct", replaceOp("0000000A", moved, "0000000A/opaque:2")))
	if c.Outcome != Committed {
		t.Fatalf("reorder direct: %+v", c.Diagnostic)
	}
	got := mainXML(t, path)
	if strings.Contains(got, "AUTHOR") || strings.Index(got, "<m:oMath") > strings.Index(got, "<w:sym") {
		t.Errorf("reorder direct: %s", got)
	}
	for _, c := range []struct {
		name, mode, ops, code, path string
	}{
		{"left out", "direct", replaceOp("0000000A", "text"), "operation_failed", "ops[0].html"},
		{"reorder tracked", "track", replaceOp("0000000A", moved, "0000000A/opaque:2"), "refused_in_mode", "ops[0].html"},
		{"delete names nothing", "direct", replaceOp("0000000B", "t", "0000000B/opaque:1"), "not_found", "ops[0].delete[0]"},
		{"unknown ref", "direct", replaceOp("0000000B", `<opaque data-ref="0000000B/opaque:1"></opaque>`), "not_found", "ops[0].html"},
		{"edited field", "direct", replaceOp("0000000A", strings.Replace(keep, ">Ada<", ">Bob<", 1)), "invalid_operation", "ops[0].html"},
	} {
		path := writeDocx(t, replaceBody)
		r, err := runText(path, opsText(t, path, c.mode, c.ops), Options{View: viewOpt})
		if err != nil {
			t.Fatal(err)
		}
		if r.Outcome != Rejected || r.Diagnostic.Code != c.code || r.Diagnostic.Path != c.path {
			t.Errorf("%s: %s %+v, want %s at %s", c.name, r.Outcome, r.Diagnostic, c.code, c.path)
		}
	}
}
