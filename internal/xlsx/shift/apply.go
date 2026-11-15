package shift

import (
	"errors"
	"fmt"
	"strings"

	"github.com/jhnoel/documentdriver/internal/ooxml/ctypes"
	"github.com/jhnoel/documentdriver/internal/xlsx/edit"
	"github.com/jhnoel/documentdriver/internal/xlsx/formula"
	"github.com/jhnoel/documentdriver/internal/xlsx/model"
)

// Refused is a structural edit refused because it would cut or move a
// construct the tool cannot adjust (operation_failed; decision 12).
type Refused struct {
	// Construct names it as the contract's detail does ("a multi-cell
	// array formula at Budget!B2:B5", "a pivot table on Budget").
	Construct string
}

// Error implements error.
func (r *Refused) Error() string { return "the edit would cut or move " + r.Construct }

// Report is what Apply did.
type Report struct {
	// Adjusted is the number of formulas rewritten, by sheet name (the
	// edited sheet included when any of its formulas changed; a sheet
	// with none is left out, the edited sheet always in).
	Adjusted map[string]int
	// RefErrors are the cells ("Budget!B2") and names ("name:Tax") a
	// delete turned into #REF! (the ref-error hazard).
	RefErrors []string
}

// Apply performs e (formula.Edit: Sheet, Kind, At, Count) on ws (chunk
// 10). It is the imperative shell over the pure rewrites: it reads the
// inputs through the edit layer, computes every rewrite first (Guard
// over Parts; SheetData and Element over the edited sheet's
// Sheet.Pieces; OtherSheet over each other worksheet's text, Released
// when it needs nothing; DefinedName over each definedName; Table over
// each table part of the sheet), and only when none refused writes them:
// Sheet.SetPieces, Sheet.SetFormulaText per FormulaEdit (masters stay shared), the definedNames'
// text (SetDefinedNameText) and the table parts (SetPartText). It
// marks values and formulas (Sheet.SetText does). The glue in
// ops/structural.go records the edit in Env.Shifted. Errors are *Refused, *edit.Unreadable,
// edit.ErrNotFound (the sheet) or a bug.
//
// The edit must stay inside the grid: an insert that would push a
// non-empty row (column) past row 1,048,576 (column XFD) refuses
// (*Refused "a cell past row 1,048,576"), as does any reference
// formula.Shift refuses.
func Apply(ws *edit.Workspace, e formula.Edit) (Report, error) {
	s, err := ws.Sheet(e.Sheet)
	if err != nil {
		return Report{}, err
	}
	e.Sheet = s.Name()
	order := ws.Order()
	if err := guardParts(ws, s, e); err != nil {
		return Report{}, shiftError(err, e.Sheet)
	}
	pieces, err := s.Pieces()
	if err != nil {
		return Report{}, err
	}
	report := Report{Adjusted: map[string]int{e.Sheet: 0}}
	addStats := func(name string, stats Stats) {
		if stats.Adjusted != 0 {
			report.Adjusted[name] += stats.Adjusted
		}
		for _, cell := range stats.RefErrors {
			report.RefErrors = append(report.RefErrors, name+"!"+cell)
		}
	}
	for i := range pieces {
		p := &pieces[i]
		if p.Local == "" {
			continue
		}
		var out string
		var stats Stats
		if p.Local == "sheetData" {
			out, stats, err = SheetData(p.XML, formula.Sheets{Own: e.Sheet, Order: order}, e)
		} else {
			out, stats, err = Element(p.Local, p.XML, formula.Sheets{Own: e.Sheet, Order: order}, e)
		}
		if err != nil {
			return Report{}, shiftError(err, e.Sheet)
		}
		p.XML = out
		addStats(e.Sheet, stats)
	}
	type sheetEdits struct {
		name  string
		edits []FormulaEdit
	}
	var others []sheetEdits
	for _, ref := range ws.Sheets() {
		if strings.EqualFold(ref.Name, e.Sheet) || ref.Kind != edit.KindWorksheet {
			continue
		}
		other, err := ws.Sheet(ref.Name)
		if err != nil {
			return Report{}, err
		}
		text, err := other.Text()
		if err != nil {
			return Report{}, err
		}
		edits, stats, err := OtherSheet(text, formula.Sheets{Own: ref.Name, Order: order}, e)
		if err != nil {
			return Report{}, shiftError(err, e.Sheet)
		}
		addStats(ref.Name, stats)
		if len(edits) > 0 {
			others = append(others, sheetEdits{ref.Name, edits})
		}
		ws.Release(ref.Name)
	}
	names, err := ws.DefinedNames()
	if err != nil {
		return Report{}, err
	}
	nameTexts := make([]string, len(names))
	for i, name := range names {
		var refError bool
		nameTexts[i], refError, err = DefinedName(name.Text, formula.Sheets{Own: name.Sheet, Order: order}, e)
		if err != nil {
			return Report{}, shiftError(err, e.Sheet)
		}
		if refError {
			report.RefErrors = append(report.RefErrors, model.NameID(name.Sheet, name.Name))
		}
	}
	tables, err := ws.Tables(e.Sheet)
	if err != nil {
		return Report{}, err
	}
	tableTexts := make([]string, len(tables))
	for i, table := range tables {
		text, err := ws.PartText(table.Part)
		if err != nil {
			return Report{}, err
		}
		tableTexts[i], err = Table(text, e)
		if err != nil {
			return Report{}, shiftError(err, e.Sheet)
		}
	}
	if err := s.SetPieces(pieces); err != nil {
		return Report{}, err
	}
	for _, other := range others {
		sheet, err := ws.Sheet(other.name)
		if err != nil {
			return Report{}, err
		}
		for _, f := range other.edits {
			if err := sheet.SetFormulaText(f.Row, f.Col, f.Text); err != nil {
				return Report{}, err
			}
		}
	}
	for i, name := range names {
		if nameTexts[i] != name.Text {
			if err := ws.SetDefinedNameText(i, nameTexts[i]); err != nil {
				return Report{}, err
			}
			ws.MarkValues()
		}
	}
	for i, table := range tables {
		if err := ws.SetPartText(table.Part, tableTexts[i]); err != nil {
			return Report{}, err
		}
	}
	return report, nil
}

// shiftError translates a kernel's error: a formula rewrite refusal is a
// structural refusal; a Refused or an edit.Unreadable stays; any other
// is content of the sheet's parts the kernel cannot read (a malformed
// sqref, a VML drawing that is not well formed), which the shift cannot
// adjust safely: the op's own target is unreadable (contract §10), never
// an internal error.
func shiftError(err error, sheet string) error {
	var refusal *formula.Refusal
	if errors.As(err, &refusal) {
		return &Refused{Construct: "a reference (" + refusal.Reason + ")"}
	}
	var r *Refused
	var u *edit.Unreadable
	if errors.As(err, &r) || errors.As(err, &u) {
		return err
	}
	return &edit.Unreadable{Target: model.SheetID(sheet), Detail: err.Error()}
}

// guardParts checks each relationship separately, including multiple drawings.
func guardParts(ws *edit.Workspace, s *edit.Sheet, e formula.Edit) error {
	for _, source := range []string{s.PartName(), ws.WorkbookPart()} {
		rels, err := ws.Package().Rels(source)
		if err != nil {
			return &edit.Unreadable{Target: model.SheetID(s.Name()), Detail: "relationships cannot be read: " + err.Error()}
		}
		for _, rel := range rels.All() {
			var p Parts
			if source == s.PartName() && ctypes.Is(rel.Type, ctypes.RelPivotTable) {
				p.SheetRelTypes = []string{ctypes.RelPivotTable}
			}
			kind := 0
			if source == s.PartName() {
				switch {
				case ctypes.Is(rel.Type, ctypes.RelDrawing):
					kind = 1
				case ctypes.Is(rel.Type, ctypes.RelVmlDrawing):
					kind = 2
				case ctypes.Is(rel.Type, ctypes.RelComments):
					kind = 3
				case strings.HasSuffix(rel.Type, "/threadedComment"):
					kind = 4
				}
			} else {
				switch {
				case ctypes.Is(rel.Type, ctypes.RelPivotCacheDefinition):
					kind = 5
				case ctypes.Is(rel.Type, ctypes.RelExternalLink):
					kind = 6
				}
			}
			if kind != 0 && !rel.External() {
				part, err := rels.TargetPart(rel)
				if err != nil {
					return fmt.Errorf("resolve guard relationship: %w", err)
				}
				text, err := ws.PartText(part)
				if err != nil {
					return err
				}
				switch kind {
				case 1:
					p.Drawing = text
				case 2:
					p.VML = text
				case 3:
					p.Comments = text
				case 4:
					p.ThreadedComments = text
				case 5:
					p.PivotCaches = []string{text}
				case 6:
					p.ExternalLinks = []string{text}
				}
			}
			if err := Guard(p, e.Sheet, e); err != nil {
				return err
			}
		}
	}
	return nil
}
