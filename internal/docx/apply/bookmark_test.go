package apply

import (
	"strings"
	"testing"
)

// TestBookmarks covers marker placement (a whole paragraph after its
// properties; a word, its markers outside a wrapper whose edge is the
// word's), rename, move and delete, and the refusals.
func TestBookmarks(t *testing.T) {
	const body = `<w:p w14:paraId="0000000A"><w:pPr><w:jc w:val="left"/></w:pPr><w:r><w:t xml:space="preserve">see </w:t></w:r>` +
		`<w:ins w:id="3" w:author="Bob"><w:r><w:t>word</w:t></w:r></w:ins><w:r><w:t xml:space="preserve"> end</w:t></w:r></w:p>` +
		`<w:p w14:paraId="0000000B"><w:bookmarkStart w:id="4" w:name="Old"/><w:r><w:t>old</w:t></w:r><w:bookmarkEnd w:id="4"/></w:p>`
	for _, c := range []struct {
		name, op string
		want     []string
		not      []string
		refused  string
	}{
		{"whole paragraph", `{"op":"bookmark.add","at":"0000000A","name":"Whole"}`,
			[]string{`</w:pPr><w:bookmarkStart w:id="5" w:name="Whole"/><w:r>`, `<w:bookmarkEnd w:id="5"/></w:p>`}, nil, ""},
		{"a word in an insertion", `{"op":"bookmark.add","at":"0000000A","name":"W","select":"word"}`,
			[]string{`<w:bookmarkStart w:id="5" w:name="W"/><w:ins w:id="3" w:author="Bob"><w:r><w:t>word</w:t></w:r></w:ins><w:bookmarkEnd w:id="5"/>`}, nil, ""},
		{"part of a word", `{"op":"bookmark.add","at":"0000000A","name":"W","select":"or"}`,
			[]string{`<w:t>w</w:t></w:r><w:bookmarkStart w:id="5" w:name="W"/><w:r><w:t>or</w:t></w:r><w:bookmarkEnd w:id="5"/><w:r><w:t>d</w:t>`}, nil, ""},
		{"rename", `{"op":"bookmark.rename","at":"bookmark:old","name":"New"}`, []string{`<w:bookmarkStart w:id="4" w:name="New"/>`}, nil, ""},
		{"move", `{"op":"bookmark.move","at":"bookmark:Old","to":"0000000A","select":"see"}`,
			[]string{`<w:bookmarkStart w:id="4" w:name="Old"/><w:r><w:t xml:space="preserve">see</w:t></w:r><w:bookmarkEnd w:id="4"/>`, `<w:p w14:paraId="0000000B"><w:r>`}, nil, ""},
		{"delete", `{"op":"bookmark.delete","at":"bookmark:OLD"}`, nil, []string{"bookmarkStart", "bookmarkEnd"}, ""},
		{"name in use", `{"op":"bookmark.add","at":"0000000A","name":"old"}`, nil, nil, "operation_failed ops[0].name"},
		{"no such bookmark", `{"op":"bookmark.delete","at":"bookmark:Olt"}`, nil, nil, "not_found ops[0].at"},
		{"move to no paragraph", `{"op":"bookmark.move","at":"bookmark:Old","to":"0000000C"}`, nil, nil, "not_found ops[0].to"},
	} {
		t.Run(c.name, func(t *testing.T) {
			path := writeDocx(t, body)
			text := opsText(t, path, "track", "["+c.op+"]")
			if c.refused != "" {
				r, err := runText(path, text, Options{})
				if err != nil {
					t.Fatal(err)
				}
				if r.Outcome != Rejected || r.Diagnostic.Code+" "+r.Diagnostic.Path != c.refused {
					t.Fatalf("%s %+v, want %s", r.Outcome, r.Diagnostic, c.refused)
				}
				return
			}
			if r := commit(t, path, text); r.Outcome != Committed {
				t.Fatalf("%s %+v", r.Outcome, r.Diagnostic)
			}
			got := mainXML(t, path)
			for _, w := range c.want {
				if !strings.Contains(got, w) {
					t.Errorf("no %s in\n%s", w, got)
				}
			}
			for _, w := range c.not {
				if strings.Contains(got, w) {
					t.Errorf("%s in\n%s", w, got)
				}
			}
		})
	}
	// A header's bookmark is edited in its own part.
	in := writeFixture(t, "sections.docx")
	out := t.TempDir() + "/out.docx"
	if err := runPlan(in, out, "direct", `[{"op":"bookmark.add","at":"40000001","name":"Head","select":"header"}]`); err != nil {
		t.Fatal(err)
	}
	if hdr := partText(t, out, "word/header1.xml"); !strings.Contains(hdr, `w:name="Head"`) {
		t.Errorf("no header bookmark in %s", hdr)
	}
	if err := runPlan(out, out+"2", "direct", `[{"op":"bookmark.rename","at":"bookmark:Head","name":"Top"},{"op":"bookmark.delete","at":"bookmark:Top"}]`); err != nil {
		t.Fatal(err)
	}
	if hdr := partText(t, out+"2", "word/header1.xml"); strings.Contains(hdr, "bookmark") {
		t.Errorf("header bookmark left in %s", hdr)
	}
}
