package apply

import (
	"strings"
	"testing"
)

// TestTextReplace covers text.replace's shapes in both modes: word-level
// revisions, the replaced text's run properties kept (W3), tabs and
// breaks as characters and objects kept (W6, B8), the author's own
// pending insertion edited in place, another author's insertion split
// with fresh ids, all matches, and the refusals.
func TestTextReplace(t *testing.T) {
	const p = `<w:p w14:paraId="0000000A"><w:r><w:rPr><w:b/></w:rPr><w:t xml:space="preserve">Payment is due </w:t></w:r>` +
		`<w:r><w:rPr><w:i/></w:rPr><w:t>within thirty days</w:t></w:r><w:r><w:tab/><w:t>end</w:t></w:r></w:p>`
	for _, c := range []struct {
		name, body, op, mode string
		want, not            []string
		refused              string
	}{
		{"one word tracked", p, `"select":"thirty","with":"sixty"`, "track",
			[]string{`<w:del w:id="1" w:author="Ann" w:date="2026-01-01T00:00:00Z"><w:r><w:rPr><w:i/></w:rPr><w:delText>thirty</w:delText></w:r></w:del><w:ins w:id="2" w:author="Ann" w:date="2026-01-01T00:00:00Z"><w:r><w:rPr><w:i/></w:rPr><w:t>sixty</w:t></w:r></w:ins>`},
			nil, ""},
		{"one word direct keeps the run properties", p, `"select":"thirty","with":"sixty"`, "direct",
			[]string{`<w:r><w:rPr><w:i/></w:rPr><w:t>sixty</w:t></w:r>`}, []string{"w:ins", "w:del"}, ""},
		{"same text writes nothing", p, `"select":"thirty days","with":"thirty days"`, "track", nil, []string{"w:ins"}, ""},
		{"a tab is a character", p, `"select":"days\tend","with":"days\tfin"`, "direct",
			[]string{`<w:tab/>`, `<w:t>fin</w:t>`}, []string{">end<"}, ""},
		{"a tab can be replaced", p, `"select":"days\tend","with":"days end"`, "direct",
			[]string{`<w:t xml:space="preserve"> </w:t>`}, []string{"<w:tab/>"}, ""},
		{"a break in with", p, `"select":"days","with":"days\nmore"`, "direct", []string{`<w:br/>`}, nil, ""},
		{"all", `<w:p w14:paraId="0000000A"><w:r><w:t>a b a b a</w:t></w:r></w:p>`, `"select":"a","with":"x","all":true`, "direct",
			[]string{`<w:t>x</w:t></w:r><w:r><w:t xml:space="preserve"> b </w:t></w:r><w:r><w:t>x</w:t></w:r><w:r><w:t xml:space="preserve"> b </w:t></w:r><w:r><w:t>x</w:t>`}, []string{">a<"}, ""},
		{"own insertion edited in place", `<w:p w14:paraId="0000000A"><w:r><w:t xml:space="preserve">x </w:t></w:r><w:ins w:id="7" w:author="Ann"><w:r><w:t>my words</w:t></w:r></w:ins></w:p>`,
			`"select":"my","with":"our"`, "track", []string{`<w:ins w:id="7" w:author="Ann"><w:r><w:t>our</w:t></w:r><w:r><w:t xml:space="preserve"> words</w:t></w:r></w:ins>`}, []string{"w:del"}, ""},
		{"foreign insertion split with a fresh id", `<w:p w14:paraId="0000000A"><w:ins w:id="7" w:author="Bob"><w:r><w:t>his own words</w:t></w:r></w:ins></w:p>`,
			`"select":"own","with":"new"`, "track", []string{`<w:ins w:id="7" w:author="Bob"><w:r><w:t xml:space="preserve">his </w:t></w:r><w:del w:id="9" w:author="Ann" w:date="2026-01-01T00:00:00Z"><w:r><w:delText>own</w:delText></w:r></w:del></w:ins><w:ins w:id="10" w:author="Ann" w:date="2026-01-01T00:00:00Z"><w:r><w:t>new</w:t></w:r></w:ins><w:ins w:id="8" w:author="Bob"><w:r><w:t xml:space="preserve"> words</w:t></w:r></w:ins>`}, nil, ""},
		{"an equation is kept", `<w:p w14:paraId="0000000A"><w:r><w:t xml:space="preserve">see </w:t></w:r><m:oMath><m:r><m:t>x</m:t></m:r></m:oMath><w:r><w:t xml:space="preserve"> here</w:t></w:r></w:p>`,
			`"select":"see ￼ here","with":"look ￼ there"`, "direct", []string{"<m:oMath>", ">look<", ">there<"}, nil, ""},
		{"an equation is not dropped", `<w:p w14:paraId="0000000A"><w:r><w:t xml:space="preserve">see </w:t></w:r><m:oMath><m:r><m:t>x</m:t></m:r></m:oMath></w:p>`,
			`"select":"see ￼","with":"see"`, "direct", nil, nil, "operation_failed ops[0].with"},
		{"an object is not made up", p, `"select":"thirty","with":"￼"`, "direct", nil, nil, "operation_failed ops[0].with"},
		{"a control character", p, `"select":"thirty","with":"a\u0001"`, "direct", nil, nil, "invalid_operation ops[0].with"},
		{"direct guard", `<w:p w14:paraId="0000000A"><w:ins w:id="7" w:author="Bob"><w:r><w:t>his words</w:t></w:r></w:ins></w:p>`,
			`"select":"his","with":"her"`, "direct", nil, nil, "operation_failed ops[0].select"},
	} {
		t.Run(c.name, func(t *testing.T) {
			body := strings.Replace(c.body, "<w:p ", `<w:p xmlns:m="http://schemas.openxmlformats.org/officeDocument/2006/math" `, 1)
			path := writeDocx(t, body)
			text := opsText(t, path, c.mode, `[{"op":"text.replace","at":"0000000A",`+c.op+`}]`)
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
}
