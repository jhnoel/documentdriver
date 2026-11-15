package integrity

import (
	"strconv"
	"strings"

	"github.com/jhnoel/documentdriver/internal/xlsx/formula"
	"github.com/jhnoel/documentdriver/internal/xlsx/sml"
)

// Excel's limits, as findings (not opc.Limits).
const (
	maxRows        = 1048576
	maxCols        = 16384
	maxCellChars   = 32767
	maxFormulaLen  = 8192
	maxSheetName   = 31
	maxDefinedName = 255
)

// parseCell reads an A1 cell reference (no sheet, no $) into its 1-based
// column and row. The values may exceed Excel's limits; ok is false only
// when s is not letters followed by digits, or is absurdly long.
//
// It is not formula.ParseCell, which refuses a column past XFD or a row
// past 1,048,576: a cell there must still be read, so an out-of-range
// column is a LimitColumn finding and a mismatched row is
// WorksheetCellRow, not a malformed reference.
func parseCell(s string) (col, row int, ok bool) {
	i := 0
	for i < len(s) && (s[i] >= 'A' && s[i] <= 'Z' || s[i] >= 'a' && s[i] <= 'z') {
		i++
	}
	if i == 0 || i > 4 {
		return 0, 0, false
	}
	for j := 0; j < i; j++ {
		c := s[j] &^ 0x20
		col = col*26 + int(c-'A') + 1
	}
	row, ok = sml.ParseIndex(s[i:])
	if !ok || row == 0 {
		return 0, 0, false
	}
	return col, row, true
}

// rect is a range's corners, 1-based and inclusive.
type rect struct{ c1, r1, c2, r2 int }

// parseRange reads "A1" or "A1:B2" (corners in any order are normalised)
// into a rect.
//
// It is not formula.ParseRange: that parser refuses a column past XFD or
// a row past 1,048,576. A range whose corners exceed the grid still has
// to be read here — a merge, array or table ref then fails its own
// inLimits check, and a dimension names it without a finding — so the
// corners must come through beyond the limits.
func parseRange(s string) (rect, bool) {
	a, b, two := strings.Cut(s, ":")
	c1, r1, ok := parseCell(a)
	if !ok {
		return rect{}, false
	}
	c2, r2 := c1, r1
	if two {
		if c2, r2, ok = parseCell(b); !ok {
			return rect{}, false
		}
	}
	return rect{min(c1, c2), min(r1, r2), max(c1, c2), max(r1, r2)}, true
}

// inLimits reports whether the rect's far corner is within Excel's grid limits.
func (r rect) inLimits() bool { return r.c2 <= maxCols && r.r2 <= maxRows }

// overlaps reports whether the rect and o share at least one cell.
func (r rect) overlaps(o rect) bool {
	return r.c1 <= o.c2 && o.c1 <= r.c2 && r.r1 <= o.r2 && o.r1 <= r.r2
}

// contains reports whether the cell at column col and row row is in the rect.
func (r rect) contains(col, row int) bool {
	return col >= r.c1 && col <= r.c2 && row >= r.r1 && row <= r.r2
}

// cellName is the A1 name of a cell.
func cellName(col, row int) string { return formula.ColumnName(col) + strconv.Itoa(row) }
