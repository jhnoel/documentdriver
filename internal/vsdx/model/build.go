package model

import (
	"fmt"
	"strconv"

	"github.com/jhnoel/documentdriver/internal/ooxml/tree"
	"github.com/jhnoel/documentdriver/internal/vsdx/doc"
	"github.com/jhnoel/documentdriver/internal/vsdx/inherit"
	"github.com/jhnoel/documentdriver/internal/vsdx/place"
	"github.com/jhnoel/documentdriver/internal/vsdx/sheet"
	"github.com/jhnoel/documentdriver/internal/vsdx/vsd"
)

// Options select what the builder derives beyond the default view:
// Sites adds connection sites; Full adds raw cells (and, for a 1-D
// shape, its box). Origins are derived in every view.
//
// ReleaseNodes and Checkpoint are chunk RM2's (fields landed by the
// designer so RM2 adds no field to a file it does not own). With
// ReleaseNodes set, BuildPage sets every Object.Node to nil once the
// glue pass is done, so the page tree can be collected before the
// caller marshals; it is opt-in (read's CLI path sets it), and every
// other caller (read.XML, edit, the model tests) keeps the nodes.
// Checkpoint, when not nil, is called with "walk" after the walk and
// "link" after the glue pass: the memory gate's mark.
type Options struct {
	Sites, Full  bool
	ReleaseNodes bool
	Checkpoint   func(name string)
}

// BuildPage builds the model of page e of d. The page's contents are
// parsed once and walked iteratively (a stack of group frames, so the
// depth is the tokenizer's cap, never the Go stack); each shape is
// indexed once and resolved through r. An object the builder cannot
// model is kept with Kind other and an omission; it never fails the
// page. Chunk M1 (walk, kinds, addresses, z, the placed map); the
// per-object values are M2a to M2c, M3, M5 to M7; glue is M4.
func BuildPage(d *doc.Document, r *inherit.Resolver, e *doc.PageEntry, o Options) (*Page, error) {
	if d == nil {
		return nil, fmt.Errorf("vsdx: no document")
	}
	if e == nil {
		return nil, fmt.Errorf("vsdx: no page")
	}
	p := &Page{}
	pageFacts(p, d, r, e)
	container := vsd.PageID(e.ID, pageDup(d, e))
	p.ID = container
	contents, err := d.PageContents(e)
	if err != nil {
		return nil, err
	}
	return build(p, d, r, contents, container, e.Part, o)
}

// BuildMaster builds the model of master m, read only, as BuildPage.
// The master is shown in the frame of its own PageSheet (chunk NW):
// masterFacts sets its page height from that sheet, and a master
// without PageHeight leaves box, from and to out, with a note.
// Chunk M1; the frame is NW.
func BuildMaster(d *doc.Document, r *inherit.Resolver, m *doc.MasterEntry, o Options) (*Page, error) {
	if d == nil {
		return nil, fmt.Errorf("vsdx: no document")
	}
	if m == nil {
		return nil, fmt.Errorf("vsdx: no master")
	}
	p := &Page{Name: m.Name, NameU: m.NameU}
	container := vsd.MasterID(m.ID)
	p.ID = container
	hasHeight := masterFacts(p, r, m)
	contents, err := d.MasterContents(m.ID)
	if err != nil {
		return nil, err
	}
	p, err = build(p, d, r, contents, container, m.Part, o)
	if err != nil {
		return nil, err
	}
	if !hasHeight {
		for _, obj := range p.Objects {
			obj.Box, obj.From, obj.To = nil, nil, nil
		}
	}
	return p, nil
}

// build walks the contents of container into p: the Shapes of the root
// in document order, a group before its children, and the root's
// Connects to linkConnects. It reports an error only when the contents
// have no root; a shape it cannot address is a note and its children
// are still walked.
func build(p *Page, d *doc.Document, r *inherit.Resolver, contents *tree.Document, container, part string, o Options) (*Page, error) {
	if contents == nil || contents.Root() == nil {
		return nil, fmt.Errorf("vsdx: %s has no contents", container)
	}
	shapes, connects, _ := directChildren(contents.Root())
	w := &walker{
		p:         p,
		d:         d,
		r:         r,
		container: container,
		part:      part,
		o:         o,
		seen:      map[uint32]int{},
		placed:    map[uint32]*placed{},
	}
	w.stack = pushShapes(nil, shapes, "", place.Page, 0, false)
	for len(w.stack) > 0 {
		fr := w.stack[len(w.stack)-1]
		w.stack = w.stack[:len(w.stack)-1]
		w.step(fr)
	}
	if o.Checkpoint != nil {
		o.Checkpoint("walk")
	}
	linkConnects(p, r, connects, w.placed, p.H)
	offPage(p)
	if o.ReleaseNodes {
		for _, obj := range p.Objects {
			obj.Node = nil
		}
		// The shapes and Connects locals and the contents document
		// die with this branch (their last uses are the walk and
		// the glue pass), so with the nodes gone the page tree
		// itself can be collected before the caller marshals; the
		// walker's stack is dropped with it.
		w.stack = nil
	}
	if o.Checkpoint != nil {
		o.Checkpoint("link")
	}
	return p, nil
}

// frame is one shape waiting to be walked, with the address of its
// group ("" at the top level), the frame of the space it sits in and
// the master a group instance above it carries.
type frame struct {
	node      *tree.Node
	parent    string
	f         place.Frame
	master    uint32
	hasMaster bool
}

// placedBlockSize is how many placed values one pool block holds:
// a block is one allocation for that many shapes, instead of one
// allocation per shape.
const placedBlockSize = 1024

// walker walks one page or master contents into its Page. In a
// VisitPage walk (visit non-nil) it builds no placed map and keeps
// no objects: each built object goes to visit and is dropped, ids
// records the stored IDs objects were built for (the Connects notes
// pass), count is the next object's Z, and stop says visit returned
// false and the walk must end.
type walker struct {
	p         *Page
	d         *doc.Document
	r         *inherit.Resolver
	container string
	o         Options
	seen      map[uint32]int
	placed    map[uint32]*placed
	blocks    [][]placed
	poolUsed  int
	stack     []frame
	visit     func(*Object) bool
	ids       map[uint32]bool
	count     int
	stop      bool
	part      string                 // the contents part, for picture relationships
	media     map[string]*mediaFacts // media facts by part, read once per walk
}

// place records obj in the placed map, taking its placed value from
// the walker's pool blocks so placing a page costs one allocation
// per block rather than one per shape. A block's array never moves
// once taken, so the pointers stay valid for the glue pass.
func (w *walker) place(obj *Object, f place.Frame, master uint32, hasMaster bool, id uint32) {
	if w.poolUsed == 0 || w.poolUsed == len(w.blocks[len(w.blocks)-1]) {
		w.blocks = append(w.blocks, make([]placed, placedBlockSize))
		w.poolUsed = 0
	}
	pl := &w.blocks[len(w.blocks)-1][w.poolUsed]
	*pl = placed{obj: obj, frame: f, master: master, hasMaster: hasMaster}
	w.poolUsed++
	w.placed[id] = pl
}

// step builds the object of one shape frame and pushes its nested
// shapes on the stack. A shape with no usable ID is noted and skipped
// as an object; its children still walk, in this frame.
func (w *walker) step(fr frame) {
	node := fr.node
	head := readShapeHead(node, fr.master, fr.hasMaster)
	shapes, _, foreign := directChildren(node)
	if !head.ok {
		w.p.Notes = append(w.p.Notes, doc.Note{At: w.container, Reason: "shape id does not parse"})
		w.stack = pushShapes(w.stack, shapes, "", fr.f, fr.master, fr.hasMaster)
		return
	}
	w.seen[head.id]++
	addr := vsd.ShapeID(w.container, head.id, w.seen[head.id])
	_, begin := sheetCell(head.s, sheet.ShapeCell(vsd.CellBeginX))
	obj := &Object{
		ID:      addr,
		ShapeID: head.id,
		Kind:    kindOf(head.typ, shapes, foreign, begin),
		Type:    head.typ,
		Name:    head.name,
		NameU:   head.nameU,
		Parent:  fr.parent,
		Z:       len(w.p.Objects),
		Node:    node,
	}
	if head.s.HasMaster {
		obj.Master = vsd.MasterID(head.s.Master)
		obj.MasterName = masterName(w.d, head.s.Master)
	}
	if head.hasStored {
		obj.StoredMaster = vsd.MasterID(head.stored)
	}
	fillObject(obj, w.r, head.s, fr.f, w.p.H, w.o)
	if obj.Kind == Picture {
		if w.media == nil {
			w.media = map[string]*mediaFacts{}
		}
		pictureOf(obj, w.p, w.d, w.r, head.s, w.part, foreign, w.media)
	}
	layerValue := ""
	if cell, _, ok := w.r.Cell(head.s, sheet.ShapeCell(vsd.CellLayerMember)); ok {
		layerValue = cell.V
	}
	obj.Layers = layerMembers(w.p, layerValue)
	if w.o.Full {
		obj.Cells = rawCells(head.s.Sheet)
	}
	t, _, ok := transformOf(w.r, head.s)
	if w.visit != nil {
		w.ids[head.id] = true
	} else if _, dup := w.placed[head.id]; !dup {
		// placed keeps no indexed sheet: the walk releases head.s
		// with this step, and linkConnects re-indexes on demand
		// only the shapes a Connect names (indexShape of the
		// placed node, with the effective master carried here).
		w.place(obj, fr.f, head.s.Master, head.s.HasMaster, head.id)
	}
	if w.visit != nil {
		obj.Z = w.count
		w.count++
		if !w.visit(obj) {
			w.stop = true
		}
	} else {
		w.p.Objects = append(w.p.Objects, obj)
	}
	childFrame := fr.f
	if ok {
		childFrame = fr.f.Child(t)
	}
	childMaster, childHasMaster := fr.master, fr.hasMaster
	if head.s.HasMaster {
		childMaster, childHasMaster = head.s.Master, true
	}
	if !w.stop {
		w.stack = pushShapes(w.stack, shapes, addr, childFrame, childMaster, childHasMaster)
	}
}

// shapeHead is what the walk reads from a shape element's attributes:
// its stored ID (ok false when it does not parse) and its resolver
// shape, plus the attributes the object shows. stored and hasStored
// are the element's own Master attribute (never the master carried
// from a group instance), for Object.StoredMaster.
type shapeHead struct {
	id        uint32
	ok        bool
	stored    uint32
	hasStored bool
	typ       string
	name      string
	nameU     string
	s         *inherit.Shape
}

// readShapeHead builds the shapeHead of one shape element: the shape
// indexed once, with Master and MasterShape and the style attributes
// as stored, carrying the master the enclosing group instance carries
// when the element stores no Master of its own.
func readShapeHead(node *tree.Node, master uint32, hasMaster bool) shapeHead {
	head := shapeHead{s: &inherit.Shape{Master: master, HasMaster: hasMaster}}
	head.s.Sheet, _ = sheet.Index(node)
	for name, value := range node.AttrsSeq() {
		switch name {
		case "ID":
			if u, err := strconv.ParseUint(value, 10, 32); err == nil {
				head.id, head.ok = uint32(u), true
			}
		case "Master":
			if u, err := strconv.ParseUint(value, 10, 32); err == nil {
				head.s.Master, head.s.HasMaster = uint32(u), true
				head.stored, head.hasStored = uint32(u), true
			}
		case "MasterShape":
			if u, err := strconv.ParseUint(value, 10, 32); err == nil {
				head.s.MasterShape, head.s.HasMShape = uint32(u), true
			}
		case "LineStyle":
			if u, err := strconv.ParseUint(value, 10, 32); err == nil {
				head.s.Styles[0], head.s.HasStyle[0] = uint32(u), true
			}
		case "FillStyle":
			if u, err := strconv.ParseUint(value, 10, 32); err == nil {
				head.s.Styles[1], head.s.HasStyle[1] = uint32(u), true
			}
		case "TextStyle":
			if u, err := strconv.ParseUint(value, 10, 32); err == nil {
				head.s.Styles[2], head.s.HasStyle[2] = uint32(u), true
			}
		case "Type":
			head.typ = value
		case "Name":
			head.name = value
		case "NameU":
			head.nameU = value
		}
	}
	return head
}

// directChildren returns the first Shapes, Connects and ForeignData
// element children of node (nil when none), in one pass over its
// element children.
func directChildren(node *tree.Node) (shapes, connects, foreign *tree.Node) {
	for child := range node.ElementsSeq() {
		switch child.Local() {
		case vsd.ElemShapes:
			if shapes == nil {
				shapes = child
			}
		case vsd.ElemConnects:
			if connects == nil {
				connects = child
			}
		case vsd.ElemForeignData:
			if foreign == nil {
				foreign = child
			}
		}
	}
	return shapes, connects, foreign
}

// pushShapes appends frames for the Shape children of shapes to stack
// in reverse document order, so popping yields them in document order.
// It walks the sibling links, building no list of a node's children.
func pushShapes(stack []frame, shapes *tree.Node, parent string, f place.Frame, master uint32, hasMaster bool) []frame {
	if shapes == nil {
		return stack
	}
	for n := shapes.LastChild(); n != nil; n = n.PrevSibling() {
		if n.Kind() == tree.ElementNode && n.Local() == vsd.ElemShape {
			stack = append(stack, frame{node: n, parent: parent, f: f, master: master, hasMaster: hasMaster})
		}
	}
	return stack
}

// kindOf returns the object kind of a shape with stored type typ, a
// Shapes child (shapes, nil when none), a ForeignData child (foreign,
// nil when none) and a stored BeginX cell (begin): a group when it
// holds shapes, a picture when it is a Foreign image, other when it is
// a guide or a non-image Foreign, a connector when it stores BeginX,
// and a shape otherwise.
func kindOf(typ string, shapes, foreign *tree.Node, begin bool) Kind {
	switch {
	case typ == vsd.ShapeTypeGroup || shapes != nil:
		return Group
	case typ == vsd.ShapeTypeForeign:
		if isPicture(foreign) {
			return Picture
		}
		return Other
	case typ == vsd.ShapeTypeGuide:
		return Other
	case begin:
		return Connector
	default:
		return Shape
	}
}

// isPicture reports whether a ForeignData element holds an image: a
// bitmap or metafile ForeignType with a Rel, as the format notes list
// the ForeignType values. A nil element, an OLE Object or ink, or an
// image with no target is not a picture.
func isPicture(foreign *tree.Node) bool {
	if foreign == nil {
		return false
	}
	typ, _ := foreign.Attr("ForeignType")
	switch typ {
	case "Bitmap", "Metafile", "EnhMetaFile":
	default:
		return false
	}
	for child := range foreign.ElementsSeq() {
		if child.Local() == vsd.ElemRel {
			return true
		}
	}
	return false
}

// sheetCell returns the cell at k of s's sheet, or false when s or
// its sheet is nil.
func sheetCell(s *inherit.Shape, k sheet.Key) (sheet.Cell, bool) {
	if s == nil || s.Sheet == nil {
		return sheet.Cell{}, false
	}
	return s.Sheet.Cell(k)
}

// masterName returns the name of master id in d, "" when d or the
// master is absent.
func masterName(d *doc.Document, id uint32) string {
	if d == nil {
		return ""
	}
	if m, ok := d.Master(id); ok {
		return m.Name
	}
	return ""
}

// pageDup returns the 1-based occurrence of e among d.Pages with its
// ID: 1 for the first, 2 for the next sharing it. An entry that is not
// one of d.Pages is the first of its ID.
func pageDup(d *doc.Document, e *doc.PageEntry) int {
	dup := 1
	for i := range d.Pages {
		if &d.Pages[i] == e {
			return dup
		}
		if d.Pages[i].ID == e.ID {
			dup++
		}
	}
	return 1
}

// indexShape indexes node once and builds its resolver shape with
// the master the walk carried (master, hasMaster) as the default,
// then the Master, MasterShape and style attributes the node stores
// itself: the same shape readShapeHead builds, without the display
// attributes the glue pass never reads.
func indexShape(node *tree.Node, master uint32, hasMaster bool) *inherit.Shape {
	s := &inherit.Shape{Master: master, HasMaster: hasMaster}
	s.Sheet, _ = sheet.Index(node)
	if v, ok := node.Attr("Master"); ok {
		if u, err := strconv.ParseUint(v, 10, 32); err == nil {
			s.Master, s.HasMaster = uint32(u), true
		}
	}
	if v, ok := node.Attr("MasterShape"); ok {
		if u, err := strconv.ParseUint(v, 10, 32); err == nil {
			s.MasterShape, s.HasMShape = uint32(u), true
		}
	}
	for i, name := range [...]string{"LineStyle", "FillStyle", "TextStyle"} {
		if v, ok := node.Attr(name); ok {
			if u, err := strconv.ParseUint(v, 10, 32); err == nil {
				s.Styles[i], s.HasStyle[i] = uint32(u), true
			}
		}
	}
	return s
}
