package ops

import (
	"github.com/jhnoel/documentdriver/internal/cli"
	"github.com/jhnoel/documentdriver/internal/xlsx/model"
)

// SheetAdd is sheet.add.
type SheetAdd struct {
	Name string  `json:"name" doc:"The new sheet's name: 1 to 31 characters, none of : \\ / ? * [ ], not starting or ending with an apostrophe, not History."`
	As   *string `json:"as" doc:"A name for the new sheet; later operations write $name for it."`
}

// opSheetAdd is sheet.add's entry in the operation table.
var opSheetAdd = OpDef{
	Name: "sheet.add", New: func() Spec { return &SheetAdd{} },
	Summary: "Adds an empty worksheet after the last sheet.",
	Rules:   sheetAddRules,
}

// Run (chunk G11): a sheet whose name equals an existing one ignoring
// case is operation_failed (path name). It adds the part
// /xl/worksheets/sheetN.xml (the first free N) with a blank worksheet
// in the workbook's namespace (Transitional or Strict, as the workbook
// part), its content-type override (ctypes.CTSpreadsheetmlWorksheet),
// a workbook relationship (ctypes.RelWorksheet, Rels.Add) and
// <sheet name sheetId r:id> appended to sheets (sheetId the largest plus
// one), then ReloadBook. Created: "sheet:Name". Changed: the sheet.
func (o *SheetAdd) Run(env *Env) error {
	if _, exists := env.WS.SheetRef(o.Name); exists {
		return Refuse(cli.CodeOperationFailed, "name", "sheet %q already exists", o.Name)
	}
	ref, err := env.WS.AddSheet(o.Name)
	if err != nil {
		return FromEdit(err, "name")
	}
	env.Created = model.SheetID(ref.Name)
	env.ChangedSheet(ref.Name, 0)
	return nil
}
