package edit

import (
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/jhnoel/documentdriver/internal/ooxml/xmlscan"
	"github.com/jhnoel/documentdriver/internal/xlsx/formula"
	"github.com/jhnoel/documentdriver/internal/xlsx/sheet"
)

// The row and cell locator. Everything here reads a worksheet text that
// sheet.Scan accepted (so it is well formed) and that the Index was
// built from, and reads only the bytes it is asked about: the root's
// start tag, the top-level elements other than sheetData (the head and
// the tail), one row, or one cell.

// errLex is returned when the bytes at a position are not a node: the
// source is not the well-formed part the caller indexed.
var errLex = errors.New("edit: worksheet bytes are not where the index says")

// child is one top-level element of the worksheet root.
type child struct {
	local      string
	start, end int
}

// layout is where things are in one worksheet text.
type layout struct {
	// prefix is the root's prefix with its colon ("x:"), "" for the
	// default namespace; new elements are written with it. ns is the
	// root's namespace (Transitional or Strict).
	prefix, ns string
	// decls are the root's namespace declarations as written
	// (` xmlns="…" xmlns:r="…"`), for checking a fragment in its scope.
	decls string
	// contentStart and contentEnd bound the root's content: from the end
	// of its start tag to the start of its end tag.
	contentStart, contentEnd int
	// children are the root's element children in document order.
	children []child
	// sd is sheetData's index in children; sdEmpty says it is an
	// empty-element tag; sdInner and sdInnerEnd bound its content when
	// it is not.
	sd                  int
	sdEmpty             bool
	sdInner, sdInnerEnd int
}

// locateLayout finds the layout of src, whose index is ix: the root, its
// top-level children, and sheetData (whose rows are skipped by jumping
// from the last indexed row's start to its end). It reads the head, the
// last row and the tail only.
func locateLayout(src string, ix *sheet.Index) (*layout, error) {
	l := &layout{sd: -1}
	i := 0
	var root xmlscan.Token
	for {
		n, err := xmlscan.Next(src, i)
		if err != nil {
			return nil, errLex
		}
		if n.IsElement() {
			root = n
			break
		}
		i = n.End
	}
	if root.Kind == xmlscan.EmptyTag {
		return nil, fmt.Errorf("%w: the worksheet root is empty", errLex)
	}
	name := root.Name(src)
	l.prefix = name[:len(name)-len(root.Local(src))]
	if a, ok := xmlscan.FindAttr(src, root, xmlnsFor(l.prefix)); ok {
		l.ns = a.Value(src)
	}
	var decls strings.Builder
	for a := range xmlscan.Attrs(src, root) {
		if n := a.Name(src); n == "xmlns" || strings.HasPrefix(n, "xmlns:") {
			decls.WriteString(" " + n + `="` + a.Raw(src) + `"`)
		}
	}
	l.decls = decls.String()
	l.contentStart = root.End
	for i = root.End; ; {
		n, err := xmlscan.Next(src, i)
		if err != nil {
			return nil, errLex
		}
		switch n.Kind {
		case xmlscan.EndTag:
			l.contentEnd = n.Start
			if l.sd < 0 {
				return nil, fmt.Errorf("%w: the worksheet has no sheetData", errLex)
			}
			return l, nil
		case xmlscan.StartTag, xmlscan.EmptyTag:
			c := child{local: n.Local(src), start: n.Start}
			if c.local == "sheetData" && l.sd < 0 {
				l.sd = len(l.children)
				end, err := sheetDataEnd(src, n, ix, l)
				if err != nil {
					return nil, err
				}
				c.end = end
			} else if _, end, err := xmlscan.ElementEnd(src, n); err != nil {
				return nil, errLex
			} else {
				c.end = end
			}
			l.children = append(l.children, c)
			i = c.end
		default:
			i = n.End
		}
	}
}

// xmlnsFor returns the namespace declaration name for a tag prefix ("xmlns:x", or "xmlns" when empty).
func xmlnsFor(prefix string) string {
	if prefix == "" {
		return "xmlns"
	}
	return "xmlns:" + prefix[:len(prefix)-1]
}

// sheetDataEnd returns the end of the sheetData element whose start tag
// is n, filling l's sheetData fields. Its rows are not lexed: the scan
// resumes at the end of the last indexed row (or at the start of the
// content when there is none) and must meet only text, comments and
// sheetData's end tag; a row the index does not hold (one Scan skipped)
// is errLex.
func sheetDataEnd(src string, n xmlscan.Token, ix *sheet.Index, l *layout) (int, error) {
	if n.Kind == xmlscan.EmptyTag {
		l.sdEmpty = true
		return n.End, nil
	}
	l.sdInner = n.End
	i := n.End
	if k := ix.Rows(); k > 0 {
		_, _, off := ix.Row(k - 1)
		r, err := xmlscan.Next(src, off)
		if err != nil {
			return 0, errLex
		}
		_, end, err := xmlscan.ElementEnd(src, r)
		if err != nil {
			return 0, errLex
		}
		i = end
	}
	for {
		c, err := xmlscan.Next(src, i)
		if err != nil {
			return 0, errLex
		}
		switch c.Kind {
		case xmlscan.EndTag:
			l.sdInnerEnd = c.Start
			return c.End, nil
		case xmlscan.StartTag, xmlscan.EmptyTag:
			return 0, fmt.Errorf("%w: sheetData holds an element the index skipped", errLex)
		}
		i = c.End
	}
}

// element returns the top-level children named local, in order.
func (l *layout) element(local string) []child {
	var out []child
	for _, c := range l.children {
		if c.local == local {
			out = append(out, c)
		}
	}
	return out
}

// insertPoint returns where a new top-level element named local goes:
// the start of the first existing child the worksheet's schema order
// puts after it, else the end of the root's content. Children the order
// does not know (foreign elements) do not move the point.
func (l *layout) insertPoint(local string) (int, error) {
	want, ok := worksheetRank(local)
	if !ok {
		return 0, fmt.Errorf("edit: %s is not a worksheet child", local)
	}
	for _, c := range l.children {
		if r, ok := worksheetRank(c.local); ok && r > want {
			return c.start, nil
		}
	}
	return l.contentEnd, nil
}

// rowSpan is one located row.
type rowSpan struct {
	num               int
	start, tagEnd     int // the start tag is src[start:tagEnd]
	end               int
	empty             bool // an empty-element tag
	innerStart, inner int  // content bounds (innerStart == inner when empty)
}

// cellSpan is one located cell of a row.
type cellSpan struct {
	col        int
	start, end int
}

// findRow returns the position in ix of row num and whether it is there;
// when it is not, the position is where it would be inserted.
func findRow(ix *sheet.Index, num int) (int, bool) {
	n := ix.Rows()
	i := sort.Search(n, func(i int) bool { r, _, _ := ix.Row(i); return r >= num })
	if i < n {
		if r, _, _ := ix.Row(i); r == num {
			return i, true
		}
	}
	return i, false
}

// locateRow returns the span of the i-th indexed row of src.
func locateRow(src string, ix *sheet.Index, i int) (rowSpan, error) {
	num, _, off := ix.Row(i)
	n, err := xmlscan.Next(src, off)
	if err != nil {
		return rowSpan{}, errLex
	}
	if !n.IsElement() || n.Local(src) != "row" {
		return rowSpan{}, errLex
	}
	contentEnd, end, err := xmlscan.ElementEnd(src, n)
	if err != nil {
		return rowSpan{}, errLex
	}
	return rowSpan{num: num, start: n.Start, tagEnd: n.End, end: end, empty: n.Kind == xmlscan.EmptyTag, innerStart: n.End, inner: contentEnd}, nil
}

// rowChildren returns the element children of row r: its cells, with
// their columns, and the start of the first child that is not a cell
// (extLst), or r.inner when there is none. A cell's column is its r
// attribute, or the one after the previous cell's when r is absent, as
// sheet.Scan counts.
func rowChildren(src string, r rowSpan) (cells []cellSpan, other int, err error) {
	other = r.inner
	prev := 0
	for i := r.innerStart; i < r.inner; {
		n, err := xmlscan.Next(src, i)
		if err != nil {
			return nil, 0, errLex
		}
		switch {
		case n.IsElement():
			_, end, err := xmlscan.ElementEnd(src, n)
			if err != nil {
				return nil, 0, errLex
			}
			if n.Local(src) != "c" {
				if other == r.inner {
					other = n.Start
				}
				i = end
				continue
			}
			col := prev + 1
			if a, ok := xmlscan.FindAttr(src, n, "r"); ok {
				c, okc := formula.ParseCell(a.Value(src))
				if !okc {
					return nil, 0, fmt.Errorf("%w: cell address %q", errLex, a.Value(src))
				}
				col = c.Col
			}
			cells = append(cells, cellSpan{col: col, start: n.Start, end: end})
			prev = col
			i = end
		default:
			i = n.End
		}
	}
	return cells, other, nil
}
