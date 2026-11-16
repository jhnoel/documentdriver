package formula

import (
	"strings"
)

// EditKind is the kind of a structural edit of one sheet.
type EditKind uint8

// The structural edits.
const (
	InsertRows EditKind = iota
	DeleteRows
	InsertCols
	DeleteCols
)

// Edit is a structural edit: Count rows or columns inserted before, or
// deleted from, index At (1-based) of Sheet.
type Edit struct {
	Sheet string
	Kind  EditKind
	At    int
	Count int
}

// Inverse is the edit that undoes e: an insert's inverse deletes what it
// inserted, and a delete's inverse inserts as many again.
func (e Edit) Inverse() Edit {
	switch e.Kind {
	case InsertRows:
		e.Kind = DeleteRows
	case DeleteRows:
		e.Kind = InsertRows
	case InsertCols:
		e.Kind = DeleteCols
	default:
		e.Kind = InsertCols
	}
	return e
}

// rows reports whether the edit inserts or deletes rows.
func (e Edit) rows() bool { return e.Kind == InsertRows || e.Kind == DeleteRows }

// insert reports whether the edit inserts rows or columns.
func (e Edit) insert() bool { return e.Kind == InsertRows || e.Kind == InsertCols }

// limit returns MaxRows for a row edit and MaxCols for a column edit.
func (e Edit) limit() int {
	if e.rows() {
		return MaxRows
	}
	return MaxCols
}

// check returns a *Refusal when the edit is outside the sheet or names no sheet.
func (e Edit) check() error {
	lim := e.limit()
	if e.At < 1 || e.At > lim || e.Count < 1 || e.Count > lim {
		return refuse(0, "edit outside the sheet: at %d, count %d", e.At, e.Count)
	}
	if !e.insert() && e.At+e.Count-1 > lim {
		return refuse(0, "edit outside the sheet: deletes past index %d", lim)
	}
	if e.Sheet == "" {
		return refuse(0, "edit names no sheet")
	}
	return nil
}

// Sheets is where a formula lives: Own is its sheet ("" for a
// workbook-level defined name, where an unqualified reference has no
// sheet), and Order is the workbook's sheets in tab order, which says
// which sheets a 3-D span covers.
type Sheets struct {
	Own   string
	Order []string
}

// index returns the position of name in the sheet order, or -1 when absent.
func (s Sheets) index(name string) int {
	for i, n := range s.Order {
		if strings.EqualFold(n, name) {
			return i
		}
	}
	return -1
}

// Shift rewrites the references in src that a row or column insert or
// delete on e.Sheet moves, and copies every other byte verbatim; a
// reference left in place keeps its text. Sheet names match ignoring
// case. src should lex cleanly (Validate); Invalid tokens are copied.
//
// Rules (assumptions for excel-smoke to check; docs/design.md, xlsx,
// Formulas):
//   - Insert: a coordinate at or after At moves by Count; an area's ends
//     move independently, so an area spanning At grows.
//   - Delete: a reference wholly inside the deleted rows or columns
//     becomes #REF! (after its prefix, so Sheet2!A1 becomes Sheet2!#REF!);
//     an area that overlaps them partly is clipped to what remains.
//   - Absolute and relative coordinates move alike.
//   - Whole columns do not change on a row edit, nor whole rows on a
//     column edit.
//   - An area whose far end is the last row (column) keeps it there: it
//     reads "to the end of the sheet".
//   - A coordinate that would pass row 1,048,576 or column XFD refuses.
//   - References into another workbook ("[1]Sheet1!A1") are never
//     changed.
//   - A 3-D reference whose span holds e.Sheet refuses when the edit
//     would move it; so does an unqualified reference when the formula
//     has no own sheet, and a span whose ends are not in Order.
//
// The error is a *Refusal.
func Shift(src string, sh Sheets, e Edit) (string, error) {
	if err := e.check(); err != nil {
		return "", err
	}
	toks := Lex(src)
	var b strings.Builder
	changed := false
	for i, t := range toks {
		out := t.Text
		if t.Kind == Ref {
			s, err := shiftToken(t, sh, e)
			if err != nil {
				return "", err
			}
			out = s
		}
		if out != t.Text && !changed {
			changed = true
			b.Grow(len(src) + 8)
			for _, p := range toks[:i] {
				b.WriteString(p.Text)
			}
		}
		if changed {
			b.WriteString(out)
		}
	}
	if !changed {
		return src, nil
	}
	return b.String(), nil
}

// shiftToken returns the text of a Ref token after the edit.
func shiftToken(t Token, sh Sheets, e Edit) (string, error) {
	pre, body, ok := SplitQualified(t.Text)
	if !ok {
		return t.Text, nil
	}
	r, isErr, ok := ParseRefBody(body)
	if !ok || isErr || pre.HasBook {
		return t.Text, nil
	}
	nr, gone, err := shiftRange(r, e)
	if err != nil {
		err.Pos = t.Pos
		return "", err
	}
	if !gone && nr == r {
		return t.Text, nil
	}
	// The reference would change: is it on the edited sheet?
	switch {
	case pre.ThreeD():
		lo, hi := sh.index(pre.First), sh.index(pre.Last)
		at := sh.index(e.Sheet)
		if lo < 0 || hi < 0 {
			return "", refuse(t.Pos, "3-D reference %q names a sheet not in the workbook", t.Text)
		}
		if lo > hi {
			lo, hi = hi, lo
		}
		if at >= lo && at <= hi {
			return "", refuse(t.Pos, "3-D reference %q spans sheet %q", t.Text, e.Sheet)
		}
		return t.Text, nil
	case pre.HasSheet():
		if !strings.EqualFold(pre.First, e.Sheet) {
			return t.Text, nil
		}
	default:
		if sh.Own == "" {
			return "", refuse(t.Pos, "reference %q has no sheet and the formula has none", t.Text)
		}
		if !strings.EqualFold(sh.Own, e.Sheet) {
			return t.Text, nil
		}
	}
	if gone {
		return pre.Text + "#REF!", nil
	}
	return pre.Text + nr.String(), nil
}

// shiftRange applies the edit to one range on the edited sheet. gone
// means the range was wholly deleted.
func shiftRange(r Range, e Edit) (Range, bool, *Refusal) {
	if e.rows() && r.Kind == Cols || !e.rows() && r.Kind == Rows {
		return r, false, nil
	}
	lim := e.limit()
	get := func(c Cell) int {
		if e.rows() {
			return c.Row
		}
		return c.Col
	}
	set := func(c *Cell, v int) {
		if e.rows() {
			c.Row = v
		} else {
			c.Col = v
		}
	}
	a, b := get(r.From), get(r.To)
	fromIsLo := a <= b
	lo, hi := min(a, b), max(a, b)
	single := r.Kind == Cells && r.Single
	var nlo, nhi int
	if e.insert() {
		move := func(v int) int {
			if v >= e.At {
				return v + e.Count
			}
			return v
		}
		nlo, nhi = move(lo), move(hi)
		if !single && hi == lim {
			nhi = lim
		}
		if nlo > lim || nhi > lim {
			return r, false, refuse(0, "the edit moves a reference past %s", limitName(e))
		}
	} else {
		first, last := e.At, e.At+e.Count-1
		if lo >= first && hi <= last {
			return r, true, nil
		}
		switch {
		case lo < first:
			nlo = lo
		case lo <= last:
			nlo = first
		default:
			nlo = lo - e.Count
		}
		switch {
		case !single && hi == lim:
			nhi = lim
		case hi > last:
			nhi = hi - e.Count
		case hi >= first:
			nhi = first - 1
		default:
			nhi = hi
		}
	}
	if single {
		set(&r.From, nlo)
		set(&r.To, nlo)
		return r, false, nil
	}
	if fromIsLo {
		set(&r.From, nlo)
		set(&r.To, nhi)
	} else {
		set(&r.From, nhi)
		set(&r.To, nlo)
	}
	return r, false, nil
}

// limitName returns "row 1,048,576" for row edits and "column XFD" otherwise.
func limitName(e Edit) string {
	if e.rows() {
		return "row 1,048,576"
	}
	return "column XFD"
}

// Rename rewrites every sheet prefix in src that names old (ignoring
// case), the ends of 3-D spans included, to name, quoting it when it
// needs quotes (NeedsQuote: R10's re-quoting of names that look like
// references) and dropping quotes it no longer needs. References into
// other workbooks, unqualified references and every other byte are
// unchanged. Text in string literals (INDIRECT("Old!A1")) is not a
// reference and is not changed.
func Rename(src, old, name string) string {
	toks := Lex(src)
	var b strings.Builder
	changed := false
	for i, t := range toks {
		out := t.Text
		if t.Kind == Ref || t.Kind == Name || t.Kind == Func {
			out = renameToken(t.Text, old, name, i > 0 && toks[i-1].Text == ":")
		}
		if out != t.Text && !changed {
			changed = true
			for _, p := range toks[:i] {
				b.WriteString(p.Text)
			}
		}
		if changed {
			b.WriteString(out)
		}
	}
	if !changed {
		return src
	}
	return b.String()
}

// renameToken renames the sheet prefix in text to name when it names old
// ignoring case. afterColon says the token directly follows a ":" token:
// a 3-D span quoted as a whole there ("x:'Jan:My Sheet'!A1") would read
// back as the span x:'Jan:My Sheet' and swallow the "x:" before it, so
// the span is written with each end quoted apart ("x:'Jan':'My Sheet'!A1"),
// which a prefix of at most two names cannot extend.
func renameToken(text, old, name string, afterColon bool) string {
	pre, rest, ok := SplitQualified(text)
	if !ok || !pre.HasSheet() || pre.HasBook {
		return text
	}
	first, last := pre.First, pre.Last
	hit := false
	if strings.EqualFold(first, old) {
		first, hit = name, true
	}
	if last != "" && strings.EqualFold(last, old) {
		last, hit = name, true
	}
	if !hit {
		return text
	}
	p := SheetPrefix(first, last)
	if afterColon && last != "" && p[0] == '\'' {
		p = quotedApart(first, last)
	}
	return p + rest
}
