package schema

import "strings"

// Particle is one element a WordprocessingML or SpreadsheetML complex
// type's content model allows (see ContentModel).
type Particle struct {
	// Prefix and Local name the element; Prefix is a table prefix (w, m,
	// wp, sl; x, xdr).
	Prefix, Local string
	// Rank is the element's order rank, or NoRank: a child whose rank is
	// below one already seen is out of order. Ranks are conservative, so
	// an order the schema allows never is.
	Rank int
	// Max is the most times the element may occur, or Unbounded.
	Max int
	// Type is the element's complex type ("w:CT_P", "x:CT_Row"), or ""
	// when it is not a complex type of a vocabulary with a content model.
	Type string
	// Required is whether the element must occur.
	Required bool
}

// ModelAttr is an attribute a WordprocessingML or SpreadsheetML complex
// type allows.
type ModelAttr struct {
	// Prefix ("w", "r", "xml", or "" for an unqualified attribute) and
	// Local name the attribute.
	Prefix, Local string
	// Type is the attribute's simple type as SimpleType names it ("w:ST_Jc",
	// "xsd:string"), or "" when the schema gives none.
	Type string
	// Required is whether the attribute must be present.
	Required bool
}

// Model is the content model of a WordprocessingML or SpreadsheetML
// complex type, as an integrity check reads it: its children with order
// ranks, counts and whether they are required; the namespaces its
// wildcards admit ("##any", "##other" or a list of URIs); and its
// attributes.
type Model struct {
	Children  []Particle
	Wildcards []string
	Attrs     []ModelAttr
	AnyAttr   bool
}

// Rank and count sentinels of a Particle.
const (
	NoRank    = -1
	Unbounded = -1
)

// ContentModel returns the content model of complex type ctype
// ("w:CT_P", "x:CT_Row"), and whether it is one. Every complex type of
// wml.xsd and sml.xsd has a model, including those Slots does not
// tabulate. The slices are the table's own: callers must not modify them.
func ContentModel(ctype string) (Model, bool) {
	var m Model
	var ok bool
	switch prefix, _, _ := strings.Cut(ctype, ":"); prefix {
	case "w":
		m, ok = wmlModels[ctype]
	case "x":
		m, ok = smlModels()[ctype]
	}
	return m, ok
}

// RootType returns the complex type of the global element local of the
// vocabulary with table prefix prefix ("w", "document"; "x",
// "worksheet"), and whether there is one.
func RootType(prefix, local string) (string, bool) {
	var t string
	var ok bool
	switch prefix {
	case "w":
		t, ok = wmlRoots[local]
	case "x":
		t, ok = smlRoots()[local]
	}
	return t, ok
}
