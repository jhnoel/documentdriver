package shift

import (
	"github.com/jhnoel/documentdriver/internal/xlsx/formula"
	"testing"
)

func TestSqrefChunk(t *testing.T) {
	e := formula.Edit{Sheet: "S", Kind: formula.DeleteRows, At: 3, Count: 1}
	sh := formula.Sheets{Own: "S"}
	for _, tc := range []struct{ in, want string }{{"A1 A3 B2:B4", "A1 B2:B3"}, {"A3", ""}} {
		got, err := ShiftSqref(tc.in, e)
		if err != nil || got != tc.want {
			t.Fatalf("ShiftSqref(%q) = %q, %v", tc.in, got, err)
		}
	}
	in := `<dataValidations count="2"><dataValidation sqref="A3"><formula1><nested/></formula1></dataValidation><dataValidation sqref="B5"><formula1>A4&gt;0</formula1></dataValidation></dataValidations>`
	want := `<dataValidations count="1"><dataValidation sqref="B4"><formula1>A3&gt;0</formula1></dataValidation></dataValidations>`
	got, stats, err := ShiftDataValidations(in, sh, e)
	if err != nil || got != want || stats.Adjusted != 1 {
		t.Fatalf("got %q %+v %v", got, stats, err)
	}
	cf := `<conditionalFormatting sqref="A1:A5"><cfRule><formula>A4&gt;0</formula></cfRule></conditionalFormatting>`
	got, stats, err = ShiftConditionalFormatting(cf, sh, e)
	if err != nil || got != `<conditionalFormatting sqref="A1:A4"><cfRule><formula>A3&gt;0</formula></cfRule></conditionalFormatting>` || stats.Adjusted != 1 {
		t.Fatalf("got %q %+v %v", got, stats, err)
	}
	if _, err = ShiftSqref("not-a-ref", e); err == nil {
		t.Fatal("invalid sqref accepted")
	}
}
