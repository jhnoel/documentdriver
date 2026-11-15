package edit

import (
	"bytes"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/jhnoel/documentdriver/internal/ooxml/schema"
	"github.com/jhnoel/documentdriver/internal/ooxml/tree"
)

// Errors from Ensure and the placed inserts.
var (
	// ErrUnknownParent: the parent's complex type is not tabulated, so
	// where a child goes is not known.
	ErrUnknownParent = errors.New("edit: parent's child order is not tabulated")
	// ErrNotAChild: the schema does not allow the child in the parent.
	ErrNotAChild = errors.New("edit: element is not a child the schema allows here")
	// ErrRepeated: the child's position repeats, so Ensure cannot say
	// which occurrence is meant.
	ErrRepeated = errors.New("edit: child position repeats; Ensure places only single children")
	// ErrAmbiguousPlace: the parent holds elements the table does not
	// know and none of its known children fixes the position.
	ErrAmbiguousPlace = errors.New("edit: insertion point is ambiguous")
)

// typeOf walks path p from a branch element and returns the complex type
// of the element it reaches.
func (h *Handle) typeOf(e *tree.Node, p Path) (string, error) {
	var typ string
	ok := false
	if h.w.f.Type != nil {
		typ, ok = h.w.f.Type(e)
	}
	if !ok {
		return "", fmt.Errorf("%w: %s", ErrUnknownParent, e.Name())
	}
	for _, n := range p {
		slots, ok := schema.Slots(typ)
		if !ok {
			return "", fmt.Errorf("%w: %s (inside %s)", ErrUnknownParent, n, typ)
		}
		k, c := schema.Find(slots, n.ns.Table, n.local)
		if k < 0 {
			return "", fmt.Errorf("%w: %s in %s", ErrNotAChild, n, typ)
		}
		typ = c.Type
	}
	if _, ok := schema.Slots(typ); !ok {
		return "", fmt.Errorf("%w: %s", ErrUnknownParent, p)
	}
	return typ, nil
}

// slotOf returns the slot index of an existing child element, -1 if the
// table does not know it.
func (w *Workspace) slotOf(slots []schema.Slot, e *tree.Node) int {
	t := w.f.Vocab.table(e)
	if t == "" {
		return -1
	}
	k, _ := schema.Find(slots, t, e.Local())
	return k
}

// Ensure makes sure the element at parent (a path from the handle's
// element) has a child named c, in every branch. An existing child is
// kept as it is. A child in the same choice group (another fill, another
// geometry) is replaced by a new, empty child: the group allows only one.
// Otherwise the new child is inserted where the schema's order puts it:
// before the first existing child that the schema orders after it, else
// after the last that it orders before it. It fails for parents whose
// order is not tabulated, children the schema does not allow, repeating
// positions, and parents whose unknown elements leave the position open.
// It never appends blindly.
func (h *Handle) Ensure(parent Path, c Name) error {
	if parent == nil {
		parent = Path{} // the handle's element itself
	}
	parents := make([]Path, len(h.br))
	for i := range parents {
		parents[i] = parent
	}
	return h.EnsureEach(parents, c)
}

// EnsureEach is Ensure with one parent path per branch, for branches that
// hold the same property in different places (an mc:Choice p:sp and its
// mc:Fallback p:pic). A nil path leaves that branch as it is. As with
// Ensure, nothing changes unless every listed branch can take the child.
func (h *Handle) EnsureEach(parentPaths []Path, c Name) error {
	if len(parentPaths) != len(h.br) {
		return fmt.Errorf("%w: %d paths for %d branches", ErrBranch, len(parentPaths), len(h.br))
	}
	parents := make([]*tree.Node, len(h.br))
	for i, p := range parentPaths {
		if p == nil {
			continue
		}
		n, err := h.resolve(h.br[i], p)
		if err != nil {
			return err
		}
		if n == nil {
			return fmt.Errorf("%w: %s has no %s in branch %d", ErrBranch, h.label, p, i)
		}
		parents[i] = n
	}
	plans := make([]func() error, len(parents))
	for i, pe := range parents {
		if pe == nil {
			plans[i] = func() error { return nil }
			continue
		}
		parent := parentPaths[i]
		typ, err := h.typeOf(h.br[i], parent)
		if err != nil {
			return err
		}
		slots, _ := schema.Slots(typ)
		k, _ := schema.Find(slots, c.ns.Table, c.local)
		if k < 0 {
			return fmt.Errorf("%w: %s in %s", ErrNotAChild, c, typ)
		}
		if slots[k].Multi && !h.w.propertyChoice(typ, slots, k) {
			return fmt.Errorf("%w: %s in %s", ErrRepeated, c, typ)
		}
		plans[i], err = h.w.placement(h.part, pe, typ, slots, k, c)
		if err != nil {
			return err
		}
	}
	for _, apply := range plans {
		if err := apply(); err != nil {
			return err
		}
	}
	return nil
}

// propertyChoice reports whether slot k of complex type typ is a
// repeated choice whose alternatives are properties that each appear
// once, in any order the schema allows (WordprocessingML's run
// properties, EG_RPrBase), as the format says (Format.PropertyChoices).
// The schema, transitional or strict, fixes no order among them; Ensure
// places a new one by the alternatives' declaration order in the table
// (the order Word writes), keeps an existing one, and never reorders or
// flags the others.
func (w *Workspace) propertyChoice(typ string, slots []schema.Slot, k int) bool {
	return slots[k].Multi && len(slots[k].Names) > 1 && w.f.PropertyChoices[typ]
}

// rank orders e among the children of a parent for placing a child of
// slot k named c: its slot, and within a repeated choice its
// alternative's declaration index; ok is false for an element the table
// does not know.
func (w *Workspace) rank(slots []schema.Slot, e *tree.Node) (slot, alt int, ok bool) {
	t := w.f.Vocab.table(e)
	if t == "" {
		return -1, -1, false
	}
	for k, s := range slots {
		for i, x := range s.Names {
			if x.Prefix == t && x.Local == e.Local() {
				return k, i, true
			}
		}
	}
	return -1, -1, false
}

// spot is where a new child goes: replacing same, or before ref, or
// after ref, or (both nil) before the parent's first element, or at the
// end of a parent without elements.
type spot struct {
	same, ref *tree.Node
	before    bool
}

// spotFor decides where a new child of slot k (alternative alt) goes
// under pe, changing nothing. An existing child of the same choice slot
// (not repeated) is replaced. Otherwise the child goes before the first
// known child the schema orders after it, else right after the last
// known child it orders before. Elements the table does not know
// (extensions, mc:AlternateContent) never stop a placement and are never
// moved: with no known child to place against, the new child goes first,
// ahead of them (wildcards and extension lists come last in every
// content model). Existing children are never reordered, whatever their
// order.
func (w *Workspace) spotFor(pe *tree.Node, typ string, slots []schema.Slot, k, alt int) spot {
	var same, before, after *tree.Node
	rc := w.propertyChoice(typ, slots, k)
	for e := range pe.ElementsSeq() {
		sk, sa, ok := w.rank(slots, e)
		switch {
		case !ok:
		case sk == k && !slots[k].Multi:
			same = e
		case sk < k || (sk == k && rc && sa < alt):
			before = e
		case (sk > k || (sk == k && rc && sa > alt)) && after == nil:
			after = e
		}
	}
	switch {
	case same != nil:
		return spot{same: same}
	case after != nil:
		return spot{ref: after, before: true}
	case before != nil:
		return spot{ref: before}
	}
	return spot{}
}

// putAt inserts n under pe at sp and records it as created in part.
func (w *Workspace) putAt(part string, pe *tree.Node, sp spot, n *tree.Node) error {
	var err error
	switch {
	case sp.same != nil:
		err = pe.ReplaceChild(n, sp.same)
	case sp.ref != nil && sp.before:
		err = pe.InsertBefore(n, sp.ref)
	case sp.ref != nil:
		err = pe.InsertAfter(n, sp.ref)
	case pe.HasElements():
		err = pe.InsertBefore(n, pe.Elements()[0]) // only unknown elements: first
	default:
		err = pe.AppendChild(n) // no element children: the only place
	}
	if err != nil {
		return err
	}
	w.record(part, n)
	return nil
}

// placement decides, without changing anything, how to ensure child c of
// slot k under parent pe, and returns the change to make.
func (w *Workspace) placement(part string, pe *tree.Node, typ string, slots []schema.Slot, k int, c Name) (func() error, error) {
	for e := range pe.ElementsSeq() {
		if c.Matches(e) {
			return func() error { return nil }, nil
		}
	}
	alt := 0
	for i, x := range slots[k].Names {
		if x.Prefix == c.ns.Table && x.Local == c.local {
			alt = i
		}
	}
	sp := w.spotFor(pe, typ, slots, k, alt)
	return func() error {
		n, err := c.newIn(pe)
		if err != nil {
			return err
		}
		return w.putAt(part, pe, sp, n)
	}, nil
}

// place inserts the detached element n, of slot k, under pe where the
// schema puts it (see spotFor); an existing child with n's name in a
// repeated choice is replaced.
func (w *Workspace) place(part string, pe *tree.Node, typ string, slots []schema.Slot, k int, n *tree.Node) error {
	alt := 0
	t := w.f.Vocab.table(n)
	for i, x := range slots[k].Names {
		if x.Prefix == t && x.Local == n.Local() {
			alt = i
		}
	}
	if w.propertyChoice(typ, slots, k) {
		for e := range pe.ElementsSeq() {
			if e.Space() == n.Space() && e.Local() == n.Local() {
				if err := pe.ReplaceChild(n, e); err != nil {
					return err
				}
				w.record(part, n)
				return nil
			}
		}
	} else if slots[k].Multi {
		return fmt.Errorf("%w: %s", ErrRepeated, n.Name())
	}
	return w.putAt(part, pe, w.spotFor(pe, typ, slots, k, alt), n)
}

// Placed inserts. Repeated slots (guides in an avLst, colour modifiers,
// paragraphs, the objects of a shape tree or group) cannot be filled by
// Ensure, which cannot say which occurrence is meant. A placed insert says
// it: at an end of the slot, or before or after an existing member. With
// Ensure these are the only insertions, and like Ensure they put children
// where the schema table puts the slot.

// AppendEach appends a new element named c, with the given attributes in
// order, at the end of its repeated slot under the element at parent, in
// every branch: after the last child of the same or an earlier slot,
// before any child of a later slot. It fails unless every branch has the
// parent, the schema tabulates the parent's type with c in a repeated
// slot, and every existing child is in the table.
func (h *Handle) AppendEach(parent Path, c Name, attrs [][2]string) error {
	type ins struct {
		pe, ref *tree.Node
		before  bool
	}
	plan := make([]ins, len(h.br))
	for i, b := range h.br {
		pe, err := h.resolve(b, parent)
		if err != nil {
			return err
		}
		if pe == nil {
			return fmt.Errorf("%w: %s has no %s in branch %d", ErrBranch, h.label, parent, i)
		}
		typ, err := h.typeOf(b, parent)
		if err != nil {
			return err
		}
		slots, _ := schema.Slots(typ)
		k, _ := schema.Find(slots, c.ns.Table, c.local)
		if k < 0 {
			return fmt.Errorf("%w: %s in %s", ErrNotAChild, c, typ)
		}
		if !slots[k].Multi {
			return fmt.Errorf("edit: %s is not a repeated child of %s; use Ensure", c, typ)
		}
		// Unknown elements (extensions) stay attached to the element
		// before them: the new one goes after such a run, never inside it.
		var last, after, first *tree.Node
		for e := range pe.ElementsSeq() {
			if first == nil {
				first = e
			}
			switch sk := h.w.slotOf(slots, e); {
			case sk < 0:
				if last != nil && after == nil {
					last = e
				}
			case sk <= k:
				if after == nil {
					last = e
				}
			case after == nil:
				after = e
			}
		}
		switch {
		case after != nil:
			plan[i] = ins{pe, after, true}
		case last != nil:
			plan[i] = ins{pe, last, false}
		case first != nil:
			plan[i] = ins{pe, first, true}
		default:
			plan[i] = ins{pe, nil, false}
		}
	}
	for _, p := range plan {
		n, err := c.newIn(p.pe)
		if err != nil {
			return err
		}
		h.w.record(h.part, n)
		for _, a := range attrs {
			if err := n.SetAttr(a[0], a[1]); err != nil {
				return err
			}
		}
		switch {
		case p.ref == nil:
			err = p.pe.AppendChild(n)
		case p.before:
			err = p.pe.InsertBefore(n, p.ref)
		default:
			err = p.pe.InsertAfter(n, p.ref)
		}
		if err != nil {
			return err
		}
	}
	return nil
}

// AppendExt adds an extension to the element at parent in every branch:
// its extension list lst (made through Ensure, where the schema puts it)
// gains, at the end, an ext with the given uri (in lst's namespace)
// holding the nodes parsed from frag. frag's prefixes resolve through ns,
// mapped to the vocabulary's family member in scope, or through
// declarations in frag itself, which is how Office writes extension
// content (asvg:svgBlip declares asvg). It fails, changing nothing,
// unless every branch has the parent and none has an extension with that
// uri already, and the fragment parses as elements only.
func (h *Handle) AppendExt(parent Path, lst Name, uri string, frag []byte, ns map[string]string) error {
	for i, b := range h.br {
		pe, err := h.resolve(b, parent)
		if err != nil {
			return err
		}
		if pe == nil {
			return fmt.Errorf("%w: %s has no %s in branch %d", ErrBranch, h.label, parent, i)
		}
		if extOf(pe, lst, uri) != nil {
			return fmt.Errorf("edit: %s already has extension %s in branch %d", h.label, uri, i)
		}
		nodes, err := tree.ParseFragment(frag, h.w.f.Vocab.Remap(pe, ns))
		if err != nil {
			return fmt.Errorf("edit: extension content: %w", err)
		}
		for _, n := range nodes {
			if n.Kind() != tree.ElementNode {
				return errors.New("edit: extension content holds something other than elements")
			}
		}
	}
	if err := h.Ensure(parent, lst); err != nil {
		return err
	}
	lsts, err := h.all(append(append(Path{}, parent...), lst))
	if err != nil {
		return err
	}
	for _, l := range lsts {
		x, err := tree.NewElement(l.Space(), l.Prefix(), "ext")
		if err != nil {
			return err
		}
		if err := x.SetAttr("uri", uri); err != nil {
			return err
		}
		if err := l.AppendChild(x); err != nil {
			return err
		}
		h.w.record(h.part, x)
		if _, err := x.InsertFragment(0, frag, h.w.f.Vocab.Remap(l, ns)); err != nil {
			return err // unreachable after the parse above
		}
	}
	return nil
}

// ReplaceRun replaces the run of one repeated child under the element at
// parent: in each branch i with frags[i] not nil, the children of that
// element from its first child named c to its last inclusive (whatever
// lies between them included) become the nodes parsed from frags[i]. A
// fragment is a sequence of c elements, at least one, with only
// whitespace, comments or processing instructions between them; its
// prefixes are those in scope at the parent or declared on the elements
// themselves. When the parent has no c, the nodes go where the schema
// puts c's slot: right after the first element of the latest earlier
// slot present, else before the first element of a later slot, else at
// the end. Children before the first c and after the last are never
// touched; a nil frags[i] leaves branch i as it is. For a text body it
// replaces the paragraphs and keeps a:bodyPr and a:lstStyle.
//
// Nothing changes unless every listed branch has the parent, its type is
// tabulated with c in a repeated slot, and each fragment checks.
func (h *Handle) ReplaceRun(parent Path, c Name, frags [][]byte) error {
	if len(frags) != len(h.br) {
		return fmt.Errorf("%w: %d fragments for %d branches", ErrBranch, len(frags), len(h.br))
	}
	type change struct {
		el       *tree.Node
		from, to int // children [from, to) are replaced
		frag     []byte
	}
	var plan []change
	for i, frag := range frags {
		if frag == nil {
			continue
		}
		el, err := h.resolve(h.br[i], parent)
		if err != nil {
			return err
		}
		if el == nil {
			return fmt.Errorf("%w: %s has no %s in branch %d", ErrBranch, h.label, parent, i)
		}
		typ, err := h.typeOf(h.br[i], parent)
		if err != nil {
			return err
		}
		slots, _ := schema.Slots(typ)
		k, _ := schema.Find(slots, c.ns.Table, c.local)
		if k < 0 {
			return fmt.Errorf("%w: %s in %s", ErrNotAChild, c, typ)
		}
		if !slots[k].Multi {
			return fmt.Errorf("edit: %s is not a repeated child of %s; use Ensure", c, typ)
		}
		from, to := -1, -1
		earliest := map[int]int{} // earlier slot -> index of its first element
		later := -1
		for x, n := range el.Children() {
			switch sk := h.w.slotOf(slots, n); {
			case c.Matches(n):
				if from < 0 {
					from = x
				}
				to = x + 1
			case sk >= 0 && sk < k:
				if _, seen := earliest[sk]; !seen {
					earliest[sk] = x
				}
			case sk > k && later < 0:
				later = x
			}
		}
		if from < 0 {
			switch {
			case len(earliest) > 0:
				from = -1
				for _, x := range earliest {
					from = max(from, x+1)
				}
			case later >= 0:
				from = later
			default:
				from = len(el.Children())
			}
			to = from
		}
		if err := checkRun(el, c, frag); err != nil {
			return fmt.Errorf("%s: branch %d: %w", h.label, i, err)
		}
		plan = append(plan, change{el, from, to, frag})
	}
	for _, ch := range plan {
		old := slices.Clone(ch.el.Children()[ch.from:ch.to])
		for _, o := range old {
			if err := ch.el.RemoveChild(o); err != nil {
				return err
			}
		}
		nodes, err := ch.el.InsertFragment(ch.from, ch.frag, nil)
		if err != nil {
			return err // unreachable after checkRun
		}
		h.w.record(h.part, nodes...)
	}
	return nil
}

// parseIn parses frag as it will be inserted under el, with the namespace
// declarations in scope there, inside a wrapper element.
func parseIn(el *tree.Node, frag []byte) (*tree.Document, error) {
	scope := inScope(el)
	prefixes := make([]string, 0, len(scope))
	for p := range scope {
		prefixes = append(prefixes, p)
	}
	slices.Sort(prefixes)
	var b strings.Builder
	b.WriteString("<x")
	for _, p := range prefixes {
		name := "xmlns"
		if p != "" {
			name = "xmlns:" + p
		}
		b.WriteString(" " + name + `="` + attrEscaper.Replace(scope[p]) + `"`)
	}
	b.WriteString(">")
	b.Write(frag)
	b.WriteString("</x>")
	return tree.Parse([]byte(b.String()))
}

var attrEscaper = strings.NewReplacer("&", "&amp;", "<", "&lt;", `"`, "&quot;")

// checkRun checks that frag, parsed as it will be inserted under el,
// holds only c elements, at least one, with nothing but whitespace,
// comments and processing instructions between them.
func checkRun(el *tree.Node, c Name, frag []byte) error {
	doc, err := parseIn(el, frag)
	if err != nil {
		return fmt.Errorf("edit: %s content does not parse with the prefixes in scope at %s: %w", c, el.Name(), err)
	}
	n := 0
	for x := range doc.Root().ChildrenSeq() {
		switch x.Kind() {
		case tree.ElementNode:
			if !c.Matches(x) {
				return fmt.Errorf("edit: content holds %s, not only %s", x.Name(), c)
			}
			n++
		case tree.TextNode:
			if strings.TrimSpace(string(x.Raw())) != "" {
				return fmt.Errorf("edit: content holds text outside %s", c)
			}
		case tree.CommentNode, tree.PINode:
		default:
			return fmt.Errorf("edit: content holds a %s node", x.Kind())
		}
	}
	if n == 0 {
		return fmt.Errorf("edit: no %s; the run needs at least one", c)
	}
	return nil
}

// ReplaceChildren replaces the children of a list element inside the
// handle's element: in each branch i with frags[i] not nil, every child
// of the element at p becomes the nodes parsed from frags[i]. The
// element's type must be tabulated with only repeated slots (a list: a
// custom geometry's a:pathLst, a:cxnLst, a:ahLst, a:avLst or a:gdLst),
// and the fragment must hold only elements of those slots, in slot order,
// with whitespace, comments or processing instructions between them; its
// prefixes are those in scope at the element or declared on its own
// elements. An empty fragment empties the list. Nothing changes unless
// every listed branch has the element and its fragment checks.
func (h *Handle) ReplaceChildren(p Path, frags [][]byte) error {
	if len(frags) != len(h.br) {
		return fmt.Errorf("%w: %d fragments for %d branches", ErrBranch, len(frags), len(h.br))
	}
	type change struct {
		el   *tree.Node
		frag []byte
	}
	var plan []change
	for i, frag := range frags {
		if frag == nil {
			continue
		}
		el, err := h.resolve(h.br[i], p)
		if err != nil {
			return err
		}
		if el == nil {
			return fmt.Errorf("%w: %s has no %s in branch %d", ErrBranch, h.label, p, i)
		}
		typ, err := h.typeOf(h.br[i], p)
		if err != nil {
			return err
		}
		slots, _ := schema.Slots(typ)
		for _, s := range slots {
			if !s.Multi {
				return fmt.Errorf("edit: %s (%s) is not a list; ReplaceChildren replaces only repeated children", p, typ)
			}
		}
		if err := h.w.checkList(el, slots, typ, frag); err != nil {
			return fmt.Errorf("%s: branch %d: %w", h.label, i, err)
		}
		plan = append(plan, change{el, frag})
	}
	for _, c := range plan {
		for _, o := range slices.Clone(c.el.Children()) {
			if err := c.el.RemoveChild(o); err != nil {
				return err
			}
		}
		if len(bytes.TrimSpace(c.frag)) == 0 {
			continue
		}
		nodes, err := c.el.InsertFragment(0, c.frag, nil)
		if err != nil {
			return err // unreachable after checkList
		}
		h.w.record(h.part, nodes...)
	}
	return nil
}

// checkList parses frag as it will be inserted under el, with the
// namespace declarations in scope there, and checks that its elements
// belong to el's slots, in order.
func (w *Workspace) checkList(el *tree.Node, slots []schema.Slot, typ string, frag []byte) error {
	doc, err := parseIn(el, frag)
	if err != nil {
		return fmt.Errorf("edit: list content does not parse with the prefixes in scope at %s: %w", el.Name(), err)
	}
	last := 0
	for c := range doc.Root().ChildrenSeq() {
		switch c.Kind() {
		case tree.ElementNode:
			k := w.slotOf(slots, c)
			if k < 0 {
				return fmt.Errorf("%w: %s in %s", ErrNotAChild, c.Name(), typ)
			}
			if k < last {
				return fmt.Errorf("edit: %s out of schema order in %s", c.Name(), typ)
			}
			last = k
		case tree.TextNode:
			if strings.TrimSpace(string(c.Raw())) != "" {
				return fmt.Errorf("edit: text in %s", typ)
			}
		case tree.CommentNode, tree.PINode:
		default:
			return fmt.Errorf("edit: list content holds a %s node", c.Kind())
		}
	}
	return nil
}
