package edit

import (
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/jhnoel/documentdriver/internal/ooxml/splice"
	"github.com/jhnoel/documentdriver/internal/ooxml/xmlscan"
	"github.com/jhnoel/documentdriver/internal/xlsx/formula"
)

// Formula is a cell's <f> as an edit writes it.
type Formula struct {
	// Text is the stored form (no "=", prefixes such as _xlfn. written
	// out); "" for a shared child.
	Text string
	// Type is t: "" (normal), "shared" or "array".
	Type string
	// Ref is ref ("" for none): the cells of a shared group or an array.
	Ref string
	// SI is si, when HasSI.
	SI    int
	HasSI bool
}

// cellParts is one <c> element taken apart: its start tag's attributes
// and its element children by local name, in order, as written.
type cellParts struct {
	name     string
	attrs    []AttrEdit
	children []cellChild
}

type cellChild struct {
	local, xml string
}

// splitCell takes the <c> element xml apart. Text between its children
// (whitespace) is dropped: the cell is being rewritten.
func splitCell(xml string) (cellParts, error) {
	n, err := xmlscan.Next(xml, 0)
	if err != nil || !n.IsElement() || n.Local(xml) != "c" {
		return cellParts{}, fmt.Errorf("edit: not a <c> element")
	}
	out := cellParts{name: n.Name(xml)}
	for a := range xmlscan.Attrs(xml, n) {
		out.attrs = append(out.attrs, AttrEdit{Name: a.Name(xml), Value: a.Value(xml)})
	}
	if n.Kind == xmlscan.EmptyTag {
		return out, nil
	}
	for i := n.End; i < len(xml); {
		c, err := xmlscan.Next(xml, i)
		if err != nil {
			return cellParts{}, errLex
		}
		switch {
		case c.IsElement():
			_, end, err := xmlscan.ElementEnd(xml, c)
			if err != nil {
				return cellParts{}, errLex
			}
			out.children = append(out.children, cellChild{local: c.Local(xml), xml: xml[c.Start:end]})
			i = end
		case c.Kind == xmlscan.EndTag:
			return out, nil
		default:
			i = c.End
		}
	}
	return out, nil
}

// join renders the parts back into a <c> element, children in x:CT_Cell
// order (f, v, is, extLst; children the order does not list keep their
// place after those it does).
func (c cellParts) join() (string, error) {
	var w splice.Writer
	w.Start(c.name)
	for _, a := range c.attrs {
		w.Attr(a.Name, a.Value)
	}
	w.End()
	x, err := w.String()
	if err != nil {
		return "", err
	}
	if len(c.children) == 0 {
		return x, nil
	}
	kids := slices.Clone(c.children)
	slices.SortStableFunc(kids, func(a, b cellChild) int {
		ra, rb := cellRank(a.local), cellRank(b.local)
		if ra < 0 {
			ra = len(cellOrder())
		}
		if rb < 0 {
			rb = len(cellOrder())
		}
		return ra - rb
	})
	var b strings.Builder
	b.WriteString(strings.TrimSuffix(x, "/>") + ">")
	for _, k := range kids {
		b.WriteString(k.xml)
	}
	b.WriteString("</" + c.name + ">")
	return b.String(), nil
}

// set sets the named attribute of the cell's start tag to value.
func (c *cellParts) set(name, value string) {
	c.attrs = applyAttrEdits(c.attrs, []AttrEdit{{Name: name, Value: value}})
}

// remove deletes the named attribute from the cell's start tag.
func (c *cellParts) remove(name string) {
	c.attrs = applyAttrEdits(c.attrs, []AttrEdit{{Name: name, Remove: true}})
}

// child returns the index of the child with the given local name and whether it exists.
func (c *cellParts) child(local string) (int, bool) {
	i := slices.IndexFunc(c.children, func(k cellChild) bool { return k.local == local })
	return i, i >= 0
}

// put replaces child local with xml ("" removes it), or adds it.
func (c *cellParts) put(local, xml string) {
	i, ok := c.child(local)
	switch {
	case ok && xml == "":
		c.children = slices.Delete(c.children, i, i+1)
	case ok:
		c.children[i].xml = xml
	case xml != "":
		c.children = append(c.children, cellChild{local: local, xml: xml})
	}
}

// emptyCell is the <c r> of (row, col) in this sheet's prefix.
func (s *Sheet) emptyCell(row, col int) string {
	return "<" + s.lay.prefix + `c r="` + formula.ColumnName(col) + strconv.Itoa(row) + `"/>`
}

// current returns the cell's XML, or an empty <c r> when it does not
// exist.
func (s *Sheet) current(row, col int) (Cell, string, error) {
	c, err := s.Cell(row, col)
	if err != nil {
		return c, "", err
	}
	if !c.Exists {
		return c, s.emptyCell(row, col), nil
	}
	return c, c.XML, nil
}

// SetFormula replaces cell (row, col)'s <f> with f, or removes it when f
// is nil, keeping every other attribute and child of the cell (its s,
// t and <v>: a shifted formula reads the same values) and the <f>'s own
// attributes other than t, ref and si. A cell that does not exist is
// created when f is not nil. It marks formulas.
func (s *Sheet) SetFormula(row, col int, f *Formula) error {
	c, xml, err := s.current(row, col)
	if err != nil {
		return err
	}
	if !c.Exists && f == nil {
		return nil
	}
	parts, err := splitCell(xml)
	if err != nil {
		return s.unreadable(err)
	}
	fx := ""
	if f != nil {
		var keep []AttrEdit
		if i, ok := parts.child("f"); ok {
			old, err := splitAttrs(parts.children[i].xml)
			if err != nil {
				return s.unreadable(err)
			}
			for _, a := range old {
				if a.Name != "t" && a.Name != "ref" && a.Name != "si" {
					keep = append(keep, a)
				}
			}
		}
		var w splice.Writer
		w.Start(s.lay.prefix + "f")
		if f.Type != "" {
			w.Attr("t", f.Type)
		}
		for _, a := range keep {
			w.Attr(a.Name, a.Value)
		}
		if f.Ref != "" {
			w.Attr("ref", f.Ref)
		}
		if f.HasSI {
			w.Attr("si", strconv.Itoa(f.SI))
		}
		w.Text(f.Text)
		w.End()
		if fx, err = w.String(); err != nil {
			return err
		}
	}
	parts.put("f", fx)
	out, err := parts.join()
	if err != nil {
		return err
	}
	if err := s.setCell(row, col, out, false); err != nil {
		return err
	}
	s.w.MarkFormulas()
	return nil
}

// splitAttrs returns the attributes of the start tag that begins xml.
func splitAttrs(xml string) ([]AttrEdit, error) {
	n, err := xmlscan.Next(xml, 0)
	if err != nil {
		return nil, errLex
	}
	var out []AttrEdit
	for a := range xmlscan.Attrs(xml, n) {
		out = append(out, AttrEdit{Name: a.Name(xml), Value: a.Value(xml)})
	}
	return out, nil
}

// SetCachedValue sets the cached value of an existing formula cell: its
// <v> to v and its t to t ("" removes t: a number), keeping its <f> and
// everything else. It is the --recalc step's splice of a computed value
// and does not mark the recalculation state. A cell without a formula is
// refused: the tool never writes a value it computed into a value cell.
func (s *Sheet) SetCachedValue(row, col int, t, v string) error {
	c, err := s.Cell(row, col)
	if err != nil {
		return err
	}
	if !c.Exists || !c.Raw.F.Present {
		return fmt.Errorf("edit: %s%d holds no formula", formula.ColumnName(col), row)
	}
	parts, err := splitCell(c.XML)
	if err != nil {
		return s.unreadable(err)
	}
	if t == "" {
		parts.remove("t")
	} else {
		parts.set("t", t)
	}
	var w splice.Writer
	w.Start(s.lay.prefix + "v")
	w.Text(v)
	w.End()
	vx, err := w.String()
	if err != nil {
		return err
	}
	parts.put("v", vx)
	parts.put("is", "")
	out, err := parts.join()
	if err != nil {
		return err
	}
	return s.setCell(row, col, out, false)
}

// SetCellStyle sets cell (row, col)'s s to style (0 removes the
// attribute, Excel's default), creating a style-only cell when it does
// not exist (and doing nothing for style 0 then). Content is kept. It
// does not mark the recalculation state.
func (s *Sheet) SetCellStyle(row, col, style int) error {
	if style < 0 {
		return fmt.Errorf("edit: style %d", style)
	}
	c, xml, err := s.current(row, col)
	if err != nil {
		return err
	}
	if !c.Exists && style == 0 {
		return nil
	}
	parts, err := splitCell(xml)
	if err != nil {
		return s.unreadable(err)
	}
	if style == 0 {
		parts.remove("s")
	} else {
		parts.set("s", strconv.Itoa(style))
	}
	out, err := parts.join()
	if err != nil {
		return err
	}
	return s.setCell(row, col, out, false)
}

// ClearCell empties cell (row, col)'s content (range.clear): its <f>,
// <v>, <is> and t, cm and vm go; its s and other attributes stay. A cell
// left with nothing but r (and s 0) is deleted. It marks values (and
// formulas when the cell had one).
func (s *Sheet) ClearCell(row, col int) error {
	c, err := s.Cell(row, col)
	if err != nil || !c.Exists {
		return err
	}
	parts, err := splitCell(c.XML)
	if err != nil {
		return s.unreadable(err)
	}
	for _, a := range []string{"t", "cm", "vm"} {
		parts.remove(a)
	}
	for _, k := range []string{"f", "v", "is"} {
		parts.put(k, "")
	}
	keep := slices.ContainsFunc(parts.attrs, func(a AttrEdit) bool {
		return a.Name != "r" && !(a.Name == "s" && a.Value == "0")
	}) || len(parts.children) > 0
	out := ""
	if keep {
		if out, err = parts.join(); err != nil {
			return err
		}
	}
	return s.SetCell(row, col, out)
}

// SetFormulaText replaces the text of cell (row, col)'s existing <f>,
// keeping every attribute of the <f> (t, ref, si, ca, ...) and the rest
// of the cell: how a shift or a rename rewrites a formula on another
// sheet without unsharing its group (a shared master keeps t="shared",
// its ref and si). A cell without an <f> is an error. It marks values
// (the formula cell neither appears nor goes).
func (s *Sheet) SetFormulaText(row, col int, text string) error {
	c, err := s.Cell(row, col)
	if err != nil {
		return err
	}
	if !c.Exists || !c.Raw.F.Present {
		return fmt.Errorf("edit: %s%d holds no formula", formula.ColumnName(col), row)
	}
	parts, err := splitCell(c.XML)
	if err != nil {
		return s.unreadable(err)
	}
	i, _ := parts.child("f")
	attrs, err := splitAttrs(parts.children[i].xml)
	if err != nil {
		return s.unreadable(err)
	}
	var w splice.Writer
	w.Start(s.lay.prefix + "f")
	for _, a := range attrs {
		w.Attr(a.Name, a.Value)
	}
	w.Text(text)
	w.End()
	fx, err := w.String()
	if err != nil {
		return err
	}
	parts.children[i].xml = fx
	out, err := parts.join()
	if err != nil {
		return err
	}
	if err := s.setCell(row, col, out, false); err != nil {
		return err
	}
	s.w.MarkValues()
	return nil
}
