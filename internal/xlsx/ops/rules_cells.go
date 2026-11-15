package ops

import (
	"github.com/jhnoel/documentdriver/internal/xlsx/formula"
)

// range.set's rules (designer-owned).
var rangeSetRules = []Rule{
	rule("at must be a cell or an area, not whole rows or columns", "at",
		func(o *RangeSet) bool { return o.At.Kind != formula.Cells },
		`{"op":"range.set","at":"Budget!1:2","values":[[1]]}`),
	rule("values must fill at exactly: as many rows and columns as the range", "values",
		func(o *RangeSet) bool {
			if o.At.Single || len(o.Values) == 0 {
				return false
			}
			return len(o.Values) != o.At.R2-o.At.R1+1 || len(o.Values[0]) != o.At.C2-o.At.C1+1
		},
		`{"op":"range.set","at":"Budget!A1:B2","values":[[1,2]]}`),
	rule("values run past the last row or column of the sheet", "values",
		func(o *RangeSet) bool {
			if len(o.Values) == 0 {
				return false
			}
			return o.At.R1+len(o.Values)-1 > formula.MaxRows || o.At.C1+len(o.Values[0])-1 > formula.MaxCols
		},
		`{"op":"range.set","at":"Budget!XFD1","values":[[1,2]]}`),
}
