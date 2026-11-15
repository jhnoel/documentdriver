package read

import (
	"errors"
	"fmt"
	"sort"

	"github.com/jhnoel/documentdriver/internal/cli"
	"github.com/jhnoel/documentdriver/internal/xlsx/formula"
	"github.com/jhnoel/documentdriver/internal/xlsx/model"
	"github.com/jhnoel/documentdriver/internal/xlsx/sheet"
)

// CellsRequest is a cell read's flags.
type CellsRequest struct {
	// Range is --range ("" when absent); Sheet is --sheet ("" when
	// absent). At least one is set.
	Range, Sheet string
	// View is --view (default model.ViewCells).
	View model.View
	// Limit is the most cells to return; 0 is no limit. cmd/xlsx passes
	// DefaultLimit when --limit is absent.
	Limit int
	// Group is --group: shared-formula groups as Groups (formulas view
	// only; cmd/xlsx refuses it with another view: usage, path --group).
	Group bool
	// CellsOnly is for apply's changed units, read back from a package
	// the commit gate has just scanned in full: only Cells, Truncated
	// and View are filled (no Range, UsedRange, Merges, Hidden or
	// notes), and the scan of an area or rows range stops after its last
	// row (sheet.ScanOptions.StopAfterBand) unless the limit cuts it
	// (Truncated.Next needs the used range). The cells are those of the
	// full read: a cell's value depends only on what precedes it.
	CellsOnly bool
}

// Cells is `xlsx read <file> --sheet S` and `--range R`.
//
// Steps: openBook; resolve; SheetText (unreadable_target on error); one
// sheet.Scan with the band r1..r2 and columns c1..c2 of the target (the
// whole sheet when target.whole), converting each handed row's cells
// with converter.entry and adding those Shown in the view to Cells.
// Then, from the Index: Range and UsedRange, merges that intersect the
// rectangle, hidden rows and columns clipped to it (HiddenRows,
// HiddenCols), and the notes. sheet.Scan's Problems become NoteOrder and
// NoteGrid notes (AddCell for cells, Add at the row for rows).
//
// Limit (decision 6): rows are added whole. Before adding a row, its
// Shown entries are counted; when at least one cell was already added
// and the total would pass Limit, the read stops at that row
// (sheet.ErrStopRows): Truncated.Next is the rest of the range from that
// row (Truncated's rules), and a NoteTruncated note at the range says
// "read stopped after N cells at --limit; read Next for the rest". A
// first row larger than Limit is added whole (a read always progresses).
// Limit 0 is no limit.
//
// --group (formulas view): every shared-formula group whose members
// inside the range number two or more and fill a rectangle becomes one
// Group (At the rectangle, F the formula of its top-left member); its
// cells are left out of Cells. This needs the group's members before
// writing them, so with Group the reader hands each shared-formula cell
// to a grouper instead of CellMap, and the grouper writes, after the
// scan, the groups and the cells it did not group (O(shared cells)
// extra; only in this opt-in view). See grouper.
//
// The checkpoint "cells scanned" is marked after the scan, with the
// sheet text, the Index and the output alive.
//
// Errors: doc.go's; a non-worksheet target is the empty read of
// resolve.
func Cells(input []byte, req CellsRequest, opt Options) (*CellsResult, error) {
	b, err := openBook(input, opt)
	if err != nil {
		return nil, err
	}
	defer b.Close()
	t, err := b.resolve(req.Range, req.Sheet)
	if e := b.Err(); e != nil {
		return nil, invalidDocument("the workbook exceeds a safety limit", e)
	}
	if err != nil {
		return nil, err
	}
	sh := t.sheet
	res := &CellsResult{Base: b.Base, Sheet: model.SheetID(sh.Name), View: req.View.String(), ValuesStale: b.FullCalcOnLoad, Cells: &CellMap{}, Merges: []string{}, Hidden: []string{}}
	if sh.Kind != Worksheet {
		b.Notes.Add(model.NoteSheetKind, res.Sheet, "a "+sheetKindWord(sh.Kind)+" has no cells to read")
		res.Notes = b.Notes.List()
		return res, nil
	}
	text, err := b.SheetText(sh)
	if e := b.Err(); e != nil {
		return nil, invalidDocument("the workbook exceeds a safety limit", e)
	}
	if err != nil {
		return nil, unreadable(t.path, sh.Name, err)
	}
	r1, c1, r2, c2 := t.r1, t.c1, t.r2, t.c2
	if t.whole {
		r1, c1, r2, c2 = 1, 1, formula.MaxRows, formula.MaxCols
	}
	rd := &reader{conv: newConverter(b, sh.Name), view: req.View, limit: req.Limit, out: res.Cells, maxBytes: opt.maxOutput()}
	if req.Group {
		rd.group = &grouper{}
	}
	ix, err := sheet.Scan(text, sheet.ScanOptions{From: r1, To: r2, ColFrom: c1, ColTo: c2, Row: rd.row, StopAfterBand: req.CellsOnly && !t.whole, Problem: func(p sheet.Problem, row, col int, _ string) {
		kind := model.NoteGrid
		reason := "row outside the grid or unreadable"
		if p == sheet.ProblemOrder {
			kind = model.NoteOrder
			reason = "row out of order or repeated"
		}
		if col > 0 {
			if p == sheet.ProblemOrder {
				reason = "cell out of order or repeated"
			} else {
				reason = "cell outside the grid or unreadable"
			}
			b.Notes.AddCell(kind, sh.Name, row, col, reason)
		} else if row > 0 {
			b.Notes.AddCell(kind, sh.Name, row, 1, reason)
		} else {
			b.Notes.Add(kind, model.SheetID(sh.Name), "range or row outside the grid or unreadable")
		}
	}})
	if e := b.Err(); e != nil {
		return nil, invalidDocument("the workbook exceeds a safety limit", e)
	}
	if errors.Is(err, errRowTooLarge) {
		return nil, &cli.Error{Code: cli.CodeOperationFailed, Path: t.path,
			Message: fmt.Sprintf("the cells of one row of the range pass the %d MiB output limit", opt.maxOutput()>>20),
			Hint:    "read fewer columns, for example --range " + model.Area(sh.Name, rd.firstRow(), c1, rd.firstRow(), c1)}
	}
	if err != nil {
		return nil, unreadable(t.path, sh.Name, err)
	}
	if rd.group != nil {
		res.Groups = rd.group.finish(sh.Name, res.Cells, req.View)
	}
	if req.CellsOnly && rd.cutRow == 0 {
		// The Index may stop at the band: nothing past the cells is
		// derived from it.
		return &CellsResult{Base: res.Base, Sheet: res.Sheet, View: res.View, ValuesStale: res.ValuesStale, Cells: res.Cells, Merges: []string{}, Hidden: []string{}}, nil
	}
	ur1, uc1, ur2, uc2, used := ix.UsedRange()
	if used {
		res.UsedRange = model.Area(sh.Name, ur1, uc1, ur2, uc2)
	}
	if t.whole {
		if used {
			res.Range = res.UsedRange
			r1, c1, r2, c2 = ur1, uc1, ur2, uc2
		}
	} else {
		switch t.kind {
		case formula.Rows:
			res.Range = model.Rows(sh.Name, r1, r2)
		case formula.Cols:
			res.Range = model.Cols(sh.Name, c1, c2)
		default:
			res.Range = model.Area(sh.Name, r1, c1, r2, c2)
		}
	}
	if rd.cutRow > 0 {
		end := min(r2, ur2)
		var next string
		if t.kind == formula.Rows && !t.whole {
			next = model.Rows(sh.Name, rd.cutRow, end)
		} else {
			next = model.Area(sh.Name, rd.cutRow, c1, end, c2)
		}
		res.Truncated = &Truncated{Next: next}
		why := "at --limit"
		if rd.sizeCut {
			why = fmt.Sprintf("at the %d MiB output limit", opt.maxOutput()>>20)
		}
		b.Notes.Add(model.NoteTruncated, res.Range, fmt.Sprintf("read stopped after %d cells %s; read %s for the rest", rd.count, why, next))
	}
	if !t.whole || used {
		for _, m := range ix.Merges() {
			a, b1, c, d := m.Bounds()
			if a <= r2 && c >= r1 && b1 <= c2 && d >= c1 {
				res.Merges = append(res.Merges, model.Qualified(sh.Name, m))
			}
		}
		for _, run := range ix.HiddenRows(r1, r2) {
			res.Hidden = append(res.Hidden, model.Rows(sh.Name, run[0], run[1]))
		}
		for _, run := range ix.HiddenCols(c1, c2) {
			res.Hidden = append(res.Hidden, model.Cols(sh.Name, run[0], run[1]))
		}
	}
	res.Notes = b.Notes.List()
	opt.mark("cells scanned")
	return res, nil
}

// reader is the state of one cell read's scan callback.
type reader struct {
	conv  *converter
	view  model.View
	limit int
	out   *CellMap
	// pending holds one row's Shown entries while the limit is checked.
	pending []pendingCell
	// cutRow is the first row not read (0 while not cut).
	cutRow int
	count  int
	group  *grouper
	// bytes estimates the encoded size of what was added; maxBytes is
	// Options.maxOutput. sizeCut says the cut came from it, not --limit.
	bytes, maxBytes int
	sizeCut         bool
}

// errRowTooLarge stops a read whose first row alone passes the output
// budget.
var errRowTooLarge = errors.New("read: one row's cells exceed the output budget")

// entrySize is an upper estimate of an entry's encoded size: its texts,
// escaped at worst six bytes a byte for control characters but in
// practice near their length, plus the keys and punctuation.
func entrySize(e model.Entry) int {
	return len(e.Value.Text) + len(e.Formula) + len(e.Spill) + 48
}

type pendingCell struct {
	row, col int
	e        model.Entry
	si       int
	shared   bool
}

// row is the sheet.ScanOptions.Row callback: convert the row, apply the
// limit, add it. It returns sheet.ErrStopRows when the limit cuts.
func (r *reader) row(sr *sheet.Row) error {
	r.conv.ix = sr.Index
	r.pending = r.pending[:0]
	rowBytes := 0
	for i := range sr.Cells {
		rc := &sr.Cells[i]
		e, ok := r.conv.entry(rc)
		if !ok || !e.Shown(r.view) {
			continue
		}
		r.pending = append(r.pending, pendingCell{row: rc.Row, col: rc.Col, e: e, si: rc.F.SI, shared: rc.F.Present && rc.F.Type == sheet.Shared && rc.F.SIOK && e.Formula != ""})
		rowBytes += entrySize(e)
		if r.bytes+rowBytes > r.maxBytes {
			// Stop holding more of this row: it cannot be added.
			if r.count == 0 {
				return errRowTooLarge
			}
			r.cutRow, r.sizeCut = sr.Num, true
			return sheet.ErrStopRows
		}
	}
	if r.limit > 0 && r.count > 0 && r.count+len(r.pending) > r.limit {
		r.cutRow = sr.Num
		return sheet.ErrStopRows
	}
	r.bytes += rowBytes
	for _, c := range r.pending {
		if r.group != nil && c.shared {
			r.group.add(c.si, c)
		} else {
			r.out.Add(c.row, c.col, c.e, r.view)
		}
	}
	r.count += len(r.pending)
	return nil
}

// grouper collects the shared-formula cells of a --group read (chunk 6).
// Cells keeps writing every other cell to CellMap as it reads; a shared
// cell (F.Type sheet.Shared, master or child, with a derivable formula)
// goes to add instead, keyed by its si. Cell order in CellMap is then no
// longer strictly row-major (ungrouped shared cells are written after
// the scan, in row-major order among themselves); --group's output says
// so in help (approved).
type grouper struct {
	bySI map[int][]pendingCell
}

// add records a shared-formula cell of group si.
func (g *grouper) add(si int, c pendingCell) {
	if g.bySI == nil {
		g.bySI = make(map[int][]pendingCell)
	}
	g.bySI[si] = append(g.bySI[si], c)
}

// finish returns the groups, in the order of their top-left cells
// (row-major), and writes the cells of groups that do not qualify to out
// in row-major order. A group qualifies when it has two or more members
// in the range and they fill their bounding rectangle exactly; At is
// that rectangle (model.Area) and F the top-left member's formula.
func (g *grouper) finish(sheet string, out *CellMap, view model.View) []Group {
	if len(g.bySI) == 0 {
		return nil
	}
	type group struct {
		r1, c1, r2, c2 int
		top            pendingCell
		members        []pendingCell
	}
	var groups []group
	var loose []pendingCell
	for _, members := range g.bySI {
		top := members[0]
		r1, c1, r2, c2 := top.row, top.col, top.row, top.col
		for _, m := range members[1:] {
			if m.row < top.row || (m.row == top.row && m.col < top.col) {
				top = m
			}
			r1 = min(r1, m.row)
			c1 = min(c1, m.col)
			r2 = max(r2, m.row)
			c2 = max(c2, m.col)
		}
		if len(members) >= 2 && len(members) == (r2-r1+1)*(c2-c1+1) {
			groups = append(groups, group{r1: r1, c1: c1, r2: r2, c2: c2, top: top, members: members})
		} else {
			loose = append(loose, members...)
		}
	}
	sort.Slice(groups, func(i, j int) bool {
		a, b := groups[i], groups[j]
		return a.r1 < b.r1 || (a.r1 == b.r1 && a.c1 < b.c1)
	})
	var res []Group
	for _, gr := range groups {
		res = append(res, Group{At: model.Area(sheet, gr.r1, gr.c1, gr.r2, gr.c2), F: gr.top.e.Formula})
	}
	sort.Slice(loose, func(i, j int) bool {
		a, b := loose[i], loose[j]
		return a.row < b.row || (a.row == b.row && a.col < b.col)
	})
	for _, c := range loose {
		out.Add(c.row, c.col, c.e, view)
	}
	return res
}

// firstRow is the row of the pending cells (the row that was too large).
func (r *reader) firstRow() int {
	if len(r.pending) > 0 {
		return r.pending[0].row
	}
	return 1
}
