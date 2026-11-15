package ops_test

import (
	"strings"
	"testing"

	"github.com/jhnoel/documentdriver/internal/cli"
	"github.com/jhnoel/documentdriver/internal/xlsx/ops"
	"github.com/jhnoel/documentdriver/internal/xlsx/xlsxtest"
)

func TestNameAddChunkWorkbookScope(t *testing.T) {
	b := book("")
	b.DefinedNames = `<definedName name="Old" hidden="1">Budget!$A$1</definedName>`
	r := runOps(t, b, &ops.NameAdd{Name: "Tax", RefersTo: "=0.2"}).ok()
	wb, err := r.ws.WorkbookTree()
	if err != nil {
		t.Fatal(err)
	}
	s := string(wb.Bytes())
	for _, want := range []string{
		`<definedName name="Old" hidden="1">Budget!$A$1</definedName>`,
		`<definedName name="Tax">0.2</definedName>`,
	} {
		if !strings.Contains(s, want) {
			t.Errorf("missing %s in %s", want, s)
		}
	}
	if !r.ws.RecalcRequested() {
		t.Error("name.add did not mark values")
	}
	if len(r.env[0].Changed) != 1 || r.env[0].Changed[0].Kind != ops.UnitName || r.env[0].Changed[0].ID != "name:Tax" {
		t.Errorf("changed: %+v", r.env[0].Changed)
	}
	names, err := r.ws.DefinedNames()
	if err != nil {
		t.Fatal(err)
	}
	if len(names) != 2 || names[1].Name != "Tax" || names[1].Text != "0.2" || names[1].LocalID != -1 || names[1].Sheet != "" {
		t.Errorf("names: %+v", names)
	}
}

func TestNameAddChunkCreatesDefinedNames(t *testing.T) {
	r := runOps(t, book(""), &ops.NameAdd{Name: "Tax", RefersTo: "=Budget!$A$1"}).ok()
	s := r.part("/xl/workbook.xml")
	if !strings.Contains(s, "<definedNames>") || !strings.Contains(s, `<definedName name="Tax">Budget!$A$1</definedName>`) {
		t.Errorf("workbook: %s", s)
	}
}

func TestNameAddChunkExplicitWorkbookScope(t *testing.T) {
	r := runOps(t, book(""), &ops.NameAdd{Name: "Tax", RefersTo: "=1", Scope: &ops.NameScope{Raw: "workbook"}}).ok()
	if len(r.env[0].Changed) != 1 || r.env[0].Changed[0].ID != "name:Tax" {
		t.Errorf("changed: %+v", r.env[0].Changed)
	}
	names, err := r.ws.DefinedNames()
	if err != nil {
		t.Fatal(err)
	}
	if len(names) != 1 || names[0].LocalID != -1 {
		t.Errorf("names: %+v", names)
	}
}

func TestNameAddChunkSheetScope(t *testing.T) {
	r := runOps(t, book(""), &ops.NameAdd{Name: "Local", RefersTo: "=Budget!$B$1", Scope: &ops.NameScope{Raw: "sheet:Budget", Sheet: "Budget"}}).ok()
	s := r.part("/xl/workbook.xml")
	if want := `<definedName name="Local" localSheetId="0">Budget!$B$1</definedName>`; !strings.Contains(s, want) {
		t.Errorf("missing %s in %s", want, s)
	}
	if len(r.env[0].Changed) != 1 || r.env[0].Changed[0].Kind != ops.UnitName || r.env[0].Changed[0].ID != "name:Budget!Local" {
		t.Errorf("changed: %+v", r.env[0].Changed)
	}
	if !r.ws.RecalcRequested() {
		t.Error("name.add did not mark values")
	}
}

func TestNameAddChunkSheetScopeQuoted(t *testing.T) {
	b := xlsxtest.Book{XFs: []int{0}, Sheets: []xlsxtest.Sheet{{Name: "Q1 Budget", Rows: "", Dimension: "A1"}}}
	r := runOps(t, b, &ops.NameAdd{Name: "Local", RefersTo: "='Q1 Budget'!$A$1", Scope: &ops.NameScope{Raw: "sheet:Q1 Budget", Sheet: "Q1 Budget"}}).ok()
	if len(r.env[0].Changed) != 1 || r.env[0].Changed[0].ID != "name:'Q1 Budget'!Local" {
		t.Errorf("changed: %+v", r.env[0].Changed)
	}
	names, err := r.ws.DefinedNames()
	if err != nil {
		t.Fatal(err)
	}
	if len(names) != 1 || names[0].Sheet != "Q1 Budget" || names[0].LocalID != 0 {
		t.Errorf("names: %+v", names)
	}
}

func TestNameAddChunkStoreConversion(t *testing.T) {
	r := runOps(t, book(""), &ops.NameAdd{Name: "Joined", RefersTo: `=CONCAT("a","b")`}).ok()
	names, err := r.ws.DefinedNames()
	if err != nil {
		t.Fatal(err)
	}
	if len(names) != 1 || names[0].Text != `_xlfn.CONCAT("a","b")` {
		t.Errorf("names: %+v", names)
	}
	if strings.Contains(r.part("/xl/workbook.xml"), `>=CONCAT`) {
		t.Errorf("stored formula kept \"=\" or display form: %s", r.part("/xl/workbook.xml"))
	}
}

func TestNameAddChunkSameNameDifferentScope(t *testing.T) {
	b := book("")
	b.DefinedNames = `<definedName name="Tax">1</definedName>`
	r := runOps(t, b,
		&ops.NameAdd{Name: "Tax", RefersTo: "=2", Scope: &ops.NameScope{Raw: "sheet:Budget", Sheet: "Budget"}},
		&ops.NameAdd{Name: "Local", RefersTo: "=3"}).ok()
	names, err := r.ws.DefinedNames()
	if err != nil {
		t.Fatal(err)
	}
	if len(names) != 3 {
		t.Fatalf("names: %+v", names)
	}
	found := map[string]bool{}
	for _, n := range names {
		found[n.Name+"/"+n.Sheet] = true
	}
	if !found["Tax/"] || !found["Tax/Budget"] || !found["Local/"] {
		t.Errorf("names: %+v", names)
	}
}

func TestNameAddChunkDuplicateRefused(t *testing.T) {
	b := book("")
	b.DefinedNames = `<definedName name="Old">1</definedName><definedName name="Local" localSheetId="0">2</definedName>`
	r := runOps(t, b, &ops.NameAdd{Name: "old", RefersTo: "=1"})
	r.refused(0, cli.CodeOperationFailed, "name")
	if r.ws.RecalcRequested() || len(r.env[0].Changed) != 0 {
		t.Errorf("refused add reported changes: recalc=%v changed=%+v", r.ws.RecalcRequested(), r.env[0].Changed)
	}
	names, err := r.ws.DefinedNames()
	if err != nil {
		t.Fatal(err)
	}
	if len(names) != 2 {
		t.Errorf("refused add changed names: %+v", names)
	}
	r = runOps(t, b, &ops.NameAdd{Name: "LOCAL", RefersTo: "=1", Scope: &ops.NameScope{Raw: "sheet:Budget", Sheet: "Budget"}})
	r.refused(0, cli.CodeOperationFailed, "name")
	// A sheet-scoped name does not block the workbook scope.
	r = runOps(t, b, &ops.NameAdd{Name: "Local", RefersTo: "=1"}).ok()
	if len(r.env[0].Changed) != 1 || r.env[0].Changed[0].ID != "name:Local" {
		t.Errorf("changed: %+v", r.env[0].Changed)
	}
}

func TestNameAddChunkMissingScopeRefused(t *testing.T) {
	b := book("")
	b.DefinedNames = `<definedName name="Old">1</definedName>`
	r := runOps(t, b, &ops.NameAdd{Name: "X", RefersTo: "=1", Scope: &ops.NameScope{Raw: "sheet:Nope", Sheet: "Nope"}})
	r.refused(0, cli.CodeNotFound, "scope")
	if r.ws.RecalcRequested() || len(r.env[0].Changed) != 0 {
		t.Errorf("refused add reported changes: recalc=%v changed=%+v", r.ws.RecalcRequested(), r.env[0].Changed)
	}
	names, err := r.ws.DefinedNames()
	if err != nil {
		t.Fatal(err)
	}
	if len(names) != 1 {
		t.Errorf("refused add changed names: %+v", names)
	}
}
