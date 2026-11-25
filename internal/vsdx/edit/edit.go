// Package edit is the vsdx edit layer (P2, needed by create first): the
// workspace of a plan transaction over a drawing, resolving addresses
// to handles on pages, shapes, cells, rows, text and Connect rows
// through the edit core (internal/ooxml/edit), declaring the write set,
// allocating shape and page IDs (max+1, across nested shapes) and
// adding parts (a page, docProps/custom.xml). Child order comes from the
// corpus-derived table (chunk S3), never a hand-written list.
package edit

import (
	"bytes"
	"errors"
	"fmt"
	"strconv"

	"github.com/jhnoel/documentdriver/internal/ooxml/ctypes"
	core "github.com/jhnoel/documentdriver/internal/ooxml/edit"
	"github.com/jhnoel/documentdriver/internal/ooxml/schema"
	"github.com/jhnoel/documentdriver/internal/ooxml/tree"
	"github.com/jhnoel/documentdriver/internal/opc"
	"github.com/jhnoel/documentdriver/internal/plan"
	"github.com/jhnoel/documentdriver/internal/vsdx/doc"
	"github.com/jhnoel/documentdriver/internal/vsdx/vsd"
)

// ErrNotFound is returned (wrapped, with what is missing) for a page
// or shape the drawing does not have: not_found.
var ErrNotFound = errors.New("vsdx edit: not found")

// Unreadable is an operation's own target that cannot be edited safely
// (unreadable_target): its part is missing, or its page part is not
// well formed.
type Unreadable struct {
	// Target is the address of what is unreadable ("page2").
	Target string
	// Detail says what is wrong (the contract's detail).
	Detail string
}

// Error implements error.
func (u *Unreadable) Error() string { return u.Target + ": " + u.Detail }

// vsdxNS is the Visio main vocabulary family (either spelling, 2012
// first): new elements are unprefixed as Visio writes them, and the
// part's own spelling is reused when it is in scope.
var vsdxNS = core.NS{URIs: []string{vsd.NSMain2012, vsd.NSCore2011}, Prefix: "", Table: "v"}

// vsdxFormat is what the edit core needs from Visio: its namespaces,
// the units the verifier compares, the complex type of an element and
// the shapes a path must not step into.
var vsdxFormat = core.Format{
	Vocab: core.Vocab{vsdxNS, core.REL},
	Units: units{},
	Type:  visioType,
	Bound: func(e *tree.Node) bool { return isElem(e, vsd.ElemShape) },
}

// visioType returns the schema complex type of a Visio element
// ("v:Shape"): the "v" table is keyed by element name.
func visioType(e *tree.Node) (string, bool) {
	if e == nil || e.Kind() != tree.ElementNode || !vsd.IsMain(e.Space()) {
		return "", false
	}
	typ := "v:" + e.Local()
	if _, ok := schema.Slots(typ); !ok {
		return "", false
	}
	return typ, true
}

// isElem reports whether e is an element of local name in a Visio main
// namespace (either spelling). The local name is compared first: most
// elements a page holds are not Shapes, and resolving the namespace
// walks the ancestors, so checking the local name first keeps the
// common no-match case cheap.
func isElem(e *tree.Node, local string) bool {
	return e != nil && e.Kind() == tree.ElementNode && e.Local() == local && vsd.IsMain(e.Space())
}

// child returns the first direct element child of parent of local
// name in a Visio main namespace (either spelling), or nil.
func child(parent *tree.Node, local string) *tree.Node {
	if c := parent.Child(vsd.NSMain2012, local); c != nil {
		return c
	}
	return parent.Child(vsd.NSCore2011, local)
}

// units divides Visio parts for the verifier (plan.Units): page and
// master contents, whose Shapes and Connects hold Shape and Connect
// units. Every other part has no units and is declared as a whole.
type units struct{}

// Units returns the Visio units the verifier compares (plan.Units),
// for law harnesses that hold plans to the same division
// (internal/laws).
func Units() plan.Units { return units{} }

// HasUnits implements plan.Units: page and master contents parts.
func (units) HasUnits(d *tree.Document) bool {
	r := d.Root()
	return r != nil && (isElem(r, vsd.ElemPageContents) || isElem(r, vsd.ElemMasterContents))
}

// IsUnit implements plan.Units.
func (units) IsUnit(e *tree.Node) bool {
	if e == nil || e.Kind() != tree.ElementNode || !vsd.IsMain(e.Space()) {
		return false
	}
	switch e.Local() {
	case vsd.ElemShape, vsd.ElemConnect, vsd.ElemShapes, vsd.ElemConnects:
		return true
	}
	return false
}

// Describe implements plan.Units.
func (units) Describe(e *tree.Node) string {
	if isElem(e, vsd.ElemShape) {
		if id, ok := e.Attr("ID"); ok {
			return "shape s" + id
		}
	}
	return e.Name()
}

// Workspace is a plan transaction's view of the drawing.
type Workspace struct {
	tx        *plan.Tx
	pkg       *opc.Package
	core      *core.Workspace
	doc       *doc.Document
	pagesPart string // the pages.xml part, for page-sheet cells
	// shapes caches, per page part and on first use, the shape index
	// of the page's tree (shapeIndex). InsertShape is the only write
	// that adds shapes, and it updates the index as it inserts, so a
	// lookup never has to walk the page again.
	shapes map[string]*shapeIndex
}

// failing is the workspace of a package the edit layer refused.
type failing struct{ err error }

// Verify returns the error that made opening the package fail.
func (f failing) Verify() error { return f.err }

// Begin opens the drawing at path as a plan transaction. Chunk E1.
func Begin(path string, opts ...plan.Option) (*Workspace, error) {
	var ws *Workspace
	var openErr error
	tx, err := plan.Begin(path, func(pkg *opc.Package) plan.Workspace {
		ws, openErr = openWorkspace(pkg)
		if openErr != nil {
			return failing{err: openErr}
		}
		return ws
	}, opts...)
	if err != nil {
		return nil, err
	}
	if openErr != nil {
		tx.Rollback()
		return nil, openErr
	}
	ws.tx = tx
	return ws, nil
}

// openWorkspace binds an edit workspace to pkg: the edit core over
// the Visio format and the package structure of doc.Open.
func openWorkspace(pkg *opc.Package) (*Workspace, error) {
	d, err := doc.Open(pkg)
	if err != nil {
		return nil, fmt.Errorf("vsdx edit: %w", err)
	}
	pages, err := pagesPartName(pkg, d.Part)
	if err != nil {
		return nil, err
	}
	return &Workspace{pkg: pkg, core: core.New(pkg, vsdxFormat), doc: d, pagesPart: pages}, nil
}

// pagesPartName returns the pages.xml part of the drawing (the target
// of the document part's pages relationship), or "" when it has none.
func pagesPartName(pkg *opc.Package, docPart string) (string, error) {
	rels, err := pkg.Rels(docPart)
	if err != nil {
		return "", fmt.Errorf("vsdx edit: document relationships: %w", err)
	}
	for _, rel := range rels.All() {
		if rel.External() || !ctypes.Is(rel.Type, ctypes.RelPages) {
			continue
		}
		part, err := rels.TargetPart(rel)
		if err != nil {
			continue
		}
		return part, nil
	}
	return "", nil
}

// Tx returns the plan transaction the workspace belongs to: it writes
// the working copy through plan.Tx (Commit, CommitTo, Finish*).
func (w *Workspace) Tx() *plan.Tx { return w.tx }

// Doc returns the drawing as the workspace keeps it: the document
// opened with the workspace, whose page list the page operations keep
// current (page.add, page.rename, page.delete, page.reorder,
// page.set), so a page an earlier operation of the plan added is in
// it. doc.Open of Package reads the parts as they were opened and
// lacks such a page. Callers must not modify it.
func (w *Workspace) Doc() *doc.Document { return w.doc }

// Package returns the transaction's working copy.
func (w *Workspace) Package() *opc.Package { return w.pkg }

// Core returns the edit core's workspace: unit placement and the
// declared write set behind the shape writes.
func (w *Workspace) Core() *core.Workspace { return w.core }

// Verify checks the working copy against the declared write set (the
// plan.Workspace interface). Chunk E1.
func (w *Workspace) Verify() error { return w.core.Verify() }

// pageTree resolves a parsed page address to its entry and the working
// tree of its part (Part.Tree() of the working copy).
func (w *Workspace) pageTree(a vsd.Address) (*doc.PageEntry, string, *tree.Document, error) {
	var entry *doc.PageEntry
	depth := 0
	for i := range w.doc.Pages {
		p := &w.doc.Pages[i]
		if p.ID != a.Container {
			continue
		}
		depth++
		if depth == a.PageDup {
			entry = p
			break
		}
	}
	if entry == nil {
		return nil, "", nil, fmt.Errorf("%w: %s", ErrNotFound, vsd.PageID(a.Container, a.PageDup))
	}
	if entry.Part == "" {
		return nil, "", nil, &Unreadable{Target: vsd.PageID(entry.ID, depth), Detail: "it has no part"}
	}
	d, err := w.core.Tree(entry.Part)
	if err != nil {
		return nil, "", nil, &Unreadable{Target: vsd.PageID(entry.ID, depth), Detail: err.Error()}
	}
	if !isElem(d.Root(), vsd.ElemPageContents) {
		return nil, "", nil, &Unreadable{Target: vsd.PageID(entry.ID, depth), Detail: "it is not a " + vsd.ElemPageContents + " part"}
	}
	return entry, entry.Part, d, nil
}

// shapeID reads a Shape element's ID attribute as stored.
func shapeID(e *tree.Node) (uint32, bool) {
	v, ok := e.Attr("ID")
	if !ok {
		return 0, false
	}
	u, err := strconv.ParseUint(v, 10, 32)
	if err != nil {
		return 0, false
	}
	return uint32(u), true
}

// findShape returns the dup-th (1-based) shape with stored ID id in
// root's tree, in document order (a group before its children), or
// nil. It walks the whole tree iteratively (tree.Node.Walk).
func findShape(root *tree.Node, id uint32, dup int) *tree.Node {
	var found *tree.Node
	seen := 0
	root.Walk(func(n *tree.Node) bool {
		if found != nil {
			return false
		}
		if isElem(n, vsd.ElemShape) {
			if v, ok := shapeID(n); ok && v == id {
				seen++
				if seen == dup {
					found = n
				}
			}
		}
		return true
	})
	return found
}

// shapeIndex is the cached shape lookup of one page tree: every shape
// of the page, nested shapes included, by stored ID in document
// order (the dup-th shape of an ID is its index in the slice), the
// largest stored ID, and the last shape child of every Shapes
// element, keyed by that element. It is built once, on first use,
// and InsertShape updates it as it inserts.
type shapeIndex struct {
	root    *tree.Node
	largest uint32
	seen    bool
	byID    map[uint32][]*tree.Node
	lasts   map[*tree.Node]*tree.Node
}

// shapeIndexFor returns the shape index of the page part, building it
// on first use (one walk of the page tree) and reusing it while the
// page tree is the same one it was built from.
func (w *Workspace) shapeIndexFor(part string, root *tree.Node) *shapeIndex {
	if w.shapes == nil {
		w.shapes = map[string]*shapeIndex{}
	}
	if idx, ok := w.shapes[part]; ok && idx.root == root {
		return idx
	}
	idx := &shapeIndex{root: root, byID: map[uint32][]*tree.Node{}, lasts: map[*tree.Node]*tree.Node{}}
	root.Walk(func(n *tree.Node) bool {
		if n.Kind() == tree.ElementNode && n.Local() == vsd.ElemShape && vsd.IsMain(n.Space()) {
			idx.add(n)
		}
		return true
	})
	w.shapes[part] = idx
	return idx
}

// add records shape n in the index: its stored ID (in document
// order, as the walk and the inserts visit shapes) and as the last
// shape of its Shapes parent.
func (idx *shapeIndex) add(n *tree.Node) {
	if id, ok := shapeID(n); ok {
		idx.byID[id] = append(idx.byID[id], n)
		idx.seen = true
		if id > idx.largest {
			idx.largest = id
		}
	}
	if p := n.Parent(); p != nil && isElem(p, vsd.ElemShapes) {
		idx.lasts[p] = n
	}
}

// find returns the dup-th (1-based) shape with stored ID id, or nil.
func (idx *shapeIndex) find(id uint32, dup int) *tree.Node {
	if dup < 1 {
		return nil
	}
	if list := idx.byID[id]; dup <= len(list) {
		return list[dup-1]
	}
	return nil
}

// next returns one more than the largest shape ID in the index, or 1
// when the page has no shape ID.
func (idx *shapeIndex) next() (uint32, error) {
	if !idx.seen {
		return 1, nil
	}
	if idx.largest == ^uint32(0) {
		return 0, errors.New("vsdx edit: shape ID space exhausted")
	}
	return idx.largest + 1, nil
}

// ensureChild makes sure container has a child of local name through
// the edit core, declaring container as a container when the child is
// new (its list of child units grows), and returns it.
func (w *Workspace) ensureChild(part, label string, container *tree.Node, local string) (*tree.Node, error) {
	h := w.core.Peek(part, label, []*tree.Node{container})
	newChild := child(container, local) == nil
	if err := h.Ensure(core.Path{}, vsdxNS.Name(local)); err != nil {
		return nil, err
	}
	c := child(container, local)
	if c == nil {
		return nil, fmt.Errorf("vsdx edit: %s has no %s", label, local)
	}
	if newChild {
		w.core.DeclareContainer(part, container)
	}
	return c, nil
}

// NextShapeID returns the next free shape ID of a page: one more than
// the largest ID among all its shapes, nested ones included (notes §9:
// duplicate IDs make Visio repair the file). Chunk E1.
func (w *Workspace) NextShapeID(page string) (uint32, error) {
	a, err := vsd.Parse(page)
	if err != nil {
		return 0, fmt.Errorf("vsdx edit: %w", err)
	}
	if a.Kind != vsd.KindPage || a.InMaster {
		return 0, fmt.Errorf("vsdx edit: %s: want a page address (page0)", page)
	}
	_, part, d, err := w.pageTree(a)
	if err != nil {
		return 0, err
	}
	return w.shapeIndexFor(part, d.Root()).next()
}

// InsertShape inserts a Shape element (emit's bytes) as the last child
// of the page's Shapes, or of the group's Shapes when parent is a group
// address, creating the Shapes element through the core when absent,
// and declares the page part changed. It returns the new address.
// Chunk E1.
func (w *Workspace) InsertShape(page, parent string, frag []byte) (string, error) {
	a, err := vsd.Parse(page)
	if err != nil {
		return "", fmt.Errorf("vsdx edit: %w", err)
	}
	if a.Kind != vsd.KindPage || a.InMaster {
		return "", fmt.Errorf("vsdx edit: %s: want a page address (page0)", page)
	}
	_, part, d, err := w.pageTree(a)
	if err != nil {
		return "", err
	}
	root := d.Root()
	idx := w.shapeIndexFor(part, root)
	container := root
	label := vsd.PageID(a.Container, a.PageDup)
	if parent != "" {
		p, err := vsd.Parse(parent)
		if err != nil {
			return "", fmt.Errorf("vsdx edit: %w", err)
		}
		if p.Kind != vsd.KindShape || p.InMaster {
			return "", fmt.Errorf("vsdx edit: %s: want a shape address (page0/s3)", parent)
		}
		if p.Container != a.Container || p.PageDup != a.PageDup {
			return "", fmt.Errorf("vsdx edit: %s: not on %s", parent, label)
		}
		g := idx.find(p.Shape, p.Dup)
		if g == nil {
			return "", fmt.Errorf("%w: %s", ErrNotFound, parent)
		}
		if typ, _ := g.Attr("Type"); typ != vsd.ShapeTypeGroup {
			return "", fmt.Errorf("vsdx edit: %s: not a group", parent)
		}
		container, label = g, parent
	}
	id, err := idx.next()
	if err != nil {
		return "", err
	}
	nodes, err := w.core.Parse(container, frag, nil)
	if err != nil {
		return "", fmt.Errorf("vsdx edit: fragment: %w", err)
	}
	if len(nodes) != 1 || nodes[0].Kind() != tree.ElementNode || !isElem(nodes[0], vsd.ElemShape) {
		return "", errors.New("vsdx edit: fragment is not a single Shape element")
	}
	shapes, err := w.ensureChild(part, label, container, vsd.ElemShapes)
	if err != nil {
		return "", err
	}
	// Target as well as the Insert's container: an empty <Shapes/> has
	// no start-tag form to compare, so target+container (unchecked) is
	// what lets the first shape expand it (plan.Verify checks the
	// start tag of a container).
	_ = w.core.Target(part, label, []*tree.Node{shapes})
	pos, err := w.shapesPos(idx, shapes)
	if err != nil {
		return "", err
	}
	u := nodes[0]
	if err := w.core.Insert(part, pos, u, []core.NodeAttr{{Node: u, Name: "ID", Value: strconv.FormatUint(uint64(id), 10)}}); err != nil {
		return "", err
	}
	// The inserted shape, and any shape nested in its fragment, are
	// in the page now: record them (and their IDs) so the next lookup
	// or next ID is the same one a walk of the page would give.
	u.Walk(func(n *tree.Node) bool {
		if n.Kind() == tree.ElementNode && n.Local() == vsd.ElemShape && vsd.IsMain(n.Space()) {
			idx.add(n)
		}
		return true
	})
	return vsd.ShapeID(vsd.PageID(a.Container, a.PageDup), id, 1), nil
}

// shapesPos returns the position for a new shape at the end of the
// shape slot of shapes: after the last shape the index already knows
// (the last top-level shape, or the last shape of a group), with no
// scan of the shapes. A Shapes with no known shape, or with an
// element standing after its last known shape (one the schema does
// not place, which a scan alone can place against), goes through
// the core's SlotEnd as before.
func (w *Workspace) shapesPos(idx *shapeIndex, shapes *tree.Node) (core.Position, error) {
	if last := idx.lasts[shapes]; last != nil && last.Parent() == shapes {
		trailing := false
		for s := last.NextSibling(); s != nil; s = s.NextSibling() {
			if s.Kind() == tree.ElementNode {
				trailing = true
				break
			}
		}
		if !trailing {
			return core.After(last, nil), nil
		}
	}
	return w.core.SlotEnd(shapes, "v:Shapes", vsdxNS.Name(vsd.ElemShape), true, nil, nil)
}

// InsertConnects appends Connect rows to the page's Connects, creating
// it after Shapes when absent. Chunk E1.
func (w *Workspace) InsertConnects(page string, frags [][]byte) error {
	a, err := vsd.Parse(page)
	if err != nil {
		return fmt.Errorf("vsdx edit: %w", err)
	}
	if a.Kind != vsd.KindPage || a.InMaster {
		return fmt.Errorf("vsdx edit: %s: want a page address (page0)", page)
	}
	_, part, d, err := w.pageTree(a)
	if err != nil {
		return err
	}
	label := vsd.PageID(a.Container, a.PageDup)
	root := d.Root()
	nodes := make([]*tree.Node, 0, len(frags))
	for i, f := range frags {
		parsed, err := w.core.Parse(root, f, nil)
		if err != nil {
			return fmt.Errorf("vsdx edit: fragment %d: %w", i, err)
		}
		if len(parsed) != 1 || parsed[0].Kind() != tree.ElementNode || !isElem(parsed[0], vsd.ElemConnect) {
			return fmt.Errorf("vsdx edit: fragment %d is not a single Connect element", i)
		}
		nodes = append(nodes, parsed[0])
	}
	if len(nodes) == 0 {
		return nil
	}
	connects, err := w.ensureChild(part, label, root, vsd.ElemConnects)
	if err != nil {
		return err
	}
	_ = w.core.Target(part, label, []*tree.Node{connects})
	for i, n := range nodes {
		pos, err := w.connectPos(connects, nodes, i)
		if err != nil {
			return err
		}
		if err := w.core.Insert(part, pos, n, nil); err != nil {
			return err
		}
	}
	return nil
}

// connectPos returns the position for the i-th new Connect in connects:
// the end of the Connect slot for the first, right after the previous
// new Connect for the rest.
func (w *Workspace) connectPos(connects *tree.Node, nodes []*tree.Node, i int) (core.Position, error) {
	if i == 0 {
		return w.core.SlotEnd(connects, "v:Connects", vsdxNS.Name(vsd.ElemConnect), true, nil, nil)
	}
	return core.After(nodes[i-1], nil), nil
}

// SetText replaces a shape's own Text and its Character and Paragraph
// sections with emit.TextXML's bytes (sections placed by the
// corpus-derived child order). Chunk E1.
func (w *Workspace) SetText(shape string, sections, text []byte) error {
	a, err := vsd.Parse(shape)
	if err != nil {
		return fmt.Errorf("vsdx edit: %w", err)
	}
	if a.Kind != vsd.KindShape || a.InMaster {
		return fmt.Errorf("vsdx edit: %s: want a shape address (page0/s5)", shape)
	}
	page := vsd.Address{Kind: vsd.KindPage, Container: a.Container, PageDup: a.PageDup}
	_, part, d, err := w.pageTree(page)
	if err != nil {
		return err
	}
	shapeNode := w.shapeIndexFor(part, d.Root()).find(a.Shape, a.Dup)
	if shapeNode == nil {
		return fmt.Errorf("%w: %s", ErrNotFound, shape)
	}
	var sectionNodes []*tree.Node
	if len(bytes.TrimSpace(sections)) > 0 {
		parsed, err := w.core.Parse(shapeNode, sections, nil)
		if err != nil {
			return fmt.Errorf("vsdx edit: sections: %w", err)
		}
		for _, n := range parsed {
			if n.Kind() != tree.ElementNode || !isElem(n, vsd.ElemSection) {
				return errors.New("vsdx edit: sections fragment holds something other than Section elements")
			}
			sectionNodes = append(sectionNodes, n)
		}
	}
	var textNode *tree.Node
	if len(bytes.TrimSpace(text)) > 0 {
		parsed, err := w.core.Parse(shapeNode, text, nil)
		if err != nil {
			return fmt.Errorf("vsdx edit: text: %w", err)
		}
		if len(parsed) != 1 || parsed[0].Kind() != tree.ElementNode || !isElem(parsed[0], vsd.ElemText) {
			return errors.New("vsdx edit: text fragment is not a single Text element")
		}
		textNode = parsed[0]
	}
	h := w.core.Target(part, shape, []*tree.Node{shapeNode})
	if err := h.RemoveChildren(core.Path{}, func(c *tree.Node) bool {
		if !isElem(c, vsd.ElemSection) {
			return false
		}
		n, _ := c.Attr("N")
		return n == vsd.SectionCharacter || n == vsd.SectionParagraph
	}); err != nil {
		return err
	}
	for i, n := range sectionNodes {
		var pos core.Position
		if i == 0 {
			pos, err = w.core.SlotEnd(shapeNode, "v:Shape", vsdxNS.Name(vsd.ElemSection), false, nil, nil)
		} else {
			pos = core.After(sectionNodes[i-1], nil)
		}
		if err != nil {
			return err
		}
		if err := w.core.Insert(part, pos, n, nil); err != nil {
			return err
		}
	}
	if old := child(shapeNode, vsd.ElemText); old != nil && textNode == nil {
		if err := h.Remove(core.Path{vsdxNS.Name(vsd.ElemText)}); err != nil {
			return err
		}
	} else if old != nil {
		if err := h.ReplaceWith(old, textNode); err != nil {
			return err
		}
	} else if textNode != nil {
		pos, err := w.core.SlotEnd(shapeNode, "v:Shape", vsdxNS.Name(vsd.ElemText), true, nil, nil)
		if err != nil {
			return err
		}
		if err := w.core.Insert(part, pos, textNode, nil); err != nil {
			return err
		}
	}
	return nil
}

// SetCells sets shape-level or page-sheet cells (name, V, U, F; an
// empty F removes the formula) on the addressed shape or page sheet,
// creating Cell elements in child order. Chunk E1.
func (w *Workspace) SetCells(at string, cells []Cell) error {
	if len(cells) == 0 {
		return nil
	}
	for _, c := range cells {
		if c.N == "" {
			return errors.New("vsdx edit: a cell has no name")
		}
	}
	a, err := vsd.Parse(at)
	if err != nil {
		return fmt.Errorf("vsdx edit: %w", err)
	}
	switch a.Kind {
	case vsd.KindPage:
		return w.setPageSheetCells(at, a, cells)
	case vsd.KindShape:
		return w.setShapeCells(at, a, cells)
	default:
		return fmt.Errorf("vsdx edit: %s: want a page or shape address (page0, page0/s5)", at)
	}
}

// setShapeCells writes cells into the direct Cell children of a shape.
func (w *Workspace) setShapeCells(at string, a vsd.Address, cells []Cell) error {
	if a.InMaster {
		return fmt.Errorf("vsdx edit: %s: want a page shape address (page0/s5)", at)
	}
	page := vsd.Address{Kind: vsd.KindPage, Container: a.Container, PageDup: a.PageDup}
	_, part, d, err := w.pageTree(page)
	if err != nil {
		return err
	}
	shapeNode := w.shapeIndexFor(part, d.Root()).find(a.Shape, a.Dup)
	if shapeNode == nil {
		return fmt.Errorf("%w: %s", ErrNotFound, at)
	}
	h := w.core.Target(part, at, []*tree.Node{shapeNode})
	return writeCells(h, shapeNode, cells)
}

// setPageSheetCells writes cells into the PageSheet of a page in
// pages.xml (declared as a whole: it has no units).
func (w *Workspace) setPageSheetCells(at string, a vsd.Address, cells []Cell) error {
	if a.InMaster || w.pagesPart == "" {
		return fmt.Errorf("%w: %s", ErrNotFound, at)
	}
	part := w.pagesPart
	d, err := w.core.Tree(part)
	if err != nil {
		return &Unreadable{Target: at, Detail: err.Error()}
	}
	sheet := findPageSheet(d.Root(), a.Container, a.PageDup)
	if sheet == nil {
		return fmt.Errorf("%w: %s", ErrNotFound, at)
	}
	h := w.core.Target(part, at, []*tree.Node{sheet})
	return writeCells(h, sheet, cells)
}

// findPageSheet returns the PageSheet element of the pageDup-th Page
// with stored ID id in the pages.xml tree (the root's Page children,
// in stored order), or nil.
func findPageSheet(root *tree.Node, id uint32, dup int) *tree.Node {
	seen := 0
	for p := range root.ElementsSeq() {
		if !isElem(p, vsd.ElemPage) {
			continue
		}
		pid, ok := shapeID(p)
		if !ok || pid != id {
			continue
		}
		seen++
		if seen == dup {
			return child(p, vsd.ElemPageSheet)
		}
	}
	return nil
}

// writeCells sets cells on h's element el: an existing direct Cell
// with the name gets V, U and F (an empty U or F removes the
// attribute), a new one is appended at the end of the Cell slot in
// child order.
func writeCells(h *core.Handle, el *tree.Node, cells []Cell) error {
	for _, c := range cells {
		existing := findCell(el, c.N)
		if existing == nil {
			attrs := [][2]string{{"N", c.N}, {"V", c.V}}
			if c.U != "" {
				attrs = append(attrs, [2]string{"U", c.U})
			}
			if c.F != "" {
				attrs = append(attrs, [2]string{"F", c.F})
			}
			if err := h.AppendEach(core.Path{}, vsdxNS.Name(vsd.ElemCell), attrs); err != nil {
				return fmt.Errorf("vsdx edit: cell %s: %w", c.N, err)
			}
			continue
		}
		if err := h.SetNodeAttr(existing, "V", c.V); err != nil {
			return err
		}
		if err := setOrRemove(h, existing, "U", c.U); err != nil {
			return err
		}
		if err := setOrRemove(h, existing, "F", c.F); err != nil {
			return err
		}
	}
	return nil
}

// setOrRemove sets attribute name on e to value, removing it when
// value is "".
func setOrRemove(h *core.Handle, e *tree.Node, name, value string) error {
	if value == "" {
		return h.RemoveNodeAttr(e, name)
	}
	return h.SetNodeAttr(e, name, value)
}

// findCell returns the first direct Cell child of el with N equal to
// name, or nil.
func findCell(el *tree.Node, name string) *tree.Node {
	for c := range el.ElementsSeq() {
		if !isElem(c, vsd.ElemCell) {
			continue
		}
		if n, _ := c.Attr("N"); n == name {
			return c
		}
	}
	return nil
}

// Cell is one cell a write sets.
type Cell struct {
	N, V, U, F string
}
