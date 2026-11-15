package edit

import (
	"fmt"
	"slices"

	"github.com/jhnoel/documentdriver/internal/ooxml/ctypes"
	core "github.com/jhnoel/documentdriver/internal/ooxml/edit"
	"github.com/jhnoel/documentdriver/internal/ooxml/tree"
	"github.com/jhnoel/documentdriver/internal/pptx/derive"
)

// ErrBranch is returned when an edit cannot be applied to every
// mc:AlternateContent branch of an object: the design requires the same
// change in every branch, or none (core.ErrBranch).
var ErrBranch = core.ErrBranch

// Name is an element name: a namespace family (transitional and strict
// URIs), the prefix to use if no prefix for it is in scope, and the local
// name (core.Name).
type Name = core.Name

// Path is a sequence of child element names from an object's element
// (core.Path).
type Path = core.Path

// AttrEdit is one attribute change (core.AttrEdit).
type AttrEdit = core.AttrEdit

// The namespace families the pptx edit layer writes, with the schema
// tables that place their elements.
var (
	pmlNS = core.NS{URIs: derive.PML, Prefix: "p", Table: "p"}
	dmlNS = core.NS{URIs: derive.DML, Prefix: "a", Table: "a"}
	vocab = core.Vocab{pmlNS, dmlNS, core.NS{URIs: derive.REL, Prefix: "r"}}
)

// P returns a PresentationML element name.
func P(local string) Name { return pmlNS.Name(local) }

// A returns a DrawingML element name.
func A(local string) Name { return dmlNS.Name(local) }

// Q returns an element name in a single namespace uri.
func Q(uri, prefix, local string) Name { return core.Q(uri, prefix, local) }

// Handle is a write handle on one object: every mc:AlternateContent
// branch element of one address. It is the core's Handle (whose methods
// are the only mutators operation code uses; each applies to every branch
// or fails) with the object's address and the slide-specific reads and
// writes. A Handle is obtained only from Workspace.Target, which declares
// the object in the write set, or Peek, which declares nothing.
type Handle struct {
	*core.Handle
	w   *Workspace
	ref Ref
}

// Ref returns the handle's address.
func (h *Handle) Ref() Ref { return h.ref }

// Kind returns the local name of the projected element (sp, cxnSp, grpSp,
// pic, graphicFrame, contentPart).
func (h *Handle) Kind() string { return h.Elem(0).Local() }

// Target resolves ref to a Handle and declares the object, in every
// branch, as a target of the plan: its own content may change.
func (w *Workspace) Target(ref Ref) (*Handle, error) {
	if ref.IsZero() {
		return nil, fmt.Errorf("%w: zero address", ErrNoObject)
	}
	e, err := w.find(ref.part, ref.id, ref.ord)
	if err != nil {
		return nil, err
	}
	br, err := counterparts(e)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", ref, err)
	}
	return &Handle{Handle: w.core.Target(ref.part, ref.String(), br), w: w, ref: ref}, nil
}

// SlideContainer declares a slide's shape tree as a container of the
// plan: objects may be added to it, removed from it or reordered in it.
func (w *Workspace) SlideContainer(part string) error {
	_, st, err := w.spTree(part)
	if err != nil {
		return err
	}
	w.core.DeclareContainer(part, st)
	return nil
}

// Container declares the group at ref (every branch) as a container of
// the plan: its child objects may be added, removed or reordered.
func (w *Workspace) Container(ref Ref) error {
	e, err := w.find(ref.part, ref.id, ref.ord)
	if err != nil {
		return err
	}
	if !isGroup(e) {
		return fmt.Errorf("edit: %s is not a group", ref)
	}
	br, err := counterparts(e)
	if err != nil {
		return err
	}
	for _, b := range br {
		w.core.DeclareContainer(ref.part, b)
	}
	return nil
}

// enclosingBranch returns the nearest mc:Choice or mc:Fallback around e
// that belongs to an object-level mc:AlternateContent, and that element.
func enclosingBranch(e *tree.Node) (branch, ac *tree.Node) {
	for p := e.Parent(); p != nil; p = p.Parent() {
		if isBranch(p) && p.Parent() != nil && isAC(p.Parent()) {
			return p, p.Parent()
		}
		if derive.Is(p, derive.PML, "spTree") {
			return nil, nil
		}
	}
	return nil, nil
}

// isBranch reports whether e is an mc:Choice or mc:Fallback element.
func isBranch(e *tree.Node) bool {
	return derive.Is(e, derive.MC, "Choice") || derive.Is(e, derive.MC, "Fallback")
}

// branchObjects lists the objects in a branch subtree in document order,
// groups included.
func branchObjects(branch *tree.Node) []*tree.Node {
	var out []*tree.Node
	var walk func(c *tree.Node)
	walk = func(c *tree.Node) {
		for _, e := range c.Elements() {
			if isObject(e) {
				out = append(out, e)
				if isGroup(e) {
					walk(e)
				}
			}
		}
	}
	walk(branch)
	return out
}

// counterparts returns e followed by the matching object in every other
// branch of the mc:AlternateContent around it: the object with the same
// cNvPr id and the same ordinal among that id within its branch. An
// object outside mc:AlternateContent is its own only branch.
func counterparts(e *tree.Node) ([]*tree.Node, error) {
	branch, ac := enclosingBranch(e)
	if branch == nil {
		return []*tree.Node{e}, nil
	}
	if outer, _ := enclosingBranch(ac); outer != nil {
		return nil, fmt.Errorf("%w: nested mc:AlternateContent is not supported for writes", ErrBranch)
	}
	id := objectID(e)
	ord := 0
	for _, o := range branchObjects(branch) {
		if objectID(o) == id {
			ord++
		}
		if o == e {
			break
		}
	}
	out := []*tree.Node{e}
	for _, b := range ac.Elements() {
		if b == branch || !isBranch(b) {
			continue
		}
		n := 0
		var found *tree.Node
		for _, o := range branchObjects(b) {
			if objectID(o) == id {
				n++
				if n == ord {
					found = o
					break
				}
			}
		}
		if found == nil {
			return nil, fmt.Errorf("%w: %s has no object with id %s", ErrBranch, b.Name(), id)
		}
		out = append(out, found)
	}
	return out, nil
}

// ParentMap returns the map from branch i's parent coordinate space (its
// enclosing group's child space, or the slide) to slide space.
func (h *Handle) ParentMap(i int) (derive.Parent, error) {
	var groups []*tree.Node
	for p := h.Elem(i).Parent(); p != nil && !derive.Is(p, derive.PML, "spTree"); p = p.Parent() {
		if isGroup(p) {
			groups = append(groups, p)
		}
	}
	pm := derive.Root
	for _, group := range slices.Backward(groups) {
		g, err := derive.ReadGroupXfrm(derive.XfrmElem(group))
		if err != nil {
			return derive.Parent{}, fmt.Errorf("enclosing group: %w", err)
		}
		if pm, err = pm.Child(g.Float()); err != nil {
			return derive.Parent{}, fmt.Errorf("enclosing group: %w", err)
		}
	}
	return pm, nil
}

// Chain returns branch i's inheritance chain: the object itself, then its
// matching layout and master placeholders, as the read model builds it.
func (h *Handle) Chain(i int) ([]derive.Link, error) {
	lay, mst, err := h.w.layoutTrees(h.ref.part)
	if err != nil {
		return nil, err
	}
	chain, _, _ := derive.PlaceholderChain(h.Elem(i), lay, mst)
	return chain, nil
}

// layoutTrees returns the shape trees of a slide's layout and master, nil
// where there is none.
func (w *Workspace) layoutTrees(part string) (lay, mst *tree.Node, err error) {
	layName, layDoc, err := w.related(part, ctypes.RelSlideLayout)
	if err != nil || layDoc == nil {
		return nil, nil, err
	}
	lay = derive.Path(layDoc.Root(), derive.PML, "cSld", "spTree")
	_, mstDoc, err := w.related(layName, ctypes.RelSlideMaster)
	if err != nil || mstDoc == nil {
		return lay, nil, err
	}
	return lay, derive.Path(mstDoc.Root(), derive.PML, "cSld", "spTree"), nil
}

// related loads the part related to src by the first relationship of type
// want (a ctypes.Rel constant, either vocabulary).
func (w *Workspace) related(src, want string) (string, *tree.Document, error) {
	rels, err := w.pkg.Rels(src)
	if err != nil {
		return "", nil, err
	}
	for _, r := range rels.All() {
		if ctypes.Is(r.Type, want) && !r.External() {
			name, err := rels.TargetPart(r)
			if err != nil {
				return "", nil, err
			}
			// A layout, master or theme that cannot be parsed is a
			// defect the slide did not make: it supplies nothing, as if
			// absent, and an operation that needs what it would supply
			// then finds the value not derived (docs/contract.md,
			// section 10). It never blocks an edit that does not.
			doc, err := w.tree(name)
			if err != nil {
				return name, nil, nil //nolint:nilerr // an unreadable context part is treated as absent, on purpose
			}
			return name, doc, nil
		}
	}
	return "", nil, nil
}
