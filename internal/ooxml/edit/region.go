package edit

import (
	"errors"
	"fmt"

	"github.com/jhnoel/documentdriver/internal/ooxml/tree"
	"github.com/jhnoel/documentdriver/internal/plan"
)

// Regions. A Handle from Target edits inside one unit; some edits cross
// units (settling a paragraph mark joins two paragraphs, rejecting a row
// insertion removes the row, accepting a cell deletion removes the cell).
// A region is a Handle whose scope is several units at once: a range of
// sibling units, a story, or a whole part. The structural verbs
// (structure.go) work on any element of the region, units included, and
// refuse everything outside it; there is no second verb set.
//
// A region declares its write set when it is opened, from the
// operation's intent (the scope its selector names), never at mutation
// time: the range's container may change its own content and its list of
// units, and each unit in the region (nested ones included) may change
// its content and its own list of units. Verify then holds the part to
// that declaration: every unit outside the region stays byte for byte.
//
// Private returns a region over a private copy of a part that is never
// saved (the HTML projection's settled views): it declares into a write
// set nothing verifies.

// region is a Handle's scope when it is a region.
type region struct {
	// members are the units of a range (and their content); root, for a
	// story or a part, is the element whose content is the region.
	members map[*tree.Node]bool
	root    *tree.Node
	// For a range: its container, and the workspace's created elements
	// when it opened (w.order[:since]). An element created since and
	// placed in the container belongs to the range with its content: a
	// unit the region placed among its members is the region's.
	container *tree.Node
	w         *Workspace
	since     int
}

// owns reports whether e is in the region: a member or inside one, an
// element the range created among its members or inside one, or
// strictly inside the root.
func (r *region) owns(e *tree.Node, label string) error {
	for p := e; p != nil; p = p.Parent() {
		if r.members[p] || (p != e && p == r.root) {
			return nil
		}
		if r.container != nil && p.Parent() == r.container && r.createdSince(p) {
			return nil
		}
	}
	return fmt.Errorf("%w: %s is not in region %s", ErrOutside, e.Name(), label)
}

// createdSince reports whether the workspace created e after the range
// opened.
func (r *region) createdSince(e *tree.Node) bool {
	i, ok := r.w.seq[e]
	return ok && i >= r.since
}

// ErrRegion is returned when a region's bounds are not one container's
// units.
var ErrRegion = errors.New("edit: a region is a run of one container's units")

// declareAll declares unit u and every unit inside it as both target and
// container.
func (w *Workspace) declareAll(part string, u *tree.Node) {
	u.Walk(func(n *tree.Node) bool {
		if n.Kind() == tree.ElementNode && (n == u || w.f.Units.IsUnit(n)) {
			w.ws.DeclareTarget(part, n)
			w.ws.DeclareContainer(part, n)
		}
		return true
	})
}

// Range opens a region over the sibling units first to last (inclusive)
// of one container in part, and whatever lies between them: units may be
// joined, split, removed or rewritten, and new ones placed among them.
// The container is declared a target and a container (its content
// between the units may change, a removed bookmark between two joined
// paragraphs); every unit from first to last, and every unit inside
// them, likewise. label names the region in errors.
func (w *Workspace) Range(part, label string, first, last *tree.Node) (*Handle, error) {
	c := first.Parent()
	if c == nil || last == nil || last.Parent() != c || !w.f.Units.IsUnit(first) || !w.f.Units.IsUnit(last) {
		return nil, ErrRegion
	}
	members := map[*tree.Node]bool{}
	in := false
	for e := range c.ElementsSeq() {
		if e == first {
			in = true
		}
		if in {
			members[e] = true
		}
		if e == last {
			break
		}
	}
	if !in || !members[last] {
		return nil, ErrRegion
	}
	w.ws.DeclareTarget(part, c)
	w.ws.DeclareContainer(part, c)
	for e := range members {
		if w.f.Units.IsUnit(e) {
			w.declareAll(part, e)
		}
	}
	return &Handle{w: w, part: part, label: label, br: []*tree.Node{c}, reg: &region{members: members, container: c, w: w, since: len(w.order)}}, nil
}

// Story opens a region over the content of root, a story's container
// (a body, a header's root, a cell, a comment): everything inside it may
// change. root itself stays in place; it is declared a target and a
// container, as is every unit inside it.
func (w *Workspace) Story(part, label string, root *tree.Node) *Handle {
	w.ws.DeclareTarget(part, root)
	w.ws.DeclareContainer(part, root)
	w.declareAll(part, root)
	return &Handle{w: w, part: part, label: label, br: []*tree.Node{root}, reg: &region{root: root}}
}

// Part opens a region over the whole of part's root element, declaring
// the part as changing as a whole and every unit in it as a target and a
// container.
func (w *Workspace) Part(part string) (*Handle, error) {
	doc, err := w.Tree(part)
	if err != nil {
		return nil, err
	}
	if doc.Root() == nil {
		return nil, fmt.Errorf("edit: %s has no root element", part)
	}
	w.ws.DeclarePart(part)
	return w.Story(part, part, doc.Root()), nil
}

// Private returns a region over the content of root, an element of a
// private copy of a part that is never saved, for a format f. Its
// declarations go to a write set nothing verifies, and what it creates
// is recorded only in it.
func Private(f Format, root *tree.Node) *Handle {
	w := &Workspace{ws: plan.NewWriteSet(), f: &f, created: map[*tree.Node]string{}}
	return &Handle{w: w, part: "private", label: "private copy", br: []*tree.Node{root}, reg: &region{root: root}}
}

// IsRegion reports whether the handle is a region.
func (h *Handle) IsRegion() bool { return h.reg != nil }

// Workspace returns the workspace the handle edits through.
func (h *Handle) Workspace() *Workspace { return h.w }
