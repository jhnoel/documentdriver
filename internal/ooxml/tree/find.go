package tree

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/jhnoel/documentdriver/internal/ooxml/xmlscan"
)

// Walk calls fn for n and each of its descendants in document order. If fn
// returns false, the node's descendants are skipped. It keeps its own
// stack, so a tree of any depth walks without recursion.
func (n *Node) Walk(fn func(*Node) bool) {
	var buf [32]*Node
	stack := append(buf[:0], n)
	for len(stack) > 0 {
		e := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if !fn(e) {
			continue
		}
		if e.first != nil {
			for c := e.first.prev; ; c = c.prev {
				stack = append(stack, c)
				if c == e.first {
					break
				}
			}
		}
	}
}

// FindAll returns the descendant elements of n (excluding n) with namespace
// URI uri and local name local, in document order.
func (n *Node) FindAll(uri, local string) []*Node {
	var out []*Node
	for c := n.first; c != nil; c = c.next {
		c.Walk(func(e *Node) bool {
			if e.kind == ElementNode && e.Is(uri, local) {
				out = append(out, e)
			}
			return true
		})
	}
	return out
}

// Find returns the first descendant element of n with namespace URI uri and
// local name local, or nil.
func (n *Node) Find(uri, local string) *Node {
	var found *Node
	for c := n.first; c != nil; c = c.next {
		c.Walk(func(e *Node) bool {
			if found != nil {
				return false
			}
			if e.kind == ElementNode && e.Is(uri, local) {
				found = e
				return false
			}
			return true
		})
		if found != nil {
			break
		}
	}
	return found
}

// FindQName returns the descendant elements of n whose qualified name is
// exactly qname as written, in document order.
func (n *Node) FindQName(qname string) []*Node {
	var out []*Node
	for c := n.first; c != nil; c = c.next {
		c.Walk(func(e *Node) bool {
			if e.kind == ElementNode && e.qname() == qname {
				out = append(out, e)
			}
			return true
		})
	}
	return out
}

// Child returns the first element child of n with namespace URI uri and
// local name local, or nil.
func (n *Node) Child(uri, local string) *Node {
	for c := n.first; c != nil; c = c.next {
		if c.kind == ElementNode && c.Is(uri, local) {
			return c
		}
	}
	return nil
}

// step is one step of a selector.
type step struct {
	deep        bool   // preceded by "//"
	name        string // qualified name or "*"
	index       int    // 1-based position among matches, 0 for all
	attr, value string // [@attr='value'] predicate; value may be empty
	hasAttr     bool
}

// Select evaluates a small XPath-like selector against the document:
//
//	/p:sld/p:cSld/p:spTree/p:sp[2]
//	//p:cNvPr[@id='4']
//	//a:off[1]
//
// Steps are separated by "/" (child) or "//" (descendant). A step is a
// qualified name or "*", optionally followed by [n] (1-based position among
// the step's matches under one context node) and/or [@attr='value'] or
// [@attr]. Prefixes are resolved through the declarations in scope at the
// document element and compared by namespace URI; a prefix the document
// element does not declare is compared literally.
func (d *Document) Select(sel string) ([]*Node, error) {
	steps, err := parseSelector(sel)
	if err != nil {
		return nil, err
	}
	root := d.Root()
	ctx := []*Node{d.node}
	for _, s := range steps {
		var next []*Node
		for _, c := range ctx {
			var cand []*Node
			add := func(e *Node) {
				if e.kind == ElementNode && s.matches(root, e) {
					cand = append(cand, e)
				}
			}
			if s.deep {
				for ch := c.first; ch != nil; ch = ch.next {
					ch.Walk(func(e *Node) bool { add(e); return true })
				}
			} else {
				for ch := c.first; ch != nil; ch = ch.next {
					add(ch)
				}
			}
			if s.index > 0 {
				if s.index <= len(cand) {
					next = append(next, cand[s.index-1])
				}
			} else {
				next = append(next, cand...)
			}
		}
		ctx = next
	}
	return ctx, nil
}

// matches reports whether element e satisfies the selector step s.
func (s step) matches(root, e *Node) bool {
	if s.name != "*" {
		p, l := splitQName(s.name)
		if l != e.Local() {
			return false
		}
		uri, ok := "", false
		if root != nil {
			uri, ok = root.LookupPrefix(p)
		}
		if ok {
			if e.Space() != uri {
				return false
			}
		} else if e.qname() != s.name {
			return false
		}
	}
	if s.hasAttr {
		v, ok := e.Attr(s.attr)
		if !ok || (s.value != "" && v != s.value) {
			return false
		}
	}
	return true
}

// parseSelector parses selector sel into steps, returning an error for malformed selectors.
func parseSelector(sel string) ([]step, error) {
	if !strings.HasPrefix(sel, "/") {
		sel = "//" + sel
	}
	var steps []step
	for sel != "" {
		var s step
		switch {
		case strings.HasPrefix(sel, "//"):
			s.deep = true
			sel = sel[2:]
		case strings.HasPrefix(sel, "/"):
			sel = sel[1:]
		default:
			return nil, fmt.Errorf("tree: selector: expected / at %q", sel)
		}
		end := strings.IndexByte(sel, '/')
		if b := strings.IndexByte(sel, '['); b >= 0 && (end < 0 || b < end) {
			// Predicates may contain '/', so find the closing bracket(s).
			end = -1
			depth := 0
			for i := 0; i < len(sel); i++ {
				switch sel[i] {
				case '[':
					depth++
				case ']':
					depth--
				case '/':
					if depth == 0 {
						end = i
					}
				}
				if end >= 0 {
					break
				}
			}
		}
		var part string
		if end < 0 {
			part, sel = sel, ""
		} else {
			part, sel = sel[:end], sel[end:]
		}
		name := part
		if b := strings.IndexByte(part, '['); b >= 0 {
			name = part[:b]
			preds := part[b:]
			for preds != "" {
				if preds[0] != '[' {
					return nil, fmt.Errorf("tree: selector: bad predicate %q", preds)
				}
				end := strings.IndexByte(preds, ']')
				if end < 0 {
					return nil, fmt.Errorf("tree: selector: unclosed predicate %q", preds)
				}
				body := preds[1:end]
				preds = preds[end+1:]
				if strings.HasPrefix(body, "@") {
					s.hasAttr = true
					body = body[1:]
					if eq := strings.IndexByte(body, '='); eq >= 0 {
						s.attr = body[:eq]
						s.value = strings.Trim(body[eq+1:], `'"`)
					} else {
						s.attr = body
					}
					continue
				}
				n, err := strconv.Atoi(body)
				if err != nil || n < 1 {
					return nil, fmt.Errorf("tree: selector: bad index %q", body)
				}
				s.index = n
			}
		}
		if name != "*" && !xmlscan.ValidQName(name) {
			return nil, fmt.Errorf("tree: selector: bad name %q", name)
		}
		s.name = name
		steps = append(steps, s)
	}
	if len(steps) == 0 {
		return nil, fmt.Errorf("tree: empty selector")
	}
	return steps, nil
}
