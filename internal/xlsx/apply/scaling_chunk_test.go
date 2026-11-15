package apply

import (
	"fmt"
	"path/filepath"
	"testing"

	"github.com/jhnoel/documentdriver/internal/laws"
	"github.com/jhnoel/documentdriver/internal/xlsx/ops"
	"github.com/jhnoel/documentdriver/internal/xlsx/xlsxtest"
)

// TestScalingManySheets: one cell.set in a workbook of many small
// sheets is linear in the sheets (edit.New mapped the workbook's
// relationships once per sheet, opc.Rels.Get being a walk of them all,
// and the integrity check of the workbook did the same: quadratic).
// Units of 4 sheets.
func TestScalingManySheets(t *testing.T) {
	xlsxtest.Skip(t, func() {
		laws.Scales(t, "xlsx apply cell.set, many sheets", func(n int) (func() error, error) {
			b := xlsxtest.Book{}
			for i := range n / 4 {
				b.Sheets = append(b.Sheets, xlsxtest.Sheet{Name: fmt.Sprintf("Sh_%d", i+1), Rows: `<row r="1"><c r="A1"><v>1</v></c></row>`})
			}
			in, err := b.Write(t.TempDir(), "many.xlsx")
			if err != nil {
				return nil, err
			}
			out := filepath.Join(t.TempDir(), "out.xlsx")
			p := ops.NewPlan(&ops.CellSet{At: ops.AtCell("Sh_1!B1"), Value: ops.Num(2)})
			return func() error { _, err := Run(in, p, Options{Out: out}); return err }, nil
		})
	})
}
