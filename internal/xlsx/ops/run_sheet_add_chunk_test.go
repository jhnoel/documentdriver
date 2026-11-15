package ops_test

import (
	"strings"
	"testing"

	"github.com/jhnoel/documentdriver/internal/cli"
	"github.com/jhnoel/documentdriver/internal/xlsx/ops"
)

func TestSheetAddChunk(t *testing.T) {
	r := runOps(t, book(""), &ops.SheetAdd{Name: "Q1 & Plan"}, &ops.SheetAdd{Name: "Next"}).ok()
	if r.env[0].Created != "sheet:Q1 & Plan" || len(r.env[0].Changed) != 1 || r.env[0].Changed[0].ID != "sheet:Q1 & Plan" || r.env[0].Changed[0].Kind != ops.UnitSheet || r.env[0].Changed[0].FormulasAdjusted != 0 {
		t.Fatalf("first report: %+v", r.env[0])
	}
	refs := r.ws.Sheets()
	if len(refs) != 3 || refs[1].Name != "Q1 & Plan" || refs[1].ID != 2 || refs[2].Name != "Next" || refs[2].ID != 3 {
		t.Fatalf("sheets: %+v", refs)
	}
	if !strings.Contains(r.sheet(2), "<sheetData/>") {
		t.Fatal("new worksheet is not blank")
	}
	if _, err := r.ws.Sheet("Q1 & Plan"); err != nil {
		t.Fatalf("open new sheet: %v", err)
	}
}

func TestSheetAddChunkDuplicate(t *testing.T) {
	r := runOps(t, book(""), &ops.SheetAdd{Name: "budget"})
	r.refused(0, cli.CodeOperationFailed, "name")
	if len(r.ws.Sheets()) != 1 || r.env[0].Created != "" || len(r.env[0].Changed) != 0 {
		t.Fatalf("duplicate changed state: %+v", r.env[0])
	}
	r = runOps(t, book(""), &ops.SheetAdd{Name: "New"}, &ops.SheetAdd{Name: "new"})
	r.refused(1, cli.CodeOperationFailed, "name")
	if len(r.ws.Sheets()) != 2 || r.env[1].Created != "" || len(r.env[1].Changed) != 0 {
		t.Fatalf("duplicate changed state: %+v", r.env[1])
	}
}
