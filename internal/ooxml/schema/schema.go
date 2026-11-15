// Package schema holds the tables generated from the pinned ECMA-376
// transitional schemas, one per vocabulary: PresentationML (prefix p),
// DrawingML main (a), WordprocessingML (w), SpreadsheetML (x) and the
// shared common simple types (s). For each tabulated complex type a table
// gives its children in schema order (slots), its attributes' simple
// types, and the facets of those simple types. Code that orders children
// or validates a value before writing it reads these tables; nothing
// hand-writes an enumeration or a child order.
//
// Two small tables hold simple types WordprocessingML attributes use from
// other schemas: the relationship reference (prefix r, r:ST_RelationshipId)
// and the XML namespace (prefix xml, the type of xml:space, from the W3C
// xml.xsd the zip lacks). The WordprocessingML and SpreadsheetML content
// models (ContentModel, RootType) add what an integrity check needs and
// the slot tables leave out: order ranks, occurrence counts, required
// children and attributes, wildcards, for every complex type of wml.xsd
// and sml.xsd.
//
// Names are qualified by the table prefix, whatever prefix a document
// uses: "a:CT_TextParagraph", "w:ST_Jc", "s:ST_OnOff". The package is
// shared by every format and imports nothing format-specific.
package schema

//go:generate go run gen_schema.go OfficeOpenXML-XMLSchema-Transitional.zip

import "strings"

// ZipSHA256 is the SHA-256 of OfficeOpenXML-XMLSchema-Transitional.zip in
// Apache POI at commit 1243387d (the commit pinned by
// eval/corpus/fetch.sh), the file the tables are generated from.
const ZipSHA256 = "d34187520749998af306faf1b730e568b0ca6d88ad24638a407c0a9bb4ca04fc"

// ZipURL is where the pinned schema zip is fetched from.
const ZipURL = "https://raw.githubusercontent.com/apache/poi/1243387d7ffbac84d361323673f1b75a5b3c26f3/poi-ooxml-full/src/main/xmlschema/org/apache/poi/schemas/OfficeOpenXML-XMLSchema-Transitional.zip"

// Child is one element a complex type allows: its table prefix (p, a or
// w; another prefix such as m or wp for an element of a schema that is
// not tabulated), local name, and complex type ("" when the type is not
// a tabulated vocabulary's complex type).
type Child struct {
	Prefix, Local, Type string
}

// Slot is one position in a complex type's content: a single element, or
// a choice of alternatives of which at most one is present. Multi marks
// a position that may repeat (in which the alternatives' order is free).
type Slot struct {
	Names []Child
	Multi bool
}

// Simple is a simple type resolved through its restriction chain: the
// XSD built-in it ends in ("" for a union or a list), union members, the
// item type of a list (whitespace-separated items), enumeration,
// inclusive integer bounds, patterns (each derivation step's must match)
// and lengths (0 when absent).
type Simple struct {
	Base                   string
	Union                  []string
	List                   string
	Enum                   []string
	Min, Max               int64
	HasMin, HasMax         bool
	Patterns               []string
	Length, MinLen, MaxLen int
}

// tables returns the generated tables of the vocabulary a qualified name
// belongs to.
func tables(qname string) (types map[string][]Slot, attrs map[string]map[string]string, simple map[string]Simple) {
	prefix, _, _ := strings.Cut(qname, ":")
	switch prefix {
	case "p":
		return pmlTypes, pmlAttrs, pmlSimple
	case "a":
		return dmlTypes, dmlAttrs, dmlSimple
	case "w":
		return wmlTypes, wmlAttrs, wmlSimple
	case "x":
		return smlTypes(), smlAttrs(), smlSimple()
	case "s":
		return sharedTypes, sharedAttrs, sharedSimple
	case "r":
		return relTypes, relAttrs, relSimple
	case "xml":
		return xmlTypes, xmlAttrs, xmlSimple
	}
	return nil, nil, nil
}

// Slots returns the children of complex type ctype in schema order, slot
// by slot, and whether ctype is tabulated. The result is the generated
// table itself: callers must not modify it.
func Slots(ctype string) ([]Slot, bool) {
	types, _, _ := tables(ctype)
	s, ok := types[ctype]
	return s, ok
}

// Untabulated returns why a WordprocessingML or SpreadsheetML complex
// type that exists in the schema has no child order (its content model is
// not expressible as slots), and whether it is such a type.
func Untabulated(ctype string) (string, bool) {
	var why string
	var ok bool
	switch prefix, _, _ := strings.Cut(ctype, ":"); prefix {
	case "w":
		why, ok = wmlUntabulated[ctype]
	case "x":
		why, ok = smlUntabulated()[ctype]
	}
	return why, ok
}

// Find returns the slot of slots holding the child prefix:local and that
// child, or -1 and the zero Child.
func Find(slots []Slot, prefix, local string) (int, Child) {
	for k, s := range slots {
		for _, c := range s.Names {
			if c.Prefix == prefix && c.Local == local {
				return k, c
			}
		}
	}
	return -1, Child{}
}

// ChildType returns the complex type of child (a qualified name such as
// "a:lumMod") in the tabulated complex type ctype.
func ChildType(ctype, child string) (string, bool) {
	prefix, local, ok := strings.Cut(child, ":")
	if !ok {
		return "", false
	}
	slots, _ := Slots(ctype)
	_, c := Find(slots, prefix, local)
	return c.Type, c.Type != ""
}

// ChildOrder returns the children the schema allows in complex type
// ctype, slot by slot in schema order, each slot the names of a choice as
// prefix:local ("a:buChar"), and whether ctype is tabulated. It is a
// fresh copy of the table.
func ChildOrder(ctype string) ([][]string, bool) {
	slots, ok := Slots(ctype)
	if !ok {
		return nil, false
	}
	out := make([][]string, len(slots))
	for k, s := range slots {
		for _, c := range s.Names {
			out[k] = append(out[k], c.Prefix+":"+c.Local)
		}
	}
	return out, true
}

// AttrType returns the simple type of attribute attr of complex type
// ctype ("a:CT_LineProperties", "w"), as tabulated: the tabulated
// complex types and the types of their children. WordprocessingML
// attributes are qualified in documents (w:val) and tabulated by their
// local name (val).
func AttrType(ctype, attr string) (string, bool) {
	_, attrs, _ := tables(ctype)
	t, ok := attrs[ctype][attr]
	return t, ok
}

// SimpleType returns the tabulated simple type typ ("a:ST_LineWidth").
// Its slices are the table's own: callers must not modify them.
func SimpleType(typ string) (Simple, bool) {
	_, _, simple := tables(typ)
	st, ok := simple[typ]
	return st, ok
}

// Enum returns the enumeration of simple type typ (nil when it has none).
func Enum(typ string) []string {
	st, _ := SimpleType(typ)
	return append([]string(nil), st.Enum...)
}
