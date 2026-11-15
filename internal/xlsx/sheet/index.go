package sheet

import (
	"github.com/jhnoel/documentdriver/internal/xlsx/formula"
)

// Index is what one Scan of a worksheet learned, kept for the reader's
// output and for P2's edit overlay. It is read-only once Scan returns.
//
// Memory: 8 bytes per <row> (row number with the hidden flag, and the
// byte offset of its start tag), plus O(merges + shared groups + array
// formulas + hidden column runs). Shared masters keep their formula text.
type Index struct {
	// Dimension is dimension@ref as stored; HasDimension says the element
	// and attribute were present.
	Dimension    string
	HasDimension bool
	// Ordered is false when a row came at or before a previous row's
	// number (it was skipped and reported to ScanOptions.Problem).
	Ordered bool

	rows       []rowEntry
	used       Bounds
	merges     []formula.Range
	hiddenCols [][2]int
	// masters are the shared-formula masters in document order
	// (append-only; RawCell.Master indexes it); shared maps an si to the
	// index of its latest master.
	masters []Master
	shared  map[int]int
	anchors []Anchor
}

// rowEntry is one row: num holds the row number in its low 31 bits and
// the hidden flag in its top bit; off is the byte offset of "<row".
type rowEntry struct {
	num uint32
	off uint32
}

const hiddenBit = 1 << 31

// row returns the row number encoded in e.
func (e rowEntry) row() int { return int(e.num &^ hiddenBit) }

// isHidden reports whether e's hidden flag is set.
func (e rowEntry) isHidden() bool { return e.num&hiddenBit != 0 }

// Master is a shared formula group's master as the sheet stores it.
type Master struct {
	// Row and Col are the master cell.
	Row, Col int
	// Ref is the master's ref attribute (ok false when it does not parse;
	// a child is still translated from the master).
	Ref   formula.Range
	RefOK bool
	// Text is the master's stored formula text.
	Text string
}

// Anchor is an array formula's or a data table's anchor cell.
type Anchor struct {
	// Row and Col are the anchor cell (the cell carrying <f t="array"> or
	// <f t="dataTable">).
	Row, Col int
	// Ref is the area the formula fills, normalised (top-left to
	// bottom-right). An anchor whose ref is absent or does not parse is
	// not indexed.
	Ref formula.Range
	// Type is Array or DataTable.
	Type FormulaType
	// CM is the anchor cell's cm attribute (0 when absent): the reader
	// resolves it through xl/metadata.xml to tell a dynamic spill from a
	// CSE array.
	CM int
}

// Rows is the number of rows indexed (rows Scan skipped as out of order
// or outside the grid are not).
func (ix *Index) Rows() int { return len(ix.rows) }

// Row returns the i-th indexed row: its number, whether it is hidden,
// and the byte offset of its start tag in the scanned source.
func (ix *Index) Row(i int) (num int, hidden bool, off int) {
	e := ix.rows[i]
	return e.row(), e.isHidden(), int(e.off)
}

// UsedRange is the bounding rectangle of the cells that are not Empty
// (style-only cells excluded, survey R13); ok is false when the sheet has
// none. It covers every row and column of the sheet, not only the band.
func (ix *Index) UsedRange() (r1, c1, r2, c2 int, ok bool) {
	u := ix.used
	return u.R1, u.C1, u.R2, u.C2, u.OK
}

// HiddenRows returns the runs of hidden rows that intersect rows r1..r2,
// clipped to them, as [from, to] pairs in ascending order. Rows that are
// absent from the file are not hidden: a run is consecutive row numbers
// each with its own hidden <row>.
func (ix *Index) HiddenRows(r1, r2 int) [][2]int {
	var out [][2]int
	for _, e := range ix.rows {
		r := e.row()
		if r < r1 || r > r2 || !e.isHidden() {
			continue
		}
		if k := len(out); k > 0 && out[k-1][1] == r-1 {
			out[k-1][1] = r
			continue
		}
		out = append(out, [2]int{r, r})
	}
	return out
}

// HiddenCols returns the runs of hidden columns (cols/col@hidden) that
// intersect columns c1..c2, clipped to them, merged where they touch, in
// ascending order.
func (ix *Index) HiddenCols(c1, c2 int) [][2]int {
	var out [][2]int
	for _, run := range ix.hiddenCols {
		from, to := max(run[0], c1), min(run[1], c2)
		if from > to {
			continue
		}
		if k := len(out); k > 0 && out[k-1][1] >= from-1 {
			out[k-1][1] = max(out[k-1][1], to)
			continue
		}
		out = append(out, [2]int{from, to})
	}
	return out
}

// Merges returns the merged ranges (mergeCells/mergeCell@ref that parse
// as a Cells range), normalised, in document order. The caller selects
// those that intersect what it reads.
func (ix *Index) Merges() []formula.Range { return ix.merges }

// Masters returns the shared-formula masters in document order. It is
// append-only during a scan: RawCell.Master is a 1-based index into it
// and stays valid. A master is a shared <f> with an si and a text.
func (ix *Index) Masters() []Master { return ix.masters }

// Master returns the latest master of shared group si read so far. After
// Scan returns that is the last one in the sheet. A reader resolving a
// child uses RawCell.Master instead: an si may be reused (a later master
// with the same si, even in the same row), and only the master in effect
// at the child's position is right.
func (ix *Index) Master(si int) (Master, bool) {
	i, ok := ix.shared[si]
	if !ok {
		return Master{}, false
	}
	return ix.masters[i], true
}

// Anchors returns the array and data-table anchors in document order;
// RawCell.In is a 1-based index into it.
func (ix *Index) Anchors() []Anchor { return ix.anchors }
