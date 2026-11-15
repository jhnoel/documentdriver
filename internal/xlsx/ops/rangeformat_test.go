package ops_test

import (
	"strings"
	"testing"

	"github.com/jhnoel/documentdriver/internal/xlsx/ops"
	"github.com/jhnoel/documentdriver/internal/xlsx/xlsxtest"
)

// G8: range.format (needs K6, K11) (designer rows; frozen).

func TestRangeFormat(t *testing.T) {
	xlsxtest.Skip(t, func() {
		r := runOps(t, book(`<row r="1"><c r="A1"><v>1</v></c></row>`),
			&ops.RangeFormat{At: ops.AtRange("Budget!A1:B1"), Bold: ops.Ptr(true), NumberFormat: ops.Ptr("0.00")},
			&ops.RangeFormat{At: ops.AtRange("Budget!B1"), Bold: ops.Ptr(true), NumberFormat: ops.Ptr("0.00")}).ok()
		x := r.sheet(1)
		if !strings.Contains(x, `<c r="A1" s="5"><v>1</v></c>`) || !strings.Contains(x, `<c r="B1" s="5"/>`) {
			t.Errorf("sheet: %s", x)
		}
		st := r.part("/xl/styles.xml")
		if !strings.Contains(st, `<cellXfs count="6">`) || !strings.Contains(st, `<b/>`) {
			t.Errorf("styles: %s", st)
		}
		if r.ws.RecalcRequested() {
			t.Error("a format change asked for recalculation")
		}
	})
}
