package model

import (
	"strconv"
	"strings"

	"github.com/jhnoel/documentdriver/internal/xlsx/formula"
)

// RefKind is the kind of address `read --range` and `read --sheet` take
// (docs/xlsx-contract-draft.md, §6).
type RefKind uint8

// The address kinds. The zero RefKind is no address.
const (
	// RefSheet is "sheet:Budget": a whole sheet by its stored name.
	RefSheet RefKind = iota + 1
	// RefRange is a sheet-qualified A1 range: a cell ("Budget!B2"), an
	// area ("Budget!A1:D10"), whole rows ("Budget!5:7") or whole columns
	// ("Budget!C:E").
	RefRange
	// RefName is a defined name: "name:Tax_Rate" (workbook scope) or
	// "name:Budget!Local" (scoped to sheet Budget).
	RefName
	// RefTable is "table:Sales": a table by its name (displayName).
	RefTable
	// RefTableColumn is "table:Sales/column:Amount": the data rows of one
	// table column.
	RefTableColumn
)

// Ref is a parsed address. Which fields are set depends on Kind:
//
//   - RefSheet: Sheet.
//   - RefRange: Sheet and Range. Range never carries "$" flags, and an
//     area is normalised so that From is its top-left and To its
//     bottom-right cell ("Budget!B2:A1" parses as A1:B2); Single is set
//     for a lone cell. Rows and Cols ranges are normalised likewise.
//   - RefName: Name, and Sheet when the name is sheet-scoped ("" for
//     workbook scope).
//   - RefTable: Name (the table).
//   - RefTableColumn: Name (the table) and Column.
//
// Sheet is always the unquoted, unescaped sheet name ("Bob's", not
// "'Bob”s'"). Names, tables, columns and sheets are matched against the
// workbook ignoring case (Excel's rule) by the caller, not here.
type Ref struct {
	Kind   RefKind
	Sheet  string
	Range  formula.Range
	Name   string
	Column string
}

// AddressError is why an address was refused: the input and one
// sentence. The caller maps it to invalid_operation with the flag as the
// path (`--range`, `--sheet`).
type AddressError struct {
	Input  string
	Reason string
}

// Error implements error.
func (e *AddressError) Error() string { return "address " + quoteForError(e.Input) + ": " + e.Reason }

// quoteForError writes s in double quotes for an error message, cut to
// 80 bytes so an attacker-sized flag does not fill the errors object.
func quoteForError(s string) string {
	if len(s) > 80 {
		s = s[:80] + "..."
	}
	return `"` + s + `"`
}

// ParseRef parses an address for `read --range`: a sheet-qualified range
// (RefRange), "name:..." (RefName), "table:..." (RefTable) or
// "table:T/column:C" (RefTableColumn). "sheet:..." is also accepted
// (RefSheet), so `--range sheet:Budget` reads the sheet's used range as
// `--sheet` does.
//
// Rules (signed off 2026-11-14, decision 10):
//
//   - A range is always sheet-qualified: "A1" alone is refused.
//   - The sheet is written bare when formula.NeedsQuote(name) is false
//     ("Budget!A1"), else in single quotes with "”" for a quote
//     ("'Q1 Budget'!A1", "'Bob”s'!A1"). A quoted name that does not need
//     quotes is accepted too ("'Budget'!A1"). A bare name that needs
//     quotes ("Q1 Budget!A1"), an empty name, an unterminated or badly
//     escaped quote, and a 3-D span ("Jan:Mar!A1") are refused.
//   - The part after the last "!" outside quotes is parsed with
//     formula.ParseRange. "$" anywhere in it is refused ("$ is not
//     accepted: write Budget!A1"), as are R1C1 forms ("R1C1", "R[1]C"),
//     a workbook prefix ("[1]Budget!A1"), a mixed range ("A1:C") and
//     anything past row 1,048,576 or column XFD.
//   - "name:" is followed by a name, optionally prefixed by a sheet and
//     "!" in the same quoting as a range ("name:'Q1 Budget'!Local").
//     The name itself is not validated beyond being non-empty and
//     holding no "!"; the workbook lookup decides.
//   - "table:" is followed by a non-empty table name; "/column:" and a
//     non-empty column name may follow (the first "/column:" splits).
//   - Leading or trailing spaces anywhere are refused; nothing is
//     trimmed.
//
// The error is an *AddressError.
func ParseRef(s string) (Ref, error) {
	if s == "" {
		return Ref{}, addressErr(s, "no address")
	}
	if s[0] == ' ' || s[len(s)-1] == ' ' {
		return Ref{}, addressErr(s, "leading or trailing spaces are not accepted")
	}
	switch {
	case strings.HasPrefix(s, "sheet:"):
		name := s[len("sheet:"):]
		if name == "" {
			return Ref{}, addressErr(s, "sheet: needs a sheet name")
		}
		if name[0] == ' ' || name[len(name)-1] == ' ' {
			return Ref{}, addressErr(s, "leading or trailing spaces are not accepted")
		}
		return Ref{Kind: RefSheet, Sheet: name}, nil
	case strings.HasPrefix(s, "name:"):
		return parseNameRef(s[len("name:"):], s)
	case strings.HasPrefix(s, "table:"):
		return parseTableRef(s[len("table:"):], s)
	}
	return parseRangeRef(s)
}

// addressErr is the error ParseRef and ParseSheet return.
func addressErr(input, reason string) error {
	return &AddressError{Input: input, Reason: reason}
}

// parseNameRef parses what follows "name:": a name, optionally prefixed
// by a sheet and "!".
func parseNameRef(rest, input string) (Ref, error) {
	sheet, name, err := splitSheet(rest, input)
	if err != nil {
		return Ref{}, err
	}
	if name == "" {
		return Ref{}, addressErr(input, "name: needs a name")
	}
	if name[0] == ' ' || name[len(name)-1] == ' ' {
		return Ref{}, addressErr(input, "leading or trailing spaces are not accepted")
	}
	return Ref{Kind: RefName, Sheet: sheet, Name: name}, nil
}

// parseTableRef parses what follows "table:": a name, optionally followed
// by the first "/column:" and a column name.
func parseTableRef(rest, input string) (Ref, error) {
	if rest == "" {
		return Ref{}, addressErr(input, "table: needs a table name")
	}
	name, col, hasCol := strings.Cut(rest, "/column:")
	if name == "" {
		return Ref{}, addressErr(input, "table: needs a table name")
	}
	if name[0] == ' ' || name[len(name)-1] == ' ' {
		return Ref{}, addressErr(input, "leading or trailing spaces are not accepted")
	}
	if !hasCol {
		return Ref{Kind: RefTable, Name: name}, nil
	}
	if col == "" {
		return Ref{}, addressErr(input, "/column: needs a column name")
	}
	if col[0] == ' ' || col[len(col)-1] == ' ' {
		return Ref{}, addressErr(input, "leading or trailing spaces are not accepted")
	}
	return Ref{Kind: RefTableColumn, Name: name, Column: col}, nil
}

// parseRangeRef parses a sheet-qualified A1 range.
func parseRangeRef(s string) (Ref, error) {
	i := lastBang(s)
	if i < 0 {
		return Ref{}, addressErr(s, "a range must be sheet-qualified, like Budget!A1")
	}
	sheet, err := parseSheetPart(s[:i], s)
	if err != nil {
		return Ref{}, err
	}
	body := s[i+1:]
	if strings.ContainsAny(body, "$") {
		return Ref{}, addressErr(s, "$ is not accepted: write "+formula.SheetPrefix(sheet, "")+"A1")
	}
	r, ok := formula.ParseRange(body)
	if !ok {
		return Ref{}, addressErr(s, "the range part is not an A1 range")
	}
	return Ref{Kind: RefRange, Sheet: sheet, Range: r.Normalized()}, nil
}

// splitSheet splits rest at the last "!" outside single quotes into the
// sheet part (unquoted and unescaped) and the body after it. When there
// is no "!", sheet is "" and body is rest.
func splitSheet(rest, input string) (sheet, body string, err error) {
	i := lastBang(rest)
	if i < 0 {
		return "", rest, nil
	}
	sheet, err = parseSheetPart(rest[:i], input)
	if err != nil {
		return "", "", err
	}
	return sheet, rest[i+1:], nil
}

// lastBang is the index of the last "!" outside single quotes, or -1.
func lastBang(s string) int {
	last := -1
	inQuote := false
	for i := 0; i < len(s); i++ {
		c := s[i]
		if inQuote {
			if c == '\'' {
				if i+1 < len(s) && s[i+1] == '\'' {
					i++ // an escaped quote stays inside the quotes
				} else {
					inQuote = false
				}
			}
			continue
		}
		switch c {
		case '\'':
			inQuote = true
		case '!':
			last = i
		}
	}
	return last
}

// parseSheetPart parses the part before "!": bare when it needs no
// quoting, or fully single-quoted. It returns the unquoted, unescaped
// name.
func parseSheetPart(part, input string) (string, error) {
	if part == "" {
		return "", addressErr(input, "the sheet name is empty")
	}
	if part[0] == ' ' || part[len(part)-1] == ' ' {
		return "", addressErr(input, "leading or trailing spaces are not accepted")
	}
	if part[0] == '\'' {
		return unquoteSheet(part, input)
	}
	if strings.ContainsAny(part, "'") {
		return "", addressErr(input, "a sheet name with a quote must be fully quoted")
	}
	if strings.HasPrefix(part, "[") {
		return "", addressErr(input, "workbook prefixes are not accepted")
	}
	if strings.Contains(part, ":") {
		return "", addressErr(input, "3-D spans are not accepted")
	}
	if formula.NeedsQuote(part) {
		return "", addressErr(input, "the sheet name must be quoted")
	}
	return part, nil
}

// unquoteSheet parses a quoted sheet part (part[0] == '\”) and returns
// the unquoted, unescaped name.
func unquoteSheet(part, input string) (string, error) {
	var b strings.Builder
	for i := 1; i < len(part); i++ {
		c := part[i]
		if c == '\'' {
			if i+1 < len(part) && part[i+1] == '\'' {
				b.WriteByte('\'')
				i++
				continue
			}
			// A lone quote is the closing quote: it must end the part.
			if i != len(part)-1 {
				return "", addressErr(input, "the quoted sheet name is malformed")
			}
			name := b.String()
			if name == "" {
				return "", addressErr(input, "the sheet name is empty")
			}
			if name[0] == ' ' || name[len(name)-1] == ' ' {
				return "", addressErr(input, "leading or trailing spaces are not accepted")
			}
			if strings.Contains(name, ":") {
				return "", addressErr(input, "3-D spans are not accepted")
			}
			return name, nil
		}
		b.WriteByte(c)
	}
	return "", addressErr(input, "the quoted sheet name is not closed")
}

// ParseSheet parses the `read --sheet` and `find --sheet` value: only
// "sheet:" followed by a non-empty name is accepted ("sheet:Q1 Budget";
// the name is taken as it is, without quoting, since a sheet name
// cannot hold ":"). A bare "Budget" is refused with a reason that shows
// the form ("write sheet:Budget"). The error is an *AddressError.
func ParseSheet(s string) (string, error) {
	if name, ok := strings.CutPrefix(s, "sheet:"); ok {
		if name == "" {
			return "", addressErr(s, "sheet: needs a sheet name")
		}
		return name, nil
	}
	return "", addressErr(s, "write sheet:Budget for a sheet")
}

// SheetID is a sheet's address: "sheet:" and the stored name, unquoted.
func SheetID(name string) string { return "sheet:" + name }

// CellKey is the key of a cell in read's `cells` map: the bare A1
// address ("B2"), without sheet and without "$". Row and col are
// 1-based and inside the grid.
func CellKey(row, col int) string {
	return formula.ColumnName(col) + strconv.Itoa(row)
}

// AppendCellKey appends CellKey(row, col) to b, for the streaming
// encoder (no allocation per cell).
func AppendCellKey(b []byte, row, col int) []byte {
	b = formula.AppendColumnName(b, col)
	return strconv.AppendInt(b, int64(row), 10)
}

// Cell is the sheet-qualified address of one cell, quoted as Excel
// quotes it: Cell("Q1 Budget", 1, 1) is "'Q1 Budget'!A1".
func Cell(sheet string, row, col int) string {
	return formula.SheetPrefix(sheet, "") + CellKey(row, col)
}

// Area is the sheet-qualified address of the rectangle r1..r2 x c1..c2
// (1-based, r1 <= r2, c1 <= c2): "Budget!A1:D10", or "Budget!B2" when it
// is one cell.
func Area(sheet string, r1, c1, r2, c2 int) string {
	if r1 == r2 && c1 == c2 {
		return Cell(sheet, r1, c1)
	}
	p := formula.SheetPrefix(sheet, "")
	return p + formula.ColumnName(c1) + strconv.Itoa(r1) + ":" + formula.ColumnName(c2) + strconv.Itoa(r2)
}

// Rows is the sheet-qualified address of whole rows from..to:
// "Budget!5:7", and "Budget!5:5" for one row (Excel's form; a bare "5"
// is not a reference).
func Rows(sheet string, from, to int) string {
	return formula.SheetPrefix(sheet, "") + strconv.Itoa(from) + ":" + strconv.Itoa(to)
}

// Cols is the sheet-qualified address of whole columns from..to
// (1-based): "Budget!C:E", and "Budget!C:C" for one column.
func Cols(sheet string, from, to int) string {
	return formula.SheetPrefix(sheet, "") + formula.ColumnName(from) + ":" + formula.ColumnName(to)
}

// Qualified writes r on sheet in the form ParseRef reads back: the
// sheet prefix (formula.SheetPrefix) and r without "$" flags. It is the
// inverse of ParseRef for RefRange: ParseRef(Qualified(s, r)) gives back
// s and r (normalised).
func Qualified(sheet string, r formula.Range) string {
	r.From.RowAbs, r.From.ColAbs = false, false
	r.To.RowAbs, r.To.ColAbs = false, false
	return formula.SheetPrefix(sheet, "") + r.String()
}
