package integrity

import (
	"testing"

	"github.com/jhnoel/documentdriver/internal/xlsx/sml"
)

// FuzzCheck runs Check on arbitrary bytes, seeded with the test
// workbooks: it must return findings, never panic or hang.
func FuzzCheck(f *testing.F) {
	f.Add(baseParts().zip(f))
	f.Add(baseParts().withTable("").zip(f))
	f.Add(baseParts().set("xl/worksheets/sheet1.xml", sheet(`<row r="2"/><row r="1"/>`, "")).zip(f))
	f.Add([]byte("PK\x05\x06" + string(make([]byte, 18))))
	f.Fuzz(func(_ *testing.T, data []byte) {
		Check(data)
	})
}

// FuzzCheckParts runs Check on a package whose worksheet, table, workbook
// and shared strings are arbitrary, so the fuzzer works on the streaming
// checks rather than the zip container. The package's own consistency
// is checked too: the same input twice gives the same findings.
func FuzzCheckParts(f *testing.F) {
	b := baseParts().withTable("")
	f.Add(b["xl/worksheets/sheet1.xml"], b["xl/tables/table1.xml"], b["xl/workbook.xml"], b["xl/sharedStrings.xml"])
	f.Add(sheet(`<row r="1"><c r="A1" t="s"><v>0</v></c><c r="B1"><f t="shared" ref="B1:B3" si="0">A1</f></c></row>`,
		`<mergeCells count="2"><mergeCell ref="A1:B2"/><mergeCell ref="B2:C3"/></mergeCells>`), b["xl/tables/table1.xml"], b["xl/workbook.xml"], b["xl/sharedStrings.xml"])
	f.Add(`<x:worksheet xmlns:x="`+sml.NSMain+`"><x:sheetData><x:row><x:c><x:is><x:r><x:t>a</x:t></x:r><x:rPh><x:t/></x:rPh></x:is></x:c></x:row></x:sheetData></x:worksheet>`,
		"", `<workbook xmlns="`+sml.NSMain+`"><sheets><sheet name="a" sheetId="1"/></sheets><definedNames><definedName name="R1C1"/></definedNames></workbook>`, `<sst xmlns="`+sml.NSMain+`"><si/></sst>`)
	f.Fuzz(func(t *testing.T, ws, tbl, wb, sst string) {
		p := baseParts().withTable("")
		p["xl/worksheets/sheet1.xml"] = ws
		p["xl/tables/table1.xml"] = tbl
		p["xl/workbook.xml"] = wb
		p["xl/sharedStrings.xml"] = sst
		z := p.zip(t)
		a, c := Check(z), Check(z)
		if a.dump() != c.dump() {
			t.Fatalf("two checks differ:\n%s\n%s", a.dump(), c.dump())
		}
	})
}
