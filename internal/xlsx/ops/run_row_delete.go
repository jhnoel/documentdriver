package ops

import (
	"github.com/jhnoel/documentdriver/internal/xlsx/formula"
	"github.com/jhnoel/documentdriver/internal/xlsx/model"
)

// RowDelete is row.delete.
type RowDelete struct {
	At RowsAddress `json:"at" doc:"The rows: Budget!5:7."`
}

// opRowDelete is row.delete's entry in the operation table.
var opRowDelete = OpDef{
	Name: "row.delete", New: func() Spec { return &RowDelete{} },
	Summary: "Deletes rows; the rows below move up, references into the deleted rows become #REF!, and ranges across them shrink.",
	Adjusts: structuralAdjusts, Refuses: structuralRefuses,
}

// Run is shift.Apply with the delete (designer-owned glue, structural.go).
func (o *RowDelete) Run(env *Env) error {
	return env.structural(o.At.Sheet, formula.Edit{Kind: formula.DeleteRows, At: o.At.From, Count: o.At.To - o.At.From + 1},
		model.Rows(o.At.Sheet, o.At.From, o.At.To))
}
