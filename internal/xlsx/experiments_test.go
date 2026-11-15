package xlsx

// The Excel experiments E1 to E13 (docs/design.md, xlsx): small authored
// workbooks the maintainer opens in Excel, one question each. This test
// is their generator: it builds every fixture in memory and requires the
// committed files under testdata/xlsx/experiments to hold the same parts
// (names and bytes; the zip framing may differ between Go releases). Run
//
//	XLSX_EXPERIMENTS_UPDATE=1 go test ./internal/xlsx -run TestExperimentFixtures
//
// to rewrite them, and update the README beside them when a fixture
// changes.
//
// Provenance. The package layout, the content types and relationship
// types, the workbook, styles and worksheet markup follow a workbook Excel
// wrote (docProps/app.xml: Microsoft Excel, AppVersion 16.0300): the
// embedded ppt/embeddings/Microsoft_Excel_Worksheet.xlsx (SHA-256
// 84b8f16f840f765894848a11f60e2d101e0c0e31a807b47b08c7542af49ceae7) of
// the corpus deck
// open-xml-sdk/test/DocumentFormat.OpenXml.Tests.Assets/assets/TestFiles/Of16-01.pptx
// (SHA-256 563b633c6a696faa6efe058b15a6bd6f3fa955c2286e4fe0160ef8a7e998e976,
// eval/corpus/fetch.sh pin 431ab05c). Reduced from it: no theme, no
// document properties, no extension lists or mc:Ignorable markup, and a
// font without theme colour or scheme. The calcChain part's content type
// and relationship type are not in that file; they are written as
// ECMA-376 Part 1 §12.3.1 names them, and the xlsx corpus extraction
// confirms them (ctypes.CTSpreadsheetmlCalcChain, ctypes.RelCalcChain).
// Everything in the experiments themselves (missing cached values, inline strings, a
// stale count, an orphaned shared formula, unprefixed functions) is
// authored to ask Excel one question and is not from any file.

import (
	"archive/zip"
	"bytes"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/jhnoel/documentdriver/internal/ooxml/tree"
	"github.com/jhnoel/documentdriver/internal/repo"
)

// Names and types as the Excel-written source spells them (see above).
const (
	nsMain = "http://schemas.openxmlformats.org/spreadsheetml/2006/main"
	nsRel  = "http://schemas.openxmlformats.org/officeDocument/2006/relationships"

	relOffice   = "http://schemas.openxmlformats.org/officeDocument/2006/relationships/officeDocument"
	relSheet    = "http://schemas.openxmlformats.org/officeDocument/2006/relationships/worksheet"
	relStyles   = "http://schemas.openxmlformats.org/officeDocument/2006/relationships/styles"
	relStrings  = "http://schemas.openxmlformats.org/officeDocument/2006/relationships/sharedStrings"
	relCalc     = "http://schemas.openxmlformats.org/officeDocument/2006/relationships/calcChain" // ECMA-376 Part 1 §12.3.1
	relTable    = "http://schemas.openxmlformats.org/officeDocument/2006/relationships/table"     // ECMA-376 Part 1 §12.3.1
	ctRels      = "application/vnd.openxmlformats-package.relationships+xml"
	ctXML       = "application/xml"
	ctWorkbook  = "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet.main+xml"
	ctWorksheet = "application/vnd.openxmlformats-officedocument.spreadsheetml.worksheet+xml"
	ctStyles    = "application/vnd.openxmlformats-officedocument.spreadsheetml.styles+xml"
	ctStrings   = "application/vnd.openxmlformats-officedocument.spreadsheetml.sharedStrings+xml"
	ctCalc      = "application/vnd.openxmlformats-officedocument.spreadsheetml.calcChain+xml" // ECMA-376 Part 1 §12.3.1
	ctTable     = "application/vnd.openxmlformats-officedocument.spreadsheetml.table+xml"     // ECMA-376 Part 1 §12.3.1

	xmlDecl = `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>` + "\n"
)

// stylesXML is the source's styles part without its extension list: one
// font, the two required fills, one border, one xf.
const stylesXML = xmlDecl + `<styleSheet xmlns="` + nsMain + `">` +
	`<fonts count="1"><font><sz val="11"/><name val="Calibri"/><family val="2"/></font></fonts>` +
	`<fills count="2"><fill><patternFill patternType="none"/></fill><fill><patternFill patternType="gray125"/></fill></fills>` +
	`<borders count="1"><border><left/><right/><top/><bottom/><diagonal/></border></borders>` +
	`<cellStyleXfs count="1"><xf numFmtId="0" fontId="0" fillId="0" borderId="0"/></cellStyleXfs>` +
	`<cellXfs count="1"><xf numFmtId="0" fontId="0" fillId="0" borderId="0" xfId="0"/></cellXfs>` +
	`<cellStyles count="1"><cellStyle name="Normal" xfId="0" builtinId="0"/></cellStyles>` +
	`<dxfs count="0"/><tableStyles count="0" defaultTableStyle="TableStyleMedium2" defaultPivotStyle="PivotStyleLight16"/>` +
	`</styleSheet>`

// book is one experiment workbook: a single sheet named Sheet1.
type book struct {
	name string
	// sheetData is the worksheet's sheetData content (rows); dimension its
	// dimension ref.
	sheetData, dimension string
	// calcPr is the workbook's calcPr element, "" for none.
	calcPr string
	// definedNames is the workbook's definedNames element, "" for none,
	// placed after sheets and before calcPr (CT_Workbook order).
	definedNames string
	// sst is the shared strings part's content ("" for no part), with its
	// sst attributes in sstAttrs.
	sst, sstAttrs string
	// calcChain is the calcChain part's content ("" for no part).
	calcChain string
	// table is a worksheet table part's content ("" for no part), written
	// to xl/tables/table1.xml and referenced by the worksheet's tableParts.
	table string
	// wsTail is worksheet markup placed after sheetData and before
	// pageMargins (mergeCells, conditionalFormatting, dataValidations,
	// hyperlinks); wsEnd after pageMargins (tableParts), per CT_Worksheet
	// order.
	wsTail, wsEnd string
}

func (b book) parts() []part {
	ct := xmlDecl + `<Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types">` +
		`<Default Extension="rels" ContentType="` + ctRels + `"/><Default Extension="xml" ContentType="` + ctXML + `"/>` +
		`<Override PartName="/xl/workbook.xml" ContentType="` + ctWorkbook + `"/>` +
		`<Override PartName="/xl/worksheets/sheet1.xml" ContentType="` + ctWorksheet + `"/>` +
		`<Override PartName="/xl/styles.xml" ContentType="` + ctStyles + `"/>`
	rels := xmlDecl + `<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">` +
		`<Relationship Id="rId1" Type="` + relSheet + `" Target="worksheets/sheet1.xml"/>` +
		`<Relationship Id="rId2" Type="` + relStyles + `" Target="styles.xml"/>`
	var extra []part
	if b.sst != "" {
		ct += `<Override PartName="/xl/sharedStrings.xml" ContentType="` + ctStrings + `"/>`
		rels += `<Relationship Id="rId3" Type="` + relStrings + `" Target="sharedStrings.xml"/>`
		extra = append(extra, part{"xl/sharedStrings.xml", xmlDecl + `<sst xmlns="` + nsMain + `" ` + b.sstAttrs + `>` + b.sst + `</sst>`})
	}
	if b.calcChain != "" {
		ct += `<Override PartName="/xl/calcChain.xml" ContentType="` + ctCalc + `"/>`
		rels += `<Relationship Id="rId4" Type="` + relCalc + `" Target="calcChain.xml"/>`
		extra = append(extra, part{"xl/calcChain.xml", xmlDecl + `<calcChain xmlns="` + nsMain + `">` + b.calcChain + `</calcChain>`})
	}
	tableParts := ""
	if b.table != "" {
		ct += `<Override PartName="/xl/tables/table1.xml" ContentType="` + ctTable + `"/>`
		rels += `<Relationship Id="rId5" Type="` + relTable + `" Target="tables/table1.xml"/>`
		tableParts = `<tableParts count="1"><tablePart r:id="rId5"/></tableParts>`
		extra = append(extra, part{"xl/tables/table1.xml", xmlDecl + b.table})
	}
	ct += `</Types>`
	rels += `</Relationships>`
	dim := ""
	if b.dimension != "" {
		dim = `<dimension ref="` + b.dimension + `"/>`
	}
	return append([]part{
		{"[Content_Types].xml", ct},
		{"_rels/.rels", xmlDecl + `<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">` +
			`<Relationship Id="rId1" Type="` + relOffice + `" Target="xl/workbook.xml"/></Relationships>`},
		{"xl/workbook.xml", xmlDecl + `<workbook xmlns="` + nsMain + `" xmlns:r="` + nsRel + `">` +
			`<workbookPr/><bookViews><workbookView xWindow="0" yWindow="0" windowWidth="21570" windowHeight="10215"/></bookViews>` +
			`<sheets><sheet name="Sheet1" sheetId="1" r:id="rId1"/></sheets>` + b.definedNames + b.calcPr + `</workbook>`},
		{"xl/_rels/workbook.xml.rels", rels},
		{"xl/styles.xml", stylesXML},
		{"xl/worksheets/sheet1.xml", xmlDecl + `<worksheet xmlns="` + nsMain + `" xmlns:r="` + nsRel + `">` + dim +
			`<sheetViews><sheetView tabSelected="1" workbookViewId="0"/></sheetViews><sheetFormatPr defaultRowHeight="15"/>` +
			`<sheetData>` + b.sheetData + `</sheetData>` + b.wsTail +
			`<pageMargins left="0.7" right="0.7" top="0.75" bottom="0.75" header="0.3" footer="0.3"/>` + tableParts + b.wsEnd + `</worksheet>`},
	}, extra...)
}

type part struct{ name, body string }

// fullCalc is the calcPr the recalculation policy writes (design: set
// fullCalcOnLoad); calcId is the source's.
const fullCalc = `<calcPr calcId="162913" fullCalcOnLoad="1"/>`

// twoValues are A1=2 and A2=3, the inputs of the formula experiments.
const twoValues = `<row r="1"><c r="A1"><v>2</v></c></row><row r="2"><c r="A2"><v>3</v></c></row>`

// tableNameQty is the E10 table part: ref A1:B3, columns named Name and
// Qty. The worksheet header cells carry the names; e10-mismatch spells
// the second one differently.
const tableNameQty = `<table xmlns="` + nsMain + `" id="1" name="Table1" displayName="Table1" ref="A1:B3" totalsRowShown="0">` +
	`<autoFilter ref="A1:B3"/>` +
	`<tableColumns count="2"><tableColumn id="1" name="Name"/><tableColumn id="2" name="Qty"/></tableColumns>` +
	`<tableStyleInfo name="TableStyleMedium2" showFirstColumn="0" showLastColumn="0" showRowStripes="1" showColumnStripes="0"/>` +
	`</table>`

// experiments are the fixtures, in README order. Each name starts with
// its experiment.
var experiments = []book{
	// E1: a formula cell with no cached value, without and with
	// fullCalcOnLoad.
	{name: "e1-formula-no-value.xlsx", dimension: "A1:A3", calcPr: `<calcPr calcId="162913"/>`,
		sheetData: twoValues + `<row r="3"><c r="A3"><f>A1+A2</f></c></row>`},
	{name: "e1-formula-no-value-fullcalc.xlsx", dimension: "A1:A3", calcPr: fullCalc,
		sheetData: twoValues + `<row r="3"><c r="A3"><f>A1+A2</f></c></row>`},
	// E2: a calcChain entry naming a cell that holds no formula, against
	// the same workbook with the calcChain removed (part, relationship
	// and override).
	{name: "e2-calcchain-stale.xlsx", dimension: "A1:B3", calcPr: `<calcPr calcId="162913"/>`,
		sheetData: `<row r="1"><c r="A1"><v>2</v></c><c r="B1"><v>7</v></c></row>` +
			`<row r="2"><c r="A2"><v>3</v></c></row><row r="3"><c r="A3"><f>A1+A2</f><v>5</v></c></row>`,
		calcChain: `<c r="A3" i="1"/><c r="B1"/>`},
	{name: "e2-calcchain-removed.xlsx", dimension: "A1:B3", calcPr: `<calcPr calcId="162913"/>`,
		sheetData: `<row r="1"><c r="A1"><v>2</v></c><c r="B1"><v>7</v></c></row>` +
			`<row r="2"><c r="A2"><v>3</v></c></row><row r="3"><c r="A3"><f>A1+A2</f><v>5</v></c></row>`},
	// E3: inline strings beside a shared string: plain, edge spaces kept
	// with xml:space, a control character escaped as _x0009_ (tab), and a
	// literal _x0041_ escaped as _x005F_x0041_.
	{name: "e3-inline-strings.xlsx", dimension: "A1:A5",
		sheetData: `<row r="1"><c r="A1" t="s"><v>0</v></c></row>` +
			`<row r="2"><c r="A2" t="inlineStr"><is><t>Inline</t></is></c></row>` +
			`<row r="3"><c r="A3" t="inlineStr"><is><t xml:space="preserve">  padded  </t></is></c></row>` +
			`<row r="4"><c r="A4" t="inlineStr"><is><t>tab_x0009_here</t></is></c></row>` +
			`<row r="5"><c r="A5" t="inlineStr"><is><t>_x005F_x0041_</t></is></c></row>`,
		sst: `<si><t>Shared</t></si>`, sstAttrs: `count="1" uniqueCount="1"`},
	// E4: the shared strings' count and uniqueCount below what the sheet
	// uses (three references to three strings, declared as one).
	{name: "e4-sst-count-stale.xlsx", dimension: "A1:A3",
		sheetData: `<row r="1"><c r="A1" t="s"><v>0</v></c></row><row r="2"><c r="A2" t="s"><v>1</v></c></row>` +
			`<row r="3"><c r="A3" t="s"><v>2</v></c></row>`,
		sst: `<si><t>one</t></si><si><t>two</t></si><si><t>three</t></si>`, sstAttrs: `count="1" uniqueCount="1"`},
	// E5: a shared-formula child (si 0) whose master is gone.
	{name: "e5-orphan-shared-formula.xlsx", dimension: "A1:B2", calcPr: `<calcPr calcId="162913"/>`,
		sheetData: `<row r="1"><c r="A1"><v>2</v></c><c r="B1"><v>4</v></c></row>` +
			`<row r="2"><c r="A2"><v>3</v></c><c r="B2"><f t="shared" si="0"/><v>6</v></c></row>`},
	// E6: newer functions without the _xlfn. prefix Excel writes for them
	// (A3, A4), beside the prefixed form (A5), with fullCalcOnLoad.
	{name: "e6-new-function-no-prefix.xlsx", dimension: "A1:A5", calcPr: fullCalc,
		sheetData: `<row r="1"><c r="A1" t="inlineStr"><is><t>ab</t></is></c></row>` +
			`<row r="2"><c r="A2" t="inlineStr"><is><t>cd</t></is></c></row>` +
			`<row r="3"><c r="A3"><f>CONCAT(A1,A2)</f></c></row>` +
			`<row r="4"><c r="A4"><f>TEXTJOIN("-",TRUE,A1:A2)</f></c></row>` +
			`<row r="5"><c r="A5"><f>_xlfn.CONCAT(A1,A2)</f></c></row>`},
	// E7: a spill function written as a plain cell formula: no array
	// type, no ref, no cell metadata; prefixed (A1) and bare (C1).
	{name: "e7-spill-plain.xlsx", dimension: "A1:C1", calcPr: fullCalc,
		sheetData: `<row r="1"><c r="A1"><f>_xlfn.SEQUENCE(3)</f></c><c r="C1"><f>SEQUENCE(3)</f></c></row>`},
	// E8: the blank workbook create starts from: one empty sheet, no
	// shared strings, no theme, no document properties.
	{name: "e8-blank.xlsx", dimension: "A1"},
	// E9: a formula whose cached value went stale when an input changed
	// (A1 edited from 2 to 7 while A3 and A4 keep their old cached 5 and
	// 50), with and without fullCalcOnLoad, and no calcChain. Does the
	// reader show the stale 5/50 or recompute 10/100?
	{name: "e9-edited.xlsx", dimension: "A1:A4", calcPr: fullCalc,
		sheetData: `<row r="1"><c r="A1"><v>7</v></c></row><row r="2"><c r="A2"><v>3</v></c></row>` +
			`<row r="3"><c r="A3"><f>A1+A2</f><v>5</v></c></row><row r="4"><c r="A4"><f>A3*10</f><v>50</v></c></row>`},
	{name: "e9-edited-noflag.xlsx", dimension: "A1:A4", calcPr: `<calcPr calcId="162913"/>`,
		sheetData: `<row r="1"><c r="A1"><v>7</v></c></row><row r="2"><c r="A2"><v>3</v></c></row>` +
			`<row r="3"><c r="A3"><f>A1+A2</f><v>5</v></c></row><row r="4"><c r="A4"><f>A3*10</f><v>50</v></c></row>`},
	// E10: a table whose tableColumns names (Name, Qty) disagree with its
	// header cells (B1 "Quantity" in e10-mismatch, "Qty" in the e10-match
	// control). Does the reader trust the header cells or the tableColumns?
	{name: "e10-mismatch.xlsx", dimension: "A1:B3", table: tableNameQty,
		sheetData: `<row r="1"><c r="A1" t="inlineStr"><is><t>Name</t></is></c>` +
			`<c r="B1" t="inlineStr"><is><t>Quantity</t></is></c></row>` +
			`<row r="2"><c r="A2" t="inlineStr"><is><t>Alice</t></is></c><c r="B2"><v>4</v></c></row>` +
			`<row r="3"><c r="A3" t="inlineStr"><is><t>Bob</t></is></c><c r="B3"><v>5</v></c></row>`},
	{name: "e10-match.xlsx", dimension: "A1:B3", table: tableNameQty,
		sheetData: `<row r="1"><c r="A1" t="inlineStr"><is><t>Name</t></is></c>` +
			`<c r="B1" t="inlineStr"><is><t>Qty</t></is></c></row>` +
			`<row r="2"><c r="A2" t="inlineStr"><is><t>Alice</t></is></c><c r="B2"><v>4</v></c></row>` +
			`<row r="3"><c r="A3" t="inlineStr"><is><t>Bob</t></is></c><c r="B3"><v>5</v></c></row>`},
	// E11: empty container elements at their schema positions, one
	// workbook each: mergeCells count 0, hyperlinks with no hyperlink,
	// conditionalFormatting with an empty sqref, dataValidations count 0,
	// an empty definedNames, and tableParts count 0. Does each open
	// without a repair prompt?
	{name: "e11-mergecells.xlsx", dimension: "A1", wsTail: `<mergeCells count="0"/>`},
	{name: "e11-hyperlinks.xlsx", dimension: "A1", wsTail: `<hyperlinks/>`},
	{name: "e11-cf.xlsx", dimension: "A1",
		wsTail: `<conditionalFormatting sqref=""><cfRule type="cellIs" priority="1" operator="greaterThan"><formula>0</formula></cfRule></conditionalFormatting>`},
	{name: "e11-dv.xlsx", dimension: "A1", wsTail: `<dataValidations count="0"/>`},
	{name: "e11-definednames.xlsx", dimension: "A1", definedNames: `<definedNames/>`},
	{name: "e11-tableparts.xlsx", dimension: "A1", wsEnd: `<tableParts count="0"/>`},
	// E12: a row whose spans attribute claims columns 1:2 but whose cells
	// run to column E. Does the reader trust spans or the cells?
	{name: "e12-stale-row-spans.xlsx", dimension: "A1:E1",
		sheetData: `<row r="1" spans="1:2"><c r="A1"><v>1</v></c><c r="B1"><v>2</v></c><c r="C1"><v>3</v></c><c r="D1"><v>4</v></c><c r="E1"><v>5</v></c></row>`},
	// E13: the built-in Print_Area name holding a broken reference. Does
	// the reader surface the #REF! or drop the area?
	{name: "e13-print-area-ref.xlsx", dimension: "A1",
		definedNames: `<definedNames><definedName name="_xlnm.Print_Area" localSheetId="0">Sheet1!#REF!</definedName></definedNames>`},
}

// zipBook writes the parts as a zip, deflated, with a fixed time.
func zipBook(parts []part) ([]byte, error) {
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for _, p := range parts {
		w, err := zw.CreateHeader(&zip.FileHeader{Name: p.name, Method: zip.Deflate, Modified: time.Date(1980, 1, 1, 0, 0, 0, 0, time.UTC)})
		if err != nil {
			return nil, err
		}
		if _, err := io.WriteString(w, p.body); err != nil {
			return nil, err
		}
	}
	if err := zw.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// unzipBook reads a zip's members in order.
func unzipBook(b []byte) ([]part, error) {
	zr, err := zip.NewReader(bytes.NewReader(b), int64(len(b)))
	if err != nil {
		return nil, err
	}
	var out []part
	for _, f := range zr.File {
		r, err := f.Open()
		if err != nil {
			return nil, err
		}
		body, err := io.ReadAll(io.LimitReader(r, 1<<20))
		r.Close()
		if err != nil {
			return nil, err
		}
		out = append(out, part{f.Name, string(body)})
	}
	return out, nil
}

func TestExperimentFixtures(t *testing.T) {
	root, err := repo.Root()
	if err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(root, "testdata", "xlsx", "experiments")
	update := os.Getenv("XLSX_EXPERIMENTS_UPDATE") == "1"
	readme, err := os.ReadFile(filepath.Join(dir, "README.md"))
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, b := range experiments {
		names = append(names, b.name)
		parts := b.parts()
		for _, p := range parts {
			if _, err := tree.ParseString(p.body); err != nil {
				t.Errorf("%s %s: %v", b.name, p.name, err)
			}
		}
		if !strings.Contains(string(readme), "`"+b.name+"`") {
			t.Errorf("README.md does not describe %s", b.name)
		}
		path := filepath.Join(dir, b.name)
		if update {
			z, err := zipBook(parts)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, z, 0o644); err != nil { //nolint:gosec // G306: a tracked fixture
				t.Fatal(err)
			}
			continue
		}
		raw, err := os.ReadFile(path) //nolint:gosec // G304: a fixture under the repo's testdata
		if err != nil {
			t.Fatalf("%v (regenerate with XLSX_EXPERIMENTS_UPDATE=1)", err)
		}
		got, err := unzipBook(raw)
		if err != nil {
			t.Fatalf("%s: %v", b.name, err)
		}
		if !slices.Equal(got, parts) {
			t.Errorf("%s is stale: regenerate with XLSX_EXPERIMENTS_UPDATE=1", b.name)
		}
	}
	// No fixture without a generator entry.
	files, err := filepath.Glob(filepath.Join(dir, "*.xlsx"))
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range files {
		if !slices.Contains(names, filepath.Base(f)) {
			t.Errorf("%s has no entry in experiments", filepath.Base(f))
		}
	}
	if !t.Failed() && update {
		t.Logf("wrote %d fixtures to %s", len(names), dir)
	}
}
