package ops

import (
	"errors"
	"strings"

	"github.com/jhnoel/documentdriver/internal/cli"
	"github.com/jhnoel/documentdriver/internal/xlsx/edit"
	"github.com/jhnoel/documentdriver/internal/xlsx/formula"
	"github.com/jhnoel/documentdriver/internal/xlsx/model"
	"github.com/jhnoel/documentdriver/internal/xlsx/numfmt"
	"github.com/jhnoel/documentdriver/internal/xlsx/sheet"
)

// The value writer shared by cell.set, range.set and range.clear: the
// designer-owned shell around the PlanCell kernel.

// facts reads what PlanCell needs about (row, col) of s.
func (e *Env) facts(s *edit.Sheet, row, col int) (CellFacts, error) {
	c, err := s.Cell(row, col)
	if err != nil {
		return CellFacts{}, err
	}
	f := CellFacts{Row: row, Col: col, Exists: c.Exists, Raw: c.Raw, Date1904: e.WS.Date1904(), Anchors: s.Index().Anchors()}
	if f.Tables, err = e.WS.Tables(s.Name()); err != nil {
		return CellFacts{}, err
	}
	if c.Exists {
		f.Style = c.Raw.S
	} else if f.Style, err = s.DefaultStyle(row, col); err != nil {
		return CellFacts{}, err
	}
	st, err := e.WS.Styles()
	switch {
	case errors.Is(err, edit.ErrNoStyles):
	case err != nil:
		return CellFacts{}, err
	default:
		f.HasStyles = true
		if _, code, ok := st.NumFmt(f.Style); ok {
			f.NumFmt = numfmt.Classify(code)
		}
	}
	return f, nil
}

// writeValue writes v into (row, col) of s: facts, PlanCell, then the
// plan (unshare, date style, ClearCell or RenderCell and SetCell).
// Refusals name field.
func (e *Env) writeValue(s *edit.Sheet, row, col int, v CellValue, field string) error {
	f, err := e.facts(s, row, col)
	if err != nil {
		return FromEdit(err, field)
	}
	w, err := PlanCell(f, v)
	if err != nil {
		return e.qualify(err, s.Name(), field)
	}
	if w.Unshare {
		if err := e.unshare(s, row, col); err != nil {
			return FromEdit(err, field)
		}
	}
	if w.Clear {
		return FromEdit(s.ClearCell(row, col), field)
	}
	style := w.Style
	if w.DateFormat != 0 {
		st, err := e.WS.Styles()
		if err != nil {
			return FromEdit(err, field)
		}
		if style, err = st.WithNumFmt(style, w.DateFormat); err != nil {
			return err
		}
	}
	x, err := edit.RenderCell(s.Prefix(), row, col, style, w.Content)
	if err != nil {
		return err
	}
	return FromEdit(s.SetCell(row, col, x), field)
}

// qualify sets a kernel refusal's field and puts the sheet in its detail.
func (e *Env) qualify(err error, sheetName, field string) error {
	var r *Rejection
	if !errors.As(err, &r) {
		return err
	}
	out := *r
	out.Field = field
	if out.Detail != "" && !strings.Contains(out.Detail, "!") {
		out.Detail = strings.Replace(out.Detail, " at ", " at "+formula.QuoteSheet(sheetName)+"!", 1)
	}
	if out.Code == "" {
		out.Code = cli.CodeOperationFailed
	}
	return &out
}

// unshare turns the shared group whose master is (row, col) into plain
// formulas: every other member gets formula.Translate of the master's
// text through Sheet.SetFormula, so that the master can be overwritten
// without orphaning a child (E5). Members are the cells inside the
// master's ref carrying its si without text. A master whose ref does not
// parse is refused (operation_failed): its children cannot be found.
func (e *Env) unshare(s *edit.Sheet, row, col int) error {
	var m sheet.Master
	found := false
	for _, x := range s.Index().Masters() {
		if x.Row == row && x.Col == col {
			m, found = x, true
		}
	}
	if !found {
		return nil
	}
	if !m.RefOK {
		// Its children cannot be found without a ref: writing over it
		// would orphan them (E5).
		return &Rejection{Code: cli.CodeOperationFailed, Msg: "the cell is the master of a shared formula whose ref does not parse; its other cells cannot be written out",
			Detail: "a shared formula at " + model.Cell(s.Name(), row, col)}
	}
	master, err := s.Cell(row, col)
	if err != nil {
		return err
	}
	si := master.Raw.F.SI
	r1, c1, r2, c2 := m.Ref.Bounds()
	type member struct{ row, col int }
	var members []member
	if err := s.Each(r1, c1, r2, c2, func(c edit.Cell) error {
		f := c.Raw.F
		if f.Present && f.Type == sheet.Shared && f.SIOK && f.SI == si && f.Text == "" {
			members = append(members, member{c.Row, c.Col})
		}
		return nil
	}); err != nil {
		return err
	}
	for _, mb := range members {
		text, err := formula.Translate(m.Text, formula.Cell{Row: row, Col: col}, formula.Cell{Row: mb.row, Col: mb.col})
		if err != nil {
			return Refuse(cli.CodeOperationFailed, "", "the shared formula of %s cannot be written out: %v", model.Cell(s.Name(), mb.row, mb.col), err)
		}
		if err := s.SetFormula(mb.row, mb.col, &edit.Formula{Text: text}); err != nil {
			return err
		}
	}
	return nil
}
