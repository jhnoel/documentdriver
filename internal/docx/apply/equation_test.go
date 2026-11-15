package apply

import (
	"strings"
	"testing"

	"github.com/jhnoel/documentdriver/internal/laws"
)

// eqBody has a paragraph with a formatted inline equation and one with
// two equations.
const eqBody = `<w:p w14:paraId="0000000A"><w:r><w:t>a</w:t></w:r>` +
	`<m:oMath xmlns:m="http://schemas.openxmlformats.org/officeDocument/2006/math"><m:r><w:rPr><w:sz w:val="30"/></w:rPr><m:t>x</m:t></m:r></m:oMath></w:p>` +
	`<w:p w14:paraId="0000000B"><m:oMath xmlns:m="http://schemas.openxmlformats.org/officeDocument/2006/math"><m:r><m:t>y</m:t></m:r></m:oMath>` +
	`<m:oMath xmlns:m="http://schemas.openxmlformats.org/officeDocument/2006/math"><m:r><m:t>z</m:t></m:r></m:oMath></w:p>`

// TestEquationOps: replace keeps the run formatting of the equation it
// replaces; delete removes it; both are tracked in track mode; an
// ambiguous or missing address is refused.
func TestEquationOps(t *testing.T) {
	for _, mode := range []string{"track", "direct"} {
		path := writeDocx(t, eqBody)
		c := commit(t, path, opsText(t, path, mode, `[{"op":"equation.replace","at":"0000000A","latex":"q^{2}"},{"op":"equation.delete","at":"0000000B/equation:2"}]`))
		if c.Outcome != Committed {
			t.Fatalf("%s: %+v", mode, c.Diagnostic)
		}
		got := mainXML(t, path)
		if !strings.Contains(got, `<w:sz w:val="30"/></w:rPr><m:t>q</m:t>`) {
			t.Errorf("%s: the new equation lost the template: %s", mode, got)
		}
		if gone := !strings.Contains(got, `<m:t>z</m:t>`) && !strings.Contains(got, `<m:t>x</m:t>`); gone != (mode == "direct") {
			t.Errorf("%s: old equations: %s", mode, got)
		}
	}
	for _, c := range []struct{ ops, code, path string }{
		{`[{"op":"equation.delete","at":"0000000B"}]`, "ambiguous", "ops[0].at"},
		{`[{"op":"equation.delete","at":"0000000B/equation:3"}]`, "not_found", "ops[0].at"},
		{`[{"op":"equation.replace","at":"0000000A","latex":"a\\\\b","display":false}]`, "invalid_operation", "ops[0].latex"},
	} {
		path := writeDocx(t, eqBody)
		r, err := runText(path, opsText(t, path, "direct", c.ops), Options{View: viewOpt})
		if err != nil {
			t.Fatal(err)
		}
		if r.Outcome != Rejected || r.Diagnostic.Code != c.code || r.Diagnostic.Path != c.path {
			t.Errorf("%s: %s %+v", c.ops, r.Outcome, r.Diagnostic)
		}
	}
}

// equationSuite is an equation op's change-mode law.
func equationSuite(name, ops string) *laws.ModeSuite {
	s := replaceSuite(name, ops)
	s.Name = name
	return s
}

// TestEquationLaws runs the change-mode law of equation.replace and
// equation.delete on every equation of the law inputs and write
// fixtures.
func TestEquationLaws(t *testing.T) {
	eachInput(t, deleteInputs(t), func(t *testing.T, in string, _ int) {
		ids := bodyParagraphIDs(t, in)
		markup := paragraphMarkup(t, in, ids)
		for _, id := range ids {
			for _, m := range contentRef.FindAllStringSubmatch(markup[id], -1) {
				if !strings.Contains(m[1], "/equation:") {
					continue
				}
				equationSuite("equation.replace "+m[1], `[{"op":"equation.replace","at":"`+m[1]+`","latex":"\\frac{a}{b}"}]`).Check(t, in)
				equationSuite("equation.delete "+m[1], `[{"op":"equation.delete","at":"`+m[1]+`"}]`).Check(t, in)
			}
		}
	})
}
