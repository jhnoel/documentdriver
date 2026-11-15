package tree

import (
	"iter"

	"github.com/jhnoel/documentdriver/internal/ooxml/xmlscan"
)

// The iterators below walk a node's children and attributes without
// building a slice. They are the allocation-free counterparts of
// Children, Elements and Attrs.
//
// A child iterator may be used to edit the tree: when the loop body
// removes, moves or replaces the child it was just given, the iteration
// goes on with the child that followed it, as long as that one is still
// a child of n; otherwise it stops. Children the body inserts after the
// one it was given are not visited, nor are those it inserts before. A
// child the body moves to the end of n is met again there, so a body
// that moves every child it is given to the end never finishes.

// ChildrenSeq yields n's children in document order, text included.
func (n *Node) ChildrenSeq() iter.Seq[*Node] {
	return func(yield func(*Node) bool) {
		n.eachChild(false, yield)
	}
}

// ElementsSeq yields n's element children in document order.
func (n *Node) ElementsSeq() iter.Seq[*Node] {
	return func(yield func(*Node) bool) {
		n.eachChild(true, yield)
	}
}

// eachChild calls yield with each child of n (each element child when
// elems is set), in document order, until yield returns false.
func (n *Node) eachChild(elems bool, yield func(*Node) bool) {
	for c := n.first; c != nil; {
		next := c.next
		if (!elems || c.kind == ElementNode) && !yield(c) {
			return
		}
		if next != nil && next.parent != n {
			return
		}
		c = next
	}
}

// HasElements reports whether n has an element child. It is
// len(n.Elements()) > 0 without the slice.
func (n *Node) HasElements() bool {
	for c := n.first; c != nil; c = c.next {
		if c.kind == ElementNode {
			return true
		}
	}
	return false
}

// FirstElement returns n's first element child, or nil.
func (n *Node) FirstElement() *Node {
	for c := n.first; c != nil; c = c.next {
		if c.kind == ElementNode {
			return c
		}
	}
	return nil
}

// HasAttrs reports whether element n has an attribute, namespace
// declarations included. It is len(n.Attrs()) > 0 without the slice.
func (n *Node) HasAttrs() bool {
	_, ok := n.firstAttr()
	return ok
}

// AttrsSeq yields element n's attributes in document order, namespace
// declarations included: each qualified name as written and its decoded
// value. n's attributes must not be changed during the iteration.
func (n *Node) AttrsSeq() iter.Seq2[string, string] {
	return func(yield func(string, string) bool) {
		n.eachAttr(func(_ int, a attr) bool { return yield(a.name, xmlscan.DecodeAttr(a.raw)) })
	}
}

// AttrNamesSeq yields the qualified names of element n's attributes in
// document order, namespace declarations included, without decoding a
// value. n's attributes must not be changed during the iteration.
func (n *Node) AttrNamesSeq() iter.Seq[string] {
	return func(yield func(string) bool) {
		n.eachAttr(func(_ int, a attr) bool { return yield(a.name) })
	}
}
