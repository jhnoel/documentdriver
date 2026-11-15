package html

import (
	"errors"
	"slices"
	"strings"

	"github.com/jhnoel/documentdriver/internal/ooxml/tree"
)

// OMMLNamespace is the Office Math namespace.
const OMMLNamespace = "http://schemas.openxmlformats.org/officeDocument/2006/math"

// mergeBlockEquations joins each run of block equations in a paragraph
// that are separated only by whitespace into one equation: one
// m:oMathPara holding every m:oMath of the run, in order, rather than one
// oMathPara per equation with whitespace runs between them. The merged
// LaTeX is the parts joined by \\, the form a single display equation of
// several lines takes in LaTeX (which the converter compiles to one
// m:oMathPara of several m:oMath), so the merged LaTeX and OMML agree. Equations merge only when they carry the same revision wrapping;
// the whitespace between them is dropped.
func mergeBlockEquations(items []Item) ([]Item, error) {
	if !slices.ContainsFunc(items, isBlockEquation) {
		return items, nil
	}
	var out []Item
	for i := 0; i < len(items); i++ {
		it := items[i]
		if !isBlockEquation(it) {
			out = append(out, it)
			continue
		}
		run := []Atom{it.Atom}
		j := i + 1
		for k := j; k < len(items); k++ {
			if isBlockEquation(items[k]) && items[k].Atom.Ins == it.Atom.Ins && items[k].Atom.Del == it.Atom.Del {
				run = append(run, items[k].Atom)
				j = k + 1
				continue
			}
			if !isWhitespaceChar(items[k]) {
				break
			}
		}
		if len(run) == 1 {
			out = append(out, it)
			continue
		}
		merged, err := mergeOMathParas(run)
		if err != nil {
			return nil, err
		}
		out = append(out, Item{Kind: ItemAtom, Atom: merged})
		i = j - 1
	}
	return out, nil
}

// isBlockEquation reports whether it is a displayed equation atom.
func isBlockEquation(it Item) bool {
	return it.Kind == ItemAtom && it.Atom.Kind == AtomEquation && it.Atom.Display
}

// isWhitespaceChar reports whether it is a whitespace character atom.
func isWhitespaceChar(it Item) bool {
	return it.Kind == ItemAtom && it.Atom.Kind == AtomChar && strings.TrimFunc(it.Atom.Text, isSpace) == ""
}

var errBlockOMML = errors.New("equation converter returned a block equation that is not one m:oMathPara of m:oMath elements")

// mergeOMathParas concatenates the m:oMath children of each block
// equation's m:oMathPara under one new m:oMathPara.
func mergeOMathParas(run []Atom) (Atom, error) {
	var omml strings.Builder
	omml.WriteString(`<m:oMathPara xmlns:m="` + OMMLNamespace + `">`)
	latex := make([]string, 0, len(run))
	for _, a := range run {
		doc, err := tree.Parse([]byte(a.OMML))
		if err != nil {
			return Atom{}, errBlockOMML
		}
		root := doc.Root()
		if root == nil || !root.Is(OMMLNamespace, "oMathPara") || root.Prefix() != "m" {
			return Atom{}, errBlockOMML
		}
		for c := range root.ElementsSeq() {
			if !c.Is(OMMLNamespace, "oMath") || c.Prefix() != "m" {
				return Atom{}, errBlockOMML
			}
			omml.Write(c.Raw())
		}
		latex = append(latex, a.Text)
	}
	omml.WriteString("</m:oMathPara>")
	merged := run[0]
	obj := *merged.AtomObject // its own copy: the parts' stay as they were
	merged.AtomObject = &obj
	merged.Text = strings.Join(latex, ` \\ `)
	merged.OMML = omml.String()
	return merged, nil
}
