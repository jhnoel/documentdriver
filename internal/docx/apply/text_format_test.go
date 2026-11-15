package apply

import (
	"os"
	"strings"
	"testing"
)

// TestTextFormat: track records the run's prior properties in a
// w:rPrChange, direct writes the properties alone, formatting to what is
// there writes nothing, and a run's existing pending change keeps its
// record.
func TestTextFormat(t *testing.T) {
	const p = `<w:p w14:paraId="0000000A"><w:r><w:rPr><w:i/></w:rPr><w:t>one two three</w:t></w:r></w:p>`
	for _, c := range []struct {
		name, body, op, mode string
		want, not            []string
	}{
		{"track", p, `"select":"two","bold":true,"font_size":14.5,"color":"#C00000"`, "track",
			[]string{`<w:r><w:rPr><w:b w:val="1"/><w:i/><w:color w:val="C00000"/><w:sz w:val="29"/><w:rPrChange w:id="1" w:author="Ann" w:date="2026-01-01T00:00:00Z"><w:rPr><w:i/></w:rPr></w:rPrChange></w:rPr><w:t>two</w:t></w:r>`}, nil},
		{"direct clear", p, `"select":"two","clear":["italic"]`, "direct",
			[]string{`<w:r><w:rPr></w:rPr><w:t>two</w:t></w:r>`, `<w:i/></w:rPr><w:t xml:space="preserve">one </w:t>`}, []string{"rPrChange"}},
		{"pending change kept", `<w:p w14:paraId="0000000A"><w:r><w:rPr><w:i/><w:rPrChange w:id="5" w:author="Bob"><w:rPr/></w:rPrChange></w:rPr><w:t>two</w:t></w:r></w:p>`,
			`"select":"two","bold":true`, "track", []string{`<w:b w:val="1"/><w:i/><w:rPrChange w:id="5" w:author="Bob"><w:rPr/></w:rPrChange>`}, nil},
	} {
		t.Run(c.name, func(t *testing.T) {
			path := writeDocx(t, c.body)
			if r := commit(t, path, opsText(t, path, c.mode, `[{"op":"text.format","at":"0000000A",`+c.op+`}]`)); r.Outcome != Committed {
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
	path := writeDocx(t, p)
	before, _ := os.ReadFile(path) //nolint:errcheck,gosec // compared below
	if c := commit(t, path, opsText(t, path, "track", `[{"op":"text.format","at":"0000000A","select":"two","italic":true}]`)); c.Outcome != Committed || len(c.Parts) != 0 {
		t.Errorf("already formatted: %s %v", c.Outcome, c.Parts)
	}
	if after, _ := os.ReadFile(path); string(after) != string(before) { //nolint:errcheck,gosec // compared
		t.Error("formatting to the current value wrote the file")
	}
}
