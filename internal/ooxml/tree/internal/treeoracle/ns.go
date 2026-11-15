package treeoracle

import (
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"
)

// XMLNamespace is the namespace bound to the reserved prefix "xml".
const XMLNamespace = "http://www.w3.org/XML/1998/namespace"

const xmlnsNamespace = "http://www.w3.org/2000/xmlns/"

// ErrUndeclaredPrefix is returned when a name uses a prefix that no
// declaration in scope binds.
var ErrUndeclaredPrefix = errors.New("undeclared namespace prefix")

// declOf reports whether attribute name declares a namespace, and for which
// prefix ("" for the default namespace).
func declOf(name string) (prefix string, ok bool) {
	if name == "xmlns" {
		return "", true
	}
	if strings.HasPrefix(name, "xmlns:") {
		return name[6:], true
	}
	return "", false
}

// scope is a chain of namespace declarations.
type scope struct {
	parent *scope
	decls  map[string]string
}

func (s *scope) lookup(prefix string) (string, bool) {
	switch prefix {
	case "xml":
		return XMLNamespace, true
	case "xmlns":
		return xmlnsNamespace, true
	}
	for ; s != nil; s = s.parent {
		if uri, ok := s.decls[prefix]; ok {
			return uri, true
		}
	}
	return "", false
}

// checkNamespaces enforces namespace well-formedness below n. In fragment
// mode undeclared prefixes are allowed: they are resolved on insertion.
func checkNamespaces(n *Node, parent *scope, fragment bool) error {
	for _, c := range n.children {
		if c.kind != ElementNode {
			continue
		}
		sc, err := checkElement(c, parent, fragment)
		if err != nil {
			return err
		}
		if err := checkNamespaces(c, sc, fragment); err != nil {
			return err
		}
	}
	return nil
}

// checkElement enforces namespace well-formedness on element c, in
// scope parent, and returns the scope of its children.
func checkElement(c *Node, parent *scope, fragment bool) (*scope, error) {
	sc := parent
	var decls map[string]string
	for _, a := range c.attrs {
		p, ok := declOf(a.name)
		if !ok {
			continue
		}
		uri := decodeAttr(a.raw)
		switch {
		case p == "xmlns":
			return nil, fmt.Errorf("xml: <%s>: the xmlns prefix cannot be declared", c.name)
		case p == "xml" && uri != XMLNamespace, p != "xml" && uri == XMLNamespace:
			return nil, fmt.Errorf("xml: <%s>: invalid binding of the xml namespace", c.name)
		case p != "" && uri == "":
			return nil, fmt.Errorf("xml: <%s>: prefix %s bound to empty namespace", c.name, p)
		case uri == xmlnsNamespace:
			return nil, fmt.Errorf("xml: <%s>: the xmlns namespace cannot be bound", c.name)
		}
		if decls == nil {
			decls = map[string]string{}
		}
		decls[p] = uri
	}
	if decls != nil {
		sc = &scope{parent: parent, decls: decls}
	}
	if p := c.Prefix(); p != "" {
		if _, ok := sc.lookup(p); !ok && !fragment {
			return nil, fmt.Errorf("xml: <%s>: %w %q", c.name, ErrUndeclaredPrefix, p)
		}
	}
	// Expanded names of the prefixed attributes, to find duplicates: an
	// element has few, so a list beats a map per element.
	var seen []string
	for _, a := range c.attrs {
		if _, ok := declOf(a.name); ok {
			continue
		}
		p, local := splitQName(a.name)
		if p == "" {
			continue
		}
		uri, ok := sc.lookup(p)
		if !ok {
			if fragment {
				continue
			}
			return nil, fmt.Errorf("xml: <%s>: attribute %s: %w %q", c.name, a.name, ErrUndeclaredPrefix, p)
		}
		key := uri + " " + local
		if slices.Contains(seen, key) {
			return nil, fmt.Errorf("xml: <%s>: duplicate attribute {%s}%s", c.name, uri, local)
		}
		seen = append(seen, key)
	}
	return sc, nil
}

// LookupPrefix returns the namespace URI bound to prefix ("" for the default
// namespace) in scope at n, which may be an element or any node inside one.
func (n *Node) LookupPrefix(prefix string) (string, bool) {
	switch prefix {
	case "xml":
		return XMLNamespace, true
	case "xmlns":
		return xmlnsNamespace, true
	}
	for e := n; e != nil; e = e.parent {
		if e.kind != ElementNode {
			continue
		}
		for _, a := range e.attrs {
			if p, ok := declOf(a.name); ok && p == prefix {
				uri := e.declValue(a)
				if prefix == "" && uri == "" {
					return "", false // xmlns="" undeclares the default
				}
				return uri, true
			}
		}
		if e.parent == nil {
			// A detached subtree resolves the prefixes it was given
			// (ParseFragment's ns, NewElement's own) until it is
			// inserted, when they are resolved against the document.
			if uri, ok := e.hints()[prefix]; ok && uri != "" {
				return uri, true
			}
		}
	}
	return "", false
}

// Declarations calls f with each namespace declaration on element n, in
// document order: the declared prefix ("" for the default namespace) and
// the URI, decoded once and cached while the declaration keeps its value.
func (n *Node) Declarations(f func(prefix, uri string)) {
	if n.kind != ElementNode {
		return
	}
	for _, a := range n.attrs {
		if p, ok := declOf(a.name); ok {
			f(p, n.declValue(a))
		}
	}
}

// LookupNamespace returns a prefix bound to uri in scope at n and not
// shadowed by a nearer declaration. A non-empty prefix is preferred over
// the default namespace, since only prefixed names can be used for
// attributes; ok is false if uri is not in scope.
func (n *Node) LookupNamespace(uri string) (prefix string, ok bool) {
	if uri == XMLNamespace {
		return "xml", true
	}
	found, def := "", false
	// The prefixes declared nearer, which shadow farther declarations: a
	// handful, so a slice on the stack rather than a map.
	var seenBuf [16]string
	seen := seenBuf[:0]
	for e := n; e != nil; e = e.parent {
		if e.kind != ElementNode {
			continue
		}
		for _, a := range e.attrs {
			p, isDecl := declOf(a.name)
			if !isDecl || slices.Contains(seen, p) {
				continue
			}
			seen = append(seen, p)
			if e.declValue(a) != uri {
				continue
			}
			if p == "" {
				def = true
				continue
			}
			if found == "" {
				found = p
			}
		}
	}
	if found != "" {
		return found, true
	}
	if def {
		return "", true
	}
	return "", false
}

// adopt resolves the namespace hints of a detached subtree c that is being
// inserted under parent: each prefix used in c but declared neither inside c
// nor at parent is either renamed to an in-scope prefix for the same URI or
// declared on c.
func adopt(parent, c *Node) error {
	if c.kind != ElementNode {
		return nil
	}
	hints := c.hints()
	used := map[string]bool{}
	collectUndeclared(c, nil, used)
	type action struct {
		from, to string
	}
	var renames []action
	var declare []string
	for _, p := range slices.Sorted(maps.Keys(used)) {
		uri, hinted := hints[p]
		if !hinted {
			if p == "" {
				continue // unprefixed names simply take the default in scope, if any
			}
			if _, ok := parent.LookupPrefix(p); ok {
				continue
			}
			return fmt.Errorf("tree: inserting <%s>: %w %q", c.name, ErrUndeclaredPrefix, p)
		}
		if got, ok := parent.LookupPrefix(p); ok && got == uri {
			continue
		}
		if p == "" && uri == "" {
			if _, ok := parent.LookupPrefix(""); !ok {
				continue
			}
			declare = append(declare, p)
			continue
		}
		if q, ok := parent.LookupNamespace(uri); ok && q != "" && !declaredInside(c, q) {
			renames = append(renames, action{p, q})
			continue
		}
		declare = append(declare, p)
	}
	for _, r := range renames {
		renamePrefix(c, r.from, r.to)
	}
	for _, p := range declare {
		name := "xmlns"
		if p != "" {
			name = "xmlns:" + p
		}
		c.attrs = append(c.attrs, builtAttr(name, preferredQuote(c), escapeAttr(hints[p], preferredQuote(c))))
		c.tagDirty = true
		c.markDirty()
	}
	if c.x != nil {
		c.x.nsHints = nil
		if c.x.raw == nil && c.x.nsCache == nil {
			c.x = nil // nothing else needed it
		}
	}
	return nil
}

// collectUndeclared records prefixes used for element or attribute names in
// n's subtree that the subtree itself does not declare.
func collectUndeclared(n *Node, sc *scope, used map[string]bool) {
	if n.kind != ElementNode {
		return
	}
	var decls map[string]string
	for _, a := range n.attrs {
		if p, ok := declOf(a.name); ok {
			if decls == nil {
				decls = map[string]string{}
			}
			decls[p] = decodeAttr(a.raw)
		}
	}
	if decls != nil {
		sc = &scope{parent: sc, decls: decls}
	}
	p := n.Prefix()
	if _, ok := sc.lookup(p); !ok {
		used[p] = true
	}
	for _, a := range n.attrs {
		if _, ok := declOf(a.name); ok {
			continue
		}
		ap, _ := splitQName(a.name)
		if ap == "" {
			continue
		}
		if _, ok := sc.lookup(ap); !ok {
			used[ap] = true
		}
	}
	for _, ch := range n.children {
		collectUndeclared(ch, sc, used)
	}
}

func declaredInside(n *Node, prefix string) bool {
	if n.kind != ElementNode {
		return false
	}
	for _, a := range n.attrs {
		if p, ok := declOf(a.name); ok && p == prefix {
			return true
		}
	}
	for _, c := range n.children {
		if declaredInside(c, prefix) {
			return true
		}
	}
	return false
}

// renamePrefix rewrites names using prefix from (undeclared in n) to use
// prefix to, stopping where a nested declaration of from shadows it.
func renamePrefix(n *Node, from, to string) {
	if n.kind != ElementNode {
		return
	}
	for _, a := range n.attrs {
		if p, ok := declOf(a.name); ok && p == from {
			return
		}
	}
	rename := func(q string) (string, bool) {
		p, l := splitQName(q)
		if p != from {
			return q, false
		}
		if to == "" {
			return l, true
		}
		return to + ":" + l, true
	}
	if nn, ok := rename(n.name); ok {
		n.name = nn
		n.endAt = -1
		n.tagDirty = true
		n.markDirty()
	}
	for _, a := range n.attrs {
		if _, ok := declOf(a.name); ok {
			continue
		}
		if from == "" {
			continue // unprefixed attributes are in no namespace
		}
		if nn, ok := rename(a.name); ok {
			a.name = nn
			n.tagDirty = true
			n.markDirty()
		}
	}
	for _, c := range n.children {
		renamePrefix(c, from, to)
	}
}
