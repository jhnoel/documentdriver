package formula

import "strings"

// ParseArea parses an A1 cell or area such as "B2:D10" or "c5:a1" (no
// whole rows or columns; spaces around it are trimmed) and normalises
// it: From top-left, To bottom-right, no "$" flags, Single when it is
// one cell (A1:A1 included). It reports false for anything ParseRange
// refuses and for row or column ranges.
func ParseArea(s string) (Range, bool) {
	r, ok := ParseRange(strings.TrimSpace(s))
	if !ok || r.Kind != Cells {
		return Range{}, false
	}
	r1, c1, r2, c2 := r.Bounds()
	return Range{
		Kind:   Cells,
		From:   Cell{Row: r1, Col: c1},
		To:     Cell{Row: r2, Col: c2},
		Single: r1 == r2 && c1 == c2,
	}, true
}

// Normalized returns r without "$" flags, From at the top-left and To at
// the bottom-right (for whole rows, the lower row first; for whole
// columns, the left one). A Single cell is returned as it is, flags
// cleared; Single is never set or cleared.
func (r Range) Normalized() Range {
	r.From.RowAbs, r.From.ColAbs = false, false
	r.To.RowAbs, r.To.ColAbs = false, false
	switch r.Kind {
	case Rows:
		if r.From.Row > r.To.Row {
			r.From.Row, r.To.Row = r.To.Row, r.From.Row
		}
	case Cols:
		if r.From.Col > r.To.Col {
			r.From.Col, r.To.Col = r.To.Col, r.From.Col
		}
	default:
		if !r.Single {
			if r.From.Row > r.To.Row {
				r.From.Row, r.To.Row = r.To.Row, r.From.Row
			}
			if r.From.Col > r.To.Col {
				r.From.Col, r.To.Col = r.To.Col, r.From.Col
			}
		}
	}
	return r
}

// AppendColumnName appends the letters of 1-based column n to b (1 is
// A, 16384 is XFD) and returns the extended slice; nothing for n below
// 1. It does not allocate beyond b's growth. ColumnName is its string
// form.
func AppendColumnName(b []byte, n int) []byte {
	if n < 1 {
		return b
	}
	var tmp [8]byte
	i := len(tmp)
	for n > 0 {
		n--
		i--
		tmp[i] = byte('A' + n%26)
		n /= 26
	}
	return append(b, tmp[i:]...)
}

// QuoteSheet returns sheet name as a reference prefix writes it, without
// the "!": bare when NeedsQuote is false, else in single quotes with each
// quote doubled: "Q1 Budget" becomes 'Q1 Budget', and a name holding a
// quote gets it twice inside the quotes.
func QuoteSheet(name string) string {
	if NeedsQuote(name) {
		return "'" + strings.ReplaceAll(name, "'", "''") + "'"
	}
	return name
}
