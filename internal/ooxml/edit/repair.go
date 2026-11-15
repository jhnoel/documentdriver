package edit

import (
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/jhnoel/documentdriver/internal/ooxml/tree"
)

// MC is the markup-compatibility namespace (ECMA-376 Part 3) of
// mc:Ignorable and mc:AlternateContent, shared by every vocabulary.
var MC = NS{URIs: []string{"http://schemas.openxmlformats.org/markup-compatibility/2006"}, Prefix: "mc"}

// EnsureRootNamespace makes sure the root element of part declares
// xmlns:prefix="uri" and, when ignorable, lists prefix in mc:Ignorable
// (appended at the end; the attribute, and the mc declaration, made when
// absent), so that attributes in that namespace written anywhere in the
// part resolve. A root in that namespace itself is never marked
// ignorable: Word reads a part whose own vocabulary is ignorable as
// having no content it understands. It declares exactly these changes in
// the write set (plan.WriteSet.DeclareRootNamespace) and changes nothing
// when all are present. It fails, changing nothing, when the root binds
// prefix to another namespace.
func (w *Workspace) EnsureRootNamespace(part, prefix, uri string, ignorable bool) error {
	doc, err := w.Tree(part)
	if err != nil {
		return err
	}
	root := doc.Root()
	if root == nil {
		return fmt.Errorf("edit: %s has no root element", part)
	}
	if got, ok := root.Attr("xmlns:" + prefix); ok && got != uri {
		return fmt.Errorf("edit: %s: the root binds %s to %q, not %q", part, prefix, got, uri)
	}
	if root.Space() == uri {
		ignorable = false
	}
	mcPrefix, listName, toks := "", "", []string(nil)
	if ignorable {
		for _, a := range root.Attrs() {
			p, local, ok := strings.Cut(a.Name, ":")
			if ok && local == "Ignorable" && p != "xmlns" {
				if u, _ := root.LookupPrefix(p); u == MC.URIs[0] {
					listName, toks = a.Name, strings.Fields(a.Value)
					break
				}
			}
		}
		if listName == "" {
			mcPrefix = MC.Prefix
			if p, ok := root.LookupNamespace(MC.URIs[0]); ok && p != "" {
				mcPrefix = p
			} else if got, ok := root.LookupPrefix(MC.Prefix); ok && got != MC.URIs[0] {
				return fmt.Errorf("edit: %s: the root binds mc to %q", part, got)
			}
			listName = mcPrefix + ":Ignorable"
		}
	}
	w.ws.DeclareRootNamespace(part, prefix, uri, ignorable)
	if _, ok := root.Attr("xmlns:" + prefix); !ok {
		if err := root.SetAttr("xmlns:"+prefix, uri); err != nil {
			return err
		}
	}
	if !ignorable || slices.Contains(toks, prefix) {
		return nil
	}
	if mcPrefix != "" {
		if _, ok := root.LookupPrefix(mcPrefix); !ok {
			w.ws.DeclareRootNamespace(part, mcPrefix, MC.URIs[0], false)
			if err := root.SetAttr("xmlns:"+mcPrefix, MC.URIs[0]); err != nil {
				return err
			}
		}
	}
	return root.SetAttr(listName, strings.Join(append(toks, prefix), " "))
}

// Repair sets attribute qname of element n in part to value, declaring
// it as a repair (plan.WriteSet.DeclareRepair): n may be inside a unit no
// operation targeted. It is for identity attributes a format restores on
// every save (a missing, malformed or duplicate w14:paraId); the prefix
// of qname must be in scope at n (declare it on the root first with
// EnsureRootNamespace).
func (w *Workspace) Repair(part string, n *tree.Node, qname, value string) error {
	if n == nil || n.Kind() != tree.ElementNode {
		return fmt.Errorf("edit: repair of %s: not an element", qname)
	}
	w.ws.DeclareRepair(part, n, qname)
	return n.SetAttr(qname, value)
}

// ErrNotCreated is returned by SetCreatedAttr for an element the plan did
// not create.
var ErrNotCreated = errors.New("edit: element was not created by this plan")

// SetCreatedAttr sets attribute qname of element n to value, for ids
// assigned after an element was created (a fresh w:id on a cloned
// revision, a w14:paraId on a copied paragraph). n must be one this
// workspace created (Created), which places it inside a declared target
// or container; it fails with ErrNotCreated otherwise.
func (w *Workspace) SetCreatedAttr(n *tree.Node, qname, value string) error {
	if n == nil || !w.Created(n) {
		return ErrNotCreated
	}
	return n.SetAttr(qname, value)
}
