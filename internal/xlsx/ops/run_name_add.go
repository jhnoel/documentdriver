package ops

import (
	"errors"
	"strings"

	"github.com/jhnoel/documentdriver/internal/cli"
	"github.com/jhnoel/documentdriver/internal/xlsx/edit"
	"github.com/jhnoel/documentdriver/internal/xlsx/formula"
	"github.com/jhnoel/documentdriver/internal/xlsx/model"
)

// NameAdd is name.add.
type NameAdd struct {
	Name     string     `json:"name" doc:"The name: starts with a letter, _ or \\; letters, digits, _ and . after; not a cell reference such as A1 or R1C1; at most 255 characters."`
	RefersTo string     `json:"refers_to" doc:"What it refers to, as a formula: =Budget!$A$1:$B$10, =0.2."`
	Scope    *NameScope `json:"scope" doc:"workbook (default) or a sheet: sheet:Budget."`
}

// opNameAdd is name.add's entry in the operation table.
var opNameAdd = OpDef{
	Name: "name.add", New: func() Spec { return &NameAdd{} },
	Summary: "Adds a defined name that refers to a range, a constant or a formula.",
	Rules:   nameAddRules,
}

// Run (chunk G13): a name that exists in the same scope (ignoring case)
// is operation_failed (path name); a scope sheet that does not exist is
// not_found (path scope). It appends <definedName name localSheetId?>
// with the stored formula (formula.Store, no "=") to definedNames
// (created in x:CT_Workbook order), and marks values (MarkValues).
// Changed: the name.
func (o *NameAdd) Run(env *Env) error {
	scope := ""
	if o.Scope != nil {
		scope = o.Scope.Sheet
	}
	storedSheet := scope
	if scope != "" {
		ref, ok := env.WS.SheetRef(scope)
		if !ok {
			return Refuse(cli.CodeNotFound, "scope", "the workbook has no sheet %s", scope)
		}
		storedSheet = ref.Name
	}
	names, err := env.WS.DefinedNames()
	if err != nil {
		return FromEdit(err, "name")
	}
	for _, n := range names {
		if strings.EqualFold(n.Name, o.Name) && strings.EqualFold(n.Sheet, scope) && (scope != "" || n.LocalID < 0) {
			return Refuse(cli.CodeOperationFailed, "name", "the name %s already exists", o.Name)
		}
	}
	body, ok := strings.CutPrefix(o.RefersTo, "=")
	if !ok || body == "" {
		return Refuse(cli.CodeInvalidOperation, "refers_to", "refers_to %q must be a formula starting with =", o.RefersTo)
	}
	stored, err := formula.Store(body)
	if err != nil {
		return Refuse(cli.CodeInvalidOperation, "refers_to", "refers_to cannot be stored: %v", err)
	}
	if err := env.WS.AddDefinedName(o.Name, scope, stored); err != nil {
		if errors.Is(err, edit.ErrNotFound) {
			return Refuse(cli.CodeNotFound, "scope", "the workbook has no sheet %s", scope)
		}
		return FromEdit(err, "name")
	}
	env.WS.MarkValues()
	env.ChangedName(model.NameID(storedSheet, o.Name))
	return nil
}
