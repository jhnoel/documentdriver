package shift

import (
	"testing"

	"github.com/jhnoel/documentdriver/internal/xlsx/formula"
)

// TestShiftSqrefCorpusForms: forms TestApplyCorpus met in corpus files
// (verification chunk V): whole rows and columns shift; #REF!, a defect
// the input has, stays as it is.
func TestShiftSqrefCorpusForms(t *testing.T) {
	ins := formula.Edit{Sheet: "S", Kind: formula.InsertRows, At: 2, Count: 1}
	for _, c := range []struct{ in, want string }{
		{"27:27", "28:28"},
		{"1:5", "1:6"},
		{"C:E A1", "C:E A1"},
		{"#REF! B3", "#REF! B4"},
	} {
		got, err := ShiftSqref(c.in, ins)
		if err != nil || got != c.want {
			t.Errorf("ShiftSqref(%q) = %q, %v; want %q", c.in, got, err, c.want)
		}
	}
}

// TestElementDeletedViewStays: a delete that removes every cell a
// selection, pane or dimension names leaves it at its address (it was
// refused, on 105 corpus files for a column delete).
func TestElementDeletedViewStays(t *testing.T) {
	del := formula.Edit{Sheet: "S", Kind: formula.DeleteCols, At: 2, Count: 1}
	sh := formula.Sheets{Own: "S"}
	for _, c := range []struct{ local, in, want string }{
		{"sheetViews", `<sheetViews><sheetView workbookViewId="0"><pane topLeftCell="B3"/><selection activeCell="B5" sqref="B5"/></sheetView></sheetViews>`,
			`<sheetViews><sheetView workbookViewId="0"><pane topLeftCell="B3"/><selection activeCell="B5" sqref="B5"/></sheetView></sheetViews>`},
		{"sheetViews", `<sheetViews><sheetView workbookViewId="0"><selection activeCell="D5" sqref="B5 D5"/></sheetView></sheetViews>`,
			`<sheetViews><sheetView workbookViewId="0"><selection activeCell="C5" sqref="C5"/></sheetView></sheetViews>`},
		{"dimension", `<dimension ref="B1:B9"/>`, `<dimension ref="B1:B9"/>`},
	} {
		got, _, err := Element(c.local, c.in, sh, del)
		if err != nil || got != c.want {
			t.Errorf("%s: got %q, %v\nwant %q", c.in, got, err, c.want)
		}
	}
	if _, _, err := Element("autoFilter", `<autoFilter ref="B1:B9"/>`, sh, del); err == nil {
		t.Error("an autoFilter a delete removes whole was not refused")
	}
}
