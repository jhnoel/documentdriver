package edit

import (
	"strings"
	"testing"

	"github.com/jhnoel/documentdriver/internal/xlsx/integrity"
)

// The workbook shell (designer-owned; these must pass).
func TestWorkbookShell(t *testing.T) {
	b := oneSheet("")
	b.DefinedNames = `<definedName name="Old">S!$A$1</definedName>`
	w := openBook(t, b)
	if err := w.AddDefinedName("Tax", "", "0.2"); err != nil {
		t.Fatal(err)
	}
	if err := w.AddDefinedName("Loc", "S", "S!$B$1"); err != nil {
		t.Fatal(err)
	}
	if err := w.AddDefinedName("tax", "", "1"); err == nil {
		t.Error("a duplicate name was added")
	}
	if err := w.SetDefinedName("old", "", "S!$A$2"); err != nil {
		t.Fatal(err)
	}
	if err := w.DeleteDefinedName("Tax", ""); err != nil {
		t.Fatal(err)
	}
	ref, err := w.AddSheet("New")
	if err != nil {
		t.Fatal(err)
	}
	if ref.ID != 2 || ref.Part != "/xl/worksheets/sheet2.xml" || ref.Kind != KindWorksheet {
		t.Errorf("AddSheet %+v", ref)
	}
	if err := w.RenameSheet("s", "Budget"); err != nil {
		t.Fatal(err)
	}
	names, _ := w.DefinedNames()
	if len(names) != 2 || names[0].Text != "S!$A$2" || names[1].Sheet != "Budget" {
		t.Errorf("names %+v", names)
	}
	s, err := w.Sheet("New")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SetCell(1, 1, `<c r="A1"><v>1</v></c>`); err != nil {
		t.Fatal(err)
	}
	if err := w.Finish(); err != nil {
		t.Fatal(err)
	}
	if err := w.Verify(); err != nil {
		t.Fatal(err)
	}
	sealed, err := w.Package().Seal()
	if err != nil {
		t.Fatal(err)
	}
	doc, _ := w.WorkbookTree()
	wb := string(doc.Bytes())
	for _, want := range []string{`<sheet name="New" sheetId="2" r:id="rId2"/>`, `<definedName name="Loc" localSheetId="0">S!$B$1</definedName>`, `<calcPr fullCalcOnLoad="1"/>`} {
		if !strings.Contains(wb, want) {
			t.Errorf("workbook lacks %s: %s", want, wb)
		}
	}
	if f := integrity.Check(sealed.Bytes()); !f.Empty() {
		t.Errorf("findings %v", f.Kinds())
	}
}
