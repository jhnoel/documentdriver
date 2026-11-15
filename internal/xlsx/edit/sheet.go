package edit

import (
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/jhnoel/documentdriver/internal/ooxml/splice"
	"github.com/jhnoel/documentdriver/internal/ooxml/tree"
	"github.com/jhnoel/documentdriver/internal/ooxml/xmlscan"
	"github.com/jhnoel/documentdriver/internal/opc"
	"github.com/jhnoel/documentdriver/internal/xlsx/formula"
	"github.com/jhnoel/documentdriver/internal/xlsx/model"
	"github.com/jhnoel/documentdriver/internal/xlsx/sheet"
)

// Sheet is one worksheet open for editing: its text, its index and the
// overlay of the plan's edits to it.
//
// The text an edit is made against is the base: the part's original
// string until a structural op gives the sheet a new text (SetText), and
// that text after. Edits are kept in the overlay until Finish (or Text)
// flushes them, in one pass, into a splice patch over the base:
//
//   - cell edits: a new <c> element, or a deletion, by row and column;
//   - row edits: attributes of a row's start tag, by row;
//   - element edits: the whole of a top-level element other than
//     sheetData (dimension, cols, mergeCells, ...), by local name.
//
// Reads (Cell, Each, RowAttrs, Element) see the overlay over the base, so
// each op sees the sheet as the ops before it left it. A Sheet is used by
// one plan and is not safe for concurrent use.
type Sheet struct {
	w    *Workspace
	ref  SheetRef
	part *opc.Part
	// orig is the part's original string; base the text edits are made
	// against (orig until SetText); gen counts SetText calls.
	orig, base string
	gen        int
	// added is set for a part this plan added: it has no original to
	// patch, so Finish saves its text whole (SetBytes).
	added bool
	ix    *sheet.Index
	lay   *layout
	// origStart and origEnd bound the original root's content. After the
	// first SetText the original is not held (orig is ""; the part's
	// cache is Forgotten): origHead and origTail keep the bytes before
	// and after the root's content, and flush reads the original again
	// (structural memory stays within 3 N: the current text, the
	// kernel's output and the builder at most, never the original too).
	origStart, origEnd int
	origHead, origTail string

	cells map[cellKey]pendingCell
	rows  map[int][]AttrEdit
	elems map[string]string
	// grow bounds the cells the overlay made non-empty, for dimension.
	grow sheet.Bounds
	// cache is the last row located in the base.
	cache rowCache
}

type cellKey struct{ row, col int32 }

// pendingCell is a cell edit: its new <c> (and the RawCell it reads
// as), or a deletion.
type pendingCell struct {
	xml string
	raw sheet.RawCell
	del bool
}

type rowCache struct {
	ok    bool
	num   int
	found bool
	span  rowSpan
	cells []cellSpan
	other int
}

// AttrEdit sets attribute Name to Value, or removes it when Remove.
type AttrEdit struct {
	Name, Value string
	Remove      bool
}

// Cell is one cell as the plan's ops so far leave it.
type Cell struct {
	Row, Col int
	// Exists is false for a cell the sheet does not hold (no <c>).
	Exists bool
	// Raw is the cell as stored (sheet.RawCell). Its Master and In
	// fields are 0: shared groups and arrays are read from Index.
	Raw sheet.RawCell
	// XML is the cell's <c> element as it stands ("" when it does not
	// exist, and in Each, which does not copy it).
	XML string
}

// openSheet opens ref's part: one sheet.Scan for the index, then the
// layout. A part that is not well formed, not a worksheet, or that Scan
// had to skip rows or cells of is *Unreadable.
func openSheet(w *Workspace, ref SheetRef) (*Sheet, error) {
	target := model.SheetID(ref.Name)
	pt, err := w.pkg.Part(ref.Part)
	if err != nil {
		return nil, &Unreadable{Target: target, Detail: err.Error()}
	}
	src, ok, err := pt.OriginalString()
	if err != nil {
		if errors.Is(err, opc.ErrLimit) {
			return nil, err
		}
		return nil, &Unreadable{Target: target, Detail: err.Error()}
	}
	added := false
	if !ok {
		// A part this plan added (sheet.add): edited in memory and saved
		// whole at Finish.
		b, err := pt.Bytes()
		if err != nil {
			return nil, &Unreadable{Target: target, Detail: err.Error()}
		}
		src, added = string(b), true
	}
	s := &Sheet{w: w, ref: ref, part: pt, orig: src, added: added}
	if err := s.index(src); err != nil {
		return nil, err
	}
	s.origStart, s.origEnd = s.lay.contentStart, s.lay.contentEnd
	return s, nil
}

// index scans text and finds its layout, making it the base.
func (s *Sheet) index(text string) error {
	target := model.SheetID(s.ref.Name)
	skipped := ""
	ix, err := sheet.Scan(text, sheet.ScanOptions{Problem: func(_ sheet.Problem, row, _ int, detail string) {
		if skipped == "" {
			skipped = detail
			if row > 0 {
				skipped = "row " + strconv.Itoa(row) + ": " + detail
			}
		}
	}})
	switch {
	case errors.Is(err, sheet.ErrNotWorksheet):
		return &Unreadable{Target: target, Detail: "the part is not a worksheet"}
	case err != nil:
		return &Unreadable{Target: target, Detail: "the worksheet is not well-formed XML: " + err.Error()}
	case skipped != "":
		return &Unreadable{Target: target, Detail: "rows or cells are out of order or outside the grid (" + skipped + ")"}
	}
	lay, err := locateLayout(text, ix)
	if err != nil {
		return &Unreadable{Target: target, Detail: err.Error()}
	}
	s.base, s.ix, s.lay = text, ix, lay
	s.cells, s.rows, s.elems = map[cellKey]pendingCell{}, map[int][]AttrEdit{}, map[string]string{}
	s.grow, s.cache = sheet.Bounds{}, rowCache{}
	return nil
}

// Name returns the sheet's name.
func (s *Sheet) Name() string { return s.ref.Name }

// Ref returns the sheet's workbook entry.
func (s *Sheet) Ref() SheetRef { return s.ref }

// PartName returns the sheet's part name.
func (s *Sheet) PartName() string { return s.ref.Part }

// Index returns the index of the base. It does not see the overlay:
// shared-formula masters, array anchors, merges and the used range are
// those of the base text (the original, or the last SetText).
func (s *Sheet) Index() *sheet.Index { return s.ix }

// Prefix returns the prefix new elements of this sheet take, with its
// colon ("x:"), or "" for the default namespace.
func (s *Sheet) Prefix() string { return s.lay.prefix }

// RootDeclarations returns the worksheet root's namespace declarations
// as written (` xmlns="…" xmlns:r="…"`): an element Element returns is
// well formed inside a root carrying them (its r:id, x14ac: and other
// prefixes are declared there).
func (s *Sheet) RootDeclarations() string { return s.lay.decls }

// Namespace returns the sheet's main namespace (Transitional or Strict).
func (s *Sheet) Namespace() string { return s.lay.ns }

// dirty reports whether the sheet has pending edits to write.
func (s *Sheet) dirty() bool {
	return s.added || s.gen > 0 || len(s.cells) > 0 || len(s.rows) > 0 || len(s.elems) > 0
}

// inGrid reports whether row and col lie inside the worksheet grid.
func inGrid(row, col int) bool {
	return row >= 1 && row <= formula.MaxRows && col >= 1 && col <= formula.MaxCols
}

// locate returns the base's row num, using the one-row cache.
func (s *Sheet) locate(num int) (rowCache, error) {
	if s.cache.ok && s.cache.num == num {
		return s.cache, nil
	}
	c := rowCache{ok: true, num: num}
	i, found := findRow(s.ix, num)
	if found {
		rs, err := locateRow(s.base, s.ix, i)
		if err != nil {
			return rowCache{}, s.unreadable(err)
		}
		cells, other, err := rowChildren(s.base, rs)
		if err != nil {
			return rowCache{}, s.unreadable(err)
		}
		c.found, c.span, c.cells, c.other = true, rs, cells, other
	}
	s.cache = c
	return c, nil
}

// unreadable wraps err as an Unreadable for this sheet and returns it.
func (s *Sheet) unreadable(err error) error {
	return &Unreadable{Target: model.SheetID(s.ref.Name), Detail: err.Error()}
}

// Cell returns cell (row, col) as the overlay over the base leaves it.
// Cost: the row's bytes in the base (the last row is cached), and a scan
// of the cell's own bytes.
func (s *Sheet) Cell(row, col int) (Cell, error) {
	out := Cell{Row: row, Col: col}
	if !inGrid(row, col) {
		return out, fmt.Errorf("edit: cell %d,%d is outside the grid", row, col)
	}
	if p, ok := s.cells[cellKey{int32(row), int32(col)}]; ok { //nolint:gosec // G115: inGrid bounds both
		if !p.del {
			out.Exists, out.Raw, out.XML = true, p.raw, p.xml
		}
		return out, nil
	}
	rc, err := s.locate(row)
	if err != nil || !rc.found {
		return out, err
	}
	for _, c := range rc.cells {
		if c.col == col {
			xml := s.base[c.start:c.end]
			raw, err := s.parseCell(row, col, xml)
			if err != nil {
				return out, s.unreadable(err)
			}
			out.Exists, out.Raw, out.XML = true, raw, xml
			return out, nil
		}
	}
	return out, nil
}

// parseCell reads one <c> element of this sheet as a RawCell, by
// scanning it inside a minimal worksheet in the sheet's namespace and
// prefix. It fails when xml is not exactly one well-formed cell at (row,
// col).
func (s *Sheet) parseCell(row, col int, xml string) (sheet.RawCell, error) {
	p := s.lay.prefix
	doc := "<" + p + "worksheet " + xmlnsFor(p) + `="` + s.lay.ns + `"><` + p + "sheetData><" + p + `row r="` +
		strconv.Itoa(row) + `">` + xml + "</" + p + "row></" + p + "sheetData></" + p + "worksheet>"
	var got []sheet.RawCell
	problem := ""
	_, err := sheet.Scan(doc, sheet.ScanOptions{From: row, To: row,
		Row: func(r *sheet.Row) error {
			got = append(got, r.Cells...)
			return nil
		},
		Problem: func(_ sheet.Problem, _, _ int, d string) { problem = d },
	})
	switch {
	case err != nil:
		return sheet.RawCell{}, fmt.Errorf("edit: cell XML: %w", err)
	case problem != "":
		return sheet.RawCell{}, fmt.Errorf("edit: cell XML: %s", problem)
	case len(got) == 1 && got[0].Row == row && got[0].Col != col && !hasR(xml):
		// A cell stored without r (its position implied by its order,
		// valid OOXML) reads as column 1 on its own: it is at col.
		got[0].Col = col
	case len(got) != 1 || got[0].Row != row || got[0].Col != col:
		return sheet.RawCell{}, fmt.Errorf("edit: cell XML is not one cell at %s", formula.ColumnName(col)+strconv.Itoa(row))
	}
	return got[0], nil
}

// hasR reports whether the start tag of the cell XML x has an r attribute.
func hasR(x string) bool {
	n, err := xmlscan.Next(x, 0)
	if err != nil {
		return true // not a tag: let the caller's check refuse it
	}
	_, ok := xmlscan.FindAttr(x, n, "r")
	return ok
}

// Each calls fn for every cell in rows r1..r2 and columns c1..c2 that
// exists as the overlay over the base leaves it, in row-major order. It
// scans the base once (sheet.Scan with that band); Cell.XML is empty.
// fn's error stops the walk and is returned.
func (s *Sheet) Each(r1, c1, r2, c2 int, fn func(Cell) error) error {
	var pend []cellKey
	for k := range s.cells {
		if int(k.row) >= r1 && int(k.row) <= r2 && int(k.col) >= c1 && int(k.col) <= c2 {
			pend = append(pend, k)
		}
	}
	slices.SortFunc(pend, func(a, b cellKey) int {
		if a.row != b.row {
			return int(a.row - b.row)
		}
		return int(a.col - b.col)
	})
	next := 0
	emitPending := func(before cellKey) error {
		for next < len(pend) && (pend[next].row < before.row || (pend[next].row == before.row && pend[next].col < before.col)) {
			p := s.cells[pend[next]]
			k := pend[next]
			next++
			if p.del {
				continue
			}
			if err := fn(Cell{Row: int(k.row), Col: int(k.col), Exists: true, Raw: p.raw}); err != nil {
				return err
			}
		}
		return nil
	}
	_, err := sheet.Scan(s.base, sheet.ScanOptions{From: r1, To: r2, ColFrom: c1, ColTo: c2,
		Row: func(r *sheet.Row) error {
			for _, rc := range r.Cells {
				k := cellKey{int32(rc.Row), int32(rc.Col)} //nolint:gosec // G115: Scan keeps cells inside the grid
				if err := emitPending(k); err != nil {
					return err
				}
				if next < len(pend) && pend[next] == k {
					continue // the overlay's version is emitted by emitPending
				}
				raw := rc
				raw.Master, raw.In = 0, 0
				if err := fn(Cell{Row: rc.Row, Col: rc.Col, Exists: true, Raw: raw}); err != nil {
					return err
				}
			}
			return nil
		}})
	if err != nil {
		return err
	}
	return emitPending(cellKey{int32(formula.MaxRows + 1), 0})
}

// SetCell makes xml, one <c> element in this sheet's prefix whose r is
// (row, col), the cell at (row, col); xml "" deletes the cell. The
// element is scanned now (an error for anything but one well-formed
// cell at that position), so a bad edit fails at its op, never at save.
// It marks the workspace's recalculation state: values, and formulas
// when the old or the new cell has a formula. Cost: the cell's row in
// the base (cached) and the new bytes; nothing is written until Finish.
func (s *Sheet) SetCell(row, col int, xml string) error { return s.setCell(row, col, xml, true) }

// setCell is SetCell; values false leaves the recalculation state alone
// (a style or a cached value changed, not a value).
func (s *Sheet) setCell(row, col int, xml string, values bool) error {
	if !inGrid(row, col) {
		return fmt.Errorf("edit: cell %d,%d is outside the grid", row, col)
	}
	old, err := s.Cell(row, col)
	if err != nil {
		return err
	}
	k := cellKey{int32(row), int32(col)} //nolint:gosec // G115: inGrid bounds both
	if xml == "" {
		if !old.Exists {
			return nil
		}
		s.cells[k] = pendingCell{del: true}
		if values {
			s.mark(old.Raw.F.Present)
		}
		return nil
	}
	raw, err := s.parseCell(row, col, xml)
	if err != nil {
		return err
	}
	if old.Exists && old.XML == xml {
		return nil
	}
	s.cells[k] = pendingCell{xml: xml, raw: raw}
	if !raw.Empty() {
		s.grow.Add(row, col)
	}
	if values {
		s.mark(old.Raw.F.Present || raw.F.Present)
	}
	return nil
}

// mark records a value or formula change in the workspace recalculation state.
func (s *Sheet) mark(formulas bool) {
	if formulas {
		s.w.MarkFormulas()
	} else {
		s.w.MarkValues()
	}
}

// RowAttrs returns the attributes of row num's start tag as the overlay
// leaves them (decoded values, in order) and whether the row exists in
// the base or has edits.
func (s *Sheet) RowAttrs(num int) ([]AttrEdit, bool, error) {
	rc, err := s.locate(num)
	if err != nil {
		return nil, false, err
	}
	var out []AttrEdit
	if rc.found {
		n, err := xmlscan.Next(s.base, rc.span.start)
		if err != nil {
			return nil, false, s.unreadable(errLex)
		}
		for a := range xmlscan.Attrs(s.base, n) {
			out = append(out, AttrEdit{Name: a.Name(s.base), Value: a.Value(s.base)})
		}
	}
	edits := s.rows[num]
	return applyAttrEdits(out, edits), rc.found || len(edits) > 0, nil
}

// applyAttrEdits returns attrs with edits applied, in order.
func applyAttrEdits(as []AttrEdit, edits []AttrEdit) []AttrEdit {
	out := slices.Clone(as)
	for _, e := range edits {
		i := slices.IndexFunc(out, func(a AttrEdit) bool { return a.Name == e.Name })
		switch {
		case e.Remove && i >= 0:
			out = slices.Delete(out, i, i+1)
		case e.Remove:
		case i >= 0:
			out[i].Value = e.Value
		default:
			out = append(out, AttrEdit{Name: e.Name, Value: e.Value})
		}
	}
	return out
}

// SetRowAttrs edits attributes of row num's start tag (row.format: ht,
// customHeight, hidden; a row style: s, customFormat). r cannot be
// edited. A row the sheet does not hold is created (with r and the
// edits) at Finish. Values are checked when the row is written; names
// and values must be XML.
func (s *Sheet) SetRowAttrs(num int, edits ...AttrEdit) error {
	if num < 1 || num > formula.MaxRows {
		return fmt.Errorf("edit: row %d is outside the grid", num)
	}
	for _, e := range edits {
		if e.Name == "r" {
			return errors.New("edit: a row's r is not edited")
		}
	}
	s.rows[num] = append(s.rows[num], edits...)
	return nil
}

// Element returns the top-level element local of the sheet (dimension,
// cols, mergeCells, conditionalFormatting, ...) as the overlay leaves
// it: every occurrence's bytes, concatenated in document order, and
// whether there is one. sheetData is refused.
func (s *Sheet) Element(local string) (string, bool, error) {
	if local == "sheetData" {
		return "", false, errors.New("edit: sheetData is edited by cells and rows")
	}
	if x, ok := s.elems[local]; ok {
		return x, x != "", nil
	}
	var b strings.Builder
	occ := s.lay.element(local)
	for _, c := range occ {
		b.WriteString(s.base[c.start:c.end])
	}
	return b.String(), len(occ) > 0, nil
}

// SetElement replaces every occurrence of the top-level element local
// with xml (one or more whole elements in the sheet's prefix, written
// where the first occurrence was), inserts it where the worksheet's
// schema order puts it when there is none, or removes every occurrence
// when xml is "". xml is checked for well-formedness now. sheetData and
// elements the worksheet schema does not list are refused.
func (s *Sheet) SetElement(local, xml string) error {
	if local == "sheetData" {
		return errors.New("edit: sheetData is edited by cells and rows")
	}
	if _, ok := worksheetRank(local); !ok {
		return fmt.Errorf("edit: %s is not a worksheet child", local)
	}
	if xml != "" {
		p := s.lay.prefix
		doc := "<" + p + "worksheet" + s.lay.decls + ">" + xml + "</" + p + "worksheet>"
		if err := tree.Check([]byte(doc)); err != nil {
			return fmt.Errorf("edit: %s: %w", local, err)
		}
	}
	s.elems[local] = xml
	return nil
}

// Text returns the sheet's whole current text: the overlay flushed into
// the base. Structural ops read it, rewrite it in one pass and give the
// result to SetText. It costs a copy of the text when the overlay is not
// empty.
func (s *Sheet) Text() (string, error) {
	p, err := s.buildPatch()
	if err != nil {
		return "", err
	}
	if p.Empty() {
		return s.base, nil
	}
	if err := p.Check(); err != nil {
		return "", fmt.Errorf("edit: %s: %w", s.ref.Part, err)
	}
	return p.String(), nil
}

// SetText makes text the sheet's new base (a structural op's rewrite of
// Text). The overlay is dropped (Text included it) and the base is
// scanned and indexed again; a text that is not a well-formed worksheet
// with ordered rows and cells is an error and changes nothing. It marks
// values and formulas (the structure changed).
func (s *Sheet) SetText(text string) error {
	keep := *s
	if err := s.index(text); err != nil {
		*s = keep
		return fmt.Errorf("edit: rewritten %s: %w", s.ref.Part, err)
	}
	if s.gen == 0 && !s.added {
		s.origHead = strings.Clone(s.orig[:s.origStart])
		s.origTail = strings.Clone(s.orig[s.origEnd:])
		s.orig = ""
		s.part.Forget()
	}
	s.gen++
	s.w.MarkFormulas()
	return nil
}

// DefaultStyle is the style a new cell at (row, col) takes (survey R4):
// the row's s when the row has customFormat, else the s of the cols/col
// that covers col, else 0.
func (s *Sheet) DefaultStyle(row, col int) (int, error) {
	as, _, err := s.RowAttrs(row)
	if err != nil {
		return 0, err
	}
	custom, style := false, 0
	for _, a := range as {
		switch a.Name {
		case "customFormat":
			custom = a.Value == "1" || a.Value == "true"
		case "s":
			style, _ = strconv.Atoi(a.Value)
		}
	}
	if custom {
		return style, nil
	}
	cols, ok, err := s.Element("cols")
	if err != nil || !ok {
		return 0, err
	}
	for i := 0; i < len(cols); {
		n, err := xmlscan.Next(cols, i)
		if err != nil {
			return 0, s.unreadable(errLex)
		}
		i = n.End
		if !n.IsElement() || n.Local(cols) != "col" {
			continue
		}
		lo, hi, st := "", "", ""
		if a, ok := xmlscan.FindAttr(cols, n, "min"); ok {
			lo = a.Value(cols)
		}
		if a, ok := xmlscan.FindAttr(cols, n, "max"); ok {
			hi = a.Value(cols)
		}
		if a, ok := xmlscan.FindAttr(cols, n, "style"); ok {
			st = a.Value(cols)
		}
		l, e1 := strconv.Atoi(lo)
		h, e2 := strconv.Atoi(hi)
		if e1 == nil && e2 == nil && col >= l && col <= h {
			v, _ := strconv.Atoi(st)
			return v, nil
		}
	}
	return 0, nil
}

// flush writes the overlay into the part at Finish: a patch over the
// original when no SetText ran, else one replacement of the original
// root's content with the current text's. The replaced spans are
// declared spliced.
func (s *Sheet) flush() error {
	if !s.dirty() {
		return nil
	}
	p, err := s.buildPatch()
	if err != nil {
		return err
	}
	if s.added {
		if err := p.Check(); err != nil {
			return fmt.Errorf("edit: %s: %w", s.ref.Part, err)
		}
		s.w.core.DeclarePart(s.ref.Part)
		s.part.SetBytes([]byte(p.String()))
		return nil
	}
	if s.gen > 0 {
		text := p.String()
		if p.Empty() {
			text = s.base
		}
		if p, err = s.overOriginal(text); err != nil {
			return err
		}
	}
	if p.Empty() {
		return nil
	}
	s.w.declareSpliced(s.ref.Part, p.Spans())
	return s.part.SetPatch(p)
}

// overOriginal expresses text, a rewrite of the original, as a patch
// over the original (read again from the part) replacing the root's
// content. Everything outside the root's content (the prolog, the
// root's start and end tags) must be unchanged.
func (s *Sheet) overOriginal(text string) (*splice.Patch, error) {
	i := 0
	var root xmlscan.Token
	for {
		n, err := xmlscan.Next(text, i)
		if err != nil {
			return nil, errLex
		}
		if n.Kind == xmlscan.StartTag {
			root = n
			break
		}
		i = n.End
	}
	contentEnd, _, err := xmlscan.ElementEnd(text, root)
	if err != nil {
		return nil, errLex
	}
	start, stop := root.End, contentEnd
	orig, _, err := s.part.OriginalString()
	if err != nil {
		return nil, err
	}
	if text[:start] != s.origHead || text[stop:] != s.origTail || orig[:s.origStart] != s.origHead || orig[s.origEnd:] != s.origTail {
		return nil, fmt.Errorf("edit: %s: a rewrite changed the worksheet outside its root's content", s.ref.Part)
	}
	// The original was scanned in full when the sheet was opened.
	p := splice.NewWellFormed(orig)
	if err := p.Replace(s.origStart, s.origEnd, text[start:stop]); err != nil {
		return nil, err
	}
	return p, nil
}

// buildPatch turns the overlay into a patch over the base.
func (s *Sheet) buildPatch() (*splice.Patch, error) {
	// index scanned the base in full (sheet.Scan), so it is proved well
	// formed and Check stops after the last span.
	p := splice.NewWellFormed(s.base)
	elems := s.elems
	if dim, ok, err := s.grownDimension(); err != nil {
		return nil, err
	} else if ok {
		elems = make(map[string]string, len(s.elems)+1)
		for k, v := range s.elems {
			elems[k] = v
		}
		elems["dimension"] = dim
	}
	if err := s.patchElements(p, elems); err != nil {
		return nil, err
	}
	if err := s.patchRows(p); err != nil {
		return nil, err
	}
	return p, nil
}

// grownDimension is the dimension element widened to the cells the
// overlay filled, when the sheet stores a dimension, no op set it and it
// grows; computed afresh at every flush, never stored. It never shrinks
// (that needs the whole sheet's used range).
func (s *Sheet) grownDimension() (string, bool, error) {
	if !s.grow.OK {
		return "", false, nil
	}
	if _, set := s.elems["dimension"]; set {
		return "", false, nil
	}
	occ := s.lay.element("dimension")
	if len(occ) == 0 {
		return "", false, nil
	}
	n, err := xmlscan.Next(s.base, occ[0].start)
	if err != nil {
		return "", false, s.unreadable(errLex)
	}
	ref := ""
	if a, ok := xmlscan.FindAttr(s.base, n, "ref"); ok {
		ref = a.Value(s.base)
	}
	b := s.grow
	if r, ok := formula.ParseRange(ref); ok && r.Kind == formula.Cells {
		r1, c1, r2, c2 := r.Bounds()
		b.Add(r1, c1)
		b.Add(r2, c2)
	}
	area := formula.Range{Kind: formula.Cells, From: formula.Cell{Row: b.R1, Col: b.C1}, To: formula.Cell{Row: b.R2, Col: b.C2}, Single: b.R1 == b.R2 && b.C1 == b.C2}
	if area.String() == ref {
		return "", false, nil
	}
	var wr splice.Writer
	wr.Start(s.lay.prefix + "dimension")
	wr.Attr("ref", area.String())
	wr.End()
	x, err := wr.String()
	return x, err == nil, err
}

// patchElements writes the element edits, in schema order so that
// insertions at one point land in order.
func (s *Sheet) patchElements(p *splice.Patch, elems map[string]string) error {
	locals := make([]string, 0, len(elems))
	for l := range elems {
		locals = append(locals, l)
	}
	slices.SortFunc(locals, func(a, b string) int {
		ra, _ := worksheetRank(a)
		rb, _ := worksheetRank(b)
		return ra - rb
	})
	for _, l := range locals {
		x := elems[l]
		occ := s.lay.element(l)
		if len(occ) == 0 {
			if x == "" {
				continue
			}
			at, err := s.lay.insertPoint(l)
			if err != nil {
				return err
			}
			if err := p.Replace(at, at, x); err != nil {
				return err
			}
			continue
		}
		for i, c := range occ {
			repl := ""
			if i == 0 {
				repl = x
			}
			if err := p.Replace(c.start, c.end, repl); err != nil {
				return err
			}
		}
	}
	return nil
}

// rowEdit is one row's edits, cells by column.
type rowEdit struct {
	num   int
	cells []colEdit
	attrs []AttrEdit
}

type colEdit struct {
	col int
	p   pendingCell
}

// patchRows writes the cell and row edits, row by row in ascending
// order.
func (s *Sheet) patchRows(p *splice.Patch) error {
	byRow := map[int]*rowEdit{}
	get := func(n int) *rowEdit {
		r := byRow[n]
		if r == nil {
			r = &rowEdit{num: n}
			byRow[n] = r
		}
		return r
	}
	for k, c := range s.cells {
		r := get(int(k.row))
		r.cells = append(r.cells, colEdit{int(k.col), c})
	}
	for n, a := range s.rows {
		get(n).attrs = a
	}
	nums := make([]int, 0, len(byRow))
	for n, r := range byRow {
		nums = append(nums, n)
		// s.cells is a map: cells are written in column order (both
		// branches below; an empty sheetData's new rows were written in
		// map order, out of order and not deterministic).
		slices.SortFunc(r.cells, func(a, b colEdit) int { return a.col - b.col })
	}
	slices.Sort(nums)
	if len(nums) == 0 {
		return nil
	}
	if s.lay.sdEmpty {
		var b strings.Builder
		b.WriteString("<" + s.lay.prefix + "sheetData>")
		for _, n := range nums {
			r := byRow[n]
			x, err := s.newRow(r)
			if err != nil {
				return err
			}
			b.WriteString(x)
		}
		b.WriteString("</" + s.lay.prefix + "sheetData>")
		c := s.lay.children[s.lay.sd]
		return p.Replace(c.start, c.end, b.String())
	}
	// After the last indexed row: the end of that row, found once.
	tail := s.lay.sdInnerEnd
	if k := s.ix.Rows(); k > 0 {
		last, err := locateRow(s.base, s.ix, k-1)
		if err != nil {
			return s.unreadable(err)
		}
		tail = last.end
	}
	for _, n := range nums {
		r := byRow[n]
		i, found := findRow(s.ix, n)
		if !found {
			x, err := s.newRow(r)
			if err != nil {
				return err
			}
			if x == "" {
				continue
			}
			at := tail
			if i < s.ix.Rows() {
				_, _, at = s.ix.Row(i)
			}
			if err := p.Replace(at, at, x); err != nil {
				return err
			}
			continue
		}
		if err := s.patchRow(p, r); err != nil {
			return err
		}
	}
	return nil
}

// newRow renders a row the base does not hold: its r, its attribute
// edits and its new cells; "" when it would hold nothing (only
// deletions).
func (s *Sheet) newRow(r *rowEdit) (string, error) {
	var cells strings.Builder
	for _, c := range r.cells {
		if !c.p.del {
			cells.WriteString(c.p.xml)
		}
	}
	attrs := applyAttrEdits(nil, r.attrs)
	if cells.Len() == 0 && len(attrs) == 0 {
		return "", nil
	}
	attrs = append([]AttrEdit{{Name: "r", Value: strconv.Itoa(r.num)}}, attrs...)
	return s.rowElement(attrs, cells.String())
}

// rowElement renders <row attrs>content</row> (empty when content is "").
func (s *Sheet) rowElement(attrs []AttrEdit, content string) (string, error) {
	var w splice.Writer
	w.Start(s.lay.prefix + "row")
	for _, a := range attrs {
		w.Attr(a.Name, a.Value)
	}
	w.End()
	x, err := w.String()
	if err != nil {
		return "", err
	}
	if content == "" {
		return x, nil
	}
	return strings.TrimSuffix(x, "/>") + ">" + content + "</" + s.lay.prefix + "row>", nil
}

// patchRow writes the edits of the i-th indexed row: per-cell splices
// when its start tag stays, else the whole row rebuilt around its
// original cells (and the text and other children between them, kept as
// they are).
func (s *Sheet) patchRow(p *splice.Patch, r *rowEdit) error {
	rc, err := s.locate(r.num)
	if err != nil {
		return err
	}
	rs, cells, other := rc.span, rc.cells, rc.other
	adds := false
	for _, c := range r.cells {
		if !c.p.del {
			adds = true
		}
	}
	if len(r.attrs) == 0 && !(rs.empty && adds) {
		return s.spliceCells(p, cells, other, r.cells)
	}
	attrs, _, err := s.RowAttrs(r.num)
	if err != nil {
		return err
	}
	var b strings.Builder
	at := rs.innerStart
	k := 0
	for _, oc := range cells {
		for k < len(r.cells) && r.cells[k].col < oc.col {
			if !r.cells[k].p.del {
				b.WriteString(r.cells[k].p.xml)
			}
			k++
		}
		b.WriteString(s.base[at:oc.start])
		if k < len(r.cells) && r.cells[k].col == oc.col {
			if !r.cells[k].p.del {
				b.WriteString(r.cells[k].p.xml)
			}
			k++
		} else {
			b.WriteString(s.base[oc.start:oc.end])
		}
		at = oc.end
	}
	b.WriteString(s.base[at:other])
	for ; k < len(r.cells); k++ {
		if !r.cells[k].p.del {
			b.WriteString(r.cells[k].p.xml)
		}
	}
	b.WriteString(s.base[other:rs.inner])
	x, err := s.rowElement(attrs, b.String()) // an empty row stays an empty-element tag
	if err != nil {
		return err
	}
	return p.Replace(rs.start, rs.end, x)
}

// spliceCells replaces, deletes or inserts the edited cells of one row
// in place: a new cell goes just before the first cell after it, or
// before the row's first non-cell child (or its end tag).
func (s *Sheet) spliceCells(p *splice.Patch, cells []cellSpan, other int, edits []colEdit) error {
	j := 0
	for _, e := range edits {
		for j < len(cells) && cells[j].col < e.col {
			j++
		}
		if j < len(cells) && cells[j].col == e.col {
			repl := ""
			if !e.p.del {
				repl = e.p.xml
			}
			if err := p.Replace(cells[j].start, cells[j].end, repl); err != nil {
				return err
			}
			continue
		}
		if e.p.del {
			continue
		}
		at := other
		if j < len(cells) {
			at = cells[j].start
		}
		if err := p.Replace(at, at, e.p.xml); err != nil {
			return err
		}
	}
	return nil
}

// Piece is one run of the worksheet root's content: a top-level element
// (Local its local name, XML its bytes) or the bytes between two
// (Local "": whitespace, comments).
type Piece struct {
	Local, XML string
}

// Pieces returns the root's content split into its top-level elements
// and the bytes between them, in order: concatenated, they are the
// content exactly. Pending edits are flushed into the base first (Text,
// SetText). A structural op rewrites the pieces and gives them back with
// SetPieces.
func (s *Sheet) Pieces() ([]Piece, error) {
	if len(s.cells) > 0 || len(s.rows) > 0 || len(s.elems) > 0 {
		text, err := s.Text()
		if err != nil {
			return nil, err
		}
		if err := s.SetText(text); err != nil {
			return nil, err
		}
	}
	var out []Piece
	at := s.lay.contentStart
	for _, c := range s.lay.children {
		if c.start > at {
			out = append(out, Piece{XML: s.base[at:c.start]})
		}
		out = append(out, Piece{Local: c.local, XML: s.base[c.start:c.end]})
		at = c.end
	}
	if s.lay.contentEnd > at {
		out = append(out, Piece{XML: s.base[at:s.lay.contentEnd]})
	}
	return out, nil
}

// SetPieces makes the root's content the concatenation of pieces (a
// rewrite of Pieces) and gives the sheet that text (SetText): the
// prolog and the root's tags stay the base's.
func (s *Sheet) SetPieces(pieces []Piece) error {
	var b strings.Builder
	b.WriteString(s.base[:s.lay.contentStart])
	for _, p := range pieces {
		b.WriteString(p.XML)
	}
	b.WriteString(s.base[s.lay.contentEnd:])
	return s.SetText(b.String())
}
