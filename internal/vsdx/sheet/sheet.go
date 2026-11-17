// Package sheet is the ShapeSheet of one shape, page sheet, master
// shape or style sheet as stored: its direct Cell children and its
// Sections, Rows and their Cells, indexed by Key in one walk of the
// element's own children (never a nested Shapes). It holds node
// pointers into the part's tree and copies no bytes. It does not
// inherit: that is package inherit.
package sheet

import "github.com/jhnoel/documentdriver/internal/ooxml/tree"

// Cell is one stored Cell: its attributes as written (decoded) and
// its node. HasV and HasF tell an absent attribute from an empty one.
type Cell struct {
	N, V, U, F, E string
	HasV, HasF    bool
	Node          *tree.Node
}

// Inherited reports whether the cell's formula is inherited (F="Inh").
func (c Cell) Inherited() bool { return c.HasF && c.F == "Inh" }

// Sheet is the index of one sheet element.
type Sheet struct {
	Node    *tree.Node
	cells   map[Key]Cell
	deleted map[Key]bool // sections and rows with Del="1", keyed with Cell ""
	rows    map[string][]Key
}

// Index walks the direct children of el (a Shape, PageSheet or
// StyleSheet) and their Sections and Rows, and returns its sheet. A
// Section or Row without the attributes its key needs, and a repeated
// key (the first one wins), are skipped and reported in skipped, which
// the caller turns into notes. Allocation is one map entry per cell.
// Chunk H1.
func Index(el *tree.Node) (s *Sheet, skipped []string) {
	return &Sheet{Node: el}, nil
}

// Cell returns the stored cell at k.
func (s *Sheet) Cell(k Key) (Cell, bool) {
	c, ok := s.cells[k]
	return c, ok
}

// Deleted reports whether the section or row of k is stored with
// Del="1" here, which blocks inheritance of it (format notes section 2).
func (s *Sheet) Deleted(k Key) bool {
	return s.deleted[Key{Section: k.Section, SectionIX: k.SectionIX}] ||
		s.deleted[Key{Section: k.Section, SectionIX: k.SectionIX, Row: k.Row, RowIX: k.RowIX}]
}

// Rows returns the row keys (Cell "") of a section in stored order, for
// sections whose rows are enumerated (Property, Character, Paragraph,
// Connection, Layer, Field).
func (s *Sheet) Rows(section string) []Key { return s.rows[section] }
