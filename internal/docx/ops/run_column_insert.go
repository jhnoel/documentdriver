package ops

import (
	"strconv"

	core "github.com/jhnoel/documentdriver/internal/ooxml/edit"
	"github.com/jhnoel/documentdriver/internal/ooxml/tree"
)

// InsertColumns is column.insert (docxdriver's edit_table).
type InsertColumns struct {
	At       string         `json:"at"`
	Position InsertPosition `json:"position"`
	Count    uint32         `json:"count"`
	Alias    *string        `json:"as" default:"null"`
}

// opColumnInsert is column.insert's entry in the operation table.
var opColumnInsert = OpDef{
	Name: "column.insert", Oracle: "edit_table", New: func() Spec { return &InsertColumns{} },
	Moved: "`at` `table:t/column:c` ← `at` + `action.column`",
	Fixed: map[string]string{"action.kind": `"insert_columns"`}, Rules: insertColumnsRules,
	Track: DirectOnly, Direct: OnlyDirect, TrackGuards: refusedInTrack,
}

// refuseTracked is the refusal of an operation Word records no revision
// for, in track mode.
func (env *Env) refuseTracked(op string) error {
	if env.Mode != ModeTrack {
		return nil
	}
	return reject("refused_in_mode", "", "%s cannot be tracked: Word tracks inserted and deleted rows, not column or cell-merge changes; use a change_mode direct plan", op)
}

// gridColumn resolves a column address against a plain grid of width
// columns: the 0-based column, and the grid's w:gridCol elements, which
// must match the rows.
func gridColumn(t *tableTarget, g *tableGrid, width int) (int, error) {
	if t.column == 0 || t.row != nil {
		return 0, reject("not_found", "at", "%s does not name a column", t.at)
	}
	if t.column > width {
		return 0, reject("not_found", "at", "%s is out of range: the table has %d columns", t.at, width)
	}
	if g.grid == nil {
		return 0, reject("operation_failed", "at", "%s: the table has no grid (w:tblGrid)", t.at)
	}
	if len(g.cols) != width {
		return 0, reject("operation_failed", "at", "%s: the table grid has %d columns and its rows %d", t.at, len(g.cols), width)
	}
	return t.column - 1, nil
}

// Run inserts count columns before or after a grid column of a plain
// grid (table_structure.rs edit, insert_columns): each row gets empty
// cells like its cell in that column, and the grid copies of its
// w:gridCol. Direct only. With `as`, "$as/row:N/cell:M" names the new
// cells (M counting the new cells of row N).
func (o *InsertColumns) Run(env *Env) error {
	if err := env.refuseTracked("column.insert"); err != nil {
		return err
	}
	t, err := env.resolveTable(o.At)
	if err != nil {
		return err
	}
	if err := env.guardTable(t.table, o.At); err != nil {
		return err
	}
	g, width, err := regularTable(t.table, o.At)
	if err != nil {
		return err
	}
	col, err := gridColumn(t, g, width)
	if err != nil {
		return err
	}
	if width+int(o.Count) > structuralLimit {
		return reject("operation_failed", "count", "the table would have more than %d columns", structuralLimit)
	}
	_, main, err := env.tableRegion(t.table, o.At)
	if err != nil {
		return err
	}
	bound := map[string]*tree.Node{}
	place := func(anchor *tree.Node, build func() (*tree.Node, error)) ([]*tree.Node, error) {
		var made []*tree.Node
		cursor := anchor
		for range int(o.Count) {
			n, err := build()
			if err != nil {
				return nil, err
			}
			pos := core.Before(anchor)
			if o.Position == "after" {
				pos = core.JustAfter(cursor)
			}
			if err := env.WS.Core().Insert(main, pos, n, nil); err != nil {
				return nil, err
			}
			cursor = n
			made = append(made, n)
		}
		return made, nil
	}
	for ri, r := range g.rows {
		anchor := r.cells[col].tc
		made, err := place(anchor, func() (*tree.Node, error) { return blankCell(env, anchor) })
		if err != nil {
			return err
		}
		for ci, c := range made {
			bound["row:"+strconv.Itoa(ri+1)+"/cell:"+strconv.Itoa(ci+1)] = c
		}
	}
	gc := g.cols[col]
	if _, err := place(gc, func() (*tree.Node, error) { return env.WS.Core().Copy(gc) }); err != nil {
		return err
	}
	if err := env.refreshTables(); err != nil {
		return err
	}
	return env.bindInserted(o.Alias, bound)
}
