package shift

import (
	"errors"
	"strings"
	"testing"

	"github.com/jhnoel/documentdriver/internal/xlsx/formula"
)

func TestWorkbookChunk(t *testing.T) {
	sh := formula.Sheets{Own: "Other", Order: []string{"Budget", "Other"}}
	doc := `<worksheet xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main"><sheetData><row r="1"><c r="A1"><f>Budget!A2</f></c><c r="B1"><f>#REF!+Budget!A2</f></c></row></sheetData></worksheet>`
	edits, stats, err := OtherSheet(doc, sh, insRow2.Inverse())
	if err != nil || len(edits) != 2 || stats.Adjusted != 2 || strings.Join(stats.RefErrors, ",") != "A1,B1" {
		t.Fatalf("edits=%+v stats=%+v err=%v", edits, stats, err)
	}
	_, gained, err := DefinedName("#REF!+Budget!A1", sh, insRow2)
	if err != nil || gained {
		t.Fatalf("existing ref error: %v %v", gained, err)
	}
	bad := strings.Replace(doc, "Budget!A2</f>", "Budget!A2&amp;?</f>", 1)
	_, _, err = OtherSheet(bad, sh, insRow2)
	var refusal *Refused
	if !errors.As(err, &refusal) || !strings.Contains(err.Error(), "Other!A1") {
		t.Fatalf("invalid formula: %v", err)
	}
}
