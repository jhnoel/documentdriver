package create

import (
	"github.com/jhnoel/documentdriver/internal/ooxml/ctypes"
	"github.com/jhnoel/documentdriver/internal/opc"
	"github.com/jhnoel/documentdriver/internal/xlsx/sml"
)

// Template returns the blank workbook (chunk K14): one empty worksheet,
// Sheet1, no shared strings, no theme, no document properties, authored
// in code like internal/docx/create/template.go and the same workbook
// as experiment E8 (internal/xlsx/experiments_test.go, e8-blank.xlsx;
// provenance there): content types and relationship types from
// internal/ooxml/ctypes; the workbook with workbookPr, bookViews,
// sheets and calcPr calcId="191029"; styles.xml with one font (Calibri
// 11), the two required fills, one border, one cell style xf, one cellXf
// and the Normal cell style; a worksheet with dimension A1, sheetViews
// (tabSelected), sheetFormatPr defaultRowHeight="15", an empty
// sheetData and pageMargins. Members are deflated with a fixed time
// (1980-01-01), in a fixed order, so the bytes are reproducible. It
// passes integrity.Check with no finding.
func Template() ([]byte, error) {
	const decl = `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>` + "\n"
	parts := []opc.ZipMember{
		{Name: "[Content_Types].xml", Body: decl + `<Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types">` +
			`<Default Extension="rels" ContentType="` + ctypes.CTPackageRelationships + `"/><Default Extension="xml" ContentType="application/xml"/>` +
			`<Override PartName="/xl/workbook.xml" ContentType="` + ctypes.CTSpreadsheetmlSheetMain + `"/>` +
			`<Override PartName="/xl/worksheets/sheet1.xml" ContentType="` + ctypes.CTSpreadsheetmlWorksheet + `"/>` +
			`<Override PartName="/xl/styles.xml" ContentType="` + ctypes.CTSpreadsheetmlStyles + `"/></Types>`},
		{Name: "_rels/.rels", Body: decl + `<Relationships xmlns="` + sml.NSPackageRel + `"><Relationship Id="rId1" Type="` + ctypes.RelOfficeDocument + `" Target="xl/workbook.xml"/></Relationships>`},
		{Name: "xl/workbook.xml", Body: decl + `<workbook xmlns="` + sml.NSMain + `" xmlns:r="` + sml.NSRel + `">` +
			`<workbookPr/><bookViews><workbookView xWindow="0" yWindow="0" windowWidth="21570" windowHeight="10215"/></bookViews>` +
			`<sheets><sheet name="Sheet1" sheetId="1" r:id="rId1"/></sheets><calcPr calcId="191029"/></workbook>`},
		{Name: "xl/_rels/workbook.xml.rels", Body: decl + `<Relationships xmlns="` + sml.NSPackageRel + `">` +
			`<Relationship Id="rId1" Type="` + ctypes.RelWorksheet + `" Target="worksheets/sheet1.xml"/>` +
			`<Relationship Id="rId2" Type="` + ctypes.RelStyles + `" Target="styles.xml"/></Relationships>`},
		{Name: "xl/styles.xml", Body: decl + `<styleSheet xmlns="` + sml.NSMain + `">` +
			`<fonts count="1"><font><sz val="11"/><name val="Calibri"/><family val="2"/></font></fonts>` +
			`<fills count="2"><fill><patternFill patternType="none"/></fill><fill><patternFill patternType="gray125"/></fill></fills>` +
			`<borders count="1"><border><left/><right/><top/><bottom/><diagonal/></border></borders>` +
			`<cellStyleXfs count="1"><xf numFmtId="0" fontId="0" fillId="0" borderId="0"/></cellStyleXfs>` +
			`<cellXfs count="1"><xf numFmtId="0" fontId="0" fillId="0" borderId="0" xfId="0"/></cellXfs>` +
			`<cellStyles count="1"><cellStyle name="Normal" xfId="0" builtinId="0"/></cellStyles>` +
			`<dxfs count="0"/><tableStyles count="0" defaultTableStyle="TableStyleMedium2" defaultPivotStyle="PivotStyleLight16"/></styleSheet>`},
		{Name: "xl/worksheets/sheet1.xml", Body: decl + `<worksheet xmlns="` + sml.NSMain + `" xmlns:r="` + sml.NSRel + `">` +
			`<dimension ref="A1"/><sheetViews><sheetView tabSelected="1" workbookViewId="0"/></sheetViews>` +
			`<sheetFormatPr defaultRowHeight="15"/><sheetData></sheetData>` +
			`<pageMargins left="0.7" right="0.7" top="0.75" bottom="0.75" header="0.3" footer="0.3"/></worksheet>`},
	}
	return opc.BuildZip(parts...)
}
