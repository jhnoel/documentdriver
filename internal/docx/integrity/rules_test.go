package integrity

import (
	"bufio"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"unicode/utf16"
)

// ruleCases are packages written to break the rules the corpus never
// breaks. testdata/rules.golden holds docxdriver 0.5.2's findings on each
// (its examples/integrity.rs output on the files TestRulesWrite writes,
// paths reduced to the case name); TestRulesMatchDocxdriver compares.

const (
	relNS    = "http://schemas.openxmlformats.org/officeDocument/2006/relationships"
	relsOpen = `<?xml version="1.0" encoding="UTF-8"?><Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">`
	wOpen    = `<w:document xmlns:w="` + testW + `" xmlns:w14="` + testW14 + `" xmlns:r="` + relNS + `"><w:body>`
	wClose   = `</w:body></w:document>`
	ctOpen   = `<?xml version="1.0" encoding="UTF-8"?><Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types">` +
		`<Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/>` +
		`<Default Extension="xml" ContentType="application/xml"/>`
	mainOverride = `<Override PartName="/word/document.xml" ContentType="application/vnd.openxmlformats-officedocument.wordprocessingml.document.main+xml"/>`
	pkgRels      = relsOpen + `<Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/officeDocument" Target="word/document.xml"/></Relationships>`
	rt           = "http://schemas.openxmlformats.org/officeDocument/2006/relationships/"
)

func rel(id, typ, target string) string {
	return `<Relationship Id="` + id + `" Type="` + rt + typ + `" Target="` + target + `"/>`
}

// docx builds a package: the content types (Defaults plus extra), the
// package relationships, word/document.xml around body, the document's
// relationships when docRels is not empty, and more parts.
func docx(body, overrides, docRels string, more ...part) []part {
	parts := []part{
		{"[Content_Types].xml", ctOpen + mainOverride + overrides + `</Types>`},
		{"_rels/.rels", pkgRels},
		{"word/document.xml", wOpen + body + wClose},
	}
	if docRels != "" {
		parts = append(parts, part{"word/_rels/document.xml.rels", relsOpen + docRels + `</Relationships>`})
	}
	return append(parts, more...)
}

func wPart(root, inner string) string {
	return `<w:` + root + ` xmlns:w="` + testW + `" xmlns:w14="` + testW14 + `">` + inner + `</w:` + root + `>`
}

func utf16LE(s string) string {
	b := []byte{0xFF, 0xFE}
	for _, u := range utf16.Encode([]rune(s)) {
		b = append(b, byte(u), byte(u>>8))
	}
	return string(b)
}

var ruleCases = []struct {
	name  string
	parts []part
}{
	{"content-types", []part{
		{"[Content_Types].xml", ctOpen + `<Default Extension="XML" ContentType="application/xml"/>` + mainOverride + mainOverride +
			`<Override PartName="/word/missing.xml" ContentType="application/xml"/></Types>`},
		{"_rels/.rels", pkgRels},
		{"word/document.xml", wOpen + `<w:p/>` + wClose},
		{"[trash]/0001.dat", "x"},
		{"word/media/blob", "x"},
	}},
	{"relationships", docx(`<w:p/>`, ``,
		`<Relationship Id="1bad" Type="`+rt+`styles" Target="styles.xml"/>`+
			`<Relationship Id="rId2" Target="x.xml"/>`+
			`<Relationship Id="rId3" Type="`+rt+`settings"/>`+
			`<Relationship Id="rId4" Type="`+rt+`webSettings" Target="w.xml" TargetMode="Foo"/>`+
			`<Relationship Id="rId4" Type="`+rt+`fontTable" Target="f.xml"/>`+
			rel("rId5", "styles", "styles.xml")+
			`<Relationship Id="rId6" Type="`+rt+`numbering" Target="http://example.com/n.xml" TargetMode="External"/>`+
			rel("rId7", "theme", "theme/missing.xml")+
			rel("rId8", "image", "media/image%201.png")+
			rel("rId9", "image", "styles.xml"),
		part{"word/styles.xml", wPart("styles", "")},
		part{"word/x.xml", "<x/>"},
		part{"word/media/image 1.png", "png"},
	)},
	{"orphan-rels", docx(`<w:p/>`, ``, ``,
		part{"foo/_rels/x.xml.rels", relsOpen + `</Relationships>`},
		part{"bar.rels", relsOpen + `</Relationships>`},
		part{"baz/x.rels", relsOpen + `</Relationships>`},
	)},
	{"no-office-document", []part{
		{"[Content_Types].xml", ctOpen + mainOverride + `</Types>`},
		{"_rels/.rels", relsOpen + rel("rId1", "styles", "word/document.xml") + `</Relationships>`},
		{"word/document.xml", wOpen + `<w:p/>` + wClose},
	}},
	{"strict-office-document", []part{
		{"[Content_Types].xml", ctOpen + mainOverride + `</Types>`},
		{"_rels/.rels", relsOpen + `<Relationship Id="rId1" Type="http://purl.oclc.org/ooxml/officeDocument/relationships/officeDocument" Target="word/document.xml"/></Relationships>`},
		// Without a Transitional main part nothing past the package checks
		// runs: the unpaired bookmark goes unreported.
		{"word/document.xml", wOpen + `<w:bookmarkStart w:id="1"/>` + wClose},
		{"word/orphan.bin", "x"},
	}},
	{"references", docx(
		`<w:p><w:hyperlink r:id="rId1"><w:r><w:t>a</w:t></w:r></w:hyperlink><w:hyperlink r:id="rId9"/><w:hyperlink r:id=""/></w:p>`+
			`<w:altChunk r:id="rId2"/><w:p><w:r><w:drawing><wp:inline xmlns:wp="http://schemas.openxmlformats.org/drawingml/2006/wordprocessingDrawing"><wp:docPr id="1" name="a"/>`+
			`<a:graphic xmlns:a="http://schemas.openxmlformats.org/drawingml/2006/main"><a:graphicData uri="x"><a:blip r:embed="rId1" r:link="rId3"/></a:graphicData></a:graphic></wp:inline></w:drawing></w:r></w:p>`+
			`<w:sectPr><w:headerReference w:type="default" r:id="rId3"/></w:sectPr>`, ``,
		rel("rId1", "hyperlink", "target.xml")+rel("rId2", "aFChunk", "chunk.htm")+rel("rId3", "footer", "footer1.xml"),
		part{"word/target.xml", "<x/>"}, part{"word/chunk.htm", "x"}, part{"word/footer1.xml", wPart("ftr", `<w:p/>`)},
	)},
	{"markup-compatibility", []part{
		{"[Content_Types].xml", ctOpen + mainOverride + `</Types>`},
		{"_rels/.rels", pkgRels},
		{"word/document.xml", `<w:document xmlns:w="` + testW + `" xmlns:mc="http://schemas.openxmlformats.org/markup-compatibility/2006" xmlns:x="urn:example:x" xmlns:y="urn:example:y" mc:Ignorable="y z:q" mc:MustUnderstand="x" mc:ProcessContent="q"><w:body>` +
			`<w:p x:attr="1"><mc:AlternateContent><mc:Fallback/></mc:AlternateContent><mc:AlternateContent><mc:Choice Requires="x nope"><x:inside/></mc:Choice></mc:AlternateContent>` +
			`<x:outside/><y:ignored/><w:r><mc:AlternateContent><mc:Choice Requires="x"/><mc:Fallback><x:hidden/></mc:Fallback></mc:AlternateContent></w:r></w:p>` + wClose},
	}},
	{"fields", docx(
		`<w:p><w:r><w:fldChar w:fldCharType="separate"/><w:fldChar w:fldCharType="end"/>`+
			`<w:fldChar w:fldCharType="begin"/><w:instrText>A</w:instrText><w:fldChar w:fldCharType="separate"/><w:instrText>B</w:instrText>`+
			`<w:fldChar w:fldCharType="separate"/><w:fldChar w:fldCharType="end"/><w:fldChar w:fldCharType="begin"/><w:fldChar w:fldCharType="other"/></w:r></w:p>`, ``, ``)},
	{"ranges", docx(
		`<w:p><w:bookmarkStart w:id="1" w:name="a"/><w:bookmarkStart w:id="1" w:name="b"/><w:bookmarkEnd w:id="1"/><w:bookmarkEnd w:id="2"/>`+
			`<w:commentRangeStart w:id="3"/><w:commentRangeEnd w:id="4"/><w:commentRangeStart w:id="5"/><w:commentRangeStart w:id="5"/><w:commentRangeEnd w:id="5"/>`+
			`<w:moveFromRangeStart w:id="6" w:name="m"/><w:moveFromRangeEnd w:id="6"/><w:moveToRangeStart w:id="7" w:name="n"/><w:moveToRangeEnd w:id="8"/></w:p>`, ``, ``)},
	{"deletions", docx(
		`<w:p><w:del w:id="1" w:author="a" w:date="2020-01-01T00:00:00Z"><w:r><w:t>x</w:t><w:instrText>y</w:instrText></w:r></w:del>`+
			`<w:moveFrom w:id="2" w:author="a" w:date="2020-01-01T00:00:00Z"><w:r><w:t>z</w:t><w:delText>q</w:delText></w:r></w:moveFrom>`+
			`<w:r><w:delInstrText>v</w:delInstrText><w:t xml:space="preserve"> ok </w:t><w:t>bad </w:t></w:r>`+
			`<w:del w:id="3" w:author="a" w:date="2020-01-01T00:00:00Z"><w:r><w:pict><w:txbxContent><w:p><w:r><w:t>in box</w:t></w:r></w:p></w:txbxContent></w:pict></w:r></w:del>`+
			`<w:r><w:pict><w:txbxContent><w:tbl/></w:txbxContent></w:pict></w:r></w:p>`, ``, ``)},
	{"tables", docx(
		`<w:tbl><w:tblPr/><w:tblGrid><w:gridCol/><w:gridCol/></w:tblGrid>`+
			`<w:tr/>`+
			`<w:tr><w:tc><w:tcPr><w:vMerge/></w:tcPr></w:tc><w:tc><w:p/><w:tbl><w:tblPr/><w:tblGrid/><w:tr><w:tc><w:p/></w:tc></w:tr></w:tbl></w:tc></w:tr>`+
			`<w:sdt><w:sdtContent><w:tr><w:trPr><w:gridBefore w:val=" 1 "/></w:trPr><w:tc><w:tcPr><w:vMerge w:val="restart"/></w:tcPr><w:p/></w:tc></w:tr></w:sdtContent></w:sdt>`+
			`<w:tr><w:trPr><w:gridBefore w:val="1"/><w:gridAfter w:val="x"/></w:trPr><w:tc><w:tcPr><w:vMerge/><w:gridSpan w:val="2"/></w:tcPr><w:p/></w:tc></w:tr>`+
			`</w:tbl><w:p/>`, ``, ``)},
	{"ids", docx(
		`<w:p w14:paraId="0000000A" w14:textId="XYZ"/><w:p w14:paraId="0000000a" w14:textId="7FFFFFFF"/>`+
			`<w:p><w:ins w:id="5" w:author="a" w:date="2020-01-01T00:00:00Z"/><w:del w:id="5" w:author="a" w:date="2020-01-01T00:00:00Z"/>`+
			`<w:r><w:drawing><wp:anchor xmlns:wp="http://schemas.openxmlformats.org/drawingml/2006/wordprocessingDrawing"><wp:docPr id="7"/></wp:anchor></w:drawing><w:drawing><wp:inline xmlns:wp="http://schemas.openxmlformats.org/drawingml/2006/wordprocessingDrawing"><wp:docPr id="7"/></wp:inline></w:drawing></w:r></w:p>`, ``, ``)},
	{"comments", docx(
		`<w:p w14:paraId="00000001"><w:commentRangeStart w:id="1"/><w:r><w:commentReference w:id="1"/><w:commentReference w:id="9"/></w:r><w:commentRangeEnd w:id="1"/></w:p>`, ``, ``,
		part{"word/comments.xml", wPart("comments",
			`<w:comment w:id="1"><w:p w14:paraId="00000002"/><w:p w14:paraId="00000003"/></w:comment>`+
				`<w:comment w:id="1"><w:p w14:paraId="00000004"/></w:comment><w:comment w:id="2"/>`)},
		part{"word/commentsExtended.xml", `<w15:commentsEx xmlns:w15="http://schemas.microsoft.com/office/word/2012/wordml">` +
			`<w15:commentEx w15:paraId="00000003"/><w15:commentEx w15:paraId="00000002" w15:paraIdParent="0000000F"/><w15:commentEx w15:paraId="00000003"/></w15:commentsEx>`},
		part{"word/commentsIds.xml", `<w16cid:commentsIds xmlns:w16cid="http://schemas.microsoft.com/office/word/2016/wordml/cid">` +
			`<w16cid:commentId w16cid:paraId="00000003" w16cid:durableId="00000000"/><w16cid:commentId w16cid:paraId="00000005" w16cid:durableId="1A2B3C4D"/>` +
			`<w16cid:commentId w16cid:paraId="00000004" w16cid:durableId="1a2b3c4d"/></w16cid:commentsIds>`},
		part{"word/commentsExtensible.xml", `<w16cex:commentsExtensible xmlns:w16cex="http://schemas.microsoft.com/office/word/2018/wordml/cex">` +
			`<w16cex:commentExtensible w16cex:durableId="1A2B3C4D"/><w16cex:commentExtensible w16cex:durableId="0000BEEF"/></w16cex:commentsExtensible>`},
	)},
	{"styles", docx(`<w:p/>`, ``, rel("rId1", "styles", "styles.xml"),
		part{"word/styles.xml", wPart("styles",
			`<w:style w:styleId="A"><w:name w:val="A"/><w:basedOn w:val="B"/><w:next w:val="A"/><w:link w:val="C"/></w:style>`+
				`<w:style w:styleId="A"/><w:style><w:next/></w:style>`)},
	)},
	{"sections", docx(
		`<w:p><w:pPr><w:sectPr><w:headerReference w:type="even" r:id="h"/><w:headerReference w:type="even" r:id="h"/><w:footerReference r:id="h"/><w:footerReference w:type="default" r:id="h"/></w:sectPr></w:pPr></w:p>`, ``, ``)},
	{"numbering", docx(
		`<w:p><w:pPr><w:numPr><w:ilvl w:val="9"/><w:numId w:val="7"/></w:numPr></w:pPr></w:p><w:p><w:pPr><w:numPr><w:ilvl/><w:numId w:val="0"/></w:numPr></w:pPr></w:p>`+
			`<w:p><w:pPr><w:numPr><w:ilvl w:val="10"/><w:numId w:val="1"/></w:numPr></w:pPr></w:p>`, ``, ``,
		part{"word/numbering.xml", wPart("numbering",
			`<w:abstractNum w:abstractNumId="1"><w:lvl w:ilvl="0"/><w:lvl w:ilvl="0"/></w:abstractNum><w:abstractNum w:abstractNumId="1"/>`+
				`<w:num w:numId="1"><w:abstractNumId w:val="1"/></w:num><w:num w:numId="1"><w:abstractNumId w:val="2"/></w:num><w:num w:numId="3"/>`)},
	)},
	{"notes", docx(
		`<w:p><w:r><w:footnoteReference w:id="1"/><w:footnoteReference w:id="5"/><w:endnoteReference w:id="2"/></w:r></w:p>`+
			`<w:sectPr><w:headerReference w:type="default" r:id="rId4"/></w:sectPr>`, ``,
		rel("rId1", "footnotes", "footnotes.xml")+rel("rId2", "settings", "settings.xml")+rel("rId4", "header", "header1.xml"),
		part{"word/footnotes.xml", wPart("footnotes", `<w:footnote w:id="1"><w:p w14:paraId="00000001"/></w:footnote><w:footnote w:id="0"/>`)},
		part{"word/settings.xml", wPart("settings", `<w:footnotePr><w:footnote w:id="0"/><w:footnote w:id="-1"/></w:footnotePr><w:endnotePr><w:endnote w:id="0"/></w:endnotePr>`)},
		part{"word/header1.xml", wPart("hdr", `<w:tbl/>`)},
	)},
	{"schema", docx(
		`<w:p w:rsidR="00AB12CD" w:bogus="1"><w:pPr><w:jc w:val="both"/><w:spacing w:before="1in" w:after="-5"/><w:ind w:left="99999999999999999999"/></w:pPr>`+
			`<w:r><w:rPr><w:b w:val="yes"/><w:color w:val="red"/><w:sz w:val="+24"/></w:rPr><w:t xml:space="keep">x</w:t></w:r>stray</w:p>`+
			`<w:p><w:ins w:id="1" w:author="a" w:date="2024-02-30T24:00:00"/><w:ins w:id="2" w:author="a" w:date="2024-01-01T24:00:00"/>`+
			`<w:pPr/><w:pPr/></w:p><w:sectPr><w:pgSz w:w="12240" w:h="15840" w:orient="sideways"/><w:pgMar w:top="1440"/><w:cols w:num="2" w:space="1.5in"/></w:sectPr>`, ``, ``)},
	{"xml-parts", docx(`<w:p/>`, `<Override PartName="/word/data.bin" ContentType="text/xml; charset=utf-8"/>`, ``,
		part{"word/dtd.xml", `<!DOCTYPE x><x/>`},
		part{"word/prefix.xml", `<p:x/>`},
		part{"word/latin.xml", `<?xml version="1.0" encoding="ISO-8859-1"?><x/>`},
		part{"word/junk.xml", `junk<x/>`},
		part{"word/data.bin", `<x>`},
		part{"word/u16.xml", utf16LE(`<?xml version="1.0" encoding="UTF-16"?><x/>`)},
		part{"word/bad16.xml", utf16LE(`<?xml version="1.0" encoding="UTF-8"?><x/>`)},
	)},
	{"para-ids-across-parts", docx(
		`<w:p w14:paraId="00000001"/><w:sectPr><w:headerReference w:type="default" r:id="rId1"/><w:headerReference w:type="first" r:id="rId2"/></w:sectPr>`, ``,
		rel("rId1", "header", "header1.xml")+rel("rId2", "header", "header1.xml"),
		part{"word/header1.xml", wPart("hdr", `<w:p w14:paraId="00000001"/><w:p w14:paraId="00000002"/>`)},
	)},
	{"zip-names", append(docx(`<w:p/>`, ``, ``),
		part{"word/Document.xml", wOpen + `<w:p/>` + wClose},
		part{"word/a./b.xml", "<x/>"},
		part{"word\\c.xml", "<x/>"},
		part{"word/./d.xml", "<x/>"},
		part{"word/", ""},
	)},
}

// TestRulesWrite writes the rule packages to $INTEGRITY_RULES_DIR, to run
// docxdriver's examples/integrity.rs on and refresh testdata/rules.golden.
func TestRulesWrite(t *testing.T) {
	dir := os.Getenv("INTEGRITY_RULES_DIR")
	if dir == "" {
		t.Skip("INTEGRITY_RULES_DIR not set")
	}
	for _, c := range ruleCases {
		if err := os.WriteFile(filepath.Join(dir, c.name+".docx"), zipParts(t, c.parts), 0o600); err != nil {
			t.Fatal(err)
		}
	}
}

func TestRulesMatchDocxdriver(t *testing.T) {
	want := map[string][]string{}
	f, err := os.Open(filepath.Join("testdata", "rules.golden"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := f.Close(); err != nil {
			t.Error(err)
		}
	}()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		name, rest, _ := strings.Cut(sc.Text(), "\t")
		want[name] = append(want[name], rest)
	}
	if err := sc.Err(); err != nil {
		t.Fatal(err)
	}
	for _, c := range ruleCases {
		got := map[Finding]int{}
		findings := Check(zipParts(t, c.parts))
		for _, k := range findings.Kinds() {
			got[k] = findings.Count(k)
		}
		exp := map[Finding]int{}
		for _, line := range want[c.name] {
			f := strings.Split(line, "\t")
			n, err := strconv.Atoi(f[len(f)-1])
			if len(f) != 3 || err != nil {
				t.Fatalf("rules.golden: %s: %q", c.name, line)
			}
			exp[Finding{f[0], f[1]}] = n
		}
		if len(exp) == 0 {
			t.Errorf("%s: docxdriver found nothing; the case tests no rule", c.name)
		}
		rustOnly, goOnly := diffFindings(exp, got)
		if len(rustOnly)+len(goOnly) == 0 {
			continue
		}
		if names, ok := explained(rustOnly, goOnly); ok {
			t.Logf("%s: intended (%s): docxdriver only %q, Go only %q", c.name, strings.Join(names, "; "), rustOnly, goOnly)
		} else {
			t.Errorf("%s: unexplained difference: docxdriver only %q, Go only %q", c.name, rustOnly, goOnly)
		}
	}
}

// Checking a package twice finds the same instances in the same order:
// no check emits them in map order (the unreferenced comments of a
// package with several, which once came in map order).
func TestInstancesDeterministic(t *testing.T) {
	var comments, refs string
	for i := range 12 {
		id := strconv.Itoa(i)
		comments += `<w:comment w:id="` + id + `"><w:p/></w:comment>`
		if i%3 == 0 {
			refs += `<w:r><w:commentReference w:id="` + id + `"/></w:r>`
		}
	}
	data := zipParts(t, docx(`<w:p>`+refs+`</w:p>`, ``, ``, part{"word/comments.xml", wPart("comments", comments)}))
	want := Check(data).Instances()
	if len(want) < 8 {
		t.Fatalf("%d instances, want the unreferenced comments", len(want))
	}
	for range 20 {
		if got := Check(data).Instances(); !reflect.DeepEqual(got, want) {
			t.Fatalf("instances %v, then %v", want, got)
		}
	}
	for _, c := range ruleCases {
		data := zipParts(t, c.parts)
		want := Check(data).Instances()
		for range 5 {
			if got := Check(data).Instances(); !reflect.DeepEqual(got, want) {
				t.Fatalf("%s: instances %v, then %v", c.name, want, got)
			}
		}
	}
}
