package integrity

import (
	"fmt"
	"slices"
	"strings"

	"github.com/jhnoel/documentdriver/internal/ooxml/schema"
)

// The ECMA-376 Transitional content-model check of WordprocessingML parts.
// For each w: element it verifies which children are allowed, their
// order, how often they occur, which are required, and the attributes and
// attribute values the schema allows. Order ranks in the model are
// conservative, so an order the schema allows is never reported. As
// ECMA-376 Part 3 prescribes, an mc:AlternateContent is read as its
// mc:Fallback, and content in ignorable or Office extension namespaces is
// skipped.

// checkSchema checks a part whose root is a WordprocessingML root element.
func checkSchema(d *document, part string, m *contentModel, findings *Findings) {
	kind, ok := rootType(d.root.ns, d.root.local)
	if !ok {
		return
	}
	c := &schemaChecker{part: part, m: m, findings: findings, ignorable: ignorableNamespaces(d)}
	c.element(d.root, kind)
}

// ignorableNamespaces returns the namespaces mc:Ignorable declares
// anywhere in the part.
func ignorableNamespaces(d *document) map[string]bool {
	out := map[string]bool{}
	for n := range d.elements() {
		list, ok := n.attr(mcNS, "Ignorable")
		if !ok {
			continue
		}
		for _, prefix := range strings.Fields(list) {
			if uri, ok := n.lookupPrefix(prefix); ok {
				out[uri] = true
			}
		}
	}
	return out
}

type schemaChecker struct {
	part      string
	m         *contentModel
	findings  *Findings
	ignorable map[string]bool
	// counts and stack are scratch shared by every element: an
	// element's child counts (by childModel.index) are read before its
	// children are checked, and stack holds the children to descend
	// into, each element's above its parent's.
	counts []int
	stack  []descent
}

// add records a schema fault located at element n: the element whose
// attributes, counts or text are wrong, or the child that is out of
// place.
func (c *schemaChecker) add(n *element, message string) {
	c.findings.addAt(c.part, "schema: "+message, []*element{n})
}

// skipped reports whether namespace ns is skipped by schema checking.
func (c *schemaChecker) skipped(ns string) bool {
	return ns == mcNS || strings.HasPrefix(ns, officeExtensionPrefix) || c.ignorable[ns]
}

type descent struct {
	el   *element
	kind string
}

// contentState is what checking an element's children carries from one
// child to the next.
type contentState struct {
	n             *element
	tm            *typeModel
	highest       int
	runProperties bool
	extension     bool
}

// element checks n against complex type kind, then its children; the
// recursion follows the tree, whose depth the tokenizer bounds.
func (c *schemaChecker) element(n *element, kind string) {
	m := c.m
	tm, ok := m.typeModel(kind)
	if !ok {
		return
	}
	for _, a := range n.attrs() {
		if a.ns != wNS && a.ns != rNS && a.ns != xmlNS {
			continue
		}
		am, ok := tm.attrs[qname{a.ns, a.local}]
		switch {
		case !ok && (tm.anyAttr || a.ns == xmlNS):
		case !ok:
			c.add(n, fmt.Sprintf("%s has unexpected attribute %s", label(n.ns, n.local), label(a.ns, a.local)))
		case am.simple != "" && !m.valid(am.simple, a.value, 0):
			// A named type by its local name (ST_Jc); the anonymous type of
			// xml:space, tabulated under the attribute's name, by that name.
			short := am.simple[strings.LastIndexByte(am.simple, ':')+1:]
			if !strings.HasPrefix(short, "ST_") {
				short = am.simple
			}
			c.add(n, fmt.Sprintf("%s/@%s is not a valid %s", label(n.ns, n.local), label(a.ns, a.local), short))
		}
	}
	for _, key := range tm.attrKeys {
		if !tm.attrs[key].required {
			continue
		}
		if _, ok := n.attr(key.ns, key.local); !ok {
			c.add(n, fmt.Sprintf("%s lacks required attribute %s", label(n.ns, n.local), label(key.ns, key.local)))
		}
	}
	c.counts = slices.Grow(c.counts[:0], len(tm.order))[:len(tm.order)]
	clear(c.counts)
	base := len(c.stack)
	// Word's run properties close their base sequence with the Office
	// extensions (w14 text effects); only rPrChange may follow them.
	st := contentState{n: n, tm: tm, highest: -1, runProperties: kind == "w:CT_RPr" || kind == "w:CT_ParaRPr"}
	c.children(n, &st)
	for _, key := range tm.order {
		cm := tm.children[key]
		count := c.counts[cm.index]
		if cm.max != schema.Unbounded && count > cm.max {
			c.add(n, fmt.Sprintf("%s occurs too often in %s", label(key.ns, key.local), label(n.ns, n.local)))
		}
		if cm.required && count == 0 {
			c.add(n, fmt.Sprintf("%s lacks required %s", label(n.ns, n.local), label(key.ns, key.local)))
		}
	}
	if n.hasText() && len(tm.children) > 0 {
		c.add(n, "text is not allowed directly in "+label(n.ns, n.local))
	}
	// The descents are read by index: checking one appends its own
	// above them.
	for i, end := base, len(c.stack); i < end; i++ {
		d := c.stack[i]
		c.element(d.el, d.kind)
	}
	c.stack = c.stack[:base]
}

// children checks the element children of n (the element being checked,
// or an mc:Fallback inside it) in order, reading each
// mc:AlternateContent as its Fallback's children (depth bounded by the
// tree).
func (c *schemaChecker) children(n *element, st *contentState) {
	tm := st.tm
	for ch := n.firstEl(); ch != nil; ch = ch.nextEl() {
		if ch.is(mcNS, "AlternateContent") {
			if fb := ch.firstChild(mcNS, "Fallback"); fb != nil {
				c.children(fb, st)
			}
			continue
		}
		if st.runProperties && strings.HasPrefix(ch.ns, officeExtensionPrefix) {
			st.extension = true
		}
		if c.skipped(ch.ns) {
			continue
		}
		if ch.ns == wNS && ch.local != "rPrChange" && st.extension {
			c.add(ch, fmt.Sprintf("%s follows an Office extension element in %s", label(ch.ns, ch.local), label(st.n.ns, st.n.local)))
		}
		cm, ok := tm.children[qname{ch.ns, ch.local}]
		if !ok {
			if !admitted(tm.anyNS, ch.ns) && ch.ns == wNS {
				c.add(ch, fmt.Sprintf("%s is not allowed in %s", label(ch.ns, ch.local), label(st.n.ns, st.n.local)))
			}
			continue
		}
		c.counts[cm.index]++
		if cm.rank != schema.NoRank {
			if st.highest >= 0 && cm.rank < st.highest {
				c.add(ch, fmt.Sprintf("%s is out of order in %s", label(ch.ns, ch.local), label(st.n.ns, st.n.local)))
			}
			st.highest = max(st.highest, cm.rank)
		}
		if cm.kind != "" {
			c.stack = append(c.stack, descent{ch, cm.kind})
		}
	}
}

// admitted reports whether a wildcard list admits namespace ns.
func admitted(anyNS []string, ns string) bool {
	for _, a := range anyNS {
		switch a {
		case "##any":
			return true
		case "##other":
			if ns != wNS {
				return true
			}
		default:
			for _, n := range strings.Fields(a) {
				if n == ns {
					return true
				}
			}
		}
	}
	return false
}
