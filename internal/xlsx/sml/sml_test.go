package sml

import (
	"archive/zip"
	"bytes"
	"reflect"
	"testing"

	"github.com/jhnoel/documentdriver/internal/ooxml/ctypes"
	"github.com/jhnoel/documentdriver/internal/ooxml/tree"
	"github.com/jhnoel/documentdriver/internal/opc"
	"github.com/jhnoel/documentdriver/internal/xlsx/formula"
)

func parse(t *testing.T, s string) *tree.Node {
	t.Helper()
	doc, err := tree.ParseString(s)
	if err != nil {
		t.Fatal(err)
	}
	return doc.Root()
}

func TestSmallGrammars(t *testing.T) {
	for _, tc := range []struct {
		s  string
		n  int
		ok bool
	}{{"0", 0, true}, {"42", 42, true}, {"999999999", 999999999, true}, {"1000000000", 0, false}, {"", 0, false}, {"-1", 0, false}, {"+1", 0, false}, {" 1", 0, false}, {"1a", 0, false}} {
		if n, ok := ParseIndex(tc.s); n != tc.n || ok != tc.ok {
			t.Errorf("ParseIndex(%q) = %d, %v", tc.s, n, ok)
		}
	}
	for s, want := range map[string]bool{"1": true, "true": true, "0": false, "false": false, "TRUE": false, "": false} {
		if IsTrue(s) != want {
			t.Errorf("IsTrue(%q) = %v", s, !want)
		}
	}
	if !IsMain(NSMain) || !IsMain(NSStrict) || IsMain(NSRel) || !IsRel(NSRel) || !IsRel(NSRelStrict) || IsRel(NSMain) {
		t.Error("namespace predicates")
	}
	root := parse(t, `<styleSheet xmlns="`+NSMain+`" xmlns:o="urn:o"><o:numFmts/><numFmts count="1"/><fonts/></styleSheet>`)
	if c := MainChild(root, "numFmts"); c == nil || c.Prefix() != "" {
		t.Errorf("MainChild skipped the other namespace wrongly: %v", c)
	}
	if MainChild(root, "fills") != nil {
		t.Error("MainChild found a missing child")
	}
}

func TestUnescapeX(t *testing.T) {
	for in, want := range map[string]string{
		"plain":                  "plain",
		"_x000D_":                "\r",
		"a_x0009_b":              "a\tb",
		"_x005F_x0041_":          "_x0041_",
		"_x005f_":                "_",
		"_x00e9_":                "é",
		"_xD83D__xDE00_":         "\U0001F600",
		"_xD83D_":                "�",
		"_xDE00_x":               "�x",
		"_x12_":                  "_x12_",
		"_xGGGG_":                "_xGGGG_",
		"__x0041_":               "_A",
		"_x0041":                 "_x0041",
		"x_x0041__x0042_y_x0043": "xABy_x0043",
	} {
		if got := UnescapeX(in); got != want {
			t.Errorf("UnescapeX(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestEscapeX(t *testing.T) {
	for in, want := range map[string]string{
		"plain":   "plain",
		"a\tb\nc": "a\tb\nc",
		"\r":      "_x000D_",
		"\x01":    "_x0001_",
		"￾":       "_xFFFE_",
		"_x0041_": "_x005F_x0041_",
		"_x00G1_": "_x00G1_",
		"_x41_":   "_x41_",
	} {
		if got := EscapeX(in); got != want {
			t.Errorf("EscapeX(%q) = %q, want %q", in, got, want)
		}
	}
}

// FuzzEscapeX: UnescapeX undoes EscapeX for any string.
func FuzzEscapeX(f *testing.F) {
	for _, s := range []string{"", "_x0041_", "\r\n", "\x00", "_x005F_", "￿", "\xff_x"} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, s string) {
		if got := UnescapeX(EscapeX(s)); got != s {
			t.Fatalf("UnescapeX(EscapeX(%q)) = %q", s, got)
		}
	})
}

func TestSheetEntries(t *testing.T) {
	root := parse(t, `<workbook xmlns="`+NSMain+`" xmlns:r="`+NSRel+`" xmlns:s="`+NSRelStrict+`"><sheets>`+
		`<sheet name="A" sheetId="1" r:id="rId1"/>`+
		`<sheet name="B" sheetId="x" state="hidden" s:id="rId2"/>`+
		`<other name="C"/>`+
		`<sheet name="D"/>`+
		`</sheets></workbook>`)
	got := SheetEntries(MainChild(root, "sheets"))
	want := []SheetEntry{
		{Pos: 0, Name: "A", State: "visible", SheetID: 1, RID: "rId1"},
		{Pos: 1, Name: "B", State: "hidden", SheetID: 0, RID: "rId2"},
		{Pos: 2, Name: "D", State: "visible"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("SheetEntries = %+v, want %+v", got, want)
	}
	if SheetEntries(nil) != nil {
		t.Error("nil sheets")
	}
}

// book builds a package whose workbook relationships are rels (Id,
// Type, Target, TargetMode) and whose parts are parts.
func book(t *testing.T, rels [][4]string, parts ...string) *opc.Package {
	t.Helper()
	var b bytes.Buffer
	zw := zip.NewWriter(&b)
	put := func(name, body string) {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write([]byte(body)); err != nil {
			t.Fatal(err)
		}
	}
	put("[Content_Types].xml", `<Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types">`+
		`<Default Extension="rels" ContentType="`+ctypes.CTPackageRelationships+`"/><Default Extension="xml" ContentType="application/xml"/></Types>`)
	put("_rels/.rels", `<Relationships xmlns="`+NSPackageRel+`"><Relationship Id="rId1" Type="`+ctypes.RelOfficeDocument+`" Target="xl/workbook.xml"/></Relationships>`)
	put("xl/workbook.xml", `<workbook xmlns="`+NSMain+`"/>`)
	rx := `<Relationships xmlns="` + NSPackageRel + `">`
	for _, r := range rels {
		rx += `<Relationship Id="` + r[0] + `" Type="` + r[1] + `" Target="` + r[2] + `"`
		if r[3] != "" {
			rx += ` TargetMode="` + r[3] + `"`
		}
		rx += `/>`
	}
	put("xl/_rels/workbook.xml.rels", rx+`</Relationships>`)
	for _, p := range parts {
		put(p, `<x/>`)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	pkg, err := opc.OpenBytes(b.Bytes(), opc.DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	return pkg
}

func TestResolveSheet(t *testing.T) {
	pkg := book(t, [][4]string{
		{"rId1", ctypes.RelWorksheet, "worksheets/sheet1.xml", ""},
		{"rId2", ctypes.RelChartsheet, "chartsheets/sheet1.xml", ""},
		{"rId3", ctypes.RelDialogsheet, "dialogsheets/d.xml", ""},
		{"rId4", ctypes.RelXlMacrosheet, "macrosheets/m.xml", ""},
		{"rId5", ctypes.RelStyles, "styles.xml", ""},
		{"rId6", ctypes.RelWorksheet, "http://example.com/x.xml", "External"},
		{"rId7", ctypes.RelWorksheet, "worksheets/gone.xml", ""},
		{"rId1", ctypes.RelChartsheet, "chartsheets/sheet1.xml", ""}, // a repeated id: the first wins
	}, "xl/worksheets/sheet1.xml", "xl/chartsheets/sheet1.xml", "xl/dialogsheets/d.xml", "xl/macrosheets/m.xml", "xl/styles.xml")
	rels, err := pkg.Rels("/xl/workbook.xml")
	if err != nil {
		t.Fatal(err)
	}
	byID := RelsByID(rels)
	if len(byID) != 7 || byID["rId1"].Type != ctypes.RelWorksheet {
		t.Fatalf("RelsByID = %v", byID)
	}
	for _, tc := range []struct {
		rid     string
		kind    SheetKind
		hasPart bool
		problem string
	}{
		{"rId1", KindWorksheet, true, ""},
		{"rId2", KindChartsheet, true, ""},
		{"rId3", KindDialogsheet, true, ""},
		{"rId4", KindMacrosheet, true, ""},
		{"rId5", KindOther, true, "the sheet's relationship is not a worksheet"},
		{"rId6", KindMissing, false, "the sheet's relationship is external"},
		{"rId7", KindMissing, false, "the sheet's part is missing"},
		{"rId9", KindMissing, false, "the sheet has no relationship"},
		{"", KindMissing, false, "the sheet has no relationship"},
	} {
		part, kind, problem := ResolveSheet(SheetEntry{RID: tc.rid}, rels, byID, pkg)
		if kind != tc.kind || problem != tc.problem || (part != "") != tc.hasPart || tc.hasPart && !pkg.Has(part) {
			t.Errorf("ResolveSheet(%s) = %q, %d, %q", tc.rid, part, kind, problem)
		}
	}
	if len(RelsByID(nil)) != 0 {
		t.Error("RelsByID(nil)")
	}
	if _, kind, _ := ResolveSheet(SheetEntry{RID: "rId1"}, nil, byID, pkg); kind != KindMissing {
		t.Error("nil rels")
	}
}

func TestDefinedNames(t *testing.T) {
	root := parse(t, `<workbook xmlns="`+NSMain+`"><definedNames>`+
		`<definedName name="Tax">Sheet1!$A$1</definedName>`+
		`<definedName name="L" localSheetId="1" hidden="1">B!A1</definedName>`+
		`<definedName name="Bad" localSheetId="2">1</definedName>`+
		`<definedName name="Neg" localSheetId="-1" hidden="0">2</definedName>`+
		`<x name="no"/>`+
		`</definedNames></workbook>`)
	got := DefinedNames(MainChild(root, "definedNames"), 2)
	want := []DefinedName{
		{Name: "Tax", Text: "Sheet1!$A$1", Local: -1},
		{Name: "L", Text: "B!A1", Local: 1, Hidden: true},
		{Name: "Bad", Text: "1", Local: -2},
		{Name: "Neg", Text: "2", Local: -2},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("DefinedNames = %+v, want %+v", got, want)
	}
	if DefinedNames(nil, 1) != nil {
		t.Error("nil definedNames")
	}
}

func TestParseTable(t *testing.T) {
	tbl, err := ParseTable(parse(t, `<table xmlns="`+NSMain+`" name="T1" displayName="Sales" ref=" C5:A1 " headerRowCount="2" totalsRowCount="1">`+
		`<tableColumns count="2"><tableColumn id="1" name="Q"/><tableColumn id="2"/><other name="x"/><tableColumn id="3" name="R"/></tableColumns></table>`))
	if err != nil {
		t.Fatal(err)
	}
	want := Table{
		Name:       "Sales",
		Ref:        formula.Range{Kind: formula.Cells, From: formula.Cell{Row: 1, Col: 1}, To: formula.Cell{Row: 5, Col: 3}},
		HeaderRows: 2, TotalsRows: 1, Columns: []string{"Q", "R"},
	}
	if !reflect.DeepEqual(tbl, want) {
		t.Errorf("ParseTable = %+v, want %+v", tbl, want)
	}
	tbl, err = ParseTable(parse(t, `<table xmlns="`+NSStrict+`" name="T1" ref="$B$2" headerRowCount="-1"/>`))
	if err != nil || tbl.Name != "T1" || !tbl.Ref.Single || tbl.HeaderRows != 1 || tbl.Columns == nil {
		t.Errorf("fallbacks: %+v, %v", tbl, err)
	}
	for _, bad := range []string{
		`<table ref="A1"/>`,
		`<x xmlns="` + NSMain + `" ref="A1"/>`,
		`<table xmlns="` + NSMain + `" ref="A:A"/>`,
		`<table xmlns="` + NSMain + `"/>`,
	} {
		if _, err := ParseTable(parse(t, bad)); err == nil {
			t.Errorf("ParseTable(%s) succeeded", bad)
		}
	}
	if _, err := ParseTable(nil); err == nil {
		t.Error("nil root")
	}
}
