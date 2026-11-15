package sheet

import (
	"errors"
	"fmt"
	"math"
	"slices"
	"strconv"

	"github.com/jhnoel/documentdriver/internal/ooxml/tree"
	"github.com/jhnoel/documentdriver/internal/xlsx/formula"
	"github.com/jhnoel/documentdriver/internal/xlsx/sml"
)

// ErrStopRows is returned by ScanOptions.Row to end the band: Scan hands
// over no more rows but finishes the pass, so the Index is complete (used
// range, merges, hidden rows and the rest). It is never returned by Scan.
var ErrStopRows = errors.New("sheet: no more rows wanted")

// ErrNotWorksheet is returned by Scan when the part's root element is not
// a SpreadsheetML worksheet (a chartsheet, a dialogsheet, a macrosheet,
// or anything else).
var ErrNotWorksheet = errors.New("sheet: the part is not a worksheet")

// Problem is a class of content Scan skipped.
type Problem uint8

// The problems.
const (
	// ProblemOrder: a row whose number is at or before the previous
	// row's, or a cell whose column is at or before the previous cell's in
	// its row. The row (with its cells) or the cell is skipped.
	ProblemOrder Problem = iota + 1
	// ProblemGrid: a row or cell whose r does not parse, is outside the
	// grid, or (for a cell) names another row; a merge whose ref does
	// not parse as an area. Skipped.
	ProblemGrid
)

// ScanOptions say which rows Scan hands over and where problems go.
type ScanOptions struct {
	// From and To are the band: the rows handed to Row, 1-based and
	// inclusive. When To is 0 or Row is nil no row is handed over and
	// Scan only builds the Index.
	From, To int
	// ColFrom and ColTo are the columns kept in Row.Cells, 1-based and
	// inclusive; 0 and 0 keep all. Cells outside them still count for the
	// Index (used range, anchors, shared masters).
	ColFrom, ColTo int
	// Row receives each row of the band that holds at least one kept
	// cell, in document order (hidden rows are listed by
	// Index.HiddenRows, whether or not they hold cells). Returning ErrStopRows ends the band;
	// any other error stops Scan, which returns it.
	Row func(*Row) error
	// Problem, when set, is told of each skipped row (col 0) or cell,
	// with a short detail without document text. Merges report row 0.
	Problem func(p Problem, row, col int, detail string)
	// StopAfterBand ends the pass at the first row after To, unless Row
	// ended the band (ErrStopRows): the rest of the part is not
	// tokenized, so the Index covers only what came before that row (no
	// merges, hidden columns or used range past it), and the part is not
	// proved well formed past it. For a caller that wants the band's
	// cells only, of a part already proved well formed.
	StopAfterBand bool
}

// errBandDone ends a StopAfterBand scan.
var errBandDone = errors.New("sheet: band done")

// Scan reads the worksheet src in one streaming pass (tree.ScanString;
// no tree is built) and returns its Index, handing the band's rows to
// opt.Row on the way.
//
// Errors: a *tree.SyntaxError (or another tokenizer error) when src is
// not well-formed XML, ErrNotWorksheet when its root is not a
// worksheet, or the error opt.Row returned. Malformed content inside a
// well-formed sheet is never an error: it is skipped and reported to
// opt.Problem.
//
// Cost: one tokenizer pass over src, whatever the band; memory as the
// package doc says. Rows after the band are still tokenized (merges and
// the used range come after them), but their cells are not kept; with
// StopAfterBand the pass ends at the first row after the band.
func Scan(src string, opt ScanOptions) (*Index, error) {
	if len(src) > math.MaxUint32 {
		return nil, fmt.Errorf("sheet: part of %d bytes exceeds the 4 GiB offset range", len(src))
	}
	s := &scanner{opt: opt, ix: &Index{Ordered: true, shared: map[int]int{}}}
	s.row.Index = s.ix
	if opt.Row == nil || opt.To == 0 {
		s.stopped = true
	}
	if err := tree.ScanString(src, s); err != nil && !errors.Is(err, errBandDone) {
		return nil, err
	}
	if !s.root {
		return nil, ErrNotWorksheet
	}
	slices.SortStableFunc(s.ix.hiddenCols, func(a, b [2]int) int { return a[0] - b[0] })
	return s.ix, nil
}

// Where the scanner is, by the open elements' local names.
const (
	dRoot  = 1 // worksheet
	dPart  = 2 // dimension, cols, sheetData, mergeCells, ...
	dRow   = 3 // row, col, mergeCell
	dCell  = 4 // c
	dValue = 5 // v, f, is
	dRun   = 6 // is/t, is/r
	dRunT  = 7 // is/r/t
)

// collect is which field character data goes to.
type collect uint8

const (
	cNone collect = iota
	cV
	cF
	cIS
)

type scanner struct {
	opt    ScanOptions
	ix     *Index
	ns     tree.NSStack
	frames []frame
	// skip counts open elements inside a subtree being ignored.
	skip int
	root bool // the root is a main-namespace worksheet

	// section is the dPart element being read ("" for none).
	section string
	// The current row.
	row     Row
	rowOpen bool
	emit    bool // the row is in the band and the band is not stopped
	stopped bool // the band is over (ErrStopRows, or no band)
	lastRow int
	lastCol int
	// The current cell.
	cell     RawCell
	cellOpen bool
	keep     bool // the cell's column is kept
	into     collect
	v, f, is textAcc

	act activeSet
}

// Start handles a start-tag event, updating the scan state and returning any row or cell error.
func (s *scanner) Start(n *tree.Node) error {
	s.ns.Push(n)
	local := n.Local()
	main := sml.IsMain(s.ns.Space(n))
	s.frames = append(s.frames, frame{local: local, main: main})
	d := len(s.frames)
	if s.skip > 0 {
		s.skip++
		s.frames[d-1].skipped = true
		return nil
	}
	if !main {
		return s.skipThis()
	}
	parent := ""
	if d >= 2 {
		parent = s.frames[d-2].local
	}
	switch {
	case d == dRoot && local == "worksheet":
		s.root = true
	case d == dPart:
		s.section = local
		switch local {
		case "dimension":
			s.ix.Dimension, s.ix.HasDimension = n.Attr("ref")
			return s.skipThis()
		case "sheetData", "cols", "mergeCells":
		default:
			return s.skipThis()
		}
	case d == dRow && parent == "sheetData" && local == "row":
		return s.startRow(n)
	case d == dRow && parent == "cols" && local == "col":
		s.col(n)
		return s.skipThis()
	case d == dRow && parent == "mergeCells" && local == "mergeCell":
		s.merge(n)
		return s.skipThis()
	case d == dCell && local == "c":
		return s.startCell(n)
	case d == dValue && local == "v":
		s.into = cV
		s.cell.HasV = true
		s.v.reset()
	case d == dValue && local == "f":
		s.into = cF
		s.startFormula(n)
	case d == dValue && local == "is":
		s.cell.HasIS = true
		s.is.reset()
	case d == dRun && parent == "is" && local == "t", d == dRunT && parent == "r" && local == "t":
		s.into = cIS
	case d == dRun && parent == "is" && local == "r":
	default:
		// Anything else: another root, a part element read nowhere, the
		// text of <v> or <f> holding elements, rPh, rPr, phoneticPr, an
		// extension list. Its subtree is ignored.
		return s.skipThis()
	}
	return nil
}

// skipThis marks the element just pushed as the root of an ignored
// subtree.
func (s *scanner) skipThis() error {
	s.frames[len(s.frames)-1].skipped = true
	s.skip = 1
	return nil
}

// End handles an end-tag event, closing the current row, cell, or value element and returning any row error.
func (s *scanner) End(*tree.Node) error {
	f := s.frames[len(s.frames)-1]
	s.frames = s.frames[:len(s.frames)-1]
	s.ns.Pop()
	if s.skip > 0 {
		s.skip--
		return nil
	}
	d := len(s.frames) + 1
	switch d {
	case dPart:
		s.section = ""
	case dRow:
		if s.rowOpen {
			return s.endRow()
		}
	case dCell:
		if s.cellOpen {
			s.endCell()
		}
	case dValue:
		switch f.local {
		case "v":
			s.cell.V = s.v.String()
		case "f":
			s.cell.F.Text = s.f.String()
		case "is":
			s.cell.IS = s.is.String()
		}
		s.into = cNone
	case dRun, dRunT:
		s.into = cNone
	}
	return nil
}

// Leaf adds a text node to the current value, formula, or inline-string accumulator.
func (s *scanner) Leaf(n *tree.Node) error {
	if s.skip > 0 || s.into == cNone || !isText(n) {
		return nil
	}
	switch s.into {
	case cV:
		s.v.add(n.Data())
	case cF:
		s.f.add(n.Data())
	case cIS:
		s.is.add(n.Data())
	}
	return nil
}

// problem reports a skipped item to the Problem callback, when one is set.
func (s *scanner) problem(p Problem, row, col int, detail string) {
	if s.opt.Problem != nil {
		s.opt.Problem(p, row, col, detail)
	}
}

// startRow indexes a new row and prepares it for cells, skipping invalid or out-of-order rows.
func (s *scanner) startRow(n *tree.Node) error {
	num := s.lastRow + 1
	hidden := false
	for name, value := range n.AttrsSeq() {
		switch name {
		case "r":
			r, ok := sml.ParseIndex(value)
			if !ok || r < 1 || r > formula.MaxRows {
				s.problem(ProblemGrid, 0, 0, "row number does not parse or is outside the grid")
				return s.skipThis()
			}
			num = r
		case "hidden":
			hidden = value == "1" || value == "true"
		}
	}
	if num > formula.MaxRows {
		s.problem(ProblemGrid, num, 0, "row is past the last row of the grid")
		return s.skipThis()
	}
	if num <= s.lastRow {
		s.ix.Ordered = false
		s.problem(ProblemOrder, num, 0, "row comes after row "+strconv.Itoa(s.lastRow))
		return s.skipThis()
	}
	if s.opt.StopAfterBand && !s.stopped && num > s.opt.To {
		return errBandDone
	}
	start, _, _ := n.Span()
	e := rowEntry{num: uint32(num), off: uint32(start)} //nolint:gosec // G115: num <= MaxRows and Scan checked len(src) fits
	if hidden {
		e.num |= hiddenBit
	}
	s.ix.rows = append(s.ix.rows, e)
	s.lastRow, s.lastCol = num, 0
	s.act.advance(num)
	s.row.Num, s.row.Hidden, s.row.Cells = num, hidden, s.row.Cells[:0]
	s.rowOpen = true
	s.emit = !s.stopped && num >= s.opt.From && num <= s.opt.To
	return nil
}

// endRow closes the current row and delivers it to the Row callback, if any.
func (s *scanner) endRow() error {
	s.rowOpen = false
	if !s.emit || len(s.row.Cells) == 0 {
		return nil
	}
	if err := s.opt.Row(&s.row); err != nil {
		if errors.Is(err, ErrStopRows) {
			s.stopped = true
			return nil
		}
		return err
	}
	return nil
}

// startCell begins a cell, validating its address and recording its attributes, or skips a bad cell.
func (s *scanner) startCell(n *tree.Node) error {
	if !s.rowOpen {
		return s.skipThis()
	}
	c := RawCell{Row: s.row.Num, Col: s.lastCol + 1, SOK: true}
	hasR := false
	for name, value := range n.AttrsSeq() {
		switch name {
		case "r":
			ref, ok := formula.ParseCell(value)
			if !ok {
				s.problem(ProblemGrid, s.row.Num, 0, "cell address does not parse")
				return s.skipThis()
			}
			c.Row, c.Col, hasR = ref.Row, ref.Col, true
		case "t":
			c.T = value
		case "s":
			c.S, c.SOK = sml.ParseIndex(value)
		case "cm":
			c.CM = positive(value)
		case "vm":
			c.VM = positive(value)
		}
	}
	switch {
	case hasR && c.Row != s.row.Num:
		s.problem(ProblemGrid, s.row.Num, c.Col, "cell address names row "+strconv.Itoa(c.Row))
		return s.skipThis()
	case c.Col < 1 || c.Col > formula.MaxCols:
		s.problem(ProblemGrid, s.row.Num, 0, "cell is past the last column of the grid")
		return s.skipThis()
	case c.Col <= s.lastCol:
		s.problem(ProblemOrder, c.Row, c.Col, "cell comes after column "+formula.ColumnName(s.lastCol))
		return s.skipThis()
	}
	if !c.SOK {
		c.S = 0
	}
	s.lastCol = c.Col
	s.cell = c
	s.cellOpen = true
	s.keep = s.emit && (s.opt.ColFrom == 0 && s.opt.ColTo == 0 || c.Col >= s.opt.ColFrom && c.Col <= s.opt.ColTo)
	return nil
}

// startFormula records a cell's formula type and attributes and resets its text accumulator.
func (s *scanner) startFormula(n *tree.Node) {
	f := Formula{Present: true}
	for name, value := range n.AttrsSeq() {
		switch name {
		case "t":
			switch value {
			case "", "normal":
				f.Type = Normal
			case "shared":
				f.Type = Shared
			case "array":
				f.Type = Array
			case "dataTable":
				f.Type = DataTable
			default:
				f.Type = UnknownType
			}
		case "ref":
			f.Ref = value
		case "si":
			f.SI, f.SIOK = sml.ParseIndex(value)
		case "dt2D":
			f.DT2D = value == "1" || value == "true"
		case "dtr":
			f.DTR = value == "1" || value == "true"
		case "r1":
			f.R1 = value
		case "r2":
			f.R2 = value
		}
	}
	s.cell.F = f
	s.f.reset()
}

// endCell closes the current cell, indexing its shared master or anchor and its used range.
func (s *scanner) endCell() {
	s.cellOpen = false
	c := &s.cell
	if c.F.Present {
		switch c.F.Type {
		case Shared:
			if !c.F.SIOK {
				break
			}
			// Resolved here, in document order, not in the row
			// callback: a later master reusing the si in this row must
			// not capture an earlier child.
			if c.F.Text != "" {
				m := Master{Row: c.Row, Col: c.Col, Text: c.F.Text}
				if r, ok := formula.ParseArea(c.F.Ref); ok {
					m.Ref, m.RefOK = r, true
				}
				s.ix.shared[c.F.SI] = len(s.ix.masters)
				s.ix.masters = append(s.ix.masters, m)
			}
			if i, ok := s.ix.shared[c.F.SI]; ok {
				c.Master = i + 1
			}
		case Array, DataTable:
			if r, ok := formula.ParseArea(c.F.Ref); ok {
				s.ix.anchors = append(s.ix.anchors, Anchor{Row: c.Row, Col: c.Col, Ref: r, Type: c.F.Type, CM: c.CM})
				s.act.anchors = s.ix.anchors
				s.act.join(len(s.ix.anchors) - 1)
			}
		}
	} else if len(s.act.byCol) > 0 {
		c.In = s.act.lookup(c.Row, c.Col)
	}
	if !c.Empty() {
		s.ix.used.Add(c.Row, c.Col)
	}
	if s.keep {
		s.row.Cells = append(s.row.Cells, *c)
	}
}

// col records the hidden column range of a cols/col element, clamped to the grid.
func (s *scanner) col(n *tree.Node) {
	lo, okLo := sml.ParseIndex(attr(n, "min"))
	hi, okHi := sml.ParseIndex(attr(n, "max"))
	h := attr(n, "hidden")
	if !okLo || !okHi || h != "1" && h != "true" {
		return
	}
	lo, hi = max(lo, 1), min(hi, formula.MaxCols)
	if lo <= hi {
		s.ix.hiddenCols = append(s.ix.hiddenCols, [2]int{lo, hi})
	}
}

// merge records a mergeCell element's ref as a merged range, reporting an unparsable ref.
func (s *scanner) merge(n *tree.Node) {
	r, ok := formula.ParseArea(attr(n, "ref"))
	if !ok {
		s.problem(ProblemGrid, 0, 0, "merged range does not parse as an area")
		return
	}
	s.ix.merges = append(s.ix.merges, r)
}

// attr returns the named attribute's value, or an empty string when it is absent.
func attr(n *tree.Node, name string) string {
	v, _ := n.Attr(name)
	return v
}

// positive is a positive integer attribute, or 0.
func positive(s string) int {
	if n, ok := sml.ParseIndex(s); ok && n > 0 {
		return n
	}
	return 0
}

// Head reads only the start of worksheet src, up to sheetData, and
// returns its stored dimension@ref (ok false when there is none). It is
// what the workbook summary needs of each sheet, without tokenizing the
// cells. The error is the tokenizer's for malformed XML before sheetData,
// or ErrNotWorksheet.
func Head(src string) (dimension string, ok bool, err error) {
	h := &headScanner{}
	err = tree.ScanString(src, h)
	switch {
	case errors.Is(err, errHeadDone), err == nil:
	default:
		return "", false, err
	}
	if !h.root {
		return "", false, ErrNotWorksheet
	}
	return h.dim, h.ok, nil
}

var errHeadDone = errors.New("sheet: head read")

type headScanner struct {
	ns     tree.NSStack
	frames []frame
	skip   int
	root   bool
	dim    string
	ok     bool
}

// Start handles a start-tag event, seeking the worksheet root, dimension, or sheetData.
func (h *headScanner) Start(n *tree.Node) error {
	h.ns.Push(n)
	local := n.Local()
	main := sml.IsMain(h.ns.Space(n))
	h.frames = append(h.frames, frame{local: local, main: main})
	d := len(h.frames)
	if h.skip > 0 {
		h.skip++
		h.frames[d-1].skipped = true
		return nil
	}
	switch {
	case d == dRoot && main && local == "worksheet":
		h.root = true
	case d == dPart && main && local == "dimension":
		h.dim, h.ok = n.Attr("ref")
	case d == dPart && main && local == "sheetData":
		return errHeadDone
	case d == dRoot:
		return errHeadDone
	default:
		h.frames[d-1].skipped = true
		h.skip = 1
	}
	return nil
}

// End handles an end-tag event, popping the current frame and namespace.
func (h *headScanner) End(*tree.Node) error {
	h.frames = h.frames[:len(h.frames)-1]
	h.ns.Pop()
	if h.skip > 0 {
		h.skip--
	}
	return nil
}

// Leaf ignores a leaf node and returns nil.
func (*headScanner) Leaf(*tree.Node) error { return nil }
