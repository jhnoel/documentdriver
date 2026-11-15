package ops

import (
	"github.com/jhnoel/documentdriver/internal/docx/model"
	"github.com/jhnoel/documentdriver/internal/ooxml/tree"
)

// The grid model (docs/contract.md, section 6). A table has a grid of
// columns (w:tblGrid). Each row skips w:gridBefore columns, then lays its
// cells left to right, each taking w:gridSpan columns (1 when absent or
// unreadable), and leaves w:gridAfter columns at the end. A cell address
// counts a row's cells (cell:c is the c-th w:tc, the td position read
// shows); a column address counts grid columns. A vertically merged cell
// is a w:vMerge restart and the continuations below it that start at the
// same grid column.
//
// The model is built for one table when an operation needs it and never
// kept: it holds the table's own nodes only.

// vmerge is a cell's vertical-merge state.
type vmerge uint8

const (
	vmNone vmerge = iota
	vmRestart
	vmContinue
)

// gridCell is one cell placed on the grid.
type gridCell struct {
	tc *tree.Node
	// col is the 0-based grid column the cell starts at; span the number
	// of grid columns it takes (at least 1).
	col, span int
	merge     vmerge
	// hmerge reports a legacy w:hMerge.
	hmerge bool
}

// gridRow is one row placed on the grid.
type gridRow struct {
	tr *tree.Node
	// before and after are w:gridBefore and w:gridAfter.
	before, after int
	cells         []gridCell
}

// starting returns the index of the cell that starts at grid column col,
// or -1.
func (r *gridRow) starting(col int) int {
	for i, c := range r.cells {
		if c.col == col {
			return i
		}
		if c.col > col {
			break
		}
	}
	return -1
}

// index returns the position of tc among the row's cells, or -1.
func (r *gridRow) index(tc *tree.Node) int {
	for i, c := range r.cells {
		if c.tc == tc {
			return i
		}
	}
	return -1
}

// tableGrid is a table laid on its grid.
type tableGrid struct {
	tbl *tree.Node
	// grid is the w:tblGrid (nil when the table has none) and cols its
	// w:gridCol children.
	grid *tree.Node
	cols []*tree.Node
	rows []gridRow
}

// gridValue is a non-negative integer property w:local/@w:val of
// container c, or def when it is absent or unreadable.
func gridValue(c *tree.Node, local string, def int) int {
	if c == nil {
		return def
	}
	n := model.ChildW(c, local)
	if n == nil {
		return def
	}
	v, ok := model.ParseU32(model.AttrOr(n, "val"))
	if !ok || v > 1<<16 {
		return def
	}
	return int(v)
}

// cellOf places one cell starting at grid column col.
func cellOf(tc *tree.Node, col int) gridCell {
	c := gridCell{tc: tc, col: col, span: 1}
	tcpr := model.ChildW(tc, "tcPr")
	if tcpr == nil {
		return c
	}
	if s := gridValue(tcpr, "gridSpan", 1); s > 1 {
		c.span = s
	}
	if vm := model.ChildW(tcpr, "vMerge"); vm != nil {
		c.merge = vmContinue
		if model.AttrOr(vm, "val") == "restart" {
			c.merge = vmRestart
		}
	}
	c.hmerge = model.ChildW(tcpr, "hMerge") != nil
	return c
}

// layRow places row tr on the grid.
func layRow(tr *tree.Node) gridRow {
	trpr := model.ChildW(tr, "trPr")
	r := gridRow{tr: tr, before: gridValue(trpr, "gridBefore", 0), after: gridValue(trpr, "gridAfter", 0)}
	col := r.before
	for _, tc := range rowCells(tr) {
		c := cellOf(tc, col)
		r.cells = append(r.cells, c)
		col += c.span
	}
	return r
}

// readGrid lays table t on its grid.
func readGrid(t *tree.Node) *tableGrid {
	g := &tableGrid{tbl: t, grid: model.ChildW(t, "tblGrid")}
	if g.grid != nil {
		g.cols = childrenW(g.grid, "gridCol")
	}
	for _, tr := range tableRows(t) {
		g.rows = append(g.rows, layRow(tr))
	}
	return g
}

// rowIndex returns the position of tr among the table's rows, or -1.
func (g *tableGrid) rowIndex(tr *tree.Node) int {
	for i := range g.rows {
		if g.rows[i].tr == tr {
			return i
		}
	}
	return -1
}

// verticalRun returns the cells of the vertical merge that starts at the
// restart cell ci of row ri: the restart and each continuation below it
// at the same grid column. A cell that is not a restart is its own run.
// ok is false when a continuation spans other columns than the restart.
func (g *tableGrid) verticalRun(ri, ci int) (cells []*tree.Node, rows int, ok bool) {
	top := g.rows[ri].cells[ci]
	cells = []*tree.Node{top.tc}
	if top.merge != vmRestart {
		return cells, 1, true
	}
	for r := ri + 1; r < len(g.rows); r++ {
		i := g.rows[r].starting(top.col)
		if i < 0 || g.rows[r].cells[i].merge != vmContinue {
			break
		}
		if g.rows[r].cells[i].span != top.span {
			return nil, 0, false
		}
		cells = append(cells, g.rows[r].cells[i].tc)
	}
	return cells, len(cells), true
}

// regular reports whether the table is a plain rectangle: at least one
// cell per row, the same number in every row, no merged cell, no skipped
// grid columns (docxdriver's table_structure.rs regular). The row, column
// and merge edits take only such a table.
func (g *tableGrid) regular() (width int, ok bool) {
	if len(g.rows) == 0 {
		return 0, false
	}
	width = len(g.rows[0].cells)
	if width == 0 {
		return 0, false
	}
	for _, r := range g.rows {
		if len(r.cells) != width || r.before != 0 || r.after != 0 {
			return 0, false
		}
		if trpr := model.ChildW(r.tr, "trPr"); trpr != nil &&
			(model.ChildW(trpr, "gridBefore") != nil || model.ChildW(trpr, "gridAfter") != nil) {
			return 0, false
		}
		for _, c := range r.cells {
			if c.span != 1 || c.merge != vmNone || c.hmerge || hasSpanElement(c.tc) {
				return 0, false
			}
		}
	}
	return width, true
}

// hasSpanElement reports whether a cell's w:gridSpan says anything but 1
// (an unreadable value counts: the cell's extent is unknown).
func hasSpanElement(tc *tree.Node) bool {
	tcpr := model.ChildW(tc, "tcPr")
	if tcpr == nil {
		return false
	}
	gs := model.ChildW(tcpr, "gridSpan")
	return gs != nil && model.AttrOr(gs, "val") != "1"
}
