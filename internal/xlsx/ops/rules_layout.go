package ops

import "github.com/jhnoel/documentdriver/internal/xlsx/formula"

// row.format, column.format, cell.merge and cell.split rules (designer-owned).
var (
	rowFormatRules = []Rule{
		rule("give height, hidden or both", "",
			func(o *RowFormat) bool { return o.Height == nil && o.Hidden == nil },
			`{"op":"row.format","at":"Budget!5:5"}`),
		rule("height must be from 0 to 409 points", "height",
			func(o *RowFormat) bool { return o.Height != nil && (*o.Height < 0 || *o.Height > 409) },
			`{"op":"row.format","at":"Budget!5:5","height":500}`),
	}
	columnFormatRules = []Rule{
		rule("give width, hidden or both", "",
			func(o *ColumnFormat) bool { return o.Width == nil && o.Hidden == nil },
			`{"op":"column.format","at":"Budget!C:C"}`),
		rule("width must be from 0 to 255 characters", "width",
			func(o *ColumnFormat) bool { return o.Width != nil && (*o.Width < 0 || *o.Width > 255) },
			`{"op":"column.format","at":"Budget!C:C","width":300}`),
	}
	mergeRules = []Rule{
		rule("at must be an area of two or more cells", "at",
			func(o *CellMerge) bool { return !isArea(o.At) },
			`{"op":"cell.merge","at":"Budget!A1"}`),
	}
	splitRules = []Rule{
		rule("at must be an area of two or more cells", "at",
			func(o *CellSplit) bool { return !isArea(o.At) },
			`{"op":"cell.split","at":"Budget!A:A"}`),
	}
)

// isArea reports whether a is a cell range spanning two or more cells.
func isArea(a RangeAddress) bool {
	return a.Kind == formula.Cells && !(a.R1 == a.R2 && a.C1 == a.C2)
}
