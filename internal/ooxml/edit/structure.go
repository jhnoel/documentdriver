package edit

import (
	"bytes"
	"errors"
	"fmt"
	"slices"
	"unicode/utf8"

	"github.com/jhnoel/documentdriver/internal/ooxml/schema"
	"github.com/jhnoel/documentdriver/internal/ooxml/tree"
)

// Structural verbs. The span engine of a format isolates a span of
// content by splitting the elements that straddle its ends, wraps or
// unwraps ranges of siblings (a revision wrapper), renames elements whose
// name carries meaning (w:t and w:delText) and rewrites text. These verbs
// do that inside a handle's own content, so the format's edit layer never
// mutates a tree itself. Each takes elements the caller found by reading
// the handle's branch; each fails, changing nothing, unless the element
// lies strictly inside one of the handle's branch elements and inside no
// element the format bounds below it (another unit). They are format
// agnostic: what is split or wrapped, and which property element travels
// with a split, is the caller's.
//
// New elements (the right half of a split, a wrapper) are recorded as
// created (Workspace.Created).

// ErrOutside is returned when a structural verb is given an element that
// is not inside the handle's own content.
var ErrOutside = errors.New("edit: element is outside the handle's own content")

// owns checks that e lies strictly inside one of the handle's branch
// elements and inside no element the format bounds below it.
func (h *Handle) owns(e *tree.Node) error {
	if e == nil || e.Kind() != tree.ElementNode {
		return fmt.Errorf("%w: not an element", ErrOutside)
	}
	if h.reg != nil {
		return h.reg.owns(e, h.label)
	}
	for p := e.Parent(); p != nil; p = p.Parent() {
		if slices.Contains(h.br, p) {
			return nil
		}
		if h.w.f.Bound != nil && h.w.f.Bound(p) {
			return fmt.Errorf("%w: %s of %s is inside another unit", ErrOutside, e.Name(), h.label)
		}
	}
	return fmt.Errorf("%w: %s is not inside %s", ErrOutside, e.Name(), h.label)
}

// shell returns a detached element with e's name, prefix and attributes
// (namespace declarations included) and no children.
func (w *Workspace) shell(e *tree.Node) (*tree.Node, error) {
	tag := e.StartTag()
	if bytes.HasSuffix(tag, []byte("/>")) {
		tag = append(tag[:len(tag)-2:len(tag)-2], '>')
	}
	frag := append(tag, "</"+e.Name()+">"...)
	nodes, err := tree.ParseFragment(frag, inScope(e))
	if err != nil {
		return nil, fmt.Errorf("edit: copying the start tag of %s: %w", e.Name(), err)
	}
	if len(nodes) != 1 {
		return nil, errors.New("edit: start tag did not parse as one element")
	}
	w.noteCopy(nodes[0], e, false)
	return nodes[0], nil
}

// SplitAt splits element e before its child at (an index into all its
// children, text included): a new element with e's name and attributes
// is placed immediately after e and takes e's children from at on. Each
// child before at for which keep reports true (the property element, such
// as w:rPr of a run) is copied to the front of the new element, so both
// halves keep their properties. It returns the new element. at may be 0
// or the number of children, leaving one half without content.
func (h *Handle) SplitAt(e *tree.Node, at int, keep func(*tree.Node) bool) (*tree.Node, error) {
	if err := h.owns(e); err != nil {
		return nil, err
	}
	kids := e.Children()
	if at < 0 || at > len(kids) {
		return nil, fmt.Errorf("edit: split of %s at %d: out of range", e.Name(), at)
	}
	right, err := h.w.shell(e)
	if err != nil {
		return nil, err
	}
	var props []*tree.Node
	for _, c := range kids[:at] {
		if c.Kind() == tree.ElementNode && keep != nil && keep(c) {
			cp, err := h.w.Copy(c)
			if err != nil {
				return nil, err
			}
			props = append(props, cp)
		}
	}
	moved := slices.Clone(kids[at:])
	// Placed first, so the halves' children resolve their prefixes in the
	// document's scope.
	if err := e.Parent().InsertAfter(right, e); err != nil {
		return nil, err
	}
	for _, p := range props {
		if err := right.AppendChild(p); err != nil {
			return nil, err
		}
	}
	for _, c := range moved {
		if err := e.RemoveChild(c); err != nil {
			return nil, err
		}
		if err := right.AppendChild(c); err != nil {
			return nil, err
		}
	}
	h.w.record(h.part, right)
	return right, nil
}

// SplitText splits a text element (w:t, w:delText, a:t: an element whose
// children are character data only) at byte offset off of its decoded
// text: e keeps the text before off, and a new element with e's name and
// attributes, placed immediately after e, holds the rest. off must fall
// strictly inside the text, on a character boundary. The caller sets
// what each half needs (xml:space) afterwards.
func (h *Handle) SplitText(e *tree.Node, off int) (*tree.Node, error) {
	if err := h.owns(e); err != nil {
		return nil, err
	}
	if e.HasElements() {
		return nil, fmt.Errorf("edit: %s holds elements, not only text", e.Name())
	}
	s := e.Text()
	if off <= 0 || off >= len(s) || !utf8.RuneStart(s[off]) {
		return nil, fmt.Errorf("edit: text split of %s at byte %d of %d: not inside the text on a character boundary", e.Name(), off, len(s))
	}
	right, err := h.w.shell(e)
	if err != nil {
		return nil, err
	}
	if err := right.SetText(s[off:]); err != nil {
		return nil, err
	}
	if err := e.SetText(s[:off]); err != nil {
		return nil, err
	}
	if err := e.Parent().InsertAfter(right, e); err != nil {
		return nil, err
	}
	h.w.record(h.part, right)
	return right, nil
}

// Wrap wraps the siblings from first to last inclusive (and whatever lies
// between them) in a new element named name with the given attributes in
// order, placed where first was, and returns it. first and last must
// share a parent, with first not after last.
func (h *Handle) Wrap(first, last *tree.Node, name Name, attrs [][2]string) (*tree.Node, error) {
	if err := h.owns(first); err != nil {
		return nil, err
	}
	parent := first.Parent()
	if last == nil || last.Parent() != parent {
		return nil, errors.New("edit: wrap: first and last are not siblings")
	}
	kids := parent.Children()
	i, j := slices.Index(kids, first), slices.Index(kids, last)
	if i > j {
		return nil, errors.New("edit: wrap: last comes before first")
	}
	n, err := name.newIn(parent)
	if err != nil {
		return nil, err
	}
	for _, a := range attrs {
		if err := n.SetAttr(a[0], a[1]); err != nil {
			return nil, err
		}
	}
	rng := slices.Clone(kids[i : j+1])
	if err := parent.InsertBefore(n, first); err != nil {
		return nil, err
	}
	for _, c := range rng {
		if err := parent.RemoveChild(c); err != nil {
			return nil, err
		}
		if err := n.AppendChild(c); err != nil {
			return nil, err
		}
	}
	h.w.record(h.part, n)
	return n, nil
}

// Unwrap replaces element e by its children (a revision wrapper whose
// content stays). It fails, changing nothing, when e declares namespaces
// its children might use.
func (h *Handle) Unwrap(e *tree.Node) error {
	if err := h.owns(e); err != nil {
		return err
	}
	for _, a := range e.Attrs() {
		if a.Name == "xmlns" || len(a.Name) > 6 && a.Name[:6] == "xmlns:" {
			return fmt.Errorf("edit: unwrap: %s declares namespaces", e.Name())
		}
	}
	parent := e.Parent()
	for _, c := range slices.Clone(e.Children()) {
		if err := e.RemoveChild(c); err != nil {
			return err
		}
		if err := parent.InsertBefore(c, e); err != nil {
			return err
		}
	}
	return parent.RemoveChild(e)
}

// Rename changes element e's local name within its namespace (w:t to
// w:delText, w:instrText to w:delInstrText), keeping its attributes and
// children.
func (h *Handle) Rename(e *tree.Node, local string) error {
	if err := h.owns(e); err != nil {
		return err
	}
	return e.Rename(local)
}

// SetText replaces the character data of text element e with s. It fails
// when e holds elements.
func (h *Handle) SetText(e *tree.Node, s string) error {
	if err := h.owns(e); err != nil {
		return err
	}
	if e.HasElements() {
		return fmt.Errorf("edit: %s holds elements, not only text", e.Name())
	}
	return e.SetText(s)
}

// SetNodeAttr sets attribute qname to value on element e, which must lie
// inside the handle's own content (an attribute of an element the caller
// found or created, such as xml:space on a split w:t).
func (h *Handle) SetNodeAttr(e *tree.Node, qname, value string) error {
	if err := h.owns(e); err != nil {
		return err
	}
	return e.SetAttr(qname, value)
}

// RemoveNode removes element e from the handle's own content.
func (h *Handle) RemoveNode(e *tree.Node) error {
	if err := h.owns(e); err != nil {
		return err
	}
	return e.Parent().RemoveChild(e)
}

// holds checks that c may take children through the handle: an element
// of the handle's content, a branch element, or a region's root.
func (h *Handle) holds(c *tree.Node) error {
	if slices.Contains(h.br, c) || (h.reg != nil && c == h.reg.root) {
		return nil
	}
	return h.owns(c)
}

// moveTo checks that n is an attached element of the handle's content,
// detaches it, and puts it where put says.
func (h *Handle) moveTo(n *tree.Node, put func() error) error {
	if err := h.owns(n); err != nil {
		return err
	}
	if n.Parent() == nil {
		return errors.New("edit: move takes an attached element; new ones are placed by the schema")
	}
	if err := n.Parent().RemoveChild(n); err != nil {
		return err
	}
	return put()
}

// PutBefore moves element n, already in the handle's content, to
// immediately before ref. New elements are not placed this way: they go
// through Ensure, EnsureIn, PlaceNew or ReplaceWith.
func (h *Handle) PutBefore(n, ref *tree.Node) error {
	if err := h.owns(ref); err != nil {
		return err
	}
	if n == ref {
		return nil
	}
	return h.moveTo(n, func() error { return ref.Parent().InsertBefore(n, ref) })
}

// PutAfter moves element n, already in the handle's content, to
// immediately after ref.
func (h *Handle) PutAfter(n, ref *tree.Node) error {
	if err := h.owns(ref); err != nil {
		return err
	}
	if n == ref {
		return nil
	}
	return h.moveTo(n, func() error { return ref.Parent().InsertAfter(n, ref) })
}

// Append moves element n, already in the handle's content, to the end
// of c's content (content joined onto the end of a paragraph).
func (h *Handle) Append(c, n *tree.Node) error {
	if err := h.holds(c); err != nil {
		return err
	}
	return h.moveTo(n, func() error { return c.AppendChild(n) })
}

// Prepend moves element n, already in the handle's content, to the
// start of c's content.
func (h *Handle) Prepend(c, n *tree.Node) error {
	if err := h.holds(c); err != nil {
		return err
	}
	return h.moveTo(n, func() error { return c.InsertAt(0, n) })
}

// EnsureIn is Ensure on element e of the handle's content (rather than a
// path from the branch element): e gets a child named c where the schema
// places it, or keeps the one it has. It returns that child.
func (h *Handle) EnsureIn(e *tree.Node, c Name) (*tree.Node, error) {
	if err := h.holds(e); err != nil {
		return nil, err
	}
	for x := range e.ElementsSeq() {
		if c.Matches(x) {
			return x, nil
		}
	}
	typ, ok := "", false
	if h.w.f.Type != nil {
		typ, ok = h.w.f.Type(e)
	}
	if !ok {
		return nil, fmt.Errorf("%w: %s", ErrUnknownParent, e.Name())
	}
	slots, _ := schema.Slots(typ)
	k, _ := schema.Find(slots, c.ns.Table, c.local)
	if k < 0 {
		return nil, fmt.Errorf("%w: %s in %s", ErrNotAChild, c, typ)
	}
	n, err := c.newIn(e)
	if err != nil {
		return nil, err
	}
	if err := h.w.place(h.part, e, typ, slots, k, n); err != nil {
		return nil, err
	}
	return n, nil
}

// PlaceNew places the detached element n (from Workspace.Parse or Copy:
// a property snapshot being restored) under element e of the handle's
// content, where the schema puts its name; an existing child of the same
// name, or of the same choice, is replaced. It fails when the schema does
// not allow n under e.
func (h *Handle) PlaceNew(e, n *tree.Node) error {
	if err := h.holds(e); err != nil {
		return err
	}
	if n.Parent() != nil {
		return errors.New("edit: PlaceNew takes a detached element")
	}
	typ, ok := "", false
	if h.w.f.Type != nil {
		typ, ok = h.w.f.Type(e)
	}
	if !ok {
		return fmt.Errorf("%w: %s", ErrUnknownParent, e.Name())
	}
	slots, _ := schema.Slots(typ)
	k, _ := schema.Find(slots, h.w.f.Vocab.table(n), n.Local())
	if k < 0 {
		return fmt.Errorf("%w: %s in %s", ErrNotAChild, n.Name(), typ)
	}
	return h.w.place(h.part, e, typ, slots, k, n)
}

// slotIn returns the complex type of element e (through the format) and
// the slot index of an element named like n (or n itself) in it.
func (h *Handle) slotIn(e, n *tree.Node) (string, []schema.Slot, int, error) {
	typ, ok := "", false
	if h.w.f.Type != nil {
		typ, ok = h.w.f.Type(e)
	}
	if !ok {
		return "", nil, -1, fmt.Errorf("%w: %s", ErrUnknownParent, e.Name())
	}
	slots, _ := schema.Slots(typ)
	k, _ := schema.Find(slots, h.w.f.Vocab.table(n), n.Local())
	if k < 0 {
		return typ, slots, -1, fmt.Errorf("%w: %s in %s", ErrNotAChild, n.Name(), typ)
	}
	return typ, slots, k, nil
}

// fits checks that a child of slot k may stand between the known
// elements nearest before and after a position under parent (prev is the
// element before the position, or nil; next the element after it):
// placing it there introduces no order violation, and a slot that takes
// one element is not taken already. Elements the table does
// not know are skipped; disorder elsewhere among existing children is not
// looked at.
func (h *Handle) fits(name string, slots []schema.Slot, k int, prev, next *tree.Node, skip *tree.Node) error {
	if !slots[k].Multi {
		var pe *tree.Node
		if prev != nil {
			pe = prev.Parent()
		} else if next != nil {
			pe = next.Parent()
		}
		if pe != nil {
			for e := range pe.ChildrenSeq() {
				if e.Kind() == tree.ElementNode && e != skip && h.w.slotOf(slots, e) == k {
					return fmt.Errorf("edit: %s: the parent already has %s, and the schema allows one", name, e.Name())
				}
			}
		}
	}
	els := func(from *tree.Node, step int) *tree.Node {
		if from == nil {
			return nil
		}
		for e := from; e != nil; {
			if e != skip && e.Kind() == tree.ElementNode && h.w.slotOf(slots, e) >= 0 {
				return e
			}
			if step < 0 {
				e = e.PrevSibling()
			} else {
				e = e.NextSibling()
			}
		}
		return nil
	}
	if b := els(prev, -1); b != nil && h.w.slotOf(slots, b) > k {
		return fmt.Errorf("edit: %s would follow %s, which the schema orders after it", name, b.Name())
	}
	if a := els(next, +1); a != nil && h.w.slotOf(slots, a) < k {
		return fmt.Errorf("edit: %s would precede %s, which the schema orders before it", name, a.Name())
	}
	return nil
}

// PlaceAt places the detached element n (from Workspace.Parse or Copy)
// under element e of the handle's content, immediately before before (a
// child of e), or at the end when before is nil: a positional insert into
// a repeated slot whose order the caller chooses (a run among a
// paragraph's runs, revision wrappers, hyperlinks and fields). It fails
// unless the schema allows n under e and the position keeps n after the
// known children the schema orders before it (a paragraph's properties)
// and before those it orders after it.
func (h *Handle) PlaceAt(e, n, before *tree.Node) error {
	if err := h.holds(e); err != nil {
		return err
	}
	if n.Parent() != nil {
		return errors.New("edit: PlaceAt takes a detached element")
	}
	if before != nil && before.Parent() != e {
		return errors.New("edit: PlaceAt: before is not a child of the parent")
	}
	_, slots, k, err := h.slotIn(e, n)
	if err != nil {
		return err
	}
	prev := prevElement(e, before)
	if err := h.fits(n.Name(), slots, k, prev, before, nil); err != nil {
		return err
	}
	if before != nil {
		err = e.InsertBefore(n, before)
	} else {
		err = e.AppendChild(n)
	}
	if err != nil {
		return err
	}
	h.w.record(h.part, n)
	return nil
}

// ReplaceWith puts the detached element n where element old of the
// handle's content is, and removes old: a property element replaced by
// its recorded snapshot, or a unit replaced by another the schema allows
// in the same place (an emptied table becoming a paragraph). n must be an
// element the schema allows under old's parent at old's position (same
// name, another alternative of old's choice, or a slot that fits between
// old's known neighbours); under a parent whose type is not tabulated,
// only an element of old's name. A replaced unit inside a region is a
// declared change; outside one the handle must own old.
func (h *Handle) ReplaceWith(old, n *tree.Node) error {
	if err := h.owns(old); err != nil {
		return err
	}
	if n.Parent() != nil {
		return errors.New("edit: ReplaceWith takes a detached element")
	}
	pe := old.Parent()
	if n.Space() != old.Space() || n.Local() != old.Local() {
		_, slots, k, err := h.slotIn(pe, n)
		if err != nil {
			return err
		}
		sib := pe.Elements()
		i := slices.Index(sib, old)
		var prev, next *tree.Node
		if i > 0 {
			prev = sib[i-1]
		}
		if i+1 < len(sib) {
			next = sib[i+1]
		}
		if err := h.fits(n.Name(), slots, k, prev, next, old); err != nil {
			return err
		}
	}
	if err := pe.ReplaceChild(n, old); err != nil {
		return err
	}
	h.w.record(h.part, n)
	return nil
}

// ReplaceWithNew replaces element old of the handle's content with a new,
// empty element named name, under the same rules as ReplaceWith, and
// returns it.
func (h *Handle) ReplaceWithNew(old *tree.Node, name Name) (*tree.Node, error) {
	if old.Parent() == nil {
		return nil, errors.New("edit: ReplaceWithNew takes an attached element")
	}
	n, err := name.newIn(old.Parent())
	if err != nil {
		return nil, err
	}
	if err := h.ReplaceWith(old, n); err != nil {
		return nil, err
	}
	return n, nil
}

// RemoveNodeAttr removes attribute qname, where present, from element e
// of the handle's content.
func (h *Handle) RemoveNodeAttr(e *tree.Node, qname string) error {
	if err := h.owns(e); err != nil {
		return err
	}
	e.RemoveAttr(qname)
	return nil
}

// prevElement returns the element child of e just before before (a child
// of e), or e's last element child when before is nil; nil when there is
// none. It scans back from the end without building a list of the
// children, so placing near the end of a large body costs nothing per
// sibling.
func prevElement(e, before *tree.Node) *tree.Node {
	var k *tree.Node
	switch {
	case before == nil:
		k = e.LastChild()
	case before.Parent() != e:
		return nil
	default:
		k = before.PrevSibling()
	}
	for ; k != nil; k = k.PrevSibling() {
		if k.Kind() == tree.ElementNode {
			return k
		}
	}
	return nil
}
