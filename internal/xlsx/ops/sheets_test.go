package ops_test

import (
	"strings"
	"testing"

	"github.com/jhnoel/documentdriver/internal/cli"
	"github.com/jhnoel/documentdriver/internal/xlsx/ops"
	"github.com/jhnoel/documentdriver/internal/xlsx/xlsxtest"
)

// G11 and G12: sheet.add and sheet.rename (designer rows; frozen).

func TestSheetAdd(t *testing.T) {
	xlsxtest.Skip(t, func() {
		r := runOps(t, book(""),
			&ops.SheetAdd{Name: "Q1 Plan", As: ops.Ptr("q")},
			&ops.SheetRename{At: ops.AtSheet("$q"), Name: "Q2 Plan"},
			&ops.CellSet{At: ops.AtCell("'Q2 Plan'!A1"), Value: ops.Num(1)}).ok()
		if r.env[0].Created != "sheet:Q1 Plan" {
			t.Errorf("created %q", r.env[0].Created)
		}
		if got := r.cells("'Q2 Plan'!A1")["A1"]; got != "1" {
			t.Errorf("new sheet A1 %s", got)
		}
		refs := r.ws.Sheets()
		if len(refs) != 2 || refs[1].Name != "Q2 Plan" || refs[1].ID != 2 {
			t.Errorf("sheets %+v", refs)
		}
		runOps(t, book(""), &ops.SheetAdd{Name: "budget"}).refused(0, cli.CodeOperationFailed, "name")
	})
}

func TestSheetRename(t *testing.T) {
	xlsxtest.Skip(t, func() {
		b := xlsxtest.Book{
			DefinedNames: `<definedName name="R">Budget!$A$1</definedName>`,
			Sheets: []xlsxtest.Sheet{
				{Name: "Budget", Rows: `<row r="1"><c r="A1"><v>1</v></c></row>`},
				{Name: "Other", Rows: `<row r="1"><c r="A1"><f>Budget!A1+1</f></c><c r="B1"><f>"Budget!A1"</f></c></row>`},
			},
		}
		r := runOps(t, b, &ops.SheetRename{At: ops.AtSheet("sheet:Budget"), Name: "Q1 Budget"}).ok()
		if x := r.sheet(2); !strings.Contains(x, `<f>'Q1 Budget'!A1+1</f>`) || !strings.Contains(x, `<f>"Budget!A1"</f>`) {
			t.Errorf("Other: %s", x)
		}
		wb, _ := r.ws.WorkbookTree()
		if s := string(wb.Bytes()); !strings.Contains(s, `name="Q1 Budget"`) || !strings.Contains(s, `'Q1 Budget'!$A$1`) {
			t.Errorf("workbook: %s", s)
		}
		runOps(t, b, &ops.SheetRename{At: ops.AtSheet("sheet:Budget"), Name: "other"}).refused(0, cli.CodeOperationFailed, "name")
		runOps(t, b, &ops.SheetRename{At: ops.AtSheet("sheet:Nope"), Name: "X"}).refused(0, cli.CodeNotFound, "at")
	})
}
