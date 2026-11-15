package integrity

import (
	"slices"
	"strconv"
	"strings"
	"sync"

	"github.com/jhnoel/documentdriver/internal/ooxml/schema"
	"github.com/jhnoel/documentdriver/internal/ooxml/tree"
	"github.com/jhnoel/documentdriver/internal/xlsx/formula"
	"github.com/jhnoel/documentdriver/internal/xlsx/sml"
)

// worksheetModel is the content model of CT_Worksheet by child name.
var worksheetModel = sync.OnceValue(func() map[string]schema.Particle {
	m, _ := schema.ContentModel("x:CT_Worksheet")
	out := make(map[string]schema.Particle, len(m.Children))
	for _, p := range m.Children {
		out[p.Local] = p
	}
	return out
})

// cellTypes is the enumeration of ST_CellType, plus "d" (an ISO 8601 date),
// which the pinned ECMA-376 first-edition transitional schema lacks and ISO
// 29500 and Excel 2010 onward have.
var cellTypes = sync.OnceValue(func() []string {
	t, _ := schema.AttrType("x:CT_Cell", "t")
	return append(schema.Enum(t), "d")
})

// containers are worksheet children that must hold at least one child.
var containers = map[string]bool{
	"cols": true, "mergeCells": true, "hyperlinks": true, "conditionalFormatting": true,
	"dataValidations": true, "tableParts": true, "rowBreaks": true, "colBreaks": true,
	"ignoredErrors": true, "protectedRanges": true, "scenarios": true,
	"customSheetViews": true, "cellWatches": true, "oleObjects": true, "controls": true,
}

// headerCell is a table header cell as the sheet stores it.
type headerCell struct {
	t    string // the cell's t attribute
	text string // v, or the inline string's text
	set  bool
}

// sheetScan checks one worksheet in a single streaming pass. What it
// keeps: per-row cursor state, the shared-formula groups, the merges, the
// header cells tables ask for.
type sheetScan struct {
	f           *Findings
	part, sheet string
	nxf, nsst   int // cellXfs and shared strings counts, -1 unknown
	wants       map[int][][2]int
	hdr         map[[2]int]headerCell

	ns     tree.NSStack
	frames []frame
	skip   int
	dead   bool

	// the worksheet's children
	lastRank int
	counts   map[string]int
	contN    int

	// rows and cells
	lastRow, curRow, lastCol int
	inCell                   bool
	cellCol                  int
	cellColOK                bool
	cellT, cellS             string
	vText                    strings.Builder
	vLen, isLen              int
	inIS, inRPh              bool
	isText                   strings.Builder
	// formula
	fAttrT, fAttrSI, fAttrRef string
	fHasRef                   bool
	fLen                      int

	masters, children map[string]struct{}
	merges            []rect
	lastColMax        int
}

// newSheetScan returns a sheetScan checking part for sheet with the given counts and header requests.
func newSheetScan(f *Findings, part, sheet string, nxf, nsst int, wants map[int][][2]int) *sheetScan {
	return &sheetScan{f: f, part: part, sheet: sheet, nxf: nxf, nsst: nsst, wants: wants,
		hdr: map[[2]int]headerCell{}, counts: map[string]int{},
		masters: map[string]struct{}{}, children: map[string]struct{}{}}
}

// at returns the sheet-qualified form of the cell ref ref.
func (s *sheetScan) at(ref string) string { return s.sheet + "!" + ref }

// Start handles a worksheet start element, checking it and recording any findings, returning nil.
func (s *sheetScan) Start(n *tree.Node) error {
	if s.dead {
		s.ns.Push(n)
		s.frames = append(s.frames, frame{skipped: true})
		return nil
	}
	s.ns.Push(n)
	local, main := n.Local(), s.ns.Space(n) == sml.NSMain
	s.frames = append(s.frames, frame{local: local})
	d := len(s.frames)
	fr := &s.frames[d-1]
	if s.skip > 0 || (d > 1 && !main) {
		s.skip++
		fr.skipped = true
		return nil
	}
	if d == 1 {
		if !main || local != "worksheet" {
			s.f.add(WorksheetRoot, s.part, "", "root element "+n.Name())
			s.dead = true
			fr.skipped = true
		}
		return nil
	}
	parent := s.frames[d-2].local
	switch d {
	case 2:
		s.topChild(n, local)
	case 3:
		s.contN++
		switch {
		case parent == "sheetData" && local == "row":
			s.startRow(n)
		case parent == "mergeCells" && local == "mergeCell":
			s.merge(n)
		case parent == "cols" && local == "col":
			s.col(n)
		case parent == "tableParts" && local == "tablePart":
			if _, ok := s.ns.AttrNS(n, sml.NSRel, "id"); !ok {
				s.f.add(WorksheetTableRel, s.part, "", "tablePart without r:id")
			}
		}
	case 4:
		if parent == "row" && local == "c" && s.frames[1].local == "sheetData" {
			s.startCell(n)
		}
	case 5:
		if s.inCell && parent == "c" {
			s.cellChild(n, local)
		}
	}
	if d == 6 && s.inIS && local == "rPh" {
		s.inRPh = true
	}
	return nil
}

// topChild checks a top-level worksheet child for dimension, order and count faults.
func (s *sheetScan) topChild(n *tree.Node, local string) {
	s.contN = 0
	switch local {
	case "dimension":
		if ref, ok := n.Attr("ref"); ok {
			if _, ok := parseRange(ref); !ok {
				s.f.add(WorksheetDimension, s.part, "", "dimension "+ref)
			}
		}
	}
	p, known := worksheetModel()[local]
	if !known {
		return
	}
	if p.Rank != schema.NoRank {
		if p.Rank < s.lastRank {
			s.f.add(WorksheetChildOrder, s.part, local, local+" is out of order in "+s.sheet)
		}
		s.lastRank = max(s.lastRank, p.Rank)
	}
	s.counts[local]++
	if p.Max != schema.Unbounded && s.counts[local] > p.Max {
		s.f.add(WorksheetChildCount, s.part, local, local+" occurs more than "+strconv.Itoa(p.Max)+" times in "+s.sheet)
	}
}

// startRow checks a row element and updates the current row from its r attribute.
func (s *sheetScan) startRow(n *tree.Node) {
	r := s.lastRow + 1
	if v, ok := n.Attr("r"); ok {
		if r, ok = sml.ParseIndex(v); !ok {
			s.f.add(WorksheetRowOrder, s.part, "r", "row r="+v+" is not a number in "+s.sheet)
			r = s.lastRow + 1
		}
	}
	switch {
	case r < 1 || r > maxRows:
		s.f.add(LimitRow, s.part, "", "row "+strconv.Itoa(r)+" in "+s.sheet)
	case r <= s.lastRow:
		s.f.add(WorksheetRowOrder, s.part, "", "row "+strconv.Itoa(r)+" follows row "+strconv.Itoa(s.lastRow)+" in "+s.sheet)
	}
	s.curRow, s.lastRow, s.lastCol = r, max(s.lastRow, r), 0
	if v, ok := n.Attr("s"); ok {
		s.checkStyle(v, "row "+strconv.Itoa(r))
	}
}

// checkStyle records a WorksheetStyle finding when style v is invalid or out of range.
func (s *sheetScan) checkStyle(v, where string) {
	i, ok := sml.ParseIndex(v)
	if !ok || s.nxf >= 0 && i >= s.nxf {
		s.f.add(WorksheetStyle, s.part, v, "style "+v+" at "+s.at(where)+" with "+strconv.Itoa(s.nxf)+" cell formats")
	}
}

// merge records a mergeCell element's ref, or a finding when it is invalid or out of limits.
func (s *sheetScan) merge(n *tree.Node) {
	ref, _ := n.Attr("ref")
	r, ok := parseRange(ref)
	if !ok || !r.inLimits() {
		s.f.add(WorksheetMergeRef, s.part, "", "merge "+ref+" in "+s.sheet)
		return
	}
	s.merges = append(s.merges, r)
}

// col checks a col element's column range, order, limit and style.
func (s *sheetScan) col(n *tree.Node) {
	lo, ok1 := sml.ParseIndex(attr(n, "min"))
	hi, ok2 := sml.ParseIndex(attr(n, "max"))
	switch {
	case !ok1 || !ok2 || lo < 1 || hi < lo:
		s.f.add(WorksheetCols, s.part, "range", "col min="+attr(n, "min")+" max="+attr(n, "max")+" in "+s.sheet)
	case hi > maxCols:
		s.f.add(LimitColumn, s.part, "", "col max="+strconv.Itoa(hi)+" in "+s.sheet)
	case lo <= s.lastColMax:
		s.f.add(WorksheetCols, s.part, "order", "col min="+strconv.Itoa(lo)+" overlaps or follows an earlier col in "+s.sheet)
	}
	if ok2 {
		s.lastColMax = max(s.lastColMax, hi)
	}
	if v, ok := n.Attr("style"); ok {
		s.checkStyle(v, "column "+attr(n, "min"))
	}
}

// attr returns the value of node n's attribute name, or "" when absent.
func attr(n *tree.Node, name string) string {
	v, _ := n.Attr(name)
	return v
}

// startCell checks a cell element and starts recording its type, style and reference.
func (s *sheetScan) startCell(n *tree.Node) {
	s.inCell = true
	s.vText.Reset()
	s.isText.Reset()
	s.vLen, s.isLen = 0, 0
	s.inIS, s.inRPh = false, false
	s.cellT, s.cellS = "", ""
	s.cellCol, s.cellColOK = s.lastCol+1, true
	for name, v := range n.AttrsSeq() {
		switch name {
		case "t":
			s.cellT = v
		case "s":
			s.cellS = v
		case "r":
			col, row, ok := parseCell(v)
			switch {
			case !ok:
				s.f.add(WorksheetCellRef, s.part, "", "cell r="+v+" in "+s.sheet)
				s.cellColOK = false
			case row != s.curRow:
				s.f.add(WorksheetCellRow, s.part, "", "cell "+v+" is in row "+strconv.Itoa(s.curRow)+" of "+s.sheet)
				s.cellCol = col
			default:
				s.cellCol = col
			}
		}
	}
	if s.cellColOK {
		if s.cellCol <= s.lastCol {
			s.f.add(WorksheetCellOrder, s.part, "", "cell "+cellName(s.cellCol, s.curRow)+" follows column "+formula.ColumnName(s.lastCol)+" in "+s.sheet)
		}
		if s.cellCol > maxCols {
			s.f.add(LimitColumn, s.part, "", "cell in column "+strconv.Itoa(s.cellCol)+" of "+s.sheet)
		}
		s.lastCol = max(s.lastCol, s.cellCol)
	}
}

// cellChild records a cell child's formula attributes or marks an inline string.
func (s *sheetScan) cellChild(n *tree.Node, local string) {
	switch local {
	case "f":
		s.fLen = 0
		s.fAttrT, s.fAttrSI, s.fAttrRef = attr(n, "t"), attr(n, "si"), attr(n, "ref")
		_, s.fHasRef = n.Attr("ref")
	case "is":
		s.inIS = true
	}
}

// Leaf accumulates a cell's value, formula or inline-string text and returns nil.
func (s *sheetScan) Leaf(n *tree.Node) error {
	if s.dead || s.skip > 0 || !s.inCell || !isText(n) {
		return nil
	}
	k := len(s.frames)
	top := s.frames[k-1].local
	switch {
	case k == 5 && top == "v":
		data := n.Data()
		s.vLen += utf16Len(data)
		if s.vText.Len() < 4*maxCellChars+16 {
			s.vText.WriteString(data)
		}
	case k == 5 && top == "f":
		s.fLen += utf16Len(n.Data())
	case s.inIS && !s.inRPh && top == "t":
		data := n.Data()
		s.isLen += utf16Len(data)
		if s.isText.Len() < 4*maxCellChars+16 {
			s.isText.WriteString(data)
		}
	}
	return nil
}

// End closes a worksheet element, finishing cells, formulas and containers and returns nil.
func (s *sheetScan) End(*tree.Node) error {
	if s.dead {
		if k := len(s.frames); k > 0 {
			s.frames = s.frames[:k-1]
		}
		s.ns.Pop()
		return nil
	}
	d := len(s.frames)
	fr := s.frames[d-1]
	if fr.skipped {
		s.frames = s.frames[:d-1]
		s.ns.Pop()
		s.skip--
		return nil
	}
	switch {
	case d == 2 && containers[fr.local] && s.contN == 0:
		s.f.add(WorksheetEmpty, s.part, fr.local, fr.local+" has no children in "+s.sheet)
	case d == 4 && fr.local == "c" && s.inCell:
		s.endCell()
	case d == 5 && fr.local == "f" && s.inCell:
		s.endFormula()
	case d == 5 && fr.local == "is":
		s.inIS = false
	case d == 6 && fr.local == "rPh":
		s.inRPh = false
	case d == 1:
		s.finish()
	}
	s.frames = s.frames[:d-1]
	s.ns.Pop()
	return nil
}

// endFormula checks a formula's length and shared or array reference when it ends.
func (s *sheetScan) endFormula() {
	if s.fLen > maxFormulaLen {
		s.f.add(LimitFormula, s.part, "", "formula of "+strconv.Itoa(s.fLen)+" characters at "+s.at(cellName(s.cellCol, s.curRow)))
	}
	switch s.fAttrT {
	case "shared":
		if s.fHasRef || s.fLen > 0 {
			if _, dup := s.masters[s.fAttrSI]; dup {
				s.f.add(WorksheetSharedDup, s.part, s.fAttrSI, "shared formula group "+s.fAttrSI+" has two masters in "+s.sheet)
			}
			s.masters[s.fAttrSI] = struct{}{}
		} else {
			s.children[s.fAttrSI] = struct{}{}
		}
		if s.fHasRef {
			s.checkArrayRef(s.fAttrRef, false)
		}
	case "array", "dataTable":
		s.checkArrayRef(s.fAttrRef, true)
	}
}

// checkArrayRef records a finding for an invalid or out-of-limits formula ref, or one excluding its cell when containment is required.
func (s *sheetScan) checkArrayRef(ref string, mustContain bool) {
	r, ok := parseRange(ref)
	where := s.at(cellName(s.cellCol, s.curRow))
	switch {
	case !ok || !r.inLimits():
		s.f.add(WorksheetArrayRef, s.part, "", "formula ref "+ref+" at "+where)
	case mustContain && s.cellColOK && !r.contains(s.cellCol, s.curRow):
		s.f.add(WorksheetArrayRef, s.part, "", "formula ref "+ref+" does not contain "+where)
	}
}

// endCell checks a finished cell and records it as a table header when requested.
func (s *sheetScan) endCell() {
	s.inCell = false
	where := func() string { return cellName(s.cellCol, s.curRow) }
	if i, ok := sml.ParseIndex(s.cellS); s.cellS != "" && (!ok || s.nxf >= 0 && i >= s.nxf) {
		s.checkStyle(s.cellS, where())
	}
	if s.cellT != "" && !slices.Contains(cellTypes(), s.cellT) {
		s.f.add(WorksheetCellType, s.part, s.cellT, "cell "+s.at(where())+" has t="+s.cellT)
	}
	if s.vLen > maxCellChars || s.isLen > maxCellChars {
		s.f.add(LimitCellText, s.part, "", "cell "+s.at(where())+" has more than 32,767 characters")
	}
	var idx int
	if s.cellT == "s" {
		v := strings.TrimSpace(s.vText.String())
		i, ok := sml.ParseIndex(v)
		switch {
		case !ok:
			s.f.add(WorksheetSST, s.part, "nan", "cell "+s.at(where())+" has shared string index "+v)
		case s.nsst >= 0 && i >= s.nsst:
			s.f.add(WorksheetSST, s.part, strconv.Itoa(i), "cell "+s.at(where())+" has shared string index "+strconv.Itoa(i)+" of "+strconv.Itoa(s.nsst))
		}
		idx = i
	}
	if cols, ok := s.wants[s.curRow]; ok && s.cellColOK {
		for _, c := range cols {
			if s.cellCol >= c[0] && s.cellCol <= c[1] {
				h := headerCell{t: s.cellT, set: true}
				switch s.cellT {
				case "s":
					h.text = strconv.Itoa(idx)
				case "inlineStr":
					h.text = s.isText.String()
				default:
					h.text = s.vText.String()
				}
				s.hdr[[2]int{s.curRow, s.cellCol}] = h
			}
		}
	}
}

// finish checks the sheet's shared formulas for orphans and its merges for overlaps.
func (s *sheetScan) finish() {
	for si := range s.children {
		if _, ok := s.masters[si]; !ok {
			s.f.add(WorksheetSharedOrph, s.part, si, "shared formula group "+si+" has no master in "+s.sheet)
		}
	}
	s.checkMerges()
}

// checkMerges finds overlapping merges by a sweep over the merges sorted
// by their first row, keeping the ones still open. The work is capped, so
// a sheet of hostile merges costs bounded time; the cap is noted.
func (s *sheetScan) checkMerges() {
	const budget = 5_000_000
	ms := s.merges
	slices.SortFunc(ms, func(a, b rect) int { return a.r1 - b.r1 })
	var open []rect
	work := 0
	found := 0
	for _, m := range ms {
		kept := open[:0]
		for _, o := range open {
			if o.r2 >= m.r1 {
				kept = append(kept, o)
			}
		}
		open = kept
		for _, o := range open {
			work++
			if work > budget {
				s.f.note("merge overlap check of " + s.sheet + " stopped at its work budget")
				return
			}
			if o.overlaps(m) {
				found++
				s.f.add(WorksheetMergeOver, s.part, "", "merges overlap in "+s.sheet+": "+cellName(o.c1, o.r1)+":"+cellName(o.c2, o.r2)+" and "+cellName(m.c1, m.r1)+":"+cellName(m.c2, m.r2))
				if found >= maxInstances {
					return
				}
			}
		}
		open = append(open, m)
	}
}
