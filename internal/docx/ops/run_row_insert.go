package ops

import (
	"strconv"

	core "github.com/jhnoel/documentdriver/internal/ooxml/edit"
	"github.com/jhnoel/documentdriver/internal/ooxml/tree"
)

// InsertRows is row.insert (docxdriver's edit_table).
type InsertRows struct {
	At       string         `json:"at"`
	Position InsertPosition `json:"position"`
	Count    uint32         `json:"count"`
	Alias    *string        `json:"as" default:"null"`
}

// opRowInsert is row.insert's entry in the operation table.
var opRowInsert = OpDef{
	Name: "row.insert", Oracle: "edit_table", New: func() Spec { return &InsertRows{} },
	Moved: "`at` `table:t/row:r` ← `at` + `action.row`",
	Fixed: map[string]string{"action.kind": `"insert_rows"`}, Rules: insertRowsRules,
}

// Run inserts count rows before or after a row of a plain grid
// (table_structure.rs edit, insert_rows): each new row has the row's
// properties and an empty cell like each of its cells. Tracked, each is a
// tracked row insertion (w:trPr/w:ins). With `as`, "$as/row:N" and
// "$as/row:N/cell:M" name the new rows and cells.
func (o *InsertRows) Run(env *Env) error {
	t, err := env.resolveTable(o.At)
	if err != nil {
		return err
	}
	if t.row == nil || t.cell != nil {
		return reject("not_found", "at", "%s does not name a row", o.At)
	}
	if err := env.guardTable(t.table, o.At); err != nil {
		return err
	}
	g, width, err := regularTable(t.table, o.At)
	if err != nil {
		return err
	}
	if len(g.rows)+int(o.Count) > structuralLimit {
		return reject("operation_failed", "count", "the table would have more than %d rows", structuralLimit)
	}
	h, main, err := env.tableRegion(t.table, o.At)
	if err != nil {
		return err
	}
	bound := map[string]*tree.Node{}
	cursor := t.row
	for i := range int(o.Count) {
		r, cells, err := blankRow(env, t.row, width)
		if err != nil {
			return err
		}
		pos := core.Before(t.row)
		if o.Position == "after" {
			pos = core.JustAfter(cursor)
		}
		if err := env.WS.Core().Insert(main, pos, r, nil); err != nil {
			return err
		}
		cursor = r
		if env.Mode == ModeTrack {
			if _, err := env.Rev.MarkRow(h, r, "ins"); err != nil {
				return err
			}
		}
		n := "row:" + strconv.Itoa(i+1)
		bound[n] = r
		for ci, c := range cells {
			bound[n+"/cell:"+strconv.Itoa(ci+1)] = c
		}
	}
	if err := env.refreshTables(); err != nil {
		return err
	}
	return env.bindInserted(o.Alias, bound)
}
