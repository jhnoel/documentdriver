package math

import (
	"errors"
	"strings"
	"testing"

	"github.com/jhnoel/documentdriver/internal/ooxml/tree"
)

func TestRunProperties(t *testing.T) {
	const cambria = `<w:rPr><w:rFonts w:ascii="Cambria Math" w:hAnsi="Cambria Math"/><w:sz w:val="28"/></w:rPr>`
	src := `<m:oMath xmlns:m="` + OMMLNS + `" xmlns:w="` + wordNS + `">` +
		`<m:r><w:rPr><w:color w:val="FF0000"/></w:rPr><m:t>a</m:t></m:r>` +
		`<m:f><m:num><m:r>` + cambria + `<m:t>b</m:t></m:r></m:num><m:den><m:r><m:rPr><m:sty m:val="p"/></m:rPr>` + cambria + `<m:t>c</m:t></m:r></m:den></m:f></m:oMath>`
	doc, err := tree.Parse([]byte(src))
	if err != nil {
		t.Fatal(err)
	}
	rpr := RunProperties(doc.Root())
	if rpr != cambria {
		t.Fatalf("RunProperties = %s", rpr)
	}
	if RunProperties(mustRoot(t, `<m:oMath xmlns:m="`+OMMLNS+`"><m:r><m:t>x</m:t></m:r></m:oMath>`)) != "" {
		t.Error("an equation without run formatting gives some")
	}
	for _, c := range []struct {
		latex   string
		display bool
	}{{`\mathrm{d}x + \frac{a}{b}`, false}, {`\mathrm{d}x + \frac{a}{b} \\ y`, true}} {
		display := c.display
		omml, err := LaTeXToOMMLWith(c.latex, display, Options{RunProperties: rpr})
		if err != nil {
			t.Fatal(err)
		}
		root := mustRoot(t, omml)
		runs, with := 0, 0
		root.Walk(func(n *tree.Node) bool {
			if n.Is(OMMLNS, "r") {
				runs++
				els := n.Elements()
				i := 0
				if len(els) > 0 && els[0].Is(OMMLNS, "rPr") {
					i = 1
				}
				if i < len(els) && els[i].Is(wordNS, "rPr") && string(els[i].Raw()) == rpr {
					with++
				}
			}
			return true
		})
		if runs == 0 || with != runs {
			t.Errorf("display %v: %d of %d runs carry the formatting: %s", display, with, runs, omml)
		}
		plain, err := LaTeXToOMML(c.latex, display)
		if err != nil {
			t.Fatal(err)
		}
		got, _, losses := OMMLToLaTeX(root)
		want, _, _ := OMMLToLaTeX(mustRoot(t, plain))
		if got != want {
			t.Errorf("display %v: formatting changed the LaTeX: %q, want %q", display, got, want)
		}
		for _, l := range losses {
			if l.Kind != LossFormatting {
				t.Errorf("display %v: loss %+v", display, l)
			}
		}
	}
	for _, bad := range []string{"<w:b/>", "<x:rPr xmlns:x=\"urn:x\"/>", "<w:rPr/><w:rPr/>", "<w:rPr>", "text"} {
		if _, err := LaTeXToOMMLWith("x", false, Options{RunProperties: bad}); !errors.Is(err, errRunProperties) {
			t.Errorf("%q: err = %v", bad, err)
		}
	}
	if _, err := LaTeXToOMMLWith(`\bogus`, false, Options{RunProperties: rpr}); !errors.Is(err, ErrInvalidLaTeX) {
		t.Errorf("bad LaTeX: %v", err)
	}
	if s, _ := LaTeXToOMMLWith("x", false, Options{}); strings.Contains(s, "xmlns:w") {
		t.Errorf("no template declares w: %s", s)
	}
}

func mustRoot(t *testing.T, s string) *tree.Node {
	t.Helper()
	doc, err := tree.Parse([]byte(s))
	if err != nil {
		t.Fatalf("%v: %s", err, s)
	}
	return doc.Root()
}
