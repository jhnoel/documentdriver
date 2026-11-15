package ops

import (
	"strings"

	"github.com/jhnoel/documentdriver/internal/xlsx/formula"
	"github.com/jhnoel/documentdriver/internal/xlsx/sheet"
)

// The rename kernel (chunk K13): pure.

// FormulaRename is one formula cell to rewrite for a sheet rename.
type FormulaRename struct {
	Row, Col int
	Text     string
}

// RenameInSheet returns the formula rewrites a worksheet (src its whole
// text) needs when sheet old becomes name: formula.Rename of every <f>
// text that changes. A byte prefilter for old (ignoring case) returns
// nothing at once when the sheet cannot mention it. Only cell formulas;
// the glue does defined names, CF and DV formulas, and hyperlink
// locations with RenameText.
func RenameInSheet(src, old, name string) ([]FormulaRename, error) {
	if !strings.Contains(strings.ToLower(src), strings.ToLower(old)) && !strings.Contains(src, "&") {
		return nil, nil
	}
	var edits []FormulaRename
	_, err := sheet.Scan(src, sheet.ScanOptions{
		From: 1, To: formula.MaxRows,
		Row: func(row *sheet.Row) error {
			for _, cell := range row.Cells {
				if !cell.F.Present || cell.F.Text == "" {
					continue
				}
				text := formula.Rename(cell.F.Text, old, name)
				if text != cell.F.Text {
					edits = append(edits, FormulaRename{Row: cell.Row, Col: cell.Col, Text: text})
				}
			}
			return nil
		},
	})
	if err != nil {
		return nil, err
	}
	return edits, nil
}

// RenameText is formula.Rename for a hyperlink location ("Budget!A1",
// "'Q1 Budget'!B2": a reference without "="), and for any stored
// formula text.
func RenameText(text, old, name string) string {
	return formula.Rename(text, old, name)
}
