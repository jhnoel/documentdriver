// Package xlsxtest builds small workbooks for the xlsx write tests
// (internal/xlsx/edit, ops, apply, shift, create, recalc, cmd/xlsx). It
// is imported only by tests. The markup follows the Excel-written source
// the experiment fixtures name (internal/xlsx/experiments_test.go,
// provenance there): content types, relationship types and the styles
// part; everything a test puts in a sheet is authored by the test.
package xlsxtest

import (
	"archive/zip"
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/jhnoel/documentdriver/internal/ooxml/ctypes"
	"github.com/jhnoel/documentdriver/internal/ooxml/xmlscan"
	"github.com/jhnoel/documentdriver/internal/opc"
)

// Namespaces. NEEDS-EXPORT: xlsxtest cannot import sml or formula
// (formula's internal tests import xlsxtest: import cycle), so these
// stay until a cycle-free home for the constants lands.
const (
	NSMain   = "http://schemas.openxmlformats.org/spreadsheetml/2006/main"
	NSStrict = "http://purl.oclc.org/ooxml/spreadsheetml/main"
	NSRel    = "http://schemas.openxmlformats.org/officeDocument/2006/relationships"
	nsPkgRel = "http://schemas.openxmlformats.org/package/2006/relationships"
	xmlDecl  = `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>` + "\n"
)

// Sheet is one worksheet of a Book.
type Sheet struct {
	// Name is the sheet's name; State "" (visible), "hidden" or
	// "veryHidden".
	Name, State string
	// Rows is sheetData's content (the <row> elements, as written).
	Rows string
	// Dimension is dimension@ref ("" writes no dimension element).
	Dimension string
	// Head is markup between dimension and sheetData other than
	// sheetViews and sheetFormatPr (cols); Tail markup after sheetData
	// and before pageMargins (mergeCells, conditionalFormatting,
	// dataValidations, hyperlinks); End markup after pageMargins
	// (other than tableParts, which Tables write).
	Head, Tail, End string
	// Prefix, when set ("x"), writes the worksheet with that prefix for
	// the main namespace instead of the default namespace.
	Prefix string
	// Tables are the table parts on the sheet: each a <table> element's
	// content (attributes and children are the test's), written as
	// xl/tables/tableN.xml.
	Tables []string
	// EmptySheetData writes <sheetData/> when Rows is "".
	EmptySheetData bool
}

// Book is a workbook to build.
type Book struct {
	Sheets []Sheet
	// SST are the shared strings' <si> contents ("" items allowed); nil
	// writes no shared strings part.
	SST []string
	// NumFmts are custom number formats by id; XFs the cellXfs entries'
	// numFmtIds (nil: one General xf).
	NumFmts map[int]string
	XFs     []int
	// DefinedNames is the content of <definedNames> ("" for none);
	// CalcPr the calcPr element ("" for none); Date1904 sets
	// workbookPr@date1904.
	DefinedNames, CalcPr string
	Date1904             bool
	// CalcChain is the calcChain part's content ("" for no part).
	CalcChain string
	// Strict writes the workbook and sheets in the Strict namespace.
	Strict bool
}

// Bytes returns the workbook as an .xlsx package: deflated, with a fixed
// time, members in a fixed order.
func (b Book) Bytes() ([]byte, error) {
	ns := NSMain
	if b.Strict {
		ns = NSStrict
	}
	var ct strings.Builder
	ct.WriteString(xmlDecl + `<Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types">` +
		`<Default Extension="rels" ContentType="` + ctypes.CTPackageRelationships + `"/><Default Extension="xml" ContentType="` + ctypes.CTApplicationXml + `"/>` +
		`<Override PartName="/xl/workbook.xml" ContentType="` + ctypes.CTSpreadsheetmlSheetMain + `"/>` +
		`<Override PartName="/xl/styles.xml" ContentType="` + ctypes.CTSpreadsheetmlStyles + `"/>`)
	rels := xmlDecl + `<Relationships xmlns="` + nsPkgRel + `">` +
		`<Relationship Id="rIdStyles" Type="` + ctypes.RelStyles + `" Target="styles.xml"/>`
	var sheets strings.Builder
	var out []opc.ZipMember
	table := 0
	for i, s := range b.Sheets {
		n := strconv.Itoa(i + 1)
		ct.WriteString(`<Override PartName="/xl/worksheets/sheet` + n + `.xml" ContentType="` + ctypes.CTSpreadsheetmlWorksheet + `"/>`)
		rels += `<Relationship Id="rId` + n + `" Type="` + ctypes.RelWorksheet + `" Target="worksheets/sheet` + n + `.xml"/>`
		state := ""
		if s.State != "" {
			state = ` state="` + s.State + `"`
		}
		fmt.Fprintf(&sheets, `<sheet name="%s" sheetId="%d"%s r:id="rId%d"/>`, xmlscan.EscapeAttr(s.Name, '"'), i+1, state, i+1)
		var srels strings.Builder
		tableParts := ""
		for _, t := range s.Tables {
			table++
			tn := strconv.Itoa(table)
			ct.WriteString(`<Override PartName="/xl/tables/table` + tn + `.xml" ContentType="` + ctypes.CTSpreadsheetmlTable + `"/>`)
			srels.WriteString(`<Relationship Id="rIdT` + tn + `" Type="` + ctypes.RelTable + `" Target="../tables/table` + tn + `.xml"/>`)
			tableParts += `<tablePart r:id="rIdT` + tn + `"/>`
			out = append(out, opc.ZipMember{Name: "xl/tables/table" + tn + ".xml", Body: xmlDecl + strings.Replace(t, "<table ", `<table xmlns="`+ns+`" `, 1)})
		}
		if tableParts != "" {
			tableParts = `<tableParts count="` + strconv.Itoa(len(s.Tables)) + `">` + tableParts + `</tableParts>`
			out = append(out, opc.ZipMember{Name: "xl/worksheets/_rels/sheet" + n + ".xml.rels",
				Body: xmlDecl + `<Relationships xmlns="` + nsPkgRel + `">` + srels.String() + `</Relationships>`})
		}
		p, decl := "", `xmlns="`+ns+`"`
		if s.Prefix != "" {
			p, decl = s.Prefix+":", `xmlns:`+s.Prefix+`="`+ns+`"`
		}
		dim := ""
		if s.Dimension != "" {
			dim = `<` + p + `dimension ref="` + s.Dimension + `"/>`
		}
		data := `<` + p + `sheetData>` + s.Rows + `</` + p + `sheetData>`
		if s.Rows == "" && s.EmptySheetData {
			data = `<` + p + `sheetData/>`
		}
		body := xmlDecl + `<` + p + `worksheet ` + decl + ` xmlns:r="` + NSRel + `">` + dim +
			`<` + p + `sheetViews><` + p + `sheetView workbookViewId="0"/></` + p + `sheetViews><` + p + `sheetFormatPr defaultRowHeight="15"/>` +
			s.Head + data + s.Tail +
			`<` + p + `pageMargins left="0.7" right="0.7" top="0.75" bottom="0.75" header="0.3" footer="0.3"/>` + s.End + tableParts +
			`</` + p + `worksheet>`
		out = append(out, opc.ZipMember{Name: "xl/worksheets/sheet" + n + ".xml", Body: body})
	}
	if b.SST != nil {
		ct.WriteString(`<Override PartName="/xl/sharedStrings.xml" ContentType="` + ctypes.CTSpreadsheetmlSharedStrings + `"/>`)
		rels += `<Relationship Id="rIdSST" Type="` + ctypes.RelSharedStrings + `" Target="sharedStrings.xml"/>`
		var si strings.Builder
		for _, s := range b.SST {
			si.WriteString("<si>" + s + "</si>")
		}
		out = append(out, opc.ZipMember{Name: "xl/sharedStrings.xml", Body: xmlDecl + fmt.Sprintf(`<sst xmlns="%s" count="%d" uniqueCount="%d">`, ns, len(b.SST), len(b.SST)) + si.String() + `</sst>`})
	}
	if b.CalcChain != "" {
		ct.WriteString(`<Override PartName="/xl/calcChain.xml" ContentType="` + ctypes.CTSpreadsheetmlCalcChain + `"/>`)
		rels += `<Relationship Id="rIdCalc" Type="` + ctypes.RelCalcChain + `" Target="calcChain.xml"/>`
		out = append(out, opc.ZipMember{Name: "xl/calcChain.xml", Body: xmlDecl + `<calcChain xmlns="` + ns + `">` + b.CalcChain + `</calcChain>`})
	}
	ct.WriteString(`</Types>`)
	rels += `</Relationships>`
	wbPr := `<workbookPr/>`
	if b.Date1904 {
		wbPr = `<workbookPr date1904="1"/>`
	}
	names := ""
	if b.DefinedNames != "" {
		names = `<definedNames>` + b.DefinedNames + `</definedNames>`
	}
	all := []opc.ZipMember{
		{Name: "[Content_Types].xml", Body: ct.String()},
		{Name: "_rels/.rels", Body: xmlDecl + `<Relationships xmlns="` + nsPkgRel + `"><Relationship Id="rId1" Type="` + ctypes.RelOfficeDocument + `" Target="xl/workbook.xml"/></Relationships>`},
		{Name: "xl/workbook.xml", Body: xmlDecl + `<workbook xmlns="` + ns + `" xmlns:r="` + NSRel + `">` + wbPr +
			`<bookViews><workbookView xWindow="0" yWindow="0" windowWidth="21570" windowHeight="10215"/></bookViews>` +
			`<sheets>` + sheets.String() + `</sheets>` + names + b.CalcPr + `</workbook>`},
		{Name: "xl/_rels/workbook.xml.rels", Body: rels},
		{Name: "xl/styles.xml", Body: b.styles(ns)},
	}
	all = append(all, out...)
	return opc.BuildZip(all...)
}

// styles returns the styles part content for the book in namespace ns.
func (b Book) styles(ns string) string {
	xfs := b.XFs
	if xfs == nil {
		xfs = []int{0}
	}
	var nf, xf strings.Builder
	ids := make([]int, 0, len(b.NumFmts))
	for id := range b.NumFmts {
		ids = append(ids, id)
	}
	for i := range ids {
		for j := i + 1; j < len(ids); j++ {
			if ids[j] < ids[i] {
				ids[i], ids[j] = ids[j], ids[i]
			}
		}
	}
	for _, id := range ids {
		fmt.Fprintf(&nf, `<numFmt numFmtId="%d" formatCode="%s"/>`, id, xmlscan.EscapeAttr(b.NumFmts[id], '"'))
	}
	numFmts := ""
	if len(ids) > 0 {
		numFmts = fmt.Sprintf(`<numFmts count="%d">%s</numFmts>`, len(ids), nf.String())
	}
	for _, id := range xfs {
		apply := ""
		if id != 0 {
			apply = ` applyNumberFormat="1"`
		}
		fmt.Fprintf(&xf, `<xf numFmtId="%d" fontId="0" fillId="0" borderId="0" xfId="0"%s/>`, id, apply)
	}
	return xmlDecl + `<styleSheet xmlns="` + ns + `">` + numFmts +
		`<fonts count="1"><font><sz val="11"/><name val="Calibri"/><family val="2"/></font></fonts>` +
		`<fills count="2"><fill><patternFill patternType="none"/></fill><fill><patternFill patternType="gray125"/></fill></fills>` +
		`<borders count="1"><border><left/><right/><top/><bottom/><diagonal/></border></borders>` +
		`<cellStyleXfs count="1"><xf numFmtId="0" fontId="0" fillId="0" borderId="0"/></cellStyleXfs>` +
		fmt.Sprintf(`<cellXfs count="%d">%s</cellXfs>`, len(xfs), xf.String()) +
		`<cellStyles count="1"><cellStyle name="Normal" xfId="0" builtinId="0"/></cellStyles>` +
		`<dxfs count="0"/><tableStyles count="0" defaultTableStyle="TableStyleMedium2" defaultPivotStyle="PivotStyleLight16"/>` +
		`</styleSheet>`
}

// Write writes the workbook to dir/name and returns the path.
func (b Book) Write(dir, name string) (string, error) {
	data, err := b.Bytes()
	if err != nil {
		return "", err
	}
	p := filepath.Join(dir, name)
	return p, os.WriteFile(p, data, 0o600)
}

// Grid returns sheetData rows for a rows x cols grid of numbers (cell
// value row*1000+col), for scaling and memory tests: about 35 B of XML
// a cell, as Excel writes them.
func Grid(rows, cols int) string {
	var b strings.Builder
	b.Grow(rows * cols * 36)
	for r := 1; r <= rows; r++ {
		fmt.Fprintf(&b, `<row r="%d">`, r)
		for c := 1; c <= cols; c++ {
			fmt.Fprintf(&b, `<c r="%s%d"><v>%d</v></c>`, colName(c), r, r*1000+c)
		}
		b.WriteString(`</row>`)
	}
	return b.String()
}

// colName is the letters of 1-based column n. NEEDS-EXPORT: as above,
// xlsxtest cannot import formula (import cycle), so this stays until
// a cycle-free home for ColumnName lands.
func colName(n int) string {
	s := ""
	for n > 0 {
		n--
		s = string(rune('A'+n%26)) + s
		n /= 26
	}
	return s
}

// Parts returns the members of an .xlsx's bytes by name.
func Parts(data []byte) (map[string][]byte, error) {
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, err
	}
	out := map[string][]byte{}
	for _, f := range zr.File {
		rc, err := f.Open()
		if err != nil {
			return nil, err
		}
		var b bytes.Buffer
		_, err = b.ReadFrom(rc)
		if cerr := rc.Close(); err == nil {
			err = cerr
		}
		if err != nil {
			return nil, err
		}
		out[f.Name] = b.Bytes()
	}
	return out, nil
}

// Skip runs f and skips t when f panics with a stub's
// "unimplemented: chunk N" message (the chunk plan's skeleton), so a
// spec written against a stub skips until its chunk lands. A subtest
// (t.Run) runs in its own goroutine: wrap its body too. Any other
// panic is re-raised.
func Skip(t interface {
	Helper()
	Skip(args ...any)
}, f func()) {
	t.Helper()
	defer func() {
		if r := recover(); r != nil {
			if s, ok := r.(string); ok && strings.HasPrefix(s, "unimplemented: ") {
				t.Skip(s)
			}
			panic(r)
		}
	}()
	f()
}
