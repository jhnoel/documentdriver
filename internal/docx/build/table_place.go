package build

import (
	"errors"
	"strconv"
	"strings"

	docxedit "github.com/jhnoel/documentdriver/internal/docx/edit"
	"github.com/jhnoel/documentdriver/internal/docx/html"
	core "github.com/jhnoel/documentdriver/internal/ooxml/edit"
	"github.com/jhnoel/documentdriver/internal/ooxml/tree"
)

// The lowering of an HTML table (create): the grid HTML's table model
// gives (each cell at the first free grid column of its row, a rowspan
// covering the columns below it) as table.insert writes a table (NewTable:
// grid columns of DefaultColumnWidth and, with no width written, an
// automatic preferred width), with colspan as w:gridSpan, rowspan as a
// w:vMerge restart over continuation cells, and a row that ends short of
// the grid as w:gridAfter. The style read writes for a table, row or cell
// becomes its properties (tableCSS).

// MaxGridColumns is the most grid columns a table may have: Word's
// limit.
const MaxGridColumns = 63

// PlaceParagraph builds paragraph para in container before at (at the
// end when at is nil).
type PlaceParagraph func(h *core.Handle, container, at *tree.Node, para Para) error

// PlaceTable builds t in container before at (at the end when at is nil)
// and returns it; para builds each cell paragraph. A cell with no content,
// or ending in a table, ends with an empty paragraph, as Word requires.
func PlaceTable(h *core.Handle, container, at *tree.Node, t *Table, para PlaceParagraph) (*tree.Node, error) {
	grid, cols, err := layout(t)
	if err != nil {
		return nil, err
	}
	// The skeleton is parsed whole, as NewTable's is: rows and cells
	// stand in a table's content model, which is not tabulated for
	// placement one at a time.
	var b strings.Builder
	b.WriteString(`<w:tbl><w:tblPr/><w:tblGrid>`)
	col := `<w:gridCol w:w="` + strconv.Itoa(DefaultColumnWidth) + `"/>`
	for range cols {
		b.WriteString(col)
	}
	b.WriteString(`</w:tblGrid>`)
	for ri, row := range t.Rows {
		b.WriteString(`<w:tr>`)
		if row.CSS != "" || rowEnd(grid[ri]) < cols {
			b.WriteString(`<w:trPr/>`)
		}
		for _, s := range grid[ri] {
			if c := s.cell; c.Colspan > 1 || c.Rowspan > 1 || c.CSS != "" {
				b.WriteString(`<w:tc><w:tcPr/></w:tc>`)
			} else {
				b.WriteString(`<w:tc/>`)
			}
		}
		b.WriteString(`</w:tr>`)
	}
	b.WriteString(`</w:tbl>`)
	tbl, err := parseOne(h, container, b.String())
	if err != nil {
		return nil, err
	}
	if err := h.PlaceAt(container, tbl, at); err != nil {
		return nil, err
	}
	tblPr := firstChild(tbl)
	if err := tableCSS(h, tblPr, scopeTable, t.CSS); err != nil {
		return nil, err
	}
	if err := defaultWidth(h, tblPr); err != nil {
		return nil, err
	}
	ri := 0
	for _, tr := range tbl.Elements() {
		if tr.Local() != "tr" {
			continue
		}
		slots := grid[ri]
		var tcs []*tree.Node
		for _, c := range tr.Elements() {
			switch c.Local() {
			case "trPr":
				if err := rowProperties(h, c, t.Rows[ri].CSS, cols-rowEnd(slots)); err != nil {
					return nil, err
				}
			case "tc":
				tcs = append(tcs, c)
			}
		}
		for k, s := range slots {
			if err := fillCell(h, tcs[k], s, para); err != nil {
				return nil, err
			}
		}
		ri++
	}
	return tbl, nil
}

// rowEnd is the grid column after a row's last slot.
func rowEnd(slots []slot) int {
	if len(slots) == 0 {
		return 0
	}
	s := slots[len(slots)-1]
	return s.col + s.cell.Colspan
}

// slot is one w:tc of a row: a cell where it starts, or the continuation
// of a cell spanning down from a row above.
type slot struct {
	col  int
	cell *Cell
	cont bool
}

// layout places the cells on the grid: the slots of each row in column
// order and the number of grid columns.
func layout(t *Table) ([][]slot, int, error) {
	n := len(t.Rows)
	taken := make([][]bool, n)
	grid := make([][]slot, n)
	cols := 0
	for ri := range t.Rows {
		col := 0
		for ci := range t.Rows[ri].Cells {
			c := &t.Rows[ri].Cells[ci]
			for col < len(taken[ri]) && taken[ri][col] {
				col++
			}
			if col+c.Colspan > MaxGridColumns {
				return nil, 0, contentErr("table row %d is wider than %d grid columns", ri+1, MaxGridColumns)
			}
			rs := min(c.Rowspan, n-ri)
			for r := ri; r < ri+rs; r++ {
				for len(taken[r]) < col+c.Colspan {
					taken[r] = append(taken[r], false)
				}
				for k := col; k < col+c.Colspan; k++ {
					taken[r][k] = true
				}
				grid[r] = append(grid[r], slot{col: col, cell: c, cont: r > ri})
			}
			col += c.Colspan
			cols = max(cols, col)
		}
	}
	for ri, row := range grid {
		sortSlots(row)
		next := 0
		for _, s := range row {
			if s.col != next {
				return nil, 0, contentErr("table row %d leaves grid column %d empty before a cell", ri+1, next+1)
			}
			next = s.col + s.cell.Colspan
		}
	}
	return grid, cols, nil
}

// sortSlots orders a row's slots by column (insertion sort: a row's
// continuations arrive before its own cells, each group in order).
func sortSlots(row []slot) {
	for i := 1; i < len(row); i++ {
		for j := i; j > 0 && row[j].col < row[j-1].col; j-- {
			row[j], row[j-1] = row[j-1], row[j]
		}
	}
}

// fillCell gives tc, a cell of the skeleton, its properties and content.
func fillCell(h *core.Handle, tc *tree.Node, s slot, para PlaceParagraph) error {
	c := s.cell
	if tcPr := firstChild(tc); tcPr != nil {
		if err := tableCSS(h, tcPr, scopeCell, c.CSS); err != nil {
			return err
		}
		if c.Colspan > 1 {
			if err := setVal(h, tcPr, "gridSpan", strconv.Itoa(c.Colspan)); err != nil {
				return err
			}
		}
		if s.cont {
			if _, err := h.EnsureIn(tcPr, docxedit.W("vMerge")); err != nil {
				return err
			}
		} else if c.Rowspan > 1 {
			if err := setVal(h, tcPr, "vMerge", "restart"); err != nil {
				return err
			}
		}
	}
	endsInTable := s.cont || len(c.Blocks) == 0
	if !s.cont {
		for _, b := range c.Blocks {
			endsInTable = b.Table != nil
			if b.Table != nil {
				if _, err := PlaceTable(h, tc, nil, b.Table, para); err != nil {
					return err
				}
				continue
			}
			if err := para(h, tc, nil, *b.Para); err != nil {
				return err
			}
		}
	}
	if endsInTable {
		return para(h, tc, nil, Para{Attrs: html.ParaAttrs{Tag: "p"}})
	}
	return nil
}

// rowProperties writes a row's style and, for a row ending short of the
// grid, its w:gridAfter into trPr.
func rowProperties(h *core.Handle, trPr *tree.Node, css string, after int) error {
	if after > 0 {
		e, err := ensureRow(h, trPr, "gridAfter")
		if err != nil {
			return err
		}
		if err := h.SetNodeAttr(e, wq(e, "val"), strconv.Itoa(after)); err != nil {
			return err
		}
	}
	return tableCSS(h, trPr, scopeRow, css)
}

// defaultWidth gives a table with no preferred width written the
// automatic one, as table.insert does.
func defaultWidth(h *core.Handle, tblPr *tree.Node) error {
	for _, c := range tblPr.Elements() {
		if c.Local() == "tblW" {
			return nil
		}
	}
	w, err := h.EnsureIn(tblPr, docxedit.W("tblW"))
	if err != nil {
		return err
	}
	if err := h.SetNodeAttr(w, wq(w, "w"), "0"); err != nil {
		return err
	}
	return h.SetNodeAttr(w, wq(w, "type"), "auto")
}

// setVal ensures bag's child w:local and sets its w:val.
func setVal(h *core.Handle, bag *tree.Node, local, v string) error {
	e, err := h.EnsureIn(bag, docxedit.W(local))
	if err != nil {
		return err
	}
	return h.SetNodeAttr(e, wq(e, "val"), v)
}

// parseOne parses frag as one detached element for placement under at.
func parseOne(h *core.Handle, at *tree.Node, frag string) (*tree.Node, error) {
	nodes, err := h.Workspace().Parse(at, []byte(frag), nsW)
	if err != nil {
		return nil, err
	}
	if len(nodes) != 1 {
		return nil, errors.New("build: a fragment did not parse as one element")
	}
	return nodes[0], nil
}

// firstChild is n's first element child.
func firstChild(n *tree.Node) *tree.Node {
	for _, c := range n.Elements() {
		return c
	}
	return nil
}
