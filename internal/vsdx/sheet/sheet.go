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

// cellEntry is one stored cell and the key it is stored under, in
// document order.
type cellEntry struct {
	key  Key
	cell Cell
}

// mapThreshold is the number of cells or rows above which a sheet
// builds a lookup map for them; at or below it Cell scans the entries
// and row listing scans the rows of the section.
const mapThreshold = 48

// rowGroup is the row keys of one section name, in stored order.
type rowGroup struct {
	section string
	keys    []Key
}

// Sheet is the index of one sheet element.
type Sheet struct {
	Node     *tree.Node
	entries  []cellEntry
	cells    map[Key]Cell // lookup, built only when the sheet holds more than mapThreshold cells
	deleted  map[Key]bool // sections and rows with Del="1", keyed with Cell ""
	rows     []rowGroup
	seenRows map[Key]bool // row keys already listed in rows, built once per Index when the sheet holds more than mapThreshold rows
}

// Index walks the direct children of el (a Shape, PageSheet or
// StyleSheet) and their Sections and Rows, and returns its sheet. A
// Section or Row without the attributes its key needs, and a repeated
// key (the first one wins), are skipped and reported in skipped, which
// the caller turns into notes. The cells are kept in a slice in
// document order, sized from one count of the element's Cell and
// Section/Row children; a lookup map is built only when that count
// holds more than mapThreshold cells, and below that Cell scans the
// slice. Chunk H1, MF5.
func Index(el *tree.Node) (s *Sheet, skipped []string) {
	s = &Sheet{Node: el}
	if el == nil {
		return s, nil
	}
	cells, rows := countSheet(el)
	if cells > 0 {
		s.entries = make([]cellEntry, 0, cells)
	}
	if cells > mapThreshold {
		s.cells = make(map[Key]Cell, cells)
	}
	if rows > mapThreshold {
		s.seenRows = make(map[Key]bool, rows)
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
	s.seenRows = nil // duplicate rows are listed for good now
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
	s.addRow(section, rowKey)
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
	if s.cells != nil {
		if _, dup := s.cells[key]; dup {
			return append(skipped, "duplicate cell "+key.Section+"/"+strconv.Itoa(key.SectionIX)+"/"+key.Row+"/"+strconv.Itoa(key.RowIX)+"/"+key.Cell)
		}
		s.cells[key] = cell
	} else {
		for _, e := range s.entries {
			if e.key == key {
				return append(skipped, "duplicate cell "+key.Section+"/"+strconv.Itoa(key.SectionIX)+"/"+key.Row+"/"+strconv.Itoa(key.RowIX)+"/"+key.Cell)
			}
		}
	}
	s.entries = append(s.entries, cellEntry{key: key, cell: cell})
	return skipped
}

// addRow records rowKey under section in s, unless that row is
// already listed in it.
func (s *Sheet) addRow(section string, rowKey Key) {
	if s.seenRows != nil {
		if s.seenRows[rowKey] {
			return
		}
		s.seenRows[rowKey] = true
		for i := range s.rows {
			if s.rows[i].section == section {
				s.rows[i].keys = append(s.rows[i].keys, rowKey)
				return
			}
		}
		s.rows = append(s.rows, rowGroup{section: section, keys: []Key{rowKey}})
		return
	}
	for i := range s.rows {
		if s.rows[i].section != section {
			continue
		}
		for _, k := range s.rows[i].keys {
			if k == rowKey {
				return
			}
		}
		s.rows[i].keys = append(s.rows[i].keys, rowKey)
		return
	}
	s.rows = append(s.rows, rowGroup{section: section, keys: []Key{rowKey}})
}

// countSheet counts the Cell elements at the shape, section and row
// level of el and the Row elements at the section level: upper bounds
// on the cells and the rows Index holds (it counts repeated and
// skipped ones too). It sizes the entries slice, the lookup map and
// the row duplicate map once, in one walk of the element's own
// children.
func countSheet(el *tree.Node) (cells, rows int) {
	cells, rows = 0, 0
	for child := el.FirstChild(); child != nil; child = child.NextSibling() {
		if child.Kind() != tree.ElementNode {
			continue
		}
		switch child.Local() {
		case vsd.ElemCell:
			cells++
		case vsd.ElemSection:
			for sub := child.FirstChild(); sub != nil; sub = sub.NextSibling() {
				if sub.Kind() != tree.ElementNode {
					continue
				}
				switch sub.Local() {
				case vsd.ElemCell:
					cells++
				case vsd.ElemRow:
					rows++
					for cell := sub.FirstChild(); cell != nil; cell = cell.NextSibling() {
						if cell.Kind() == tree.ElementNode && cell.Local() == vsd.ElemCell {
							cells++
						}
					}
				}
			}
		}
	}
	return cells, rows
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
	if s.cells != nil {
		c, ok := s.cells[k]
		return c, ok
	}
	for _, e := range s.entries {
		if e.key == k {
			return e.cell, true
		}
	}
	return Cell{}, false
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
func (s *Sheet) Rows(section string) []Key {
	for i := range s.rows {
		if s.rows[i].section == section {
			return s.rows[i].keys
		}
	}
	return nil
}
