package tree

import "slices"

import "github.com/jhnoel/documentdriver/internal/ooxml/xmlscan"

// NSStack resolves namespace prefixes for a Visitor: Scan reports nodes
// without their ancestors' declarations, so a visitor that needs a
// node's namespace keeps them here. Push in Start, Pop in End (once per
// element, self-closing ones included). The zero value is ready to use;
// in steady state it does not allocate. It keeps only bindings: a
// visitor's own per-element state stays in the visitor.
type NSStack struct {
	binds []nsDecl // every declaration in scope, outermost first
	marks []int    // how many binds each open element added
}

// Push records the namespace declarations of element n (Node.Declarations)
// for n and its descendants.
func (s *NSStack) Push(n *Node) {
	nb := 0
	n.Declarations(func(prefix, uri string) {
		s.binds = append(s.binds, nsDecl{prefix, uri})
		nb++
	})
	s.marks = append(s.marks, nb)
}

// Pop drops the declarations of the innermost element pushed.
func (s *NSStack) Pop() {
	if len(s.marks) == 0 {
		return
	}
	nb := s.marks[len(s.marks)-1]
	s.marks = s.marks[:len(s.marks)-1]
	s.binds = s.binds[:len(s.binds)-nb]
}

// Lookup returns the namespace URI bound to prefix ("" for the default
// namespace; "xml" is always bound) in the innermost scope pushed, and
// false when none is.
func (s *NSStack) Lookup(prefix string) (string, bool) {
	switch prefix {
	case "xml":
		return XMLNamespace, true
	case "xmlns":
		return xmlnsNamespace, true
	}
	for _, v := range slices.Backward(s.binds) {
		if v.prefix == prefix {
			if prefix == "" && v.uri == "" {
				return "", false
			}
			return v.uri, true
		}
	}
	return "", false
}

// Space returns the namespace URI of element n's name in the scope
// pushed so far (n itself pushed): "" for no namespace or an undeclared
// prefix.
func (s *NSStack) Space(n *Node) string {
	uri, _ := s.Lookup(n.Prefix())
	return uri
}

// AttrNS returns the decoded value of element n's attribute whose
// namespace is uri and local name is local, resolving its prefix in the
// scope pushed so far ("" uri for an unprefixed attribute).
func (s *NSStack) AttrNS(n *Node, uri, local string) (string, bool) {
	var value string
	ok := false
	n.eachAttr(func(_ int, a attr) bool {
		if _, isDecl := declOf(a.name); isDecl {
			return true
		}
		p, l := splitQName(a.name)
		if l != local {
			return true
		}
		if p == "" {
			if uri == "" {
				value, ok = xmlscan.DecodeAttr(a.raw), true
				return false
			}
			return true
		}
		if got, found := s.Lookup(p); found && got == uri {
			value, ok = xmlscan.DecodeAttr(a.raw), true
			return false
		}
		return true
	})
	return value, ok
}
