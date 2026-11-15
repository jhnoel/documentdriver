package math

import (
	"errors"
	"fmt"
	"strings"

	"github.com/jhnoel/documentdriver/internal/ooxml/tree"
)

// Run formatting is not part of the LaTeX surface: LaTeX carries the
// semantic styles (\mathrm, \mathbf, \boldsymbol, the alphabets), not a
// run's w:rPr (font, size, kerning, colour, language). An edit that
// replaces an equation gives the new runs the replaced equation's run
// formatting instead, so a replacement does not visibly change them:
// RunProperties picks it and LaTeXToOMMLWith applies it.

// Options are LaTeXToOMMLWith's settings.
type Options struct {
	// RunProperties is one w:rPr element, written with the w prefix, that
	// every m:r of the output carries after its m:rPr; "" for none. The
	// output root then declares xmlns:w as well as xmlns:m.
	RunProperties string
}

// errRunProperties is returned for Options.RunProperties that is not one
// w:rPr element with the w prefix.
var errRunProperties = errors.New("run properties must be one w:rPr element with the w prefix")

// LaTeXToOMMLWith is LaTeXToOMML with options.
func LaTeXToOMMLWith(latex string, display bool, opt Options) (string, error) {
	omml, err := LaTeXToOMML(latex, display)
	if err != nil || opt.RunProperties == "" {
		return omml, err
	}
	doc, err := tree.Parse([]byte(`<w:x xmlns:w="` + wordNS + `">` + opt.RunProperties + `</w:x>`))
	if err != nil {
		return "", fmt.Errorf("%w: %w", errRunProperties, err)
	}
	kids := doc.Root().Children()
	if len(kids) != 1 || !kids[0].Is(wordNS, "rPr") || kids[0].Prefix() != "w" {
		return "", errRunProperties
	}
	return withRunProperties(omml, opt.RunProperties), nil
}

// withRunProperties puts rpr in every m:r of OMML this package wrote
// (each run is "<m:r>", an optional m:rPr, then m:t; text never holds
// "<"), and declares the w prefix on the root.
func withRunProperties(omml, rpr string) string {
	var b strings.Builder
	rest := omml
	if i := strings.Index(rest, `xmlns:m="`); i >= 0 {
		b.WriteString(rest[:i] + `xmlns:w="` + wordNS + `" `)
		rest = rest[i:]
	}
	for {
		i := strings.Index(rest, "<m:r>")
		if i < 0 {
			b.WriteString(rest)
			return b.String()
		}
		i += len("<m:r>")
		if strings.HasPrefix(rest[i:], "<m:rPr>") {
			i += strings.Index(rest[i:], "</m:rPr>") + len("</m:rPr>")
		}
		b.WriteString(rest[:i] + rpr)
		rest = rest[i:]
	}
}

// RunProperties returns the run formatting an equation's runs share most:
// the raw w:rPr (w prefix) that the most m:r elements of eq carry, the
// first such in document order on a tie, or "" when no run has one. A
// replacement equation's runs take it (Options.RunProperties).
func RunProperties(eq *tree.Node) string {
	counts := map[string]int{}
	var order []string
	eq.Walk(func(n *tree.Node) bool {
		if !isElem(n, "r") || !inMath(n) {
			return true
		}
		for c := range n.ElementsSeq() {
			if c.Is(wordNS, "rPr") && c.Prefix() == "w" {
				s := string(c.Raw())
				if counts[s] == 0 {
					order = append(order, s)
				}
				counts[s]++
			}
		}
		return true
	})
	best := ""
	for _, s := range order {
		if counts[s] > counts[best] {
			best = s
		}
	}
	return best
}
