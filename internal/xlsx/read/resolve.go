package read

import (
	"fmt"
	"github.com/jhnoel/documentdriver/internal/cli"
	"github.com/jhnoel/documentdriver/internal/xlsx/formula"
	"github.com/jhnoel/documentdriver/internal/xlsx/model"
	"strings"
)

// target is what a cell read reads: a worksheet and a rectangle.
type target struct {
	sheet *SheetInfo
	// path is the flag the target came from ("--range" or "--sheet"),
	// for unreadable_target.
	path string
	// whole says the rectangle is the sheet's used range, known only
	// after the scan (--sheet alone, or --range sheet:S).
	whole bool
	// kind is the requested range's kind: formula.Cells, Rows or Cols;
	// it decides how Range and Truncated.Next are written.
	kind formula.RangeKind
	// r1, c1, r2, c2 is the rectangle, inside the grid (a Rows range has
	// c1 = 1, c2 = MaxCols; a Cols range r1 = 1, r2 = MaxRows). Unset
	// when whole.
	r1, c1, r2, c2 int
}

// resolve maps a cell read's --range and --sheet to a target.
//
//   - --sheet S alone, or --range sheet:S: the worksheet named S
//     (Book.Sheet), whole.
//   - --range R (model.ParseRef):
//   - RefRange: its sheet and range.
//   - RefName: the defined name (matched ignoring case; a sheet-scoped
//     name with that scope, else none: name:Local does not find a
//     sheet-scoped name). Its stored text must be exactly one Ref token
//     of formula.Lex, possibly padded with Space tokens, with a one-sheet
//     prefix (no workbook, no 3-D span) and a cell, area, rows or cols
//     body ("$" flags dropped); anything else (a constant, a formula, a
//     union, #REF!) is invalid_operation "name X does not refer to one
//     plain range". The sheet must exist (else not_found for the sheet).
//   - RefTable: the table's Ref (Book.Table).
//   - RefTableColumn: Table.ColumnRange.
//   - Both flags: the range's sheet must be the --sheet sheet (ignoring
//     case), else invalid_operation at --range ("the range is on
//     sheet X, not Y").
//   - Neither: not called (the summary runs instead).
//
// The target sheet must be a Worksheet: a chartsheet, dialogsheet or
// macrosheet is not an error but an empty read with a NoteSheetKind note
// (brief: "reading their cells is a note"); resolve returns it with
// whole set and the caller skips the scan. A MissingSheet is
// unreadable_target.
//
// Errors: invalid_operation for a malformed address (badAddress) or an
// unresolvable name; not_found for an unknown sheet, name, table or
// column; as listed in doc.go.
func (b *Book) resolve(rangeFlag, sheetFlag string) (target, error) {
	t := target{path: "--range"}
	var requested *SheetInfo
	if sheetFlag != "" {
		name, err := model.ParseSheet(sheetFlag)
		if err != nil {
			return t, badAddress("--sheet", err)
		}
		var ok bool
		requested, ok = b.Sheet(name)
		if !ok {
			return t, notFound("--sheet", "sheet "+name)
		}
	}
	if rangeFlag == "" {
		t.sheet, t.whole, t.path = requested, true, "--sheet"
	} else {
		ref, err := model.ParseRef(rangeFlag)
		if err != nil {
			return t, badAddress("--range", err)
		}
		name, r := ref.Sheet, ref.Range
		switch ref.Kind {
		case model.RefSheet:
			t.whole = true
		case model.RefName:
			found := false
			for _, n := range b.Names {
				if !strings.EqualFold(n.Name, ref.Name) {
					continue
				}
				if ref.Sheet == "" && n.Local != -1 {
					continue
				}
				if ref.Sheet != "" && (n.Local < 0 || n.Local >= len(b.Sheets) || !strings.EqualFold(b.Sheets[n.Local].Name, ref.Sheet)) {
					continue
				}
				found = true
				var ok bool
				name, r, ok = nameRange(n.Text)
				if !ok {
					return t, &cli.Error{Code: cli.CodeInvalidOperation, Path: "--range", Message: "name " + ref.Name + " does not refer to one plain range"}
				}
				break
			}
			if !found {
				return t, notFound("--range", "name "+ref.Name)
			}
		case model.RefTable, model.RefTableColumn:
			table, ok := b.Table(ref.Name)
			if !ok {
				return t, notFound("--range", "table "+ref.Name)
			}
			name, r = table.Sheet, table.Ref
			if ref.Kind == model.RefTableColumn {
				var found, has bool
				r, found, has = table.ColumnRange(ref.Column)
				if !found {
					return t, notFound("--range", "column "+ref.Column)
				}
				if !has {
					return t, &cli.Error{Code: cli.CodeInvalidOperation, Path: "--range", Message: "table " + ref.Name + " has no data rows"}
				}
			}
		}
		var ok bool
		t.sheet, ok = b.Sheet(name)
		if !ok {
			return t, notFound("--range", "sheet "+name)
		}
		if requested != nil && requested != t.sheet {
			return t, &cli.Error{Code: cli.CodeInvalidOperation, Path: "--range", Message: fmt.Sprintf("the range is on sheet %s, not %s", t.sheet.Name, requested.Name)}
		}
		if !t.whole {
			t.kind = r.Kind
			t.r1, t.c1, t.r2, t.c2 = r.Bounds()
		}
	}
	if t.sheet == nil {
		return t, badAddress(t.path, fmt.Errorf("no sheet specified"))
	}
	if t.sheet.Kind == MissingSheet {
		return t, unreadable(t.path, t.sheet.Name, nil)
	}
	if t.sheet.Kind != Worksheet {
		t.whole = true
	}
	return t, nil
}

// nameRange is the plain range a defined name's stored text refers to,
// or ok false (the resolve rules for RefName).
func nameRange(text string) (sheet string, r formula.Range, ok bool) {
	var ref string
	for _, tok := range formula.Lex(text) {
		if tok.Kind == formula.Space {
			continue
		}
		if tok.Kind != formula.Ref || ref != "" {
			return "", formula.Range{}, false
		}
		ref = tok.Text
	}
	if ref == "" {
		return "", formula.Range{}, false
	}
	p, body, valid := formula.SplitQualified(ref)
	if !valid || !p.HasSheet() || p.HasBook || p.ThreeD() {
		return "", formula.Range{}, false
	}
	r, isErr, valid := formula.ParseRefBody(body)
	if !valid || isErr {
		return "", formula.Range{}, false
	}
	r1, c1, r2, c2 := r.Bounds()
	switch r.Kind {
	case formula.Rows:
		r.From, r.To = formula.Cell{Row: r1}, formula.Cell{Row: r2}
	case formula.Cols:
		r.From, r.To = formula.Cell{Col: c1}, formula.Cell{Col: c2}
	default:
		r.From, r.To = formula.Cell{Row: r1, Col: c1}, formula.Cell{Row: r2, Col: c2}
	}
	return p.First, r, true
}
