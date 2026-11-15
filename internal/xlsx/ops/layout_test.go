package ops_test

import (
	"strings"
	"testing"

	"github.com/jhnoel/documentdriver/internal/cli"
	"github.com/jhnoel/documentdriver/internal/xlsx/ops"
	"github.com/jhnoel/documentdriver/internal/xlsx/xlsxtest"
)

// G6, G7, G9, G10: row.format, column.format, cell.merge, cell.split
// (needs K11, K12) (designer rows; frozen).

func TestRowAndColumnFormat(t *testing.T) {
	xlsxtest.Skip(t, func() {
		b := book(`<row r="2"><c r="A2"><v>1</v></c></row>`)
		b.Sheets[0].Head = `<cols><col min="1" max="5" width="9" customWidth="1"/></cols>`
		r := runOps(t, b,
			&ops.RowFormat{At: ops.AtRows("Budget!2:3"), Height: ops.Ptr(30.0)},
			&ops.RowFormat{At: ops.AtRows("Budget!3:3"), Hidden: ops.Ptr(true)},
			&ops.ColumnFormat{At: ops.AtCols("Budget!C:D"), Width: ops.Ptr(20.0)}).ok()
		x := r.sheet(1)
		for _, want := range []string{
			`<row r="2" ht="30" customHeight="1"><c r="A2"><v>1</v></c></row>`,
			`<row r="3" ht="30" customHeight="1" hidden="1"/>`,
			`<cols><col min="1" max="2" width="9" customWidth="1"/><col min="3" max="4" width="20" customWidth="1"/><col min="5" max="5" width="9" customWidth="1"/></cols>`,
		} {
			if !strings.Contains(x, want) {
				t.Errorf("missing %s in %s", want, x)
			}
		}
	})
}

func TestMergeAndSplit(t *testing.T) {
	xlsxtest.Skip(t, func() {
		r := runOps(t, book(""), &ops.CellMerge{At: ops.AtRange("Budget!A1:C1")}).ok()
		if x := r.sheet(1); !strings.Contains(x, `<mergeCells count="1"><mergeCell ref="A1:C1"/></mergeCells>`) {
			t.Errorf("merge: %s", x)
		}
		b := book("")
		b.Sheets[0].Tail = `<mergeCells count="1"><mergeCell ref="A1:C1"/></mergeCells>`
		runOps(t, b, &ops.CellMerge{At: ops.AtRange("Budget!B1:D2")}).refused(0, cli.CodeOperationFailed, "at")
		runOps(t, b, &ops.CellSplit{At: ops.AtRange("Budget!A1:B1")}).refused(0, cli.CodeNotFound, "at")
		r = runOps(t, b, &ops.CellSplit{At: ops.AtRange("Budget!A1:C1")}).ok()
		if strings.Contains(r.sheet(1), "mergeCells") {
			t.Error("an emptied mergeCells was kept (R7)")
		}
	})
}
