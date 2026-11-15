package ops

import (
	"errors"
	"maps"
	"slices"

	"github.com/jhnoel/documentdriver/internal/cli"
	"github.com/jhnoel/documentdriver/internal/xlsx/formula"
	"github.com/jhnoel/documentdriver/internal/xlsx/shift"
)

// structural is the glue of the four structural ops (designer-owned):
// shift.Apply on the sheet (its real name), its Refused mapped to
// operation_failed at "at" with the construct as detail, and the report
// into the result (changed sheets, deleted address, ref errors).
func (e *Env) structural(sheetName string, ed formula.Edit, deleted string) error {
	ref, ok := e.WS.SheetRef(sheetName)
	if !ok {
		return Refuse(cli.CodeNotFound, "at", "the workbook has no sheet %s", sheetName)
	}
	ed.Sheet = ref.Name
	rep, err := shift.Apply(e.WS, ed)
	var r *shift.Refused
	switch {
	case errors.As(err, &r):
		return &Rejection{Code: cli.CodeOperationFailed, Field: "at", Msg: "the edit cannot be made: " + r.Error(), Detail: r.Construct}
	case err != nil:
		return FromEdit(err, "at")
	}
	names := slices.Sorted(maps.Keys(rep.Adjusted))
	e.ChangedSheet(ref.Name, rep.Adjusted[ref.Name])
	for _, n := range names {
		if n != ref.Name {
			e.ChangedSheet(n, rep.Adjusted[n])
		}
	}
	if deleted != "" {
		e.Deleted = append(e.Deleted, deleted)
	}
	e.RefErrors = append(e.RefErrors, rep.RefErrors...)
	e.Shifted = append(e.Shifted, ed)
	return nil
}

// count is an insert's count (default 1).
func count(c *int) int {
	if c == nil {
		return 1
	}
	return *c
}
