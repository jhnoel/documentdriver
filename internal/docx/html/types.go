// Package html is the HTML input side of the docx tool, ported from
// docxdriver 0.5.2 (crates/docxdriver-core/src/html input.rs and parse.rs):
// ParseFragment and ParseDocument parse HTML5 with the shared front end
// (internal/htmlx) and lower the supported subset into dialect items.
//
// Errors carry docxdriver's messages verbatim, because agents read them
// and the parity oracle compares them.
package html

import "strings"

// Opt is an optional value, the port of Rust's Option. The zero value is
// absent. Opt is comparable when T is.
type Opt[T any] struct {
	V  T
	OK bool
}

// Some returns a present Opt holding v.
func Some[T any](v T) Opt[T] { return Opt[T]{V: v, OK: true} }

// VertAlign is superscript or subscript (w:vertAlign).
type VertAlign uint8

// The vertical alignments.
const (
	Superscript VertAlign = iota + 1
	Subscript
)

// DialectTag is the dialect tag for the alignment: "sup" or "sub".
func (v VertAlign) DialectTag() string {
	if v == Superscript {
		return "sup"
	}
	return "sub"
}

// DocxVal is the w:vertAlign value: "superscript" or "subscript".
func (v VertAlign) DocxVal() string {
	if v == Superscript {
		return "superscript"
	}
	return "subscript"
}

// FontFamily is a validated CSS font family name (see ParseFontFamily).
type FontFamily string

// Fmt is the run formatting of one character. Resets holds the explicitly
// disabled properties as bits: bold 1, italic 2, underline 4, strike 8,
// baseline 16.
type Fmt struct {
	Resets     uint8
	Bold       bool
	Italic     bool
	Underline  bool
	Strike     bool
	VertAlign  Opt[VertAlign]
	Highlight  Opt[HighlightColor]
	Color      Opt[uint32]
	FontSize   Opt[uint32] // half-points
	FontFamily Opt[FontFamily]
}

// FmtPending is pending run-format revision metadata (w:rPrChange); ID and
// Author are absent for a newly authored change (the engine assigns them).
type FmtPending struct {
	ID     Opt[string]
	Author Opt[string]
}

// WrapID is a revision-wrap identity: absent means not wrapped, a present
// absent ID a new agent-authored wrapper, and a present ID an existing
// revision (Rust's Option<Option<String>>).
type WrapID = Opt[Opt[string]]

// BrKind is the w:br break type as the dialect surfaces it.
type BrKind uint8

// The break kinds.
const (
	BrLine BrKind = iota
	BrPage
	BrColumn
)

// BrKindFromDocx maps a w:br w:type value; anything but page and column
// is a line break.
func BrKindFromDocx(value Opt[string]) BrKind {
	switch {
	case value.OK && value.V == "page":
		return BrPage
	case value.OK && value.V == "column":
		return BrColumn
	}
	return BrLine
}

// BrKindFromDialect maps the dialect's <br type="…"/> value.
func BrKindFromDialect(value Opt[string]) (BrKind, error) {
	switch {
	case !value.OK:
		return BrLine, nil
	case value.V == "page":
		return BrPage, nil
	case value.V == "column":
		return BrColumn, nil
	}
	return BrLine, errorf("unknown break type \"%s\" (expected page or column)", value.V)
}

// DialectType is the dialect type attribute value, absent for a line break.
func (k BrKind) DialectType() Opt[string] {
	switch k {
	case BrPage:
		return Some("page")
	case BrColumn:
		return Some("column")
	}
	return Opt[string]{}
}

// AtomKind is the variant of an Atom.
type AtomKind uint8

// The atom variants.
const (
	AtomChar AtomKind = iota
	AtomBr
	AtomTab
	AtomMilestone
	AtomNote
	AtomEquation
	AtomImage
	// AtomOpaque is a placeholder: existing content the HTML cannot
	// express, by its address (docs/contract.md, section 7).
	AtomOpaque
)

// Atom is one unit of paragraph content (Rust's Atom enum, flattened):
// which fields are meaningful depends on Kind.
//
//   - AtomChar: Text (one or more characters, never a tab or a line
//     end, that share the rest), Fmt, FmtPending, Ins, Del, Link,
//     Field. Rust has one atom per character; a run of them is one atom
//     here, so the items stay proportional to the markup, not to its
//     characters times the atom's size.
//   - AtomBr: Br, Ins, Del, Link, Field.
//   - AtomTab: Ins, Del, Link, Field.
//   - AtomMilestone: Start, ID (a comment range start or end).
//   - AtomNote: Endnote, NoteID, Inner, Ins, Del.
//   - AtomEquation: Text (the LaTeX), OMML, Ref, Display (block), Ins, Del.
//   - AtomImage: Src, RID, Width, Height, Alt, Ins, Del, Link, Field.
//   - AtomOpaque: Ref (its address, the paragraph part upper-cased),
//     PlaceholderKind (data-kind, derived), Text (the element's text
//     content, which must be the content's), Ins, Del, Link, Field.
type Atom struct {
	Kind       AtomKind
	Fmt        Fmt
	FmtPending Opt[FmtPending]
	Br         BrKind
	Ins, Del   WrapID
	Link       Opt[string]
	Field      Opt[string]
	Text       string
	// *AtomObject holds the fields of milestones, notes, equations and
	// images (nil for the other kinds), so that a run of characters,
	// the common atom, does not carry them.
	*AtomObject
}

// AtomObject is the part of an Atom only milestones, notes, equations
// and images have.
type AtomObject struct {
	Start   bool
	ID      string
	Endnote bool
	NoteID  Opt[string]
	Inner   string
	OMML    string
	// Ref is an equation's or a placeholder's data-ref: the address of
	// the existing content it stands for ("" for a new equation).
	Ref string
	// PlaceholderKind is a placeholder's data-kind as sent (derived:
	// never checked against the content).
	PlaceholderKind string
	Display         bool
	Src, RID        string
	Width           Opt[uint32]
	Height          Opt[uint32]
	Alt             Opt[string]
}

// ListAttrs is the list membership of a paragraph.
type ListAttrs struct {
	Continuation bool
	ID           uint32
	Level        uint32
	Format       string
	Start        uint32
}

// ParaAttrs is paragraph open-tag data. Tag is "p" or "h1".."h6".
type ParaAttrs struct {
	Tag      string
	Class    Opt[string]
	BreakIns WrapID
	BreakDel WrapID
	Num      Opt[string]
	List     Opt[ListAttrs]
	CSS      Opt[string]
}

// ItemKind is the variant of an Item.
type ItemKind uint8

// The item variants.
const (
	ItemParaOpen ItemKind = iota
	ItemParaClose
	ItemAtom
	ItemStruct
)

// Item is a parsed fragment item: a paragraph open (Para), a paragraph
// close, an atom (Atom), or a table-structure token carried verbatim
// (Struct: "table", "/table", "tr", "/tr", "td …", "/td").
type Item struct {
	Kind ItemKind
	// Para is an ItemParaOpen's attributes (nil otherwise): by pointer,
	// since every character run is an Item too.
	Para   *ParaAttrs
	Atom   Atom
	Struct string
	// Table is an opening table, row or cell token's attributes (nil
	// otherwise).
	Table *TableAttrs
}

// TableAttrs are the attributes of a table, row or cell as written:
// a cell's colspan and rowspan, and the style document HTML may give
// any of them (the lowering checks the values).
type TableAttrs struct {
	Colspan, Rowspan Opt[string]
	CSS              Opt[string]
}

// EscapeText escapes &, < and > for element content.
func EscapeText(text string) string {
	if !strings.ContainsAny(text, "&<>") {
		return text
	}
	var b strings.Builder
	b.Grow(len(text) + 8)
	for _, r := range text {
		switch r {
		case '&':
			b.WriteString("&amp;")
		case '<':
			b.WriteString("&lt;")
		case '>':
			b.WriteString("&gt;")
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

// EscapeAttr escapes &, <, > and " for a double-quoted attribute value.
func EscapeAttr(text string) string {
	if !strings.ContainsAny(text, "&<>\"") {
		return text
	}
	var b strings.Builder
	b.Grow(len(text) + 8)
	for _, r := range text {
		switch r {
		case '&':
			b.WriteString("&amp;")
		case '<':
			b.WriteString("&lt;")
		case '>':
			b.WriteString("&gt;")
		case '"':
			b.WriteString("&quot;")
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}
