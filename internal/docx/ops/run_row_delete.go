package ops

// DeleteRows is row.delete (docxdriver's edit_table).
type DeleteRows struct {
	At    string `json:"at"`
	Count uint32 `json:"count"`
}

// opRowDelete is row.delete's entry in the operation table.
var opRowDelete = OpDef{
	Name: "row.delete", Oracle: "edit_table", New: func() Spec { return &DeleteRows{} },
	Moved: "`at` ← `at` + `action.row`",
	Fixed: map[string]string{"action.kind": `"delete_rows"`}, Rules: deleteRowsRules,
}

// Run deletes count rows of a plain grid from the row at
// (table_structure.rs edit, delete_rows); a table keeps at least one
// row. Tracked, each is a tracked row deletion with its content deleted,
// and a row the author inserted goes outright; direct, the rows are
// removed.
func (o *DeleteRows) Run(env *Env) error {
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
	g, _, err := regularTable(t.table, o.At)
	if err != nil {
		return err
	}
	first := g.rowIndex(t.row)
	if first < 0 {
		return reject("not_found", "at", "%s is not a row of its table", o.At)
	}
	end := first + int(o.Count)
	if end > len(g.rows) {
		return reject("operation_failed", "count", "%s: %d rows from it run past the table's %d", o.At, o.Count, len(g.rows))
	}
	if first == 0 && end == len(g.rows) {
		return reject("operation_failed", "count", "cannot delete every row of the table: delete the table instead")
	}
	h, _, err := env.tableRegion(t.table, o.At)
	if err != nil {
		return err
	}
	for _, r := range g.rows[first:end] {
		if env.Mode == ModeDirect {
			err = h.RemoveNode(r.tr)
		} else {
			err = env.deleteRowTracked(h, r.tr)
		}
		if err != nil {
			return err
		}
	}
	return env.refreshTables()
}
