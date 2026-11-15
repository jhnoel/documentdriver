package edit

import (
	"slices"
	"strings"

	"github.com/jhnoel/documentdriver/internal/ooxml/tree"
)

// NS is a namespace family: the URIs that mean the same vocabulary
// (transitional first, then strict), the prefix a new element takes when
// none of them is in scope, and the prefix of the vocabulary's table in
// internal/ooxml/schema ("p", "a", "w"; "" for a namespace that is not
// tabulated, such as relationships).
type NS struct {
	URIs   []string
	Prefix string
	Table  string
}

// Name returns the element name local in the family.
func (ns NS) Name(local string) Name { return Name{ns: ns, local: local} }

// Has reports whether n is an element in one of the family's URIs.
func (ns NS) Has(n *tree.Node) bool {
	return n != nil && n.Kind() == tree.ElementNode && slices.Contains(ns.URIs, n.Space())
}

// Is reports whether n is the element local in the family.
func (ns NS) Is(n *tree.Node, local string) bool { return ns.Has(n) && n.Local() == local }

// REL is the relationships namespace of r:id, r:embed and r:link
// attributes, shared by every OOXML vocabulary.
var REL = NS{URIs: []string{
	"http://schemas.openxmlformats.org/officeDocument/2006/relationships",
	"http://purl.oclc.org/ooxml/officeDocument/relationships",
}, Prefix: "r"}

// Name is an element name: a namespace family and a local name.
type Name struct {
	ns    NS
	local string
}

// Q returns an element name in a single namespace uri, which no schema
// table covers.
func Q(uri, prefix, local string) Name { return NS{URIs: []string{uri}, Prefix: prefix}.Name(local) }

// Local returns the local name.
func (n Name) Local() string { return n.local }

// NS returns the name's namespace family.
func (n Name) NS() NS { return n.ns }

// String returns the name with the family's prefix: p:sp.
func (n Name) String() string { return n.ns.Prefix + ":" + n.local }

// Matches reports whether e is an element with this name.
func (n Name) Matches(e *tree.Node) bool { return n.ns.Is(e, n.local) }

// newIn returns a new, empty element with this name for insertion under
// at: in the family's URI in scope there (transitional or strict), else
// its first, with the family's prefix.
func (n Name) newIn(at *tree.Node) (*tree.Node, error) {
	uri := n.ns.URIs[0]
	for _, u := range n.ns.URIs {
		if _, ok := at.LookupNamespace(u); ok {
			uri = u
			break
		}
	}
	return tree.NewElement(uri, n.ns.Prefix, n.local)
}

// Path is a sequence of child element names from a handle's element.
type Path []Name

// String joins the path's names with slashes.
func (p Path) String() string {
	var s []string
	for _, n := range p {
		s = append(s, n.String())
	}
	return strings.Join(s, "/")
}

// Vocab is the namespace families a format writes: the tabulated ones
// whose child orders place inserts, and the others whose URIs a fragment
// is mapped to (relationships).
type Vocab []NS

// table returns the schema table prefix of element e, "" when e is in no
// tabulated family.
func (v Vocab) table(e *tree.Node) string {
	for _, ns := range v {
		if ns.Table != "" && ns.Has(e) {
			return ns.Table
		}
	}
	return ""
}

// Remap maps each namespace URI in ns (prefix to URI, as for
// tree.ParseFragment) that is the first URI of one of the families to the
// member of that family declared at c, so new elements reuse the
// document's own namespace (transitional or strict).
func (v Vocab) Remap(c *tree.Node, ns map[string]string) map[string]string {
	out := map[string]string{}
	for p, uri := range ns {
		out[p] = uri
		for _, fam := range v {
			if fam.URIs[0] != uri {
				continue
			}
			for _, u := range fam.URIs {
				if _, ok := c.LookupNamespace(u); ok {
					out[p] = u
					break
				}
			}
		}
	}
	return out
}

// inScope returns every namespace declaration in scope at e, by prefix,
// so that a copy of e parses and re-inserts with the same bindings.
func inScope(e *tree.Node) map[string]string {
	out := map[string]string{}
	for p := e; p != nil; p = p.Parent() {
		p.Declarations(func(prefix, uri string) {
			if _, seen := out[prefix]; !seen {
				out[prefix] = uri
			}
		})
	}
	return out
}
