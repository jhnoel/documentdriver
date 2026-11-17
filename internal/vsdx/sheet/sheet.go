// Package sheet is the ShapeSheet of one shape, page sheet, master
// shape or style sheet as stored: its direct Cell children and its
// Sections, Rows and their Cells, indexed by Key in one walk of the
// element's own children (never a nested Shapes). It holds node
// pointers into the part's tree and copies no bytes. It does not
// inherit: that is package inherit.
package sheet

import (
	"strconv"

	"github.com/jhnoel/documentdriver/internal/ooxml/tree"
	"github.com/jhnoel/documentdriver/internal/vsdx/vsd"
)

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
	Node     *tree.Node
	cells    map[Key]Cell
	deleted  map[Key]bool // sections and rows with Del="1", keyed with Cell ""
	rows     map[string][]Key
	seenRows map[Key]bool // row keys already listed in rows, built once per Index
}

// Index walks the direct children of el (a Shape, PageSheet or
// StyleSheet) and their Sections and Rows, and returns its sheet. A
// Section or Row without the attributes its key needs, and a repeated
// key (the first one wins), are skipped and reported in skipped, which
// the caller turns into notes. Allocation is one map entry per cell.
// Chunk H1.
func Index(el *tree.Node) (s *Sheet, skipped []string) {
	s = &Sheet{Node: el, cells: make(map[Key]Cell, 32)}
	if el == nil {
		return s, nil
	}
	for child := range el.ElementsSeq() {
		switch child.Local() {
		case vsd.ElemCell:
			cell, ok := readCell(child)
			if !ok {
				skipped = append(skipped, "cell without N")
				continue
			}
			skipped = s.addCell(cell, ShapeCell(cell.N), skipped)
		case vsd.ElemSection:
			skipped = s.indexSection(child, skipped)
		}
	}
	return s, skipped
}

// indexSection indexes one Section element of s and returns the updated
// skipped list. A Section without a name, or with an IX that is not a
// non-negative integer, is skipped with its contents.
func (s *Sheet) indexSection(section *tree.Node, skipped []string) []string {
	name, ok := section.Attr("N")
	if !ok || name == "" {
		return append(skipped, "section without N")
	}
	secIX := -1
	if v, ok := section.Attr("IX"); ok {
		ix, ok := parseIX(v)
		if !ok {
			return append(skipped, "section "+name+": bad IX "+v)
		}
		secIX = ix
	}
	if v, ok := section.Attr("Del"); ok && v == "1" {
		if s.deleted == nil {
			s.deleted = make(map[Key]bool)
		}
		s.deleted[Key{Section: name, SectionIX: secIX, RowIX: -1}] = true
	}
	for child := range section.ElementsSeq() {
		switch child.Local() {
		case vsd.ElemCell:
			cell, ok := readCell(child)
			if !ok {
				skipped = append(skipped, "cell without N in section "+name)
				continue
			}
			key := Key{Section: name, SectionIX: secIX, RowIX: -1, Cell: cell.N}
			skipped = s.addCell(cell, key, skipped)
		case vsd.ElemRow:
			skipped = s.indexRow(child, name, secIX, skipped)
		}
	}
	return skipped
}

// indexRow indexes one Row element of the section named section with
// section index secIX, and returns the updated skipped list. A Row with
// neither a name nor an index is skipped with its cells.
func (s *Sheet) indexRow(row *tree.Node, section string, secIX int, skipped []string) []string {
	rowName, rowIX, ok := rowIdentity(row)
	if !ok {
		return append(skipped, "row without N or IX in section "+section)
	}
	rowKey := Key{Section: section, SectionIX: secIX, Row: rowName, RowIX: rowIX}
	if v, ok := row.Attr("Del"); ok && v == "1" {
		if s.deleted == nil {
			s.deleted = make(map[Key]bool)
		}
		s.deleted[rowKey] = true
	}
	if s.seenRows == nil {
		s.seenRows = make(map[Key]bool)
	}
	if !s.seenRows[rowKey] {
		s.seenRows[rowKey] = true
		if s.rows == nil {
			s.rows = make(map[string][]Key)
		}
		s.rows[section] = append(s.rows[section], rowKey)
	}
	for child := range row.ElementsSeq() {
		if child.Local() != vsd.ElemCell {
			continue
		}
		cell, ok := readCell(child)
		if !ok {
			skipped = append(skipped, "cell without N in section "+section)
			continue
		}
		skipped = s.addCell(cell, rowKey.WithCell(cell.N), skipped)
	}
	return skipped
}

// addCell inserts cell at key into s, keeping the first cell stored at
// a key and reporting a repeated key in skipped.
func (s *Sheet) addCell(cell Cell, key Key, skipped []string) []string {
	if _, dup := s.cells[key]; dup {
		return append(skipped, "duplicate cell "+key.Section+"/"+strconv.Itoa(key.SectionIX)+"/"+key.Row+"/"+strconv.Itoa(key.RowIX)+"/"+key.Cell)
	}
	s.cells[key] = cell
	return skipped
}

// readCell returns the cell stored by node and whether it has a name. An
// absent V or F leaves the corresponding Has flag clear, so an empty
// stored value stays distinct from an absent one.
func readCell(node *tree.Node) (Cell, bool) {
	name, ok := node.Attr("N")
	if !ok || name == "" {
		return Cell{}, false
	}
	cell := Cell{N: name, Node: node}
	if v, ok := node.Attr("V"); ok {
		cell.V, cell.HasV = v, true
	}
	if v, ok := node.Attr("U"); ok {
		cell.U = v
	}
	if v, ok := node.Attr("F"); ok {
		cell.F, cell.HasF = v, true
	}
	if v, ok := node.Attr("E"); ok {
		cell.E = v
	}
	return cell, true
}

// rowIdentity returns the name and index of a Row element: a named row
// has its name and index -1, an indexed row has an empty name and its
// index. It reports false when the row has neither a non-empty name nor
// a usable index.
func rowIdentity(row *tree.Node) (name string, ix int, ok bool) {
	if n, ok := row.Attr("N"); ok && n != "" {
		return n, -1, true
	}
	if v, ok := row.Attr("IX"); ok {
		if ix, ok := parseIX(v); ok {
			return "", ix, true
		}
	}
	return "", 0, false
}

// parseIX parses an IX attribute value, reporting false for anything
// that is not a non-negative decimal integer.
func parseIX(v string) (int, bool) {
	ix, err := strconv.Atoi(v)
	if err != nil || ix < 0 {
		return 0, false
	}
	return ix, true
}

// Cell returns the stored cell at k.
func (s *Sheet) Cell(k Key) (Cell, bool) {
	c, ok := s.cells[k]
	return c, ok
}

// Deleted reports whether the section or row of k is stored with
// Del="1" here, which blocks inheritance of it (format notes section 2).
// A section deletion is keyed with RowIX -1, so it never collides with
// row zero.
func (s *Sheet) Deleted(k Key) bool {
	return s.deleted[Key{Section: k.Section, SectionIX: k.SectionIX, RowIX: -1}] ||
		s.deleted[Key{Section: k.Section, SectionIX: k.SectionIX, Row: k.Row, RowIX: k.RowIX}]
}

// Rows returns the row keys (Cell "") of every section of that name in
// stored order, so Geometry rows of different section IX are mixed; each
// key carries its SectionIX.
func (s *Sheet) Rows(section string) []Key { return s.rows[section] }
