package shift

import (
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/jhnoel/documentdriver/internal/ooxml/splice"
	"github.com/jhnoel/documentdriver/internal/ooxml/xmlscan"
	"github.com/jhnoel/documentdriver/internal/xlsx/formula"
)

// Stats is what one rewrite did.
type Stats struct {
	// Adjusted counts the formulas whose text changed.
	Adjusted int
	// RefErrors are the cells (A1 form, no sheet) or names whose
	// formula gained a #REF! it did not have.
	RefErrors []string
}

// SheetData rewrites xml, the edited sheet's whole <sheetData> element
// as Sheet.Pieces gives it (prefix as in the sheet), for e on that same
// sheet (sh.Own is e.Sheet), in one streaming pass (chunk K7a): no tree.
//
//   - Rows at or after the point move by Count (r rewritten), rows a
//     delete removes go; every cell's r follows its row (row edits) or
//     its column (column edits); cells a column delete removes go. Every
//     other byte of a row and a cell (attributes, values, inline
//     strings, unknown children) is kept as it is. A row or cell with
//     no r has the position its order gives it; one that moves gains
//     an explicit r, one that does not move stays implicit. An r
//     added here (single-quoted, see insertR) is dropped again when
//     a later edit puts the row or cell back at its order position,
//     so insert then inverse delete restores the input.
//   - Every <f> text is rewritten with formula.Shift(text, sh, e) (a
//     reference to the sheet from its own cells is unqualified).
//   - Shared groups (t="shared"): collected in a first pass (masters
//     and members by si, in document order: an si reused later starts a
//     new group) and handed to PlanShared (shared.go, chunk K7b), whose
//     per-cell answer this pass writes; nextSI is the sheet's largest si
//     plus one.
//   - Array formulas and data tables (t="array", t="dataTable"): moved
//     whole (ref shifted) when the edit is entirely before or after
//     them; refused (*Refused) when it cuts them; a delete that removes
//     one whole removes it.
//   - A cell left past the grid by an insert is refused (*Refused).
//   - row@spans is left as it is (E12: a stale spans loses nothing).
//
// Stats.Adjusted counts the <f> texts that changed (shared children
// without text are not counted); RefErrors the cells whose formula
// gained #REF!. The output is well formed (each piece is built with
// splice.Writer or copied); the caller re-indexes it (SetPieces).
func SheetData(xml string, sh formula.Sheets, e formula.Edit) (string, Stats, error) {
	if _, err := formula.Shift("", sh, e); err != nil {
		return "", Stats{}, err
	}
	groups, nextSI, err := sdSharedGroups(xml)
	if err != nil {
		return "", Stats{}, err
	}
	stats := Stats{}
	var plan map[formula.Cell]SharedCell
	if len(groups) > 0 {
		var ps Stats
		plan, ps, err = PlanShared(groups, nextSI, sh, e)
		if err != nil {
			return "", Stats{}, err
		}
		stats.Adjusted = ps.Adjusted
		stats.RefErrors = append(stats.RefErrors, ps.RefErrors...)
	}
	w := &sdW{xml: xml, sh: sh, e: e, plan: plan, stats: &stats,
		isRow: e.Kind == formula.InsertRows || e.Kind == formula.DeleteRows}
	w.b.Grow(len(xml) + len(xml)/16 + 64)
	if err := w.run(); err != nil {
		return "", Stats{}, err
	}
	if len(stats.RefErrors) > 1 {
		sort.Slice(stats.RefErrors, func(i, j int) bool {
			ci, _ := formula.ParseCell(stats.RefErrors[i])
			cj, _ := formula.ParseCell(stats.RefErrors[j])
			if ci.Row != cj.Row {
				return ci.Row < cj.Row
			}
			return ci.Col < cj.Col
		})
	}
	return w.b.String(), stats, nil
}

// sdSharedGroups collects the shared-formula groups of xml (pre-edit
// positions, document order) in one light scan: it keeps only the
// groups, never the sheet.
func sdSharedGroups(xml string) ([]SharedGroup, int, error) {
	var groups []SharedGroup
	var siIdx map[int]int
	maxSI := -1
	var stack []sdFr
	lastRow, row, lastCol, cellR, cellC := 0, 0, 0, 0, 0
	rootSeen, rootClosed := false, false
	pos := 0
	for pos < len(xml) {
		t, err := xmlscan.Next(xml, pos)
		if err != nil {
			return nil, 0, err
		}
		pos = t.End
		if !t.IsElement() && t.Kind != xmlscan.EndTag {
			continue
		}
		name := t.Local(xml)
		if t.Kind == xmlscan.EndTag {
			if len(stack) == 0 || stack[len(stack)-1].name != name {
				return nil, 0, fmt.Errorf("sheetdata: mismatched closing %s", name)
			}
			fr := stack[len(stack)-1]
			stack = stack[:len(stack)-1]
			if fr.kind == sdKRoot {
				rootClosed = true
			}
			continue
		}
		empty := t.Kind == xmlscan.EmptyTag
		if len(stack) == 0 {
			if name != "sheetData" {
				return nil, 0, fmt.Errorf("sheetdata: unexpected root %s", name)
			}
			if rootSeen {
				return nil, 0, fmt.Errorf("sheetdata: multiple roots")
			}
			rootSeen = true
			if empty {
				rootClosed = true
			} else {
				stack = append(stack, sdFr{name, sdKRoot, false})
			}
			continue
		}
		parent := stack[len(stack)-1]
		switch {
		case parent.kind == sdKRoot && name == "row":
			n, _, _, err := sdRowNum(xml, t, &lastRow)
			if err != nil {
				return nil, 0, err
			}
			row, lastCol = n, 0
			if !empty {
				stack = append(stack, sdFr{name, sdKRow, false})
			}
		case parent.kind == sdKRow && name == "c":
			r, c, _, _, _, _, err := sdCellPos(xml, t, row, &lastCol)
			if err != nil {
				return nil, 0, err
			}
			cellR, cellC = r, c
			if !empty {
				stack = append(stack, sdFr{name, sdKCell, false})
			}
		case parent.kind == sdKCell && name == "f":
			f, err := sdParseF(xml, t)
			if err != nil {
				return nil, 0, err
			}
			if f.typ == "shared" {
				if !f.hasSI {
					return nil, 0, fmt.Errorf("sheetdata: shared formula without si")
				}
				if f.si > maxSI {
					maxSI = f.si
				}
				p := formula.Cell{Row: cellR, Col: cellC}
				if f.text != "" {
					if !f.hasRef {
						return nil, 0, fmt.Errorf("sheetdata: shared master without ref")
					}
					r, ok := formula.ParseRange(strings.TrimSpace(f.ref))
					if !ok {
						return nil, 0, fmt.Errorf("sheetdata: invalid shared ref %q", f.ref)
					}
					groups = append(groups, SharedGroup{SI: f.si, Master: p, Text: f.text, Ref: r, Members: []formula.Cell{p}})
					if siIdx == nil {
						siIdx = map[int]int{}
					}
					siIdx[f.si] = len(groups) - 1
				} else if idx, ok := siIdx[f.si]; ok {
					groups[idx].Members = append(groups[idx].Members, p)
				}
			}
			pos = f.end
		default:
			if !empty {
				stack = append(stack, sdFr{name, sdKOther, false})
			}
		}
	}
	if len(stack) != 0 {
		return nil, 0, fmt.Errorf("sheetdata: unclosed element %s", stack[len(stack)-1].name)
	}
	if !rootSeen {
		return nil, 0, fmt.Errorf("sheetdata: missing sheetData")
	}
	if !rootClosed {
		return nil, 0, fmt.Errorf("sheetdata: unclosed sheetData")
	}
	nextSI := 0
	if maxSI >= 0 {
		nextSI = maxSI + 1
	}
	return groups, nextSI, nil
}

const (
	sdKRoot = iota
	sdKRow
	sdKCell
	sdKOther
)

type sdFr struct {
	name string
	kind int
	skip bool
}

// sdW is the streaming writer: it copies xml into b unchanged except
// for the spans it rewrites, holding only the current row and cell.
type sdW struct {
	xml   string
	b     strings.Builder
	at    int
	stack []sdFr
	sh    formula.Sheets
	e     formula.Edit
	plan  map[formula.Cell]SharedCell
	stats *Stats
	isRow bool

	lastRow, row, lastCol int
	lastOutRow, outRow    int
	lastOutCol            int
	cellR, cellC          int
}

// emitTo copies the input up to pos into the output.
func (w *sdW) emitTo(pos int) {
	if pos > w.at {
		w.b.WriteString(w.xml[w.at:pos])
		w.at = pos
	}
}

// writeInt writes n in decimal into the output.
func (w *sdW) writeInt(n int) {
	var buf [20]byte
	w.b.Write(strconv.AppendInt(buf[:0], int64(n), 10))
}

// writeCell writes the A1 form of cell (r, c) into b, keeping the
// position's $-flags.
func (w *sdW) writeCell(r, c int, rowAbs, colAbs bool) {
	if colAbs {
		w.b.WriteByte('$')
	}
	var buf [8]byte
	w.b.Write(formula.AppendColumnName(buf[:0], c))
	if rowAbs {
		w.b.WriteByte('$')
	}
	w.writeInt(r)
}

// insertR writes an r attribute holding the new position of a row or
// cell that had no r (its position was implicit) but moved. The
// attribute is inserted just after the tag name; the rest of the tag
// is copied unchanged by the caller's emitTo. It is written with
// single quotes: that is how a later edit in this package tells an
// r it added itself (which an inverse edit may drop again when the
// row or cell is back at the position its order gives it) from an
// r the sheet already had, which is never dropped.
func (w *sdW) insertR(t xmlscan.Token, write func()) {
	w.emitTo(t.NameEnd)
	w.b.WriteString(" r='")
	write()
	w.b.WriteByte('\'')
	w.at = t.NameEnd
}

// dropAttr removes the r attribute of t (with the space before it):
// a row or cell whose r was added by insertR (single-quoted) and
// whose new position is the position its order in the output gives
// it is written implicit again, so an insert followed by its
// inverse delete restores a moved implicit row or cell. Rows and
// cells that do not move, and originally explicit ones, keep
// their r as it was. a is the r attribute as xmlscan writes it, so
// a.Start is the whitespace before its name.
func (w *sdW) dropAttr(a xmlscan.Attr) {
	w.emitTo(a.Start)
	w.at = a.End()
}

// run rewrites the whole sheetData and returns the first error met.
func (w *sdW) run() error {
	rootSeen, rootClosed := false, false
	pos := 0
	for pos < len(w.xml) {
		t, err := xmlscan.Next(w.xml, pos)
		if err != nil {
			return err
		}
		pos = t.End
		if !t.IsElement() && t.Kind != xmlscan.EndTag {
			continue
		}
		name := t.Local(w.xml)
		if t.Kind == xmlscan.EndTag {
			if len(w.stack) == 0 {
				return fmt.Errorf("sheetdata: unexpected closing %s", name)
			}
			fr := w.stack[len(w.stack)-1]
			if fr.name != name {
				return fmt.Errorf("sheetdata: mismatched closing %s for %s", name, fr.name)
			}
			w.stack = w.stack[:len(w.stack)-1]
			switch {
			case fr.skip:
				if len(w.stack) == 0 || !w.stack[len(w.stack)-1].skip {
					w.at = t.End
				}
			default:
				w.emitTo(t.End)
			}
			if fr.kind == sdKRoot {
				rootClosed = true
			}
			continue
		}
		empty := t.Kind == xmlscan.EmptyTag
		if len(w.stack) > 0 && w.stack[len(w.stack)-1].skip {
			if !empty {
				w.stack = append(w.stack, sdFr{name, sdKOther, true})
			}
			continue
		}
		if len(w.stack) == 0 {
			if name != "sheetData" {
				return fmt.Errorf("sheetdata: unexpected root %s", name)
			}
			if rootSeen {
				return fmt.Errorf("sheetdata: multiple roots")
			}
			rootSeen = true
			w.emitTo(t.End)
			if empty {
				rootClosed = true
			} else {
				w.stack = append(w.stack, sdFr{name, sdKRoot, false})
			}
			continue
		}
		parent := w.stack[len(w.stack)-1]
		switch {
		case parent.kind == sdKRoot && name == "row":
			pos, err = w.rowTag(t)
		case parent.kind == sdKRow && name == "c":
			pos, err = w.cellTag(t)
		case parent.kind == sdKCell && name == "f":
			pos, err = w.formulaTag(t)
		default:
			w.emitTo(t.End)
			if !empty {
				w.stack = append(w.stack, sdFr{name, sdKOther, false})
			}
			pos = t.End
		}
		if err != nil {
			return err
		}
	}
	if len(w.stack) != 0 {
		return fmt.Errorf("sheetdata: unclosed element %s", w.stack[len(w.stack)-1].name)
	}
	if !rootSeen {
		return fmt.Errorf("sheetdata: missing sheetData")
	}
	if !rootClosed {
		return fmt.Errorf("sheetdata: unclosed sheetData")
	}
	w.emitTo(len(w.xml))
	return nil
}

// rowTag handles one row start tag, rewriting or dropping it, and returns its end.
func (w *sdW) rowTag(t xmlscan.Token) (int, error) {
	n, hasR, attr, err := sdRowNum(w.xml, t, &w.lastRow)
	if err != nil {
		return 0, err
	}
	w.row, w.lastCol = n, 0
	newRow, del := n, false
	if w.isRow {
		nv, d, err := sdMapCoord(n, w.e, formula.MaxRows)
		if err != nil {
			return 0, err
		}
		newRow, del = nv, d
	}
	if del {
		return w.drop(t, sdKRow), nil
	}
	outImpRow := w.lastOutRow + 1
	w.lastOutRow, w.outRow, w.lastOutCol = newRow, newRow, 0
	if hasR && newRow != n && newRow == outImpRow && attr.Quote(w.xml) == '\'' {
		w.dropAttr(attr)
	} else if hasR && newRow != n {
		w.emitTo(attr.ValueStart)
		w.writeInt(newRow)
		w.at = attr.ValueEnd
	} else if !hasR && newRow != n {
		w.insertR(t, func() { w.writeInt(newRow) })
	}
	w.emitTo(t.End)
	if t.Kind != xmlscan.EmptyTag {
		w.stack = append(w.stack, sdFr{t.Local(w.xml), sdKRow, false})
	}
	return t.End, nil
}

// cellTag handles one cell tag, refusing a cut array formula, and returns its end.
func (w *sdW) cellTag(t xmlscan.Token) (int, error) {
	r, c, hasR, attr, rowAbs, colAbs, err := sdCellPos(w.xml, t, w.row, &w.lastCol)
	if err != nil {
		return 0, err
	}
	newR, newC, del := r, c, false
	if w.isRow {
		nv, d, err := sdMapCoord(r, w.e, formula.MaxRows)
		if err != nil {
			return 0, err
		}
		newR, del = nv, d
	} else {
		nv, d, err := sdMapCoord(c, w.e, formula.MaxCols)
		if err != nil {
			return 0, err
		}
		newC, del = nv, d
	}
	if !del && t.Kind != xmlscan.EmptyTag {
		whole, cut, ref, ftype, err := sdArrayCell(w.xml, t, w.e, w.isRow)
		if err != nil {
			return 0, err
		}
		if cut {
			kind := "a multi-cell array formula"
			if ftype == "dataTable" {
				kind = "a data table"
			}
			return 0, &Refused{Construct: kind + " at " + w.e.Sheet + "!" + ref}
		}
		del = whole
	}
	w.cellR, w.cellC = newR, newC
	if del {
		return w.drop(t, sdKCell), nil
	}
	outImpCol := w.lastOutCol + 1
	w.lastOutCol = newC
	if hasR && (newR != r || newC != c) && newR == w.outRow && newC == outImpCol && !rowAbs && !colAbs && attr.Quote(w.xml) == '\'' {
		w.dropAttr(attr)
	} else if hasR && (newR != r || newC != c) {
		w.emitTo(attr.ValueStart)
		w.writeCell(newR, newC, rowAbs, colAbs)
		w.at = attr.ValueEnd
	} else if !hasR && (newR != r || newC != c) {
		w.insertR(t, func() { w.writeCell(newR, newC, rowAbs, colAbs) })
	}
	w.emitTo(t.End)
	if t.Kind != xmlscan.EmptyTag {
		w.stack = append(w.stack, sdFr{t.Local(w.xml), sdKCell, false})
	}
	return t.End, nil
}

// drop removes the element starting at t (already decided): the text
// before it is copied, the element itself is not.
func (w *sdW) drop(t xmlscan.Token, kind int) int {
	w.emitTo(t.Start)
	if t.Kind == xmlscan.EmptyTag {
		w.at = t.End
	} else {
		w.stack = append(w.stack, sdFr{t.Local(w.xml), kind, true})
	}
	return t.End
}

// formulaTag rewrites one formula element for the edit and returns its end.
func (w *sdW) formulaTag(t xmlscan.Token) (int, error) {
	f, err := sdParseF(w.xml, t)
	if err != nil {
		return 0, err
	}
	post := formula.Cell{Row: w.cellR, Col: w.cellC}
	switch f.typ {
	case "shared":
		entry, ok := w.plan[post]
		if !ok {
			w.emitTo(f.end)
			return f.end, nil
		}
		return f.end, w.sharedF(t, f, entry)
	case "array", "dataTable":
		return f.end, w.arrayF(f, post)
	default:
		if f.empty {
			w.emitTo(f.end)
			return f.end, nil
		}
		shifted, err := formula.Shift(f.text, w.sh, w.e)
		if err != nil {
			return 0, err
		}
		if shifted != f.text {
			w.emitTo(f.textVS)
			w.b.WriteString(sdEscape(shifted))
			w.at = f.textVE
			w.stats.Adjusted++
		}
		if strings.Count(shifted, "#REF!") > strings.Count(f.text, "#REF!") {
			w.stats.RefErrors = append(w.stats.RefErrors, post.String())
		}
		w.emitTo(f.end)
		return f.end, nil
	}
}

// arrayF shifts an array or data-table formula and emits its edits.
func (w *sdW) arrayF(f sdF, post formula.Cell) error {
	var edits []sdEdit
	if f.hasRef {
		ref := strings.TrimSpace(f.ref)
		shifted, err := formula.Shift(ref, w.sh, w.e)
		if err != nil {
			return err
		}
		if shifted != ref {
			edits = append(edits, sdEdit{f.refVS, f.refVE, shifted})
		}
	}
	if f.typ == "array" && !f.empty {
		shifted, err := formula.Shift(f.text, w.sh, w.e)
		if err != nil {
			return err
		}
		if shifted != f.text {
			edits = append(edits, sdEdit{f.textVS, f.textVE, sdEscape(shifted)})
			w.stats.Adjusted++
		}
		if strings.Count(shifted, "#REF!") > strings.Count(f.text, "#REF!") {
			w.stats.RefErrors = append(w.stats.RefErrors, post.String())
		}
	}
	if f.typ == "dataTable" {
		for _, a := range []struct {
			has    bool
			v      string
			vs, ve int
		}{{f.hasR1, f.r1, f.r1VS, f.r1VE}, {f.hasR2, f.r2, f.r2VS, f.r2VE}} {
			if !a.has {
				continue
			}
			orig := strings.TrimSpace(a.v)
			shifted, err := formula.Shift(orig, w.sh, w.e)
			if err != nil {
				return err
			}
			if shifted != orig {
				edits = append(edits, sdEdit{a.vs, a.ve, shifted})
			}
		}
	}
	w.emitEdits(edits, f.end)
	return nil
}

// sharedF adjusts a shared formula to its planned group entry.
func (w *sdW) sharedF(t xmlscan.Token, f sdF, entry SharedCell) error {
	master := f.text != ""
	switch {
	case !entry.Shared:
		return w.rebuildF(t, f, sdDesired{}, entry.Text, entry.Text != "")
	case entry.Master && !master:
		return w.rebuildF(t, f, sdDesired{t: "shared", hasT: true, ref: entry.Ref, hasRef: true, si: strconv.Itoa(entry.SI), hasSI: true}, entry.Text, true)
	case !entry.Master && master:
		return w.rebuildF(t, f, sdDesired{t: "shared", hasT: true, si: strconv.Itoa(entry.SI), hasSI: true}, "", false)
	}
	var edits []sdEdit
	if f.hasSI && entry.SI != f.si {
		edits = append(edits, sdEdit{f.siVS, f.siVE, strconv.Itoa(entry.SI)})
	}
	if entry.Master {
		if f.hasRef && entry.Ref != strings.TrimSpace(f.ref) {
			edits = append(edits, sdEdit{f.refVS, f.refVE, entry.Ref})
		}
		if !f.empty && entry.Text != f.text {
			edits = append(edits, sdEdit{f.textVS, f.textVE, sdEscape(entry.Text)})
		}
	}
	w.emitEdits(edits, f.end)
	return nil
}

type sdDesired struct {
	t, ref, si          string
	hasT, hasRef, hasSI bool
}

// rebuildF rewrites a formula element with the desired type, ref, si, and text.
func (w *sdW) rebuildF(t xmlscan.Token, f sdF, d sdDesired, text string, hasText bool) error {
	w.emitTo(t.Start)
	sw := &splice.Writer{}
	sw.Start(t.Name(w.xml))
	var produced [3]bool
	for a := range xmlscan.Attrs(w.xml, t) {
		name := a.Name(w.xml)
		if name == "t" || name == "ref" || name == "si" {
			var v string
			var has bool
			var idx int
			switch name {
			case "t":
				v, has, idx = d.t, d.hasT, 0
			case "ref":
				v, has, idx = d.ref, d.hasRef, 1
			default:
				v, has, idx = d.si, d.hasSI, 2
			}
			if has {
				sw.Attr(name, v)
				produced[idx] = true
			}
			continue
		}
		if strings.Contains(name, ":") {
			continue
		}
		v := xmlscan.DecodeText(a.Raw(w.xml))
		sw.Attr(name, v)
	}
	if d.hasT && !produced[0] {
		sw.Attr("t", d.t)
	}
	if d.hasRef && !produced[1] {
		sw.Attr("ref", d.ref)
	}
	if d.hasSI && !produced[2] {
		sw.Attr("si", d.si)
	}
	if hasText {
		sw.Text(text)
	}
	sw.End()
	s, err := sw.String()
	if err != nil {
		return err
	}
	w.b.WriteString(s)
	w.at = f.end
	return nil
}

type sdEdit struct {
	vs, ve int
	repl   string
}

// emitEdits emits the sorted span edits up to end into the output.
func (w *sdW) emitEdits(edits []sdEdit, end int) {
	if len(edits) == 0 {
		w.emitTo(end)
		return
	}
	sort.Slice(edits, func(i, j int) bool { return edits[i].vs < edits[j].vs })
	for _, ed := range edits {
		w.emitTo(ed.vs)
		w.b.WriteString(ed.repl)
		w.at = ed.ve
	}
	w.emitTo(end)
}

// sdMapCoord maps coordinate v through edit e, reporting a deletion or grid overflow.
func sdMapCoord(v int, e formula.Edit, limit int) (int, bool, error) {
	if e.Kind == formula.InsertRows || e.Kind == formula.InsertCols {
		if v >= e.At {
			nv := v + e.Count
			if nv > limit {
				if limit == formula.MaxRows {
					return 0, false, &Refused{Construct: "a cell past row 1,048,576"}
				}
				return 0, false, &Refused{Construct: "a cell past column XFD"}
			}
			return nv, false, nil
		}
		return v, false, nil
	}
	if v >= e.At && v < e.At+e.Count {
		return 0, true, nil
	}
	if v >= e.At+e.Count {
		return v - e.Count, false, nil
	}
	return v, false, nil
}

// sdArrayCell reports whether the cell's last <f> is an array formula
// or data table the edit cuts or removes whole.
func sdArrayCell(xml string, cell xmlscan.Token, e formula.Edit, isRow bool) (whole, cut bool, ref, ftype string, err error) {
	var lastT, lastRef string
	var lastHasRef, found bool
	depth := 1
	pos := cell.End
	for pos < len(xml) {
		t, err := xmlscan.Next(xml, pos)
		if err != nil {
			return false, false, "", "", err
		}
		pos = t.End
		if !t.IsElement() && t.Kind != xmlscan.EndTag {
			continue
		}
		name := t.Local(xml)
		if t.Kind == xmlscan.EndTag {
			depth--
			if depth == 0 {
				if !found || (lastT != "array" && lastT != "dataTable") {
					return false, false, "", "", nil
				}
				whole, cut, err := sdArrayCut(lastRef, lastHasRef, e, isRow)
				return whole, cut, strings.TrimSpace(lastRef), lastT, err
			}
			continue
		}
		if depth == 1 && name == "f" {
			ty := ""
			if a, ok := xmlscan.FindAttr(xml, t, "t"); ok {
				ty = xmlscan.DecodeText(a.Raw(xml))
			}
			rv, hasRef := "", false
			if a, ok := xmlscan.FindAttr(xml, t, "ref"); ok {
				rv, hasRef = xmlscan.DecodeText(a.Raw(xml)), true
			}
			lastT, lastRef, lastHasRef, found = ty, rv, hasRef, true
		}
		if t.Kind != xmlscan.EmptyTag {
			depth++
		}
	}
	return false, false, "", "", fmt.Errorf("sheetdata: unclosed element %s", cell.Local(xml))
}

// sdArrayCut reports whether the array range is wholely deleted or cut by edit e.
func sdArrayCut(ref string, hasRef bool, e formula.Edit, isRow bool) (whole, cut bool, err error) {
	if !hasRef {
		return false, false, fmt.Errorf("sheetdata: array or data table without ref")
	}
	trimmed := strings.TrimSpace(ref)
	r, ok := formula.ParseRange(trimmed)
	if !ok {
		return false, false, fmt.Errorf("sheetdata: invalid ref %q", trimmed)
	}
	r1, c1, r2, c2 := r.Bounds()
	lo, hi := r1, r2
	if !isRow {
		lo, hi = c1, c2
	}
	if e.Kind == formula.InsertRows || e.Kind == formula.InsertCols {
		if e.At > lo && e.At <= hi {
			return false, true, nil
		}
		return false, false, nil
	}
	dStart, dEnd := e.At, e.At+e.Count-1
	if dStart <= lo && dEnd >= hi {
		return true, false, nil
	}
	if dEnd < lo || dStart > hi {
		return false, false, nil
	}
	return false, true, nil
}

// sdRowNum is the row number of a <row> tag: its r attribute when it
// has one, else the next row after last.
func sdRowNum(xml string, t xmlscan.Token, last *int) (n int, hasR bool, attr xmlscan.Attr, err error) {
	if a, ok := xmlscan.FindAttr(xml, t, "r"); ok {
		dec := strings.TrimSpace(xmlscan.DecodeText(a.Raw(xml)))
		num, aerr := strconv.Atoi(dec)
		if aerr != nil || num < 1 || num > formula.MaxRows {
			return 0, false, xmlscan.Attr{}, fmt.Errorf("sheetdata: invalid row r %q", dec)
		}
		*last = num
		return num, true, a, nil
	}
	n = *last + 1
	if n < 1 || n > formula.MaxRows {
		return 0, false, xmlscan.Attr{}, fmt.Errorf("sheetdata: row past grid")
	}
	*last = n
	return n, false, xmlscan.Attr{}, nil
}

// sdCellPos is the row and column of a <c> tag in row row: its r
// attribute when it has one, else the next column after lastCol.
func sdCellPos(xml string, t xmlscan.Token, row int, lastCol *int) (r, c int, hasR bool, attr xmlscan.Attr, rowAbs, colAbs bool, err error) {
	if a, ok := xmlscan.FindAttr(xml, t, "r"); ok {
		dec := strings.TrimSpace(xmlscan.DecodeText(a.Raw(xml)))
		cell, ok2 := formula.ParseCell(dec)
		if !ok2 {
			return 0, 0, false, xmlscan.Attr{}, false, false, fmt.Errorf("sheetdata: invalid cell r %q", dec)
		}
		if cell.Row != row {
			return 0, 0, false, xmlscan.Attr{}, false, false, fmt.Errorf("sheetdata: cell %s names row %d but row is %d", dec, cell.Row, row)
		}
		*lastCol = cell.Col
		return cell.Row, cell.Col, true, a, cell.RowAbs, cell.ColAbs, nil
	}
	col := *lastCol + 1
	if col < 1 || col > formula.MaxCols {
		return 0, 0, false, xmlscan.Attr{}, false, false, fmt.Errorf("sheetdata: cell past grid")
	}
	*lastCol = col
	return row, col, false, xmlscan.Attr{}, false, false, nil
}

type sdF struct {
	typ                 string
	hasRef              bool
	ref                 string
	refVS, refVE        int
	hasSI               bool
	si                  int
	siVS, siVE          int
	hasR1               bool
	r1                  string
	r1VS, r1VE          int
	hasR2               bool
	r2                  string
	r2VS, r2VE          int
	text                string
	empty               bool
	textVS, textVE, end int
}

// sdParseF reads one <f> tag and its text (decoded), validating si.
func sdParseF(xml string, t xmlscan.Token) (sdF, error) {
	var f sdF
	if a, ok := xmlscan.FindAttr(xml, t, "t"); ok {
		f.typ = xmlscan.DecodeText(a.Raw(xml))
	}
	if a, ok := xmlscan.FindAttr(xml, t, "si"); ok {
		dec := strings.TrimSpace(xmlscan.DecodeText(a.Raw(xml)))
		n, err := strconv.Atoi(dec)
		if err != nil || n < 0 {
			return f, fmt.Errorf("sheetdata: invalid si %q", dec)
		}
		f.hasSI, f.si, f.siVS, f.siVE = true, n, a.ValueStart, a.ValueEnd
	}
	if f.typ == "shared" || f.typ == "array" || f.typ == "dataTable" {
		if a, ok := xmlscan.FindAttr(xml, t, "ref"); ok {
			f.hasRef, f.ref, f.refVS, f.refVE = true, xmlscan.DecodeText(a.Raw(xml)), a.ValueStart, a.ValueEnd
		}
	}
	if f.typ == "dataTable" {
		if a, ok := xmlscan.FindAttr(xml, t, "r1"); ok {
			f.hasR1, f.r1, f.r1VS, f.r1VE = true, xmlscan.DecodeText(a.Raw(xml)), a.ValueStart, a.ValueEnd
		}
		if a, ok := xmlscan.FindAttr(xml, t, "r2"); ok {
			f.hasR2, f.r2, f.r2VS, f.r2VE = true, xmlscan.DecodeText(a.Raw(xml)), a.ValueStart, a.ValueEnd
		}
	}
	if t.Kind == xmlscan.EmptyTag {
		f.empty, f.end = true, t.End
		return f, nil
	}
	text, vs, ve, end, err := sdFText(xml, t)
	if err != nil {
		return f, err
	}
	f.text, f.textVS, f.textVE, f.end = text, vs, ve, end
	return f, nil
}

// sdFText returns the decoded text of the <f> in t, with the content's
// span [vs, ve) and the element's end. A CDATA section's bytes are
// text, not references, so they join the decoded text as they are.
func sdFText(xml string, t xmlscan.Token) (string, int, int, int, error) {
	start := t.End
	contentEnd, end, err := xmlscan.ElementEnd(xml, t)
	if err != nil {
		return "", 0, 0, 0, err
	}
	pos := start
	var b *strings.Builder
	for pos < contentEnd {
		ct, err := xmlscan.Next(xml, pos)
		if err != nil {
			return "", 0, 0, 0, err
		}
		pos = ct.End
		switch ct.Kind {
		case xmlscan.Text:
			seg := xml[ct.Start:ct.End]
			if b == nil {
				if ct.End == contentEnd {
					return xmlscan.DecodeText(seg), start, contentEnd, end, nil
				}
				b = &strings.Builder{}
				b.Grow(len(xml) - start)
				b.WriteString(xmlscan.DecodeText(seg))
			} else {
				b.WriteString(xmlscan.DecodeText(seg))
			}
		case xmlscan.CData:
			if b == nil {
				b = &strings.Builder{}
				b.Grow(len(xml) - start)
			}
			b.WriteString(xml[ct.Start+9 : ct.End-3])
		default:
			return "", 0, 0, 0, fmt.Errorf("sheetdata: unexpected markup in formula")
		}
	}
	if b == nil {
		return "", start, contentEnd, end, nil
	}
	return b.String(), start, contentEnd, end, nil
}

// sdEscape escapes a formula text for its element content. A bare
// > needs no escape there and is left as it was (only ]]> must not
// appear in content), so a shifted formula keeps the input's bytes
// apart from the references that moved.
func sdEscape(s string) string {
	if !strings.ContainsAny(s, "&<\r") && !strings.Contains(s, "]]>") {
		return s
	}
	s = strings.NewReplacer("&", "&amp;", "<", "&lt;", "\r", "&#13;").Replace(s)
	return strings.ReplaceAll(s, "]]>", "]]&gt;")
}
