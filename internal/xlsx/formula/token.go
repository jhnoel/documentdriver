package formula

import "strings"

// Kind is the class of a token.
type Kind uint8

// The token kinds. Every byte of a formula belongs to exactly one token.
const (
	// Invalid is text the lexer cannot classify: an unterminated string,
	// quoted name or bracket, a quoted name with no "!", or a stray
	// character. Validate refuses it.
	Invalid Kind = iota
	// Space is a run of spaces, tabs, carriage returns and line feeds:
	// padding, or the intersection operator between two operands.
	Space
	// Number is a numeric literal (1, 1.5, .5, 1E5, 1.5e-3); a sign is an
	// Op.
	Number
	// String is a double-quoted literal with "" for a quote.
	String
	// Bool is TRUE or FALSE not followed by "(".
	Bool
	// Error is an error literal such as #REF! or #N/A.
	Error
	// Ref is a cell, area, whole-row or whole-column reference, or #REF!
	// after a sheet prefix, with an optional prefix: a workbook "[n]", a
	// sheet, a 3-D sheet span "First:Last", quoted or bare, and "!".
	Ref
	// Name is a defined name, a LAMBDA or LET parameter ("_xlpm.x"), or a
	// sheet- or workbook-qualified name ("Sheet1!Rate", "[1]!Rate").
	Name
	// Func is a function name, prefix included ("SUM", "_xlfn.XLOOKUP",
	// "_xlfn._xlws.SORT", "ANCHORARRAY"); its "(" is the next token.
	Func
	// StructRef is a table reference: a table name and its bracketed
	// specifier ("Sales[[#This Row],[Qty]]"), or a bare bracketed one.
	StructRef
	// Op is an operator: + - * / ^ & = <> < <= > >= % and the range
	// operator ":".
	Op
	// Comma separates arguments, array columns and union operands.
	Comma
	// Semicolon separates array rows.
	Semicolon
	// LParen, RParen, LBrace and RBrace are the brackets.
	LParen
	RParen
	LBrace
	RBrace
	// Hash is "#" that starts no error literal: the display form of the
	// spill operator, stored as ANCHORARRAY.
	Hash
	// At is "@": the display form of implicit intersection, stored as
	// _xlfn.SINGLE.
	At
)

var kindNames = [...]string{
	Invalid: "invalid", Space: "space", Number: "number", String: "string",
	Bool: "bool", Error: "error", Ref: "ref", Name: "name", Func: "func",
	StructRef: "structref", Op: "op", Comma: "comma", Semicolon: "semicolon",
	LParen: "lparen", RParen: "rparen", LBrace: "lbrace", RBrace: "rbrace",
	Hash: "hash", At: "at",
}

// String names the kind.
func (k Kind) String() string {
	if int(k) < len(kindNames) {
		return kindNames[k]
	}
	return "kind?"
}

// Token is one lexed token: its kind, its text (a substring of the input)
// and the byte offset where it starts.
type Token struct {
	Kind Kind
	Text string
	Pos  int
}

// Join concatenates the token texts: for the tokens of Lex(s) it is s.
func Join(toks []Token) string {
	n := 0
	for _, t := range toks {
		n += len(t.Text)
	}
	var b strings.Builder
	b.Grow(n)
	for _, t := range toks {
		b.WriteString(t.Text)
	}
	return b.String()
}

// errorLiterals are the error constants a formula may contain: the seven
// of ECMA-376 Part 1 §18.17.2 (formula grammar, error-constant) and
// #GETTING_DATA, which [MS-XLSX] §2.2.2 adds.
var errorLiterals = [...]string{
	"#NULL!", "#DIV/0!", "#VALUE!", "#REF!", "#NAME?", "#NUM!", "#N/A", "#GETTING_DATA",
}

// Stored function and parameter prefixes ([MS-XLSX] §2.2.2 and the
// corpus; see functions_table.go for where each was seen).
const (
	// PrefixFuture marks a function newer than the 2007 file format.
	PrefixFuture = "_xlfn."
	// PrefixWorksheet follows PrefixFuture for functions that exist only
	// on worksheets (_xlfn._xlws.SORT).
	PrefixWorksheet = "_xlfn._xlws."
	// PrefixParam marks a LAMBDA or LET parameter name.
	PrefixParam = "_xlpm."
)

// SplitPrefix splits a Func or Name token's text into its stored prefix
// (PrefixWorksheet, PrefixFuture, PrefixParam, or "") and the rest,
// ignoring case.
func SplitPrefix(name string) (prefix, rest string) {
	for _, p := range [...]string{PrefixWorksheet, PrefixFuture, PrefixParam} {
		if len(name) > len(p) && strings.EqualFold(name[:len(p)], p) {
			return name[:len(p)], name[len(p):]
		}
	}
	return "", name
}
