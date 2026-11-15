package sml

import (
	"github.com/jhnoel/documentdriver/internal/ooxml/ctypes"
	"github.com/jhnoel/documentdriver/internal/ooxml/tree"
	"github.com/jhnoel/documentdriver/internal/opc"
)

// SheetEntry is one <sheet> of a workbook's <sheets>, in tab order.
type SheetEntry struct {
	// Pos is the 0-based position in <sheets>, which a defined name's
	// localSheetId refers to.
	Pos int
	// Name is sheet@name as stored; State is sheet@state ("visible" when
	// absent).
	Name, State string
	// SheetID is sheet@sheetId (ParseIndex), 0 when absent or malformed.
	SheetID int
	// RID is the r:id, found by namespace (Transitional or Strict), not
	// by prefix; "" when absent.
	RID string
}

// SheetEntries returns the <sheet> children of sheets (which may be nil)
// in the namespace of sheets itself, in order.
func SheetEntries(sheets *tree.Node) []SheetEntry {
	if sheets == nil {
		return nil
	}
	ns := sheets.Space()
	var out []SheetEntry
	for e := range sheets.ElementsSeq() {
		if !e.Is(ns, "sheet") {
			continue
		}
		s := SheetEntry{Pos: len(out), State: "visible"}
		s.Name, _ = e.Attr("name")
		if v, ok := e.Attr("state"); ok {
			s.State = v
		}
		if v, ok := e.Attr("sheetId"); ok {
			s.SheetID, _ = ParseIndex(v)
		}
		if v, ok := e.AttrNS(NSRel, "id"); ok {
			s.RID = v
		} else if v, ok := e.AttrNS(NSRelStrict, "id"); ok {
			s.RID = v
		}
		out = append(out, s)
	}
	return out
}

// RelsByID maps rels' relationships by id, the first of a repeated id
// winning (as opc.Rels.Get answers). Build it once per source part: a
// Get per entry would walk every relationship each time. A nil rels gives
// an empty map.
func RelsByID(rels *opc.Rels) map[string]opc.Relationship {
	m := map[string]opc.Relationship{}
	if rels == nil {
		return m
	}
	for _, r := range rels.All() {
		if _, dup := m[r.ID]; !dup {
			m[r.ID] = r
		}
	}
	return m
}

// SheetKind is what a <sheet> entry's relationship points at.
type SheetKind uint8

// The sheet kinds, from the relationship type (compared with ctypes.Is,
// so Strict types match).
const (
	// KindMissing is an entry with no usable part: no relationship, an
	// external one, or a target the package lacks.
	KindMissing SheetKind = iota
	KindWorksheet
	KindChartsheet
	KindDialogsheet
	KindMacrosheet
	// KindOther is a part of another relationship type.
	KindOther
)

// ResolveSheet resolves entry e of the workbook whose relationships are
// rels (the workbook part's) to its part and kind. byID, from RelsByID
// over the same rels and built once, finds the relationship; rels only
// resolves its target against the workbook part (TargetPart), never
// looks one up. part is "" for KindMissing. problem says, in the read
// notes' words, why the entry is KindMissing or KindOther ("" for the
// four sheet kinds).
func ResolveSheet(e SheetEntry, rels *opc.Rels, byID map[string]opc.Relationship, pkg *opc.Package) (part string, kind SheetKind, problem string) {
	r, ok := byID[e.RID]
	switch {
	case !ok || rels == nil:
		return "", KindMissing, "the sheet has no relationship"
	case r.External():
		return "", KindMissing, "the sheet's relationship is external"
	}
	part, err := rels.TargetPart(r)
	if err != nil || !pkg.Has(part) {
		return "", KindMissing, "the sheet's part is missing"
	}
	switch {
	case ctypes.Is(r.Type, ctypes.RelWorksheet):
		return part, KindWorksheet, ""
	case ctypes.Is(r.Type, ctypes.RelChartsheet):
		return part, KindChartsheet, ""
	case ctypes.Is(r.Type, ctypes.RelDialogsheet):
		return part, KindDialogsheet, ""
	case ctypes.Is(r.Type, ctypes.RelXlMacrosheet):
		return part, KindMacrosheet, ""
	}
	return part, KindOther, "the sheet's relationship is not a worksheet"
}

// DefinedName is one <definedName> of a workbook.
type DefinedName struct {
	// Name is definedName@name; Text the element's text (the formula).
	Name, Text string
	// Local is localSheetId: -1 when absent (workbook scope), -2 when it
	// is not the position of a sheet (not ParseIndex, or not below the
	// sheet count): a scope that matches no sheet and not the workbook.
	Local int
	// Hidden is definedName@hidden (IsTrue).
	Hidden bool
}

// DefinedNames returns the <definedName> children of el (which may be
// nil) in the namespace of el itself, in order; nsheets is the number of
// sheets, which bounds a valid localSheetId.
func DefinedNames(el *tree.Node, nsheets int) []DefinedName {
	if el == nil {
		return nil
	}
	ns := el.Space()
	var out []DefinedName
	for e := range el.ElementsSeq() {
		if !e.Is(ns, "definedName") {
			continue
		}
		n := DefinedName{Local: -1}
		n.Name, _ = e.Attr("name")
		if v, ok := e.Attr("localSheetId"); ok {
			if id, ok := ParseIndex(v); ok && id < nsheets {
				n.Local = id
			} else {
				n.Local = -2
			}
		}
		n.Text = e.Text()
		if v, ok := e.Attr("hidden"); ok {
			n.Hidden = IsTrue(v)
		}
		out = append(out, n)
	}
	return out
}
