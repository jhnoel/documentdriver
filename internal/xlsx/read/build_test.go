package read

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/jhnoel/documentdriver/internal/cli"
	"github.com/jhnoel/documentdriver/internal/xlsx/model"
)

// Fixture workbooks, authored for these tests in the shape Excel writes.
// The package layout, content types and relationship types follow
// internal/xlsx/integrity/build_test.go and the experiment fixtures of
// internal/xlsx/experiments_test.go (whose provenance comment names the
// Excel-written source); the metadata part follows the dynamic-array
// metadata Excel writes (xl/metadata.xml with XLDAPR, as in the corpus
// file the formula package's function table cites for spills).
const (
	xmlDecl = `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>` + "\n"
	nsMainT = "http://schemas.openxmlformats.org/spreadsheetml/2006/main"
	xmlnsS  = `xmlns="` + nsMainT + `" xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships"`
	relNS   = "http://schemas.openxmlformats.org/officeDocument/2006/relationships/"
	ctNS    = "application/vnd.openxmlformats-officedocument.spreadsheetml."
	pkgRels = `<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">`
)

// dynamicMetadata is a metadata part whose cellMetadata block 1 (cm="1")
// is a dynamic array and block 2 (cm="2") is another kind of metadata.
const dynamicMetadata = xmlDecl + `<metadata xmlns="` + nsMainT + `" xmlns:xda="http://schemas.microsoft.com/office/spreadsheetml/2017/dynamicarray">` +
	`<metadataTypes count="2"><metadataType name="XLDAPR" minSupportedVersion="120000" copy="1" pasteAll="1" pasteValues="1" merge="1" splitFirst="1" rowColShift="1" clearFormats="1" clearComments="1" assign="1" coerce="1" cellMeta="1"/>` +
	`<metadataType name="XLRICHVALUE" minSupportedVersion="120000" copy="1" pasteAll="1" pasteValues="1" merge="1" splitFirst="1" rowColShift="1" clearFormats="1" clearComments="1" assign="1" coerce="1"/></metadataTypes>` +
	`<futureMetadata name="XLDAPR" count="1"><bk><extLst><ext uri="{bdbb8cdc-fa1e-496e-a857-3c3f30c029c3}"><xda:dynamicArrayProperties fDynamic="1" fCollapsed="0"/></ext></extLst></bk></futureMetadata>` +
	`<cellMetadata count="2"><bk><rc t="1" v="0"/></bk><bk><rc t="2" v="0"/></bk></cellMetadata></metadata>`

// fx is a fixture workbook.
type fx struct {
	sheets []fxSheet
	// sst are the shared string items' inner XML ("<t>Name</t>"); nil for
	// no shared strings part. sstRaw replaces the whole part.
	sst    []string
	sstRaw string
	// numFmts are custom formats by id; xfs the numFmtId of each cellXfs
	// entry (nil: one General xf). stylesRaw replaces the whole part;
	// noStyles leaves it out.
	numFmts   map[int]string
	xfs       []int
	stylesRaw string
	noStyles  bool
	// metadata is the metadata part ("" for none).
	metadata string
	// workbookPr, definedNames and calcPr are raw workbook elements.
	workbookPr, definedNames, calcPr string
	// workbookRaw replaces workbook.xml; workbookType its content type.
	workbookRaw, workbookType string
	// omit lists parts to leave out of the zip (by zip name).
	omit []string
}

// fxSheet is one sheet of a fixture.
type fxSheet struct {
	name, state string
	// kind is "" (worksheet), "chartsheet" or "missing" (a relationship
	// to a part the zip lacks).
	kind string
	// head, rows and tail are the worksheet around and inside sheetData;
	// raw replaces the whole part.
	head, rows, tail, raw string
	// tables are table parts' contents, referenced from this sheet.
	tables []string
}

// table is a table part.
func table(name, ref string, cols ...string) string {
	var b strings.Builder
	fmt.Fprintf(&b, `%s<table %s id="1" name="%s" displayName="%s" ref="%s"><autoFilter ref="%s"/><tableColumns count="%d">`, xmlDecl, xmlnsS, name, name, ref, ref, len(cols))
	for i, c := range cols {
		fmt.Fprintf(&b, `<tableColumn id="%d" name="%s"/>`, i+1, c)
	}
	b.WriteString(`</tableColumns></table>`)
	return b.String()
}

func (f fx) parts() map[string]string {
	p := map[string]string{}
	var ct strings.Builder
	ct.WriteString(xmlDecl + `<Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types">` +
		`<Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/><Default Extension="xml" ContentType="application/xml"/>`)
	wbType := f.workbookType
	if wbType == "" {
		wbType = ctNS + "sheet.main+xml"
	}
	fmt.Fprintf(&ct, `<Override PartName="/xl/workbook.xml" ContentType="%s"/>`, wbType)
	p["_rels/.rels"] = xmlDecl + pkgRels + `<Relationship Id="rId1" Type="` + relNS + `officeDocument" Target="xl/workbook.xml"/></Relationships>`
	var wrels, sheets strings.Builder
	wrels.WriteString(xmlDecl + pkgRels)
	tableN := 0
	for i, sh := range f.sheets {
		id := i + 1
		state := ""
		if sh.state != "" {
			state = ` state="` + sh.state + `"`
		}
		fmt.Fprintf(&sheets, `<sheet name="%s" sheetId="%d"%s r:id="rId%d"/>`, escAttr(sh.name), id, state, id)
		switch sh.kind {
		case "chartsheet":
			fmt.Fprintf(&wrels, `<Relationship Id="rId%d" Type="%schartsheet" Target="chartsheets/sheet%d.xml"/>`, id, relNS, id)
			fmt.Fprintf(&ct, `<Override PartName="/xl/chartsheets/sheet%d.xml" ContentType="%schartsheet+xml"/>`, id, ctNS)
			p[fmt.Sprintf("xl/chartsheets/sheet%d.xml", id)] = xmlDecl + `<chartsheet ` + xmlnsS + `><sheetViews><sheetView workbookViewId="0"/></sheetViews></chartsheet>`
			continue
		case "missing":
			fmt.Fprintf(&wrels, `<Relationship Id="rId%d" Type="%sworksheet" Target="worksheets/sheet%d.xml"/>`, id, relNS, id)
			continue
		}
		fmt.Fprintf(&wrels, `<Relationship Id="rId%d" Type="%sworksheet" Target="worksheets/sheet%d.xml"/>`, id, relNS, id)
		fmt.Fprintf(&ct, `<Override PartName="/xl/worksheets/sheet%d.xml" ContentType="%sworksheet+xml"/>`, id, ctNS)
		body := sh.raw
		if body == "" {
			tail := sh.tail
			if len(sh.tables) > 0 {
				var tp strings.Builder
				fmt.Fprintf(&tp, `<tableParts count="%d">`, len(sh.tables))
				var srels strings.Builder
				srels.WriteString(xmlDecl + pkgRels)
				for j, tb := range sh.tables {
					tableN++
					fmt.Fprintf(&tp, `<tablePart r:id="rId%d"/>`, j+1)
					fmt.Fprintf(&srels, `<Relationship Id="rId%d" Type="%stable" Target="../tables/table%d.xml"/>`, j+1, relNS, tableN)
					fmt.Fprintf(&ct, `<Override PartName="/xl/tables/table%d.xml" ContentType="%stable+xml"/>`, tableN, ctNS)
					p[fmt.Sprintf("xl/tables/table%d.xml", tableN)] = tb
				}
				tp.WriteString(`</tableParts>`)
				srels.WriteString(`</Relationships>`)
				p[fmt.Sprintf("xl/worksheets/_rels/sheet%d.xml.rels", id)] = srels.String()
				tail += tp.String()
			}
			body = xmlDecl + `<worksheet ` + xmlnsS + `>` + sh.head + `<sheetData>` + sh.rows + `</sheetData>` + tail + `</worksheet>`
		}
		p[fmt.Sprintf("xl/worksheets/sheet%d.xml", id)] = body
	}
	n := len(f.sheets)
	if !f.noStyles {
		n++
		fmt.Fprintf(&wrels, `<Relationship Id="rId%d" Type="%sstyles" Target="styles.xml"/>`, n, relNS)
		fmt.Fprintf(&ct, `<Override PartName="/xl/styles.xml" ContentType="%sstyles+xml"/>`, ctNS)
		p["xl/styles.xml"] = f.styles()
	}
	if f.sst != nil || f.sstRaw != "" {
		n++
		fmt.Fprintf(&wrels, `<Relationship Id="rId%d" Type="%ssharedStrings" Target="sharedStrings.xml"/>`, n, relNS)
		fmt.Fprintf(&ct, `<Override PartName="/xl/sharedStrings.xml" ContentType="%ssharedStrings+xml"/>`, ctNS)
		body := f.sstRaw
		if body == "" {
			var b strings.Builder
			fmt.Fprintf(&b, `%s<sst %s count="%d" uniqueCount="%d">`, xmlDecl, xmlnsS, len(f.sst), len(f.sst))
			for _, si := range f.sst {
				b.WriteString("<si>" + si + "</si>")
			}
			b.WriteString(`</sst>`)
			body = b.String()
		}
		p["xl/sharedStrings.xml"] = body
	}
	if f.metadata != "" {
		n++
		fmt.Fprintf(&wrels, `<Relationship Id="rId%d" Type="%ssheetMetadata" Target="metadata.xml"/>`, n, relNS)
		fmt.Fprintf(&ct, `<Override PartName="/xl/metadata.xml" ContentType="%ssheetMetadata+xml"/>`, ctNS)
		p["xl/metadata.xml"] = f.metadata
	}
	wrels.WriteString(`</Relationships>`)
	p["xl/_rels/workbook.xml.rels"] = wrels.String()
	wb := f.workbookRaw
	if wb == "" {
		wb = xmlDecl + `<workbook ` + xmlnsS + `>` + f.workbookPr + `<sheets>` + sheets.String() + `</sheets>` + f.definedNames + f.calcPr + `</workbook>`
	}
	p["xl/workbook.xml"] = wb
	ct.WriteString(`</Types>`)
	p["[Content_Types].xml"] = ct.String()
	for _, name := range f.omit {
		delete(p, name)
	}
	return p
}

func (f fx) styles() string {
	if f.stylesRaw != "" {
		return f.stylesRaw
	}
	var b strings.Builder
	b.WriteString(xmlDecl + `<styleSheet ` + xmlnsS + `>`)
	if len(f.numFmts) > 0 {
		ids := make([]int, 0, len(f.numFmts))
		for id := range f.numFmts {
			ids = append(ids, id)
		}
		slices.Sort(ids)
		fmt.Fprintf(&b, `<numFmts count="%d">`, len(ids))
		for _, id := range ids {
			fmt.Fprintf(&b, `<numFmt numFmtId="%d" formatCode="%s"/>`, id, escAttr(f.numFmts[id]))
		}
		b.WriteString(`</numFmts>`)
	}
	b.WriteString(`<fonts count="1"><font/></fonts><fills count="2"><fill><patternFill patternType="none"/></fill><fill><patternFill patternType="gray125"/></fill></fills><borders count="1"><border/></borders>` +
		`<cellStyleXfs count="1"><xf numFmtId="0" fontId="0" fillId="0" borderId="0"/></cellStyleXfs>`)
	xfs := f.xfs
	if xfs == nil {
		xfs = []int{0}
	}
	fmt.Fprintf(&b, `<cellXfs count="%d">`, len(xfs))
	for _, id := range xfs {
		fmt.Fprintf(&b, `<xf numFmtId="%d" fontId="0" fillId="0" borderId="0" xfId="0" applyNumberFormat="1"/>`, id)
	}
	b.WriteString(`</cellXfs></styleSheet>`)
	return b.String()
}

// zip writes the fixture as a deflated zip, [Content_Types].xml first.
func (f fx) zip(t testing.TB) []byte {
	t.Helper()
	b, err := zipParts(f.parts())
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func zipParts(p map[string]string) ([]byte, error) {
	names := make([]string, 0, len(p))
	for name := range p {
		if name != "[Content_Types].xml" {
			names = append(names, name)
		}
	}
	slices.Sort(names)
	if _, ok := p["[Content_Types].xml"]; ok {
		names = append([]string{"[Content_Types].xml"}, names...)
	}
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for _, name := range names {
		w, err := zw.CreateHeader(&zip.FileHeader{Name: name, Method: zip.Deflate, Modified: time.Date(1980, 1, 1, 0, 0, 0, 0, time.UTC)})
		if err != nil {
			return nil, err
		}
		if _, err := w.Write([]byte(p[name])); err != nil {
			return nil, err
		}
	}
	if err := zw.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func escAttr(s string) string {
	return strings.NewReplacer("&", "&amp;", "<", "&lt;", `"`, "&quot;").Replace(s)
}

// skipIfUnimplemented runs probe and skips t when it panics with a stub's
// "unimplemented: chunk N" message, so these specs turn on by themselves
// as the chunks of a1-chunks.md land. Any other panic is re-raised.
func skipIfUnimplemented(t *testing.T, probe func()) {
	t.Helper()
	defer func() {
		if r := recover(); r != nil {
			if s, ok := r.(string); ok && strings.HasPrefix(s, "unimplemented: ") {
				t.Skip(s)
			}
			panic(r)
		}
	}()
	probe()
}

// probeAll probes every command: tests that run all three skip until the
// last of them lands.
func probeAll() {
	probeWorkbook()
	probeCells()
	probeFind()
}

// cellsJSON returns the cells of a result as compact JSON texts by key,
// and their keys in output order.
func cellsJSON(t *testing.T, res *CellsResult) (map[string]string, []string) {
	t.Helper()
	b, err := res.Cells.MarshalJSON()
	if err != nil {
		t.Fatal(err)
	}
	return objectMembers(t, b)
}

// objectMembers splits a JSON object into its members' compact texts and
// its keys in order; a duplicate key fails the test.
func objectMembers(t *testing.T, b []byte) (map[string]string, []string) {
	t.Helper()
	dec := json.NewDecoder(bytes.NewReader(b))
	if tok, err := dec.Token(); err != nil || tok != json.Delim('{') {
		t.Fatalf("not an object: %s", b)
	}
	out := map[string]string{}
	var keys []string
	for dec.More() {
		tok, err := dec.Token()
		if err != nil {
			t.Fatalf("%v in %s", err, b)
		}
		k := tok.(string)
		var raw json.RawMessage
		if err := dec.Decode(&raw); err != nil {
			t.Fatalf("%v in %s", err, b)
		}
		if _, dup := out[k]; dup {
			t.Fatalf("duplicate key %q in %s", k, b)
		}
		var c bytes.Buffer
		if err := json.Compact(&c, raw); err != nil {
			t.Fatal(err)
		}
		out[k] = c.String()
		keys = append(keys, k)
	}
	return out, keys
}

// encode is the result as the CLI prints it.
func encode(t *testing.T, v any) []byte {
	t.Helper()
	b, err := cli.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	if !json.Valid(b) {
		t.Fatalf("invalid JSON: %s", b)
	}
	return b
}

// noteKinds counts notes by kind.
func noteKinds(notes []model.Note) map[model.NoteKind]int {
	out := map[model.NoteKind]int{}
	for _, n := range notes {
		out[n.Kind]++
	}
	return out
}

// errCode is the contract code and path of err ("" for nil or a non-cli
// error).
func errCode(err error) (code, path string) {
	if e, ok := err.(*cli.Error); ok {
		return e.Code, e.Path
	}
	return "", ""
}
