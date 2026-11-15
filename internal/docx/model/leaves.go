package model

import (
	"github.com/jhnoel/documentdriver/internal/ooxml/tree"
)

// LeafKind is what a paragraph-content element is to the text model: the
// one taxonomy that read text, find, the segment offsets, comment anchors
// and the edit side's span engine (internal/docx/span) all use. It is
// decided once, here; docs/design.md ("docx read side", B8) has the table.
type LeafKind uint8

// The kinds.
const (
	// KindUnknown is an element the taxonomy does not name. Text counts
	// through it (as docxdriver reads it), but a span op refuses to cross
	// it (span.Blocker).
	KindUnknown LeafKind = iota
	// KindWrapper holds paragraph content whose text counts: a run, a
	// hyperlink, smart tag, custom XML or bidi wrapper, a simple field, a
	// content control, a ruby, and the revision wrappers (w:ins, w:del,
	// w:moveFrom, w:moveTo).
	KindWrapper
	// KindText is character data: w:t, and w:delText (deleted text).
	KindText
	// KindChar is a one-character leaf with a fixed character: w:tab and
	// w:ptab "\t", w:br and w:cr "\n", w:noBreakHyphen U+2011 and
	// w:softHyphen U+00AD.
	KindChar
	// KindObject is an atomic object that counts as one U+FFFC and is
	// never split: equations (m:oMath, m:oMathPara), w:sym (its glyph is
	// the font's), drawings (w:drawing, w:pict, w:object, w:contentPart),
	// note references and marks, generated page numbers and dates, and
	// note separators. Nothing inside one counts.
	KindObject
	// KindMarker has no width and no text of its own: range markers
	// (bookmarks, comments, permissions, move and custom XML ranges), a
	// comment reference, proofing marks, the rendered page break cache,
	// and field characters and codes (w:fldChar, w:instrText,
	// w:delInstrText; the field result is ordinary runs).
	KindMarker
	// KindProps is a properties element (w:pPr, w:rPr, w:sdtPr, ...):
	// no text, it describes its parent.
	KindProps
	// KindStory is another story: a paragraph nested in a text box or
	// drawing. It occupies no offsets in the enclosing paragraph.
	KindStory
)

// ObjectChar is the one character an object (KindObject) occupies in
// paragraph text and offsets.
const ObjectChar = "\uFFFC"

// EquationChar is ObjectChar: an equation is an object.
const EquationChar = ObjectChar

var wKinds = map[string]LeafKind{
	"r": KindWrapper, "hyperlink": KindWrapper, "smartTag": KindWrapper, "customXml": KindWrapper,
	"dir": KindWrapper, "bdo": KindWrapper, "fldSimple": KindWrapper, "sdt": KindWrapper,
	"sdtContent": KindWrapper, "ins": KindWrapper, "del": KindWrapper, "moveFrom": KindWrapper,
	"moveTo": KindWrapper, "ruby": KindWrapper, "rt": KindWrapper, "rubyBase": KindWrapper,

	"t": KindText, "delText": KindText,

	"tab": KindChar, "ptab": KindChar, "br": KindChar, "cr": KindChar,
	"noBreakHyphen": KindChar, "softHyphen": KindChar,

	"sym": KindObject, "drawing": KindObject, "pict": KindObject, "object": KindObject,
	"contentPart": KindObject, "footnoteReference": KindObject, "endnoteReference": KindObject,
	"footnoteRef": KindObject, "endnoteRef": KindObject, "annotationRef": KindObject,
	"separator": KindObject, "continuationSeparator": KindObject, "pgNum": KindObject,
	"dayShort": KindObject, "dayLong": KindObject, "monthShort": KindObject,
	"monthLong": KindObject, "yearShort": KindObject, "yearLong": KindObject,

	"bookmarkStart": KindMarker, "bookmarkEnd": KindMarker, "commentRangeStart": KindMarker,
	"commentRangeEnd": KindMarker, "commentReference": KindMarker, "permStart": KindMarker,
	"permEnd": KindMarker, "proofErr": KindMarker, "moveFromRangeStart": KindMarker,
	"moveFromRangeEnd": KindMarker, "moveToRangeStart": KindMarker, "moveToRangeEnd": KindMarker,
	"customXmlInsRangeStart": KindMarker, "customXmlInsRangeEnd": KindMarker,
	"customXmlDelRangeStart": KindMarker, "customXmlDelRangeEnd": KindMarker,
	"customXmlMoveFromRangeStart": KindMarker, "customXmlMoveFromRangeEnd": KindMarker,
	"customXmlMoveToRangeStart": KindMarker, "customXmlMoveToRangeEnd": KindMarker,
	"lastRenderedPageBreak": KindMarker, "fldChar": KindMarker, "instrText": KindMarker,
	"delInstrText": KindMarker,

	"pPr": KindProps, "rPr": KindProps, "sdtPr": KindProps, "sdtEndPr": KindProps,
	"smartTagPr": KindProps, "customXmlPr": KindProps, "rubyPr": KindProps,

	"p": KindStory,
}

// charText is the character of each KindChar leaf.
var charText = map[string]string{
	"tab": "\t", "ptab": "\t", "br": "\n", "cr": "\n",
	"noBreakHyphen": "\u2011", "softHyphen": "\u00AD",
}

// Classify returns n's kind. WordprocessingML names are matched as IsW
// does (the WordML family, Strict included, or no namespace); equations by local name in any
// namespace; mc:AlternateContent is a wrapper whose effective branch
// (AlternateContentBranch) counts.
func Classify(n *tree.Node) LeafKind {
	if !IsElement(n) {
		return KindUnknown
	}
	switch {
	case IsLocal(n, "oMath"), IsLocal(n, "oMathPara"):
		return KindObject
	case IsLocal(n, "AlternateContent"):
		return KindWrapper
	}
	if n.Space() != "" && !WordML.Has(n) {
		return KindUnknown
	}
	return wKinds[n.Local()]
}

// LeafText returns the text a paragraph-content element contributes as one
// unit, and whether it does (KindText, KindChar and KindObject):
// w:t and w:delText their character data (deleted reports w:delText, which
// only views showing deletions show), a KindChar leaf its character, and
// an object one U+FFFC; nothing inside such an element counts. Read
// text, find, the segment offsets the edit side selects by and comment
// anchors all count this way, so a selection never silently spans a
// break, tab, symbol, equation or drawing. docxdriver 0.5.2 counts only
// w:t and w:delText (an intended difference, see
// tools/oracle/baseline/README.md, items 10 and 11).
func LeafText(n *tree.Node) (text string, deleted, ok bool) {
	switch Classify(n) {
	case KindText:
		return TextContent(n), n.Local() == "delText", true
	case KindChar:
		return charText[n.Local()], false, true
	case KindObject:
		return ObjectChar, false, true
	}
	return "", false, false
}

// isLeaf reports whether n is a text, character or object leaf.
func isLeaf(n *tree.Node) bool {
	switch Classify(n) {
	case KindText, KindChar, KindObject:
		return true
	}
	return false
}
