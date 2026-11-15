package ops_test

import (
	"strings"
	"testing"

	"github.com/jhnoel/documentdriver/internal/cli"
	"github.com/jhnoel/documentdriver/internal/xlsx/ops"
)

func TestNameDeleteChunk(t *testing.T) {
	b := book("")
	b.DefinedNames = `<definedName name="Tax">1</definedName><definedName name="Tax" localSheetId="0">2</definedName>`
	r := runOps(t, b, &ops.NameDelete{At: ops.AtName("name:budget!tax")}).ok()
	names, err := r.ws.DefinedNames()
	if err != nil {
		t.Fatal(err)
	}
	if len(names) != 1 || names[0].Name != "Tax" || names[0].LocalID != -1 {
		t.Fatalf("remaining names: %+v", names)
	}
	if !r.ws.RecalcRequested() || len(r.env[0].Deleted) != 1 || r.env[0].Deleted[0] != "name:budget!tax" {
		t.Fatalf("recalc or deleted: %v %v", r.ws.RecalcRequested(), r.env[0].Deleted)
	}
	r = runOps(t, b, &ops.NameDelete{At: ops.AtName("name:Tax")}, &ops.NameDelete{At: ops.AtName("name:Budget!Tax")}).ok()
	if strings.Contains(r.part("/xl/workbook.xml"), "definedNames") {
		t.Fatal("empty definedNames retained")
	}
	missing := runOps(t, b, &ops.NameDelete{At: ops.AtName("name:Missing")})
	missing.refused(0, cli.CodeNotFound, "at")
	if missing.ws.RecalcRequested() || len(missing.env[0].Deleted) != 0 {
		t.Fatal("failed deletion reported changes")
	}
}
