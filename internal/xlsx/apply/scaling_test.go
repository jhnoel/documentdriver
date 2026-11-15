package apply

import (
	"path/filepath"
	"testing"

	"github.com/jhnoel/documentdriver/internal/laws"
	"github.com/jhnoel/documentdriver/internal/xlsx/ops"
	"github.com/jhnoel/documentdriver/internal/xlsx/xlsxtest"
)

// The scaling gate and the memory budget (make scaling). Budget
// (docs/design.md, xlsx: Write (P2)): apply of one cell.set holds at most
// 1.3 N + T of live heap. Measured (V, 2026-11-14) at 200,000 x 10 (N =
// 68.7 MB): peak live 82.8 MB (1.21 N, at "sealed"), so the design's
// formula stands (9% margin); allocated 1010.6 MB (14.7 N), budget 17.7 N
// (20% margin). Of the allocation, about 740 MB is the tokenizer's
// per-node garbage (tree.ScanString allocates every node it hands a
// visitor) over the two whole-sheet passes left: the edit layer's index
// of the input and the gate's check of the output. The other passes are
// gone: the gate checks the input only when the output has findings,
// splice.Patch.Check stops after the last span (NewWellFormed), and the
// changed range is read back only up to its last row (CellsOnly). The
// rest is the sheet text read three times (open, gate, read back: 200
// MB). A tokenizer that reuses scan nodes is the follow-up that lowers
// this (internal/ooxml/tree).

func gridBook(t testing.TB, rows int) string {
	t.Helper()
	p, err := xlsxtest.Book{SST: []string{"<t>x</t>"}, Sheets: []xlsxtest.Sheet{{Name: "Budget", Rows: xlsxtest.Grid(rows, 10)}}}.Write(t.TempDir(), "grid.xlsx")
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func TestScalingCellSet(t *testing.T) {
	xlsxtest.Skip(t, func() {
		laws.Scales(t, "xlsx apply cell.set", func(n int) (func() error, error) {
			in := gridBook(t, n*25) // Small and Large units of 25 rows
			out := filepath.Join(t.TempDir(), "out.xlsx")
			p := ops.NewPlan(&ops.CellSet{At: ops.AtCell("Budget!C7"), Value: ops.Num(1)})
			return func() error { _, err := Run(in, p, Options{Out: out}); return err }, nil
		})
	})
}

func TestScalingRowInsert(t *testing.T) {
	xlsxtest.Skip(t, func() {
		laws.Scales(t, "xlsx apply row.insert", func(n int) (func() error, error) {
			in := gridBook(t, n*25)
			p := ops.NewPlan(&ops.RowInsert{At: ops.AtRows("Budget!2:2"), Position: ops.Before})
			return func() error { _, err := Run(in, p, Options{DryRun: true}); return err }, nil
		})
	})
}

func TestMemoryBudget(t *testing.T) {
	xlsxtest.Skip(t, func() {
		in := gridBook(t, 200000)
		// N is the sheet's XML (about 35 B a cell); T the shared strings.
		n := float64(200000*10*36) / (1 << 20)
		budget := laws.Budget{PeakLive: laws.MB(1.3*n + 1), Alloc: laws.MB(17.7 * n)}
		p := ops.NewPlan(&ops.CellSet{At: ops.AtCell("Budget!C7"), Value: ops.Num(1)})
		laws.WithinMemory(t, "xlsx apply cell.set", budget, func(mark func(string)) error {
			_, err := Run(in, p, Options{DryRun: true, Checkpoint: mark})
			return err
		})
	})
}
