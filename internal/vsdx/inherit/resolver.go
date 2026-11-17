// Package inherit resolves a shape's effective cells: the shape's own
// sheet, then its master shape's, then the style sheets of the cell's
// category (line, fill, text) up their parent chain, as Visio does
// (format notes section 2; docs/vsdx-design.md, decision a). Every
// result says where it came from. Nothing is defaulted: a cell no
// sheet stores is absent.
package inherit

import (
	"strconv"

	"github.com/jhnoel/documentdriver/internal/ooxml/tree"
	"github.com/jhnoel/documentdriver/internal/vsdx/doc"
	"github.com/jhnoel/documentdriver/internal/vsdx/sheet"
	"github.com/jhnoel/documentdriver/internal/vsdx/vsd"
)

// Origin is where an effective cell came from, as read --detail full
// shows it in origins.
type Origin string

// Origins.
const (
	// Local: the shape stores the cell with its own formula or value.
	Local Origin = "local"
	// Inherited: the shape stores the value but its formula is F="Inh";
	// the value is the cached result of the master's or style's formula.
	Inherited Origin = "inherited"
	// Master: the shape does not store the cell; its master shape does.
	Master Origin = "master"
	// Style: neither stores it; a style sheet on the chain does.
	Style Origin = "style"
)

// MaxStyleDepth bounds a style sheet's parent chain; a longer chain or
// a cycle stops there and the cell is absent with a note (fixture V3).
const MaxStyleDepth = 32

// Shape is what the resolver needs of one shape: its sheet, its master
// and master shape IDs as stored, and its own style attributes.
type Shape struct {
	Sheet       *sheet.Sheet
	Master      uint32 // the top-level instance's Master; inherited by its sub-shapes
	HasMaster   bool
	MasterShape uint32
	HasMShape   bool
	Styles      [3]uint32 // line, fill, text, by vsd.Category-1
	HasStyle    [3]bool
}

// Resolver resolves cells against one document. It indexes each master
// once (map from master shape ID to sheet) and memoises each style's
// chain, so a page of n shapes resolves in O(n) lookups.
type Resolver struct {
	doc     *doc.Document
	masters map[uint32]*masterIndex
	styles  map[uint32]*sheet.Sheet
	notes   []doc.Note
}

// masterIndex is one master's shapes by stored ID, and its top shape
// (the shape an instance without MasterShape inherits from).
type masterIndex struct {
	shapes map[uint32]*Shape
	top    *Shape
}

// New returns a resolver for d.
func New(d *doc.Document) *Resolver {
	return &Resolver{doc: d, masters: map[uint32]*masterIndex{}, styles: map[uint32]*sheet.Sheet{}}
}

// Cell returns the effective cell at k for s and its origin, or false
// when no sheet on the chain stores it or a Del="1" section or row
// blocks it. A master that cannot be read is a note and the chain goes
// on to the styles. Chunk H2.
func (r *Resolver) Cell(s *Shape, k sheet.Key) (sheet.Cell, Origin, bool) {
	category := vsd.StyleCategory(k.Section, k.Cell)
	if s == nil {
		if r == nil {
			return sheet.Cell{}, "", false
		}
		if c, ok := r.styleCell(category, 0, k); ok {
			return c, Style, true
		}
		return sheet.Cell{}, "", false
	}
	if sh := s.Sheet; sh != nil {
		if sh.Deleted(k) {
			return sheet.Cell{}, "", false
		}
		if c, ok := sh.Cell(k); ok {
			if c.Inherited() {
				return c, Inherited, true
			}
			return c, Local, true
		}
	}
	var master *Shape
	if ms, ok := r.MasterShape(s); ok {
		master = ms
	}
	if master != nil && master.Sheet != nil {
		if master.Sheet.Deleted(k) {
			return sheet.Cell{}, "", false
		}
		if c, ok := master.Sheet.Cell(k); ok {
			return c, Master, true
		}
	}
	if category == vsd.NoStyle || r == nil {
		return sheet.Cell{}, "", false
	}
	id, ok := styleStart(s, master, category)
	if !ok {
		return sheet.Cell{}, "", false
	}
	if c, ok := r.styleCell(category, id, k); ok {
		return c, Style, true
	}
	return sheet.Cell{}, "", false
}

// styleStart returns the style sheet ID a shape's chain of category c
// starts from: the shape's own style attribute for that category, or
// its master shape's when the shape has none. Page cells start from
// style 0. It reports false when neither has a style for c.
func styleStart(s, master *Shape, c vsd.Category) (uint32, bool) {
	if c == vsd.PageCategory {
		return 0, true
	}
	if c < vsd.LineCategory || c > vsd.TextCategory {
		return 0, false
	}
	i := int(c) - 1
	if s != nil && s.HasStyle[i] {
		return s.Styles[i], true
	}
	if master != nil && master.HasStyle[i] {
		return master.Styles[i], true
	}
	return 0, false
}

// MasterShape returns the master shape s inherits from: s's MasterShape
// in its master, or the master's top shape when s has a Master and no
// MasterShape. Chunk H2.
func (r *Resolver) MasterShape(s *Shape) (*Shape, bool) {
	if r == nil || s == nil || !s.HasMaster {
		return nil, false
	}
	idx := r.master(s.Master)
	if idx == nil {
		return nil, false
	}
	if s.HasMShape {
		sh, ok := idx.shapes[s.MasterShape]
		return sh, ok && sh != nil
	}
	if idx.top != nil {
		return idx.top, true
	}
	return nil, false
}

// master returns the index of master id, building it on first use and
// caching it, failures included, so every lookup of one master costs
// one map entry and reports one note at most.
func (r *Resolver) master(id uint32) *masterIndex {
	if idx, ok := r.masters[id]; ok {
		return idx
	}
	idx := r.buildMaster(id)
	if r.masters == nil {
		r.masters = map[uint32]*masterIndex{}
	}
	r.masters[id] = idx
	return idx
}

// buildMaster indexes every shape of master id, including the shapes
// nested in its groups, in document order. A master that cannot be
// read is one note and a nil index.
func (r *Resolver) buildMaster(id uint32) *masterIndex {
	if r.doc == nil {
		return nil
	}
	contents, err := r.doc.MasterContents(id)
	if err != nil {
		at := vsd.MasterID(id)
		if _, ok := r.doc.Master(id); !ok {
			r.notes = append(r.notes, doc.Note{At: at, Reason: "no such master"})
		} else {
			r.notes = append(r.notes, doc.Note{At: at, Reason: "unreadable: " + err.Error()})
		}
		return nil
	}
	idx := &masterIndex{shapes: map[uint32]*Shape{}}
	root := contents.Root()
	if root == nil {
		return idx
	}
	shapes := shapesChild(root)
	if shapes == nil {
		return idx
	}
	stack := pushShapes(nil, shapes)
	for len(stack) > 0 {
		node := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		sh, shapeID, hasID := masterShape(node)
		if idx.top == nil {
			idx.top = sh
		}
		if hasID {
			if _, dup := idx.shapes[shapeID]; !dup {
				idx.shapes[shapeID] = sh
			}
		}
		if nested := shapesChild(node); nested != nil {
			stack = pushShapes(stack, nested)
		}
	}
	return idx
}

// masterShape builds the Shape of one master shape element, with its
// sheet and its style attributes, and returns its stored ID.
func masterShape(node *tree.Node) (*Shape, uint32, bool) {
	sh := &Shape{}
	sh.Sheet, _ = sheet.Index(node)
	var id uint32
	hasID := false
	for name, value := range node.AttrsSeq() {
		switch name {
		case "ID":
			if u, err := strconv.ParseUint(value, 10, 32); err == nil {
				id, hasID = uint32(u), true
			}
		case "LineStyle":
			if u, err := strconv.ParseUint(value, 10, 32); err == nil {
				sh.Styles[0], sh.HasStyle[0] = uint32(u), true
			}
		case "FillStyle":
			if u, err := strconv.ParseUint(value, 10, 32); err == nil {
				sh.Styles[1], sh.HasStyle[1] = uint32(u), true
			}
		case "TextStyle":
			if u, err := strconv.ParseUint(value, 10, 32); err == nil {
				sh.Styles[2], sh.HasStyle[2] = uint32(u), true
			}
		}
	}
	return sh, id, hasID
}

// shapesChild returns the first direct Shapes child of node, or nil.
func shapesChild(node *tree.Node) *tree.Node {
	for child := range node.ElementsSeq() {
		if child.Local() == vsd.ElemShapes {
			return child
		}
	}
	return nil
}

// pushShapes appends the Shape children of shapes to stack in reverse
// document order, so popping yields them in document order. It walks
// the sibling links, building no list of a node's children.
func pushShapes(stack []*tree.Node, shapes *tree.Node) []*tree.Node {
	for n := shapes.LastChild(); n != nil; n = n.PrevSibling() {
		if n.Kind() == tree.ElementNode && n.Local() == vsd.ElemShape {
			stack = append(stack, n)
		}
	}
	return stack
}

// Doc returns the document the resolver reads, for colour and face
// resolution by the model.
func (r *Resolver) Doc() *doc.Document { return r.doc }

// Notes returns what the resolver could not read (a master part, a
// style cycle), each once.
func (r *Resolver) Notes() []doc.Note { return r.notes }
