package edit

import (
	"errors"
	"fmt"
	"strconv"

	"github.com/jhnoel/documentdriver/internal/ooxml/ctypes"
	core "github.com/jhnoel/documentdriver/internal/ooxml/edit"
	"github.com/jhnoel/documentdriver/internal/ooxml/tree"
	"github.com/jhnoel/documentdriver/internal/ooxml/xmlscan"
	"github.com/jhnoel/documentdriver/internal/xlsx/sml"
)

// Styles is the working copy's styles part (xl/styles.xml), a small part
// edited as a tree through the edit core. This file is the designer-owned
// shell: it reads the lists into a StyleTable, asks the pure kernel
// (stylekernel.go: PlanFormat, PlanNumFmt, NumFmtOf; kernel K6) what to
// do, and appends what the plan says (numFmt, font, fill, xf), updating
// each list's count. It never reorders, rewrites or removes an entry, so
// every existing s index keeps its meaning.
type Styles struct {
	w     *Workspace
	part  string
	h     *core.Handle
	table StyleTable
	// cellStyleXfs is the number of cellStyleXfs entries (-1 when the
	// part has none), for checkBase.
	cellStyleXfs int
}

// CellFormat is a set of formatting changes (range.format). A nil field
// leaves that property as the base xf has it.
type CellFormat struct {
	// NumberFormat is a format code ("0.00", "yyyy-mm-dd"); a built-in
	// code is written as its built-in id.
	NumberFormat *string
	// Bold, Italic and Underline (single) set the font's b, i and u.
	Bold, Italic, Underline *bool
	// Color is the font colour and Fill a solid fill, each "RRGGBB"
	// (written as rgb="FFRRGGBB", upper case).
	Color, Fill *string
	// Align is alignment@horizontal and Valign alignment@vertical, as
	// ST_HorizontalAlignment and ST_VerticalAlignment spell them; Wrap is
	// alignment@wrapText.
	Align, Valign *string
	Wrap          *bool
}

// ErrNoStyles is returned by Styles for a workbook without a styles
// part (an op that needs one refuses with unreadable_target).
var ErrNoStyles = errors.New("edit: the workbook has no styles part")

// Styles returns the styles part, opened on first use: ErrNoStyles
// without one; *Unreadable when it does not parse or has no cellXfs.
func (w *Workspace) Styles() (*Styles, error) {
	if w.styles != nil {
		return w.styles, nil
	}
	rels, err := w.pkg.Rels(w.wb)
	if err != nil {
		return nil, err
	}
	part := ""
	for _, r := range rels.ByType(ctypes.RelStyles) {
		if p, err := rels.TargetPart(r); err == nil && !r.External() && w.pkg.Has(p) {
			part = p
			break
		}
	}
	if part == "" {
		return nil, ErrNoStyles
	}
	doc, err := w.smallTree(part)
	if err != nil {
		return nil, &Unreadable{Target: part, Detail: "the styles part cannot be read: " + err.Error()}
	}
	root := doc.Root()
	if root == nil || !sml.IsMain(root.Space()) || root.Local() != "styleSheet" || sml.MainChild(root, "cellXfs") == nil {
		return nil, &Unreadable{Target: part, Detail: "the styles part has no cellXfs"}
	}
	st := &Styles{w: w, part: part, table: readTable(root), cellStyleXfs: -1}
	if c := sml.MainChild(root, "cellStyleXfs"); c != nil {
		st.cellStyleXfs = 0
		for e := range c.ElementsSeq() {
			if e.Local() == "xf" {
				st.cellStyleXfs++
			}
		}
	}
	w.styles = st
	return st, nil
}

// readTable reads the lists the kernel plans over: each element's bytes
// as stored, in order.
func readTable(root *tree.Node) StyleTable {
	t := StyleTable{NumFmts: map[int]string{}}
	list := func(container, member string) []string {
		var out []string
		if c := sml.MainChild(root, container); c != nil {
			for e := range c.ElementsSeq() {
				if sml.IsMain(e.Space()) && e.Local() == member {
					out = append(out, string(e.Bytes()))
				}
			}
		}
		return out
	}
	if c := sml.MainChild(root, "numFmts"); c != nil {
		for e := range c.ElementsSeq() {
			if e.Local() != "numFmt" {
				continue
			}
			id, _ := e.Attr("numFmtId")
			code, _ := e.Attr("formatCode")
			if n, err := strconv.Atoi(id); err == nil {
				t.NumFmts[n] = code
			}
		}
	}
	t.Fonts, t.Fills, t.XFs = list("fonts", "font"), list("fills", "fill"), list("cellXfs", "xf")
	return t
}

// Count returns the number of cellXfs entries: a valid s is below it.
func (st *Styles) Count() int { return len(st.table.XFs) }

// NumFmt returns the number format of cellXfs entry xf: its id and its
// code (a built-in's from numfmt.BuiltIn), ok false when xf is out of
// range or the code cannot be found.
func (st *Styles) NumFmt(xf int) (id int, code string, ok bool) { return NumFmtOf(st.table, xf) }

// checkBase is *Unreadable when cellXfs entry xf cannot be the base of a
// new entry: it does not exist, or names a font, fill or cell style the
// part does not have. A new entry would copy the broken reference, a
// fault the input had once made twice (the gate counts it introduced);
// the target is too broken to edit safely (contract §10).
func (st *Styles) checkBase(xf int) error {
	bad := func(detail string) error {
		return &Unreadable{Target: st.part, Detail: detail}
	}
	if xf < 0 || xf >= len(st.table.XFs) {
		return bad(fmt.Sprintf("the cell's style %d is not in cellXfs (%d entries)", xf, len(st.table.XFs)))
	}
	tag := st.table.XFs[xf]
	tok, terr := xmlscan.Next(tag, 0)
	if terr != nil {
		return bad("cellXfs entry " + strconv.Itoa(xf) + " cannot be read")
	}
	for _, c := range []struct {
		attr string
		n    int
	}{{"fontId", len(st.table.Fonts)}, {"fillId", len(st.table.Fills)}, {"xfId", st.cellStyleXfs}} {
		v, ok := "", false
		if a, found := xmlscan.FindAttr(tag, tok, c.attr); found {
			v, ok = a.Value(tag), true
		}
		if c.n < 0 {
			continue
		}
		if !ok {
			v = "0" // the schema default
		}
		if id, err := strconv.Atoi(v); err != nil || id < 0 || id >= c.n {
			return bad(fmt.Sprintf("cellXfs entry %d has %s=%s, the list has %d", xf, c.attr, v, c.n))
		}
	}
	return nil
}

// WithNumFmt returns the index of a cellXfs entry that is xf with its
// number format set to id (built-in) and applyNumberFormat="1",
// interning it. It is how a date written into a cell without a date
// format gets format 14 (date), 22 (date and time) or 21 (time)
// (decision 9, survey W7).
func (st *Styles) WithNumFmt(xf, id int) (int, error) {
	if err := st.checkBase(xf); err != nil {
		return 0, err
	}
	p, err := PlanNumFmt(st.table, xf, id)
	if err != nil {
		return 0, err
	}
	return st.apply(p)
}

// WithFormat returns the index of a cellXfs entry that is xf with f
// applied (and the matching apply* attributes set), interning it and
// any font, fill or numFmt it needs. A bad value is an error that
// changes nothing.
func (st *Styles) WithFormat(xf int, f CellFormat) (int, error) {
	if err := st.checkBase(xf); err != nil {
		return 0, err
	}
	p, err := PlanFormat(st.table, xf, f)
	if err != nil {
		return 0, err
	}
	return st.apply(p)
}

// apply appends what plan p says, in list order, and returns p.XF.
func (st *Styles) apply(p StylePlan) (int, error) {
	if p.NewNumFmt == "" && p.NewFont == "" && p.NewFill == "" && p.NewXF == "" {
		return p.XF, nil
	}
	if st.h == nil {
		h, err := st.w.core.Part(st.part)
		if err != nil {
			return 0, err
		}
		st.h = h
	}
	root := st.h.Elem(0)
	add := func(container, ctype, member, x string) error {
		if x == "" {
			return nil
		}
		c := sml.MainChild(root, container)
		if c == nil {
			if err := st.w.insertSmall(st.part, root, "x:CT_Stylesheet", container, `<`+container+` count="0"></`+container+`>`); err != nil {
				return err
			}
			c = sml.MainChild(root, container)
		}
		if err := st.w.insertSmall(st.part, c, ctype, member, x); err != nil {
			return err
		}
		n := 0
		for e := range c.ElementsSeq() {
			if e.Local() == member {
				n++
			}
		}
		return st.h.SetAttr(core.Path{SML.Name(container)}, "count", strconv.Itoa(n))
	}
	if err := add("numFmts", "x:CT_NumFmts", "numFmt", p.NewNumFmt); err != nil {
		return 0, err
	}
	if err := add("fonts", "x:CT_Fonts", "font", p.NewFont); err != nil {
		return 0, err
	}
	if err := add("fills", "x:CT_Fills", "fill", p.NewFill); err != nil {
		return 0, err
	}
	if err := add("cellXfs", "x:CT_CellXfs", "xf", p.NewXF); err != nil {
		return 0, err
	}
	st.table = readTable(root)
	return p.XF, nil
}
