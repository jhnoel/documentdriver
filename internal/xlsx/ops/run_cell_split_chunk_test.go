package ops_test

import (
	"strings"
	"testing"

	"github.com/jhnoel/documentdriver/internal/cli"
	"github.com/jhnoel/documentdriver/internal/xlsx/ops"
	"github.com/jhnoel/documentdriver/internal/xlsx/xlsxtest"
)

func TestCellSplitMissingSheetChunk(t *testing.T) {
	runOps(t, book(""), &ops.CellSplit{At: ops.AtRange("Missing!A1:B1")}).refused(0, cli.CodeNotFound, "at")
}

func TestCellSplitChunk(t *testing.T) {
	xlsxtest.Skip(t, func() {
		b := book("")
		b.Sheets[0].Tail = `<mergeCells count="2"><mergeCell ref="A1:C1"/><mergeCell ref="A2:B2"/></mergeCells>`
		r := runOps(t, b, &ops.CellSplit{At: ops.AtRange("Budget!C1:A1")}).ok()
		if x := r.sheet(1); strings.Contains(x, `ref="A1:C1"`) || !strings.Contains(x, `ref="A2:B2"`) || !strings.Contains(x, `count="1"`) {
			t.Errorf("split: %s", x)
		}
		if len(r.env[0].Changed) != 1 || r.env[0].Changed[0].ID != "Budget!A1:C1" {
			t.Errorf("changed: %+v", r.env[0].Changed)
		}
		runOps(t, b, &ops.CellSplit{At: ops.AtRange("Budget!A1:B1")}).refused(0, cli.CodeNotFound, "at")
	})
}
