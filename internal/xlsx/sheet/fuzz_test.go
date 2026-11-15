package sheet

import "testing"

// FuzzScan scans arbitrary worksheet text: it never panics, rows come in
// ascending order, cells in ascending columns, and every In names an
// anchor whose ref holds the cell.
func FuzzScan(f *testing.F) {
	f.Add(ws(`<dimension ref="A1"/><cols><col min="2" max="3" hidden="1"/></cols>`, `<row r="1"><c r="A1" cm="1"><f t="array" ref="A1:B2">X()</f><v>1</v></c><c r="B1"><v>2</v></c></row><row r="2" hidden="1"><c><v>3</v></c><c t="inlineStr"><is><r><t>a</t></r></is></c></row>`, `<mergeCells><mergeCell ref="C1:D2"/></mergeCells>`))
	f.Add(ws("", `<row r="3"/><row r="2"><c r="A2"><f t="shared" ref="A2:A3" si="0">B2</f></c><c r="A2"/></row>`, ""))
	f.Add(`<x:worksheet xmlns:x="http://schemas.openxmlformats.org/spreadsheetml/2006/main"><x:sheetData><x:row><x:c><x:v>1</x:v></x:c></x:row></x:sheetData></x:worksheet>`)
	f.Fuzz(func(t *testing.T, src string) {
		lastRow := 0
		ix, err := Scan(src, ScanOptions{From: 1, To: 1 << 20, Row: func(r *Row) error {
			if r.Num <= lastRow {
				t.Fatalf("row %d after %d", r.Num, lastRow)
			}
			lastRow = r.Num
			lastCol := 0
			for _, c := range r.Cells {
				if c.Col <= lastCol || c.Row != r.Num {
					t.Fatalf("cell %d,%d after column %d in row %d", c.Row, c.Col, lastCol, r.Num)
				}
				lastCol = c.Col
				if c.In != 0 {
					a := r.Index.Anchors()[c.In-1]
					if !a.Ref.Contains(c.Row, c.Col) || a.Row == c.Row && a.Col == c.Col || c.F.Present {
						t.Fatalf("cell %d,%d In anchor %+v", c.Row, c.Col, a)
					}
				}
			}
			return nil
		}})
		if err != nil {
			return
		}
		for i := 1; i < ix.Rows(); i++ {
			a, _, _ := ix.Row(i - 1)
			b, _, _ := ix.Row(i)
			if b <= a {
				t.Fatalf("index rows %d then %d", a, b)
			}
		}
		if _, _, err := Head(src); err != nil && err != ErrNotWorksheet {
			// Head reads less than Scan: it cannot fail where Scan passed.
			t.Fatalf("Head failed where Scan passed: %v", err)
		}
	})
}

// FuzzSST indexes arbitrary shared strings: it never panics, and Text
// of every item equals what Each streams.
func FuzzSST(f *testing.F) {
	f.Add(`<sst xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main"><si><t>a</t></si><si><r><t>b</t></r><rPh><t>x</t></rPh></si><si/></sst>`)
	f.Add(`<x:sst xmlns:x="http://schemas.openxmlformats.org/spreadsheetml/2006/main"><x:si><x:t>_x0041_</x:t></x:si></x:sst>`)
	f.Fuzz(func(t *testing.T, src string) {
		s, err := OpenSST(src)
		if err != nil {
			return
		}
		var each []string
		if err := s.Each(func(_ int, text string) error { each = append(each, text); return nil }); err != nil {
			t.Fatalf("Each failed where OpenSST passed: %v", err)
		}
		if len(each) != s.Len() {
			t.Fatalf("Each gave %d items, Len %d", len(each), s.Len())
		}
		for i, want := range each {
			if got, err := s.Text(i); err != nil || got != want {
				t.Fatalf("Text(%d) = %q, %v; Each %q", i, got, err, want)
			}
		}
	})
}
