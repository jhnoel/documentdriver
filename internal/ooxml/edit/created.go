package edit

import (
	"github.com/jhnoel/documentdriver/internal/ooxml/tree"
	"github.com/jhnoel/documentdriver/internal/plan"
)

// Created-node recording. Every element the core makes new (Ensure, the
// placed inserts, Insert of a parsed or copied unit, and the structural
// verbs SplitAt, SplitText and Wrap) is recorded with its part, so that
// operations can tell what this plan created from what the document
// already held.
//
// The record serves two purposes only: write-set verification (a new
// element needs no declaration of its own) and id assignment (a created
// paragraph gets a fresh w14:paraId, a created revision a fresh w:id). It
// must never decide which revisions to accept or reject: isolating a span
// splits runs and wrappers, so a created element can carry an existing
// revision that belongs to another author, and a Reviser that settled
// "what this plan created" would accept that foreign revision too.
// Revisions are told apart by their own ids and authors, not by this
// record.

// record notes nodes as created in part, elements only.
func (w *Workspace) record(part string, nodes ...*tree.Node) {
	k := plan.PartKey(part)
	for _, n := range nodes {
		if n == nil || n.Kind() != tree.ElementNode {
			continue
		}
		if _, ok := w.created[n]; !ok {
			if w.seq == nil {
				w.seq = map[*tree.Node]int{}
			}
			w.seq[n] = len(w.order)
			w.order = append(w.order, n)
		}
		w.created[n] = k
	}
}

// Created reports whether element n was created by this workspace: n or
// one of its ancestors was inserted new by the core. An element that
// existed when the plan started reports false even after it was moved,
// renamed or had children added. See the type's notes for what the
// answer may be used for: write-set verification and id assignment,
// never choosing which revisions to accept.
func (w *Workspace) Created(n *tree.Node) bool {
	for p := n; p != nil; p = p.Parent() {
		if _, ok := w.created[p]; ok {
			return true
		}
	}
	return false
}

// CreatedIn returns the elements the core created in part that are still
// in its tree, outermost only (an element whose ancestor is also listed is
// left out), in creation order. Elements created and later removed are
// left out.
func (w *Workspace) CreatedIn(part string) ([]*tree.Node, error) {
	doc, err := w.Tree(part)
	if err != nil {
		return nil, err
	}
	k := plan.PartKey(part)
	var out []*tree.Node
	for _, n := range w.order {
		if w.created[n] != k || !attached(doc, n) {
			continue
		}
		outer := true
		for p := n.Parent(); p != nil; p = p.Parent() {
			if _, ok := w.created[p]; ok {
				outer = false
				break
			}
		}
		if outer {
			out = append(out, n)
		}
	}
	return out, nil
}

// attached reports whether n is in doc's tree.
func attached(doc *tree.Document, n *tree.Node) bool {
	p := n
	for p.Parent() != nil {
		p = p.Parent()
	}
	return p == doc.Node()
}

// Created reports whether element n was created by this plan (see
// Workspace.Created).
func (h *Handle) Created(n *tree.Node) bool { return h.w.Created(n) }

// Copy provenance. An element the core made as a copy of another (the
// new half of SplitAt and SplitText takes the split element's start tag;
// Copy reparses an element's bytes) records the element it was made
// from, and each descendant of a Copy its counterpart. The integrity gate
// reads it: a copy carries the faults its source had, while an element
// parsed or placed fresh never does, however much it resembles one.

// noteCopy records that element c was made from element src and, when
// deep and their subtrees have the same elements by name in document
// order, each descendant of c from its counterpart in src.
func (w *Workspace) noteCopy(c, src *tree.Node, deep bool) {
	if c == nil || src == nil || c.Kind() != tree.ElementNode || src.Kind() != tree.ElementNode {
		return
	}
	if w.source == nil {
		w.source = map[*tree.Node]*tree.Node{}
	}
	if !deep {
		w.source[c] = src
		return
	}
	a, b := elementsOf(c), elementsOf(src)
	if len(a) != len(b) {
		w.source[c] = src
		return
	}
	for i := range a {
		if a[i].Name() != b[i].Name() {
			w.source[c] = src
			return
		}
	}
	for i := range a {
		w.source[a[i]] = b[i]
	}
}

// elementsOf returns n and its descendant elements in document order.
func elementsOf(n *tree.Node) []*tree.Node {
	var out []*tree.Node
	n.Walk(func(e *tree.Node) bool {
		if e.Kind() != tree.ElementNode {
			return false
		}
		out = append(out, e)
		return true
	})
	return out
}

// RecordCopy records that element c, built by a format layer from the
// bytes of element src (a property snapshot, rebuilt properties), is a
// copy of it, and each descendant of c a copy of its counterpart when
// the two subtrees have the same elements. Use it only for a true copy:
// the gate lets a copy keep its source's faults.
func (w *Workspace) RecordCopy(c, src *tree.Node) { w.noteCopy(c, src, true) }

// RecordCopies records that the element children of parent, built from
// the bytes of the elements from in order (properties rebuilt without
// some of their children), are copies of them, each with its
// descendants. It records nothing when the counts differ.
func (w *Workspace) RecordCopies(parent *tree.Node, from []*tree.Node) {
	kids := parent.Elements()
	if len(kids) != len(from) {
		return
	}
	for i := range kids {
		w.noteCopy(kids[i], from[i], true)
	}
}

// SourceOf returns the element n was copied or split from, or nil when n
// was not made as a copy (it existed in the input, or was parsed or
// placed fresh). The source may itself be a copy; follow the chain to an
// element of the input.
func (w *Workspace) SourceOf(n *tree.Node) *tree.Node { return w.source[n] }
