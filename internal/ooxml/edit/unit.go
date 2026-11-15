package edit

import (
	"errors"
	"fmt"

	"github.com/jhnoel/documentdriver/internal/ooxml/schema"
	"github.com/jhnoel/documentdriver/internal/ooxml/tree"
)

// Units are the elements a container holds in a repeated slot: the
// objects of a shape tree or group, the paragraphs and tables of a
// document body. A format inserts, moves and deletes them whole through
// the workspace, at a Position, and the workspace declares the
// containers (and, for a deletion, the unit) in the write set.

// Position is where Insert or Move puts a unit: in a container, before an
// element (or at the end), or immediately after a sibling. Positions are
// made only by SlotEnd, which reads the schema's child order, and by
// Before, After and JustAfter, which are next to an existing element.
type Position struct {
	container, before, after *tree.Node
}

// Container returns the element the position is in.
func (p Position) Container() *tree.Node { return p.container }

// Holds reports whether u already stands at p: inserting it there would
// leave it where it is.
func (p Position) Holds(u *tree.Node) bool {
	if p.after != nil {
		return p.after == u || next(p.after) == u
	}
	return u.Parent() == p.container && next(u) == p.before
}

// next returns the element after e among its parent's elements, or nil.
// It follows the tree's sibling links: time proportional to the nodes
// it skips, no allocation.
func next(e *tree.Node) *tree.Node {
	for s := e.NextSibling(); s != nil; s = s.NextSibling() {
		if s.Kind() == tree.ElementNode {
			return s
		}
	}
	return nil
}

// Before is immediately before element e, in e's parent.
func Before(e *tree.Node) Position { return Position{container: e.Parent(), before: e} }

// After is after element e in e's parent: before the element that
// follows it (skipping skip, a unit being moved), or at the end.
// Whitespace after e stays with e.
func After(e, skip *tree.Node) Position {
	n := next(e)
	if n != nil && n == skip {
		n = next(n)
	}
	return Position{container: e.Parent(), before: n}
}

// JustAfter is immediately after element e, before anything that
// follows it (whitespace included), so that deleting e later leaves the
// new unit exactly where e was.
func JustAfter(e *tree.Node) Position { return Position{container: e.Parent(), after: e} }

// Into is at the end of container c: right after its last element, or,
// when c holds no element, as its only element content. It is for lists
// in vocabularies the schema tables do not cover whose content is one
// repeated element (w15:commentsEx, w16cid:commentsIds); a tabulated
// container is placed with SlotEnd.
func Into(c *tree.Node) Position {
	els := c.Elements()
	if len(els) == 0 {
		return Position{container: c}
	}
	return After(els[len(els)-1], nil)
}

// SlotEnd returns the front (after the last member, before any later
// slot) or the back (after any earlier slot, before the first member) of
// the repeated slot of member in container c, whose complex type is typ,
// by the schema table. Elements for which also reports true count as
// members (an object-level mc:AlternateContent); except, a unit being
// moved, is skipped. Elements the table does not know never stop it:
// with no known element to place against, the position is before the
// first element. It fails when typ is not tabulated or member is not in
// it.
func (w *Workspace) SlotEnd(c *tree.Node, typ string, member Name, front bool, except *tree.Node, also func(*tree.Node) bool) (Position, error) {
	slots, ok := schema.Slots(typ)
	if !ok {
		return Position{}, fmt.Errorf("%w: %s", ErrUnknownParent, typ)
	}
	slot, _ := schema.Find(slots, member.ns.Table, member.local)
	if slot < 0 {
		return Position{}, fmt.Errorf("%w: %s in %s", ErrNotAChild, member, typ)
	}
	var firstMember, firstLater, lastKnown, firstEl *tree.Node
	for e := range c.ElementsSeq() {
		if e == except {
			continue
		}
		if firstEl == nil {
			firstEl = e
		}
		k := w.slotOf(slots, e)
		if also != nil && also(e) {
			k = slot
		}
		switch {
		case k < 0:
			// Unknown elements (extensions) never stop a placement; they
			// stay attached to the element before them.
		case k == slot && firstMember == nil:
			firstMember = e
		case k > slot && firstLater == nil:
			firstLater = e
		}
		if k >= 0 && k <= slot && firstLater == nil {
			lastKnown = e
		}
	}
	if !front && firstMember != nil {
		return Position{container: c, before: firstMember}, nil
	}
	switch {
	case firstLater != nil:
		return Position{container: c, before: firstLater}, nil
	case lastKnown != nil || firstEl == nil:
		return Position{container: c}, nil
	}
	// Only unknown elements: first, ahead of them.
	return Position{container: c, before: firstEl}, nil
}

// Parse parses frag, a fragment to insert at or under element at, with
// the prefixes ns resolves (as in tree.ParseFragment) mapped to the
// vocabulary's family members in scope at at (Vocab.Remap). The nodes are
// detached: Insert places them.
//
// Prefixes the fragment uses that ns does not name resolve as they are in
// scope at at, so the detached nodes already know their namespaces
// (Space) before Insert or PlaceNew places them.
func (w *Workspace) Parse(at *tree.Node, frag []byte, ns map[string]string) ([]*tree.Node, error) {
	all := inScope(at)
	for p, uri := range w.f.Vocab.Remap(at, ns) {
		all[p] = uri
	}
	return tree.ParseFragment(frag, all)
}

// Copy returns a detached copy of element u, parsed from its bytes with
// every namespace binding in scope at u, for Insert.
func (w *Workspace) Copy(u *tree.Node) (*tree.Node, error) {
	nodes, err := tree.ParseFragment(u.Bytes(), inScope(u))
	if err != nil {
		return nil, err
	}
	if len(nodes) != 1 {
		return nil, errors.New("edit: copy did not parse as one element")
	}
	w.noteCopy(nodes[0], u, true)
	return nodes[0], nil
}

// NodeAttr is an attribute to set on an element of a detached unit
// before Insert places it (a new id).
type NodeAttr struct {
	Node        *tree.Node
	Name, Value string
}

// Insert sets the attributes set on elements of the detached unit n (n
// itself or its descendants), then inserts n at p, declaring p's
// container in part as a container of the plan. It fails, changing
// nothing, when n is attached or an attribute's element is outside n.
func (w *Workspace) Insert(part string, p Position, n *tree.Node, set []NodeAttr) error {
	if n.Parent() != nil {
		return errors.New("edit: Insert takes a detached unit")
	}
	for _, a := range set {
		x := a.Node
		for x != nil && x != n {
			x = x.Parent()
		}
		if x == nil {
			return fmt.Errorf("edit: attribute %s is for an element outside the unit", a.Name)
		}
	}
	for _, a := range set {
		if err := a.Node.SetAttr(a.Name, a.Value); err != nil {
			return err
		}
	}
	w.ws.DeclareContainer(part, p.container)
	if err := put(p, n); err != nil {
		return err
	}
	w.record(part, n)
	return nil
}

// put inserts the detached unit n at p.
func put(p Position, n *tree.Node) error {
	switch {
	case p.after != nil:
		return p.container.InsertAfter(n, p.after)
	case p.before != nil:
		return p.container.InsertBefore(n, p.before)
	}
	return p.container.AppendChild(n)
}

// Move moves unit u of part, bytes unchanged, to p, declaring its
// container and p's as containers of the plan. Moving a unit to where it
// stands changes nothing and declares nothing.
func (w *Workspace) Move(part string, u *tree.Node, p Position) error {
	if p.Holds(u) {
		return nil
	}
	if p.before == u || p.after == u {
		return errors.New("edit: a unit cannot be placed relative to itself")
	}
	from := u.Parent()
	if from == nil {
		return errors.New("edit: Move takes an attached unit; Insert places new ones")
	}
	w.ws.DeclareContainer(part, from)
	w.ws.DeclareContainer(part, p.container)
	if err := from.RemoveChild(u); err != nil {
		return err
	}
	return put(p, u)
}

// Delete removes unit u of part, declaring it as a target of the plan
// and its container as a container.
func (w *Workspace) Delete(part string, u *tree.Node) error {
	from := u.Parent()
	if from == nil {
		return errors.New("edit: Delete takes an attached unit")
	}
	w.ws.DeclareContainer(part, from)
	w.ws.DeclareTarget(part, u)
	return from.RemoveChild(u)
}
