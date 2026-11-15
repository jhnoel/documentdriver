package model

import "github.com/jhnoel/documentdriver/internal/ooxml/tree"

// CurrentEquations returns the paragraph's current equations in document
// order: every m:oMath and m:oMathPara (by local name) inside it that is
// not inside a deletion, another m:oMathPara or a nested paragraph.
func CurrentEquations(p *tree.Node) []*tree.Node {
	return Descendants(p, func(n *tree.Node) bool {
		if !IsLocal(n, "oMath") && !IsLocal(n, "oMathPara") {
			return false
		}
		for a := n.Parent(); a != nil; a = a.Parent() {
			if a == p {
				return true
			}
			if IsRevDel(a) || IsLocal(a, "oMathPara") || IsW(a, "p") {
				return false
			}
		}
		return false
	})
}
