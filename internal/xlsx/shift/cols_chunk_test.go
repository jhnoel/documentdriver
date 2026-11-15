package shift

import (
	"github.com/jhnoel/documentdriver/internal/xlsx/formula"
	"testing"
)

func TestShiftColsChunk(t *testing.T) {
	for _, tc := range []struct {
		in, want  string
		kind      formula.EditKind
		at, count int
	}{
		{`<cols><col min="2" max="5" width="9"/></cols>`, `<cols><col min="2" max="3" width="9"/></cols>`, formula.DeleteCols, 3, 2},
		{`<cols><col max='5' min='2'/></cols>`, `<cols><col max='7' min='2'/></cols>`, formula.InsertCols, 3, 2},
		{`<cols><col min="2" max="5"/></cols>`, ``, formula.DeleteCols, 1, 8},
		{`<cols><col min="2" max="5"/></cols>`, `<cols><col min="2" max="5"/></cols>`, formula.InsertRows, 1, 1},
		{`<cols><!-- <col min="1" max="1"/> --><col min="2" max="2"/></cols>`, `<cols><!-- <col min="1" max="1"/> --><col min="3" max="3"/></cols>`, formula.InsertCols, 2, 1},
		{`<cols><col min="1" max="1"></col><col min="3" max="3"/></cols>`, `<cols><col min="2" max="2"/></cols>`, formula.DeleteCols, 1, 1},
	} {
		got, err := ShiftCols(tc.in, formula.Edit{Kind: tc.kind, At: tc.at, Count: tc.count})
		if err != nil || got != tc.want {
			t.Errorf("got %q, %v; want %q", got, err, tc.want)
		}
	}
	if _, err := ShiftCols(`<cols><col min="2" max="16384"/></cols>`, formula.Edit{Kind: formula.InsertCols, At: 2, Count: 1}); err == nil {
		t.Error("expected overflow error")
	}
}
