package derive

import "github.com/jhnoel/documentdriver/internal/ooxml/tree"

// Namespace families: each holds the transitional and the strict URI.
var (
	// DML is DrawingML main.
	DML = []string{NS, "http://purl.oclc.org/ooxml/drawingml/main"}
	// PML is PresentationML main.
	PML = []string{"http://schemas.openxmlformats.org/presentationml/2006/main", "http://purl.oclc.org/ooxml/presentationml/main"}
	// REL is the relationships namespace used for r:id attributes.
	REL = []string{"http://schemas.openxmlformats.org/officeDocument/2006/relationships", "http://purl.oclc.org/ooxml/officeDocument/relationships"}
	// MC is markup compatibility.
	MC = []string{"http://schemas.openxmlformats.org/markup-compatibility/2006"}
)

// Is reports whether n is an element with local name local in one of the
// namespaces in fam.
func Is(n *tree.Node, fam []string, local string) bool {
	if n == nil || n.Kind() != tree.ElementNode || n.Local() != local {
		return false
	}
	sp := n.Space()
	for _, u := range fam {
		if sp == u {
			return true
		}
	}
	return false
}

// In reports whether n is an element in one of the namespaces in fam.
func In(n *tree.Node, fam []string) bool {
	if n == nil || n.Kind() != tree.ElementNode {
		return false
	}
	sp := n.Space()
	for _, u := range fam {
		if sp == u {
			return true
		}
	}
	return false
}

// Child returns the first element child of n with local name local in fam.
func Child(n *tree.Node, fam []string, local string) *tree.Node {
	if n == nil {
		return nil
	}
	for c := range n.ChildrenSeq() {
		if Is(c, fam, local) {
			return c
		}
	}
	return nil
}

// Path follows a chain of children in fam, returning nil if any is missing.
func Path(n *tree.Node, fam []string, locals ...string) *tree.Node {
	for _, l := range locals {
		n = Child(n, fam, l)
		if n == nil {
			return nil
		}
	}
	return n
}

// AttrNS returns the value of an attribute in one of the namespaces of
// fam with local name local.
func AttrNS(n *tree.Node, fam []string, local string) (string, bool) {
	for _, u := range fam {
		if v, ok := n.AttrNS(u, local); ok {
			return v, true
		}
	}
	return "", false
}
