package formula

import (
	"strconv"
	"strings"
)

// Grid limits of a worksheet (ECMA-376 Part 1 §18.3.1.73 and the
// ST_CellRef range: rows 1 to 1,048,576, columns A to XFD).
const (
	MaxRows = 1 << 20
	MaxCols = 1 << 14
)

// Cell is one A1 cell reference. Row and Col are 1-based; RowAbs and
// ColAbs are the "$" flags.
type Cell struct {
	Row, Col       int
	RowAbs, ColAbs bool
}

// String writes the cell in A1 form with its "$" flags.
func (c Cell) String() string {
	var b strings.Builder
	c.write(&b)
	return b.String()
}

// write appends the cell in A1 form with its "$" flags to b.
func (c Cell) write(b *strings.Builder) {
	if c.ColAbs {
		b.WriteByte('$')
	}
	b.WriteString(ColumnName(c.Col))
	if c.RowAbs {
		b.WriteByte('$')
	}
	b.WriteString(strconv.Itoa(c.Row))
}

// RangeKind says which coordinates a Range carries.
type RangeKind uint8

// The kinds of range.
const (
	// Cells is a cell or a rectangle of cells (A1, A1:B2).
	Cells RangeKind = iota
	// Rows is a run of whole rows (1:3); its Col fields are unused.
	Rows
	// Cols is a run of whole columns (A:C); its Row fields are unused.
	Cols
)

// Range is an A1 range: a cell, an area, whole rows or whole columns.
// From and To are as written (a reversed area such as B2:A1 is kept
// reversed); Single marks a lone cell written without ":".
type Range struct {
	Kind     RangeKind
	From, To Cell
	Single   bool
}

// String writes the range in A1 form, a lone cell without ":".
func (r Range) String() string {
	var b strings.Builder
	r.write(&b)
	return b.String()
}

// write appends the range in A1 form to b, a lone cell without ":".
func (r Range) write(b *strings.Builder) {
	switch r.Kind {
	case Rows:
		writeRow(b, r.From)
		b.WriteByte(':')
		writeRow(b, r.To)
	case Cols:
		writeCol(b, r.From)
		b.WriteByte(':')
		writeCol(b, r.To)
	default:
		r.From.write(b)
		if !r.Single {
			b.WriteByte(':')
			r.To.write(b)
		}
	}
}

// writeRow appends the row number in A1 form to b, with "$" when absolute.
func writeRow(b *strings.Builder, c Cell) {
	if c.RowAbs {
		b.WriteByte('$')
	}
	b.WriteString(strconv.Itoa(c.Row))
}

// writeCol appends the column name in A1 form to b, with "$" when absolute.
func writeCol(b *strings.Builder, c Cell) {
	if c.ColAbs {
		b.WriteByte('$')
	}
	b.WriteString(ColumnName(c.Col))
}

// Bounds returns the rows and columns the range covers, smallest first;
// a Rows range covers every column and a Cols range every row.
func (r Range) Bounds() (r1, c1, r2, c2 int) {
	switch r.Kind {
	case Rows:
		r1, r2 = order(r.From.Row, r.To.Row)
		return r1, 1, r2, MaxCols
	case Cols:
		c1, c2 = order(r.From.Col, r.To.Col)
		return 1, c1, MaxRows, c2
	}
	r1, r2 = order(r.From.Row, r.To.Row)
	c1, c2 = order(r.From.Col, r.To.Col)
	return r1, c1, r2, c2
}

// Contains reports whether the cell at row, col is inside the range.
func (r Range) Contains(row, col int) bool {
	r1, c1, r2, c2 := r.Bounds()
	return row >= r1 && row <= r2 && col >= c1 && col <= c2
}

// order returns a and b smallest first.
func order(a, b int) (int, int) {
	if a > b {
		return b, a
	}
	return a, b
}

// ColumnName is the letters of 1-based column n (1 is A, 16384 is XFD).
// It returns "" for n below 1.
func ColumnName(n int) string {
	var buf [8]byte
	return string(AppendColumnName(buf[:0], n))
}

// ColumnIndex is the 1-based column of letters such as "A" or "xfd"; ok is
// false when s is not one to three letters or is past XFD.
func ColumnIndex(s string) (int, bool) {
	if len(s) == 0 || len(s) > 3 {
		return 0, false
	}
	n := 0
	for i := 0; i < len(s); i++ {
		c := s[i] | 0x20
		if c < 'a' || c > 'z' {
			return 0, false
		}
		n = n*26 + int(c-'a') + 1
	}
	return n, n <= MaxCols
}

// scanCol reads "$"? letters{1,3} at s[i:], returning the column, its flag
// and the end. It does not check bounds beyond three letters.
func scanCol(s string, i int) (col int, abs bool, end int, ok bool) {
	if i < len(s) && s[i] == '$' {
		abs = true
		i++
	}
	j := i
	for j < len(s) && j-i < 4 && isASCIILetter(s[j]) {
		j++
	}
	if j == i || j-i > 3 {
		return 0, false, 0, false
	}
	col, ok = ColumnIndex(s[i:j])
	return col, abs, j, ok
}

// scanRow reads "$"? digits at s[i:], returning the row, its flag and the
// end; ok is false for no digits or a row outside 1 to MaxRows.
func scanRow(s string, i int) (row int, abs bool, end int, ok bool) {
	if i < len(s) && s[i] == '$' {
		abs = true
		i++
	}
	j := i
	for j < len(s) && isDigit(s[j]) {
		if j-i > 7 {
			return 0, false, 0, false
		}
		row = row*10 + int(s[j]-'0')
		j++
	}
	if j == i || row < 1 || row > MaxRows {
		return 0, false, 0, false
	}
	return row, abs, j, true
}

// scanCell reads one A1 cell at s[i:] and returns its end.
func scanCell(s string, i int) (Cell, int, bool) {
	col, cabs, j, ok := scanCol(s, i)
	if !ok {
		return Cell{}, 0, false
	}
	row, rabs, k, ok := scanRow(s, j)
	if !ok {
		return Cell{}, 0, false
	}
	return Cell{Row: row, Col: col, RowAbs: rabs, ColAbs: cabs}, k, true
}

// scanRange reads the longest range at s[i:]: an area, a cell, whole
// columns or whole rows. It does not look at what follows.
func scanRange(s string, i int) (Range, int, bool) {
	if c, j, ok := scanCell(s, i); ok {
		if j < len(s) && s[j] == ':' {
			if d, k, ok := scanCell(s, j+1); ok {
				return Range{Kind: Cells, From: c, To: d}, k, true
			}
		}
		return Range{Kind: Cells, From: c, To: c, Single: true}, j, true
	}
	if c, cabs, j, ok := scanCol(s, i); ok && j < len(s) && s[j] == ':' {
		if d, dabs, k, ok := scanCol(s, j+1); ok {
			return Range{Kind: Cols, From: Cell{Col: c, ColAbs: cabs}, To: Cell{Col: d, ColAbs: dabs}}, k, true
		}
	}
	if r, rabs, j, ok := scanRow(s, i); ok && j < len(s) && s[j] == ':' {
		if q, qabs, k, ok := scanRow(s, j+1); ok {
			return Range{Kind: Rows, From: Cell{Row: r, RowAbs: rabs}, To: Cell{Row: q, RowAbs: qabs}}, k, true
		}
	}
	return Range{}, 0, false
}

// ParseCell parses one A1 cell such as "B2" or "$B$2" (letters in either
// case).
func ParseCell(s string) (Cell, bool) {
	c, end, ok := scanCell(s, 0)
	if !ok || end != len(s) {
		return Cell{}, false
	}
	return c, true
}

// ParseRange parses an A1 range: "A1", "A1:B2", "A:C", "1:3", each part
// with optional "$" flags.
func ParseRange(s string) (Range, bool) {
	r, end, ok := scanRange(s, 0)
	if !ok || end != len(s) {
		return Range{}, false
	}
	return r, true
}

// ParseSqref parses a space-separated list of ranges (ST_Sqref), as in a
// mergeCell, conditional formatting or data validation sqref. Runs of
// spaces between ranges are accepted; an empty list is not.
func ParseSqref(s string) ([]Range, bool) {
	var out []Range
	for f := range strings.FieldsSeq(s) {
		r, ok := ParseRange(f)
		if !ok {
			return nil, false
		}
		out = append(out, r)
	}
	if len(out) == 0 {
		return nil, false
	}
	return out, true
}

// LooksLikeCellRef reports whether s, ignoring case, reads as a cell
// reference in A1 form ("A1", "xfd1048576", with or without "$") or R1C1
// form ("R", "C", "RC", "R1", "C2", "R1C1", "R[-1]C[2]"). A sheet or
// defined name that looks like one cannot be written bare.
func LooksLikeCellRef(s string) bool {
	if _, ok := ParseCell(s); ok {
		return true
	}
	return looksLikeR1C1(s)
}

// looksLikeR1C1 matches R<part>?C<part>? with at least one of R and C,
// where a part is digits or a bracketed signed offset, ignoring case.
func looksLikeR1C1(s string) bool {
	i := 0
	seen := false
	if i < len(s) && (s[i]|0x20) == 'r' {
		seen = true
		i++
		i = r1c1Part(s, i)
		if i < 0 {
			return false
		}
	}
	if i < len(s) && (s[i]|0x20) == 'c' {
		seen = true
		i++
		i = r1c1Part(s, i)
		if i < 0 {
			return false
		}
	}
	return seen && i == len(s)
}

// r1c1Part skips digits or "[" signed digits "]" at s[i:]; -1 means a
// malformed bracket.
func r1c1Part(s string, i int) int {
	if i < len(s) && s[i] == '[' {
		j := i + 1
		if j < len(s) && (s[j] == '-' || s[j] == '+') {
			j++
		}
		k := j
		for k < len(s) && isDigit(s[k]) {
			k++
		}
		if k == j || k >= len(s) || s[k] != ']' {
			return -1
		}
		return k + 1
	}
	for i < len(s) && isDigit(s[i]) {
		i++
	}
	return i
}

// isASCIILetter reports whether c is an ASCII letter.
func isASCIILetter(c byte) bool { return c|0x20 >= 'a' && c|0x20 <= 'z' }

// isDigit reports whether c is an ASCII digit.
func isDigit(c byte) bool { return c >= '0' && c <= '9' }
