package edit

import (
	"github.com/jhnoel/documentdriver/internal/ooxml/schema"
	"github.com/jhnoel/documentdriver/internal/ooxml/tree"
	"github.com/jhnoel/documentdriver/internal/plan"
)

// storyRoots are the part roots whose parts are divided into units.
var storyRoots = map[string]bool{"document": true, "hdr": true, "ftr": true}

// units divides WordprocessingML parts for the verifier (plan.Units).
type units struct{}

// Units returns the docx units the verifier compares, for law harnesses
// that hold plans to the same division (internal/laws).
func Units() plan.Units { return units{} }

// HasUnits implements plan.Units: the main document, header and footer
// parts. Other parts (styles, numbering, comments, settings) are declared
// as a whole.
func (units) HasUnits(doc *tree.Document) bool {
	r := doc.Root()
	return r != nil && WML.Has(r) && storyRoots[r.Local()]
}

// IsUnit implements plan.Units.
func (units) IsUnit(e *tree.Node) bool { return IsUnit(e) }

// Describe implements plan.Units.
func (units) Describe(e *tree.Node) string {
	switch {
	case is(e, "p"):
		if id, ok := e.AttrNS(W14.URIs[0], "paraId"); ok {
			return "paragraph " + id
		}
		return "paragraph"
	case is(e, "tbl"):
		return "table"
	case is(e, "tr"):
		return "row"
	case is(e, "tc"):
		return "cell"
	case is(e, "sectPr"):
		return "section"
	case is(e, "sdt"):
		return "content control"
	case is(e, "sdtContent"):
		return "content control content"
	}
	return e.Name()
}

// IsUnit reports whether element e is a unit the verifier compares on its
// own: w:body; every w:p, w:tbl, w:tr and w:tc; a w:sectPr outside
// w:sectPrChange; and a w:sdt that is not inside a paragraph (block, row
// or cell level), with its w:sdtContent. Part roots are units by
// plan.Verify itself.
func IsUnit(e *tree.Node) bool {
	if e == nil || e.Kind() != tree.ElementNode || !WML.Has(e) {
		return false
	}
	switch e.Local() {
	case "body", "p", "tbl", "tr", "tc":
		return true
	case "sectPr":
		return !is(e.Parent(), "sectPrChange")
	case "sdt":
		p := e.Parent()
		return isRoot(p) || IsUnit(p) && !is(p, "p")
	case "sdtContent":
		return is(e.Parent(), "sdt") && IsUnit(e.Parent())
	}
	return false
}

// Type returns the schema complex type of WordprocessingML element e
// (w:p is w:CT_P), and false when the schema does not tabulate it (w:tbl,
// whose content model the slot tables cannot express) or e is not
// WordprocessingML.
func Type(e *tree.Node) (string, bool) {
	t := typeOf(e)
	if t == "" {
		return "", false
	}
	if _, ok := schema.Slots(t); !ok {
		return "", false
	}
	return t, true
}

// typeOf returns e's complex type, tabulated or not, or "". It walks up
// to the nearest element whose type is fixed by its name (a paragraph,
// run, row, cell, body or part root), at most the tree's depth.
func typeOf(e *tree.Node) string {
	if e == nil || e.Kind() != tree.ElementNode || !WML.Has(e) {
		return ""
	}
	if isRoot(e) {
		t, _ := schema.RootType("w", e.Local())
		return t
	}
	switch e.Local() {
	case "p":
		return "w:CT_P"
	case "tbl":
		return "w:CT_Tbl"
	case "tr":
		return "w:CT_Row"
	case "tc":
		return "w:CT_Tc"
	case "r":
		// A run is w:CT_R wherever it stands, also under parents whose
		// type is not tabulated.
		return "w:CT_R"
	case "body":
		return "w:CT_Body"
	}
	pt := typeOf(e.Parent())
	if pt == "w:CT_Tbl" {
		// w:CT_Tbl is untabulated (range markup sits both before
		// w:tblPr and among the rows); its other children are fixed.
		switch e.Local() {
		case "tblPr":
			return "w:CT_TblPr"
		case "tblGrid":
			return "w:CT_TblGrid"
		case "sdt":
			return "w:CT_SdtRow"
		case "customXml":
			return "w:CT_CustomXmlRow"
		}
		return ""
	}
	slots, ok := schema.Slots(pt)
	if !ok {
		return ""
	}
	_, c := schema.Find(slots, "w", e.Local())
	return c.Type
}
