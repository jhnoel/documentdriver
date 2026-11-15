package read

import (
	"bytes"
	"fmt"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/jhnoel/documentdriver/internal/cli"
	"github.com/jhnoel/documentdriver/internal/xlsx/model"
)

func probeCells() { _, _ = Cells(nil, CellsRequest{Sheet: "sheet:S"}, Options{}) }

func probeGroup() { (&grouper{}).finish("S", &CellMap{}, model.ViewFormulas) }

// readCells runs a cell read that must succeed.
func readCells(t *testing.T, input []byte, req CellsRequest) *CellsResult {
	t.Helper()
	res, err := Cells(input, req, Options{})
	if err != nil {
		t.Fatalf("Cells(%+v): %v", req, err)
	}
	return res
}

// sameCells compares a result's cells with want (compact JSON by key).
func sameCells(t *testing.T, name string, res *CellsResult, want map[string]string) {
	t.Helper()
	got, _ := cellsJSON(t, res)
	for k, w := range want {
		if got[k] != w {
			t.Errorf("%s: %s = %s, want %s", name, k, got[k], w)
		}
	}
	for k, g := range got {
		if _, ok := want[k]; !ok {
			t.Errorf("%s: unexpected %s = %s", name, k, g)
		}
	}
}

// TestValueForms: every value form of §7a, every t, dates through the
// number format, and the cells left out.
func TestValueForms(t *testing.T) {
	skipIfUnimplemented(t, probeCells)
	input := valuesBook().zip(t)
	for _, view := range []model.View{model.ViewCells, model.ViewValues} {
		res := readCells(t, input, CellsRequest{Sheet: "sheet:Values", View: view, Limit: DefaultLimit})
		sameCells(t, view.String(), res, valuesWant)
		if res.Sheet != "sheet:Values" || res.View != view.String() || res.Range != "Values!A1:K6" || res.UsedRange != "Values!A1:K6" {
			t.Errorf("%s: sheet %q view %q range %q used %q", view, res.Sheet, res.View, res.Range, res.UsedRange)
		}
	}
	res := readCells(t, input, CellsRequest{Sheet: "sheet:Values", Limit: DefaultLimit})
	kinds := noteKinds(res.Notes)
	for k, n := range map[model.NoteKind]int{
		// E1 bad index, E2 NaN, F2 not a number, C3 bool 2, H3 unknown t,
		// D5 unparsable t="d", C6 no index: one aggregated note per
		// distinct reason, so at least 4 and at most 7.
		model.NoteDuration:  1, // D4
		model.NoteSerial:    1, // E4 and J4 share a reason or not: at least 1
		model.NoteRichValue: 1, // A6
	} {
		if kinds[k] < n {
			t.Errorf("%s notes: %d, want at least %d (%+v)", k, kinds[k], n, res.Notes)
		}
	}
	if v := kinds[model.NoteValue]; v < 4 || v > 7 {
		t.Errorf("value notes: %d (%+v)", v, res.Notes)
	}
	if !slices.ContainsFunc(res.Notes, func(n model.Note) bool { return n.At == "Values!I4" }) {
		t.Errorf("no note for the out-of-range style of I4: %+v", res.Notes)
	}
	// The formulas view of a sheet without formulas is empty.
	res = readCells(t, input, CellsRequest{Sheet: "sheet:Values", View: model.ViewFormulas, Limit: DefaultLimit})
	if res.Cells.Len() != 0 {
		t.Errorf("formulas view: %d cells", res.Cells.Len())
	}
}

// TestCellOrder: cells are keyed by bare address, in row-major order.
func TestCellOrder(t *testing.T) {
	skipIfUnimplemented(t, probeCells)
	res := readCells(t, bigBook(12, 3).zip(t), CellsRequest{Sheet: "sheet:Big", Limit: 0})
	_, keys := cellsJSON(t, res)
	var want []string
	for r := 1; r <= 12; r++ {
		for c := 1; c <= 3; c++ {
			want = append(want, colName(c)+fmt.Sprint(r))
		}
	}
	if !reflect.DeepEqual(keys, want) {
		t.Errorf("keys %v", keys)
	}
}

func TestDate1904(t *testing.T) {
	skipIfUnimplemented(t, probeCells)
	f := valueStyles(fx{workbookPr: `<workbookPr date1904="1"/>`, sheets: []fxSheet{{name: "S", rows: fmt.Sprintf(
		`<row r="1"><c r="A1" s="%d"><v>0</v></c><c r="B1" s="%d"><v>43890</v></c><c r="C1" s="%d"><v>30</v></c><c r="D1" s="%d"><v>0.5</v></c></row>`, xfDate, xfDate, xfDate, xfTime)}}})
	res := readCells(t, f.zip(t), CellsRequest{Sheet: "sheet:S", Limit: DefaultLimit})
	sameCells(t, "1904", res, map[string]string{
		"A1": `{"date":"1904-01-01"}`, "B1": `{"date":"2024-03-01"}`, "C1": `{"date":"1904-01-31"}`, "D1": `{"time":"12:00:00"}`,
	})
}

func TestFormulaForms(t *testing.T) {
	skipIfUnimplemented(t, probeCells)
	input := formulasBook().zip(t)
	res := readCells(t, input, CellsRequest{Sheet: "sheet:Calc", Limit: DefaultLimit})
	sameCells(t, "cells", res, formulasWant)
	if k := noteKinds(res.Notes); k[model.NoteFormula] != 2 {
		t.Errorf("formula notes (B2 orphan child, E2 empty text): %+v", res.Notes)
	}
	res = readCells(t, input, CellsRequest{Sheet: "sheet:Calc", View: model.ViewValues, Limit: DefaultLimit})
	sameCells(t, "values", res, map[string]string{
		"A1": `3`, "C1": `"ab"`, "D1": `{"error":"#DIV/0!"}`, "A2": `2`, "B2": `1`, "C2": `0`, "D2": `6`, "E2": `7`,
		"A3": `4`, "B3": `2`, "A4": `6`, "B4": `true`,
		// B1 has no cached value: not shown.
	})
	res = readCells(t, input, CellsRequest{Sheet: "sheet:Calc", View: model.ViewFormulas, Limit: DefaultLimit})
	sameCells(t, "formulas", res, map[string]string{
		"A1": `"=B1+C1"`, "B1": `"=XLOOKUP(1,A:A,B:B)"`, "C1": `"=\"a\"&\"b\""`, "D1": `"=1/0"`,
		"A2": `"=B2*2"`, "C2": `"=@A1:A3"`, "D2": `"=SUM(E5#)"`, "A3": `"=B3*2"`, "B3": `"=LET(x,1,x+1)"`,
		"A4": `"=B4*2"`, "B4": `"=TRUE()"`,
	})
	// A shared child read alone still finds its master above the range.
	res = readCells(t, input, CellsRequest{Range: "Calc!A4", Limit: DefaultLimit})
	sameCells(t, "child alone", res, map[string]string{"A4": `{"f":"=B4*2","v":6}`})
}

// TestSpills: dynamic spills, CSE arrays, data tables and unresolvable
// array metadata in the three views, and members whose anchor is outside
// the range read (decision 8, proposed CSE and data-table marks).
func TestSpills(t *testing.T) {
	skipIfUnimplemented(t, probeCells)
	input := spillBook().zip(t)
	for _, tc := range []struct {
		view model.View
		want map[string]string
	}{
		{model.ViewCells, spillWantCells},
		{model.ViewValues, spillWantValues},
		{model.ViewFormulas, spillWantFormulas},
	} {
		res := readCells(t, input, CellsRequest{Sheet: "sheet:Spill", View: tc.view, Limit: DefaultLimit})
		sameCells(t, tc.view.String(), res, tc.want)
	}
	res := readCells(t, input, CellsRequest{Range: "Spill!A2:D3", Limit: DefaultLimit})
	sameCells(t, "sub-range", res, map[string]string{
		"A2": spillWantCells["A2"], "A3": spillWantCells["A3"], "C2": spillWantCells["C2"], "D2": spillWantCells["D2"],
	})
	res = readCells(t, input, CellsRequest{Sheet: "sheet:Spill", Limit: DefaultLimit})
	k := noteKinds(res.Notes)
	if k[model.NoteDataTable] != 1 || k[model.NoteArray] != 1 {
		t.Errorf("notes: data table %d, array %d (%+v)", k[model.NoteDataTable], k[model.NoteArray], res.Notes)
	}
	// Without a metadata part every cm is unresolvable: the dynamic spill
	// reads as an array of unknown kind, never guessed from the function.
	f := spillBook()
	f.metadata = ""
	res = readCells(t, f.zip(t), CellsRequest{Range: "Spill!A1:A2", Limit: DefaultLimit})
	sameCells(t, "no metadata", res, map[string]string{"A1": `{"f":"=SEQUENCE(3)","v":1}`, "A2": `{"v":2,"spill":"Spill!A1"}`})
}

// TestAddressForms: every --range and --sheet form, and what each
// resolves to.
func TestAddressForms(t *testing.T) {
	skipIfUnimplemented(t, probeCells)
	input := addressBook().zip(t)
	for _, tc := range []struct {
		rng, sheet string
		wantSheet  string
		wantRange  string
		keys       string
	}{
		{"", "sheet:Budget", "sheet:Budget", "Budget!A1:B4", "A1 B1 A2 B2 A3 B3 A4 B4"},
		{"sheet:Budget", "", "sheet:Budget", "Budget!A1:B4", "A1 B1 A2 B2 A3 B3 A4 B4"},
		{"Budget!B2", "", "sheet:Budget", "Budget!B2", "B2"},
		{"Budget!A2:B3", "", "sheet:Budget", "Budget!A2:B3", "A2 B2 A3 B3"},
		{"Budget!B3:A2", "", "sheet:Budget", "Budget!A2:B3", "A2 B2 A3 B3"},
		{"Budget!2:3", "", "sheet:Budget", "Budget!2:3", "A2 B2 A3 B3"},
		{"Budget!B:B", "", "sheet:Budget", "Budget!B:B", "B1 B2 B3 B4"},
		{"budget!B2", "", "sheet:Budget", "Budget!B2", "B2"},
		{"'Budget'!B2", "", "sheet:Budget", "Budget!B2", "B2"},
		{"Budget!B2", "sheet:BUDGET", "sheet:Budget", "Budget!B2", "B2"},
		{"'Q1 Budget'!C4", "", "sheet:Q1 Budget", "'Q1 Budget'!C4", "C4"},
		{"'Bob''s'!A1:A2", "", "sheet:Bob's", "'Bob''s'!A1:A2", "A1 A2"},
		{"'2024'!B1", "", "sheet:2024", "'2024'!B1", "B1"},
		{"name:Rate", "", "sheet:Budget", "Budget!B2", "B2"},
		{"name:rate", "", "sheet:Budget", "Budget!B2", "B2"},
		{"name:Budget!Local", "", "sheet:Budget", "Budget!B2:B3", "B2 B3"},
		{"name:Quoted", "", "sheet:Q1 Budget", "'Q1 Budget'!A:A", "A1 A2 A3 A4"},
		{"name:Rows", "", "sheet:Budget", "Budget!2:3", "A2 B2 A3 B3"},
		{"table:Sales", "", "sheet:Budget", "Budget!A1:B4", "A1 B1 A2 B2 A3 B3 A4 B4"},
		{"table:sales/column:Amount", "", "sheet:Budget", "Budget!B2:B3", "B2 B3"},
	} {
		res := readCells(t, input, CellsRequest{Range: tc.rng, Sheet: tc.sheet, Limit: DefaultLimit})
		_, keys := cellsJSON(t, res)
		if res.Sheet != tc.wantSheet || res.Range != tc.wantRange || strings.Join(keys, " ") != tc.keys {
			t.Errorf("--range %q --sheet %q: sheet %q range %q keys %v; want %q %q %s", tc.rng, tc.sheet, res.Sheet, res.Range, keys, tc.wantSheet, tc.wantRange, tc.keys)
		}
	}
}

// TestAddressRefusals: the errors of resolve, with their codes and
// paths.
func TestAddressRefusals(t *testing.T) {
	skipIfUnimplemented(t, probeCells)
	input := addressBook().zip(t)
	for _, tc := range []struct {
		rng, sheet string
		code, path string
	}{
		{"A1", "", cli.CodeInvalidOperation, "--range"},
		{"Budget!$A$1", "", cli.CodeInvalidOperation, "--range"},
		{"Budget!A$1", "", cli.CodeInvalidOperation, "--range"},
		{"Budget!R1C1", "", cli.CodeInvalidOperation, "--range"},
		{"Budget!A1:C", "", cli.CodeInvalidOperation, "--range"},
		{"Budget!XFE1", "", cli.CodeInvalidOperation, "--range"},
		{"Budget!A1048577", "", cli.CodeInvalidOperation, "--range"},
		{"Q1 Budget!A1", "", cli.CodeInvalidOperation, "--range"},
		{"'Q1 Budget!A1", "", cli.CodeInvalidOperation, "--range"},
		{"'Bob's'!A1", "", cli.CodeInvalidOperation, "--range"},
		{"[1]Budget!A1", "", cli.CodeInvalidOperation, "--range"},
		{"Budget:Q1!A1", "", cli.CodeInvalidOperation, "--range"},
		{" Budget!A1", "", cli.CodeInvalidOperation, "--range"},
		{"Nope!A1", "", cli.CodeNotFound, "--range"},
		{"name:Const", "", cli.CodeInvalidOperation, "--range"},
		{"name:Union", "", cli.CodeInvalidOperation, "--range"},
		{"name:Ref", "", cli.CodeInvalidOperation, "--range"},
		{"name:Local", "", cli.CodeNotFound, "--range"},
		{"name:Missing", "", cli.CodeNotFound, "--range"},
		{"name:", "", cli.CodeInvalidOperation, "--range"},
		{"table:Nope", "", cli.CodeNotFound, "--range"},
		{"table:Sales/column:Nope", "", cli.CodeNotFound, "--range"},
		{"table:", "", cli.CodeInvalidOperation, "--range"},
		{"", "Budget", cli.CodeInvalidOperation, "--sheet"},
		{"", "sheet:", cli.CodeInvalidOperation, "--sheet"},
		{"", "sheet:Nope", cli.CodeNotFound, "--sheet"},
		{"Budget!A1", "sheet:Q1 Budget", cli.CodeInvalidOperation, "--range"},
		{"", "sheet:Gone", cli.CodeUnreadableTarget, "--sheet"},
		{"Broken!A1", "", cli.CodeUnreadableTarget, "--range"},
	} {
		_, err := Cells(input, CellsRequest{Range: tc.rng, Sheet: tc.sheet, Limit: DefaultLimit}, Options{})
		if code, path := errCode(err); code != tc.code || path != tc.path {
			t.Errorf("--range %q --sheet %q: %q at %q (%v), want %q at %q", tc.rng, tc.sheet, code, path, err, tc.code, tc.path)
		}
	}
}

// TestChartsheetRead: reading a chartsheet's cells is a note, not an
// error (brief).
func TestChartsheetRead(t *testing.T) {
	skipIfUnimplemented(t, probeCells)
	res := readCells(t, addressBook().zip(t), CellsRequest{Sheet: "sheet:Chart1", Limit: DefaultLimit})
	if res.Cells.Len() != 0 || noteKinds(res.Notes)[model.NoteSheetKind] != 1 || res.Range != "" {
		t.Errorf("chartsheet: %d cells, range %q, notes %+v", res.Cells.Len(), res.Range, res.Notes)
	}
}

// TestHiddenAndMerges: decision 5's hidden list (rows then columns,
// clipped to the range; zero height is not hidden) and merges that
// intersect the range (unclipped).
func TestHiddenAndMerges(t *testing.T) {
	skipIfUnimplemented(t, probeCells)
	var rows strings.Builder
	for r := 1; r <= 10; r++ {
		attr := ""
		switch {
		case r >= 5 && r <= 7:
			attr = ` hidden="1"`
		case r == 9:
			attr = ` ht="0" customHeight="1"`
		}
		fmt.Fprintf(&rows, `<row r="%d"%s><c r="A%d"><v>%d</v></c><c r="D%d"><v>%d</v></c></row>`, r, attr, r, r, r, r)
	}
	f := fx{sheets: []fxSheet{{name: "Budget",
		head: `<cols><col min="3" max="3" width="0" hidden="1"/><col min="5" max="6" hidden="1"/></cols>`,
		rows: rows.String(),
		tail: `<mergeCells count="2"><mergeCell ref="A1:B1"/><mergeCell ref="D10:E11"/></mergeCells>`}}}
	input := f.zip(t)
	for _, tc := range []struct {
		rng            string
		hidden, merges []string
	}{
		{"Budget!A1:F10", []string{"Budget!5:7", "Budget!C:C", "Budget!E:F"}, []string{"Budget!A1:B1", "Budget!D10:E11"}},
		{"Budget!A6:B20", []string{"Budget!6:7"}, []string{}},
		{"Budget!E11", []string{"Budget!E:E"}, []string{"Budget!D10:E11"}},
		{"Budget!A1", []string{}, []string{"Budget!A1:B1"}},
	} {
		res := readCells(t, input, CellsRequest{Range: tc.rng, Limit: DefaultLimit})
		if !reflect.DeepEqual(res.Hidden, tc.hidden) || !reflect.DeepEqual(res.Merges, tc.merges) {
			t.Errorf("%s: hidden %v merges %v; want %v %v", tc.rng, res.Hidden, res.Merges, tc.hidden, tc.merges)
		}
	}
	res := readCells(t, input, CellsRequest{Sheet: "sheet:Budget", Limit: DefaultLimit})
	if b := encode(t, res); !bytes.Contains(b, []byte(`"merges":[`)) || !bytes.Contains(b, []byte(`"hidden":[`)) {
		t.Errorf("encoding lacks merges or hidden: %s", b)
	}
}

// TestLimitTruncation: decision 6. Rows are whole; next is the rest of
// the range; 0 is no limit; a first row larger than the limit is read
// whole.
func TestLimitTruncation(t *testing.T) {
	skipIfUnimplemented(t, probeCells)
	input := bigBook(10, 3).zip(t)
	for _, tc := range []struct {
		rng, sheet string
		limit      int
		cells      int
		next       string
	}{
		{"", "sheet:Big", 0, 30, ""},
		{"", "sheet:Big", 30, 30, ""},
		{"", "sheet:Big", 7, 6, "Big!A3:C10"},
		{"", "sheet:Big", 6, 6, "Big!A3:C10"},
		{"", "sheet:Big", 2, 3, "Big!A2:C10"},
		{"Big!A1:C10", "", 7, 6, "Big!A3:C10"},
		{"Big!B2:C20", "", 5, 4, "Big!B4:C10"},
		{"Big!1:10", "", 7, 6, "Big!3:10"},
		{"Big!A:B", "", 3, 2, "Big!A2:B10"},
		{"Big!A1:C10", "", 29, 27, "Big!A10:C10"},
	} {
		res := readCells(t, input, CellsRequest{Range: tc.rng, Sheet: tc.sheet, Limit: tc.limit})
		next := ""
		if res.Truncated != nil {
			next = res.Truncated.Next
		}
		if res.Cells.Len() != tc.cells || next != tc.next {
			t.Errorf("%q %q limit %d: %d cells, next %q; want %d, %q", tc.rng, tc.sheet, tc.limit, res.Cells.Len(), next, tc.cells, tc.next)
		}
		hasNote := noteKinds(res.Notes)[model.NoteTruncated] == 1
		if hasNote != (tc.next != "") {
			t.Errorf("%q limit %d: truncated note %v", tc.rng, tc.limit, hasNote)
		}
		// Reading next continues where the cut stopped.
		if tc.next != "" {
			rest := readCells(t, input, CellsRequest{Range: tc.next, Limit: 0})
			got, _ := cellsJSON(t, rest)
			first, _ := cellsJSON(t, res)
			for k := range first {
				if _, dup := got[k]; dup {
					t.Errorf("%q: %s read twice", tc.rng, k)
				}
			}
		}
	}
}

// TestDamagedParts: contract §10. A broken styles, shared strings or
// metadata part disables only its facet, with a note.
func TestDamagedParts(t *testing.T) {
	skipIfUnimplemented(t, probeCells)
	styles := valuesBook()
	styles.stylesRaw = `<styleSheet`
	res := readCells(t, styles.zip(t), CellsRequest{Range: "Values!A4:C4", Limit: DefaultLimit})
	sameCells(t, "no styles", res, map[string]string{"A4": `45352`, "B4": `45352.5625`, "C4": `0.5625`})
	if noteKinds(res.Notes)[model.NotePart] != 1 {
		t.Errorf("styles note: %+v", res.Notes)
	}
	sst := valuesBook()
	sst.sstRaw = `<sst xmlns="` + nsMainT + `"><si><t>x</si></sst>`
	res = readCells(t, sst.zip(t), CellsRequest{Range: "Values!A1:A2", Limit: DefaultLimit})
	sameCells(t, "no sst", res, map[string]string{"A2": `1200`})
	if noteKinds(res.Notes)[model.NotePart] != 1 {
		t.Errorf("sst note: %+v", res.Notes)
	}
	meta := spillBook()
	meta.metadata = `<metadata`
	res = readCells(t, meta.zip(t), CellsRequest{Range: "Spill!A1:A2", Limit: DefaultLimit})
	sameCells(t, "no metadata", res, map[string]string{"A1": `{"f":"=SEQUENCE(3)","v":1}`, "A2": `{"v":2,"spill":"Spill!A1"}`})
	if noteKinds(res.Notes)[model.NotePart] != 1 {
		t.Errorf("metadata note: %+v", res.Notes)
	}
}

// TestOrderAndGridNotes: rows and cells out of order or off the grid are
// skipped with notes, and the JSON never has a duplicate key.
func TestOrderAndGridNotes(t *testing.T) {
	skipIfUnimplemented(t, probeCells)
	f := fx{sheets: []fxSheet{{name: "S", rows: `<row r="2"><c r="A2"><v>1</v></c><c r="A2"><v>2</v></c></row><row r="1"><c r="A1"><v>3</v></c></row><row r="3"><c r="ZZZZ3"><v>4</v></c></row>`}}}
	res := readCells(t, f.zip(t), CellsRequest{Sheet: "sheet:S", Limit: DefaultLimit})
	sameCells(t, "order", res, map[string]string{"A2": `1`})
	k := noteKinds(res.Notes)
	if k[model.NoteOrder] != 2 || k[model.NoteGrid] != 1 {
		t.Errorf("notes %+v", res.Notes)
	}
}

// TestValuesStaleOnCells: values_stale is on cell reads too.
func TestValuesStaleOnCells(t *testing.T) {
	skipIfUnimplemented(t, probeCells)
	f := bigBook(1, 1)
	f.calcPr = `<calcPr fullCalcOnLoad="true"/>`
	if res := readCells(t, f.zip(t), CellsRequest{Sheet: "sheet:Big", Limit: DefaultLimit}); !res.ValuesStale {
		t.Error("values_stale not set")
	}
}

// TestGroup: --group lists shared-formula groups that fill a rectangle
// once (proposed shape).
func TestGroup(t *testing.T) {
	skipIfUnimplemented(t, func() { probeCells(); probeGroup() })
	var rows strings.Builder
	for r := 1; r <= 5; r++ {
		fmt.Fprintf(&rows, `<row r="%d"><c r="A%d"><v>%d</v></c>`, r, r, r)
		if r == 1 {
			rows.WriteString(`<c r="B1"><f t="shared" ref="B1:B5" si="0">A1*2</f><v>2</v></c>`)
		} else {
			fmt.Fprintf(&rows, `<c r="B%d"><f t="shared" si="0"/><v>%d</v></c>`, r, 2*r)
		}
		if r == 2 || r == 4 {
			fmt.Fprintf(&rows, `<c r="C%d"><f t="shared" ref="C2:C4" si="1">A%d+1</f><v>0</v></c>`, r, r)
		}
		rows.WriteString(`</row>`)
	}
	input := fx{sheets: []fxSheet{{name: "G", rows: rows.String()}}}.zip(t)
	res := readCells(t, input, CellsRequest{Sheet: "sheet:G", View: model.ViewFormulas, Group: true, Limit: DefaultLimit})
	if want := []Group{{At: "G!B1:B5", F: "=A1*2"}}; !reflect.DeepEqual(res.Groups, want) {
		t.Errorf("groups %+v", res.Groups)
	}
	// C2 and C4 do not fill C2:C4 (C3 is absent): not grouped.
	sameCells(t, "group", res, map[string]string{"C2": `"=A2+1"`, "C4": `"=A4+1"`})
	res = readCells(t, input, CellsRequest{Range: "G!B3:B4", View: model.ViewFormulas, Group: true, Limit: DefaultLimit})
	if want := []Group{{At: "G!B3:B4", F: "=A3*2"}}; !reflect.DeepEqual(res.Groups, want) {
		t.Errorf("clipped groups %+v", res.Groups)
	}
}
