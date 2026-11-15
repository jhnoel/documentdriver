package ops

import (
	"strings"
	"testing"
)

// TestReplaceEquationLaTeX: equation.replace's LaTeX is checked with
// math.CanonicalLaTeX (display math unless display is false) and held in
// canonical form in the canonical JSON, so equivalent spellings give the
// same plan identity.
func TestReplaceEquationLaTeX(t *testing.T) {
	const prefix = `{"author":"Reviewer","base":"` + zeroBase + `","change_mode":"track","ops":[{"at":"0000000A",`
	for _, c := range []struct {
		op, canonical string
	}{
		{`{"op":"equation.replace","at":"0000000A","latex":"x^2+\\dfrac12"}`,
			prefix + `"latex":"x^{2}+\\frac{1}{2}","op":"equation.replace"}]}`},
		{`{"op":"equation.replace","at":"0000000A/equation:1","latex":" \\left( \\alpha \\right) ","display":false}`,
			strings.Replace(prefix, `"0000000A"`, `"0000000A/equation:1"`, 1) + `"display":false,"latex":"\\left(\\alpha\\right)","op":"equation.replace"}]}`},
		{`{"op":"equation.replace","at":"0000000A","latex":"\\sum_{i=1}^n i","display":true}`,
			prefix + `"display":true,"latex":"\\sum_{i=1}^{n}{i}","op":"equation.replace"}]}`},
		// Lines are display math: accepted when display is true or unset
		// (the target's form decides when the plan is applied).
		{`{"op":"equation.replace","at":"0000000A","latex":"a\\\\b"}`,
			prefix + `"latex":"a \\\\ b","op":"equation.replace"}]}`},
		{`{"op":"equation.replace","at":"0000000A","latex":"\\text{if } x<1","display":true}`,
			prefix + `"display":true,"latex":"\\text{if }x<1","op":"equation.replace"}]}`},
	} {
		p, d := ParsePlanJSON([]byte(wrapOps("[" + c.op + "]")))
		if d != nil {
			t.Errorf("%s: %s %s: %s", c.op, d.Code, d.Path, d.Message)
			continue
		}
		got := p.CanonicalJSON()
		if got != c.canonical {
			t.Errorf("%s:\ngot  %s\nwant %s", c.op, got, c.canonical)
			continue
		}
		// Canonical JSON is a fixed point: parsing it gives it back.
		q, d := ParsePlanJSON([]byte(got))
		if d != nil || q.CanonicalJSON() != got || q.PreviewKey() != p.PreviewKey() {
			t.Errorf("%s: canonical JSON does not parse back to itself: %v", c.op, d)
		}
	}
	for _, c := range []struct{ op, msg string }{
		{`{"op":"equation.replace","at":"0000000A","latex":"\\frac{a}"}`, "invalid LaTeX at character 8: missing argument"},
		{`{"op":"equation.replace","at":"0000000A","latex":"a\\\\b","display":false}`,
			`invalid LaTeX at character 1: \\ starts a new line, which only display math ($$...$$) has`},
		{`{"op":"equation.replace","at":"0000000A","latex":"\\bogus"}`, `invalid LaTeX at character 0: unknown command \bogus; supported commands: `},
		{`{"op":"equation.replace","at":"0000000A/equation:0","latex":"x"}`, "equation must be positive (1-based)"},
	} {
		_, d := ParsePlanJSON([]byte(wrapOps("[" + c.op + "]")))
		if d == nil || d.Code != "invalid_operation" || !strings.HasPrefix(d.Message, c.msg) {
			t.Errorf("%s: got %+v, want invalid_operation %q", c.op, d, c.msg)
			continue
		}
		if want := "ops[0].latex"; !strings.HasPrefix(c.msg, "equation must") && d.Path != want {
			t.Errorf("%s: path %q, want %q", c.op, d.Path, want)
		}
	}
	// Equivalent spellings are one plan.
	a, _ := ParsePlanJSON([]byte(wrapOps(`[{"op":"equation.replace","at":"0000000A","latex":"x^2"}]`)))
	b, _ := ParsePlanJSON([]byte(wrapOps(`[{"op":"equation.replace","at":"0000000A","latex":"x^{2}"}]`)))
	if a == nil || b == nil || a.PreviewKey() != b.PreviewKey() {
		t.Error("x^2 and x^{2} give different plans")
	}
}
