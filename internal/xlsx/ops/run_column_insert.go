package ops

import (
	"github.com/jhnoel/documentdriver/internal/xlsx/formula"
)

// ColumnInsert is column.insert.
type ColumnInsert struct {
	At       ColsAddress `json:"at" doc:"One column: Budget!C:C."`
	Position Position    `json:"position" enum:"before,after" doc:"Insert before or after at."`
	Count    *int        `json:"count" default:"1" doc:"How many columns."`
}

// opColumnInsert is column.insert's entry in the operation table.
var opColumnInsert = OpDef{
	Name: "column.insert", New: func() Spec { return &ColumnInsert{} },
	Summary: "Inserts count empty columns before or after a column; the columns to the right move and every reference to them is adjusted.",
	Rules:   columnInsertRules,
	Adjusts: structuralAdjusts, Refuses: structuralRefuses,
}

// Run is shift.Apply with the insert (designer-owned glue, structural.go).
func (o *ColumnInsert) Run(env *Env) error {
	at := o.At.From
	if o.Position == After {
		at++
	}
	return env.structural(o.At.Sheet, formula.Edit{Kind: formula.InsertCols, At: at, Count: count(o.Count)}, "")
}
