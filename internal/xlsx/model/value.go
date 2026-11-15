package model

import (
	"strings"
	"unicode/utf8"
)

// ValueKind is the form of a cell value (docs/xlsx-contract-draft.md,
// §7a). The zero ValueKind is no value: the cell stores none.
type ValueKind uint8

// The value forms.
const (
	// None is no stored value (a formula without a cached result, or a
	// spill member whose <v> is missing).
	None ValueKind = iota
	// Number is a finite double, printed by FormatNumber.
	Number
	// String is text, printed as a JSON string, or as {"text":"=..."}
	// when it starts with "=".
	String
	// Bool is true or false.
	Bool
	// Error is one of Excel's error values, {"error":"#N/A"}.
	Error
	// Date is {"date":"YYYY-MM-DD"} or {"date":"YYYY-MM-DDTHH:MM:SS"}
	// (numfmt.Value with Key "date").
	Date
	// Time is {"time":"HH:MM:SS"} (numfmt.Value with Key "time").
	Time
)

// Value is one cell value in its agent form.
//
//   - Number: Num, always finite (a reader never builds a NaN or an
//     infinity: it leaves the cell out with a note).
//   - String: Text, already unescaped (_xHHHH_ decoded) and flattened
//     (rich text runs joined, phonetic runs dropped).
//   - Bool: Bool.
//   - Error: Text, the error literal as stored ("#N/A", "#DIV/0!").
//   - Date, Time: Text, the numfmt.Value text.
type Value struct {
	Kind ValueKind
	Num  float64
	Text string
	Bool bool
}

// AppendJSON appends the value's JSON form to b:
//
//	Number  1200, 0.5, -3e-7 (FormatNumber)
//	String  "Item"; {"text":"=literal"} when Text starts with "="
//	Bool    true, false
//	Error   {"error":"#N/A"}
//	Date    {"date":"2024-03-01"}, {"date":"2024-03-01T13:30:00"}
//	Time    {"time":"13:30:00"}
//	None    nothing: b is returned unchanged (the caller leaves the key
//	        out).
//
// Strings are escaped as encoding/json escapes them without HTML
// escaping (cli.Marshal's setting): `"`, `\` and control characters, and
// invalid UTF-8 as U+FFFD. Object keys are in the order shown.
func (v Value) AppendJSON(b []byte) []byte {
	switch v.Kind {
	case Number:
		return AppendNumber(b, v.Num)
	case String:
		if strings.HasPrefix(v.Text, "=") {
			b = append(b, `{"text":`...)
			b = appendJSONString(b, v.Text)
			return append(b, '}')
		}
		return appendJSONString(b, v.Text)
	case Bool:
		if v.Bool {
			return append(b, "true"...)
		}
		return append(b, "false"...)
	case Error:
		b = append(b, `{"error":`...)
		b = appendJSONString(b, v.Text)
		return append(b, '}')
	case Date:
		b = append(b, `{"date":`...)
		b = appendJSONString(b, v.Text)
		return append(b, '}')
	case Time:
		b = append(b, `{"time":`...)
		b = appendJSONString(b, v.Text)
		return append(b, '}')
	}
	return b
}

// MatchText is the text `find --view values` matches the query against
// (approved shape, docs/xlsx-contract-draft.md): the value as read
// prints it, without JSON quoting or object wrapping.
//
//	Number  FormatNumber ("1200", "0.5")
//	String  Text (with its "=", if any)
//	Bool    "true", "false"
//	Error   Text ("#N/A")
//	Date    Text ("2024-03-01T13:30:00")
//	Time    Text ("13:30:00")
//	None    "" (and find never matches a cell with no value)
func (v Value) MatchText() string {
	switch v.Kind {
	case Number:
		return FormatNumber(v.Num)
	case String, Error, Date, Time:
		return v.Text
	case Bool:
		if v.Bool {
			return "true"
		}
		return "false"
	}
	return ""
}

// View is the projection of read's cells (decision 2) and of find's
// matching text.
type View uint8

// The views. ViewCells is the default of read; find takes ViewValues
// (its default) and ViewFormulas only.
const (
	ViewCells View = iota
	ViewValues
	ViewFormulas
)

// Views are the --view values of read, in help order.
var Views = []string{"cells", "values", "formulas"}

// ParseView maps a --view value to a View; ok is false for any other
// string (the cli Enum check refuses those first).
func ParseView(s string) (View, bool) {
	switch s {
	case "cells":
		return ViewCells, true
	case "values":
		return ViewValues, true
	case "formulas":
		return ViewFormulas, true
	}
	return 0, false
}

// String is the --view value.
func (v View) String() string {
	if int(v) < len(Views) {
		return Views[v]
	}
	return "view(" + string(rune('0'+v)) + ")"
}

// ArrayKind says what kind of multi-cell formula a cell anchors or
// belongs to.
type ArrayKind uint8

// The array kinds.
const (
	// NoArray: the cell is not part of an array formula.
	NoArray ArrayKind = iota
	// Dynamic: a dynamic-array formula (the anchor's cm resolves to
	// XLDAPR fDynamic in xl/metadata.xml). Members are marked with
	// "spill" only.
	Dynamic
	// CSE: a legacy array formula (t="array" whose anchor has no
	// dynamic-array metadata). Anchor and members carry "array":"cse"
	// (approved shape).
	CSE
	// DataTable: a what-if data table (t="dataTable"). Anchor and members
	// carry "array":"data_table" (approved shape).
	DataTable
	// UnknownArray: t="array" with a cm the metadata part cannot
	// resolve (missing or unreadable): members are marked with "spill"
	// and no "array", and a note says why.
	UnknownArray
)

// Mark is the "array" member's value for k: "cse", "data_table", or ""
// when no "array" key is written (NoArray, Dynamic, UnknownArray).
func (k ArrayKind) Mark() string {
	switch k {
	case CSE:
		return "cse"
	case DataTable:
		return "data_table"
	}
	return ""
}

// Entry is one cell as read shows it, before the view is applied.
type Entry struct {
	// Value is the stored value (the cached result for a formula cell),
	// None when there is none.
	Value Value
	// Formula is the formula in Excel's display form with a leading "="
	// (formula.Display of the stored text, a shared child's text written
	// out by formula.Translate first). "" when the cell has no formula or
	// its formula could not be derived (then a note says why).
	Formula string
	// Spill is set on a non-anchor member of an array formula, a dynamic
	// spill or a data table: the anchor's sheet-qualified address
	// ("Budget!A1").
	Spill string
	// Array is the kind of the array formula the cell anchors (Formula
	// set, Spill empty) or belongs to (Spill set).
	Array ArrayKind
}

// Shown reports whether the entry has a key in view: whether
// AppendJSON writes anything.
//
//	ViewCells     a value, a formula or a spill mark
//	ViewValues    a value
//	ViewFormulas  a formula or a spill mark
func (e Entry) Shown(view View) bool {
	switch view {
	case ViewValues:
		return e.Value.Kind != None
	case ViewFormulas:
		return e.Formula != "" || e.Spill != "" || e.Array.Mark() != ""
	}
	return e.Value.Kind != None || e.Formula != "" || e.Spill != "" || e.Array.Mark() != ""
}

// AppendJSON appends the entry's JSON form in view to b (nothing when
// !e.Shown(view)). The forms (decisions 2 and 8, and the approved shapes
// of docs/xlsx-contract-draft.md):
//
// ViewCells:
//
//	plain value            the Value form ("Item", 1200, {"date":...})
//	formula                {"f":"=B2+C2","v":2400}, "v" left out when Value is None
//	CSE or data-table      {"f":"=...","v":...,"array":"cse"} (or "data_table")
//	  anchor
//	member                 {"v":3,"spill":"Budget!A1"}, and "array":"cse" or
//	                       "data_table" for those kinds; "v" left out when None
//
// ViewValues: the Value form only, for every kind of cell (members and
// anchors alike; no "spill" or "array").
//
// ViewFormulas: a formula as a JSON string ("=B2+C2"); a CSE or
// data-table anchor as {"f":"=...","array":"cse"}; a member as
// {"spill":"Budget!A1"} with "array" for those kinds (approved).
//
// An anchor whose formula was not derived (Formula "", a note says why)
// is written as its plain value in ViewCells, or as {"v":...,"array":...}
// when Array has a Mark; in ViewFormulas it is not shown unless Array has
// a Mark ({"array":"data_table"}).
//
// Keys are written in the order f, v, spill, array.
func (e Entry) AppendJSON(b []byte, view View) []byte {
	mark := e.Array.Mark()
	switch view {
	case ViewValues:
		return e.Value.AppendJSON(b)
	case ViewFormulas:
		if e.Spill != "" {
			b = append(b, `{"spill":`...)
			b = appendJSONString(b, e.Spill)
			if mark != "" {
				b = append(b, `,"array":`...)
				b = appendJSONString(b, mark)
			}
			return append(b, '}')
		}
		if e.Formula != "" {
			if mark != "" {
				b = append(b, `{"f":`...)
				b = appendJSONString(b, e.Formula)
				b = append(b, `,"array":`...)
				b = appendJSONString(b, mark)
				return append(b, '}')
			}
			return appendJSONString(b, e.Formula)
		}
		if mark != "" {
			b = append(b, `{"array":`...)
			b = appendJSONString(b, mark)
			return append(b, '}')
		}
		return b
	}
	// ViewCells.
	if e.Spill != "" {
		b = append(b, '{')
		if e.Value.Kind != None {
			b = append(b, `"v":`...)
			b = e.Value.AppendJSON(b)
			b = append(b, ',')
		}
		b = append(b, `"spill":`...)
		b = appendJSONString(b, e.Spill)
		if mark != "" {
			b = append(b, `,"array":`...)
			b = appendJSONString(b, mark)
		}
		return append(b, '}')
	}
	if e.Formula != "" {
		b = append(b, `{"f":`...)
		b = appendJSONString(b, e.Formula)
		if e.Value.Kind != None {
			b = append(b, `,"v":`...)
			b = e.Value.AppendJSON(b)
		}
		if mark != "" {
			b = append(b, `,"array":`...)
			b = appendJSONString(b, mark)
		}
		return append(b, '}')
	}
	if mark != "" {
		b = append(b, '{')
		if e.Value.Kind != None {
			b = append(b, `"v":`...)
			b = e.Value.AppendJSON(b)
			b = append(b, ',')
		}
		b = append(b, `"array":`...)
		b = appendJSONString(b, mark)
		return append(b, '}')
	}
	return e.Value.AppendJSON(b)
}

// appendJSONString appends s as a JSON string, escaped as encoding/json
// escapes it without HTML escaping (cli.Marshal's setting): `"`, `\` and
// control characters, U+2028 and U+2029, and invalid UTF-8 as U+FFFD.
func appendJSONString(b []byte, s string) []byte {
	const hex = "0123456789abcdef"
	b = append(b, '"')
	start := 0
	for i := 0; i < len(s); {
		if c := s[i]; c < utf8.RuneSelf {
			if c >= 0x20 && c != '"' && c != '\\' {
				i++
				continue
			}
			b = append(b, s[start:i]...)
			switch c {
			case '\\', '"':
				b = append(b, '\\', c)
			case '\b':
				b = append(b, '\\', 'b')
			case '\f':
				b = append(b, '\\', 'f')
			case '\n':
				b = append(b, '\\', 'n')
			case '\r':
				b = append(b, '\\', 'r')
			case '\t':
				b = append(b, '\\', 't')
			default:
				b = append(b, '\\', 'u', '0', '0', hex[c>>4], hex[c&0xF])
			}
			i++
			start = i
			continue
		}
		c, size := utf8.DecodeRuneInString(s[i:])
		if c == utf8.RuneError && size == 1 {
			b = append(b, s[start:i]...)
			b = append(b, "\uFFFD"...)
			i += size
			start = i
			continue
		}
		if c == '\u2028' || c == '\u2029' {
			b = append(b, s[start:i]...)
			b = append(b, '\\', 'u', '2', '0', '2', hex[c&0xF])
			i += size
			start = i
			continue
		}
		i += size
	}
	b = append(b, s[start:]...)
	b = append(b, '"')
	return b
}

// Excel's limits on what a cell holds (§7a): text of at most MaxText
// characters and formulas of at most MaxFormula, both counted in UTF-16
// code units as Excel counts. A reader leaves a longer stored text or
// formula out with a note instead of printing it: a file cannot hold one
// Excel wrote, and one shared string of a megabyte referenced by every
// cell would otherwise multiply into gigabytes of output (the corpus has
// such a file, apache-poi poc-shared-strings.xlsx).
const (
	MaxText    = 32767
	MaxFormula = 8192
)

// TextLen is the length of s in UTF-16 code units, Excel's character
// count. It stops counting past limit (returning limit+1) so a huge
// string costs no more than the limit.
func TextLen(s string, limit int) int {
	n := 0
	for _, r := range s {
		n++
		if r >= 0x10000 {
			n++
		}
		if n > limit {
			return limit + 1
		}
	}
	return n
}
