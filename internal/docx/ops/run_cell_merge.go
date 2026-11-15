package ops

import (
	"github.com/jhnoel/documentdriver/internal/docx/model"
	"github.com/jhnoel/documentdriver/internal/ooxml/tree"
)

// MergeCells is cell.merge (docxdriver's edit_table).
type MergeCells struct {
	At         string `json:"at"`
	RowSpan    uint32 `json:"row_span"`
	ColumnSpan uint32 `json:"column_span"`
}

// opCellMerge is cell.merge's entry in the operation table.
var opCellMerge = OpDef{
	Name: "cell.merge", Oracle: "edit_table", New: func() Spec { return &MergeCells{} },
	Moved: "`at` `table:t/row:r/cell:c` ← `at` + `action.row`, `action.column` (a grid column: the cell is the one at it)",
	Fixed: map[string]string{"action.kind": `"merge_cells"`}, Rules: mergeCellsRules,
	Track: DirectOnly, Direct: OnlyDirect, TrackGuards: refusedInTrack,
}

// Run merges a row_span x column_span block of a plain grid, whose top
// left cell is at, into one logical cell (table_structure.rs edit,
// merge_cells): the other cells' content moves into the top left cell in
// row-major order, each row of the block keeps its first cell, spanning
// the block's columns (w:gridSpan), and a block of several rows is a
// vertical merge (w:vMerge restart, then continue). Spans count grid
// columns and rows; in a plain grid a cell's position is its grid
// column. Direct only.
func (o *MergeCells) Run(env *Env) error {
	if err := env.refuseTracked("cell.merge"); err != nil {
		return err
	}
	t, err := env.resolveTable(o.At)
	if err != nil {
		return err
	}
	if t.cell == nil {
		return reject("not_found", "at", "%s does not name a cell", o.At)
	}
	if err := env.guardTable(t.table, o.At); err != nil {
		return err
	}
	g, width, err := regularTable(t.table, o.At)
	if err != nil {
		return err
	}
	r0 := g.rowIndex(t.row)
	c0 := -1
	if r0 >= 0 {
		c0 = g.rows[r0].index(t.cell)
	}
	if c0 < 0 {
		return reject("not_found", "at", "%s is not a cell of its table", o.At)
	}
	r1, c1 := r0+int(o.RowSpan), c0+int(o.ColumnSpan)
	if r1 > len(g.rows) {
		return reject("operation_failed", "row_span", "%s: a block of %d rows runs past the table's %d", o.At, o.RowSpan, len(g.rows))
	}
	if c1 > width {
		return reject("operation_failed", "column_span", "%s: a block of %d columns runs past the table's %d", o.At, o.ColumnSpan, width)
	}
	h, _, err := env.tableRegion(t.table, o.At)
	if err != nil {
		return err
	}
	primary := t.cell
	// Content moves in row-major order; nested tables move whole.
	for ri := r0; ri < r1; ri++ {
		for _, c := range g.rows[ri].cells[c0:c1] {
			if c.tc == primary {
				continue
			}
			for _, n := range c.tc.Elements() {
				if model.IsW(n, "tcPr") {
					continue
				}
				if err := h.Append(primary, n); err != nil {
					return err
				}
			}
		}
	}
	// A cell ends with a paragraph, merge continuations included.
	if last := lastElement(primary); last == nil || !model.IsW(last, "p") {
		if err := appendParagraph(env, h, primary); err != nil {
			return err
		}
	}
	tall := r1-r0 > 1
	for ri := r0; ri < r1; ri++ {
		cells := g.rows[ri].cells
		keep := cells[c0].tc
		for _, c := range cells[c0+1 : c1] {
			if err := h.RemoveNode(c.tc); err != nil {
				return err
			}
		}
		if keep != primary {
			if err := appendParagraph(env, h, keep); err != nil {
				return err
			}
		}
		var vertical *bool
		if tall {
			v := ri == r0
			vertical = &v
		}
		if err := setMerge(h, keep, c1-c0, vertical); err != nil {
			return err
		}
	}
	return env.refreshTables()
}

// lastElement returns n's last element child, or nil.
func lastElement(n *tree.Node) *tree.Node {
	els := n.Elements()
	if len(els) == 0 {
		return nil
	}
	return els[len(els)-1]
}
