package ops

import (
	"strconv"

	"github.com/jhnoel/documentdriver/internal/cli"
	"github.com/jhnoel/documentdriver/internal/geom"
	"github.com/jhnoel/documentdriver/internal/vsdx/edit"
	"github.com/jhnoel/documentdriver/internal/vsdx/vsd"
)

// valignCells maps text_block.valign to the stored VerticalAlign.
var valignCells = map[string]string{"top": "0", "middle": "1", "bottom": "2"}

// marginCells are the margin cells in text_block.margins order.
var marginCells = [4]string{vsd.CellLeftMargin, vsd.CellTopMargin, vsd.CellRightMargin, vsd.CellBottomMargin}

// setTextBlock applies s.TextBlock to the shape at s.At (resolved
// through aliases) and returns the changed address when it wrote
// anything: box and angle through textBoxCells; valign as VerticalAlign 0, 1 or 2; margins (in the plan's
// units) as LeftMargin, TopMargin, RightMargin and BottomMargin in
// inches with U="PT", the form Visio stores; background "none" as
// TextBkgnd 0 (and TextBkgndTrans 0 when one is stored that is not),
// a colour as TextBkgnd #RRGGBB with TextBkgndTrans the transparency
// as a fraction (0 when none is given). A cell whose shown value
// already equals the wanted one is not written, so writing back what
// read shows changes nothing. A cell whose effective formula is
// outside formula.Allowed refuses with operation_failed at
// text_block, before anything is written; the shape refusals are
// setStyle's (loadStyleShape, shapeRefusal), and a picture or kind
// other is operation_failed at at.
func setTextBlock(ws *edit.Workspace, s *ObjectSetSpec, aliases map[string]string, units string) ([]string, string, error) {
	tb := s.TextBlock
	if tb == nil {
		return nil, "", nil
	}
	addr, err := resolveShape(s.At, aliases)
	if err != nil {
		return nil, "", err
	}
	a, err := vsd.Parse(addr)
	if err != nil {
		return nil, "", cli.Errorf(cli.CodeOperationFailed, "at", "%s is not a shape address", addr)
	}
	st, err := loadStyleShape(ws, a, addr)
	if err != nil {
		return nil, "", err
	}
	if err := shapeRefusal(st.res, st.shape, addr, "at", false); err != nil {
		return nil, "", err
	}
	scale := 1.0
	if units == "in" {
		scale = vsd.PointsPerInch
	}
	var cells []edit.Cell
	want := func(c edit.Cell, same bool) {
		if !same {
			cells = append(cells, c)
		}
	}
	if tb.VAlign != "" {
		v := valignCells[tb.VAlign]
		got, ok := styleStoredCell(st, vsd.CellVerticalAlign)
		want(edit.Cell{N: vsd.CellVerticalAlign, V: v}, ok && numberEquals(got, v))
	}
	for i, m := range tb.Margins {
		pts := m * scale
		got, ok := styleNumberCell(st, marginCells[i])
		same := ok && geom.FormatNum(got*vsd.PointsPerInch) == geom.FormatNum(pts)
		want(edit.Cell{N: marginCells[i], V: vsd.FormatInches(pts / vsd.PointsPerInch), U: "PT"}, same)
	}
	if bg := tb.Background; bg != nil {
		cur, curOK := textBgOf(st)
		trans := 0.0
		if bg.Transparency != nil {
			trans = *bg.Transparency
		}
		switch {
		case bg.None:
			want(edit.Cell{N: vsd.CellTextBkgnd, V: "0"}, curOK && cur.none)
		default:
			want(edit.Cell{N: vsd.CellTextBkgnd, V: bg.Hex}, curOK && !cur.none && cur.hex == bg.Hex)
			got, ok := styleNumberCell(st, vsd.CellTextBkgndTrans)
			same := (ok && geom.FormatNum(got*100) == geom.FormatNum(trans)) || (!ok && trans == 0)
			want(edit.Cell{N: vsd.CellTextBkgndTrans, V: strconv.FormatFloat(trans/100, 'f', -1, 64)}, same)
		}
	}
	for _, c := range cells {
		if fc, ok := styleRefused(st, c.N); ok {
			return nil, "", formulaRefusal(addr, "text_block", []formulaCell{fc})
		}
	}
	var control []edit.Cell
	if tb.Box != nil || tb.Angle != nil {
		// The box's cells are checked against the Formulas rule as
		// they are built (textBoxCells keeps Visio's own formulas).
		w, err := textBoxCells(ws, st, a, addr, tb, scale)
		if err != nil {
			return nil, "", err
		}
		cells = append(cells, w.cells...)
		control = w.control
	}
	if len(cells) == 0 && len(control) == 0 {
		return nil, "", nil
	}
	if err := ws.SetCells(addr, cells); err != nil {
		return nil, "", err
	}
	if len(control) > 0 {
		if err := ws.SetRowCells(addr, controlsSection, textPositionRow, control); err != nil {
			return nil, "", err
		}
	}
	return []string{addr}, "", nil
}

// numberEquals reports whether the stored value got is the number v.
func numberEquals(got, v string) bool {
	g, ok := vsd.Number(got)
	w, ok2 := vsd.Number(v)
	return ok && ok2 && g == w
}

// textBg is a shape's effective text background: none, or a colour.
type textBg struct {
	none bool
	hex  string
}

// textBgOf returns the effective TextBkgnd of st (vsd.TextBackground,
// an index resolved through the document's colours); false when no
// sheet stores it, it is themed, or it does not resolve.
func textBgOf(st *styleShape) (textBg, bool) {
	v, ok := styleStoredCell(st, vsd.CellTextBkgnd)
	if !ok {
		return textBg{}, false
	}
	none, hex, index, ok := vsd.TextBackground(v)
	switch {
	case !ok:
		return textBg{}, false
	case none:
		return textBg{none: true}, true
	case index >= 0:
		h, ok := st.res.Doc().Color(strconv.Itoa(index))
		return textBg{hex: h}, ok
	}
	return textBg{hex: hex}, true
}
