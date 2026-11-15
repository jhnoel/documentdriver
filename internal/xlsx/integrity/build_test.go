package integrity

import (
	"archive/zip"
	"bytes"
	"fmt"
	"strings"
	"testing"
)

// A minimal workbook, authored for these tests, in the shape Excel writes
// (the package layout follows testdata/xlsx/experiments).
const (
	hdrMain = `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>` + "\n"
	xmlnsS  = `xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main" xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships"`
	relNS   = "http://schemas.openxmlformats.org/officeDocument/2006/relationships/"
)

type parts map[string]string

func baseParts() parts {
	return parts{
		"[Content_Types].xml": hdrMain + `<Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types">` +
			`<Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/>` +
			`<Default Extension="xml" ContentType="application/xml"/>` +
			`<Override PartName="/xl/workbook.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.sheet.main+xml"/>` +
			`<Override PartName="/xl/worksheets/sheet1.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.worksheet+xml"/>` +
			`<Override PartName="/xl/styles.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.styles+xml"/>` +
			`<Override PartName="/xl/sharedStrings.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.sharedStrings+xml"/>` +
			`<Override PartName="/xl/tables/table1.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.table+xml"/>` +
			`</Types>`,
		"_rels/.rels": hdrMain + `<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">` +
			`<Relationship Id="rId1" Type="` + relNS + `officeDocument" Target="xl/workbook.xml"/></Relationships>`,
		"xl/workbook.xml": hdrMain + `<workbook ` + xmlnsS + `><sheets><sheet name="Data" sheetId="1" r:id="rId1"/></sheets></workbook>`,
		"xl/_rels/workbook.xml.rels": hdrMain + `<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">` +
			`<Relationship Id="rId1" Type="` + relNS + `worksheet" Target="worksheets/sheet1.xml"/>` +
			`<Relationship Id="rId2" Type="` + relNS + `styles" Target="styles.xml"/>` +
			`<Relationship Id="rId3" Type="` + relNS + `sharedStrings" Target="sharedStrings.xml"/></Relationships>`,
		"xl/styles.xml": hdrMain + `<styleSheet ` + xmlnsS + `><fonts count="1"><font/></fonts><fills count="1"><fill/></fills><borders count="1"><border/></borders>` +
			`<cellStyleXfs count="1"><xf/></cellStyleXfs><cellXfs count="2"><xf numFmtId="0" fontId="0" fillId="0" borderId="0" xfId="0"/><xf numFmtId="14" fontId="0" fillId="0" borderId="0" xfId="0"/></cellXfs></styleSheet>`,
		"xl/sharedStrings.xml": hdrMain + `<sst ` + xmlnsS + ` count="2" uniqueCount="2"><si><t>Name</t></si><si><r><t>Va</t></r><r><t>lue</t></r><rPh><t>ignored</t></rPh></si></sst>`,
		"xl/worksheets/sheet1.xml": sheet(`<row r="1"><c r="A1" t="s"><v>0</v></c><c r="B1" t="s"><v>1</v></c></row>`+
			`<row r="2"><c r="A2" s="1"><v>1</v></c><c r="B2"><f>A2*2</f><v>2</v></c></row>`, ""),
	}
}

// sheet wraps rows and the elements after sheetData in a worksheet.
func sheet(rows, tail string) string {
	return hdrMain + `<worksheet ` + xmlnsS + `><dimension ref="A1:B2"/><sheetData>` + rows + `</sheetData>` + tail + `</worksheet>`
}

// withTable adds a table over A1:B2 to the base workbook's sheet, with the
// given header names.
func (p parts) withTable(extra string) parts {
	p["xl/worksheets/sheet1.xml"] = strings.Replace(p["xl/worksheets/sheet1.xml"], "</worksheet>", `<tableParts count="1"><tablePart r:id="rId1"/></tableParts></worksheet>`, 1)
	p["xl/worksheets/_rels/sheet1.xml.rels"] = hdrMain + `<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">` +
		`<Relationship Id="rId1" Type="` + relNS + `table" Target="../tables/table1.xml"/></Relationships>`
	p["xl/tables/table1.xml"] = hdrMain + `<table ` + xmlnsS + ` id="1" name="T1" displayName="T1" ref="A1:B2" ` + extra + `>` +
		`<autoFilter ref="A1:B2"/><tableColumns count="2"><tableColumn id="1" name="Name"/><tableColumn id="2" name="Value"/></tableColumns></table>`
	return p
}

func (p parts) set(name, content string) parts {
	p[name] = content
	return p
}

func (p parts) zip(t testing.TB) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	// [Content_Types].xml first, the rest in a fixed order.
	order := []string{"[Content_Types].xml", "_rels/.rels"}
	for name := range p {
		if name != order[0] && name != order[1] {
			order = append(order, name)
		}
	}
	sortStrings(order[2:])
	for _, name := range order {
		if p[name] == "" {
			continue
		}
		w, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write([]byte(p[name])); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func sortStrings(s []string) {
	for i := 1; i < len(s); i++ {
		for j := i; j > 0 && s[j] < s[j-1]; j-- {
			s[j], s[j-1] = s[j-1], s[j]
		}
	}
}

func (f *Findings) dump() string {
	var b strings.Builder
	for _, k := range f.Kinds() {
		fmt.Fprintf(&b, "%s x%d\n", k, f.Count(k))
	}
	return b.String()
}
