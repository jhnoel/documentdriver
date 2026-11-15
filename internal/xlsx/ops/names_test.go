package ops_test

import (
	"strings"
	"testing"

	"github.com/jhnoel/documentdriver/internal/cli"
	"github.com/jhnoel/documentdriver/internal/xlsx/ops"
	"github.com/jhnoel/documentdriver/internal/xlsx/xlsxtest"
)

// G13, G14, G15: name.add, name.set, name.delete (needs K4) (designer
// rows; frozen).

func TestNames(t *testing.T) {
	xlsxtest.Skip(t, func() {
		b := book("")
		b.DefinedNames = `<definedName name="Old" hidden="1">Budget!$A$1</definedName>`
		r := runOps(t, b,
			&ops.NameAdd{Name: "Tax", RefersTo: "=0.2"},
			&ops.NameAdd{Name: "Local", RefersTo: "=Budget!$B$1", Scope: &ops.NameScope{Raw: "sheet:Budget", Sheet: "Budget"}},
			&ops.NameSet{At: ops.AtName("name:Old"), RefersTo: "=CONCAT(\"a\",\"b\")"},
			&ops.NameDelete{At: ops.AtName("name:Tax")}).ok()
		wb, _ := r.ws.WorkbookTree()
		s := string(wb.Bytes())
		for _, want := range []string{
			`<definedName name="Local" localSheetId="0">Budget!$B$1</definedName>`,
			`<definedName name="Old" hidden="1">_xlfn.CONCAT("a","b")</definedName>`,
		} {
			if !strings.Contains(s, want) {
				t.Errorf("missing %s in %s", want, s)
			}
		}
		if strings.Contains(s, `name="Tax"`) {
			t.Error("name.delete left the name")
		}
		if !r.ws.RecalcRequested() {
			t.Error("name edits did not mark values")
		}
		if strings.Join(r.env[3].Deleted, ",") != "name:Tax" {
			t.Errorf("deleted %v", r.env[3].Deleted)
		}
		runOps(t, b, &ops.NameAdd{Name: "old", RefersTo: "=1"}).refused(0, cli.CodeOperationFailed, "name")
		runOps(t, b, &ops.NameSet{At: ops.AtName("name:Nope"), RefersTo: "=1"}).refused(0, cli.CodeNotFound, "at")
		runOps(t, b, &ops.NameAdd{Name: "X", RefersTo: "=1", Scope: &ops.NameScope{Raw: "sheet:Nope", Sheet: "Nope"}}).refused(0, cli.CodeNotFound, "scope")
	})
}
