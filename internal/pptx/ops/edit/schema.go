package edit

import (
	"errors"
	"fmt"
	"slices"
	"strings"

	core "github.com/jhnoel/documentdriver/internal/ooxml/edit"
	"github.com/jhnoel/documentdriver/internal/ooxml/schema"
	"github.com/jhnoel/documentdriver/internal/ooxml/tree"
	"github.com/jhnoel/documentdriver/internal/pptx/derive"
)

// Errors from Ensure and the placed inserts (the edit core's).
var (
	// ErrUnknownParent: the parent's complex type is not tabulated, so
	// where a child goes is not known.
	ErrUnknownParent = core.ErrUnknownParent
	// ErrNotAChild: the schema does not allow the child in the parent.
	ErrNotAChild = core.ErrNotAChild
	// ErrRepeated: the child's position repeats, so Ensure cannot say
	// which occurrence is meant.
	ErrRepeated = core.ErrRepeated
	// ErrAmbiguousPlace: the parent holds elements the table does not
	// know and none of its known children fixes the position.
	ErrAmbiguousPlace = core.ErrAmbiguousPlace
)

// objectElems are the object elements whose paths start a type walk;
// each one's complex type is the type the schema gives it as a child of
// a group (p:CT_GroupShape).
var objectElems = map[string]bool{"sp": true, "cxnSp": true, "pic": true, "grpSp": true}

// objectType returns the complex type of an object element (p:sp is
// p:CT_Shape): the edit core's Format.Type for slides.
func objectType(obj *tree.Node) (string, bool) {
	if !derive.In(obj, derive.PML) || !objectElems[obj.Local()] {
		return "", false
	}
	return schema.ChildType("p:CT_GroupShape", "p:"+obj.Local())
}

// groupType is the complex type of a shape tree or group, whose objects
// fill one repeated slot.
const groupType = "p:CT_GroupShape"

// Anchor says where an object goes among the objects of a container: at
// the back (first in paint order), at the front (last), or before or
// after another object of the same container.
type Anchor struct {
	kind int
	ref  Ref
}

const (
	anchorFront = iota
	anchorBack
	anchorBefore
	anchorAfter
	anchorInFront
	anchorInBack
)

// InFront is the front of group g's children: painted last within the
// group. It places a new object inside a group.
func InFront(g Ref) Anchor { return Anchor{kind: anchorInFront, ref: g} }

// InBack is the back of group g's children: painted first within the
// group.
func InBack(g Ref) Anchor { return Anchor{kind: anchorInBack, ref: g} }

// Front is the end of the container's objects: painted last, on top. For
// a new object, the container is the slide's shape tree.
func Front() Anchor { return Anchor{kind: anchorFront} }

// Back is the start of the container's objects: painted first.
func Back() Anchor { return Anchor{kind: anchorBack} }

// Before is immediately before (behind) the object r, in r's container.
func Before(r Ref) Anchor { return Anchor{kind: anchorBefore, ref: r} }

// After is immediately after (in front of) the object r, in r's
// container.
func After(r Ref) Anchor { return Anchor{kind: anchorAfter, ref: r} }

// unitOf returns the element that stands for an object in its container:
// the object itself, or the object-level mc:AlternateContent around it
// when that holds only this object in each branch. Other objects in
// mc:AlternateContent have no unit of their own and cannot be placed,
// removed or copied on their own.
func unitOf(e *tree.Node) (*tree.Node, error) {
	branch, ac := enclosingBranch(e)
	if branch == nil {
		return e, nil
	}
	if outer, _ := enclosingBranch(ac); outer != nil {
		return nil, fmt.Errorf("%w: nested mc:AlternateContent is not supported for writes", ErrBranch)
	}
	if e.Parent() != branch {
		return nil, fmt.Errorf("%w: the object is inside a group in mc:AlternateContent", ErrBranch)
	}
	for _, b := range ac.Elements() {
		if !isBranch(b) {
			continue
		}
		n := 0
		for _, c := range b.Elements() {
			if isObject(c) {
				n++
			}
		}
		if n != 1 {
			return nil, fmt.Errorf("%w: the mc:AlternateContent around the object holds other objects", ErrBranch)
		}
	}
	return ac, nil
}

// place returns where an anchor puts a unit. def is the container for
// Front and Back; except is a unit being moved, skipped as a neighbour.
func (w *Workspace) place(part string, def *tree.Node, at Anchor, except *tree.Node) (core.Position, error) {
	if at.kind == anchorFront || at.kind == anchorBack {
		return w.containerEnd(def, at.kind == anchorFront, except)
	}
	if at.ref.part != part {
		return core.Position{}, fmt.Errorf("edit: anchor %s is on another slide", at.ref)
	}
	if at.kind == anchorInFront || at.kind == anchorInBack {
		g, err := w.find(at.ref.part, at.ref.id, at.ref.ord)
		if err != nil {
			return core.Position{}, err
		}
		if !isGroup(g) {
			return core.Position{}, fmt.Errorf("edit: %s is not a group", at.ref)
		}
		return w.containerEnd(g, at.kind == anchorInFront, except)
	}
	e, err := w.find(at.ref.part, at.ref.id, at.ref.ord)
	if err != nil {
		return core.Position{}, err
	}
	u, err := unitOf(e)
	if err != nil {
		return core.Position{}, fmt.Errorf("anchor %s: %w", at.ref, err)
	}
	if u == except {
		return core.Position{}, fmt.Errorf("edit: %s cannot be placed relative to itself", at.ref)
	}
	if at.kind == anchorBefore {
		return core.Before(u), nil
	}
	return core.After(u, except), nil
}

// containerEnd returns the insertion point at the front (after the last
// object, before p:extLst) or the back (after p:grpSpPr, before the first
// object) of a shape tree or group, by the schema table of
// CT_GroupShape. Object-level mc:AlternateContent counts as an object.
func (w *Workspace) containerEnd(c *tree.Node, front bool, except *tree.Node) (core.Position, error) {
	return w.core.SlotEnd(c, groupType, P("sp"), front, except, isAC)
}

// checkContainer fails unless c is a shape tree or group outside
// mc:AlternateContent: groups inside one are refused, since the same
// change would be needed in every branch.
func checkContainer(c *tree.Node) error {
	if !derive.Is(c, derive.PML, "spTree") && !isGroup(c) {
		return fmt.Errorf("edit: objects can be placed only in a shape tree or group, not in %s", c.Name())
	}
	if b, _ := enclosingBranch(c); b != nil {
		return fmt.Errorf("%w: placing objects in a group inside mc:AlternateContent", ErrBranch)
	}
	return nil
}

// InsertObject parses frag, a single p:sp, p:cxnSp, p:pic or p:grpSp
// element whose prefixes ns resolves (as in tree.ParseFragment), gives it
// a new id from NextID and inserts it at the anchor (Front and Back are
// the slide's shape tree). Namespace URIs in ns from a transitional or
// strict family take the family member in scope. It declares the
// container and returns the new object's address.
func (w *Workspace) InsertObject(part string, frag []byte, ns map[string]string, at Anchor) (Ref, error) {
	_, st, err := w.spTree(part)
	if err != nil {
		return Ref{}, err
	}
	pos, err := w.place(part, st, at, nil)
	if err != nil {
		return Ref{}, err
	}
	nodes, err := w.core.Parse(pos.Container(), frag, ns)
	if err != nil {
		return Ref{}, err
	}
	// A detached fragment resolves its prefixes only once inserted; check
	// the name through the namespace map it was parsed with.
	if len(nodes) != 1 || nodes[0].Kind() != tree.ElementNode || !objectKinds[nodes[0].Local()] ||
		nodes[0].Local() == "contentPart" || !slices.Contains(derive.PML, ns[nodes[0].Prefix()]) {
		return Ref{}, errors.New("edit: fragment is not a single p:sp, p:cxnSp, p:pic or p:grpSp element")
	}
	u := nodes[0]
	cnv := cNvPr(u)
	if cnv == nil {
		return Ref{}, errors.New("edit: fragment has no cNvPr")
	}
	id, err := w.NextID(part)
	if err != nil {
		return Ref{}, err
	}
	if err := checkContainer(pos.Container()); err != nil {
		return Ref{}, err
	}
	if err := w.core.Insert(part, pos, u, []core.NodeAttr{{Node: cnv, Name: "id", Value: id}}); err != nil {
		return Ref{}, err
	}
	if !isObject(u) {
		return Ref{}, errors.New("edit: inserted fragment is not a PresentationML object")
	}
	return Ref{part: part, id: id, ord: 1}, nil
}

// cNvPr returns an object element's cNvPr.
func cNvPr(e *tree.Node) *tree.Node {
	for _, c := range e.Elements() {
		if strings.HasPrefix(c.Local(), "nv") {
			for _, cc := range c.Elements() {
				if cc.Local() == "cNvPr" {
					return cc
				}
			}
		}
	}
	return nil
}

// MoveObject moves the object at ref (with its own mc:AlternateContent,
// if any) to the anchor within its own container: paint order only,
// never into another group (Front and Back are the ends of its own
// container). The object's bytes do not change; the container is
// declared. Moving an object to where it is changes nothing.
func (w *Workspace) MoveObject(ref Ref, at Anchor) error {
	e, err := w.find(ref.part, ref.id, ref.ord)
	if err != nil {
		return err
	}
	u, err := unitOf(e)
	if err != nil {
		return fmt.Errorf("%s: %w", ref, err)
	}
	cur := u.Parent()
	pos, err := w.place(ref.part, cur, at, u)
	if err != nil {
		return err
	}
	if pos.Container() != cur {
		return fmt.Errorf("edit: %s and its anchor are in different containers; reorder moves only within one", ref)
	}
	if pos.Holds(u) {
		return nil
	}
	if err := checkContainer(cur); err != nil {
		return err
	}
	return w.core.Move(ref.part, u, pos)
}

// RemoveObject removes the object at ref (with its own
// mc:AlternateContent, if any), declaring it as a target and its
// container. Objects elsewhere that refer to it (glue) are not changed.
func (w *Workspace) RemoveObject(ref Ref) error {
	e, err := w.find(ref.part, ref.id, ref.ord)
	if err != nil {
		return err
	}
	u, err := unitOf(e)
	if err != nil {
		return fmt.Errorf("%s: %w", ref, err)
	}
	if err := checkContainer(u.Parent()); err != nil {
		return err
	}
	return w.core.Delete(ref.part, u)
}

// DuplicateObject copies the object at ref (with its own
// mc:AlternateContent, if any) and inserts the copy immediately after
// it, in the same container. Every object id in the copy gets a new id
// from NextID, one per distinct original id (so mc branches stay
// paired); glue between objects inside the copy follows the new ids and
// glue to objects outside it is kept; each a16:creationId gets a new GUID
// from newGUID (one per distinct original); every other extension and
// relationship id is copied as it is, so pictures share their media. It
// returns the copy's address.
func (w *Workspace) DuplicateObject(ref Ref, newGUID func() string) (Ref, error) {
	e, err := w.find(ref.part, ref.id, ref.ord)
	if err != nil {
		return Ref{}, err
	}
	u, err := unitOf(e)
	if err != nil {
		return Ref{}, fmt.Errorf("%s: %w", ref, err)
	}
	cp, err := w.core.Copy(u)
	if err != nil {
		return Ref{}, err
	}
	ids := map[string]string{}
	newID := ""
	// The copy has the original's structure; namespaces are classified on
	// the original, where they resolve (the detached copy resolves its
	// prefixes only once inserted).
	elems := func(r *tree.Node) []*tree.Node {
		var out []*tree.Node
		r.Walk(func(n *tree.Node) bool {
			if n.Kind() == tree.ElementNode {
				out = append(out, n)
			}
			return true
		})
		return out
	}
	orig, copies := elems(u), elems(cp)
	if len(orig) != len(copies) {
		return Ref{}, errors.New("edit: copy does not match the original")
	}
	var cnvs, glue, cids []*tree.Node
	for k, n := range orig {
		switch {
		case n.Local() == "cNvPr" && n.Parent() != nil && strings.HasPrefix(n.Parent().Local(), "nv"):
			cnvs = append(cnvs, copies[k])
		case (n.Local() == "stCxn" || n.Local() == "endCxn") && derive.In(n, derive.DML):
			glue = append(glue, copies[k])
		case n.Local() == "creationId" && n.Space() == nsA16:
			cids = append(cids, copies[k])
		}
	}
	var set []core.NodeAttr
	for _, n := range cnvs {
		old, _ := n.Attr("id")
		nid, ok := ids[old]
		if !ok {
			if nid, err = w.NextID(ref.part); err != nil {
				return Ref{}, err
			}
			ids[old] = nid
		}
		if newID == "" {
			newID = nid
		}
		set = append(set, core.NodeAttr{Node: n, Name: "id", Value: nid})
	}
	for _, n := range glue {
		if old, _ := n.Attr("id"); ids[old] != "" {
			set = append(set, core.NodeAttr{Node: n, Name: "id", Value: ids[old]})
		}
	}
	guids := map[string]string{}
	for _, n := range cids {
		old, _ := n.Attr("id")
		g, ok := guids[old]
		if !ok {
			g = newGUID()
			guids[old] = g
		}
		set = append(set, core.NodeAttr{Node: n, Name: "id", Value: g})
	}
	if newID == "" {
		return Ref{}, errors.New("edit: object has no cNvPr")
	}
	// Immediately after the original, before any whitespace that follows
	// it, so that deleting the original leaves the copy exactly where the
	// original was.
	if err := checkContainer(u.Parent()); err != nil {
		return Ref{}, err
	}
	if err := w.core.Insert(ref.part, core.JustAfter(u), cp, set); err != nil {
		return Ref{}, err
	}
	return Ref{part: ref.part, id: newID, ord: 1}, nil
}

// nsA16 is the Office 2014 drawing namespace of a16:creationId.
const nsA16 = "http://schemas.microsoft.com/office/drawing/2014/main"

// ChildOrder returns the children the schema allows in complex type typ
// (such as "a:CT_TextParagraphProperties"), slot by slot in schema order,
// each slot the names of a choice as prefix:local ("a:buChar"), and
// whether typ is tabulated. It is schema.ChildOrder, a fresh copy of the
// generated table, so that code outside the edit layer that orders
// children (the text builder) reads the one table Ensure uses instead of
// keeping its own.
func ChildOrder(typ string) ([][]string, bool) { return schema.ChildOrder(typ) }

// ReplaceParagraphs replaces the paragraphs of the object's text body:
// in each branch i with frags[i] not nil, the children of p:txBody from
// its first a:p to its last inclusive (whatever lies between them
// included) become the nodes parsed from frags[i]. A fragment is a
// sequence of a:p elements, with only whitespace, comments or processing
// instructions between them; its prefixes are those in scope at the
// p:txBody or declared on the elements themselves. When the p:txBody has
// no a:p, the nodes go after a:lstStyle, else after a:bodyPr. a:bodyPr,
// a:lstStyle and anything after the last a:p are never touched; a nil
// frags[i] leaves branch i as it is.
//
// It is the edit core's ReplaceRun of a:p under p:txBody (whose schema
// order puts the paragraphs after a:bodyPr and a:lstStyle), after
// checking that every listed branch has a p:txBody holding an a:bodyPr.
// Like the other Handle mutators it changes only the object Target
// declared, and nothing unless every listed branch can change; each
// fragment must parse, with the prefixes in scope there, as one or more
// a:p elements (the schema requires at least one).
func (h *Handle) ReplaceParagraphs(frags [][]byte) error {
	if len(frags) != h.Branches() {
		return fmt.Errorf("%w: %d fragments for %d branches", ErrBranch, len(frags), h.Branches())
	}
	body := Path{P("txBody")}
	for i, frag := range frags {
		if frag == nil {
			continue
		}
		if h.FindIn(i, body) == nil {
			return fmt.Errorf("%w: %s has no p:txBody in branch %d", ErrBranch, h.ref, i)
		}
		if h.FindIn(i, Path{P("txBody"), A("bodyPr")}) == nil {
			return fmt.Errorf("%w: %s has no a:bodyPr in its p:txBody in branch %d", ErrBranch, h.ref, i)
		}
	}
	return h.ReplaceRun(body, A("p"), frags)
}

// GroupObjects makes the objects at refs, all children of one container
// (the slide's shape tree or a group), the children of a new group: frag,
// a single p:grpSp whose prefixes ns resolves (as for InsertObject), gets
// a new id from NextID and goes where the frontmost of the objects was,
// in the same container; the objects move into it in their paint order,
// each with its own mc:AlternateContent if any, their bytes unchanged.
// The container is declared; the new group is new, so its content is the
// plan's. It returns the group's address. Objects inside
// mc:AlternateContent with other objects, and containers inside
// mc:AlternateContent, are refused (ErrBranch).
func (w *Workspace) GroupObjects(part string, refs []Ref, frag []byte, ns map[string]string) (Ref, error) {
	if len(refs) == 0 {
		return Ref{}, errors.New("edit: no objects to group")
	}
	var units []*tree.Node
	var container *tree.Node
	seen := map[*tree.Node]bool{}
	for _, r := range refs {
		if r.part != part {
			return Ref{}, fmt.Errorf("edit: %s is on another slide", r)
		}
		e, err := w.find(r.part, r.id, r.ord)
		if err != nil {
			return Ref{}, err
		}
		u, err := unitOf(e)
		if err != nil {
			return Ref{}, fmt.Errorf("%s: %w", r, err)
		}
		if seen[u] {
			return Ref{}, fmt.Errorf("edit: %s is named twice", r)
		}
		seen[u] = true
		switch {
		case container == nil:
			container = u.Parent()
		case u.Parent() != container:
			return Ref{}, fmt.Errorf("edit: %s is not in the same container as %s; group objects of one container (the slide or one group)", r, refs[0])
		}
		units = append(units, u)
	}
	// Paint order: document order within the container.
	pos := map[*tree.Node]int{}
	for k, c := range container.Children() {
		pos[c] = k
	}
	slices.SortFunc(units, func(a, b *tree.Node) int { return pos[a] - pos[b] })
	nodes, err := w.core.Parse(container, frag, ns)
	if err != nil {
		return Ref{}, err
	}
	if len(nodes) != 1 || nodes[0].Kind() != tree.ElementNode || nodes[0].Local() != "grpSp" || !slices.Contains(derive.PML, ns[nodes[0].Prefix()]) {
		return Ref{}, errors.New("edit: fragment is not a single p:grpSp element")
	}
	g := nodes[0]
	cnv := cNvPr(g)
	if cnv == nil {
		return Ref{}, errors.New("edit: fragment has no cNvPr")
	}
	if err := checkContainer(container); err != nil {
		return Ref{}, err
	}
	id, err := w.NextID(part)
	if err != nil {
		return Ref{}, err
	}
	if err := w.core.Insert(part, core.JustAfter(units[len(units)-1]), g, []core.NodeAttr{{Node: cnv, Name: "id", Value: id}}); err != nil {
		return Ref{}, err
	}
	for _, u := range units {
		at, err := w.containerEnd(g, true, nil)
		if err != nil {
			return Ref{}, err
		}
		if err := w.core.Move(part, u, at); err != nil {
			return Ref{}, err
		}
	}
	return Ref{part: part, id: id, ord: 1}, nil
}

// Ungroup moves the children of the group at ref (each object with its
// own mc:AlternateContent, if any, in paint order, bytes unchanged) into
// the group's container where the group was, and removes the group. The
// group is declared as a target (it is removed) and its container as a
// container. Children whose transforms change must be targeted by the
// caller, before or after. A group inside mc:AlternateContent is refused
// (ErrBranch).
func (w *Workspace) Ungroup(ref Ref) error {
	e, err := w.find(ref.part, ref.id, ref.ord)
	if err != nil {
		return err
	}
	if !isGroup(e) {
		return fmt.Errorf("edit: %s is not a group", ref)
	}
	if b, _ := enclosingBranch(e); b != nil {
		return fmt.Errorf("%w: ungrouping a group inside mc:AlternateContent", ErrBranch)
	}
	if err := checkContainer(e.Parent()); err != nil {
		return err
	}
	var kids []*tree.Node
	for _, c := range e.Elements() {
		if isObject(c) || isAC(c) {
			kids = append(kids, c)
		}
	}
	for _, c := range kids {
		if err := w.core.Move(ref.part, c, core.Before(e)); err != nil {
			return err
		}
	}
	return w.core.Delete(ref.part, e)
}
