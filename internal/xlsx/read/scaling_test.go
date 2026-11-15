package read

import (
	"fmt"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/jhnoel/documentdriver/internal/cli"
	"github.com/jhnoel/documentdriver/internal/laws"
	"github.com/jhnoel/documentdriver/internal/xlsx/model"
)

// scaleBook is a sheet of rows x cols cells in Excel's shape: a unique
// shared string per row (column A uses item r-1, so the shared strings
// and their 4 B/item index grow with the rows), numbers, a dated column, a shared formula group per ten rows,
// a dynamic spill every 100 rows, a merge every 100 rows; and the sizes
// of its sheet and shared strings parts.
func scaleBook(t testing.TB, rows, cols int) (input []byte, sheetBytes, sstBytes, sstItems int) {
	t.Helper()
	var b strings.Builder
	for r := 1; r <= rows; r++ {
		fmt.Fprintf(&b, `<row r="%d" spans="1:%d">`, r, cols)
		for c := 1; c <= cols; c++ {
			col := colName(c)
			switch {
			case c == 1:
				fmt.Fprintf(&b, `<c r="A%d" t="s"><v>%d</v></c>`, r, r-1)
			case c == 2 && r%10 == 1:
				fmt.Fprintf(&b, `<c r="B%d"><f t="shared" ref="B%d:B%d" si="%d">C%d*2</f><v>1</v></c>`, r, r, r+9, (r-1)/10, r)
			case c == 2:
				fmt.Fprintf(&b, `<c r="B%d"><f t="shared" si="%d"/><v>1</v></c>`, r, (r-1)/10)
			case c == 3 && r%100 == 1 && r+2 <= rows:
				fmt.Fprintf(&b, `<c r="C%d" cm="1"><f t="array" ref="C%d:C%d">_xlfn.SEQUENCE(3)</f><v>1</v></c>`, r, r, r+2)
			case c == 4:
				fmt.Fprintf(&b, `<c r="D%d" s="1"><v>%d</v></c>`, r, 40000+r%5000)
			default:
				fmt.Fprintf(&b, `<c r="%s%d"><v>%d.5</v></c>`, col, r, r*c)
			}
		}
		b.WriteString(`</row>`)
	}
	var m strings.Builder
	for r := 1; r+1 <= rows; r += 100 {
		fmt.Fprintf(&m, `<mergeCell ref="%s%d:%s%d"/>`, colName(cols), r, colName(cols), r+1)
	}
	sst := make([]string, rows)
	for i := range sst {
		sst[i] = fmt.Sprintf("<t>item %d</t>", i)
	}
	f := fx{sst: sst, metadata: dynamicMetadata, xfs: []int{0, 14}, sheets: []fxSheet{{name: "Big", rows: b.String(), tail: "<mergeCells>" + m.String() + "</mergeCells>"}}}
	p := f.parts()
	data, err := zipParts(p)
	if err != nil {
		t.Fatal(err)
	}
	return data, len(p["xl/worksheets/sheet1.xml"]), len(p["xl/sharedStrings.xml"]), len(sst)
}

// TestScaling: a cell read of the whole sheet, and a find, allocate in
// proportion to the cells (make scaling's rule through laws.Scales: at
// most 5.5 times the bytes and allocations for four times the rows).
func TestScaling(t *testing.T) {
	skipIfUnimplemented(t, func() { probeCells(); probeFind() })
	laws.Scales(t, "xlsx read --sheet", func(n int) (func() error, error) {
		input, _, _, _ := scaleBook(t, n, 10)
		return func() error {
			_, err := Cells(input, CellsRequest{Sheet: "sheet:Big", Limit: 0}, Options{})
			return err
		}, nil
	})
	laws.Scales(t, "xlsx find", func(n int) (func() error, error) {
		input, _, _, _ := scaleBook(t, n, 10)
		return func() error {
			_, err := Find(input, FindRequest{Query: "item 7", View: model.ViewValues, Limit: 0}, Options{})
			return err
		}, nil
	})
}

// TestMemoryBudget measures a cell read of 100,000 rows by 10 columns (1M
// cells, the brief's reporting size) with no limit: the live heap at the
// "cells scanned" checkpoint against the amended budget
//
//	N + T + S + 8 B per row + 4 B per shared string + k x output
//
// with N the input, T the sheet's text, S the shared strings' text
// (held while any sheet is read; the brief's formula does not name it,
// so it is stated here and in doc.go) and k = 2 for the output: the cell
// buffer in CellMap and the encoded result, both alive at the "encoded"
// checkpoint after cli.Marshal. The peak is taken over Cells' own
// checkpoint ("cells scanned") and "encoded". Chunk 8 records the
// measured numbers (and GOMAXPROCS); raising k needs the coordinator's
// agreement.
func TestMemoryBudget(t *testing.T) {
	skipIfUnimplemented(t, probeCells)
	if testing.Short() {
		t.Skip("1M cells")
	}
	const rows, cols, k = 100000, 10, 2
	input, sheetBytes, sstBytes, sstItems := scaleBook(t, rows, cols)
	var out int
	var elapsed time.Duration
	mem, err := laws.MeasureMemory(func(mark func(string)) error {
		start := time.Now()
		res, err := Cells(input, CellsRequest{Sheet: "sheet:Big", Limit: 0}, Options{Checkpoint: mark})
		if err != nil {
			return err
		}
		b, err := cli.Marshal(res)
		if err != nil {
			return err
		}
		out = len(b)
		elapsed = time.Since(start)
		// Both copies of the output alive: the cell buffer in res and
		// the encoded result, so k=2 is measured, not assumed.
		mark("encoded")
		runtime.KeepAlive(res)
		runtime.KeepAlive(b)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	budget := int64(len(input) + sheetBytes + sstBytes + 8*rows + 4*sstItems + k*out)
	t.Logf("1M cells in %v: input %.1f MB, sheet %.1f MB, sst %.2f MB, output %.1f MB; peak live %.1f MB at %q (budget %.1f MB); allocated %.1f MB; GOMAXPROCS %d",
		elapsed.Round(time.Millisecond), mb(len(input)), mb(sheetBytes), mb(sstBytes), mb(out), mb(int(mem.PeakLive)), mem.At, mb(int(budget)), mb(int(mem.Alloc)), runtime.GOMAXPROCS(0))
	if int64(mem.PeakLive) > budget {
		t.Errorf("peak live %d bytes exceeds the budget %d", mem.PeakLive, budget)
	}
}

func mb(n int) float64 { return float64(n) / (1 << 20) }

// TestScalingOpenManySheets: opening a workbook costs in proportion to its
// sheets (a 1,024-sheet corpus file opened in 72 ms when each sheet's
// relationship was looked up by walking them all; 6 ms after). Bytes
// allocated by openBook at four times the sheets stay within make
// scaling's 5.5 times.
func TestScalingOpenManySheets(t *testing.T) {
	allocated := func(n int) uint64 {
		f := fx{noStyles: true}
		for i := 0; i < n; i++ {
			f.sheets = append(f.sheets, fxSheet{name: fmt.Sprintf("S%d", i), rows: `<row r="1"><c r="A1"><v>1</v></c></row>`})
		}
		input := f.zip(t)
		var before, after runtime.MemStats
		runtime.ReadMemStats(&before)
		b, err := openBook(input, Options{})
		runtime.ReadMemStats(&after)
		if err != nil {
			t.Fatal(err)
		}
		b.Close()
		return after.TotalAlloc - before.TotalAlloc
	}
	small, large := allocated(250), allocated(1000)
	t.Logf("openBook: 250 sheets %.2f MB, 1000 sheets %.2f MB (ratio %.2f)", mb(int(small)), mb(int(large)), float64(large)/float64(small))
	if float64(large) > laws.MaxRatio*float64(small) {
		t.Errorf("openBook allocates %.1f times as much for 4 times the sheets", float64(large)/float64(small))
	}
}
