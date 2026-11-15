package integrity

import (
	"strings"

	"github.com/jhnoel/documentdriver/internal/ooxml/ctypes"
	"github.com/jhnoel/documentdriver/internal/ooxml/tree"
)

// Namespaces.
const (
	wNS        = "http://schemas.openxmlformats.org/wordprocessingml/2006/main"
	rNS        = "http://schemas.openxmlformats.org/officeDocument/2006/relationships"
	mcNS       = "http://schemas.openxmlformats.org/markup-compatibility/2006"
	w14NS      = "http://schemas.microsoft.com/office/word/2010/wordml"
	w15NS      = "http://schemas.microsoft.com/office/word/2012/wordml"
	w16cidNS   = "http://schemas.microsoft.com/office/word/2016/wordml/cid"
	w16cexNS   = "http://schemas.microsoft.com/office/word/2018/wordml/cex"
	wpNS       = "http://schemas.openxmlformats.org/drawingml/2006/wordprocessingDrawing"
	aNS        = "http://schemas.openxmlformats.org/drawingml/2006/main"
	pkgRelsNS  = "http://schemas.openxmlformats.org/package/2006/relationships"
	pkgTypesNS = "http://schemas.openxmlformats.org/package/2006/content-types"
	xmlNS      = tree.XMLNamespace

	// strictRelNS is the Strict vocabulary's relationships namespace; a
	// Strict relationship type is it, a slash and the type's last segment.
	strictRelNS = "http://purl.oclc.org/ooxml/officeDocument/relationships"

	// officeExtensionPrefix starts every Office extension namespace (w14,
	// w15, ...) and every Microsoft relationship type.
	officeExtensionPrefix = "http://schemas.microsoft.com/office/"

	contentTypesName = "[Content_Types].xml"
	trashPrefix      = "[trash]/"
)

// officeRel is the prefix of the Transitional relationship types: the
// relationships namespace and a slash.
const officeRel = rNS + "/"

// strictRel returns the Strict spelling of the relationship type rel.
func strictRel(rel string) string { return strictRelNS + "/" + ctypes.Last(rel) }

// relPrefix returns rel without its last segment.
func relPrefix(rel string) string { return strings.TrimSuffix(rel, ctypes.Last(rel)) }

// aFChunkRel is the altChunk relationship type. The ctypes table has no
// entry for it (no Office-authored file in the corpora uses altChunk), so
// it is spelled as the Transitional relationships namespace and its last
// segment, as docxdriver did.
const aFChunkRel = officeRel + "aFChunk"

// understoodPrefixes are the namespaces Word reads without mc:Ignorable:
// ECMA-376 Transitional plus the Office extension namespaces Word
// declares on every part it writes.
var understoodPrefixes = []string{
	"http://schemas.openxmlformats.org/",
	"http://purl.oclc.org/ooxml/",
	officeExtensionPrefix,
	"urn:schemas-microsoft-com:",
	"http://www.w3.org/",
	"http://purl.org/dc/",
}

// understood reports whether ns starts with a known vocabulary prefix.
func understood(ns string) bool {
	for _, p := range understoodPrefixes {
		if strings.HasPrefix(ns, p) {
			return true
		}
	}
	return false
}

// revisionElements are the w: elements whose w:id is a revision id.
var revisionElements = map[string]bool{
	"ins": true, "del": true, "moveFrom": true, "moveTo": true, "pPrChange": true, "rPrChange": true,
	"sectPrChange": true, "tblPrChange": true, "trPrChange": true, "tcPrChange": true, "tblGridChange": true,
	"tblPrExChange": true, "numberingChange": true, "cellIns": true, "cellDel": true, "cellMerge": true,
}

// expectedRels are the relationship types whose targets' content types
// are checked: the ones docxdriver checked. The content types themselves
// come from the ctypes table (what Office-authored files use); docxdriver
// kept its own list, and docs/docx-integrity.md lists where they differ.
var expectedRels = map[string]bool{
	ctypes.RelOfficeDocument: true, ctypes.RelStyles: true, ctypes.RelSettings: true,
	ctypes.RelWebSettings: true, ctypes.RelFontTable: true, ctypes.RelNumbering: true,
	ctypes.RelHeader: true, ctypes.RelFooter: true, ctypes.RelFootnotes: true, ctypes.RelEndnotes: true,
	ctypes.RelComments: true, ctypes.RelGlossaryDocument: true, ctypes.RelTheme: true,
	ctypes.RelExtendedProperties: true, ctypes.RelCustomProperties: true, ctypes.RelCustomXmlProps: true,
	ctypes.RelChart: true, ctypes.RelCoreProperties: true, ctypes.RelCommentsExtended: true,
	ctypes.RelCommentsIds: true, ctypes.RelCommentsExtensible: true, ctypes.RelPeople: true,
	ctypes.RelStylesWithEffects: true,
}

// expectedContentTypes returns the content types Word writes for the
// targets of relationship type rel, and whether rel is checked.
func expectedContentTypes(rel string) ([]string, bool) {
	if !expectedRels[rel] {
		return nil, false
	}
	return ctypes.ExpectedContentTypes(rel), true
}

// singleSources are the relationship type prefixes under which a type may
// occur at most once per source part (ECMA-376 Part 1 part summaries),
// keyed by last segment so the Strict and Transitional spellings of one
// type count together.
var singleSources = []string{
	officeRel,
	strictRelNS + "/",
	relPrefix(ctypes.RelCoreProperties),
	relPrefix(ctypes.RelStylesWithEffects),
	relPrefix(ctypes.RelPeople),
	relPrefix(ctypes.RelCommentsIds),
	relPrefix(ctypes.RelCommentsExtensible),
}

var singleNames = map[string]bool{
	"officeDocument": true, "core-properties": true, "extended-properties": true, "settings": true,
	"webSettings": true, "styles": true, "stylesWithEffects": true, "numbering": true, "fontTable": true,
	"theme": true, "footnotes": true, "endnotes": true, "comments": true, "glossaryDocument": true,
	"commentsExtended": true, "commentsIds": true, "commentsExtensible": true, "people": true,
}

// singleInstance returns the key under which rel counts towards the
// one-per-source rule, and whether it does.
func singleInstance(rel string) (string, bool) {
	for _, p := range singleSources {
		if short, ok := strings.CutPrefix(rel, p); ok {
			return short, singleNames[short]
		}
	}
	return "", false
}
