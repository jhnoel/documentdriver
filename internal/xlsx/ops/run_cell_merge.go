package ops

import (
	"errors"

	"github.com/jhnoel/documentdriver/internal/cli"
	"github.com/jhnoel/documentdriver/internal/xlsx/model"
	"github.com/jhnoel/documentdriver/internal/xlsx/sheet"
)

// CellMerge is cell.merge.
type CellMerge struct {
	At RangeAddress `json:"at" doc:"The range to merge: Budget!A1:C1."`
}

// opCellMerge is cell.merge's entry in the operation table.
var opCellMerge = OpDef{
	Name: "cell.merge", New: func() Spec { return &CellMerge{} },
	Summary: "Merges a range into one cell. Excel shows the top-left cell's content; the other cells' content is kept in the file but hidden.",
	Rules:   mergeRules,
}

// Run (chunk G9) adds a mergeCell to mergeCells (created where the
// schema puts it; count updated). A range that overlaps an existing
// merge, or crosses a table, an array formula or a spill, is
// operation_failed (path at). Changed: the range.
func (o *CellMerge) Run(env *Env) error {
	s, err := env.Sheet(o.At.Sheet, "at")
	if err != nil {
		return err
	}
	a := o.At
	intersects := func(r1, c1, r2, c2 int) bool {
		return a.R1 <= r2 && r1 <= a.R2 && a.C1 <= c2 && c1 <= a.C2
	}
	for _, anchor := range s.Index().Anchors() {
		if anchor.Type != sheet.Array {
			continue
		}
		r1, c1, r2, c2 := anchor.Ref.Bounds()
		if intersects(r1, c1, r2, c2) {
			return &Rejection{Code: cli.CodeOperationFailed, Field: "at", Msg: "the range overlaps an array formula or spill", Detail: model.Area(s.Name(), r1, c1, r2, c2)}
		}
	}
	tables, err := env.WS.Tables(s.Name())
	if err != nil {
		return FromEdit(err, "at")
	}
	for _, table := range tables {
		r1, c1, r2, c2 := table.Ref.Bounds()
		if intersects(r1, c1, r2, c2) {
			return &Rejection{Code: cli.CodeOperationFailed, Field: "at", Msg: "the range overlaps a table", Detail: table.Name}
		}
	}
	xml, _, err := s.Element("mergeCells")
	if err != nil {
		return FromEdit(err, "at")
	}
	area := model.CellKey(a.R1, a.C1) + ":" + model.CellKey(a.R2, a.C2)
	xml, err = AddMerge(xml, s.Prefix(), area)
	if err != nil {
		var rejection *Rejection
		if errors.As(err, &rejection) && rejection.Field == "" {
			rejection.Field = "at"
		}
		return FromEdit(err, "at")
	}
	if err := s.SetElement("mergeCells", xml); err != nil {
		return FromEdit(err, "at")
	}
	env.ChangedRange(s.Name(), a.R1, a.C1, a.R2, a.C2)
	return nil
}
