package plan

import (
	"fmt"
	"slices"
	"strings"

	"github.com/jhnoel/documentdriver/internal/ooxml/tree"
)

// mcURI is the markup-compatibility namespace of mc:Ignorable (ECMA-376
// Part 3), shared by every OOXML vocabulary.
const mcURI = "http://schemas.openxmlformats.org/markup-compatibility/2006"

// skipAttr marks attribute qname at offset at as excluded from byte comparisons.
func (v *verifier) skipAttr(at int, qname string) {
	if v.skip[at] == nil {
		v.skip[at] = map[string]bool{}
	}
	v.skip[at][qname] = true
}

// startTag returns n's start tag in doc, without the attributes the plan
// declared may change on it.
func (v *verifier) startTag(n *tree.Node, doc *tree.Document) []byte {
	s, _, ok := n.SpanIn(doc)
	if !ok || v.skip[s] == nil {
		return n.StartTag()
	}
	names := v.skip[s]
	return n.StartTagWithout(func(q string) bool { return names[q] })
}

// ignorable returns the qualified name and the tokens of root's
// mc:Ignorable attribute, or "" when it has none.
func ignorable(root *tree.Node) (string, []string) {
	for _, a := range root.Attrs() {
		prefix, local, ok := strings.Cut(a.Name, ":")
		if !ok || local != "Ignorable" || prefix == "xmlns" {
			continue
		}
		if uri, _ := root.LookupPrefix(prefix); uri == mcURI {
			return a.Name, strings.Fields(a.Value)
		}
	}
	return "", nil
}

// checkRoot holds the root element's declared namespace changes to what
// was declared: the declarations and the mc:Ignorable attribute are left
// out of the byte comparison (skip) and checked here instead.
func (v *verifier) checkRoot(orig, cur *tree.Node, decls map[string]RootNamespace) {
	s, _, _ := orig.Span()
	anyIgnorable := false
	prefixes := make([]string, 0, len(decls))
	for p, d := range decls {
		prefixes = append(prefixes, p)
		anyIgnorable = anyIgnorable || d.Ignorable
	}
	slices.Sort(prefixes)
	for _, p := range prefixes {
		q := "xmlns:" + p
		v.skipAttr(s, q)
		was, had := orig.Attr(q)
		now, has := cur.Attr(q)
		switch {
		case had && (!has || now != was):
			v.fail(orig, fmt.Sprintf("changed its declaration of %s", p))
		case !had && has && now != decls[p].URI:
			v.fail(orig, fmt.Sprintf("declares %s as %q, not the declared %q", p, now, decls[p].URI))
		}
	}
	if !anyIgnorable {
		return
	}
	oq, otoks := ignorable(orig)
	cq, ctoks := ignorable(cur)
	if oq != "" {
		v.skipAttr(s, oq)
	}
	if cq != "" {
		v.skipAttr(s, cq)
	}
	switch {
	case oq != "" && cq == "":
		v.fail(orig, "lost its mc:Ignorable list")
		return
	case oq != "" && cq != oq:
		v.fail(orig, "renamed its mc:Ignorable attribute")
		return
	}
	if len(ctoks) < len(otoks) || !slices.Equal(ctoks[:len(otoks)], otoks) {
		v.fail(orig, "changed the existing entries of its mc:Ignorable list")
		return
	}
	seen := map[string]bool{}
	for _, t := range otoks {
		seen[t] = true
	}
	for _, t := range ctoks[len(otoks):] {
		if !decls[t].Ignorable || seen[t] {
			v.fail(orig, fmt.Sprintf("added %s to its mc:Ignorable list, which no operation declared", t))
			return
		}
		seen[t] = true
	}
}
