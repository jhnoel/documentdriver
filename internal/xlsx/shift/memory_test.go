package shift

import (
	"fmt"
	"runtime"
	"strings"
	"testing"

	"github.com/jhnoel/documentdriver/internal/laws"
	"github.com/jhnoel/documentdriver/internal/xlsx/formula"
	"github.com/jhnoel/documentdriver/internal/xlsx/xlsxtest"
)

// sheetDataGrid is a sheetData of rows x 5 cells, the last a formula on
// its row (about 190 B a row).
func sheetDataGrid(rows int) string {
	var b strings.Builder
	b.WriteString(`<sheetData>`)
	for r := 1; r <= rows; r++ {
		fmt.Fprintf(&b, `<row r="%d"><c r="A%d"><v>%d</v></c><c r="B%d"><v>%d</v></c><c r="C%d"><v>1</v></c><c r="D%d"><v>2</v></c><c r="E%d"><f>A%d+B%d</f><v>3</v></c></row>`, r, r, r, r, r, r, r, r, r, r)
	}
	b.WriteString(`</sheetData>`)
	return b.String()
}

// TestMemoryBudgetSheetData: a row insert near the top of a 1M-cell sheet
// rewrites it streaming, patching only what moves. Measured (V,
// 2026-11-14, N = 35 MB): peak live 37.2 MB above the input (the output,
// 1.06 N), 72.0 MB allocated (2.06 N); the budgets are those with about
// 20% margin, well inside the structural op's documented 3N (the first
// implementations held a per-element tree, 13.5 N and 21.5 N).
func TestMemoryBudgetSheetData(t *testing.T) {
	xlsxtest.Skip(t, func() {
		src := sheetDataGrid(200000)
		n := float64(len(src)) / (1 << 20)
		budget := laws.Budget{PeakLive: laws.MB(1.3*n + 1), Alloc: laws.MB(2.5 * n)}
		laws.WithinMemory(t, "xlsx shift SheetData row insert", budget, func(mark func(string)) error {
			out, _, err := SheetData(src, formula.Sheets{Own: "S"}, formula.Edit{Kind: formula.InsertRows, Sheet: "S", At: 10, Count: 1})
			mark("rewritten")
			// The base live heap holds src, which SheetData stops using
			// before the mark, and only out's length is used after it:
			// without KeepAlive both are freed at the mark, and peak live
			// read 0.0 MB (laws.Memory.PeakLive).
			runtime.KeepAlive(src)
			runtime.KeepAlive(out)
			if len(out) == 0 && err == nil {
				return fmt.Errorf("empty output")
			}
			return err
		})
	})
}
