package apply

import (
	"strings"
	"testing"
)

// TestTextInsert covers text.insert's insertion points: before and after
// a match, the start and end of a paragraph, the run properties the new
// text takes, and the author's own pending insertion extended in place.
func TestTextInsert(t *testing.T) {
	const p = `<w:p w14:paraId="0000000A"><w:r><w:rPr><w:b/></w:rPr><w:t>bold</w:t></w:r><w:r><w:t xml:space="preserve"> plain</w:t></w:r></w:p>`
	for _, c := range []struct {
		name, body, op, mode string
		want                 []string
	}{
		{"after a bold word", p, `"select":"bold","position":"after","with":"er"`, "direct", []string{`<w:r><w:rPr><w:b/></w:rPr><w:t>er</w:t></w:r>`}},
		{"before a plain word", p, `"select":"plain","position":"before","with":"very"`, "direct", []string{`<w:t>very</w:t>`}},
		{"start", p, `"position":"before","with":"A "`, "track", []string{`<w:ins w:id="1" w:author="Ann" w:date="2026-01-01T00:00:00Z"><w:r><w:rPr><w:b/></w:rPr><w:t xml:space="preserve">A </w:t></w:r></w:ins><w:r><w:rPr><w:b/></w:rPr><w:t>bold</w:t>`}},
		{"end", p, `"position":"after","with":" end"`, "track", []string{`<w:t xml:space="preserve"> plain</w:t></w:r><w:ins w:id="1" w:author="Ann" w:date="2026-01-01T00:00:00Z"><w:r><w:t xml:space="preserve"> end</w:t></w:r></w:ins>`}},
		{"own insertion extended", `<w:p w14:paraId="0000000A"><w:ins w:id="3" w:author="Ann"><w:r><w:t>mine</w:t></w:r></w:ins></w:p>`,
			`"position":"after","with":"!"`, "track", []string{`<w:ins w:id="3" w:author="Ann"><w:r><w:t>mine</w:t></w:r><w:r><w:t>!</w:t></w:r></w:ins>`}},
		{"empty paragraph", `<w:p w14:paraId="0000000A"/>`, `"position":"after","with":"x"`, "direct", []string{`<w:p w14:paraId="0000000A"><w:r><w:t>x</w:t></w:r></w:p>`}},
	} {
		t.Run(c.name, func(t *testing.T) {
			path := writeDocx(t, c.body)
			if r := commit(t, path, opsText(t, path, c.mode, `[{"op":"text.insert","at":"0000000A",`+c.op+`}]`)); r.Outcome != Committed {
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
