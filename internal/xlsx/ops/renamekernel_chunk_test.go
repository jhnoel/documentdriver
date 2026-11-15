package ops

import "testing"

func TestRenameKernelChunk(t *testing.T) {
	src := `<worksheet xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main"><sheetData><row r="2"><c r="B2"><f>'B&#117;dget'!A1&amp;"x"</f></c><c r="C2"><f t="shared" si="0"/></c></row></sheetData></worksheet>`
	got, err := RenameInSheet(src, "Budget", "New Name")
	if err != nil || len(got) != 1 || got[0] != (FormulaRename{Row: 2, Col: 2, Text: `'New Name'!A1&"x"`}) {
		t.Fatalf("got %+v, %v", got, err)
	}
	if _, err := RenameInSheet(`<worksheet>Budget`, "Budget", "New"); err == nil {
		t.Fatal("expected malformed XML error")
	}
	if got := RenameText(`"Budget!A1"+Budget!A1`, "Budget", "New"); got != `"Budget!A1"+New!A1` {
		t.Fatalf("RenameText: %s", got)
	}
}
