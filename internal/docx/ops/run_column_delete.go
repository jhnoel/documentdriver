package ops

// DeleteColumns is column.delete (docxdriver's edit_table).
type DeleteColumns struct {
	At    string `json:"at"`
	Count uint32 `json:"count"`
}

// opColumnDelete is column.delete's entry in the operation table.
var opColumnDelete = OpDef{
	Name: "column.delete", Oracle: "edit_table", New: func() Spec { return &DeleteColumns{} },
	Moved: "`at` ← `at` + `action.column`",
	Fixed: map[string]string{"action.kind": `"delete_columns"`}, Rules: deleteColumnsRules,
	Track: DirectOnly, Direct: OnlyDirect, TrackGuards: refusedInTrack,
}

// Run deletes count grid columns of a plain grid from the column at
// (table_structure.rs edit, delete_columns): each row's cells in them and
// their w:gridCol; a table keeps at least one column. Direct only.
func (o *DeleteColumns) Run(env *Env) error {
	if err := env.refuseTracked("column.delete"); err != nil {
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
	end := col + int(o.Count)
	if end > width {
		return reject("operation_failed", "count", "%s: %d columns from it run past the table's %d", o.At, o.Count, width)
	}
	if col == 0 && end == width {
		return reject("operation_failed", "count", "cannot delete every column of the table: delete the table instead")
	}
	h, _, err := env.tableRegion(t.table, o.At)
	if err != nil {
		return err
	}
	for _, r := range g.rows {
		for _, c := range r.cells[col:end] {
			if err := h.RemoveNode(c.tc); err != nil {
				return err
			}
		}
	}
	for _, gc := range g.cols[col:end] {
		if err := h.RemoveNode(gc); err != nil {
			return err
		}
	}
	return env.refreshTables()
}
