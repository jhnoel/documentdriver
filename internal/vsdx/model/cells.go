package model

import (
	"strconv"
	"strings"

	"github.com/jhnoel/documentdriver/internal/ooxml/tree"
	"github.com/jhnoel/documentdriver/internal/vsdx/inherit"
	"github.com/jhnoel/documentdriver/internal/vsdx/sheet"
	"github.com/jhnoel/documentdriver/internal/vsdx/vsd"
)

// CellKey spells a sheet key as --detail full shows it, in ShapeSheet
// reference syntax where Visio has one: "PinX"; named rows
// "<prefix>.<row>.<cell>" with Visio's section prefixes (Prop, User,
// Hyperlink, Actions, Controls, ...): "Prop.Cost.Value",
// "Controls.Row_1.X"; Geometry "Geometry1.X2" (section IX+1, row IX);
// Connection "Connections.X1" (row IX 0); indexed text rows with a
// 1-based index: "Char.Size[1]", "Para.HorzAlign[1]", "Tabs.Position[1]",
// "Fields.Value[1]"; layers, which formulas cannot name,
// "Layer[1].Name". The table of prefixes is designer-owned (chunk M7).
func CellKey(k sheet.Key) string {
	if k.Section == "" {
		return k.Cell
	}
	if k.Row != "" {
		// A named row: the prefix is the section's reference prefix,
		// its stored name except Property (Prop) and Control
		// (Controls); User, Hyperlink, Actions and the rest keep
		// their name.
		prefix := k.Section
		switch k.Section {
		case vsd.SectionProperty:
			prefix = "Prop"
		case "Control":
			prefix = "Controls"
		}
		if k.Cell == "" {
			return prefix + "." + k.Row
		}
		return prefix + "." + k.Row + "." + k.Cell
	}
	if k.RowIX >= 0 {
		head, suffix := indexedHead(k)
		if k.Cell == "" {
			return strings.TrimSuffix(head, ".")
		}
		return head + k.Cell + suffix
	}
	// A section-level cell (no row): Geometry names its section with
	// the 1-based section index (Geometry1.NoFill); any other
	// section is spelled with its stored name.
	prefix := k.Section
	if k.Section == vsd.SectionGeometry {
		prefix = "Geometry" + strconv.Itoa(k.SectionIX+1)
	}
	if k.Cell == "" {
		return prefix
	}
	return prefix + "." + k.Cell
}

// indexedHead returns the parts of an indexed row's key around its
// cell: Geometry names the section and the row in the name itself
// (Geometry1.X2, row IX as written), Connection and Scratch append
// the 1-based row (X1 for row IX 0), Layer puts the 1-based index on
// the section name (Layer[1].Name) and the remaining indexed
// sections (Character, Paragraph, Tabs, Field, ...) carry a 1-based
// bracket index after the cell (Char.Size[1]); Field is spelled
// Fields there. Chunk M7.
func indexedHead(k sheet.Key) (head, suffix string) {
	switch k.Section {
	case vsd.SectionGeometry:
		return "Geometry" + strconv.Itoa(k.SectionIX+1) + ".", strconv.Itoa(k.RowIX)
	case vsd.SectionConnection:
		return "Connections.", strconv.Itoa(k.RowIX + 1)
	case vsd.SectionLayer:
		return "Layer[" + strconv.Itoa(k.RowIX+1) + "].", ""
	case vsd.SectionCharacter:
		return "Char.", "[" + strconv.Itoa(k.RowIX+1) + "]"
	case vsd.SectionParagraph:
		return "Para.", "[" + strconv.Itoa(k.RowIX+1) + "]"
	case vsd.SectionField:
		return "Fields.", "[" + strconv.Itoa(k.RowIX+1) + "]"
	case "Scratch":
		return "Scratch.", strconv.Itoa(k.RowIX + 1)
	default:
		return k.Section + ".", "[" + strconv.Itoa(k.RowIX+1) + "]"
	}
}

// rawCells returns the shape's own stored cells in stored order with
// their keys. Chunk M7.
func rawCells(s *sheet.Sheet) []RawCell {
	if s == nil || s.Node == nil {
		return nil
	}
	count := storedCellCount(s.Node)
	if count == 0 {
		return nil
	}
	out := make([]RawCell, 0, count)
	seen := make(map[sheet.Key]bool, count)
	add := func(k sheet.Key) {
		if seen[k] {
			return
		}
		cell, ok := s.Cell(k)
		if !ok {
			return
		}
		seen[k] = true
		out = append(out, RawCell{Key: CellKey(k), V: cell.V, U: cell.U, F: cell.F})
	}
	for child := range s.Node.ElementsSeq() {
		switch child.Local() {
		case vsd.ElemCell:
			if name, ok := child.Attr("N"); ok && name != "" {
				add(sheet.ShapeCell(name))
			}
		case vsd.ElemSection:
			name, ok := child.Attr("N")
			if !ok || name == "" {
				continue
			}
			secIX := -1
			if v, ok := child.Attr("IX"); ok {
				ix, err := strconv.Atoi(v)
				if err != nil || ix < 0 {
					continue
				}
				secIX = ix
			}
			for sub := range child.ElementsSeq() {
				switch sub.Local() {
				case vsd.ElemCell:
					if cellName, ok := sub.Attr("N"); ok && cellName != "" {
						add(sheet.Key{Section: name, SectionIX: secIX, RowIX: -1, Cell: cellName})
					}
				case vsd.ElemRow:
					rowName, rowIX := "", -1
					if n, ok := sub.Attr("N"); ok && n != "" {
						rowName = n
					} else if v, ok := sub.Attr("IX"); ok {
						ix, err := strconv.Atoi(v)
						if err != nil || ix < 0 {
							continue
						}
						rowIX = ix
					} else {
						continue
					}
					for cellNode := range sub.ElementsSeq() {
						if cellNode.Local() != vsd.ElemCell {
							continue
						}
						if cellName, ok := cellNode.Attr("N"); ok && cellName != "" {
							add(sheet.Key{Section: name, SectionIX: secIX, Row: rowName, RowIX: rowIX, Cell: cellName})
						}
					}
				}
			}
		}
	}
	return out
}

// storedCellCount counts the Cell elements at the shape, section and
// row level of node: an upper bound on the cells sheet.Index holds
// (it counts repeated and skipped cells too). It sizes rawCells'
// result and its duplicate map once, so neither grows by rehashing;
// overcounting only overallocates in proportion to the input. Chunk M7.
func storedCellCount(node *tree.Node) int {
	count := 0
	for child := range node.ElementsSeq() {
		switch child.Local() {
		case vsd.ElemCell:
			count++
		case vsd.ElemSection:
			for sub := range child.ElementsSeq() {
				switch sub.Local() {
				case vsd.ElemCell:
					count++
				case vsd.ElemRow:
					for cell := range sub.ElementsSeq() {
						if cell.Local() == vsd.ElemCell {
							count++
						}
					}
				}
			}
		}
	}
	return count
}

// boxOrigin is the origin of a property shown from several cells: the
// most inherited of theirs, in the order local < inherited < master <
// style. Chunk M7.
func boxOrigin(origins ...inherit.Origin) inherit.Origin {
	best := inherit.Origin("")
	bestRank := -1
	for _, origin := range origins {
		rank := -1
		switch origin {
		case inherit.Local:
			rank = 0
		case inherit.Inherited:
			rank = 1
		case inherit.Master:
			rank = 2
		case inherit.Style:
			rank = 3
		}
		if rank > bestRank {
			best, bestRank = origin, rank
		}
	}
	return best
}
