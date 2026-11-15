package apply

import (
	"strings"
	"testing"
)

// TestTextRevise covers text.revise's scopes and refusals: every match
// in the document, a within span, and the field each refusal names.
func TestTextRevise(t *testing.T) {
	const body = `<w:p w14:paraId="0000000A"><w:r><w:t>one cat, one dog</w:t></w:r></w:p>` +
		`<w:p w14:paraId="0000000B"><w:r><w:t>one bird</w:t></w:r></w:p>`
	for _, c := range []struct {
		name, op string
		want     []string
		refused  string
	}{
		{"document all", `{"op":"text.revise","edits":[{"select":"one","with":"two","all":true}]}`,
			[]string{">two<", "> bird<"}, ""},
		{"within", `{"op":"text.revise","at":"0000000A","within":"one dog","edits":[{"select":"one","with":"a"}]}`,
			[]string{">one cat, <", ">a<"}, ""},
		{"two edits", `{"op":"text.revise","at":"0000000A","edits":[{"select":"cat","with":"cow"},{"select":"dog","with":"pig"}]}`,
			[]string{">cow<", ">pig<"}, ""},
		{"ambiguous edit", `{"op":"text.revise","edits":[{"select":"one","with":"two"}]}`, nil, "ambiguous ops[0].edits[0].select"},
		{"edit occurrence", `{"op":"text.revise","at":"0000000A","edits":[{"select":"one","with":"two","occurrence":3}]}`, nil, "not_found ops[0].edits[0].occurrence"},
		{"within not found", `{"op":"text.revise","within":"zebra","edits":[{"select":"one","with":"two"}]}`, nil, "not_found ops[0].within"},
		{"within ambiguous", `{"op":"text.revise","within":"one","edits":[{"select":"one","with":"two"}]}`, nil, "ambiguous ops[0].within"},
		{"overlap", `{"op":"text.revise","at":"0000000A","edits":[{"select":"one cat","with":"x"},{"select":"cat,","with":"y"}]}`, nil, "operation_failed ops[0].edits"},
	} {
		t.Run(c.name, func(t *testing.T) {
			path := writeDocx(t, body)
			text := opsText(t, path, "direct", "["+c.op+"]")
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
		})
	}
}
