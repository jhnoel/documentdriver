package ops

import (
	"github.com/jhnoel/documentdriver/internal/xlsx/formula"
	"github.com/jhnoel/documentdriver/internal/xlsx/model"
)

// ColumnDelete is column.delete.
type ColumnDelete struct {
	At ColsAddress `json:"at" doc:"The columns: Budget!C:E."`
}

// opColumnDelete is column.delete's entry in the operation table.
var opColumnDelete = OpDef{
	Name: "column.delete", New: func() Spec { return &ColumnDelete{} },
	Summary: "Deletes columns; the columns to the right move left, references into the deleted columns become #REF!, and ranges across them shrink.",
	Adjusts: structuralAdjusts, Refuses: structuralRefuses,
}

// Run is shift.Apply with the delete (designer-owned glue, structural.go).
func (o *ColumnDelete) Run(env *Env) error {
	return env.structural(o.At.Sheet, formula.Edit{Kind: formula.DeleteCols, At: o.At.From, Count: o.At.To - o.At.From + 1},
		model.Cols(o.At.Sheet, o.At.From, o.At.To))
}
