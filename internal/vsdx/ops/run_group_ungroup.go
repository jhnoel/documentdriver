package ops

import (
	"errors"
	"strconv"

	"github.com/jhnoel/documentdriver/internal/cli"
	"github.com/jhnoel/documentdriver/internal/ids"
	"github.com/jhnoel/documentdriver/internal/vsdx/doc"
	"github.com/jhnoel/documentdriver/internal/vsdx/edit"
	"github.com/jhnoel/documentdriver/internal/vsdx/formula"
	"github.com/jhnoel/documentdriver/internal/vsdx/inherit"
	"github.com/jhnoel/documentdriver/internal/vsdx/place"
	"github.com/jhnoel/documentdriver/internal/vsdx/vsd"
)

// Run implements group.ungroup: it moves the children of the group at
// At into the group's container where the group stood, in paint
// order, and removes the group. Each child keeps its place on the
// page: a 2-D child's PinX, PinY, Width, Height, Angle, FlipX and
// FlipY are rewritten from the box read shows (place.Box in the
// group, place.Unplace in the container), a 1-D child's BeginX,
// BeginY, EndX and EndY from its end points; a formula on the group
// (Sheet.<group>!...) is dropped, a constant one replaced, and any
// other kept (an inherited one as F="Inh"). A shape that is not a
// group, a master instance, a group with text or shape data (which
// would be deleted) and a group a Connect row names are refused with
// operation_failed at at. Changed: the children; deleted: the group.
func (s *GroupUngroupSpec) Run(ws *edit.Workspace, aliases map[string]string, _ string, _ ids.Source) (string, []string, []string, string, error) {
	addr, err := ResolveAlias(s.At, aliases, "at")
	if err != nil {
		return "", nil, nil, "", err
	}
	if err := targetRefusal(ws, addr, "at", true); err != nil {
		return "", nil, nil, "", err
	}
	if err := inheritedSubShapeRefusal(ws, addr, "at"); err != nil {
		return "", nil, nil, "", err
	}
	d := ws.Doc()
	if d.Kind == doc.Stencil {
		return "", nil, nil, "", cli.Errorf(cli.CodeOperationFailed, "at", "%s is in a stencil: stencils cannot be edited", addr)
	}
	a, err := vsd.Parse(addr)
	if err != nil {
		return "", nil, nil, "", cli.Errorf(cli.CodeOperationFailed, "at", "%s is not a shape address", addr)
	}
	pageAddr := vsd.PageID(a.Container, a.PageDup)
	r := inherit.New(d)
	if err := scaledRefusal(r, d, a.Container, a.PageDup, pageAddr, "at"); err != nil {
		return "", nil, nil, "", err
	}
	st, err := loadStyleShape(ws, a, addr)
	if err != nil {
		return "", nil, nil, "", err
	}
	if typ, _ := st.node.Attr("Type"); typ != vsd.ShapeTypeGroup {
		return "", nil, nil, "", cli.Errorf(cli.CodeOperationFailed, "at", "%s is not a group", addr)
	}
	if _, ok := st.node.Attr("Master"); ok {
		return "", nil, nil, "", cli.Errorf(cli.CodeOperationFailed, "at", "%s is a master instance: its sub-shapes come from the master", addr)
	}
	if node, _ := st.res.Text(st.shape); node != nil {
		return "", nil, nil, "", cli.Errorf(cli.CodeOperationFailed, "at", "%s has text, which ungrouping would delete: text.set it to \"\" or move it to a child first", addr)
	}
	if len(st.res.Rows(st.shape, vsd.SectionProperty)) > 0 {
		return "", nil, nil, "", cli.Errorf(cli.CodeOperationFailed, "at", "%s has shape data, which ungrouping would delete", addr)
	}
	pg, err := addLoadPage(ws, st.res, a.Container, a.PageDup)
	if err != nil {
		return "", nil, nil, "", err
	}
	// The group and its children are located before they move.
	wanted := map[string]bool{addr: true}
	for g := range st.node.ElementsSeq() {
		if g.Local() != vsd.ElemShapes {
			continue
		}
		for c := range g.ElementsSeq() {
			if c.Local() != vsd.ElemShape {
				continue
			}
			if id, ok := c.Attr("ID"); ok {
				if n, err := strconv.ParseUint(id, 10, 32); err == nil {
					wanted[vsd.ShapeID(pageAddr, uint32(n), 1)] = true
				}
			}
		}
	}
	located := addLocate(pg.root, pageAddr, wanted, st.res)
	gloc, ok := located[addr]
	if !ok || !gloc.ok {
		return "", nil, nil, "", cli.Errorf(cli.CodeOperationFailed, "at", "%s: its transform is not derived, so it cannot be ungrouped", addr)
	}
	outer := gloc.f
	type rewrite struct {
		addr  string
		cells []edit.Cell
	}
	var rewrites []rewrite
	children, err := ws.Ungroup(addr)
	if err != nil {
		switch {
		case errors.Is(err, edit.ErrGlued):
			return "", nil, nil, "", cli.Errorf(cli.CodeOperationFailed, "at", "%s is named by a Connect row (glued to or from); ungrouping would leave that glue pointing at nothing", addr)
		case errors.Is(err, edit.ErrNotFound):
			return "", nil, nil, "", cli.Errorf(cli.CodeNotFound, "at", "%s", err)
		}
		return "", nil, nil, "", moveEditError(err)
	}
	for _, ch := range children {
		loc, ok := located[ch]
		if !ok || !loc.ok {
			return "", nil, nil, "", cli.Errorf(cli.CodeOperationFailed, "at", "%s: the transform of its child %s is not derived", addr, ch)
		}
		ca, err := vsd.Parse(ch)
		if err != nil {
			return "", nil, nil, "", cli.Errorf(cli.CodeInternal, "", "%v", err)
		}
		cst, err := loadShapeSheet(ws, ca, ch)
		if err != nil {
			return "", nil, nil, "", err
		}
		cells, err := ungroupCells(cst, loc, outer, pg.height, a.Shape)
		if err != nil {
			return "", nil, nil, "", cli.Errorf(cli.CodeOperationFailed, "at", "%s: child %s: %s", addr, ch, err)
		}
		rewrites = append(rewrites, rewrite{ch, cells})
	}
	for _, rw := range rewrites {
		if err := ws.SetCells(rw.addr, rw.cells); err != nil {
			return "", nil, nil, "", moveEditError(err)
		}
	}
	return "", children, []string{addr}, "", nil
}

// ungroupCells returns the place cells of the child cst (located at
// loc inside a group of stored ID gid) for the same place in the
// group's container frame outer, as Run describes.
func ungroupCells(cst *styleShape, loc addLocated, outer place.Frame, pageH float64, gid uint32) ([]edit.Cell, error) {
	var cells []edit.Cell
	put := func(name string, v float64) {
		c := edit.Cell{N: name, V: vsd.FormatInches(v)}
		f, origin := textCellFormula(cst, name)
		switch _, _, onGroup := groupScaleFormula(f, gid); {
		case onGroup, f == "", formula.Allowed(f):
		case origin == inherit.Local:
			c.F = f
		default:
			c.F = vsd.FormulaInherited
		}
		if u, ok := storedUnit(cst, name); ok {
			c.U = u
		}
		cells = append(cells, c)
	}
	if bx, ok := styleNumberCell(cst, vsd.CellBeginX); ok {
		by, _ := styleNumberCell(cst, vsd.CellBeginY)
		ex, _ := styleNumberCell(cst, vsd.CellEndX)
		ey, _ := styleNumberCell(cst, vsd.CellEndY)
		bv := place.Point(loc.f, bx, by, pageH)
		ev := place.Point(loc.f, ex, ey, pageH)
		nbx, nby, ok1 := addViewToParent(outer, bv, pageH)
		nex, ney, ok2 := addViewToParent(outer, ev, pageH)
		if !ok1 || !ok2 {
			return nil, errors.New("the group's container frame has no inverse")
		}
		put(vsd.CellBeginX, nbx)
		put(vsd.CellBeginY, nby)
		put(vsd.CellEndX, nex)
		put(vsd.CellEndY, ney)
		// The pin follows the ends.
		pin := place.Point(loc.f, loc.t.PinX, loc.t.PinY, pageH)
		px, py, _ := addViewToParent(outer, pin, pageH)
		put(vsd.CellPinX, px)
		put(vsd.CellPinY, py)
		return cells, nil
	}
	box, err := place.Box(loc.f, loc.t, pageH)
	if err != nil {
		return nil, err
	}
	// Unplace takes the LocPin as a fraction of the box.
	lx, ly := 0.5, 0.5
	if loc.t.Width != 0 {
		lx = loc.t.LocPinX / loc.t.Width
	}
	if loc.t.Height != 0 {
		ly = loc.t.LocPinY / loc.t.Height
	}
	t, err := place.Unplace(outer, box, pageH, lx, ly)
	if err != nil {
		return nil, err
	}
	put(vsd.CellPinX, t.PinX)
	put(vsd.CellPinY, t.PinY)
	put(vsd.CellWidth, t.Width)
	put(vsd.CellHeight, t.Height)
	put(vsd.CellAngle, t.Angle)
	flip := func(name string, on bool) {
		v := "0"
		if on {
			v = "1"
		}
		if cur, ok := styleStoredCell(cst, name); ok && cur == v {
			return
		}
		cells = append(cells, edit.Cell{N: name, V: v})
	}
	flip(vsd.CellFlipX, t.FlipX)
	flip(vsd.CellFlipY, t.FlipY)
	return cells, nil
}
