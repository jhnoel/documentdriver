package ops

import (
	"errors"

	"github.com/jhnoel/documentdriver/internal/xlsx/model"
)

// CellSplit is cell.split.
type CellSplit struct {
	At RangeAddress `json:"at" doc:"The merged range, exactly as read lists it in merges: Budget!A1:C1."`
}

// opCellSplit is cell.split's entry in the operation table.
var opCellSplit = OpDef{
	Name: "cell.split", New: func() Spec { return &CellSplit{} },
	Summary: "Splits a merged range back into its cells.",
	Rules:   splitRules,
}

// Run (chunk G10) removes the mergeCell whose ref is the range; a range
// that is not a merge is not_found (path at). An emptied mergeCells is
// removed (R7). Changed: the range.
func (o *CellSplit) Run(env *Env) error {
	s, err := env.Sheet(o.At.Sheet, "at")
	if err != nil {
		return err
	}
	merges, _, err := s.Element("mergeCells")
	if err != nil {
		return FromEdit(err, "at")
	}
	area := model.CellKey(o.At.R1, o.At.C1) + ":" + model.CellKey(o.At.R2, o.At.C2)
	updated, err := RemoveMerge(merges, area)
	if err != nil {
		var rejection *Rejection
		if errors.As(err, &rejection) {
			qualified := *rejection
			qualified.Field = "at"
			return &qualified
		}
		return FromEdit(err, "at")
	}
	if err := s.SetElement("mergeCells", updated); err != nil {
		return FromEdit(err, "at")
	}
	env.ChangedRange(o.At.Sheet, o.At.R1, o.At.C1, o.At.R2, o.At.C2)
	return nil
}
