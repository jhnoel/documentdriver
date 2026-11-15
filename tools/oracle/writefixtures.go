package main

import (
	"archive/zip"
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/jhnoel/documentdriver/internal/ooxml/ctypes"
)

// The write fixtures are small packages authored here for the write
// baseline: the mode-sensitive content the corpus has too little of
// (foreign authors' revisions, the plan author's own pending insertions,
// comment ranges across runs and paragraphs, paragraphs without paraIds,
// tables with tracked rows, several sections with header and footer
// slots). `oracle write fixtures` rebuilds them byte for byte (stored zip
// members, fixed times); TestWriteFixtures keeps the committed files equal
// to the builder.

// writeFixtureDir is where the fixtures live, relative to the root.
const writeFixtureDir = "tools/oracle/testdata/write"

// setWrite is the input set of the write fixtures.
const setWrite = "write"

const wNS = `xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main" ` +
	`xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships" ` +
	`xmlns:w14="http://schemas.microsoft.com/office/word/2010/wordml" ` +
	`xmlns:w15="http://schemas.microsoft.com/office/word/2012/wordml" ` +
	`xmlns:m="http://schemas.openxmlformats.org/officeDocument/2006/math" ` +
	`xmlns:mc="http://schemas.openxmlformats.org/markup-compatibility/2006" mc:Ignorable="w14 w15"`

const xmlDecl = `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>` + "\n"

// fixturePart is one zip member.
type fixturePart struct {
	name, ctype, body string
}

// The fixture files.
var writeFixtureNames = []string{"badroot.docx", "dirty.docx", "dupname.docx", "lazy.docx", "malformed.docx", "mixed.docx", "sections.docx"}

// writeFixtures returns every fixture by file name.
func writeFixtures() map[string][]byte {
	return map[string][]byte{
		"mixed.docx":     buildPackage(mixedParts()),
		"sections.docx":  buildPackage(sectionParts()),
		"dirty.docx":     buildPackage(dirtyParts()),
		"lazy.docx":      lazyPackage(),
		"dupname.docx":   dupPackage(),
		"badroot.docx":   badRootPackage(),
		"malformed.docx": malformedPackage(),
	}
}

// buildPackage zips the parts with [Content_Types].xml and the package
// relationships, stored and with a fixed time so the bytes reproduce.
func buildPackage(parts []fixturePart, docRels []string) []byte {
	var ct strings.Builder
	ct.WriteString(xmlDecl + `<Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types">`)
	ct.WriteString(`<Default Extension="rels" ContentType="` + ctypes.CTPackageRelationships + `"/>`)
	ct.WriteString(`<Default Extension="xml" ContentType="` + ctypes.CTApplicationXml + `"/>`)
	for _, p := range parts {
		fmt.Fprintf(&ct, `<Override PartName="/%s" ContentType="%s"/>`, p.name, p.ctype)
	}
	ct.WriteString(`</Types>`)
	rels := xmlDecl + `<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">` +
		`<Relationship Id="rId1" Type="` + ctypes.RelOfficeDocument + `" Target="word/document.xml"/></Relationships>`
	var dr strings.Builder
	dr.WriteString(xmlDecl + `<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">`)
	for i, r := range docRels {
		if r == "" {
			continue
		}
		typ, target, _ := strings.Cut(r, " ")
		fmt.Fprintf(&dr, `<Relationship Id="rId%d" Type="%s" Target="%s"/>`, i+1, typ, target)
	}
	dr.WriteString(`</Relationships>`)
	all := append([]fixturePart{
		{name: "[Content_Types].xml", body: ct.String()},
		{name: "_rels/.rels", body: rels},
		{name: "word/_rels/document.xml.rels", body: dr.String()},
	}, parts...)
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	when := time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC)
	for _, p := range all {
		w, err := zw.CreateHeader(&zip.FileHeader{Name: p.name, Method: zip.Store, Modified: when})
		if err != nil {
			panic(err) // unreachable: writing to a bytes.Buffer
		}
		if _, err := w.Write([]byte(p.body)); err != nil {
			panic(err) // unreachable: writing to a bytes.Buffer
		}
	}
	if err := zw.Close(); err != nil {
		panic(err) // unreachable: writing to a bytes.Buffer
	}
	return buf.Bytes()
}

// Small builders for WordprocessingML text.

// para builds a w:p element with optional paraId and paragraph properties.
func para(id, ppr string, content ...string) string {
	attr := ""
	if id != "" {
		attr = ` w14:paraId="` + id + `" w14:textId="77777777"`
	}
	if ppr != "" {
		ppr = "<w:pPr>" + ppr + "</w:pPr>"
	}
	return "<w:p" + attr + ">" + ppr + strings.Join(content, "") + "</w:p>"
}

// run builds a simple w:r run containing text.
func run(text string) string { return runP("", text) }

// runP builds a w:r run with optional run properties containing text.
func runP(rpr, text string) string {
	if rpr != "" {
		rpr = "<w:rPr>" + rpr + "</w:rPr>"
	}
	return `<w:r>` + rpr + `<w:t xml:space="preserve">` + text + `</w:t></w:r>`
}

// ins builds a w:ins revision element by id and author around content.
func ins(id int, author, content string) string {
	return fmt.Sprintf(`<w:ins w:id="%d" w:author="%s" w:date="1999-12-31T00:00:00Z">%s</w:ins>`, id, author, content)
}

// del builds a w:del revision element by id and author deleting text.
func del(id int, author, text string) string {
	return fmt.Sprintf(`<w:del w:id="%d" w:author="%s" w:date="1999-12-31T00:00:00Z"><w:r><w:delText xml:space="preserve">%s</w:delText></w:r></w:del>`, id, author, text)
}

// cell builds a w:tc table cell with width, properties and paragraphs.
func cell(width int, tcpr string, paras ...string) string {
	return fmt.Sprintf(`<w:tc><w:tcPr><w:tcW w:w="%d" w:type="dxa"/>%s</w:tcPr>%s</w:tc>`, width, tcpr, strings.Join(paras, ""))
}

const stylesXML = xmlDecl + `<w:styles ` + wNS + `>` +
	`<w:docDefaults><w:rPrDefault><w:rPr><w:sz w:val="22"/></w:rPr></w:rPrDefault><w:pPrDefault/></w:docDefaults>` +
	`<w:style w:type="paragraph" w:default="1" w:styleId="Normal"><w:name w:val="Normal"/></w:style>` +
	`<w:style w:type="paragraph" w:styleId="Heading1"><w:name w:val="heading 1"/><w:basedOn w:val="Normal"/><w:next w:val="Normal"/><w:pPr><w:outlineLvl w:val="0"/></w:pPr><w:rPr><w:b/><w:sz w:val="32"/></w:rPr></w:style>` +
	`<w:style w:type="paragraph" w:styleId="ListParagraph"><w:name w:val="List Paragraph"/><w:basedOn w:val="Normal"/><w:pPr><w:ind w:left="720"/></w:pPr></w:style>` +
	`<w:style w:type="paragraph" w:styleId="Quote"><w:name w:val="Quote"/><w:basedOn w:val="Normal"/><w:rPr><w:i/></w:rPr></w:style>` +
	`<w:style w:type="paragraph" w:styleId="Header"><w:name w:val="header"/><w:basedOn w:val="Normal"/></w:style>` +
	`<w:style w:type="paragraph" w:styleId="Footer"><w:name w:val="footer"/><w:basedOn w:val="Normal"/></w:style>` +
	`<w:style w:type="character" w:default="1" w:styleId="DefaultParagraphFont"><w:name w:val="Default Paragraph Font"/></w:style>` +
	`<w:style w:type="table" w:default="1" w:styleId="TableNormal"><w:name w:val="Normal Table"/></w:style>` +
	`<w:style w:type="table" w:styleId="TableGrid"><w:name w:val="Table Grid"/><w:basedOn w:val="TableNormal"/>` +
	`<w:tblPr><w:tblBorders><w:top w:val="single" w:sz="4" w:space="0" w:color="auto"/><w:bottom w:val="single" w:sz="4" w:space="0" w:color="auto"/></w:tblBorders></w:tblPr></w:style>` +
	`</w:styles>`

const settingsXML = xmlDecl + `<w:settings ` + wNS + `><w:defaultTabStop w:val="720"/></w:settings>`

const numberingXML = xmlDecl + `<w:numbering ` + wNS + `>` +
	`<w:abstractNum w:abstractNumId="0"><w:multiLevelType w:val="hybridMultilevel"/>` +
	`<w:lvl w:ilvl="0"><w:start w:val="1"/><w:numFmt w:val="decimal"/><w:lvlText w:val="%1."/><w:lvlJc w:val="left"/><w:pPr><w:ind w:left="720" w:hanging="360"/></w:pPr></w:lvl>` +
	`<w:lvl w:ilvl="1"><w:start w:val="1"/><w:numFmt w:val="lowerLetter"/><w:lvlText w:val="%2)"/><w:lvlJc w:val="left"/><w:pPr><w:ind w:left="1440" w:hanging="360"/></w:pPr></w:lvl>` +
	`</w:abstractNum>` +
	`<w:abstractNum w:abstractNumId="1"><w:multiLevelType w:val="hybridMultilevel"/>` +
	`<w:lvl w:ilvl="0"><w:start w:val="1"/><w:numFmt w:val="bullet"/><w:lvlText w:val="` + "•" + `"/><w:lvlJc w:val="left"/><w:pPr><w:ind w:left="720" w:hanging="360"/></w:pPr></w:lvl>` +
	`</w:abstractNum>` +
	`<w:num w:numId="1"><w:abstractNumId w:val="0"/></w:num>` +
	`<w:num w:numId="2"><w:abstractNumId w:val="1"/></w:num>` +
	`</w:numbering>`

// mixedParts is mixed.docx: one section; revisions by two foreign authors
// and pending insertions by the plan author (writeAuthor), property
// changes, comments over runs and across paragraphs with a reply, body
// bookmarks, inline and display equations, a line break, numbered and
// bulleted lists, a table with a horizontal and a vertical merge and
// tracked row insertion and deletion, and paragraphs with and without
// paraIds.
func mixedParts() ([]fixturePart, []string) {
	listPr := func(num, lvl int) string {
		return fmt.Sprintf(`<w:pStyle w:val="ListParagraph"/><w:numPr><w:ilvl w:val="%d"/><w:numId w:val="%d"/></w:numPr>`, lvl, num)
	}
	frac := `<m:f><m:num><m:r><m:t>a</m:t></m:r></m:num><m:den><m:r><m:t>b</m:t></m:r></m:den></m:f>`
	sq := `<m:sSup><m:e><m:r><m:t>x</m:t></m:r></m:e><m:sup><m:r><m:t>2</m:t></m:r></m:sup></m:sSup>`
	trIns := `<w:trPr><w:ins w:id="40" w:author="Alice" w:date="1999-12-31T00:00:00Z"/></w:trPr>`
	trDel := `<w:trPr><w:del w:id="41" w:author="Bob" w:date="1999-12-31T00:00:00Z"/></w:trPr>`
	table := `<w:tbl><w:tblPr><w:tblStyle w:val="TableGrid"/><w:tblW w:w="0" w:type="auto"/><w:tblLook w:val="04A0"/></w:tblPr>` +
		`<w:tblGrid><w:gridCol w:w="3000"/><w:gridCol w:w="3000"/><w:gridCol w:w="3000"/></w:tblGrid>` +
		`<w:tr w14:paraId="20000000" w14:textId="77777777">` +
		cell(3000, "", para("20000001", "", run("Name"))) + cell(3000, "", para("20000002", "", run("Role"))) + cell(3000, "", para("20000003", "", run("Notes"))) + `</w:tr>` +
		`<w:tr>` + cell(6000, `<w:gridSpan w:val="2"/>`, para("20000004", "", run("Merged across two"))) +
		cell(3000, `<w:vMerge w:val="restart"/>`, para("20000005", "", run("Merged down"))) + `</w:tr>` +
		`<w:tr>` + cell(3000, "", para("20000006", "", run("Ada"))) + cell(3000, "", para("", "", run("Analyst"))) +
		cell(3000, `<w:vMerge/>`, para("20000008", "")) + `</w:tr>` +
		`<w:tr>` + trIns + cell(3000, "", para("20000009", "", ins(42, "Alice", run("Inserted row")))) +
		cell(3000, "", para("2000000A", "", ins(43, "Alice", run("by Alice")))) + cell(3000, "", para("2000000B", "")) + `</w:tr>` +
		`<w:tr>` + trDel + cell(3000, "", para("2000000C", "", del(44, "Bob", "Deleted row"))) +
		cell(3000, "", para("2000000D", "", del(45, "Bob", "by Bob"))) + cell(3000, "", para("2000000E", "")) + `</w:tr>` +
		`</w:tbl>`
	body := strings.Join([]string{
		para("10000001", `<w:pStyle w:val="Heading1"/>`, run("Mixed fixture")),
		para("10000002", "", run("Alpha beta "), runP("<w:b/>", "gamma"), run(" beta delta epsilon.")),
		para("", "", run("Foreign revisions keep "), ins(1, "Alice", run("inserted words")), run(" and "),
			del(2, "Bob", "removed words"), run(" around the end.")),
		para("10000004", "", run("Own pending: "), ins(3, writeAuthor, run("my insertion")), run(" then a tail.")),
		para("10000005", `<w:jc w:val="center"/><w:pPrChange w:id="4" w:author="Alice" w:date="1999-12-31T00:00:00Z"><w:pPr/></w:pPrChange>`,
			run("Property changes on "),
			`<w:r><w:rPr><w:i/><w:rPrChange w:id="5" w:author="Bob" w:date="1999-12-31T00:00:00Z"><w:rPr/></w:rPrChange></w:rPr><w:t>formatted</w:t></w:r>`,
			run(" text.")),
		para("10000006", "", run("Comment "), `<w:commentRangeStart w:id="0"/><w:commentRangeStart w:id="2"/>`, run("over "), runP("<w:b/>", "two runs"),
			`<w:commentRangeEnd w:id="0"/><w:commentRangeEnd w:id="2"/><w:r><w:commentReference w:id="0"/></w:r><w:r><w:commentReference w:id="2"/></w:r>`, run(" and "),
			`<w:commentRangeStart w:id="1"/>`, run("a range that crosses")),
		para("", "", run("into the next paragraph"), `<w:commentRangeEnd w:id="1"/><w:r><w:commentReference w:id="1"/></w:r>`, run(" here.")),
		para("10000008", "", run("Bookmark "), `<w:bookmarkStart w:id="0" w:name="Target"/>`, run("target words"),
			`<w:bookmarkEnd w:id="0"/>`, run(" in a sentence.")),
		`<w:bookmarkStart w:id="1" w:name="Whole"/>` + para("10000009", "", run("A whole bookmarked paragraph.")) + `<w:bookmarkEnd w:id="1"/>`,
		para("1000000A", "", run("Inline math "), `<m:oMath>`+sq+`</m:oMath>`, run(" between words.")),
		para("1000000B", "", `<m:oMathPara><m:oMath>`+frac+`</m:oMath></m:oMathPara>`),
		para("1000000C", "", run("Line one"), `<w:r><w:br/></w:r>`, run("line two of the same paragraph.")),
		para("1000000D", listPr(1, 0), run("First numbered item")),
		para("1000000E", listPr(1, 1), run("Nested lettered item")),
		para("", listPr(1, 0), run("Second numbered item")),
		para("10000010", listPr(2, 0), run("Bullet item")),
		para("10000011", "", run("Before the table.")),
		table,
		para("10000012", "", run("After the table, repeated words: repeat repeat repeat.")),
		para("", `<w:pStyle w:val="Quote"/>`, run("Quoted paragraph without a paraId.")),
		para("10000015", "", run("Two equations "), `<m:oMath>`+sq+`</m:oMath>`, run(" and "), `<m:oMath>`+frac+`</m:oMath>`, run(" here.")),
		para("10000016", "", run("A tracked equation "), `<w:ins w:id="60" w:author="Alice" w:date="1999-12-31T00:00:00Z"><m:oMath>`+sq+`</m:oMath></w:ins>`, run(" inside an insertion.")),
		para("10000017", "", run("Content control: "), `<w:sdt><w:sdtPr><w:id w:val="5"/></w:sdtPr><w:sdtContent><w:r><w:t>controlled text</w:t></w:r></w:sdtContent></w:sdt>`, run(" after it.")),
		para("10000018", "", run("Field before "), `<w:r><w:fldChar w:fldCharType="begin"/></w:r><w:r><w:instrText xml:space="preserve"> DATE </w:instrText></w:r><w:r><w:fldChar w:fldCharType="separate"/></w:r><w:r><w:t>today</w:t></w:r><w:r><w:fldChar w:fldCharType="end"/></w:r>`, run(" field after.")),
		para("10000014", "", run("Final paragraph.")),
		`<w:sectPr><w:pgSz w:w="12240" w:h="15840"/><w:pgMar w:top="1440" w:right="1440" w:bottom="1440" w:left="1440" w:header="720" w:footer="720" w:gutter="0"/></w:sectPr>`,
	}, "")
	doc := xmlDecl + `<w:document ` + wNS + `><w:body>` + body + `</w:body></w:document>`
	cpara := func(id, text string) string {
		return `<w:p w14:paraId="` + id + `" w14:textId="77777777"><w:r><w:t xml:space="preserve">` + text + `</w:t></w:r></w:p>`
	}
	comments := xmlDecl + `<w:comments ` + wNS + `>` +
		`<w:comment w:id="0" w:author="Alice" w:date="1999-12-31T00:00:00Z" w:initials="A">` + cpara("0C000000", "Check these runs.") + `</w:comment>` +
		`<w:comment w:id="1" w:author="Bob" w:date="1999-12-31T00:00:00Z" w:initials="B">` + cpara("0C000001", "Spans two paragraphs.") + `</w:comment>` +
		`<w:comment w:id="2" w:author="Bob" w:date="1999-12-31T00:00:00Z" w:initials="B">` + cpara("0C000002", "A reply.") + `</w:comment>` +
		`</w:comments>`
	ext := xmlDecl + `<w15:commentsEx ` + wNS + `>` +
		`<w15:commentEx w15:paraId="0C000000" w15:done="0"/>` +
		`<w15:commentEx w15:paraId="0C000001" w15:done="1"/>` +
		`<w15:commentEx w15:paraId="0C000002" w15:paraIdParent="0C000000" w15:done="0"/>` +
		`</w15:commentsEx>`
	parts := []fixturePart{
		{"word/document.xml", ctypes.CTWordprocessingmlDocumentMain, doc},
		{"word/styles.xml", ctypes.CTWordprocessingmlStyles, stylesXML},
		{"word/settings.xml", ctypes.CTWordprocessingmlSettings, settingsXML},
		{"word/numbering.xml", ctypes.CTWordprocessingmlNumbering, numberingXML},
		{"word/comments.xml", ctypes.CTWordprocessingmlComments, comments},
		{"word/commentsExtended.xml", ctypes.CTWordprocessingmlCommentsExtended, ext},
	}
	rels := []string{
		ctypes.RelStyles + " styles.xml",
		ctypes.RelSettings + " settings.xml",
		ctypes.RelNumbering + " numbering.xml",
		ctypes.RelComments + " comments.xml",
		ctypes.RelCommentsExtended + " commentsExtended.xml",
	}
	return parts, rels
}

// sectionParts is sections.docx: three sections. Section 1 has default
// and first-page headers and a default footer with a PAGE field and
// titlePg; section 2 (continuous, two columns, roman page numbers
// restarting at 1) inherits section 1's stories; section 3 (landscape)
// has its own default header and an even header while even/odd headers
// are off. One header holds a tracked insertion by a foreign author.
func sectionParts() ([]fixturePart, []string) {
	ref := func(kind, typ, id string) string {
		return `<w:` + kind + `Reference w:type="` + typ + `" r:id="` + id + `"/>`
	}
	pg := `<w:pgSz w:w="12240" w:h="15840"/><w:pgMar w:top="1440" w:right="1440" w:bottom="1440" w:left="1440" w:header="720" w:footer="720" w:gutter="0"/>`
	sect1 := `<w:sectPr>` + ref("header", "default", "rId4") + ref("header", "first", "rId5") + ref("footer", "default", "rId6") +
		pg + `<w:titlePg/></w:sectPr>`
	sect2 := `<w:sectPr><w:type w:val="continuous"/>` + pg + `<w:pgNumType w:fmt="lowerRoman" w:start="1"/><w:cols w:num="2" w:space="720"/></w:sectPr>`
	sect3 := `<w:sectPr>` + ref("header", "default", "rId7") + ref("header", "even", "rId8") +
		`<w:type w:val="nextPage"/><w:pgSz w:w="15840" w:h="12240" w:orient="landscape"/><w:pgMar w:top="1080" w:right="1080" w:bottom="1080" w:left="1080" w:header="720" w:footer="720" w:gutter="0"/>` +
		`<w:sectPrChange w:id="70" w:author="Alice" w:date="1999-12-31T00:00:00Z"><w:sectPr><w:pgSz w:w="12240" w:h="15840"/></w:sectPr></w:sectPrChange></w:sectPr>`
	body := strings.Join([]string{
		para("30000001", `<w:pStyle w:val="Heading1"/>`, run("Sections fixture")),
		para("30000002", "", run("Section one body text with a few words.")),
		para("30000003", sect1, run("Last paragraph of section one.")),
		para("30000004", "", run("Section two starts here and runs in two columns.")),
		para("", sect2, run("Section two ends without a paraId.")),
		para("30000006", "", run("Section three is landscape.")),
		`<w:tbl><w:tblPr><w:tblStyle w:val="TableGrid"/><w:tblW w:w="0" w:type="auto"/></w:tblPr><w:tblGrid><w:gridCol w:w="3000"/><w:gridCol w:w="3000"/><w:gridCol w:w="3000"/></w:tblGrid>` +
			`<w:tr>` + cell(3000, "", para("3A000001", "", run("A1"))) + cell(3000, "", para("3A000002", "", run("B1"))) + cell(3000, `<w:vMerge w:val="restart"/>`, para("3A000003", "", run("C1"))) + `</w:tr>` +
			`<w:tr>` + cell(6000, `<w:gridSpan w:val="2"/>`, para("3A000004", "", run("A2 spans two"))) + cell(3000, `<w:vMerge/>`, para("3A000005", "")) + `</w:tr>` +
			`</w:tbl>`,
		para("30000008", "", run("After the clean table.")),
		para("30000007", "", run("The last paragraph of the document.")),
		sect3,
	}, "")
	doc := xmlDecl + `<w:document ` + wNS + `><w:body>` + body + `</w:body></w:document>`
	story := func(root, content string) string {
		return xmlDecl + `<w:` + root + ` ` + wNS + `>` + content + `</w:` + root + `>`
	}
	field := `<w:r><w:fldChar w:fldCharType="begin"/></w:r><w:r><w:instrText xml:space="preserve"> PAGE </w:instrText></w:r>` +
		`<w:r><w:fldChar w:fldCharType="separate"/></w:r><w:r><w:t>1</w:t></w:r><w:r><w:fldChar w:fldCharType="end"/></w:r>`
	hp := `<w:pStyle w:val="Header"/>`
	parts := []fixturePart{
		{"word/document.xml", ctypes.CTWordprocessingmlDocumentMain, doc},
		{"word/styles.xml", ctypes.CTWordprocessingmlStyles, stylesXML},
		{"word/settings.xml", ctypes.CTWordprocessingmlSettings, settingsXML},
		{"word/header1.xml", ctypes.CTWordprocessingmlHeader, story("hdr", para("40000001", hp, run("Default header one"), ins(50, "Alice", run(" (revised)"))))},
		{"word/header2.xml", ctypes.CTWordprocessingmlHeader, story("hdr", para("40000002", hp, run("First page header")))},
		{"word/footer1.xml", ctypes.CTWordprocessingmlFooter, story("ftr", para("40000003", `<w:pStyle w:val="Footer"/>`, run("Page "), field))},
		{"word/header3.xml", ctypes.CTWordprocessingmlHeader, story("hdr", para("40000004", hp, run("Section three header")))},
		{"word/header4.xml", ctypes.CTWordprocessingmlHeader, story("hdr", para("40000005", hp, run("Even header")))},
	}
	rels := []string{
		ctypes.RelStyles + " styles.xml",
		ctypes.RelSettings + " settings.xml",
		"", // rId3 unused, keeps the header ids readable
		ctypes.RelHeader + " header1.xml",
		ctypes.RelHeader + " header2.xml",
		ctypes.RelFooter + " footer1.xml",
		ctypes.RelHeader + " header3.xml",
		ctypes.RelHeader + " header4.xml",
	}
	return parts, rels
}

// saveWriteFixtures writes the fixtures under the root.
func saveWriteFixtures(root string) error {
	dir := filepath.Join(root, filepath.FromSlash(writeFixtureDir))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	for name, b := range writeFixtures() {
		if err := os.WriteFile(filepath.Join(dir, name), b, 0o644); err != nil {
			return err
		}
	}
	return nil
}

// dirtyParts is dirty.docx: a document whose input already has integrity
// and schema findings, for the dirty case family (edits must save and
// leave pre-existing defects alone): run properties out of schema order,
// an unknown WordprocessingML element, transitional w:hint markup, a
// style based on a style that does not exist, a numbering definition no
// paragraph uses and a comment no range or reference anchors.
func dirtyParts() ([]fixturePart, []string) {
	body := strings.Join([]string{
		para("50000001", `<w:pStyle w:val="Heading1"/>`, run("Dirty fixture")),
		para("50000002", "", runP(`<w:sz w:val="28"/><w:b/>`, "Out of order run properties"), run(" then plain text here.")),
		para("50000003", "", `<w:bogusElement w:val="1"/>`, run("Unknown element before this text.")),
		para("50000004", "", runP(`<w:rFonts w:hint="cs"/>`, "Hinted fonts on a run"), run(" and more words.")),
		para("50000005", `<w:pStyle w:val="Orphan"/>`, run("Paragraph in a style based on a missing style.")),
		para("50000006", "", run("A clean paragraph at the end.")),
		`<w:sectPr><w:pgSz w:w="12240" w:h="15840"/><w:pgMar w:top="1440" w:right="1440" w:bottom="1440" w:left="1440" w:header="720" w:footer="720" w:gutter="0"/></w:sectPr>`,
	}, "")
	doc := xmlDecl + `<w:document ` + wNS + `><w:body>` + body + `</w:body></w:document>`
	styles := strings.Replace(stylesXML, `</w:styles>`,
		`<w:style w:type="paragraph" w:styleId="Orphan"><w:name w:val="Orphan"/><w:basedOn w:val="NoSuchParent"/></w:style></w:styles>`, 1)
	comments := xmlDecl + `<w:comments ` + wNS + `><w:comment w:id="7" w:author="Ghost" w:date="1999-12-31T00:00:00Z">` +
		`<w:p w14:paraId="C0000007" w14:textId="77777777"><w:r><w:t>Unanchored</w:t></w:r></w:p></w:comment></w:comments>`
	parts := []fixturePart{
		{"word/document.xml", ctypes.CTWordprocessingmlDocumentMain, doc},
		{"word/styles.xml", ctypes.CTWordprocessingmlStyles, styles},
		{"word/settings.xml", ctypes.CTWordprocessingmlSettings, settingsXML},
		{"word/numbering.xml", ctypes.CTWordprocessingmlNumbering, numberingXML},
		{"word/comments.xml", ctypes.CTWordprocessingmlComments, comments},
	}
	rels := []string{
		ctypes.RelStyles + " styles.xml",
		ctypes.RelSettings + " settings.xml",
		ctypes.RelNumbering + " numbering.xml",
		ctypes.RelComments + " comments.xml",
	}
	return parts, rels
}

// lazyPackage is lazy.docx: a small clean document plus one stored
// member, word/unused.xml, that no relationship names and whose CRC does
// not match its bytes. A lazy reader never touches it (contract.md §10);
// docxdriver reads every member at open.
func lazyPackage() []byte {
	body := strings.Join([]string{
		para("60000001", `<w:pStyle w:val="Heading1"/>`, run("Lazy fixture")),
		para("60000002", "", run("Only this paragraph and its neighbours are read.")),
		para("60000003", "", run("The last paragraph.")),
		`<w:sectPr><w:pgSz w:w="12240" w:h="15840"/></w:sectPr>`,
	}, "")
	doc := xmlDecl + `<w:document ` + wNS + `><w:body>` + body + `</w:body></w:document>`
	clean := buildPackage([]fixturePart{
		{"word/document.xml", ctypes.CTWordprocessingmlDocumentMain, doc},
		{"word/styles.xml", ctypes.CTWordprocessingmlStyles, stylesXML},
	}, []string{ctypes.RelStyles + " styles.xml"})
	zr, err := zip.NewReader(bytes.NewReader(clean), int64(len(clean)))
	if err != nil {
		panic(err) // unreachable: buildPackage writes a valid zip
	}
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for _, f := range zr.File {
		if err := zw.Copy(f); err != nil {
			panic(err) // unreachable: writing to a bytes.Buffer
		}
	}
	junk := []byte(xmlDecl + `<unused/>`)
	w, err := zw.CreateRaw(&zip.FileHeader{Name: "word/unused.xml", Method: zip.Store,
		Modified: time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC), CRC32: 0xDEADBEEF,
		CompressedSize64: uint64(len(junk)), UncompressedSize64: uint64(len(junk))})
	if err != nil {
		panic(err) // unreachable: writing to a bytes.Buffer
	}
	if _, err := w.Write(junk); err != nil {
		panic(err) // unreachable: writing to a bytes.Buffer
	}
	if err := zw.Close(); err != nil {
		panic(err) // unreachable: writing to a bytes.Buffer
	}
	return buf.Bytes()
}

// smallDoc is a clean three-paragraph package for the opc fixtures.
func smallDoc(prefix, title string) []byte {
	body := strings.Join([]string{
		para(prefix+"0001", `<w:pStyle w:val="Heading1"/>`, run(title)),
		para(prefix+"0002", "", run("Only this paragraph and its neighbours are read.")),
		para(prefix+"0003", "", run("The last paragraph.")),
		`<w:sectPr><w:pgSz w:w="12240" w:h="15840"/></w:sectPr>`,
	}, "")
	doc := xmlDecl + `<w:document ` + wNS + `><w:body>` + body + `</w:body></w:document>`
	return buildPackage([]fixturePart{
		{"word/document.xml", ctypes.CTWordprocessingmlDocumentMain, doc},
		{"word/styles.xml", ctypes.CTWordprocessingmlStyles, stylesXML},
	}, []string{ctypes.RelStyles + " styles.xml"})
}

// rewritePackage copies a package's members, letting edit change a
// member's bytes (nil keeps them) and extra append members.
func rewritePackage(pkg []byte, edit func(name string, b []byte) []byte, extra ...fixturePart) []byte {
	zr, err := zip.NewReader(bytes.NewReader(pkg), int64(len(pkg)))
	if err != nil {
		panic(err) // unreachable: our own package
	}
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	when := time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC)
	add := func(name string, b []byte) {
		w, err := zw.CreateHeader(&zip.FileHeader{Name: name, Method: zip.Store, Modified: when})
		if err != nil {
			panic(err) // unreachable: writing to a bytes.Buffer
		}
		if _, err := w.Write(b); err != nil {
			panic(err) // unreachable: writing to a bytes.Buffer
		}
	}
	for _, f := range zr.File {
		rc, err := f.Open()
		if err != nil {
			panic(err) // unreachable: our own package
		}
		b, err := io.ReadAll(rc)
		rc.Close()
		if err != nil {
			panic(err) // unreachable: our own package
		}
		if e := edit(f.Name, b); e != nil {
			b = e
		}
		add(f.Name, b)
	}
	for _, p := range extra {
		add(p.name, []byte(p.body))
	}
	if err := zw.Close(); err != nil {
		panic(err) // unreachable: writing to a bytes.Buffer
	}
	return buf.Bytes()
}

// dupPackage is dupname.docx: a second word/styles.xml member after the
// first (P4a: the first entry wins and the later one is copied through).
func dupPackage() []byte {
	return rewritePackage(smallDoc("7000", "Duplicate part name"), func(string, []byte) []byte { return nil },
		fixturePart{name: "word/styles.xml", body: xmlDecl + `<w:styles ` + wNS + `/>`})
}

// badRootPackage is badroot.docx: [Content_Types].xml whose root is not
// Types (P4a: read as empty and copied through; only an op that must
// register a part refuses, up front, with unreadable_target).
func badRootPackage() []byte {
	return rewritePackage(smallDoc("7100", "Content types root"), func(name string, b []byte) []byte {
		if name != "[Content_Types].xml" {
			return nil
		}
		return []byte(strings.NewReplacer("<Types ", "<Typez ", "</Types>", "</Typez>").Replace(string(b)))
	})
}

// malformedPackage is malformed.docx: a clean body whose comment
// extension, settings and numbering parts are not well-formed XML, for
// the refusal sites that read those parts (comments.rs, headers.rs,
// lists.rs: "malformed ...") and the dirty family (an op that does not
// need the part must not fail).
func malformedPackage() []byte {
	body := strings.Join([]string{
		para("72000001", `<w:pStyle w:val="Heading1"/>`, run("Malformed parts")),
		para("72000002", "", `<w:commentRangeStart w:id="0"/>`, run("Commented words"), `<w:commentRangeEnd w:id="0"/><w:r><w:commentReference w:id="0"/></w:r>`, run(" in a paragraph.")),
		para("72000003", "", run("The last paragraph.")),
		`<w:sectPr><w:pgSz w:w="12240" w:h="15840"/></w:sectPr>`,
	}, "")
	doc := xmlDecl + `<w:document ` + wNS + `><w:body>` + body + `</w:body></w:document>`
	comments := xmlDecl + `<w:comments ` + wNS + `><w:comment w:id="0" w:author="Alice" w:date="1999-12-31T00:00:00Z">` +
		`<w:p w14:paraId="0D000000" w14:textId="77777777"><w:r><w:t>Note</w:t></w:r></w:p></w:comment></w:comments>`
	broken := xmlDecl + `<w15:commentsEx ` + wNS + `><w15:commentEx w15:paraId="0D000000"`
	return buildPackage([]fixturePart{
		{"word/document.xml", ctypes.CTWordprocessingmlDocumentMain, doc},
		{"word/styles.xml", ctypes.CTWordprocessingmlStyles, stylesXML},
		{"word/settings.xml", ctypes.CTWordprocessingmlSettings, xmlDecl + `<w:settings ` + wNS + `><w:defaultTabStop`},
		{"word/numbering.xml", ctypes.CTWordprocessingmlNumbering, xmlDecl + `<w:numbering ` + wNS + `><w:num`},
		{"word/comments.xml", ctypes.CTWordprocessingmlComments, comments},
		{"word/commentsExtended.xml", ctypes.CTWordprocessingmlCommentsExtended, broken},
	}, []string{
		ctypes.RelStyles + " styles.xml",
		ctypes.RelSettings + " settings.xml",
		ctypes.RelNumbering + " numbering.xml",
		ctypes.RelComments + " comments.xml",
		ctypes.RelCommentsExtended + " commentsExtended.xml",
	})
}
