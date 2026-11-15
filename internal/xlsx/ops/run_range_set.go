package ops

import (
	"github.com/jhnoel/documentdriver/internal/cli"
	"github.com/jhnoel/documentdriver/internal/xlsx/model"
)

// RangeSet is range.set.
type RangeSet struct {
	At     RangeAddress `json:"at" doc:"The range (Budget!A1:C3) or its top-left cell (Budget!A1)."`
	Values Grid         `json:"values" doc:"Rows of values, top row first, all the same length; null leaves a cell empty, \"\" writes an empty string."`
}

// opRangeSet is range.set's entry in the operation table.
var opRangeSet = OpDef{
	Name: "range.set", New: func() Spec { return &RangeSet{} },
	Summary: "Sets a rectangle of cells to rows of values, top row first. at is the range, which values must fill exactly, or its top-left cell, from which values extend. null leaves a cell empty, and \"\" writes an empty string.",
	Rules:   rangeSetRules,
}

// Run writes every value as cell.set does (chunk G2), cell by cell in
// row-major order through one overlay; a null value empties the cell's
// content as range.clear does. Refusals are cell.set's, for the first
// cell that has one (path at). Changed: the range.
func (o *RangeSet) Run(env *Env) error {
	s, err := env.Sheet(o.At.Sheet, "at")
	if err != nil {
		return err
	}
	r1, c1 := o.At.R1, o.At.C1
	if len(o.Values) == 0 || len(o.Values[0]) == 0 {
		return Refuse(cli.CodeInvalidOperation, "values", "values must contain a nonempty rectangle")
	}
	r2, c2 := r1+len(o.Values)-1, c1+len(o.Values[0])-1
	for _, a := range s.Index().Anchors() {
		ar1, ac1, ar2, ac2 := a.Ref.Bounds()
		if r1 <= ar2 && r2 >= ar1 && c1 <= ac2 && c2 >= ac1 &&
			!(r1 <= ar1 && c1 <= ac1 && r2 >= ar2 && c2 >= ac2) {
			return &Rejection{Code: cli.CodeOperationFailed, Field: "at", Msg: "the range cuts an array formula or data table", Detail: "an array formula at " + model.Area(s.Name(), ar1, ac1, ar2, ac2)}
		}
	}
	for i, row := range o.Values {
		for j, v := range row {
			if err := env.writeValue(s, r1+i, c1+j, v, "at"); err != nil {
				return err
			}
		}
	}
	env.ChangedRange(s.Name(), r1, c1, r2, c2)
	return nil
}
