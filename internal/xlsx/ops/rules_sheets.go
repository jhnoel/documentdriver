package ops

import (
	"strings"
	"unicode/utf16"

	"github.com/jhnoel/documentdriver/internal/cli"
)

// sheet.add's and sheet.rename's rules and help lists (designer-owned).
var (
	sheetAddRules = []Rule{
		rule("a sheet name is 1 to 31 characters, none of : \\ / ? * [ ], does not start or end with an apostrophe, and is not History", "name",
			func(o *SheetAdd) bool { return sheetNameProblem(o.Name) != "" },
			`{"op":"sheet.add","name":"Q1/Q2"}`),
		rule("as must match [A-Za-z_][A-Za-z0-9_]*", "as",
			func(o *SheetAdd) bool { return o.As != nil && !cli.ValidAlias(*o.As) },
			`{"op":"sheet.add","name":"New","as":"1x"}`),
	}
	sheetRenameRules = []Rule{
		rule("a sheet name is 1 to 31 characters, none of : \\ / ? * [ ], does not start or end with an apostrophe, and is not History", "name",
			func(o *SheetRename) bool { return sheetNameProblem(o.Name) != "" },
			`{"op":"sheet.rename","at":"sheet:Budget","name":""}`),
	}
	// renameAdjusts is what sheet.rename rewrites; renameRefuses what it
	// refuses (operation_failed, detail the construct).
	renameAdjusts = []string{
		"cell formulas on every sheet", "defined names",
		"conditional formatting and data validation formulas", "hyperlink locations",
	}
	renameRefuses = []string{
		"a chart whose series reference the sheet (c:f)",
		"a pivot cache whose worksheet source names the sheet",
		"a reference to the sheet from another workbook (external link)",
		"a formula naming the sheet that does not lex cleanly",
	}
)

// sheetNameProblem says what is wrong with a sheet name, "" when nothing
// (Excel's rules; the integrity check's sheet.name-* kinds).
func sheetNameProblem(name string) string {
	switch n := len(utf16.Encode([]rune(name))); {
	case n == 0:
		return "empty"
	case n > 31:
		return "longer than 31 characters"
	}
	if strings.ContainsAny(name, `:\/?*[]`) {
		return "holds one of : \\ / ? * [ ]"
	}
	if strings.HasPrefix(name, "'") || strings.HasSuffix(name, "'") {
		return "starts or ends with an apostrophe"
	}
	if strings.EqualFold(name, "History") {
		return "History is reserved"
	}
	for _, r := range name {
		if r < 0x20 {
			return "holds a control character"
		}
	}
	return ""
}
