package tree

import (
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/jhnoel/documentdriver/internal/ooxml/xmlscan"
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

// lookup returns the namespace URI bound to prefix, and whether it is bound.
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
	for c := n.first; c != nil; c = c.next {
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
	var err error
	if c.has(fDecls) {
		c.eachAttr(func(_ int, a attr) bool {
			p, ok := declOf(a.name)
			if !ok {
				return true
			}
			uri := xmlscan.DecodeAttr(a.raw)
			switch {
			case p == "xmlns":
				err = fmt.Errorf("xml: <%s>: the xmlns prefix cannot be declared", c.qname())
			case p == "xml" && uri != XMLNamespace, p != "xml" && uri == XMLNamespace:
				err = fmt.Errorf("xml: <%s>: invalid binding of the xml namespace", c.qname())
			case p != "" && uri == "":
				err = fmt.Errorf("xml: <%s>: prefix %s bound to empty namespace", c.qname(), p)
			case uri == xmlnsNamespace:
				err = fmt.Errorf("xml: <%s>: the xmlns namespace cannot be bound", c.qname())
			}
			if err != nil {
				return false
			}
			if decls == nil {
				decls = map[string]string{}
			}
			decls[p] = uri
			return true
		})
		if err != nil {
			return nil, err
		}
	}
	if decls != nil {
		sc = &scope{parent: parent, decls: decls}
	}
	if p := c.Prefix(); p != "" {
		if _, ok := sc.lookup(p); !ok && !fragment {
			return nil, fmt.Errorf("xml: <%s>: %w %q", c.qname(), ErrUndeclaredPrefix, p)
		}
	}
	// Expanded names of the prefixed attributes, to find duplicates: an
	// element has few, so a list beats a map per element.
	var seen []string
	c.eachAttr(func(_ int, a attr) bool {
		if _, ok := declOf(a.name); ok {
			return true
		}
		p, local := splitQName(a.name)
		if p == "" {
			return true
		}
		uri, ok := sc.lookup(p)
		if !ok {
			if fragment {
				return true
			}
			err = fmt.Errorf("xml: <%s>: attribute %s: %w %q", c.qname(), a.name, ErrUndeclaredPrefix, p)
			return false
		}
		key := uri + " " + local
		if slices.Contains(seen, key) {
			err = fmt.Errorf("xml: <%s>: duplicate attribute {%s}%s", c.qname(), uri, local)
			return false
		}
		seen = append(seen, key)
		return true
	})
	if err != nil {
		return nil, err
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
		if e.has(fDecls) {
			for _, d := range e.x.ns {
				if d.prefix == prefix {
					if prefix == "" && d.uri == "" {
						return "", false // xmlns="" undeclares the default
					}
					return d.uri, true
				}
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
// the URI, decoded.
func (n *Node) Declarations(f func(prefix, uri string)) {
	if n.kind != ElementNode || !n.has(fDecls) {
		return
	}
	for _, d := range n.x.ns {
		f(d.prefix, d.uri)
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
		if e.kind != ElementNode || !e.has(fDecls) {
			continue
		}
		for _, d := range e.x.ns {
			p := d.prefix
			if slices.Contains(seen, p) {
				continue
			}
			seen = append(seen, p)
			if d.uri != uri {
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
			return fmt.Errorf("tree: inserting <%s>: %w %q", c.qname(), ErrUndeclaredPrefix, p)
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
		q := preferredQuote(c)
		as := c.ownAttrList()
		c.x.attrs = append(as, builtAttr(name, q, xmlscan.EscapeAttr(hints[p], q)))
		c.attrsChanged()
	}
	if c.x != nil {
		c.x.nsHints = nil
		if !c.x.hasRaw && !c.has(fOwnAttrs) && !c.has(fDecls) && c.x.name == "" && c.tagLen != tagLenFar {
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
	n.eachAttr(func(_ int, a attr) bool {
		if p, ok := declOf(a.name); ok {
			if decls == nil {
				decls = map[string]string{}
			}
			decls[p] = xmlscan.DecodeAttr(a.raw)
		}
		return true
	})
	if decls != nil {
		sc = &scope{parent: sc, decls: decls}
	}
	p := n.Prefix()
	if _, ok := sc.lookup(p); !ok {
		used[p] = true
	}
	n.eachAttr(func(_ int, a attr) bool {
		if _, ok := declOf(a.name); ok {
			return true
		}
		ap, _ := splitQName(a.name)
		if ap == "" {
			return true
		}
		if _, ok := sc.lookup(ap); !ok {
			used[ap] = true
		}
		return true
	})
	for ch := n.first; ch != nil; ch = ch.next {
		collectUndeclared(ch, sc, used)
	}
}

// declaredInside reports whether any element in n's subtree declares prefix.
func declaredInside(n *Node, prefix string) bool {
	if n.kind != ElementNode {
		return false
	}
	if declares(n, prefix) {
		return true
	}
	for c := n.first; c != nil; c = c.next {
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
	if declares(n, from) {
		return
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
	if nn, ok := rename(n.qname()); ok {
		n.setName(nn)
		n.flags |= fEndLost
		n.flags |= fTagDirty
		n.markDirty()
	}
	if from != "" { // unprefixed attributes are in no namespace
		var renames []int
		n.eachAttr(func(i int, a attr) bool {
			if _, ok := declOf(a.name); !ok {
				if _, ok := rename(a.name); ok {
					renames = append(renames, i)
				}
			}
			return true
		})
		if len(renames) > 0 {
			as := n.ownAttrList()
			for _, i := range renames {
				as[i].name, _ = rename(as[i].name)
			}
			n.attrsChanged()
		}
	}
	for c := n.first; c != nil; c = c.next {
		renamePrefix(c, from, to)
	}
}

// declares reports whether element n itself declares prefix ("" for the
// default namespace).
func declares(n *Node, prefix string) bool {
	if !n.has(fDecls) {
		return false
	}
	for _, d := range n.x.ns {
		if d.prefix == prefix {
			return true
		}
	}
	return false
}
