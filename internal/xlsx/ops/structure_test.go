package ops_test

import (
	"strings"
	"testing"

	"github.com/jhnoel/documentdriver/internal/cli"
	"github.com/jhnoel/documentdriver/internal/xlsx/ops"
	"github.com/jhnoel/documentdriver/internal/xlsx/xlsxtest"
)

// G5: row and column insert and delete through shift.Apply (needs K7a,
// K7b, K8a-c, K9a-c) (designer rows; frozen).

func structBook() xlsxtest.Book {
	return xlsxtest.Book{
		DefinedNames: `<definedName name="Rates">Budget!$B$2:$B$4</definedName>`,
		Sheets: []xlsxtest.Sheet{
			{Name: "Budget", Dimension: "A1:B4",
				Rows: `<row r="1"><c r="A1"><v>1</v></c><c r="B1"><f>SUM(B2:B4)</f></c></row>` +
					`<row r="2"><c r="B2"><v>2</v></c></row><row r="3"><c r="B3"><v>3</v></c></row><row r="4"><c r="B4"><v>4</v></c></row>`,
				Tail: `<mergeCells count="1"><mergeCell ref="A3:A4"/></mergeCells>`},
			{Name: "Other", Rows: `<row r="1"><c r="A1"><f>Budget!B3</f><v>3</v></c></row>`},
		},
	}
}

func TestRowInsert(t *testing.T) {
	xlsxtest.Skip(t, func() {
		r := runOps(t, structBook(), &ops.RowInsert{At: ops.AtRows("Budget!3:3"), Position: ops.Before, Count: ops.Ptr(2)}).ok()
		got := r.cells("Budget!A1:B6")
		if got["B1"] != `{"f":"=SUM(B2:B6)"}` || got["B5"] != `3` {
			t.Errorf("Budget after insert: %v", got)
		}
		if x := r.sheet(2); !strings.Contains(x, `<f>Budget!B5</f>`) {
			t.Errorf("Other not adjusted: %s", x)
		}
		if x := r.sheet(1); !strings.Contains(x, `<mergeCell ref="A5:A6"/>`) || !strings.Contains(x, `<dimension ref="A1:B6"/>`) {
			t.Errorf("merges or dimension: %s", x)
		}
		wb, _ := r.ws.WorkbookTree()
		if !strings.Contains(string(wb.Bytes()), `Budget!$B$2:$B$6`) {
			t.Error("defined name not adjusted")
		}
		if len(r.env[0].Changed) < 2 {
			t.Errorf("changed %+v", r.env[0].Changed)
		}
	})
}

func TestRowDelete(t *testing.T) {
	xlsxtest.Skip(t, func() {
		r := runOps(t, structBook(), &ops.RowDelete{At: ops.AtRows("Budget!3:3")}).ok()
		if got := r.cells("Budget!B1")["B1"]; got != `{"f":"=SUM(B2:B3)"}` {
			t.Errorf("B1 %s", got)
		}
		if x := r.sheet(2); !strings.Contains(x, `Budget!#REF!`) {
			t.Errorf("Other: %s", x)
		}
		if len(r.env[0].RefErrors) == 0 || strings.Join(r.env[0].Deleted, ",") != "Budget!3:3" {
			t.Errorf("ref errors %v deleted %v", r.env[0].RefErrors, r.env[0].Deleted)
		}
	})
}

func TestColumnInsertAndDelete(t *testing.T) {
	xlsxtest.Skip(t, func() {
		r := runOps(t, structBook(), &ops.ColumnInsert{At: ops.AtCols("Budget!B:B"), Position: ops.Before}).ok()
		if got := r.cells("Budget!C1")["C1"]; got != `{"f":"=SUM(C2:C4)"}` {
			t.Errorf("C1 %s", got)
		}
		r = runOps(t, structBook(), &ops.ColumnDelete{At: ops.AtCols("Budget!A:A")}).ok()
		if got := r.cells("Budget!A1")["A1"]; got != `{"f":"=SUM(A2:A4)"}` {
			t.Errorf("A1 %s", got)
		}
	})
}

func TestStructuralRefusals(t *testing.T) {
	xlsxtest.Skip(t, func() {
		b := structBook()
		b.Sheets[0].Rows += `<row r="6"><c r="A6"><f t="array" ref="A6:A8">B6:B8</f></c></row>`
		r := runOps(t, b, &ops.RowInsert{At: ops.AtRows("Budget!7:7"), Position: ops.Before})
		r.refused(0, cli.CodeOperationFailed, "at")
		var rej *ops.Rejection
		if rej, _ = r.err.(*ops.Rejection); rej == nil || rej.Detail == "" {
			t.Errorf("refusal has no detail naming the construct: %+v", r.err)
		}
		if pt, _ := r.pkg.Part("/xl/worksheets/sheet2.xml"); pt.Patch() != nil {
			t.Error("a refused shift wrote another sheet")
		}
	})
}
