package ops

import "github.com/jhnoel/documentdriver/internal/xlsx/formula"

// The structural ops' rules and help lists (designer-owned).
var (
	rowInsertRules = []Rule{
		rule("at must be one row (Budget!5:5)", "at",
			func(o *RowInsert) bool { return o.At.From != o.At.To },
			`{"op":"row.insert","at":"Budget!5:6","position":"before"}`),
		rule("count must be from 1 to 1048576", "count",
			func(o *RowInsert) bool { return o.Count != nil && (*o.Count < 1 || *o.Count > formula.MaxRows) },
			`{"op":"row.insert","at":"Budget!5:5","position":"before","count":0}`),
	}
	columnInsertRules = []Rule{
		rule("at must be one column (Budget!C:C)", "at",
			func(o *ColumnInsert) bool { return o.At.From != o.At.To },
			`{"op":"column.insert","at":"Budget!C:D","position":"after"}`),
		rule("count must be from 1 to 16384", "count",
			func(o *ColumnInsert) bool { return o.Count != nil && (*o.Count < 1 || *o.Count > formula.MaxCols) },
			`{"op":"column.insert","at":"Budget!C:C","position":"after","count":0}`),
	}

	// structuralAdjusts is the closed list of references a row or column
	// insert or delete rewrites (docs/design.md, xlsx: Formulas, the
	// "rewritten" tier); help prints it. Anything not listed here or in
	// structuralRefuses is never touched.
	structuralAdjusts = []string{
		"cell formulas on every sheet (shared formulas kept, split or unshared by position)",
		"defined names",
		"the sheet's dimension, merged cells, column widths (cols), row and column breaks",
		"hyperlink ranges",
		"conditional formatting and data validation ranges and formulas",
		"the sheet's autoFilter",
		"selections and panes of the sheet's views",
		"table ranges (row shifts), with their autoFilter and sortState",
	}
	// structuralRefuses is the closed list of constructs a shift refuses
	// when it would cut or move them (decision 12), each with the
	// operation_failed detail it gives.
	structuralRefuses = []string{
		"a multi-cell array formula or a dynamic-array spill the shift cuts",
		"a data table the shift cuts",
		"a pivot table or pivot cache source on the sheet",
		"a table crossed by a column shift; deleting a table's header row or its last data row",
		"x14 extension references (sparklines, x14 conditional formatting and data validation) at or after the shift",
		"a reference to the sheet from another workbook (external link)",
		"drawing anchors and chart ranges at or after the shift (charts, images, shapes)",
		"comments and threaded comments at or after the shift",
		"a formula on any sheet that does not lex cleanly and names the sheet",
		"a shift past row 1,048,576 or column XFD",
	}
)
