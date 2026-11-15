package ops

import (
	core "github.com/jhnoel/documentdriver/internal/ooxml/edit"
)

// SplitCell is cell.split (docxdriver's edit_table).
type SplitCell struct {
	At string `json:"at"`
}

// opCellSplit is cell.split's entry in the operation table.
var opCellSplit = OpDef{
	Name: "cell.split", Oracle: "edit_table", New: func() Spec { return &SplitCell{} },
	Moved: "`at` ← `at` + `action.row`, `action.column` (a grid column: the cell is the one at it)",
	Fixed: map[string]string{"action.kind": `"split_cell"`}, Rules: splitCellRules,
	Track: DirectOnly, Direct: OnlyDirect, TrackGuards: refusedInTrack,
}

// Run splits a merged logical cell back into grid cells
// (table_structure.rs edit, split_cell): the cell and, for a vertical
// merge, each continuation below it lose their merge properties, and each
// gets an empty cell like it for every further grid column it spanned.
// Content stays in the cells that held it. A vertical continuation is
// refused (split its first cell), and so is a cell that is not merged.
// Direct only.
func (o *SplitCell) Run(env *Env) error {
	if err := env.refuseTracked("cell.split"); err != nil {
		return err
	}
	t, cells, err := env.logicalCell(o.At)
	if err != nil {
		return err
	}
	if err := env.guardTable(t.table, o.At); err != nil {
		return err
	}
	g := readGrid(t.table)
	ri := g.rowIndex(t.row)
	span := g.rows[ri].cells[g.rows[ri].index(t.cell)].span
	if span == 1 && len(cells) == 1 {
		return reject("operation_failed", "at", "%s is not merged", o.At)
	}
	h, main, err := env.tableRegion(t.table, o.At)
	if err != nil {
		return err
	}
	for _, c := range cells {
		if err := setMerge(h, c, 1, nil); err != nil {
			return err
		}
		cursor := c
		for range span - 1 {
			n, err := blankCell(env, c)
			if err != nil {
				return err
			}
			if err := env.WS.Core().Insert(main, core.JustAfter(cursor), n, nil); err != nil {
				return err
			}
			cursor = n
		}
	}
	return env.refreshTables()
}
