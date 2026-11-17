package edit

import (
	"fmt"
	"strconv"
	"strings"

	core "github.com/jhnoel/documentdriver/internal/ooxml/edit"
	"github.com/jhnoel/documentdriver/internal/ooxml/tree"
	"github.com/jhnoel/documentdriver/internal/vsdx/vsd"
)

// GlueEnd is one desired end of a 1-D shape: its stored coordinates
// in parent inches (y up), and its glue. Glue false is a free end.
// Whole glue targets the whole shape (ToCell PinX, ToPart 3); point
// glue targets Connections.XPoint (ToPart 100+point-1). Formula is
// the desired F of the end's X and Y cells ("" is no formula).
// KeepFormula keeps the existing F (a whole-shape _WALKGLUE the
// connector's master carries, an unchanged end), and KeepConnect
// leaves the end's Connect rows untouched (an unchanged end whose
// ToCell this write does not model).
type GlueEnd struct {
	X, Y        float64
	Glue        bool
	Whole       bool
	Target      uint32
	Point       int
	Formula     string
	KeepFormula bool
	KeepConnect bool
}

// ConnectorEnds is the desired state of a 1-D shape's ends and the
// transform cells formula.OneD derives from them (inches, radians):
// PinX, PinY, Width, Height, LocPinX, LocPinY and Angle.
type ConnectorEnds struct {
	Begin, End                                  GlueEnd
	PinX, PinY, Width, Height, LocPinX, LocPinY float64
	Angle                                       float64
}

// SetConnectorEnds writes the ends of the 1-D shape at shape: the
// BeginX/Y and EndX/Y cells (V and F, creating a cell that is
// absent), the transform cells when the shape stores them locally
// (V only, F kept, so a (BeginX+EndX)/2 formula stays), the X cell
// of the first Geometry LineTo row (V only, F kept), and the
// Connect rows of the ends whose KeepConnect is false (a desired
// row already there is kept, other rows of that end go, a missing
// desired row is inserted through InsertConnects). A stored value
// that already reads back as the wanted double is left byte for
// byte, so an identity write changes nothing. It reports whether
// anything changed. A missing shape is an ErrNotFound error.
func (w *Workspace) SetConnectorEnds(shape string, ends ConnectorEnds) (bool, error) {
	a, err := vsd.Parse(shape)
	if err != nil {
		return false, fmt.Errorf("vsdx edit: %w", err)
	}
	if a.Kind != vsd.KindShape || a.InMaster {
		return false, fmt.Errorf("vsdx edit: %s: want a page shape address (page0/s5)", shape)
	}
	page := vsd.Address{Kind: vsd.KindPage, Container: a.Container, PageDup: a.PageDup}
	_, part, d, err := w.pageTree(page)
	if err != nil {
		return false, err
	}
	shapeNode := w.shapeIndexFor(part, d.Root()).find(a.Shape, a.Dup)
	if shapeNode == nil {
		return false, fmt.Errorf("%w: %s", ErrNotFound, shape)
	}
	id, ok := shapeID(shapeNode)
	if !ok {
		return false, fmt.Errorf("vsdx edit: %s has no shape ID", shape)
	}
	h := w.core.Target(part, shape, []*tree.Node{shapeNode})
	changed := false
	type endCells struct {
		end  GlueEnd
		x, y string
	}
	for _, ec := range []endCells{
		{ends.Begin, vsd.CellBeginX, vsd.CellBeginY},
		{ends.End, vsd.CellEndX, vsd.CellEndY},
	} {
		if c, err := setGlueCell(h, shapeNode, ec.x, ec.end.X, ec.end); err != nil {
			return false, err
		} else if c {
			changed = true
		}
		if c, err := setGlueCell(h, shapeNode, ec.y, ec.end.Y, ec.end); err != nil {
			return false, err
		} else if c {
			changed = true
		}
	}
	for _, tc := range []struct {
		name  string
		value float64
	}{
		{vsd.CellPinX, ends.PinX}, {vsd.CellPinY, ends.PinY},
		{vsd.CellWidth, ends.Width}, {vsd.CellHeight, ends.Height},
		{vsd.CellLocPinX, ends.LocPinX}, {vsd.CellLocPinY, ends.LocPinY},
		{vsd.CellAngle, ends.Angle},
	} {
		if c, err := setKeptCell(h, shapeNode, tc.name, tc.value); err != nil {
			return false, err
		} else if c {
			changed = true
		}
	}
	if c, err := setLineToWidth(h, shapeNode, ends.Width); err != nil {
		return false, err
	} else if c {
		changed = true
	}
	removals, inserts := glueConnects(d.Root(), id, ends)
	for _, r := range removals {
		if err := w.core.Delete(part, r); err != nil {
			return false, fmt.Errorf("vsdx edit: %w", err)
		}
		changed = true
	}
	if len(inserts) > 0 {
		if err := w.InsertConnects(vsd.PageID(a.Container, a.PageDup), inserts); err != nil {
			return false, err
		}
		changed = true
	}
	return changed, nil
}

// setGlueCell writes one end cell of a shape: V in FormatInches
// digits and F from e (removed when e.Formula is "", kept when
// e.KeepFormula), creating the cell when the shape does not store
// it. It reports whether it changed anything.
func setGlueCell(h *core.Handle, shapeNode *tree.Node, name string, value float64, e GlueEnd) (bool, error) {
	cell := findCell(shapeNode, name)
	if cell == nil {
		attrs := [][2]string{{"N", name}, {"V", vsd.FormatInches(value)}}
		if !e.KeepFormula && e.Formula != "" {
			attrs = append(attrs, [2]string{"F", e.Formula})
		}
		if err := h.AppendEach(core.Path{}, vsdxNS.Name(vsd.ElemCell), attrs); err != nil {
			return false, fmt.Errorf("vsdx edit: cell %s: %w", name, err)
		}
		return true, nil
	}
	return setCellValue(h, cell, value, &e)
}

// setKeptCell writes V of a transform cell when the shape stores it
// locally, keeping its F and U, and reports whether it changed
// anything. A cell the shape does not store is left alone: its
// value comes from the master, whose formula the op did not write.
func setKeptCell(h *core.Handle, shapeNode *tree.Node, name string, value float64) (bool, error) {
	cell := findCell(shapeNode, name)
	if cell == nil {
		return false, nil
	}
	return setCellValue(h, cell, value, nil)
}

// setCellValue writes V of cell (only when the stored V does not
// already read back as value) and, when e is not nil and e does not
// keep the formula, F from e. It reports whether it changed
// anything.
func setCellValue(h *core.Handle, cell *tree.Node, value float64, e *GlueEnd) (bool, error) {
	changed := false
	if cur, _ := cell.Attr("V"); !sameValue(cur, value) {
		if err := h.SetNodeAttr(cell, "V", vsd.FormatInches(value)); err != nil {
			return false, fmt.Errorf("vsdx edit: %w", err)
		}
		changed = true
	}
	if e == nil || e.KeepFormula {
		return changed, nil
	}
	cur, has := cell.Attr("F")
	if (has && cur == e.Formula) || (!has && e.Formula == "") {
		return changed, nil
	}
	if err := setOrRemove(h, cell, "F", e.Formula); err != nil {
		return false, fmt.Errorf("vsdx edit: %w", err)
	}
	return true, nil
}

// sameValue reports whether stored V already reads back as value:
// the K5b rule that a write keeps a stored spelling of the wanted
// double (Visio's 17-digit form included).
func sameValue(stored string, value float64) bool {
	f, ok := vsd.Number(stored)
	return ok && f == value
}

// setLineToWidth writes V (F kept) of the X cell of the shape's
// first Geometry LineTo row, whose formula is Width or Width*1 in
// the corpus's 1-D shapes, and reports whether it changed anything.
func setLineToWidth(h *core.Handle, shapeNode *tree.Node, width float64) (bool, error) {
	section := findChild(shapeNode, vsd.ElemSection, vsd.SectionGeometry)
	if section == nil {
		return false, nil
	}
	for row := range section.ElementsSeq() {
		if !isElem(row, vsd.ElemRow) {
			continue
		}
		if typ, _ := row.Attr("T"); typ != "LineTo" {
			continue
		}
		cell := findCell(row, "X")
		if cell == nil {
			return false, nil
		}
		return setCellValue(h, cell, width, nil)
	}
	return false, nil
}

// glueEndRow is one end's desired glue with the Connect row's
// FromCell and FromPart (9 for BeginX, 12 for EndX).
type glueEndRow struct {
	end     GlueEnd
	cell    string
	fromPar int
}

// glueConnects returns the Connect rows of the page root to delete
// and the fragments to insert for ends: for each end whose
// KeepConnect is false, every row of that end (FromSheet id and
// FromCell BeginX or EndX) except a row already equal to the
// desired glue, which is kept, and the desired row's fragment when
// no kept row exists. Rows of a free end all go.
func glueConnects(root *tree.Node, id uint32, ends ConnectorEnds) (removals []*tree.Node, inserts [][]byte) {
	connects := child(root, vsd.ElemConnects)
	var rows []*tree.Node
	if connects != nil {
		for c := range connects.ElementsSeq() {
			if isElem(c, vsd.ElemConnect) {
				rows = append(rows, c)
			}
		}
	}
	for _, ec := range []glueEndRow{{ends.Begin, vsd.FromCellBegin, 9}, {ends.End, vsd.FromCellEnd, 12}} {
		if ec.end.KeepConnect {
			continue
		}
		kept := false
		for _, r := range rows {
			if !isEndRow(r, id, ec.cell) {
				continue
			}
			if !kept && ec.end.Glue && connectEqual(r, ec) {
				kept = true
				continue
			}
			removals = append(removals, r)
		}
		if ec.end.Glue && !kept {
			inserts = append(inserts, connectFragment(id, ec.cell, ec.fromPar, ec.end))
		}
	}
	return removals, inserts
}

// isEndRow reports whether Connect r is the end row of shape id for
// fromCell (BeginX or EndX).
func isEndRow(r *tree.Node, id uint32, fromCell string) bool {
	fromID, ok := connectID(r, "FromSheet")
	if !ok || fromID != id {
		return false
	}
	cell, _ := r.Attr("FromCell")
	return cell == fromCell
}

// connectEqual reports whether Connect r already is the glue ec
// wants: same end cell, target sheet, target cell and target
// part. (The connector is isEndRow's check, done by the caller.)
func connectEqual(r *tree.Node, ec glueEndRow) bool {
	toID, ok := connectID(r, "ToSheet")
	if !ok || toID != ec.end.Target {
		return false
	}
	toCell, toPart := glueTargetCell(ec.end)
	if cell, _ := r.Attr("ToCell"); cell != toCell {
		return false
	}
	if part, _ := r.Attr("ToPart"); part != strconv.Itoa(toPart) {
		return false
	}
	fromCell, _ := r.Attr("FromCell")
	return fromCell == ec.cell
}

// glueTargetCell returns the ToCell and ToPart of a glued end: PinX
// and 3 for whole-shape glue, Connections.XPoint and
// 100+point-1 for point glue.
func glueTargetCell(e GlueEnd) (string, int) {
	if e.Whole {
		return vsd.ToCellPin, 3
	}
	return vsd.ToCellConnectionPfx + strconv.Itoa(e.Point), 99 + e.Point
}

// connectFragment returns one Connect row for a glued end as XML,
// in the corpus's attribute order (FromSheet, FromCell, FromPart,
// ToSheet, ToCell, ToPart).
func connectFragment(id uint32, fromCell string, fromPart int, e GlueEnd) []byte {
	var b strings.Builder
	b.Grow(128)
	attr := func(name, value string) {
		b.WriteByte(' ')
		b.WriteString(name)
		b.WriteString("='")
		b.WriteString(value)
		b.WriteByte('\'')
	}
	b.WriteString("<Connect")
	attr("FromSheet", strconv.FormatUint(uint64(id), 10))
	attr("FromCell", fromCell)
	attr("FromPart", strconv.Itoa(fromPart))
	attr("ToSheet", strconv.FormatUint(uint64(e.Target), 10))
	toCell, toPart := glueTargetCell(e)
	attr("ToCell", toCell)
	attr("ToPart", strconv.Itoa(toPart))
	b.WriteString("/>")
	return []byte(b.String())
}
