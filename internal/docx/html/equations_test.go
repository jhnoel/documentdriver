package html

import (
	"errors"
	"strings"
	"testing"

	"github.com/jhnoel/documentdriver/internal/docx/math"
	"github.com/jhnoel/documentdriver/internal/ooxml/tree"
)

// ommlStub converts LaTeX to the OMML shape docxdriver's converter writes:
// a namespaced m:oMath, wrapped in m:oMathPara for a block equation.
func ommlStub(latex string, block bool) (string, error) {
	if latex == "bad" {
		return "", errors.New("unknown command")
	}
	math := `<m:oMath><m:r><m:t>` + EscapeText(latex) + `</m:t></m:r></m:oMath>`
	if !block {
		return `<m:oMath xmlns:m="` + OMMLNamespace + `">` + strings.TrimPrefix(math, "<m:oMath>"), nil
	}
	return `<m:oMathPara xmlns:m="` + OMMLNamespace + `">` + math + `</m:oMathPara>`, nil
}

func TestConsecutiveBlockEquationsShareOneOMathPara(t *testing.T) {
	items := mustFragment(t, "<p>\n<equation display=\"block\">a</equation> \n <equation display=\"block\">b</equation><equation display=\"block\">c</equation> x <equation display=\"block\">d</equation></p>", ommlStub)
	var kinds []string
	for _, it := range items {
		switch {
		case it.Kind == ItemAtom && it.Atom.Kind == AtomEquation:
			kinds = append(kinds, "eq:"+strings.ReplaceAll(it.Atom.Text, "\n", "|"))
		case it.Kind == ItemAtom:
			for _, ch := range it.Atom.Text {
				kinds = append(kinds, string(ch))
			}
		}
	}
	if got := strings.Join(kinds, ","); got != ` ,eq:a \\ b \\ c, ,x, ,eq:d` {
		t.Fatalf("items %q", got)
	}
	eqs := equations(items)
	want := `<m:oMathPara xmlns:m="` + OMMLNamespace + `"><m:oMath><m:r><m:t>a</m:t></m:r></m:oMath><m:oMath><m:r><m:t>b</m:t></m:r></m:oMath><m:oMath><m:r><m:t>c</m:t></m:r></m:oMath></m:oMathPara>`
	if eqs[0].OMML != want || !eqs[0].Display {
		t.Fatalf("merged OMML %s", eqs[0].OMML)
	}
	if single, _ := ommlStub("d", true); eqs[1].OMML != single {
		t.Fatalf("lone block equation changed: %s", eqs[1].OMML)
	}
}

func TestBlockEquationsMergeOnlyLikeWrapped(t *testing.T) {
	items := mustFragment(t, `<p><equation display="block">a</equation><ins><equation display="block">b</equation></ins><equation display="inline">c</equation><equation display="block">d</equation></p>`, ommlStub)
	if n := len(equations(items)); n != 4 {
		t.Fatalf("%d equations, want 4 (no merge across revisions or inline equations)", n)
	}
}

func TestBlockEquationMergeChecksOMML(t *testing.T) {
	bad := func(string, bool) (string, error) { return "<x/>", nil }
	_, err := ParseFragment(`<p><equation display="block">a</equation><equation display="block">b</equation></p>`, bad)
	if !errors.Is(err, errBlockOMML) {
		t.Fatalf("got %v", err)
	}
}

// TestRealConverterBlockContract: math.LaTeXToOMML, the converter the docx
// CLI passes, meets the block-output contract (one m:oMathPara of m:oMath
// elements, prefix m), so consecutive block equations merge, and the
// merged LaTeX converts to the same equation as the merged OMML.
func TestRealConverterBlockContract(t *testing.T) {
	var conv LatexToOMML = math.LaTeXToOMML
	items := mustFragment(t, `<p><equation display="block">\frac{a}{b}</equation> <equation display="block">x \\ y</equation>`+
		`<equation display="inline">z</equation> <equation display="block">\sqrt{2}</equation></p>`, conv)
	eqs := equations(items)
	if len(eqs) != 3 {
		t.Fatalf("%d equations, want 3", len(eqs))
	}
	merged := eqs[0]
	if !merged.Display || merged.Text != `\frac{a}{b} \\ x \\ y` {
		t.Fatalf("merged equation %+v", merged)
	}
	doc, err := tree.Parse([]byte(merged.OMML))
	if err != nil {
		t.Fatal(err)
	}
	root := doc.Root()
	if !root.Is(OMMLNamespace, "oMathPara") || root.Prefix() != "m" || len(root.Elements()) != 3 {
		t.Fatalf("merged OMML %s", merged.OMML)
	}
	latex, display, losses := math.OMMLToLaTeX(root)
	want, err := math.CanonicalLaTeX(merged.Text, true)
	if err != nil {
		t.Fatal(err)
	}
	if latex != want || !display || len(losses) != 0 {
		t.Errorf("merged OMML reads back as %q (display %v, losses %v), want %q", latex, display, losses, want)
	}
	if eqs[1].Display || eqs[2].OMML == "" {
		t.Errorf("inline or lone block equation changed: %+v", eqs[1:])
	}
	// The read side's equation attributes are accepted on input.
	mustFragment(t, `<p><equation display="inline" data-ref="0000000A/equation:1" data-loss="w:ins">x</equation></p>`, conv)
	if _, err := ParseFragment(`<p><equation display="inline">\bogus</equation></p>`, conv); err == nil ||
		!strings.HasPrefix(err.Error(), "invalid LaTeX equation: ") {
		t.Errorf("unknown command: %v", err)
	}
	if _, err := ParseFragment(`<p><equation display="inline">a \\ b</equation></p>`, conv); err == nil {
		t.Error("a line break in inline LaTeX is accepted")
	}
}
