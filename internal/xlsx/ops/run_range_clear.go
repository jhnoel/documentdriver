package ops

import (
	"github.com/jhnoel/documentdriver/internal/cli"
	"github.com/jhnoel/documentdriver/internal/xlsx/edit"
	"github.com/jhnoel/documentdriver/internal/xlsx/model"
)

// RangeClear is range.clear.
type RangeClear struct {
	At RangeAddress `json:"at" doc:"The range: Budget!A1:D10, whole rows Budget!5:7 or columns Budget!C:E."`
}

// opRangeClear is range.clear's entry in the operation table.
var opRangeClear = OpDef{
	Name: "range.clear", New: func() Spec { return &RangeClear{} },
	Summary: "Empties the content of every cell in a range (values and formulas), keeping each cell's format.",
	Rules:   nil,
}

// Run clears each existing cell of the range (Sheet.Each, then
// Sheet.ClearCell) (chunk G3). Refusals: a range that holds part of a
// multi-cell array or spill without all of it is operation_failed (path
// at); a range in a table's header row is allowed (the header text is
// the table's column name: refused, decision 13, path at). Changed: the
// range, clipped to the sheet's used range.
func (o *RangeClear) Run(env *Env) error {
	s, err := env.Sheet(o.At.Sheet, "at")
	if err != nil {
		return err
	}
	a := o.At
	for _, anchor := range s.Index().Anchors() {
		r1, c1, r2, c2 := anchor.Ref.Bounds()
		if a.R1 <= r2 && a.R2 >= r1 && a.C1 <= c2 && a.C2 >= c1 &&
			(a.R1 > r1 || a.R2 < r2 || a.C1 > c1 || a.C2 < c2) {
			return &Rejection{Code: cli.CodeOperationFailed, Field: "at", Msg: "the range cuts an array formula or data table", Detail: "an array formula at " + model.Area(s.Name(), r1, c1, r2, c2)}
		}
	}
	tables, err := env.WS.Tables(s.Name())
	if err != nil {
		return FromEdit(err, "at")
	}
	for _, table := range tables {
		r1, c1, _, c2 := table.Ref.Bounds()
		if table.HeaderRows > 0 && a.R1 <= r1+table.HeaderRows-1 && a.R2 >= r1 && a.C1 <= c2 && a.C2 >= c1 {
			return &Rejection{Code: cli.CodeOperationFailed, Field: "at", Msg: "the range includes a table header", Detail: "the header of table " + table.Name}
		}
	}
	// Collect coordinates before mutation: Each walks the overlay as well.
	var cells []edit.Cell
	if err := s.Each(a.R1, a.C1, a.R2, a.C2, func(c edit.Cell) error {
		cells = append(cells, c)
		return nil
	}); err != nil {
		return FromEdit(err, "at")
	}
	for _, c := range cells {
		if err := env.unshare(s, c.Row, c.Col); err != nil {
			return FromEdit(env.qualify(err, s.Name(), "at"), "at")
		}
		if err := s.ClearCell(c.Row, c.Col); err != nil {
			return FromEdit(err, "at")
		}
	}
	r1, c1, r2, c2, ok := s.Index().UsedRange()
	// The base index does not include cells written by earlier operations.
	for _, c := range cells {
		if !ok {
			r1, c1, r2, c2, ok = c.Row, c.Col, c.Row, c.Col, true
		} else {
			r1, c1 = min(r1, c.Row), min(c1, c.Col)
			r2, c2 = max(r2, c.Row), max(c2, c.Col)
		}
	}
	if ok {
		r1, c1 = max(r1, a.R1), max(c1, a.C1)
		r2, c2 = min(r2, a.R2), min(c2, a.C2)
		if r1 <= r2 && c1 <= c2 {
			env.ChangedRange(s.Name(), r1, c1, r2, c2)
		}
	}
	return nil
}
