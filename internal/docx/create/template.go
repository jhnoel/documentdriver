package create

import (
	"archive/zip"
	"bytes"
	"time"

	"github.com/jhnoel/documentdriver/internal/docx/model"
	"github.com/jhnoel/documentdriver/internal/ooxml/ctypes"
)

// The blank template a created document starts from, authored here (not
// docxdriver's blank.docx): a main document with one section (US Letter,
// one-inch margins) and no paragraphs, styles with the document defaults,
// Normal, Heading1 to Heading6, Title, Quote and the Hyperlink character
// style, and settings. Every part is schema-valid, so a document built from
// it passes integrity.GateCreated when its content does.

const xmlDecl = `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>` + "\n"

const documentXML = xmlDecl + `<w:document xmlns:w="` + model.NSW + `"` +
	` xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships"` +
	` xmlns:m="http://schemas.openxmlformats.org/officeDocument/2006/math"` +
	` xmlns:w14="http://schemas.microsoft.com/office/word/2010/wordml"` +
	` xmlns:mc="http://schemas.openxmlformats.org/markup-compatibility/2006" mc:Ignorable="w14">` +
	`<w:body><w:sectPr><w:pgSz w:w="12240" w:h="15840"/>` +
	`<w:pgMar w:top="1440" w:right="1440" w:bottom="1440" w:left="1440" w:header="720" w:footer="720" w:gutter="0"/>` +
	`<w:cols w:space="720"/><w:docGrid w:linePitch="360"/></w:sectPr></w:body></w:document>`

// headingSizes are the half-point sizes of Heading1 to Heading6.
var headingSizes = []string{"32", "28", "26", "24", "22", "22"}

// stylesXML returns the XML of the new document's styles part.
func stylesXML() string {
	var b bytes.Buffer
	b.WriteString(xmlDecl + `<w:styles xmlns:w="` + model.NSW + `">` +
		`<w:docDefaults><w:rPrDefault><w:rPr><w:rFonts w:ascii="Calibri" w:hAnsi="Calibri" w:eastAsia="Calibri" w:cs="Calibri"/>` +
		`<w:sz w:val="22"/><w:szCs w:val="22"/><w:lang w:val="en-US" w:eastAsia="en-US" w:bidi="ar-SA"/></w:rPr></w:rPrDefault>` +
		`<w:pPrDefault><w:pPr><w:spacing w:after="160" w:line="259" w:lineRule="auto"/></w:pPr></w:pPrDefault></w:docDefaults>` +
		`<w:style w:type="paragraph" w:default="1" w:styleId="Normal"><w:name w:val="Normal"/><w:qFormat/></w:style>` +
		`<w:style w:type="character" w:default="1" w:styleId="DefaultParagraphFont"><w:name w:val="Default Paragraph Font"/><w:uiPriority w:val="1"/><w:semiHidden/><w:unhideWhenUsed/></w:style>`)
	for i, sz := range headingSizes {
		n := string(rune('1' + i))
		b.WriteString(`<w:style w:type="paragraph" w:styleId="Heading` + n + `"><w:name w:val="heading ` + n + `"/>` +
			`<w:basedOn w:val="Normal"/><w:next w:val="Normal"/><w:uiPriority w:val="9"/><w:qFormat/>` +
			`<w:pPr><w:keepNext/><w:keepLines/><w:spacing w:before="240" w:after="80"/><w:outlineLvl w:val="` + string(rune('0'+i)) + `"/></w:pPr>` +
			`<w:rPr><w:b/><w:sz w:val="` + sz + `"/><w:szCs w:val="` + sz + `"/></w:rPr></w:style>`)
	}
	b.WriteString(`<w:style w:type="paragraph" w:styleId="Title"><w:name w:val="Title"/><w:basedOn w:val="Normal"/><w:next w:val="Normal"/><w:uiPriority w:val="10"/><w:qFormat/>` +
		`<w:pPr><w:spacing w:after="80"/></w:pPr><w:rPr><w:sz w:val="56"/><w:szCs w:val="56"/></w:rPr></w:style>` +
		`<w:style w:type="paragraph" w:styleId="Quote"><w:name w:val="Quote"/><w:basedOn w:val="Normal"/><w:next w:val="Normal"/><w:uiPriority w:val="29"/><w:qFormat/>` +
		`<w:pPr><w:ind w:left="864" w:right="864"/></w:pPr><w:rPr><w:i/></w:rPr></w:style>` +
		`<w:style w:type="character" w:styleId="Hyperlink"><w:name w:val="Hyperlink"/><w:basedOn w:val="DefaultParagraphFont"/><w:uiPriority w:val="99"/><w:unhideWhenUsed/>` +
		`<w:rPr><w:color w:val="0563C1"/><w:u w:val="single"/></w:rPr></w:style>` +
		`</w:styles>`)
	return b.String()
}

const settingsXML = xmlDecl + `<w:settings xmlns:w="` + model.NSW + `">` +
	`<w:defaultTabStop w:val="720"/><w:characterSpacingControl w:val="doNotCompress"/>` +
	`<w:compat><w:compatSetting w:name="compatibilityMode" w:uri="http://schemas.microsoft.com/office/word" w:val="15"/></w:compat>` +
	`</w:settings>`

const relsNS = `http://schemas.openxmlformats.org/package/2006/relationships`

// contentTypesXML returns the package's content types XML.
func contentTypesXML() string {
	return xmlDecl + `<Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types">` +
		`<Default Extension="rels" ContentType="` + ctypes.CTPackageRelationships + `"/>` +
		`<Default Extension="xml" ContentType="application/xml"/>` +
		`<Override PartName="/word/document.xml" ContentType="` + ctypes.CTWordprocessingmlDocumentMain + `"/>` +
		`<Override PartName="/word/styles.xml" ContentType="` + ctypes.CTWordprocessingmlStyles + `"/>` +
		`<Override PartName="/word/settings.xml" ContentType="` + ctypes.CTWordprocessingmlSettings + `"/>` +
		`</Types>`
}

// packageRels returns the package relationships XML.
func packageRels() string {
	return xmlDecl + `<Relationships xmlns="` + relsNS + `">` +
		`<Relationship Id="rId1" Type="` + ctypes.RelOfficeDocument + `" Target="word/document.xml"/></Relationships>`
}

// documentRels returns the main document relationships XML.
func documentRels() string {
	return xmlDecl + `<Relationships xmlns="` + relsNS + `">` +
		`<Relationship Id="rId1" Type="` + ctypes.RelStyles + `" Target="styles.xml"/>` +
		`<Relationship Id="rId2" Type="` + ctypes.RelSettings + `" Target="settings.xml"/></Relationships>`
}

// templateStyles are the paragraph styles the template declares, which a
// <p class> may name.
var templateStyles = map[string]bool{"Normal": true, "Title": true, "Quote": true,
	"Heading1": true, "Heading2": true, "Heading3": true, "Heading4": true, "Heading5": true, "Heading6": true}

// stamp is every member's modification time: a created file is the same
// bytes for the same HTML.
var stamp = time.Date(1980, 1, 1, 0, 0, 0, 0, time.UTC)

// template returns the blank package's bytes.
func template() ([]byte, error) {
	var b bytes.Buffer
	zw := zip.NewWriter(&b)
	for _, m := range [][2]string{
		{"[Content_Types].xml", contentTypesXML()},
		{"_rels/.rels", packageRels()},
		{"word/_rels/document.xml.rels", documentRels()},
		{"word/document.xml", documentXML},
		{"word/styles.xml", stylesXML()},
		{"word/settings.xml", settingsXML},
	} {
		w, err := zw.CreateHeader(&zip.FileHeader{Name: m[0], Method: zip.Deflate, Modified: stamp})
		if err != nil {
			return nil, err
		}
		if _, err := w.Write([]byte(m[1])); err != nil {
			return nil, err
		}
	}
	if err := zw.Close(); err != nil {
		return nil, err
	}
	return b.Bytes(), nil
}
