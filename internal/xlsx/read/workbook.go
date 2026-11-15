package read

import (
	"github.com/jhnoel/documentdriver/internal/xlsx/formula"
	"github.com/jhnoel/documentdriver/internal/xlsx/model"
	"github.com/jhnoel/documentdriver/internal/xlsx/sheet"
)

// Workbook is `xlsx read <file>` with neither --range nor --sheet: the
// workbook summary.
//
//   - sheets: every <sheet> in tab order (SheetEntry). For a Worksheet,
//     dimension comes from sheet.Head of its text (the cells are not
//     tokenized), and the part is forgotten after (Book.Forget), so the
//     summary holds one sheet's text at a time. A worksheet whose head
//     does not scan gets no dimension and a NoteSheet note; a stored
//     dimension that does not parse as a range likewise. A chartsheet,
//     dialogsheet or macrosheet is listed with a NoteSheetKind note ("a
//     chartsheet has no cells to read"); a MissingSheet with its
//     NoteSheet note from openBook. tables lists Book.Tables names.
//   - names: every defined name (NameEntry), hidden and _xlnm. ones
//     included, as stored (approved); hidden is definedName@hidden.
//   - tables: every table of every worksheet, in sheet order then
//     relationship order (TableEntry).
//   - values_stale: Book.FullCalcOnLoad.
//   - notes: Book.Notes.List().
//
// Errors: the invalid_document cases of openBook; nothing else fails.
func Workbook(input []byte, opt Options) (*WorkbookResult, error) {
	b, err := openBook(input, opt)
	if err != nil {
		return nil, err
	}
	defer b.Close()

	res := &WorkbookResult{
		Base:        b.Base,
		ValuesStale: b.FullCalcOnLoad,
		Sheets:      []SheetEntry{},
		Names:       []NameEntry{},
		Tables:      []TableEntry{},
	}
	for i := range b.Sheets {
		sh := &b.Sheets[i]
		se := SheetEntry{
			ID:     model.SheetID(sh.Name),
			Name:   sh.Name,
			State:  stateName(sh.State),
			Tables: []string{},
		}
		switch sh.Kind {
		case Worksheet:
			text, err := b.SheetText(sh)
			switch {
			case err != nil:
				b.Notes.Add(model.NoteSheet, model.SheetID(sh.Name), "the worksheet cannot be read")
			default:
				dim, ok, err := sheet.Head(text)
				switch {
				case err != nil:
					b.Notes.Add(model.NoteSheet, model.SheetID(sh.Name), "the worksheet cannot be read")
				case !ok:
					// No stored dimension: left out, no note.
				default:
					r, ok := formula.ParseRange(dim)
					if !ok {
						b.Notes.Add(model.NoteSheet, model.SheetID(sh.Name), "the stored dimension does not parse")
					} else {
						se.Dimension = model.Qualified(sh.Name, r)
					}
				}
				b.Forget(sh)
			}
		case Chartsheet, Dialogsheet, Macrosheet:
			b.Notes.Add(model.NoteSheetKind, model.SheetID(sh.Name),
				"a "+sheetKindWord(sh.Kind)+" has no cells to read")
		}
		for _, t := range b.Tables(sh) {
			se.Tables = append(se.Tables, t.Name)
			res.Tables = append(res.Tables, TableEntry{
				Name:    t.Name,
				Sheet:   model.SheetID(t.Sheet),
				Ref:     model.Qualified(t.Sheet, t.Ref),
				Columns: t.Columns,
			})
		}
		res.Sheets = append(res.Sheets, se)
	}

	for _, n := range b.Names {
		ne := NameEntry{Name: n.Name, RefersTo: "=" + formula.Display(n.Text)}
		if n.Hidden {
			ne.Hidden = true
		}
		switch n.Local {
		case -1:
			ne.Scope = "workbook"
		case -2:
			b.Notes.Add(model.NoteName, "name:"+n.Name, "the name's sheet scope does not exist")
		default:
			ne.Scope = b.sheetOf(n)
		}
		res.Names = append(res.Names, ne)
	}

	res.Notes = b.Notes.List()
	if err := b.Err(); err != nil {
		return nil, invalidDocument("the workbook exceeds a safety limit", err)
	}
	return res, nil
}

// sheetKindWord is the noun for a non-worksheet sheet kind's note.
func sheetKindWord(k SheetKind) string {
	switch k {
	case Chartsheet:
		return "chartsheet"
	case Dialogsheet:
		return "dialogsheet"
	case Macrosheet:
		return "macrosheet"
	}
	return "sheet"
}
