package ops

import (
	"strings"

	"github.com/jhnoel/documentdriver/internal/ascii"
	"github.com/jhnoel/documentdriver/internal/cli"
	"github.com/jhnoel/documentdriver/internal/geom"
	"github.com/jhnoel/documentdriver/internal/vsdx/edit"
	"github.com/jhnoel/documentdriver/internal/vsdx/formula"
	"github.com/jhnoel/documentdriver/internal/vsdx/inherit"
	"github.com/jhnoel/documentdriver/internal/vsdx/place"
	"github.com/jhnoel/documentdriver/internal/vsdx/sheet"
	"github.com/jhnoel/documentdriver/internal/vsdx/vsd"
)

// The Controls row Visio's Dynamic connector moves its text with.
const (
	controlsSection = "Controls"
	textPositionRow = "TextPosition"
)

// textBoxWrite is what a text_block box or angle writes: shape-level
// cells, and the Controls.TextPosition row cells when the shape's text
// pin follows that control handle.
type textBoxWrite struct {
	cells   []edit.Cell
	control []edit.Cell
}

// textBoxCells builds the writes that put the text block of st (the
// shape at addr, address a) at tb.Box and tb.Angle (the plan's units
// scaled by scale; a member left out keeps its value), through
// place.TextCellsFor: the block pinned at its centre. Nothing is
// written when the box and angle read shows already equal the wanted
// ones (in read's rounding). Each cell is written in Visio's form: a
// plain value, TxtLocPinX and TxtLocPinY with Visio's TxtWidth*0.5 and
// TxtHeight*0.5 formulas, and, on a shape whose TxtPinX and TxtPinY
// follow Controls.TextPosition (the Dynamic connector), the control's
// X and Y, with the pins and the control's XDyn and YDyn as
// inherited caches (F="Inh"), as Visio stores a connector whose text
// was moved. A cell bound to another formula refuses (the Formulas
// rule, operation_failed at text_block).
func textBoxCells(ws *edit.Workspace, st *styleShape, a vsd.Address, addr string, tb *TextBlockSpec, scale float64) (textBoxWrite, error) {
	var out textBoxWrite
	pageAddr := vsd.PageID(a.Container, a.PageDup)
	pg, err := addLoadPage(ws, st.res, a.Container, a.PageDup)
	if err != nil {
		return out, err
	}
	loc, ok := addLocate(pg.root, pageAddr, map[string]bool{addr: true}, st.res)[addr]
	if !ok || !loc.ok {
		return out, cli.Errorf(cli.CodeOperationFailed, "text_block.box", "%s: its transform is not derived, so its text cannot be placed", addr)
	}
	cur := currentTextCells(st, loc.t)
	curBox := place.TextBox(cur, loc.t, loc.f, pg.height)
	want := curBox
	if tb.Box != nil {
		want = geom.Box{X: tb.Box[0] * scale, Y: tb.Box[1] * scale, W: tb.Box[2] * scale, H: tb.Box[3] * scale, Rot: curBox.Rot}
	}
	if tb.Angle != nil {
		want.Rot = geom.NormDeg(*tb.Angle)
	}
	same := true
	for i, p := range [5][2]float64{{want.X, curBox.X}, {want.Y, curBox.Y}, {want.W, curBox.W}, {want.H, curBox.H}, {want.Rot, curBox.Rot}} {
		if i == 4 {
			if geom.FormatNum(geom.NormDeg(p[0])) != geom.FormatNum(geom.NormDeg(p[1])) {
				same = false
			}
		} else if geom.FormatNum(p[0]) != geom.FormatNum(p[1]) {
			same = false
		}
	}
	if same {
		return out, nil
	}
	next, err := place.TextCellsFor(want, loc.t, loc.f, pg.height)
	if err != nil {
		return out, cli.Errorf(cli.CodeOperationFailed, "text_block.box", "%s: %s", addr, err)
	}
	// What the plan keeps as read shows it keeps its stored cells
	// exactly: the size (and so the local pin), the angle, the centre.
	eq := func(a, b float64) bool { return geom.FormatNum(a) == geom.FormatNum(b) }
	if eq(want.W, curBox.W) && eq(want.H, curBox.H) && eq(geom.NormDeg(want.Rot), geom.NormDeg(curBox.Rot)) {
		next.W, next.H, next.LocPinX, next.LocPinY, next.Angle = cur.W, cur.H, cur.LocPinX, cur.LocPinY, cur.Angle
		if eq(want.X+want.W/2, curBox.X+curBox.W/2) && eq(want.Y+want.H/2, curBox.Y+curBox.H/2) {
			next.PinX, next.PinY = cur.PinX, cur.PinY
		} else {
			// Only the centre moves: shift the pin by the move, in the
			// shape's local space.
			moved, err := place.TextCellsFor(geom.Box{X: want.X, Y: want.Y, W: curBox.W, H: curBox.H, Rot: curBox.Rot}, loc.t, loc.f, pg.height)
			if err != nil {
				return out, cli.Errorf(cli.CodeOperationFailed, "text_block.box", "%s: %s", addr, err)
			}
			from, err := place.TextCellsFor(curBox, loc.t, loc.f, pg.height)
			if err != nil {
				return out, cli.Errorf(cli.CodeOperationFailed, "text_block.box", "%s: %s", addr, err)
			}
			next.PinX, next.PinY = cur.PinX+moved.PinX-from.PinX, cur.PinY+moved.PinY-from.PinY
		}
	}
	var refused []formulaCell
	put := func(name string, v, old float64, visioFormula string) {
		// A cell some sheet stores with the wanted value is kept; one
		// no sheet stores is written even at Visio's default, as Visio
		// stores every text transform cell of moved text (and
		// LibreOffice reads an absent TxtPinX as 0, not Width*0.5).
		if _, _, stored := st.res.Cell(st.shape, sheet.ShapeCell(name)); stored && vsd.FormatInches(v) == vsd.FormatInches(old) {
			return
		}
		f, origin := textCellFormula(st, name)
		val := vsd.FormatInches(v)
		switch {
		case visioFormula != "" && ascii.EqualFold(strings.ReplaceAll(f, " ", ""), visioFormula):
			// The value follows from the size; keep the formula (its
			// cache on an instance, as Visio does).
			if origin == inherit.Local {
				out.cells = append(out.cells, edit.Cell{N: name, V: val, F: f})
			} else {
				out.cells = append(out.cells, edit.Cell{N: name, V: val, F: vsd.FormulaInherited})
			}
		case f == "" || formula.Allowed(f):
			out.cells = append(out.cells, edit.Cell{N: name, V: val, F: visioFormula})
		default:
			refused = append(refused, formulaCell{name: name, formula: f})
		}
	}
	put(vsd.CellTxtWidth, next.W, cur.W, "")
	put(vsd.CellTxtHeight, next.H, cur.H, "")
	put(vsd.CellTxtLocPinX, next.LocPinX, cur.LocPinX, "TxtWidth*0.5")
	put(vsd.CellTxtLocPinY, next.LocPinY, cur.LocPinY, "TxtHeight*0.5")
	put(vsd.CellTxtAngle, next.Angle, cur.Angle, "")
	fx, _ := textCellFormula(st, vsd.CellTxtPinX)
	fy, _ := textCellFormula(st, vsd.CellTxtPinY)
	if followsTextPosition(fx) && followsTextPosition(fy) {
		if vsd.FormatInches(next.PinX) != vsd.FormatInches(cur.PinX) || vsd.FormatInches(next.PinY) != vsd.FormatInches(cur.PinY) {
			x, y := vsd.FormatInches(next.PinX), vsd.FormatInches(next.PinY)
			out.control = []edit.Cell{{N: "X", V: x}, {N: "Y", V: y}, {N: "XDyn", V: x, F: vsd.FormulaInherited}, {N: "YDyn", V: y, F: vsd.FormulaInherited}}
			out.cells = append(out.cells, edit.Cell{N: vsd.CellTxtPinX, V: x, F: vsd.FormulaInherited}, edit.Cell{N: vsd.CellTxtPinY, V: y, F: vsd.FormulaInherited})
		}
	} else {
		put(vsd.CellTxtPinX, next.PinX, cur.PinX, "")
		put(vsd.CellTxtPinY, next.PinY, cur.PinY, "")
	}
	if len(refused) > 0 {
		return textBoxWrite{}, formulaRefusal(addr, "text_block", refused)
	}
	return out, nil
}

// currentTextCells returns st's effective text transform cells, a cell
// no sheet stores taking Visio's default (place.DefaultText), as read
// derives them.
func currentTextCells(st *styleShape, t place.Transform) place.TextCells {
	c := place.DefaultText(t)
	num := func(name string, def float64) float64 {
		if v, ok := styleNumberCell(st, name); ok {
			return v
		}
		return def
	}
	c.W = num(vsd.CellTxtWidth, c.W)
	c.H = num(vsd.CellTxtHeight, c.H)
	c.PinX = num(vsd.CellTxtPinX, c.PinX)
	c.PinY = num(vsd.CellTxtPinY, c.PinY)
	c.LocPinX = num(vsd.CellTxtLocPinX, c.W/2)
	c.LocPinY = num(vsd.CellTxtLocPinY, c.H/2)
	c.Angle = num(vsd.CellTxtAngle, 0)
	return c
}

// textCellFormula returns the formula that governs cell name of st and
// where the cell is stored: the shape's own formula, or for a cell the
// shape stores only as an inherited cache (F="Inh") or does not store,
// the formula of its master or style.
func textCellFormula(st *styleShape, name string) (string, inherit.Origin) {
	c, origin, ok := st.res.Cell(st.shape, sheet.ShapeCell(name))
	if !ok {
		return "", ""
	}
	if origin == inherit.Inherited || ascii.EqualFold(c.F, vsd.FormulaInherited) {
		below := *st.shape
		below.Sheet = nil
		if bc, borigin, ok := st.res.Cell(&below, sheet.ShapeCell(name)); ok {
			return bc.F, borigin
		}
		return "", origin
	}
	return c.F, origin
}

// followsTextPosition reports whether formula f sets a text pin from
// the Controls.TextPosition handle (the Dynamic connector's
// SETATREF(Controls.TextPosition) and SETATREF(Controls.TextPosition.Y)).
func followsTextPosition(f string) bool {
	return strings.Contains(ascii.Lower(f), "controls.textposition")
}
