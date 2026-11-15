package edit

import (
	"errors"
	"fmt"

	"github.com/jhnoel/documentdriver/internal/ooxml/schema"
	"github.com/jhnoel/documentdriver/internal/ooxml/tree"
)

// ErrBranch is returned when an edit cannot be applied to every branch
// element of a handle (every mc:AlternateContent branch of an object):
// the same change is made in every branch, or none.
var ErrBranch = errors.New("edit: cannot apply to every mc:AlternateContent branch")

// Handle is a write handle on one addressed thing: its branch elements
// (one outside mc:AlternateContent; one per branch inside). Its methods
// are the only mutators operation code uses; each applies to every
// branch or fails before changing anything. A Handle is obtained from
// Workspace.Target, which declares its elements in the write set (or
// Peek, which declares nothing).
type Handle struct {
	w     *Workspace
	part  string
	label string
	br    []*tree.Node // [0] is the projected branch
	// reg, when set, makes the handle a region (see Region): the verbs
	// work on any element of it, crossing units, instead of only inside
	// the branch elements.
	reg *region
}

// Part returns the name of the part the handle's elements are in.
func (h *Handle) Part() string { return h.part }

// Branches returns the number of branch elements (1 outside
// mc:AlternateContent).
func (h *Handle) Branches() int { return len(h.br) }

// Elem returns the element of branch i (0 is the projected branch) for
// reading. Callers must not mutate it; the architecture test forbids tree
// mutators outside the edit core.
func (h *Handle) Elem(i int) *tree.Node { return h.br[i] }

// resolve finds the element at path p under branch element e, refusing to
// step onto an element the format bounds (another object). It returns
// nil without error when the path does not exist.
func (h *Handle) resolve(e *tree.Node, p Path) (*tree.Node, error) {
	cur := e
	for _, n := range p {
		var next *tree.Node
		for c := range cur.ElementsSeq() {
			if n.Matches(c) {
				next = c
				break
			}
		}
		if next == nil {
			return nil, nil
		}
		if h.w.f.Bound != nil && h.w.f.Bound(next) {
			return nil, fmt.Errorf("edit: path %s steps into another object", p)
		}
		cur = next
	}
	return cur, nil
}

// Find returns the element at path p in the projected branch for
// reading, or nil.
func (h *Handle) Find(p Path) *tree.Node {
	n, _ := h.resolve(h.br[0], p)
	return n
}

// FindIn returns the element at path p in branch i for reading, or nil.
func (h *Handle) FindIn(i int, p Path) *tree.Node {
	n, _ := h.resolve(h.br[i], p)
	return n
}

// all resolves p in every branch, failing unless every branch has it.
func (h *Handle) all(p Path) ([]*tree.Node, error) {
	out := make([]*tree.Node, len(h.br))
	for i, b := range h.br {
		n, err := h.resolve(b, p)
		if err != nil {
			return nil, err
		}
		if n == nil {
			return nil, fmt.Errorf("%w: %s has no %s in branch %d", ErrBranch, h.label, p, i)
		}
		out[i] = n
	}
	return out, nil
}

// SetAttr sets attribute name to value on the element at p in every
// branch. Setting the value an attribute already has changes nothing.
func (h *Handle) SetAttr(p Path, name, value string) error {
	nodes, err := h.all(p)
	if err != nil {
		return err
	}
	for _, n := range nodes {
		if err := n.SetAttr(name, value); err != nil {
			return err
		}
	}
	return nil
}

// RemoveAttr removes attribute name from the element at p in every
// branch.
func (h *Handle) RemoveAttr(p Path, name string) error {
	nodes, err := h.all(p)
	if err != nil {
		return err
	}
	for _, n := range nodes {
		n.RemoveAttr(name)
	}
	return nil
}

// Attr is an attribute name: a local name, in a namespace family when
// the attribute is qualified (w:val), or unqualified (val) when NS has no
// URIs.
type Attr struct {
	NS    NS
	Local string
}

// SetChecked sets attribute a to value on the element at p in every
// branch after checking value against the attribute's simple type in the
// schema table (schema.CheckAttr on the complex type the path reaches,
// which tabulates attributes by local name). It fails, changing nothing,
// when the element's type or the attribute is not tabulated, the value
// is outside its type, or a branch lacks the element. A qualified
// attribute is written in the family's URI in scope at the element
// (reusing its prefix), else with the family's prefix.
func (h *Handle) SetChecked(p Path, a Attr, value string) error {
	nodes, err := h.all(p)
	if err != nil {
		return err
	}
	for i, b := range h.br {
		typ, err := h.typeOf(b, p)
		if err != nil {
			return err
		}
		if err := schema.CheckAttr(typ, a.Local, value); err != nil {
			return fmt.Errorf("%s: branch %d: %w", h.label, i, err)
		}
	}
	for _, n := range nodes {
		if len(a.NS.URIs) == 0 {
			if err := n.SetAttr(a.Local, value); err != nil {
				return err
			}
			continue
		}
		uri := a.NS.URIs[0]
		for _, u := range a.NS.URIs {
			if _, ok := n.LookupNamespace(u); ok {
				uri = u
				break
			}
		}
		if err := n.SetAttrNS(uri, a.NS.Prefix, a.Local, value); err != nil {
			return err
		}
	}
	return nil
}

// AttrEdit is one attribute change: set Name to Value on the element at
// Path, or remove Name when Remove is set.
type AttrEdit struct {
	Path   Path
	Name   string
	Value  string
	Remove bool
}

// ApplyEach applies edits[i] to branch i, for values that differ between
// branches (a slide-space transform stored under different parents). It
// fails before changing anything unless there is one list per branch and
// every path exists in its branch.
func (h *Handle) ApplyEach(edits [][]AttrEdit) error {
	if len(edits) != len(h.br) {
		return fmt.Errorf("%w: %d edit lists for %d branches", ErrBranch, len(edits), len(h.br))
	}
	nodes := make([][]*tree.Node, len(edits))
	for i, list := range edits {
		for _, e := range list {
			n, err := h.resolve(h.br[i], e.Path)
			if err != nil {
				return err
			}
			if n == nil {
				return fmt.Errorf("%w: %s has no %s in branch %d", ErrBranch, h.label, e.Path, i)
			}
			nodes[i] = append(nodes[i], n)
		}
	}
	for i, list := range edits {
		for k, e := range list {
			if e.Remove {
				nodes[i][k].RemoveAttr(e.Name)
				continue
			}
			if err := nodes[i][k].SetAttr(e.Name, e.Value); err != nil {
				return err
			}
		}
	}
	return nil
}

// Locator finds, for reading or an attribute edit, an element inside a
// branch element, or nil. It lets a format address elements a fixed path
// cannot (a child whose name varies by object kind).
type Locator func(branch *tree.Node) *tree.Node

// located runs loc in every branch, failing unless every branch has the
// element and it lies inside the branch element (and inside no element
// the format bounds below it). what names it in errors.
func (h *Handle) located(loc Locator, what string) ([]*tree.Node, error) {
	nodes := make([]*tree.Node, len(h.br))
	for i, b := range h.br {
		n := loc(b)
		if n == nil {
			return nil, fmt.Errorf("%w: %s has no %s in branch %d", ErrBranch, h.label, what, i)
		}
		for p := n; p != b; p = p.Parent() {
			if p == nil {
				return nil, fmt.Errorf("edit: %s of %s is outside branch %d", what, h.label, i)
			}
			if p != n && h.w.f.Bound != nil && h.w.f.Bound(p) {
				return nil, fmt.Errorf("edit: %s of %s is inside another object", what, h.label)
			}
		}
		nodes[i] = n
	}
	return nodes, nil
}

// SetAttrAt sets attribute name to value on the element loc finds in
// every branch (what names it in errors). It fails, changing nothing,
// unless every branch has it.
func (h *Handle) SetAttrAt(loc Locator, what, name, value string) error {
	nodes, err := h.located(loc, what)
	if err != nil {
		return err
	}
	for _, n := range nodes {
		if err := n.SetAttr(name, value); err != nil {
			return err
		}
	}
	return nil
}

// RemoveAttrAt removes attribute name, where present, from the element
// loc finds in every branch (what names it in errors). It fails, changing
// nothing, unless every branch has the element.
func (h *Handle) RemoveAttrAt(loc Locator, what, name string) error {
	nodes, err := h.located(loc, what)
	if err != nil {
		return err
	}
	for _, n := range nodes {
		if _, ok := n.Attr(name); ok {
			n.RemoveAttr(name)
		}
	}
	return nil
}

// SetChildAttr sets attribute name to value on the first child element
// of the element at p for which pick reports true, in every branch (what
// names the child in errors). Setting the value it already has changes
// nothing. It fails, changing nothing, unless every branch has the child.
func (h *Handle) SetChildAttr(p Path, pick func(*tree.Node) bool, what, name, value string) error {
	nodes, err := h.all(p)
	if err != nil {
		return err
	}
	kids := make([]*tree.Node, len(nodes))
	for i, n := range nodes {
		for c := range n.ElementsSeq() {
			if pick(c) {
				kids[i] = c
				break
			}
		}
		if kids[i] == nil {
			return fmt.Errorf("%w: %s has no %s in branch %d", ErrBranch, h.label, what, i)
		}
	}
	for _, k := range kids {
		if err := k.SetAttr(name, value); err != nil {
			return err
		}
	}
	return nil
}

// Remove removes the element at p from every branch. It changes nothing
// when no branch has it, and fails (changing nothing) when only some do.
func (h *Handle) Remove(p Path) error {
	nodes := make([]*tree.Node, len(h.br))
	have := 0
	for i, b := range h.br {
		n, err := h.resolve(b, p)
		if err != nil {
			return err
		}
		if n != nil {
			have++
		}
		nodes[i] = n
	}
	if have == 0 {
		return nil
	}
	if have != len(h.br) {
		return fmt.Errorf("%w: %s has %s in only some branches", ErrBranch, h.label, p)
	}
	for _, n := range nodes {
		if err := n.Remove(); err != nil {
			return err
		}
	}
	return nil
}

// RemoveChildren removes every child element of the element at p for
// which drop reports true, in every branch. It fails, changing nothing,
// unless every branch has the element at p.
func (h *Handle) RemoveChildren(p Path, drop func(*tree.Node) bool) error {
	nodes, err := h.all(p)
	if err != nil {
		return err
	}
	for _, n := range nodes {
		for c := range n.ElementsSeq() {
			if drop(c) {
				if err := n.RemoveChild(c); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

// relAttr sets the relationships-namespace attribute local on e, reusing
// the attribute or the prefix in scope, else declaring r on e.
func relAttr(e *tree.Node, local, value string) error {
	for _, u := range REL.URIs {
		if _, ok := e.AttrNS(u, local); ok {
			return e.SetAttrNS(u, "r", local, value)
		}
	}
	uri := REL.URIs[0]
	for _, u := range REL.URIs {
		if _, ok := e.LookupNamespace(u); ok {
			uri = u
			break
		}
	}
	return e.SetAttrNS(uri, "r", local, value)
}

// SetRelAttr sets the relationship-id attribute local (r:id, r:embed,
// r:link) of the element at p in every branch, in the relationships
// namespace in scope there. It fails, changing nothing, unless every
// branch has the element.
func (h *Handle) SetRelAttr(p Path, local, value string) error {
	nodes, err := h.all(p)
	if err != nil {
		return err
	}
	for _, n := range nodes {
		if err := relAttr(n, local, value); err != nil {
			return err
		}
	}
	return nil
}

// extOf returns the ext child with the given uri in the extension list
// lst (a:extLst, p:extLst, ...) of e, or nil. Extensions are named ext in
// their list's namespace in every OOXML vocabulary.
func extOf(e *tree.Node, lst Name, uri string) *tree.Node {
	var l *tree.Node
	for c := range e.ElementsSeq() {
		if lst.Matches(c) {
			l = c
			break
		}
	}
	if l == nil {
		return nil
	}
	for x := range l.ElementsSeq() {
		if u, _ := x.Attr("uri"); lst.ns.Is(x, "ext") && u == uri {
			return x
		}
	}
	return nil
}

// ExtChild returns, for reading, the child named (space, local) of the
// extension with the given uri in the extension list lst of the element
// at p in branch i, or nil.
func (h *Handle) ExtChild(i int, p Path, lst Name, uri, space, local string) *tree.Node {
	e, _ := h.resolve(h.br[i], p)
	if e == nil {
		return nil
	}
	x := extOf(e, lst, uri)
	if x == nil {
		return nil
	}
	for c := range x.ElementsSeq() {
		if c.Space() == space && c.Local() == local {
			return c
		}
	}
	return nil
}

// SetExtRelAttr sets the relationship-id attribute local of the child
// (space, child) of extension uri in the list lst under the element at p,
// in every branch. It fails, changing nothing, unless every branch has
// that child.
func (h *Handle) SetExtRelAttr(p Path, lst Name, uri, space, child, local, value string) error {
	nodes := make([]*tree.Node, len(h.br))
	for i := range h.br {
		if nodes[i] = h.ExtChild(i, p, lst, uri, space, child); nodes[i] == nil {
			return fmt.Errorf("%w: %s has no extension %s with %s in branch %d", ErrBranch, h.label, uri, child, i)
		}
	}
	for _, n := range nodes {
		if err := relAttr(n, local, value); err != nil {
			return err
		}
	}
	return nil
}

// RemoveExt removes the extension with the given uri from the list lst of
// the element at p in every branch, and the list when nothing is left in
// it. It changes nothing when no branch has the extension, and fails,
// changing nothing, when only some do.
func (h *Handle) RemoveExt(p Path, lst Name, uri string) error {
	exts := make([]*tree.Node, len(h.br))
	have := 0
	for i, b := range h.br {
		e, err := h.resolve(b, p)
		if err != nil {
			return err
		}
		if e != nil {
			exts[i] = extOf(e, lst, uri)
		}
		if exts[i] != nil {
			have++
		}
	}
	if have == 0 {
		return nil
	}
	if have != len(h.br) {
		return fmt.Errorf("%w: %s has extension %s in only some branches", ErrBranch, h.label, uri)
	}
	for _, x := range exts {
		l := x.Parent()
		if err := x.Remove(); err != nil {
			return err
		}
		if !l.HasElements() {
			if err := l.Remove(); err != nil {
				return err
			}
		}
	}
	return nil
}
