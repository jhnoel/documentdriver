package ops

import (
	"errors"
	"fmt"

	"github.com/jhnoel/documentdriver/internal/cli"
	"github.com/jhnoel/documentdriver/internal/xlsx/edit"
	"github.com/jhnoel/documentdriver/internal/xlsx/formula"
	"github.com/jhnoel/documentdriver/internal/xlsx/model"
)

// Env is what an operation runs against: the plan's working copy and
// what the operation reports back for the result.
type Env struct {
	// WS is the plan's working copy.
	WS *edit.Workspace
	// Index is the operation's position in the plan.
	Index int

	// Created is the address of what the operation created that its `as`
	// names (sheet.add: "sheet:New").
	Created string
	// Changed are the result's changed units, in the order reported;
	// Deleted the addresses that no longer exist (rows "Budget!5:7",
	// columns "Budget!C:E", names "name:Tax"); RefErrors the cells or
	// names a delete turned into #REF! (the ref-error hazard).
	Changed   []Unit
	Deleted   []string
	RefErrors []string
	// Shifted are the structural edits the op made (row and column
	// inserts and deletes) and Renamed the sheet renames ([old, new]),
	// in order: apply rebases the units earlier ops reported, and the
	// aliases, so that every id in the result is a final address.
	Shifted []formula.Edit
	Renamed [][2]string
}

// UnitKind is the kind of a changed unit.
type UnitKind uint8

// The changed-unit kinds (docs/xlsx-contract-draft.md, §4).
const (
	// UnitRange is a targeted range, read back as {id, cells} ("Budget!A1:D3").
	UnitRange UnitKind = iota
	// UnitSheet is a sheet whose structure changed or whose references
	// were adjusted, {id, formulas_adjusted} ("sheet:Budget").
	UnitSheet
	// UnitName is a defined name, as read --kind names lists it
	// ("name:Tax" or "name:Budget!Local").
	UnitName
)

// Unit is one changed unit.
type Unit struct {
	Kind UnitKind
	// ID is its address.
	ID string
	// Sheet and the bounds are a UnitRange's (to read it back).
	Sheet          string
	R1, C1, R2, C2 int
	// FormulasAdjusted is a UnitSheet's count of rewritten formulas.
	FormulasAdjusted int
}

// ChangedRange reports rows r1..r2, columns c1..c2 of sheet as changed.
func (e *Env) ChangedRange(sheet string, r1, c1, r2, c2 int) {
	e.Changed = append(e.Changed, Unit{Kind: UnitRange, ID: model.Area(sheet, r1, c1, r2, c2), Sheet: sheet, R1: r1, C1: c1, R2: r2, C2: c2})
}

// ChangedSheet reports sheet as restructured or adjusted.
func (e *Env) ChangedSheet(sheet string, adjusted int) {
	for i, u := range e.Changed {
		if u.Kind == UnitSheet && u.Sheet == sheet {
			e.Changed[i].FormulasAdjusted += adjusted
			return
		}
	}
	e.Changed = append(e.Changed, Unit{Kind: UnitSheet, ID: model.SheetID(sheet), Sheet: sheet, FormulasAdjusted: adjusted})
}

// ChangedName reports a defined name (its address) as changed.
func (e *Env) ChangedName(id string) { e.Changed = append(e.Changed, Unit{Kind: UnitName, ID: id}) }

// Sheet opens the sheet named name for the operation, mapping the edit
// layer's errors onto the contract's: not_found and unreadable_target
// at field.
func (e *Env) Sheet(name, field string) (*edit.Sheet, error) {
	s, err := e.WS.Sheet(name)
	if err != nil {
		return nil, FromEdit(err, field)
	}
	return s, nil
}

// Rejection is an operation refusing the document: a contract error
// code (not_found, ambiguous, unreadable_target, operation_failed), the
// field at fault ("" for the operation), the message, and the detail
// the contract asks for (unreadable_target: what is wrong;
// operation_failed for a refused construct: the construct).
type Rejection struct {
	Code, Field, Msg, Detail string
	// Matches is an ambiguous selection's count.
	Matches int
}

// Error returns the message.
func (r *Rejection) Error() string { return r.Msg }

// Refuse returns a Rejection with code, field and a formatted message.
func Refuse(code, field, format string, args ...any) *Rejection {
	return &Rejection{Code: code, Field: field, Msg: fmt.Sprintf(format, args...)}
}

// FromEdit maps an error of the edit layer onto a Rejection at field:
// edit.ErrNotFound is not_found, *edit.Unreadable unreadable_target with
// its detail; a Rejection passes; anything else is returned as it is (a
// bug or an I/O failure: internal).
func FromEdit(err error, field string) error {
	if err == nil {
		return nil
	}
	var r *Rejection
	if errors.As(err, &r) {
		return r
	}
	var u *edit.Unreadable
	if errors.As(err, &u) {
		return &Rejection{Code: cli.CodeUnreadableTarget, Field: field, Msg: u.Target + " cannot be edited safely", Detail: u.Detail}
	}
	if errors.Is(err, edit.ErrNotFound) {
		return &Rejection{Code: cli.CodeNotFound, Field: field, Msg: err.Error()}
	}
	return err
}
