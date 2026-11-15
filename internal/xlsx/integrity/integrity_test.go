package integrity

import (
	"errors"
	"strings"
	"testing"

	"github.com/jhnoel/documentdriver/internal/xlsx/formula"
	"github.com/jhnoel/documentdriver/internal/xlsx/sml"
)

func TestCleanWorkbook(t *testing.T) {
	f := Check(baseParts().zip(t))
	if !f.Empty() {
		t.Fatalf("clean workbook has findings:\n%s%v", f.dump(), f.Instances())
	}
	if len(f.Notes()) == 0 {
		t.Error("no pending note for the formula lexer")
	}
	g := Check(baseParts().withTable("").zip(t))
	if !g.Empty() {
		t.Fatalf("clean workbook with a table has findings:\n%s%v", g.dump(), g.Instances())
	}
}

func TestKinds(t *testing.T) {
	rows := func(r string) parts { return baseParts().set("xl/worksheets/sheet1.xml", sheet(r, "")) }
	tail := func(x string) parts {
		return baseParts().set("xl/worksheets/sheet1.xml", sheet(`<row r="1"><c r="A1"/><c r="B1"/></row><row r="2"><c r="A2"/><c r="B2"/></row>`, x))
	}
	wbNames := func(sheets, names string) parts {
		return baseParts().
			set("xl/workbook.xml", hdrMain+`<workbook `+xmlnsS+`><sheets>`+sheets+`</sheets>`+names+`</workbook>`)
	}
	sh := func(name, id, rid string) string {
		return `<sheet name="Data" sheetId="1" r:id="rId1"/><sheet name="` + name + `" sheetId="` + id + `" r:id="` + rid + `"/>`
	}
	cases := []struct {
		name string
		p    parts
		want Kind
	}{
		{"row order", rows(`<row r="2"><c r="A2"/></row><row r="1"><c r="A1"/></row>`), WorksheetRowOrder},
		{"row repeated", rows(`<row r="1"/><row r="1"/>`), WorksheetRowOrder},
		{"cell order", rows(`<row r="1"><c r="B1"/><c r="A1"/></row>`), WorksheetCellOrder},
		{"cell row", rows(`<row r="1"><c r="A2"/></row>`), WorksheetCellRow},
		{"cell ref", rows(`<row r="1"><c r="1A"/></row>`), WorksheetCellRef},
		{"cell type", rows(`<row r="1"><c r="A1" t="q"><v>1</v></c></row>`), WorksheetCellType},
		{"style range", rows(`<row r="1"><c r="A1" s="2"/></row>`), WorksheetStyle},
		{"row style range", rows(`<row r="1" s="9" customFormat="1"/>`), WorksheetStyle},
		{"sst range", rows(`<row r="1"><c r="A1" t="s"><v>2</v></c></row>`), WorksheetSST},
		{"row limit", rows(`<row r="1048577"/>`), LimitRow},
		{"column limit", rows(`<row r="1"><c r="XFE1"/></row>`), LimitColumn},
		{"cell text limit", rows(`<row r="1"><c r="A1" t="inlineStr"><is><t>` + strings.Repeat("x", 32768) + `</t></is></c></row>`), LimitCellText},
		{"cell text limit v", rows(`<row r="1"><c r="A1" t="str"><v>` + strings.Repeat("x", 32768) + `</v></c></row>`), LimitCellText},
		{"formula limit", rows(`<row r="1"><c r="A1"><f>` + strings.Repeat("1+", 4097) + `1</f></c></row>`), LimitFormula},
		{"shared orphan", rows(`<row r="1"><c r="A1"><f t="shared" si="3"/></c></row>`), WorksheetSharedOrph},
		{"shared two masters", rows(`<row r="1"><c r="A1"><f t="shared" ref="A1:A2" si="0">1+1</f></c><c r="B1"><f t="shared" ref="B1:B2" si="0">1+1</f></c></row>`), WorksheetSharedDup},
		{"array ref", rows(`<row r="1"><c r="C1"><f t="array" ref="A1:B2">1</f></c></row>`), WorksheetArrayRef},
		{"merge overlap", tail(`<mergeCells count="2"><mergeCell ref="A1:B2"/><mergeCell ref="B2:C3"/></mergeCells>`), WorksheetMergeOver},
		{"merge ref", tail(`<mergeCells count="1"><mergeCell ref="A1:Q"/></mergeCells>`), WorksheetMergeRef},
		{"empty mergeCells", tail(`<mergeCells count="0"/>`), WorksheetEmpty},
		{"empty cols", baseParts().set("xl/worksheets/sheet1.xml", sheet(`<row r="1"/>`, ``)).set("xl/worksheets/sheet1.xml", hdrMain+`<worksheet `+xmlnsS+`><cols/><sheetData/></worksheet>`), WorksheetEmpty},
		{"cols overlap", baseParts().set("xl/worksheets/sheet1.xml", hdrMain+`<worksheet `+xmlnsS+`><cols><col min="1" max="3"/><col min="2" max="4"/></cols><sheetData/></worksheet>`), WorksheetCols},
		{"head order", baseParts().set("xl/worksheets/sheet1.xml", hdrMain+`<worksheet `+xmlnsS+`><sheetData/><cols><col min="1" max="1"/></cols></worksheet>`), WorksheetChildOrder},
		{"child count", baseParts().set("xl/worksheets/sheet1.xml", hdrMain+`<worksheet `+xmlnsS+`><sheetData/><sheetData/></worksheet>`), WorksheetChildCount},
		{"dimension", baseParts().set("xl/worksheets/sheet1.xml", hdrMain+`<worksheet `+xmlnsS+`><dimension ref="A1:"/><sheetData/></worksheet>`), WorksheetDimension},
		{"root", baseParts().set("xl/worksheets/sheet1.xml", hdrMain+`<other `+xmlnsS+`/>`), WorksheetRoot},
		{"malformed sheet", baseParts().set("xl/worksheets/sheet1.xml", `<worksheet><sheetData></worksheet>`), XMLMalformed},
		{"table header", baseParts().withTable("").set("xl/sharedStrings.xml", hdrMain+`<sst `+xmlnsS+` count="2" uniqueCount="2"><si><t>Name</t></si><si><t>Other</t></si></sst>`), TableHeaderMismatch},
		{"table header cell missing", baseParts().withTable("").set("xl/worksheets/sheet1.xml", strings.Replace(baseParts().withTable("")["xl/worksheets/sheet1.xml"], `<c r="B1" t="s"><v>1</v></c>`, "", 1)), TableHeaderMissing},
		{"table headerRowCount", baseParts().withTable(`headerRowCount="2"`), TableHeaderRows},
		{"table columns", baseParts().withTable("").set("xl/tables/table1.xml", strings.Replace(baseParts().withTable("")["xl/tables/table1.xml"], `ref="A1:B2"`, `ref="A1:C2"`, 1)), TableColumnCount},
		{"sheet name dup", wbNames(sh("data", "2", "rId1"), ""), SheetNameDuplicate},
		{"sheet name char", wbNames(sh("a:b", "2", "rId1"), ""), SheetNameInvalid},
		{"sheet name long", wbNames(sh(strings.Repeat("n", 32), "2", "rId1"), ""), LimitSheetName},
		{"sheet name history", wbNames(sh("History", "2", "rId1"), ""), SheetNameHistory},
		{"sheet name apostrophe", wbNames(sh("'x", "2", "rId1"), ""), SheetNameApostrophe},
		{"sheet id dup", wbNames(sh("Two", "1", "rId1"), ""), SheetIDDuplicate},
		{"sheet rid dup", wbNames(sh("Two", "2", "rId1"), ""), SheetRIDDuplicate},
		{"sheet rid unresolved", wbNames(sh("Two", "2", "rId9"), ""), SheetRIDUnresolved},
		{"no visible", wbNames(`<sheet name="Data" sheetId="1" state="hidden" r:id="rId1"/>`, ""), WorkbookNoVisible},
		{"no sheets", wbNames(``, ""), WorkbookNoSheets},
		{"name dup", wbNames(sh("Two", "2", "rId1"), `<definedNames><definedName name="Foo">Data!$A$1</definedName><definedName name="FOO">Data!$A$2</definedName></definedNames>`), NameDuplicate},
		{"name ref shaped", wbNames(sh("Two", "2", "rId1"), `<definedNames><definedName name="TAX2023">Data!$A$1</definedName></definedNames>`), NameRefShaped},
		{"name r1c1", wbNames(sh("Two", "2", "rId1"), `<definedNames><definedName name="R1C1">Data!$A$1</definedName></definedNames>`), NameRefShaped},
		{"name invalid", wbNames(sh("Two", "2", "rId1"), `<definedNames><definedName name="a b">Data!$A$1</definedName></definedNames>`), NameInvalid},
		{"name local sheet", wbNames(sh("Two", "2", "rId1"), `<definedNames><definedName name="a" localSheetId="7">Data!$A$1</definedName></definedNames>`), NameLocalSheet},
		{"names empty", wbNames(sh("Two", "2", "rId1"), `<definedNames/>`), NameEmptyContainer},
		{"styles range", baseParts().set("xl/styles.xml", strings.Replace(baseParts()["xl/styles.xml"], `fontId="0" fillId="0" borderId="0" xfId="0"/></cellXfs>`, `fontId="5" fillId="0" borderId="0" xfId="0"/></cellXfs>`, 1)), StylesRange},
		{"styles numFmt", baseParts().set("xl/styles.xml", strings.Replace(baseParts()["xl/styles.xml"], `numFmtId="14"`, `numFmtId="170"`, 1)), StylesRange},
		{"styles count", baseParts().set("xl/styles.xml", strings.Replace(baseParts()["xl/styles.xml"], `<cellXfs count="2">`, `<cellXfs count="7">`, 1)), StylesCount},
		{"sst unique", baseParts().set("xl/sharedStrings.xml", strings.Replace(baseParts()["xl/sharedStrings.xml"], `uniqueCount="2"`, `uniqueCount="9"`, 1)), SSTUniqueCount},
		{"sst item limit", baseParts().set("xl/sharedStrings.xml", hdrMain+`<sst `+xmlnsS+` uniqueCount="1"><si><t>`+strings.Repeat("y", 32768)+`</t></si></sst>`), LimitSSTItemLen},
		{"missing part", baseParts().set("xl/sharedStrings.xml", ""), PackagePartMissing},
		{"no content type", baseParts().set("[Content_Types].xml", strings.Replace(strings.Replace(baseParts()["[Content_Types].xml"], `<Override PartName="/xl/styles.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.styles+xml"/>`, "", 1), `<Default Extension="xml" ContentType="application/xml"/>`, "", 1)), PackageNoType},
		{"styles type default", baseParts().set("[Content_Types].xml", strings.Replace(baseParts()["[Content_Types].xml"], `<Override PartName="/xl/styles.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.styles+xml"/>`, "", 1)), PackageTypeMismatch},
		{"content type mismatch", baseParts().set("[Content_Types].xml", strings.Replace(baseParts()["[Content_Types].xml"], "spreadsheetml.styles+xml", "spreadsheetml.sharedStrings+xml", 1)), PackageTypeMismatch},
		{"calcChain without rel", baseParts().set("xl/calcChain.xml", hdrMain+`<calcChain `+xmlnsS+`/>`), PackageCalcChain},
		{"no workbook", baseParts().set("_rels/.rels", hdrMain+`<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"/>`), PackageNoWorkbook},
		{"workbook root", baseParts().set("xl/workbook.xml", hdrMain+`<styleSheet `+xmlnsS+`/>`), WorkbookRoot},
		{"workbook malformed", baseParts().set("xl/workbook.xml", `<workbook>`), XMLMalformed},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f := Check(c.p.zip(t))
			if !f.HasKind(c.want) {
				t.Errorf("want %s, got:\n%s%v", c.want, f.dump(), f.Instances())
			}
		})
	}
}

// TestOnlyWhatWasBroken: a case's fault does not drag in unrelated kinds.
func TestOnlyWhatWasBroken(t *testing.T) {
	f := Check(baseParts().set("xl/worksheets/sheet1.xml", sheet(`<row r="2"/><row r="1"/>`, "")).zip(t))
	if got := f.dump(); got != "worksheet.row-order in /xl/worksheets/sheet1.xml x1\n" {
		t.Errorf("findings:\n%s", got)
	}
}

func TestNotAZip(t *testing.T) {
	for _, in := range [][]byte{nil, []byte("not a zip"), []byte("PK\x05\x06" + strings.Repeat("\x00", 18))} {
		f := Check(in)
		if !f.HasKind(PackageUnreadable) && !f.HasKind(PackageNoWorkbook) {
			t.Errorf("Check(%q): %s", in, f.dump())
		}
	}
}

// TestDefects: a malformed part is a finding or a note; the rest of the
// workbook is still checked and nothing fails for the whole workbook.
func TestDefects(t *testing.T) {
	p := baseParts().
		set("xl/workbook.xml", hdrMain+`<workbook `+xmlnsS+`><sheets><sheet name="Data" sheetId="1" r:id="rId1"/><sheet name="Two" sheetId="2" r:id="rId4"/></sheets></workbook>`).
		set("xl/_rels/workbook.xml.rels", strings.Replace(baseParts()["xl/_rels/workbook.xml.rels"], "</Relationships>", `<Relationship Id="rId4" Type="`+relNS+`worksheet" Target="worksheets/sheet2.xml"/></Relationships>`, 1)).
		set("xl/worksheets/sheet2.xml", `<worksheet><sheetData><row r="1"><c r="A1"></row></sheetData></worksheet>`).
		set("[Content_Types].xml", strings.Replace(baseParts()["[Content_Types].xml"], "</Types>", `<Override PartName="/xl/worksheets/sheet2.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.worksheet+xml"/></Types>`, 1))
	p["xl/worksheets/sheet1.xml"] = sheet(`<row r="2"/><row r="1"/>`, "")
	f := Check(p.zip(t))
	if !f.Has(Finding{XMLMalformed, "/xl/worksheets/sheet2.xml", ""}) {
		t.Errorf("malformed sheet not found:\n%s", f.dump())
	}
	if !f.HasKind(WorksheetRowOrder) {
		t.Errorf("the other sheet was not checked:\n%s", f.dump())
	}
	for name, bad := range map[string]string{
		"xl/styles.xml":        "<styleSheet",
		"xl/sharedStrings.xml": "<sst><si>",
	} {
		q := baseParts().set(name, bad)
		f := Check(q.zip(t))
		if !f.HasKind(XMLMalformed) {
			t.Errorf("%s: %s", name, f.dump())
		}
	}
}

func TestIntroduced(t *testing.T) {
	before := Check(baseParts().set("xl/worksheets/sheet1.xml", sheet(`<row r="2"/><row r="1"/>`, "")).zip(t))
	// The same fault moved: not introduced.
	moved := Check(baseParts().set("xl/worksheets/sheet1.xml", sheet(`<row r="5"/><row r="1"/>`, "")).zip(t))
	if got := Introduced(before, moved); len(got) != 0 {
		t.Errorf("a moved fault is introduced: %v", got)
	}
	// A second instance is introduced.
	worse := Check(baseParts().set("xl/worksheets/sheet1.xml", sheet(`<row r="2"/><row r="1"/><row r="1"/>`, "")).zip(t))
	if got := Introduced(before, worse); len(got) != 1 || got[0].Kind != WorksheetRowOrder {
		t.Errorf("Introduced = %v", got)
	}
	// A clean input and a clean output: no gate.
	clean := baseParts().zip(t)
	if err := Gate(clean, clean); err != nil {
		t.Errorf("Gate on equal inputs: %v", err)
	}
	broken := baseParts().set("xl/worksheets/sheet1.xml", sheet(`<row r="2"/><row r="1"/>`, "")).zip(t)
	err := Gate(clean, broken)
	var gf *GateFailure
	if !errors.As(err, &gf) || len(gf.Introduced) != 1 || !strings.Contains(err.Error(), "row 1 follows row 2") {
		t.Errorf("Gate(clean, broken) = %v", err)
	}
	// An existing fault never blocks, and fixing it passes.
	if err := Gate(broken, broken); err != nil {
		t.Errorf("Gate(broken, broken) = %v", err)
	}
	if err := Gate(broken, clean); err != nil {
		t.Errorf("Gate(broken, clean) = %v", err)
	}
}

func TestRefHelpers(t *testing.T) {
	for in, want := range map[string]bool{"A1": true, "TAX2023": true, "XFD1048576": true, "XFE1": false, "R": true, "C": true, "R1C1": true, "RC": true, "r2c": true, "Revenue": false, "Rate": false, "Col1": true, "Cat": false, "R1x": false, "_a": false, "A0": false} {
		if got := formula.LooksLikeCellRef(in); got != want {
			t.Errorf("LooksLikeCellRef(%q) = %v", in, got)
		}
	}
	if c, r, ok := parseCell("AB12"); !ok || c != 28 || r != 12 {
		t.Errorf("parseCell(AB12) = %d %d %v", c, r, ok)
	}
	if formula.ColumnName(16384) != "XFD" || formula.ColumnName(27) != "AA" {
		t.Error("ColumnName")
	}
	if got := sml.UnescapeX("a_x000a_b_x005F_x0041_ _xZZZZ_"); got != "a\nb_x0041_ _xZZZZ_" {
		t.Errorf("sml.UnescapeX = %q", got)
	}
	// A surrogate pair is one rune but two UTF-16 units, as the old
	// decoder's two replacement runes also counted.
	if got := sml.UnescapeX("_xD83D__xDE00_"); got != "\U0001F600" || utf16Len(got) != 2 {
		t.Errorf("sml.UnescapeX surrogate pair = %q (%d units)", got, utf16Len(got))
	}
}

// TestHeaderForms: a header that is a number, has a newline or has
// rich-text runs matches the column name as Excel writes it.
func TestHeaderForms(t *testing.T) {
	p := baseParts().withTable("")
	p["xl/worksheets/sheet1.xml"] = sheet(`<row r="1"><c r="A1"><v>2020</v></c><c r="B1" t="inlineStr"><is><r><t>Line</t></r><r><t>&#10;two</t></r></is></c></row><row r="2"><c r="A2"/><c r="B2"/></row>`, `<tableParts count="1"><tablePart r:id="rId1"/></tableParts>`)
	p["xl/tables/table1.xml"] = strings.NewReplacer(`name="Name"`, `name="2020"`, `name="Value"`, `name="Line_x000a_two"`).Replace(p["xl/tables/table1.xml"])
	if f := Check(p.zip(t)); !f.Empty() {
		t.Errorf("findings:\n%s%v", f.dump(), f.Instances())
	}
}

func TestStrictIsNotChecked(t *testing.T) {
	p := baseParts().set("xl/workbook.xml", hdrMain+`<workbook xmlns="http://purl.oclc.org/ooxml/spreadsheetml/main"/>`)
	f := Check(p.zip(t))
	if got := f.dump(); got != "package.strict in /xl/workbook.xml x1\n" {
		t.Errorf("findings:\n%s", got)
	}
}
