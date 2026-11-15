package ops

// CellSet is cell.set.
type CellSet struct {
	At    CellAddress `json:"at" doc:"The cell, sheet-qualified: Budget!B2."`
	Value CellValue   `json:"value" doc:"The value, in the form read shows (help values)."`
}

// opCellSet is cell.set's entry in the operation table.
var opCellSet = OpDef{
	Name: "cell.set", New: func() Spec { return &CellSet{} },
	Summary: "Sets one cell to a value: a number, text, a boolean, an error, a date or time, or a formula (a string starting with =). The cell keeps its format; a date written into a cell whose format shows no date gets the built-in format 14 (date), 22 (date and time) or 21 (time). A formula is written without a cached value, and the saved workbook asks Excel to recalculate on open.",
	Rules:   nil,
}

// Run writes the value (chunk G1; shared code in write_value.go):
//
//   - not_found: the sheet (path at).
//   - unreadable_target: the sheet cannot be edited safely (path at).
//   - operation_failed, path at: the cell is a member of a multi-cell
//     array formula or a dynamic-array spill, or its anchor (decision
//     8); the cell is in a table's header row (decision 13; detail
//     names the table).
//   - A shared formula's master is unshared first: every other member
//     of its group gets its own formula text (formula.Translate) as a
//     plain formula, so no member loses its formula; a shared child is
//     simply replaced.
//   - The cell's s is kept; a new cell takes Sheet.DefaultStyle (R4). A
//     date or time into a cell whose number format is not a date or
//     time format (numfmt.Classify) gets Styles.WithNumFmt(s, 14, 22 or
//     21); a workbook without a styles part refuses a date with
//     unreadable_target.
//   - Text and errors and booleans and numbers go through
//     Sheet.RenderCell; t="s" is never written (decision 11).
//   - Changed: the cell, as a range unit.
func (o *CellSet) Run(env *Env) error {
	s, err := env.Sheet(o.At.Sheet, "at")
	if err != nil {
		return err
	}
	if err := env.writeValue(s, o.At.Row, o.At.Col, o.Value, "at"); err != nil {
		return err
	}
	env.ChangedRange(s.Name(), o.At.Row, o.At.Col, o.At.Row, o.At.Col)
	return nil
}
