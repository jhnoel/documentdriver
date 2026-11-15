package read

import (
	"bytes"
	"reflect"
	"testing"

	"github.com/jhnoel/documentdriver/internal/cli"
	"github.com/jhnoel/documentdriver/internal/xlsx/model"
)

func probeWorkbook() { _, _ = Workbook(nil, Options{}) }

func TestWorkbookSummary(t *testing.T) {
	skipIfUnimplemented(t, probeWorkbook)
	f := addressBook()
	f.sheets[0].head = `<dimension ref="A1:B4"/>`
	f.sheets[1].head = `<dimension ref="A1:C4"/>`
	f.sheets[2].head = `<dimension ref="nonsense"/>`
	f.sheets[1].state = "hidden"
	f.sheets[2].state = "veryHidden"
	f.calcPr = `<calcPr calcId="191029" fullCalcOnLoad="1"/>`
	input := f.zip(t)
	res, err := Workbook(input, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if res.Base != cli.Hash(input) {
		t.Errorf("base %q", res.Base)
	}
	if !res.ValuesStale {
		t.Error("values_stale not set with fullCalcOnLoad")
	}
	wantSheets := []SheetEntry{
		{ID: "sheet:Budget", Name: "Budget", State: "visible", Dimension: "Budget!A1:B4", Tables: []string{"Sales"}},
		{ID: "sheet:Q1 Budget", Name: "Q1 Budget", State: "hidden", Dimension: "'Q1 Budget'!A1:C4", Tables: []string{}},
		{ID: "sheet:Bob's", Name: "Bob's", State: "very_hidden", Tables: []string{}},
		{ID: "sheet:2024", Name: "2024", State: "visible", Tables: []string{}},
		{ID: "sheet:Chart1", Name: "Chart1", State: "visible", Tables: []string{}},
		{ID: "sheet:Gone", Name: "Gone", State: "visible", Tables: []string{}},
		{ID: "sheet:Broken", Name: "Broken", State: "visible", Tables: []string{}},
	}
	if !reflect.DeepEqual(res.Sheets, wantSheets) {
		t.Errorf("sheets\n got %+v\nwant %+v", res.Sheets, wantSheets)
	}
	wantNames := []NameEntry{
		{Name: "Const", Scope: "workbook", RefersTo: "=0.5", Hidden: true},
		{Name: "Local", Scope: "sheet:Budget", RefersTo: "=Budget!$B$2:$B$3"},
		{Name: "Rate", Scope: "workbook", RefersTo: "=Budget!$B$2"},
		{Name: "Union", Scope: "workbook", RefersTo: "=Budget!$A$1,Budget!$B$2"},
		{Name: "Quoted", Scope: "workbook", RefersTo: "='Q1 Budget'!$A:$A"},
		{Name: "Rows", Scope: "workbook", RefersTo: "=Budget!$2:$3"},
		{Name: "Ref", Scope: "workbook", RefersTo: "=Budget!#REF!"},
		{Name: "_xlnm.Print_Area", Scope: "sheet:Budget", RefersTo: "=Budget!$A$1:$B$4"},
		{Name: "Bad", RefersTo: "=Budget!$A$1"},
	}
	if !reflect.DeepEqual(res.Names, wantNames) {
		t.Errorf("names\n got %+v\nwant %+v", res.Names, wantNames)
	}
	wantTables := []TableEntry{{Name: "Sales", Sheet: "sheet:Budget", Ref: "Budget!A1:B4", Columns: []string{"Region", "Amount"}}}
	if !reflect.DeepEqual(res.Tables, wantTables) {
		t.Errorf("tables %+v", res.Tables)
	}
	kinds := noteKinds(res.Notes)
	for k, n := range map[model.NoteKind]int{model.NoteSheetKind: 1, model.NoteSheet: 2, model.NoteName: 1} {
		// NoteSheetKind: Chart1. NoteSheet: Gone (missing part) and Bob's
		// (unparsable dimension); Broken is malformed only after
		// sheetData starts, which Head never reads. NoteName: Bad.
		if kinds[k] != n {
			t.Errorf("%s notes: %d, want %d (%+v)", k, kinds[k], n, res.Notes)
		}
	}
	b := encode(t, res)
	if !bytes.HasPrefix(b, []byte(`{"base":"sha256:`)) || !bytes.Contains(b, []byte(`"notes":[`)) {
		t.Errorf("encoding: %s", b)
	}
}

// TestWorkbookEmptyLists: lists are [] and notes present, never null;
// values_stale is left out when false.
func TestWorkbookEmptyLists(t *testing.T) {
	skipIfUnimplemented(t, probeWorkbook)
	res, err := Workbook(fx{sheets: []fxSheet{{name: "S"}}}.zip(t), Options{})
	if err != nil {
		t.Fatal(err)
	}
	b := encode(t, res)
	for _, want := range []string{`"names":[]`, `"tables":[]`, `"notes":[]`, `"tables":[]}`} {
		if !bytes.Contains(b, []byte(want)) {
			t.Errorf("%s lacks %s", b, want)
		}
	}
	if bytes.Contains(b, []byte("values_stale")) || bytes.Contains(b, []byte("null")) {
		t.Errorf("%s", b)
	}
}

func TestWorkbookDate1904AndStrict(t *testing.T) {
	skipIfUnimplemented(t, probeWorkbook)
	strictNS := `xmlns="http://purl.oclc.org/ooxml/spreadsheetml/main" xmlns:r="http://purl.oclc.org/ooxml/officeDocument/relationships"`
	f := fx{sheets: []fxSheet{{name: "S", raw: xmlDecl + `<worksheet ` + strictNS + `><dimension ref="A1"/><sheetData/></worksheet>`}}}
	f.workbookRaw = xmlDecl + `<workbook ` + strictNS + `><workbookPr date1904="1"/><sheets><sheet name="S" sheetId="1" r:id="rId1"/></sheets></workbook>`
	res, err := Workbook(f.zip(t), Options{})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Sheets) != 1 || res.Sheets[0].Dimension != "S!A1" {
		t.Errorf("strict sheets %+v", res.Sheets)
	}
}

// TestInvalidDocument: the invalid_document cases of doc.go.
func TestInvalidDocument(t *testing.T) {
	skipIfUnimplemented(t, probeAll)
	good := fx{sheets: []fxSheet{{name: "S"}}}
	noRel := good.parts()
	noRel["_rels/.rels"] = xmlDecl + pkgRels + `</Relationships>`
	noPart := good
	noPart.omit = []string{"xl/workbook.xml"}
	binary := good
	binary.workbookType = "application/vnd.ms-excel.sheet.binary.macroEnabled.main"
	malformed := good
	malformed.workbookRaw = `<workbook`
	wrongRoot := good
	wrongRoot.workbookRaw = `<document xmlns="` + nsMainT + `"/>`
	noRelZip, err := zipParts(noRel)
	if err != nil {
		t.Fatal(err)
	}
	for name, input := range map[string][]byte{
		"not a zip":        []byte("not a zip"),
		"encrypted (CFB)":  append([]byte{0xD0, 0xCF, 0x11, 0xE0, 0xA1, 0xB1, 0x1A, 0xE1}, make([]byte, 512)...),
		"no officeDoc rel": noRelZip,
		"no workbook part": noPart.zip(t),
		"binary workbook":  binary.zip(t),
		"malformed":        malformed.zip(t),
		"wrong root":       wrongRoot.zip(t),
	} {
		for _, run := range []func() error{
			func() error { _, err := Workbook(input, Options{}); return err },
			func() error {
				_, err := Cells(input, CellsRequest{Sheet: "sheet:S", Limit: DefaultLimit}, Options{})
				return err
			},
			func() error {
				_, err := Find(input, FindRequest{Query: "x", View: model.ViewValues, Limit: DefaultLimit}, Options{})
				return err
			},
		} {
			if code, _ := errCode(run()); code != cli.CodeInvalidDocument {
				t.Errorf("%s: code %q, want invalid_document", name, code)
			}
		}
	}
}

// TestLimits: a package past a safety limit is invalid_document (opc's
// limits, decision 15), also when the limit trips on a part read lazily
// (a worksheet, the shared strings): contract §10 makes every limit trip
// invalid_document.
func TestLimits(t *testing.T) {
	skipIfUnimplemented(t, probeAll)
	input := bigBook(200, 10).zip(t)
	opt := Options{}
	opt.Limits.MaxPartSize = 1 << 10
	for name, run := range map[string]func() error{
		"cells":   func() error { _, err := Cells(input, CellsRequest{Sheet: "sheet:Big"}, opt); return err },
		"find":    func() error { _, err := Find(input, FindRequest{Query: "1", View: model.ViewValues}, opt); return err },
		"summary": func() error { _, err := Workbook(input, opt); return err },
	} {
		if code, _ := errCode(run()); code != cli.CodeInvalidDocument {
			t.Errorf("%s: code %q, want invalid_document", name, code)
		}
	}
}
