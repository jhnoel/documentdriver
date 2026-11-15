package ops

import (
	"errors"
	"testing"

	"github.com/jhnoel/documentdriver/internal/cli"
	"github.com/jhnoel/documentdriver/internal/xlsx/edit"
	"github.com/jhnoel/documentdriver/internal/xlsx/formula"
	"github.com/jhnoel/documentdriver/internal/xlsx/sheet"
)

func TestPlanCellChunk(t *testing.T) {
	f := CellFacts{Row: 2, Col: 2, Style: 4}
	for _, tc := range []struct {
		v CellValue
		c edit.Content
	}{
		{Num(3), edit.Content{Kind: edit.ContentNumber, Num: 3}},
		{Str(""), edit.Content{Kind: edit.ContentString}},
		{Boolean(true), edit.Content{Kind: edit.ContentBool, Bool: true}},
		{Err("#N/A"), edit.Content{Kind: edit.ContentError, Text: "#N/A"}},
	} {
		w, err := PlanCell(f, tc.v)
		if err != nil || w.Style != 4 || w.Content != tc.c {
			t.Fatalf("got %+v, %v", w, err)
		}
	}
	f.Anchors = []sheet.Anchor{{Type: sheet.DataTable, Ref: formula.Range{From: formula.Cell{Row: 1, Col: 2}, To: formula.Cell{Row: 3, Col: 2}}}}
	_, err := PlanCell(f, Null())
	var r *Rejection
	if !errors.As(err, &r) || r.Code != cli.CodeOperationFailed || r.Detail != "a data table at B1:B3" {
		t.Fatalf("refusal: %v", err)
	}
}
