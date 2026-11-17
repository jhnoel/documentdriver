// Package inherit resolves a shape's effective cells: the shape's own
// sheet, then its master shape's, then the style sheets of the cell's
// category (line, fill, text) up their parent chain, as Visio does
// (format notes section 2; docs/vsdx-design.md, decision a). Every
// result says where it came from. Nothing is defaulted: a cell no
// sheet stores is absent.
package inherit

import (
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
	if c, ok := r.styleCell(vsd.StyleCategory(k.Section, k.Cell), 0, k); ok && s == nil {
		return c, Style, true
	}
	return sheet.Cell{}, "", false
}

// MasterShape returns the master shape s inherits from: s's MasterShape
// in its master, or the master's top shape when s has a Master and no
// MasterShape. Chunk H2.
func (r *Resolver) MasterShape(s *Shape) (*Shape, bool) {
	if mi := r.masters[s.Master]; mi != nil && mi.shapes != nil {
		return mi.top, mi.top != nil
	}
	return nil, false
}

// Notes returns what the resolver could not read (a master part, a
// style cycle), each once.
func (r *Resolver) Notes() []doc.Note { return r.notes }

// styleCell walks the style chain of category c from style id, at most
// MaxStyleDepth steps. Chunk H3.
func (r *Resolver) styleCell(c vsd.Category, id uint32, k sheet.Key) (sheet.Cell, bool) {
	_, _, _ = c, id, k
	return sheet.Cell{}, false
}
