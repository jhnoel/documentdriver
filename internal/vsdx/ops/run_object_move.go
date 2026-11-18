package ops

import (
	"errors"
	"math"
	"strings"

	"github.com/jhnoel/documentdriver/internal/cli"
	"github.com/jhnoel/documentdriver/internal/geom"
	"github.com/jhnoel/documentdriver/internal/ids"
	"github.com/jhnoel/documentdriver/internal/ooxml/tree"
	"github.com/jhnoel/documentdriver/internal/vsdx/doc"
	"github.com/jhnoel/documentdriver/internal/vsdx/edit"
	"github.com/jhnoel/documentdriver/internal/vsdx/formula"
	"github.com/jhnoel/documentdriver/internal/vsdx/inherit"
	"github.com/jhnoel/documentdriver/internal/vsdx/place"
	"github.com/jhnoel/documentdriver/internal/vsdx/sheet"
	"github.com/jhnoel/documentdriver/internal/vsdx/vsd"
)

// Run applies object.move: it resolves at through aliases, refuses
// stencils, converts dx, dy from the plan's units (view points, y
// down) to the parent's inches through the frame's inverse, and moves
// the shape. A 2-D shape moves by PinX and PinY; a 1-D shape (chunk
// CC2) moves its free ends by the delta (a glued end keeps its glue
// and position; both ends glued is operation_failed), re-evaluates
// its transform cells over the closed evaluator and rewrites
// Geometry1 as for a free end. A group moves as one: only its own
// cells change, its children's cells are untouched. A cell outside
// the decision (c) allow list is refused with operation_failed
// naming the cell and its formula, except a 1-D derived cell whose
// formula is CC1-evaluable. A dynamic connector move reports
// stale-values "<address> route"; connectors glued to the moved
// shape are reported as the stale detail, so apply asks Visio to
// recalculate. A zero move, and any move whose stored cells already
// show the new values, writes nothing.
func (s *ObjectMoveSpec) Run(ws *edit.Workspace, aliases map[string]string, units string, _ ids.Source) (string, []string, []string, string, error) {
	shapeAddr, err := resolveShape(s.At, aliases)
	if err != nil {
		return "", nil, nil, "", err
	}
	if math.IsNaN(s.DX) || math.IsInf(s.DX, 0) {
		return "", nil, nil, "", cli.Errorf(cli.CodeInvalidOperation, "dx", "dx must hold finite numbers")
	}
	if math.IsNaN(s.DY) || math.IsInf(s.DY, 0) {
		return "", nil, nil, "", cli.Errorf(cli.CodeInvalidOperation, "dy", "dy must hold finite numbers")
	}
	scale := 1.0
	if units == "in" {
		scale = vsd.PointsPerInch
	}
	dxPt, dyPt := s.DX*scale, s.DY*scale
	d, err := doc.Open(ws.Package())
	if err != nil {
		return "", nil, nil, "", cli.Errorf(cli.CodeInternal, "", "%v", err)
	}
	if d.Kind == doc.Stencil {
		return "", nil, nil, "", cli.Errorf(cli.CodeOperationFailed, "at", "%s is in a stencil: stencils cannot be edited", shapeAddr)
	}
	a, err := vsd.Parse(shapeAddr)
	if err != nil {
		return "", nil, nil, "", cli.Errorf(cli.CodeNotFound, "at", "%s", err)
	}
	r := inherit.New(d)
	pageAddr := vsd.PageID(a.Container, a.PageDup)
	pg, err := addLoadPage(ws, r, a.Container, a.PageDup)
	if err != nil {
		return "", nil, nil, "", err
	}
	located := addLocate(pg.root, pageAddr, map[string]bool{shapeAddr: true}, r)
	loc, ok := located[shapeAddr]
	if !ok {
		return "", nil, nil, "", cli.Errorf(cli.CodeNotFound, "at", "no shape %s: not found", shapeAddr)
	}
	if dxPt == 0 && dyPt == 0 {
		return "", nil, nil, "", nil
	}
	if !loc.ok {
		return "", nil, nil, "", cli.Errorf(cli.CodeOperationFailed, "at", "%s has no readable transform", shapeAddr)
	}
	node := findTextShape(pg.root, a.Shape, a.Dup)
	if node == nil {
		return "", nil, nil, "", cli.Errorf(cli.CodeNotFound, "at", "no shape %s: not found", shapeAddr)
	}
	if err := nodeRefusal(r, node, shapeAddr, "at", true); err != nil {
		return "", nil, nil, "", err
	}
	if err := scaledRefusal(r, d, a.Container, a.PageDup, pageAddr, "at"); err != nil {
		return "", nil, nil, "", err
	}
	if !moveAncestorsOK(r, node) {
		return "", nil, nil, "", cli.Errorf(cli.CodeOperationFailed, "at", "%s has no readable transform", shapeAddr)
	}
	h := textShapeOf(node)
	x0, y0, ok := addViewToParent(loc.f, geom.Pt{}, pg.height)
	if !ok {
		return "", nil, nil, "", cli.Errorf(cli.CodeOperationFailed, "at", "%s cannot be placed", shapeAddr)
	}
	x1, y1, ok := addViewToParent(loc.f, geom.Pt{X: dxPt, Y: dyPt}, pg.height)
	if !ok {
		return "", nil, nil, "", cli.Errorf(cli.CodeOperationFailed, "at", "%s cannot be placed", shapeAddr)
	}
	deltaX, deltaY := x1-x0, y1-y0
	if _, _, has := r.Cell(h, sheet.ShapeCell(vsd.CellBeginX)); has {
		changed, stale, err := moveConnector(ws, r, d, h, node, a, pg, loc, deltaX, deltaY, shapeAddr)
		if err != nil {
			return "", nil, nil, "", err
		}
		if !changed {
			return "", nil, nil, "", nil
		}
		return "", []string{shapeAddr}, nil, stale, nil
	}
	wants, err := moveWants(r, h, loc.t, deltaX, deltaY, shapeAddr)
	if err != nil {
		return "", nil, nil, "", err
	}
	var cells []edit.Cell
	for _, want := range wants {
		if math.IsNaN(want.value) || math.IsInf(want.value, 0) {
			return "", nil, nil, "", cli.Errorf(cli.CodeOperationFailed, "at", "%s cannot be placed", shapeAddr)
		}
		formatted := vsd.FormatInches(want.value)
		if want.present {
			old, numOK := vsd.Number(want.cell.V)
			if numOK && vsd.FormatInches(old) == formatted {
				continue
			}
		}
		if err := moveRefusal(want); err != nil {
			return "", nil, nil, "", err
		}
		cells = append(cells, edit.Cell{N: want.name, V: formatted})
	}
	if len(cells) == 0 {
		return "", nil, nil, "", nil
	}
	staleEnds, err := ws.MoveShape(shapeAddr, cells)
	if err != nil {
		return "", nil, nil, "", moveEditError(err)
	}
	stale := ""
	if len(staleEnds) > 0 {
		stale = strings.Join(staleEnds, ", ") + " left stale (stale-connector)"
	}
	return "", []string{shapeAddr}, nil, stale, nil
}

// moveConnector moves the 1-D shape node: its free ends move by
// deltaX, deltaY (parent inches) while a glued end keeps its glue
// and position (both ends glued is operation_failed at at). The
// transform cells are re-evaluated and Geometry1 rewritten as
// for a free end; a dynamic move reports route stale.
func moveConnector(ws *edit.Workspace, r *inherit.Resolver, d *doc.Document, h *inherit.Shape, node *tree.Node, a vsd.Address, pg *addPage, _ addLocated, deltaX, deltaY float64, shapeAddr string) (bool, string, error) {
	if err := checkConnectorEditable(r, h, shapeAddr); err != nil {
		return false, "", err
	}
	cur := endsCurrent{r: r, h: h}
	beginX, ok := cur.number(vsd.CellBeginX)
	if !ok {
		return false, "", endsCellRefusal(shapeAddr, vsd.CellBeginX)
	}
	beginY, ok := cur.number(vsd.CellBeginY)
	if !ok {
		return false, "", endsCellRefusal(shapeAddr, vsd.CellBeginY)
	}
	endX, ok := cur.number(vsd.CellEndX)
	if !ok {
		return false, "", endsCellRefusal(shapeAddr, vsd.CellEndX)
	}
	endY, ok := cur.number(vsd.CellEndY)
	if !ok {
		return false, "", endsCellRefusal(shapeAddr, vsd.CellEndY)
	}
	beginGlued, endGlued := isGluedEnds(pg.root, a.Shape, r, h)
	if beginGlued && endGlued {
		return false, "", cli.Errorf(cli.CodeOperationFailed, "at", "%s has both ends glued: set from_glue or to_glue to none first", shapeAddr)
	}
	newBeginX, newBeginY := beginX, beginY
	newEndX, newEndY := endX, endY
	if !beginGlued {
		newBeginX, newBeginY = beginX+deltaX, beginY+deltaY
	}
	if !endGlued {
		newEndX, newEndY = endX+deltaX, endY+deltaY
	}
	if math.IsNaN(newBeginX) || math.IsInf(newBeginX, 0) || math.IsNaN(newEndX) || math.IsInf(newEndX, 0) {
		return false, "", cli.Errorf(cli.CodeOperationFailed, "at", "%s cannot be placed", shapeAddr)
	}
	beginEnd := edit.GlueEnd{X: newBeginX, Y: newBeginY}
	endEnd := edit.GlueEnd{X: newEndX, Y: newEndY}
	if beginGlued {
		beginEnd.KeepFormula, beginEnd.KeepConnect = true, true
	}
	if endGlued {
		endEnd.KeepFormula, endEnd.KeepConnect = true, true
	}
	if !beginGlued {
		if err := checkEndFormulas(beginEnd, cur, vsd.CellBeginX, vsd.CellBeginY); err != nil {
			return false, "", err
		}
	}
	if !endGlued {
		if err := checkEndFormulas(endEnd, cur, vsd.CellEndX, vsd.CellEndY); err != nil {
			return false, "", err
		}
	}
	tr, err := evalTransform(r, h, shapeAddr, newBeginX, newBeginY, newEndX, newEndY)
	if err != nil {
		return false, "", err
	}
	changed, err := ws.SetConnectorEnds(shapeAddr, edit.ConnectorEnds{
		Begin: beginEnd, End: endEnd,
		PinX: tr.pinX, PinY: tr.pinY, Width: tr.width, Height: tr.height,
		LocPinX: tr.locPinX, LocPinY: tr.locPinY, Angle: tr.angle,
	})
	if err != nil {
		return false, "", moveEditError(err)
	}
	part, err := connectorPagePart(ws, d, a)
	if err != nil {
		return false, "", err
	}
	if c, err := materialiseTransform(ws, r, h, shapeAddr, tr); err != nil {
		return false, "", err
	} else if c {
		changed = true
	}
	if c, err := writeGeometry1(ws, part, shapeAddr, node, isLineTransform(r, h), tr, newBeginY, newEndY); err != nil {
		return false, "", err
	} else if c {
		changed = true
	}
	if !changed {
		return false, "", nil
	}
	stale := ""
	if newBeginX != beginX || newBeginY != beginY || newEndX != endX || newEndY != endY {
		if isDynamicConnector(r, h) {
			stale = shapeAddr + " route"
		} else if s := endsStale(node, shapeAddr); s != "" {
			stale = s
		}
	}
	return true, stale, nil
}

// moveAncestorsOK reports whether every group ancestor of node has a
// readable effective transform, so the frame addLocate gave the
// shape is the frame of its real parent chain.
func moveAncestorsOK(r *inherit.Resolver, node *tree.Node) bool {
	for p := node.Parent(); p != nil; p = p.Parent() {
		if p.Kind() != tree.ElementNode || p.Local() != vsd.ElemShape || !vsd.IsMain(p.Space()) {
			continue
		}
		if _, ok := addTransform(r, textShapeOf(p)); !ok {
			return false
		}
	}
	return true
}

// moveWant is one cell a move would write: its name, its new value in
// parent inches, and the effective cell it would replace.
type moveWant struct {
	name    string
	value   float64
	cell    sheet.Cell
	present bool
}

// moveWants returns the cells a move by deltaX, deltaY (parent
// inches) would write, in check order: for a 1-D shape the ends then
// the derived transform cells, for a 2-D shape the pin.
func moveWants(r *inherit.Resolver, h *inherit.Shape, t place.Transform, deltaX, deltaY float64, shapeAddr string) ([]moveWant, error) {
	beginX, beginXCell, beginXOK, beginXPresent := moveNumber(r, h, vsd.CellBeginX)
	beginY, beginYCell, beginYOK, beginYPresent := moveNumber(r, h, vsd.CellBeginY)
	endX, endXCell, endXOK, endXPresent := moveNumber(r, h, vsd.CellEndX)
	endY, endYCell, endYOK, endYPresent := moveNumber(r, h, vsd.CellEndY)
	if beginXPresent || beginYPresent || endXPresent || endYPresent {
		if !beginXOK || !beginYOK || !endXOK || !endYOK {
			return nil, cli.Errorf(cli.CodeOperationFailed, "at", "%s has no readable ends", shapeAddr)
		}
		newBeginX, newBeginY := beginX+deltaX, beginY+deltaY
		newEndX, newEndY := endX+deltaX, endY+deltaY
		pinX, pinY, width, angle, oneDOK := formula.OneD(newBeginX, newBeginY, newEndX, newEndY)
		if !oneDOK {
			return nil, cli.Errorf(cli.CodeOperationFailed, "at", "%s cannot be placed: its ends coincide", shapeAddr)
		}
		return []moveWant{
			{name: vsd.CellBeginX, value: newBeginX, cell: beginXCell, present: true},
			{name: vsd.CellBeginY, value: newBeginY, cell: beginYCell, present: true},
			{name: vsd.CellEndX, value: newEndX, cell: endXCell, present: true},
			{name: vsd.CellEndY, value: newEndY, cell: endYCell, present: true},
			moveWantOf(r, h, vsd.CellPinX, pinX),
			moveWantOf(r, h, vsd.CellPinY, pinY),
			moveWantOf(r, h, vsd.CellWidth, width),
			moveWantOf(r, h, vsd.CellAngle, angle),
		}, nil
	}
	return []moveWant{
		moveWantOf(r, h, vsd.CellPinX, t.PinX+deltaX),
		moveWantOf(r, h, vsd.CellPinY, t.PinY+deltaY),
	}, nil
}

// moveNumber returns the effective number of one shape-level cell,
// the cell itself, and whether it is present and a number.
func moveNumber(r *inherit.Resolver, h *inherit.Shape, name string) (float64, sheet.Cell, bool, bool) {
	cell, _, ok := r.Cell(h, sheet.ShapeCell(name))
	if !ok {
		return 0, sheet.Cell{}, false, false
	}
	v, numOK := vsd.Number(cell.V)
	return v, cell, numOK, true
}

// moveWantOf returns the want to write name with value, with the
// effective cell of name.
func moveWantOf(r *inherit.Resolver, h *inherit.Shape, name string, value float64) moveWant {
	_, cell, _, present := moveNumber(r, h, name)
	return moveWant{name: name, value: value, cell: cell, present: present}
}

// moveRefusal reports whether writing want's cell is outside the
// allow list: its effective formula must be on the decision (c) list
// (formula.Allowed), or, for a derived 1-D transform cell, the
// formula formula.OneD derives it from. The refusal names the cell
// and its formula.
func moveRefusal(want moveWant) error {
	if !want.present {
		return nil
	}
	f := want.cell.F
	if formula.Allowed(f) {
		return nil
	}
	switch want.name {
	case vsd.CellPinX:
		if f == "(BeginX+EndX)/2" {
			return nil
		}
	case vsd.CellPinY:
		if f == "(BeginY+EndY)/2" {
			return nil
		}
	case vsd.CellWidth:
		if f == "SQRT((EndX-BeginX)^2+(EndY-BeginY)^2)" {
			return nil
		}
	case vsd.CellAngle:
		if f == "ATAN2(EndY-BeginY,EndX-BeginX)" {
			return nil
		}
	}
	return cli.Errorf(cli.CodeOperationFailed, "", "%s formula %q is outside the allow list", want.name, f)
}

// moveEditError maps an edit.MoveShape error to the op's error: a
// not-found or unreadable error stays as it is, anything else is
// internal.
func moveEditError(err error) error {
	var un *edit.Unreadable
	if errors.Is(err, edit.ErrNotFound) || errors.As(err, &un) {
		return err
	}
	return cli.Errorf(cli.CodeInternal, "", "%v", err)
}
