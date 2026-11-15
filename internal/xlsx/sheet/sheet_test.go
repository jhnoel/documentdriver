package sheet

import (
	"errors"
	"fmt"
	"reflect"
	"runtime"
	"strconv"
	"strings"
	"testing"

	"github.com/jhnoel/documentdriver/internal/xlsx/formula"
)

const ns = `xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main" xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships" xmlns:x14ac="http://schemas.microsoft.com/office/spreadsheetml/2009/9/ac"`

// ws builds a worksheet from its head (before sheetData), rows and tail.
func ws(head, rows, tail string) string {
	return `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>` + "\n" +
		`<worksheet ` + ns + `>` + head + `<sheetData>` + rows + `</sheetData>` + tail + `</worksheet>`
}

// collectRows scans src and returns the band's rows as copies.
func collectRows(t *testing.T, src string, opt ScanOptions) (*Index, []Row) {
	t.Helper()
	var got []Row
	opt.Row = func(r *Row) error {
		got = append(got, Row{Num: r.Num, Hidden: r.Hidden, Cells: append([]RawCell(nil), r.Cells...)})
		return nil
	}
	if opt.To == 0 {
		opt.From, opt.To = 1, formula.MaxRows
	}
	ix, err := Scan(src, opt)
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	return ix, got
}

func TestScanCells(t *testing.T) {
	src := ws(`<dimension ref="A1:D3"/>`,
		`<row r="1" spans="1:4" x14ac:dyDescent="0.25"><c r="A1" t="s"><v>0</v></c><c r="B1" s="3"><v>1.5</v></c><c r="C1" t="b"><v>1</v></c><c r="D1" t="e"><v>#N/A</v></c></row>`+
			`<row r="2"><c r="A2" t="inlineStr"><is><r><rPr><b/></rPr><t>Va</t></r><r><t xml:space="preserve">lue </t></r><rPh sb="0" eb="1"><t>ignored</t></rPh></is></c>`+
			`<c r="B2" t="str"><f>"a"&amp;"b"</f><v>ab</v></c><c r="C2" s="1"/><c r="D2" t="d"><v>2024-03-01T00:00:00</v></c></row>`+
			`<row r="3"><c r="A3" cm="1" vm="2"><f t="array" ref="A3:A5">SEQUENCE(3)</f><v>1</v></c></row>`, "")
	ix, rows := collectRows(t, src, ScanOptions{})
	if !ix.HasDimension || ix.Dimension != "A1:D3" || !ix.Ordered || ix.Rows() != 3 {
		t.Fatalf("index: dimension %q %v, ordered %v, rows %d", ix.Dimension, ix.HasDimension, ix.Ordered, ix.Rows())
	}
	want := [][]RawCell{
		{
			{Row: 1, Col: 1, T: "s", SOK: true, V: "0", HasV: true},
			{Row: 1, Col: 2, S: 3, SOK: true, V: "1.5", HasV: true},
			{Row: 1, Col: 3, T: "b", SOK: true, V: "1", HasV: true},
			{Row: 1, Col: 4, T: "e", SOK: true, V: "#N/A", HasV: true},
		},
		{
			{Row: 2, Col: 1, T: "inlineStr", SOK: true, IS: "Value ", HasIS: true},
			{Row: 2, Col: 2, T: "str", SOK: true, V: "ab", HasV: true, F: Formula{Present: true, Text: `"a"&"b"`}},
			{Row: 2, Col: 3, S: 1, SOK: true},
			{Row: 2, Col: 4, T: "d", SOK: true, V: "2024-03-01T00:00:00", HasV: true},
		},
		{
			{Row: 3, Col: 1, SOK: true, CM: 1, VM: 2, V: "1", HasV: true, F: Formula{Present: true, Type: Array, Text: "SEQUENCE(3)", Ref: "A3:A5"}},
		},
	}
	if len(rows) != len(want) {
		t.Fatalf("rows: got %d, want %d", len(rows), len(want))
	}
	for i := range want {
		if !reflect.DeepEqual(rows[i].Cells, want[i]) {
			t.Errorf("row %d:\n got %+v\nwant %+v", i+1, rows[i].Cells, want[i])
		}
	}
	if r1, c1, r2, c2, ok := ix.UsedRange(); !ok || r1 != 1 || c1 != 1 || r2 != 3 || c2 != 4 {
		t.Errorf("used range %d,%d:%d,%d %v; the style-only C2 must not count", r1, c1, r2, c2, ok)
	}
}

// TestScanImplicitPositions: rows and cells without r take the next
// position (Excel accepts them; some writers omit r).
func TestScanImplicitPositions(t *testing.T) {
	src := ws("", `<row><c><v>1</v></c><c><v>2</v></c><c r="E1"><v>3</v></c><c><v>4</v></c></row><row><c><v>5</v></c></row><row r="7"><c><v>6</v></c></row>`, "")
	_, rows := collectRows(t, src, ScanOptions{})
	var got []string
	for _, r := range rows {
		for _, c := range r.Cells {
			got = append(got, formula.ColumnName(c.Col)+fmt.Sprint(c.Row)+"="+c.V)
		}
	}
	if want := "A1=1 B1=2 E1=3 F1=4 A2=5 A7=6"; strings.Join(got, " ") != want {
		t.Errorf("got %s, want %s", strings.Join(got, " "), want)
	}
}

// TestScanProblems: out-of-order, repeated and off-grid rows and cells
// are skipped and reported, never an error.
func TestScanProblems(t *testing.T) {
	src := ws("", `<row r="2"><c r="A2"><v>1</v></c><c r="A2"><v>dup</v></c><c r="B3"><v>wrongrow</v></c><c r="XFE2"><v>off</v></c><c r="C2"><v>2</v></c></row>`+
		`<row r="1"><c r="A1"><v>early</v></c></row><row r="1048577"><c><v>off</v></c></row><row r="x"/><row r="3"><c r="Z"><v>bad</v></c><c r="B3"><v>3</v></c></row>`,
		`<mergeCells count="2"><mergeCell ref="A1:B2"/><mergeCell ref="nonsense"/></mergeCells>`)
	var probs []string
	ix, rows := collectRows(t, src, ScanOptions{Problem: func(p Problem, row, col int, _ string) {
		probs = append(probs, fmt.Sprintf("%d@%d,%d", p, row, col))
	}})
	var got []string
	for _, r := range rows {
		for _, c := range r.Cells {
			got = append(got, c.V)
		}
	}
	if strings.Join(got, ",") != "1,2,3" {
		t.Errorf("cells read: %v", got)
	}
	if ix.Ordered {
		t.Error("Ordered after a row out of order")
	}
	wantProbs := []string{"1@2,1", "2@2,2", "2@2,0", "1@1,0", "2@0,0", "2@0,0", "2@3,0", "2@0,0"}
	if !reflect.DeepEqual(probs, wantProbs) {
		t.Errorf("problems %v, want %v", probs, wantProbs)
	}
	if m := ix.Merges(); len(m) != 1 || m[0].String() != "A1:B2" {
		t.Errorf("merges %v", m)
	}
}

func TestScanBand(t *testing.T) {
	var b strings.Builder
	for r := 1; r <= 10; r++ {
		fmt.Fprintf(&b, `<row r="%d"><c r="A%d"><v>%d</v></c><c r="B%d"><v>%d</v></c><c r="C%d"><v>%d</v></c></row>`, r, r, r, r, r*10, r, r*100)
	}
	src := ws("", b.String(), `<mergeCells><mergeCell ref="B9:C10"/></mergeCells>`)
	ix, rows := collectRows(t, src, ScanOptions{From: 3, To: 5, ColFrom: 2, ColTo: 2})
	var got []string
	for _, r := range rows {
		for _, c := range r.Cells {
			got = append(got, c.V)
		}
	}
	if strings.Join(got, ",") != "30,40,50" {
		t.Errorf("band cells %v", got)
	}
	// The Index covers the whole sheet whatever the band.
	if _, _, r2, c2, _ := ix.UsedRange(); r2 != 10 || c2 != 3 || ix.Rows() != 10 || len(ix.Merges()) != 1 {
		t.Errorf("index after a band: used to %d,%d, %d rows, %d merges", r2, c2, ix.Rows(), len(ix.Merges()))
	}
	// ErrStopRows ends the band but not the index.
	n := 0
	ix, err := Scan(src, ScanOptions{From: 1, To: 10, Row: func(*Row) error {
		n++
		if n == 2 {
			return ErrStopRows
		}
		return nil
	}})
	if err != nil || n != 2 || ix.Rows() != 10 || len(ix.Merges()) != 1 {
		t.Errorf("stop: err %v, %d rows handed, %d indexed, %d merges", err, n, ix.Rows(), len(ix.Merges()))
	}
	// Another error stops Scan.
	boom := errors.New("boom")
	if _, err := Scan(src, ScanOptions{From: 1, To: 10, Row: func(*Row) error { return boom }}); !errors.Is(err, boom) {
		t.Errorf("callback error: %v", err)
	}
	// Row offsets point at the row's start tag.
	for i := 0; i < ix.Rows(); i++ {
		num, _, off := ix.Row(i)
		if !strings.HasPrefix(src[off:], fmt.Sprintf(`<row r="%d"`, num)) {
			t.Errorf("row %d offset %d: %q", num, off, src[off:off+12])
		}
	}
}

func TestScanHidden(t *testing.T) {
	src := ws(`<cols><col min="5" max="6" hidden="1"/><col min="1" max="1" width="9"/><col min="3" max="3" hidden="true"/><col min="7" max="7" hidden="1"/></cols>`,
		`<row r="2" hidden="1"/><row r="3" hidden="1"><c r="A3"><v>1</v></c></row><row r="4" ht="0" customHeight="1"><c r="A4"><v>2</v></c></row><row r="6" hidden="1"/>`, "")
	ix, _ := collectRows(t, src, ScanOptions{})
	if got := ix.HiddenRows(1, 100); !reflect.DeepEqual(got, [][2]int{{2, 3}, {6, 6}}) {
		t.Errorf("hidden rows %v (a zero-height row is not hidden)", got)
	}
	if got := ix.HiddenRows(3, 5); !reflect.DeepEqual(got, [][2]int{{3, 3}}) {
		t.Errorf("hidden rows clipped %v", got)
	}
	if got := ix.HiddenCols(1, formula.MaxCols); !reflect.DeepEqual(got, [][2]int{{3, 3}, {5, 7}}) {
		t.Errorf("hidden cols %v", got)
	}
	if got := ix.HiddenCols(6, 6); !reflect.DeepEqual(got, [][2]int{{6, 6}}) {
		t.Errorf("hidden cols clipped %v", got)
	}
}

// TestScanAnchors: members of an array or data table, including those
// of a band that starts below the anchor, name their anchor.
func TestScanAnchors(t *testing.T) {
	src := ws("",
		`<row r="1"><c r="A1" cm="1"><f t="array" ref="A1:B3">SEQUENCE(3,2)</f><v>1</v></c><c r="B1"><v>2</v></c><c r="C1"><f t="array" ref="C1">SUM(A1:A3*B1:B3)</f><v>9</v></c><c r="D1"><v>x</v></c></row>`+
			`<row r="2"><c r="A2"><v>3</v></c><c r="B2"><v>4</v></c><c r="E2"><f t="dataTable" ref="E2:F3" dt2D="1" dtr="1" r1="A1" r2="B1"/><v>5</v></c><c r="F2"><v>6</v></c></row>`+
			`<row r="3"><c r="A3"><v>5</v></c><c r="B3"><f>A3</f><v>5</v></c><c r="E3"><v>7</v></c><c r="F3"><v>8</v></c></row>`+
			`<row r="4"><c r="A4"><v>none</v></c><c r="F4"><v>none</v></c></row>`, "")
	ix, rows := collectRows(t, src, ScanOptions{From: 2, To: 4})
	an := ix.Anchors()
	if len(an) != 3 || an[0].Ref.String() != "A1:B3" || an[0].CM != 1 || an[1].Ref.String() != "C1" || an[2].Type != DataTable {
		t.Fatalf("anchors %+v", an)
	}
	got := map[string]int{}
	for _, r := range rows {
		for _, c := range r.Cells {
			got[formula.ColumnName(c.Col)+fmt.Sprint(c.Row)] = c.In
		}
	}
	want := map[string]int{"A2": 1, "B2": 1, "E2": 0, "F2": 3, "A3": 1, "B3": 0, "E3": 3, "F3": 3, "A4": 0, "F4": 0}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("membership\n got %v\nwant %v (B3 has its own formula; E2 is the anchor)", got, want)
	}
	_, rows = collectRows(t, src, ScanOptions{From: 1, To: 1})
	for _, c := range rows[0].Cells {
		if w := map[int]int{1: 0, 2: 1, 3: 0, 4: 0}[c.Col]; c.In != w {
			t.Errorf("row 1 col %d: In %d, want %d", c.Col, c.In, w)
		}
	}
	if f := ix.Anchors()[2]; !reflect.DeepEqual([]any{f.Row, f.Col}, []any{2, 5}) {
		t.Errorf("data table anchor at %d,%d", f.Row, f.Col)
	}
}

// TestScanAnchorsMany: many anchors active at once (a column of
// single-row arrays beside long column arrays) stay correct.
func TestScanAnchorsMany(t *testing.T) {
	var b strings.Builder
	const n = 300
	for r := 1; r <= n; r++ {
		fmt.Fprintf(&b, `<row r="%d">`, r)
		if r == 1 {
			for c := 1; c <= 50; c += 2 {
				col := formula.ColumnName(c)
				fmt.Fprintf(&b, `<c r="%s1"><f t="array" ref="%s1:%s%d">X()</f><v>0</v></c>`, col, col, col, n)
				fmt.Fprintf(&b, `<c r="%s1"><v>free</v></c>`, formula.ColumnName(c+1))
			}
		} else {
			for c := 1; c <= 50; c++ {
				fmt.Fprintf(&b, `<c r="%s%d"><v>%d</v></c>`, formula.ColumnName(c), r, c)
			}
		}
		fmt.Fprintf(&b, `<c r="BA%d"><f t="array" ref="BA%d:BB%d">Y()</f><v>0</v></c><c r="BB%d"><v>m</v></c>`, r, r, r, r)
		b.WriteString(`</row>`)
	}
	_, rows := collectRows(t, ws("", b.String(), ""), ScanOptions{})
	for _, r := range rows {
		for _, c := range r.Cells {
			member := c.In != 0
			var want bool
			switch {
			case c.Col <= 50:
				want = c.Col%2 == 1 && r.Num > 1
			case c.Col == 54: // BB
				want = true
			}
			if member != want {
				t.Fatalf("%s%d: member %v, want %v", formula.ColumnName(c.Col), r.Num, member, want)
			}
		}
	}
}

func TestScanSharedMasters(t *testing.T) {
	src := ws("", `<row r="1"><c r="A1"><f t="shared" ref="A1:A3" si="0">B1*2</f><v>2</v></c></row>`+
		`<row r="2"><c r="A2"><f t="shared" si="0"/><v>4</v></c><c r="B2"><f t="shared" ref="B2:C2" si="7">A2&amp;"x"</f></c></row>`, "")
	var seen []bool
	ix, err := Scan(src, ScanOptions{From: 2, To: 2, Row: func(r *Row) error {
		_, ok := r.Cells[0].F.SI, r.Cells[0].F.SIOK
		seen = append(seen, ok)
		return nil
	}})
	if err != nil {
		t.Fatal(err)
	}
	m, ok := ix.Master(0)
	if !ok || m.Row != 1 || m.Col != 1 || m.Text != "B1*2" || !m.RefOK || m.Ref.String() != "A1:A3" {
		t.Errorf("master 0: %+v %v", m, ok)
	}
	if m, ok := ix.Master(7); !ok || m.Text != `A2&"x"` {
		t.Errorf("master 7: %+v %v", m, ok)
	}
	if _, ok := ix.Master(1); ok {
		t.Error("master 1 exists")
	}
	if len(seen) != 1 || !seen[0] {
		t.Errorf("child si: %v", seen)
	}
}

func TestScanNamespaces(t *testing.T) {
	prefixed := `<x:worksheet xmlns:x="http://schemas.openxmlformats.org/spreadsheetml/2006/main"><x:sheetData><x:row r="1"><x:c r="A1" t="inlineStr"><x:is><x:t>hi</x:t></x:is></x:c><y:c xmlns:y="urn:other" r="B1"><y:v>no</y:v></y:c></x:row></x:sheetData></x:worksheet>`
	strict := `<worksheet xmlns="http://purl.oclc.org/ooxml/spreadsheetml/main"><sheetData><row r="1"><c r="A1" t="d"><v>2024-01-02</v></c></row></sheetData></worksheet>`
	for name, tc := range map[string]struct{ src, want string }{
		"prefixed": {prefixed, "A1:hi"},
		"strict":   {strict, "A1:2024-01-02"},
	} {
		_, rows := collectRows(t, tc.src, ScanOptions{})
		var got []string
		for _, r := range rows {
			for _, c := range r.Cells {
				got = append(got, formula.ColumnName(c.Col)+fmt.Sprint(c.Row)+":"+c.V+c.IS)
			}
		}
		if strings.Join(got, " ") != tc.want {
			t.Errorf("%s: %v", name, got)
		}
	}
	for _, src := range []string{
		`<chartsheet xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main"/>`,
		`<worksheet xmlns="urn:not-sml"><sheetData/></worksheet>`,
	} {
		if _, err := Scan(src, ScanOptions{}); !errors.Is(err, ErrNotWorksheet) {
			t.Errorf("%s: %v", src, err)
		}
	}
	if _, err := Scan(`<worksheet`, ScanOptions{}); err == nil || errors.Is(err, ErrNotWorksheet) {
		t.Errorf("malformed: %v", err)
	}
}

func TestSST(t *testing.T) {
	plain := `<?xml version="1.0"?><sst xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main" count="5" uniqueCount="5">` +
		`<si><t>Name</t></si>  <si><r><rPr><b/></rPr><t>Va</t></r><r><t xml:space="preserve">lue </t></r><rPh sb="0" eb="1"><t>ignored</t></rPh><phoneticPr fontId="1"/></si>` +
		`<!-- c --><si/><si><t>a_x000D_b _x005F_x0041_ &amp;&lt;</t></si><si><t></t></si><extLst><ext uri="u"><x/></ext></extLst></sst>`
	prefixed := `<x:sst xmlns:x="http://schemas.openxmlformats.org/spreadsheetml/2006/main"><x:si><x:t>one</x:t></x:si><x:si><x:r><x:t>t</x:t></x:r><x:r><x:t>wo</x:t></x:r></x:si></x:sst>`
	for name, tc := range map[string]struct {
		src  string
		want []string
	}{
		"plain":    {plain, []string{"Name", "Value ", "", "a\rb _x0041_ &<", ""}},
		"prefixed": {prefixed, []string{"one", "two"}},
	} {
		s, err := OpenSST(tc.src)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if s.Len() != len(tc.want) {
			t.Fatalf("%s: Len %d, want %d", name, s.Len(), len(tc.want))
		}
		for i, w := range tc.want {
			if got, err := s.Text(i); err != nil || got != w {
				t.Errorf("%s: Text(%d) = %q, %v; want %q", name, i, got, err, w)
			}
		}
		// Out of order access decodes the same.
		for i := len(tc.want) - 1; i >= 0; i-- {
			if got, _ := s.Text(i); got != tc.want[i] {
				t.Errorf("%s: reverse Text(%d) = %q", name, i, got)
			}
		}
		var each []string
		if err := s.Each(func(i int, text string) error {
			if i != len(each) {
				t.Errorf("%s: Each index %d at %d", name, i, len(each))
			}
			each = append(each, text)
			return nil
		}); err != nil {
			t.Fatalf("%s: Each: %v", name, err)
		}
		if !reflect.DeepEqual(each, tc.want) {
			t.Errorf("%s: Each %q", name, each)
		}
		for _, i := range []int{-1, len(tc.want)} {
			if _, err := s.Text(i); err == nil {
				t.Errorf("%s: Text(%d) succeeded", name, i)
			}
		}
	}
	for _, bad := range []string{`<sst`, `<other/>`, `<sst xmlns="urn:x"/>`} {
		if _, err := OpenSST(bad); err == nil {
			t.Errorf("OpenSST(%q) succeeded", bad)
		}
	}
	stop := errors.New("stop")
	s, _ := OpenSST(plain)
	if err := s.Each(func(int, string) error { return stop }); !errors.Is(err, stop) {
		t.Errorf("Each error: %v", err)
	}
}

func TestHead(t *testing.T) {
	for _, tc := range []struct {
		src  string
		dim  string
		ok   bool
		fail bool
	}{
		{ws(`<sheetPr><tabColor rgb="FF0000"/></sheetPr><dimension ref="A1:D9"/>`, `<row r="1"/>`, ""), "A1:D9", true, false},
		{ws("", `<row r="1"/>`, ""), "", false, false},
		// Malformed after sheetData starts: not read, not an error.
		{`<worksheet xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main"><dimension ref="A1"/><sheetData><row>`, "A1", true, false},
		{`<chartsheet xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main"/>`, "", false, true},
		{`<worksheet xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main"><dimension ref="A1"`, "", false, true},
	} {
		dim, ok, err := Head(tc.src)
		if (err != nil) != tc.fail || dim != tc.dim || ok != tc.ok {
			t.Errorf("Head(%.60q) = %q, %v, %v", tc.src, dim, ok, err)
		}
	}
}

// TestSSTLookupLinear: a lookup costs in proportion to its item, not to
// the rest of the table, so n lookups are O(n). Wall time flakes under
// parallel test load, so it measures two load-independent costs per
// lookup at 20k and 80k items: the bytes Text gives the tokenizer (its
// work is linear in them; the quadratic version gave it the rest of the
// part) and the bytes allocated (runtime.MemStats.TotalAlloc). Linear
// gives a ratio of about 1; the test allows 1.5 (quadratic was 4).
func TestSSTLookupLinear(t *testing.T) {
	if testing.Short() {
		t.Skip("timing")
	}
	build := func(n int, prefixed bool) *SST {
		var b strings.Builder
		p := ""
		if prefixed {
			p = "x:"
			b.WriteString(`<x:sst xmlns:x="http://schemas.openxmlformats.org/spreadsheetml/2006/main">`)
		} else {
			b.WriteString(`<sst xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main">`)
		}
		for i := 0; i < n; i++ {
			fmt.Fprintf(&b, "<%ssi><%st>item %d</%st></%ssi>", p, p, i, p, p)
		}
		fmt.Fprintf(&b, "</%ssst>", p)
		s, err := OpenSST(b.String())
		if err != nil {
			t.Fatal(err)
		}
		return s
	}
	for _, prefixed := range []bool{false, true} {
		perLookup := func(n int) (fed, alloc float64) {
			s := build(n, prefixed)
			s.fed = 0
			var before, after runtime.MemStats
			runtime.ReadMemStats(&before)
			for i := 0; i < n; i++ {
				if got, err := s.Text(i); err != nil || got != "item "+strconv.Itoa(i) {
					t.Fatalf("Text(%d) = %q, %v", i, got, err)
				}
			}
			runtime.ReadMemStats(&after)
			return float64(s.fed) / float64(n), float64(after.TotalAlloc-before.TotalAlloc) / float64(n)
		}
		fedS, allocS := perLookup(20000)
		fedL, allocL := perLookup(80000)
		t.Logf("prefixed %v: per lookup at 20k / 80k items: tokenized %.0f / %.0f B, allocated %.0f / %.0f B", prefixed, fedS, fedL, allocS, allocL)
		if fedL > 1.5*fedS || allocL > 1.5*allocS {
			t.Errorf("prefixed %v: lookups cost more as the table grows (tokenized %.0f -> %.0f B, allocated %.0f -> %.0f B)", prefixed, fedS, fedL, allocS, allocL)
		}
	}
}

// TestScanSharedReusedSI: an si reused within one row resolves each
// child to the master before it, not to the row's last master (the
// master is resolved when the cell is read, not in the row callback).
func TestScanSharedReusedSI(t *testing.T) {
	src := ws("", `<row r="1"><c r="A1"><f t="shared" ref="A1:B1" si="0">X1+1</f></c><c r="B1"><f t="shared" si="0"/></c>`+
		`<c r="C1"><f t="shared" ref="C1:D1" si="0">Z1*2</f></c><c r="D1"><f t="shared" si="0"/></c><c r="E1"><f t="shared" si="3"/></c></row>`+
		`<row r="2"><c r="A2"><f t="shared" si="0"/></c></row>`, "")
	got := map[string]string{}
	_, err := Scan(src, ScanOptions{From: 1, To: 2, Row: func(r *Row) error {
		for _, c := range r.Cells {
			key := formula.ColumnName(c.Col) + fmt.Sprint(c.Row)
			if c.Master == 0 {
				got[key] = "none"
				continue
			}
			m := r.Index.Masters()[c.Master-1]
			got[key] = formula.ColumnName(m.Col) + fmt.Sprint(m.Row) + ":" + m.Text
		}
		return nil
	}})
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{"A1": "A1:X1+1", "B1": "A1:X1+1", "C1": "C1:Z1*2", "D1": "C1:Z1*2", "E1": "none", "A2": "C1:Z1*2"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("masters\n got %v\nwant %v", got, want)
	}
}
