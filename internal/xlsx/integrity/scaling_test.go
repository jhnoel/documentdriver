package integrity

import (
	"fmt"
	"runtime"
	"strings"
	"testing"

	"github.com/jhnoel/documentdriver/internal/laws"
	"github.com/jhnoel/documentdriver/internal/xlsx/formula"
)

// bigWorkbook is the base workbook with a sheet of n rows of cols cells
// (numbers, shared strings, a shared formula group per ten rows, one
// merge per hundred rows), and the bytes of the sheet part.
func bigWorkbook(t testing.TB, n, cols int) (data []byte, sheetBytes int) {
	t.Helper()
	var b strings.Builder
	b.WriteString(hdrMain + `<worksheet ` + xmlnsS + `><dimension ref="A1:` + cellName(cols, n) + `"/><sheetData>`)
	for r := 1; r <= n; r++ {
		fmt.Fprintf(&b, `<row r="%d" spans="1:%d">`, r, cols)
		for c := 1; c <= cols; c++ {
			switch {
			case c == 1:
				fmt.Fprintf(&b, `<c r="A%d" t="s"><v>%d</v></c>`, r, r%2)
			case c == 2 && r%10 == 1:
				fmt.Fprintf(&b, `<c r="B%d"><f t="shared" ref="B%d:B%d" si="%d">A%d*2</f><v>1</v></c>`, r, r, r+9, (r-1)/10, r)
			case c == 2:
				fmt.Fprintf(&b, `<c r="B%d"><f t="shared" si="%d"/><v>1</v></c>`, r, (r-1)/10)
			default:
				fmt.Fprintf(&b, `<c r="%s%d" s="1"><v>%d</v></c>`, formula.ColumnName(c), r, r*c)
			}
		}
		b.WriteString(`</row>`)
	}
	b.WriteString(`</sheetData>`)
	var m strings.Builder
	k := 0
	for r := 1; r+1 <= n; r += 100 {
		fmt.Fprintf(&m, `<mergeCell ref="%s%d:%s%d"/>`, formula.ColumnName(cols), r, formula.ColumnName(cols), r+1)
		k++
	}
	if k > 0 {
		fmt.Fprintf(&b, `<mergeCells count="%d">%s</mergeCells>`, k, m.String())
	}
	b.WriteString(`</worksheet>`)
	p := baseParts()
	p["xl/worksheets/sheet1.xml"] = b.String()
	// A real package compresses: build it deflated, as parts() does.
	return p.zip(t), b.Len()
}

// TestScaling: Check allocates in proportion to the workbook (make
// scaling's rule: at most 5.5 times the bytes and allocations for four
// times the rows).
func TestScaling(t *testing.T) {
	laws.Scales(t, "integrity.Check", func(n int) (func() error, error) {
		data, _ := bigWorkbook(t, n, 10)
		return func() error {
			if f := Check(data); !f.Empty() {
				return fmt.Errorf("findings: %s", f.dump())
			}
			return nil
		}, nil
	})
}

// TestMemory measures what Check holds while it reads a sheet of 200,000
// rows by 10 columns (the design's budget input): the live heap at the
// checkpoint after the scan, less the part's own text (the package bytes were held before the run).
// The scan state is per row, per shared formula group and per merge, so
// the remainder is a small part of the sheet.
func TestMemory(t *testing.T) {
	if testing.Short() {
		t.Skip("200,000 rows")
	}
	const rows = 200000
	data, sheetBytes := bigWorkbook(t, rows, 10)
	mem, err := laws.MeasureMemory(func(mark func(string)) error {
		if f := checkMarked(data, mark); !f.Empty() {
			return fmt.Errorf("findings: %s", f.dump())
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	// The sheet's text is held once (the part's original string).
	state := int64(mem.PeakLive) - int64(sheetBytes)
	t.Logf("200,000 x 10: sheet %.1f MB, package %.1f MB, peak live %.1f MB at %q, so scan state %.2f MB (%.1f B per row); allocated %.1f MB; GOMAXPROCS %d",
		mb(sheetBytes), mb(len(data)), mb(int(mem.PeakLive)), mem.At, float64(state)/(1<<20), float64(state)/rows, mb(int(mem.Alloc)), runtime.GOMAXPROCS(0))
	// Budget: the part and 64 bytes a row of state (the shared formula
	// group keys and merges are the only per-row keeps).
	if limit := int64(sheetBytes) + 64*rows; int64(mem.PeakLive) > limit {
		t.Errorf("peak live %d bytes exceeds the part plus 64 B per row (%d)", mem.PeakLive, limit)
	}
}

func mb(n int) float64 { return float64(n) / (1 << 20) }
