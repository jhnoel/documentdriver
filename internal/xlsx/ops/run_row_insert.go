package ops

import (
	"github.com/jhnoel/documentdriver/internal/xlsx/formula"
)

// RowInsert is row.insert.
type RowInsert struct {
	At       RowsAddress `json:"at" doc:"One row: Budget!5:5."`
	Position Position    `json:"position" enum:"before,after" doc:"Insert before or after at."`
	Count    *int        `json:"count" default:"1" doc:"How many rows."`
}

// opRowInsert is row.insert's entry in the operation table.
var opRowInsert = OpDef{
	Name: "row.insert", New: func() Spec { return &RowInsert{} },
	Summary: "Inserts count empty rows before or after a row; the rows below move down and every reference to them is adjusted.",
	Rules:   rowInsertRules,
	Adjusts: structuralAdjusts, Refuses: structuralRefuses,
}

// Run is shift.Apply with the insert (designer-owned glue, structural.go).
func (o *RowInsert) Run(env *Env) error {
	at := o.At.From
	if o.Position == After {
		at++
	}
	return env.structural(o.At.Sheet, formula.Edit{Kind: formula.InsertRows, At: at, Count: count(o.Count)}, "")
}
