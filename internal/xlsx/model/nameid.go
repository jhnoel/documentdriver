package model

import "github.com/jhnoel/documentdriver/internal/xlsx/formula"

// NameID is the unit id of a defined name: "name:Tax" for workbook scope
// (sheet ""), "name:Budget!Local" or "name:'Q1 Budget'!Local" for a name
// scoped to a sheet (the sheet quoted as formula.SheetPrefix quotes it).
// ParseRef parses it back.
func NameID(sheet, name string) string {
	if sheet == "" {
		return "name:" + name
	}
	return "name:" + formula.SheetPrefix(sheet, "") + name
}
