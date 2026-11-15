package ops_test

import (
	"strings"
	"testing"

	"github.com/jhnoel/documentdriver/internal/cli"
	"github.com/jhnoel/documentdriver/internal/xlsx/ops"
)

func TestNameSetChunk(t *testing.T) {
	b := book("")
	b.DefinedNames = `<definedName name="Rate" hidden="1">1</definedName><definedName name="Rate" localSheetId="0" hidden="1">2</definedName>`
	r := runOps(t, b, &ops.NameSet{At: ops.AtName("name:budget!rate"), RefersTo: `=CONCAT("a","b")`}).ok()
	names, err := r.ws.DefinedNames()
	if err != nil {
		t.Fatal(err)
	}
	if len(names) != 2 || names[0].Text != "1" || names[1].Text != `_xlfn.CONCAT("a","b")` || !names[1].Hidden || names[1].LocalID != 0 {
		t.Fatalf("names: %+v", names)
	}
	if !r.ws.RecalcRequested() || len(r.env[0].Changed) != 1 || r.env[0].Changed[0].Kind != ops.UnitName || r.env[0].Changed[0].ID != "name:budget!rate" {
		t.Fatalf("recalc or changed: %v %+v", r.ws.RecalcRequested(), r.env[0].Changed)
	}
	if !strings.Contains(r.part("/xl/workbook.xml"), `name="Rate" localSheetId="0" hidden="1"`) {
		t.Fatal("attributes not preserved")
	}
	missing := runOps(t, b, &ops.NameSet{At: ops.AtName("name:Missing"), RefersTo: "=1"})
	missing.refused(0, cli.CodeNotFound, "at")
	if missing.ws.RecalcRequested() || len(missing.env[0].Changed) != 0 {
		t.Fatal("failed set reported changes")
	}
	for _, value := range []string{"", "1", "=A1#"} {
		failed := runOps(t, b, &ops.NameSet{At: ops.AtName("name:Rate"), RefersTo: value})
		if failed.err == nil || failed.ws.RecalcRequested() || len(failed.env[0].Changed) != 0 {
			t.Fatalf("invalid formula %q: err=%v changed=%v", value, failed.err, failed.env[0].Changed)
		}
	}
}
