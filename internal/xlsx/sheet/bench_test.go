package sheet

import (
	"fmt"
	"strings"
	"testing"

	"github.com/jhnoel/documentdriver/internal/ooxml/tree"
)

// benchSheet is a worksheet of rows x 10 cells in Excel's shape: a shared
// string, a shared formula, numbers.
func benchSheet(rows int) string {
	var b strings.Builder
	for r := 1; r <= rows; r++ {
		fmt.Fprintf(&b, `<row r="%d" spans="1:10" x14ac:dyDescent="0.25"><c r="A%d" t="s"><v>%d</v></c>`, r, r, r%100)
		if r%10 == 1 {
			fmt.Fprintf(&b, `<c r="B%d"><f t="shared" ref="B%d:B%d" si="%d">C%d*2</f><v>1</v></c>`, r, r, r+9, r/10, r)
		} else {
			fmt.Fprintf(&b, `<c r="B%d"><f t="shared" si="%d"/><v>1</v></c>`, r, r/10)
		}
		for c := 'C'; c <= 'J'; c++ {
			fmt.Fprintf(&b, `<c r="%c%d" s="1"><v>%d.25</v></c>`, c, r, r)
		}
		b.WriteString(`</row>`)
	}
	return ws(`<dimension ref="A1:J1"/>`, b.String(), "")
}

// BenchmarkScan: the whole sheet as the band (a read with no limit), and
// the index alone (find's and the edit overlay's pass).
func BenchmarkScan(b *testing.B) {
	src := benchSheet(100000)
	b.SetBytes(int64(len(src)))
	for _, band := range []bool{true, false} {
		b.Run(fmt.Sprintf("band=%v", band), func(b *testing.B) {
			b.ReportAllocs()
			opt := ScanOptions{}
			if band {
				n := 0
				opt = ScanOptions{From: 1, To: 1 << 20, Row: func(r *Row) error { n += len(r.Cells); return nil }}
			}
			for b.Loop() {
				if _, err := Scan(src, opt); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

type nopVisitor struct{}

func (nopVisitor) Start(*tree.Node) error { return nil }
func (nopVisitor) End(*tree.Node) error   { return nil }
func (nopVisitor) Leaf(*tree.Node) error  { return nil }

// BenchmarkTokenizer is the floor Scan stands on: tree.ScanString with a
// visitor that does nothing.
func BenchmarkTokenizer(b *testing.B) {
	src := benchSheet(100000)
	b.SetBytes(int64(len(src)))
	b.ReportAllocs()
	for b.Loop() {
		if err := tree.ScanString(src, nopVisitor{}); err != nil {
			b.Fatal(err)
		}
	}
}
