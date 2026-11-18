package ops

import (
	"errors"
	"math"
	"strconv"
	"strings"

	"github.com/jhnoel/documentdriver/internal/cli"
	"github.com/jhnoel/documentdriver/internal/geom"
	"github.com/jhnoel/documentdriver/internal/ooxml/tree"
	"github.com/jhnoel/documentdriver/internal/vsdx/doc"
	"github.com/jhnoel/documentdriver/internal/vsdx/edit"
	"github.com/jhnoel/documentdriver/internal/vsdx/formula"
	"github.com/jhnoel/documentdriver/internal/vsdx/inherit"
	"github.com/jhnoel/documentdriver/internal/vsdx/place"
	"github.com/jhnoel/documentdriver/internal/vsdx/sheet"
	"github.com/jhnoel/documentdriver/internal/vsdx/vsd"
)

// setEnds applies the from, to, from_glue and to_glue fields of an
// object.set to a 1-D shape (chunk OSG; run_object_set.go calls it
// for the fields given; chunk CC2 adds Visio-authored connectors).
// A free end (from/to) is placed at the view point and freed: its
// Connect row and end formula go, its V becomes the point, and
// its trigger cell is freed as for "none". A point glue
// (from_glue/to_glue "s2:2") writes the create form
// (PAR(PNT(Sheet.N!Connections.Xk, ...)) on the end's cells and a
// Connect row) and moves the end to the site, with the trigger
// _XFTRIGGER(Sheet.N!EventXFMod) V 2. Whole-shape glue ("s2")
// writes the _WALKGLUE form and a ToCell PinX ToPart 3 Connect
// row when the connector is from a master with Visio's dynamic
// glue cells, the cached end at the midpoint of the target's box
// edge facing the other end; otherwise operation_failed at the
// glue field. "none" frees the end and keeps its V. The transform
// cells are re-evaluated over the closed evaluator (decision c-R):
// every cell whose formula is CC1-evaluable gets its new V with
// F kept (Inh materialised with F="Inh"), and Geometry1 is
// rewritten as the straight two-point line. A dynamic free-end
// write reports stale-values "<address> route". Changed is the
// shape's address, or nothing for an identity write. Stale names
// a dependent the write did not rewrite ("" when there is none),
// for apply's stale-values report.
func setEnds(ws *edit.Workspace, s *ObjectSetSpec, aliases map[string]string, units string) ([]string, string, error) {
	if s.From == nil && s.To == nil && s.FromGlue == nil && s.ToGlue == nil {
		return nil, "", nil
	}
	shapeAddr, err := resolveShape(s.At, aliases)
	if err != nil {
		return nil, "", err
	}
	a, err := vsd.Parse(shapeAddr)
	if err != nil {
		return nil, "", cli.Errorf(cli.CodeNotFound, "at", "%s", err)
	}
	d, err := doc.Open(ws.Package())
	if err != nil {
		return nil, "", err
	}
	if d.Kind == doc.Stencil {
		return nil, "", cli.Errorf(cli.CodeOperationFailed, "at", "%s is in a stencil: stencils cannot be edited", shapeAddr)
	}
	if s.From != nil && s.FromGlue != nil {
		return nil, "", cli.Errorf(cli.CodeOperationFailed, "from_glue", "connector takes from or from_glue, not both")
	}
	if s.To != nil && s.ToGlue != nil {
		return nil, "", cli.Errorf(cli.CodeOperationFailed, "to_glue", "connector takes to or to_glue, not both")
	}
	if s.From != nil && !addFinite(s.From) {
		return nil, "", cli.Errorf(cli.CodeOperationFailed, "from", "from must hold finite numbers")
	}
	if s.To != nil && !addFinite(s.To) {
		return nil, "", cli.Errorf(cli.CodeOperationFailed, "to", "to must hold finite numbers")
	}
	pageAddr := vsd.PageID(a.Container, a.PageDup)
	beginReq, err := parseEndRequest(s.From, s.FromGlue, aliases, pageAddr, a.Container, a.PageDup, "from_glue")
	if err != nil {
		return nil, "", err
	}
	endReq, err := parseEndRequest(s.To, s.ToGlue, aliases, pageAddr, a.Container, a.PageDup, "to_glue")
	if err != nil {
		return nil, "", err
	}
	r := inherit.New(d)
	pg, err := addLoadPage(ws, r, a.Container, a.PageDup)
	if err != nil {
		return nil, "", err
	}
	wanted := map[string]bool{shapeAddr: true}
	for _, req := range []endRequest{beginReq, endReq} {
		if req.present && !req.freePoint && !req.freeGlue {
			wanted[req.glue.target] = true
		}
	}
	located := addLocate(pg.root, pageAddr, wanted, r)
	loc, ok := located[shapeAddr]
	if !ok {
		return nil, "", cli.Errorf(cli.CodeNotFound, "at", "no shape %s: not found", shapeAddr)
	}
	if !loc.ok {
		return nil, "", cli.Errorf(cli.CodeOperationFailed, "at", "%s has no readable transform", shapeAddr)
	}
	node := findEndsNode(pg.root, a)
	if node == nil {
		return nil, "", cli.Errorf(cli.CodeNotFound, "at", "no shape %s: not found", shapeAddr)
	}
	if err := nodeRefusal(r, node, shapeAddr, "at", false); err != nil {
		return nil, "", err
	}
	if s.From != nil || s.To != nil {
		field := "from"
		if s.From == nil {
			field = "to"
		}
		if err := scaledRefusal(r, d, a.Container, a.PageDup, pageAddr, field); err != nil {
			return nil, "", err
		}
	}
	h := addHead(node, 0, false)
	if _, _, has := r.Cell(h, sheet.ShapeCell(vsd.CellBeginX)); !has {
		return nil, "", cli.Errorf(cli.CodeOperationFailed, "at", "%s is not a 1-D shape: from/to and glue apply to a connector", shapeAddr)
	}
	cur := endsCurrent{r: r, h: h}
	beginX, ok := cur.number(vsd.CellBeginX)
	if !ok {
		return nil, "", endsCellRefusal(shapeAddr, vsd.CellBeginX)
	}
	beginY, ok := cur.number(vsd.CellBeginY)
	if !ok {
		return nil, "", endsCellRefusal(shapeAddr, vsd.CellBeginY)
	}
	endX, ok := cur.number(vsd.CellEndX)
	if !ok {
		return nil, "", endsCellRefusal(shapeAddr, vsd.CellEndX)
	}
	endY, ok := cur.number(vsd.CellEndY)
	if !ok {
		return nil, "", endsCellRefusal(shapeAddr, vsd.CellEndY)
	}
	scale := 1.0
	if units == "in" {
		scale = vsd.PointsPerInch
	}
	curBeginView := place.Point(loc.f, beginX, beginY, pg.height)
	curEndView := place.Point(loc.f, endX, endY, pg.height)
	// A glue equal to the end's current glue writes nothing
	// (Fable identity): it is treated as unchanged, so its
	// cached value (and the position's round-trip) is kept.
	// The check needs the Connect equality and the end
	// formulas already in the wanted form: a Connector with
	// the row but no formula (V5 stores none) is a real
	// write, not an identity.
	if beginReq.present && !beginReq.freePoint && !beginReq.freeGlue {
		if target, point, whole, present := currentGlue(pg.root, a.Shape, true); present && target == beginReq.glue.id && whole == beginReq.isWhole() && (whole || point == beginReq.glue.point) {
			wantFormula := glueFormula(beginReq.glue.id, beginReq.glue.point)
			if beginReq.isWhole() {
				wantFormula = walkFormula(true)
			}
			xf, _ := cur.formula(vsd.CellBeginX)
			yf, _ := cur.formula(vsd.CellBeginY)
			if xf == wantFormula && yf == wantFormula {
				beginReq = endRequest{}
			}
		}
	}
	if endReq.present && !endReq.freePoint && !endReq.freeGlue {
		if target, point, whole, present := currentGlue(pg.root, a.Shape, false); present && target == endReq.glue.id && whole == endReq.isWhole() && (whole || point == endReq.glue.point) {
			wantFormula := glueFormula(endReq.glue.id, endReq.glue.point)
			if endReq.isWhole() {
				wantFormula = walkFormula(false)
			}
			xf, _ := cur.formula(vsd.CellEndX)
			yf, _ := cur.formula(vsd.CellEndY)
			if xf == wantFormula && yf == wantFormula {
				endReq = endRequest{}
			}
		}
	}
	beginView, endView := curBeginView, curEndView
	if beginReq.freePoint {
		beginView = geom.Pt{X: s.From[0] * scale, Y: s.From[1] * scale}
	} else if beginReq.glue.hasPoint {
		beginView, err = addEndView(nil, true, beginReq.glue, located, "from", "from_glue", pg.height, scale)
		if err != nil {
			return nil, "", err
		}
	}
	if endReq.freePoint {
		endView = geom.Pt{X: s.To[0] * scale, Y: s.To[1] * scale}
	} else if endReq.glue.hasPoint {
		endView, err = addEndView(nil, true, endReq.glue, located, "to", "to_glue", pg.height, scale)
		if err != nil {
			return nil, "", err
		}
	}
	if beginReq.isWhole() {
		if !hasDynamicMaster(r, h) {
			return nil, "", wholeGlueRefusal(shapeAddr, "from_glue")
		}
		beginView, err = wholeEndView(beginReq.glue, located, endView, "from_glue", pg.height)
		if err != nil {
			return nil, "", err
		}
	}
	if endReq.isWhole() {
		if !hasDynamicMaster(r, h) {
			return nil, "", wholeGlueRefusal(shapeAddr, "to_glue")
		}
		endView, err = wholeEndView(endReq.glue, located, beginView, "to_glue", pg.height)
		if err != nil {
			return nil, "", err
		}
	}
	if err := checkConnectorEditable(r, h, shapeAddr); err != nil {
		return nil, "", err
	}
	beginEnd, err := desiredEnd(beginReq, cur, loc, beginView, pg.height, vsd.CellBeginX, vsd.CellBeginY, "from", true)
	if err != nil {
		return nil, "", err
	}
	endEnd, err := desiredEnd(endReq, cur, loc, endView, pg.height, vsd.CellEndX, vsd.CellEndY, "to", false)
	if err != nil {
		return nil, "", err
	}
	tr, err := evalTransform(r, h, shapeAddr, beginEnd.X, beginEnd.Y, endEnd.X, endEnd.Y)
	if err != nil {
		return nil, "", err
	}
	if beginEnd.X == beginX && beginEnd.Y == beginY && endEnd.X == endX && endEnd.Y == endY {
		// Ends unchanged (for example "none" with its V
		// kept): the cached transform and Geometry1 stay
		// byte for byte, so re-evaluation drift must not
		// write. The current transform is the stored one.
		tr = endOfConnector{pinX: loc.t.PinX, pinY: loc.t.PinY, width: loc.t.Width, height: loc.t.Height, locPinX: loc.t.LocPinX, locPinY: loc.t.LocPinY, angle: loc.t.Angle, present: true}
	}
	if math.IsNaN(tr.width) || math.IsInf(tr.width, 0) {
		return nil, "", cli.Errorf(cli.CodeOperationFailed, "to", "the connector ends cannot form a 1-D transform")
	}
	desired := edit.ConnectorEnds{
		Begin: beginEnd, End: endEnd,
		PinX: tr.pinX, PinY: tr.pinY, Width: tr.width, Height: tr.height,
		LocPinX: tr.locPinX, LocPinY: tr.locPinY, Angle: tr.angle,
	}
	if err := checkEndFormulas(beginEnd, cur, vsd.CellBeginX, vsd.CellBeginY); err != nil {
		return nil, "", err
	}
	if err := checkEndFormulas(endEnd, cur, vsd.CellEndX, vsd.CellEndY); err != nil {
		return nil, "", err
	}
	changed, err := ws.SetConnectorEnds(shapeAddr, desired)
	if err != nil {
		if errors.Is(err, edit.ErrNotFound) {
			return nil, "", cli.Errorf(cli.CodeNotFound, "at", "%s", err)
		}
		var un *edit.Unreadable
		if errors.As(err, &un) {
			return nil, "", err
		}
		return nil, "", cli.Errorf(cli.CodeInternal, "", "%v", err)
	}
	part, err := connectorPagePart(ws, d, a)
	if err != nil {
		return nil, "", err
	}
	if c, err := materialiseTransform(ws, r, h, shapeAddr, tr); err != nil {
		return nil, "", err
	} else if c {
		changed = true
	}
	if beginReq.present {
		if beginReq.freePoint || beginReq.freeGlue {
			if c, err := freeTrigger(ws, part, r, h, shapeAddr, cellBegTrigger, node); err != nil {
				return nil, "", err
			} else if c {
				changed = true
			}
		} else if c, err := writeTriggerGlue(ws, r, h, shapeAddr, cellBegTrigger, beginReq.glue.id); err != nil {
			return nil, "", err
		} else if c {
			changed = true
		}
	}
	if endReq.present {
		if endReq.freePoint || endReq.freeGlue {
			if c, err := freeTrigger(ws, part, r, h, shapeAddr, cellEndTrigger, node); err != nil {
				return nil, "", err
			} else if c {
				changed = true
			}
		} else if c, err := writeTriggerGlue(ws, r, h, shapeAddr, cellEndTrigger, endReq.glue.id); err != nil {
			return nil, "", err
		} else if c {
			changed = true
		}
	}
	if beginEnd.X != beginX || beginEnd.Y != beginY || endEnd.X != endX || endEnd.Y != endY {
		if c, err := writeGeometry1(ws, part, shapeAddr, node, isLineTransform(r, h), tr, beginEnd.Y, endEnd.Y); err != nil {
			return nil, "", err
		} else if c {
			changed = true
		}
	}
	if !changed {
		return nil, "", nil
	}
	stale := ""
	if beginEnd.X != beginX || beginEnd.Y != beginY || endEnd.X != endX || endEnd.Y != endY {
		if isDynamicConnector(r, h) {
			stale = shapeAddr + " route"
		} else if s := endsStale(node, shapeAddr); s != "" {
			stale = s
		}
	}
	return []string{shapeAddr}, stale, nil
}

// wholeEndView returns the cached whole-glue end of one request
// as a view point: the midpoint of the target's box edge facing
// other. A missing target is not_found at the glue field, an
// unreadable target operation_failed there.
func wholeEndView(g addGlue, located map[string]addLocated, other geom.Pt, field string, height float64) (geom.Pt, error) {
	loc, ok := located[g.target]
	if !ok {
		return geom.Pt{}, cli.Errorf(cli.CodeNotFound, field, "no shape %s: not found", g.target)
	}
	if !loc.ok {
		return geom.Pt{}, cli.Errorf(cli.CodeOperationFailed, field, "%s has no readable transform", g.target)
	}
	return wholeEdgeView(loc, other, height), nil
}

// materialiseTransform materialises every transform cell the
// shape does not store locally (its effective origin is the
// master) whose re-evaluated value differs from the effective
// value, as a local F="Inh" cell. It reports whether it
// changed anything.
func materialiseTransform(ws *edit.Workspace, r *inherit.Resolver, h *inherit.Shape, shapeAddr string, tr endOfConnector) (bool, error) {
	changed := false
	for _, w := range []struct {
		name  string
		value float64
	}{
		{vsd.CellPinX, tr.pinX}, {vsd.CellPinY, tr.pinY},
		{vsd.CellWidth, tr.width}, {vsd.CellHeight, tr.height},
		{vsd.CellLocPinX, tr.locPinX}, {vsd.CellLocPinY, tr.locPinY},
		{vsd.CellAngle, tr.angle},
	} {
		cell, origin, ok := r.Cell(h, sheet.ShapeCell(w.name))
		if !ok || origin != inherit.Master {
			continue
		}
		cur, _ := vsd.Number(cell.V)
		formatted := vsd.FormatInches(w.value)
		if vsd.FormatInches(cur) == formatted {
			continue
		}
		if err := ws.SetCells(shapeAddr, []edit.Cell{{N: w.name, V: formatted, F: vsd.FormulaInherited}}); err != nil {
			return false, err
		}
		changed = true
	}
	return changed, nil
}

// endRequest is one end's setEnds input: a free point, a glue to a
// shape (point when hasPoint, whole otherwise), or "none".
type endRequest struct {
	freePoint bool
	freeGlue  bool
	glue      addGlue
	present   bool
}

// isWhole reports whether the request is whole-shape glue: a glue
// with no connection point.
func (req endRequest) isWhole() bool {
	return req.present && !req.freePoint && !req.freeGlue && !req.glue.hasPoint
}

// parseEndRequest parses one end's from/from_glue pair into an
// endRequest: a free point, "none", or a glue reference.
func parseEndRequest(point []float64, glue *string, aliases map[string]string, pageAddr string, container uint32, dup int, glueField string) (endRequest, error) {
	if point != nil {
		return endRequest{freePoint: true, present: true}, nil
	}
	if glue == nil {
		return endRequest{}, nil
	}
	raw := *glue
	if raw == "none" {
		return endRequest{freeGlue: true, present: true}, nil
	}
	g, err := addParseGlue(raw, aliases, pageAddr, container, dup, glueField)
	if err != nil {
		return endRequest{}, err
	}
	return endRequest{glue: g, present: true}, nil
}

// endsCurrent reads a connector's effective cells through the
// resolver: numbers and formulas.
type endsCurrent struct {
	r *inherit.Resolver
	h *inherit.Shape
}

// number returns the effective number of the shape-level cell name.
func (c endsCurrent) number(name string) (float64, bool) {
	cell, _, ok := c.r.Cell(c.h, sheet.ShapeCell(name))
	if !ok {
		return 0, false
	}
	return vsd.Number(cell.V)
}

// formula returns the effective formula of the shape-level cell
// name and whether it has one.
func (c endsCurrent) formula(name string) (string, bool) {
	cell, _, ok := c.r.Cell(c.h, sheet.ShapeCell(name))
	if !ok || !cell.HasF {
		return "", false
	}
	return cell.F, true
}

// endsCellRefusal refuses an ends write whose target end cell
// cannot be read: the cell is named.
func endsCellRefusal(shapeAddr, cell string) error {
	return cli.Errorf(cli.CodeOperationFailed, "at", "%s %s is not stored", shapeAddr, cell)
}

// findEndsNode returns the shape a names in the page tree (the
// dup-th shape of its stored ID, in document order), or nil. It
// walks once and keeps no per-shape structure.
func findEndsNode(root *tree.Node, a vsd.Address) *tree.Node {
	var found *tree.Node
	seen := 0
	root.Walk(func(n *tree.Node) bool {
		if found != nil {
			return false
		}
		if n.Kind() != tree.ElementNode || n.Local() != vsd.ElemShape || !vsd.IsMain(n.Space()) {
			return true
		}
		idText, ok := n.Attr("ID")
		if !ok {
			return true
		}
		id, err := strconv.ParseUint(idText, 10, 32)
		if err != nil || uint32(id) != a.Shape {
			return true
		}
		seen++
		if seen == a.Dup {
			found = n
		}
		return true
	})
	return found
}

// walkGlueFormula is the _WALKGLUE formula prefix of a dynamic
// connector end, which a point glue or a free end replaces
// (edit/delete.go carries the same spelling).
const walkGlueFormula = "_WALKGLUE"

// desiredEnd builds one end's desired state: an unchanged end keeps
// its stored coordinates, formula and Connect rows; a free point or
// "none" frees it; a point glue writes the PAR(PNT(...)) formula;
// a whole-shape glue writes the _WALKGLUE formula and targets the
// whole shape. Begin reports whether the end is the begin.
// Coordinates are parent inches.
func desiredEnd(req endRequest, cur endsCurrent, loc addLocated, view geom.Pt, height float64, xName, yName, pointField string, begin bool) (edit.GlueEnd, error) {
	if !req.present {
		x, _ := cur.number(xName)
		y, _ := cur.number(yName)
		return edit.GlueEnd{X: x, Y: y, KeepFormula: true, KeepConnect: true}, nil
	}
	if req.freeGlue {
		x, _ := cur.number(xName)
		y, _ := cur.number(yName)
		return edit.GlueEnd{X: x, Y: y}, nil
	}
	x, y, ok := addViewToParent(loc.f, view, height)
	if !ok {
		return edit.GlueEnd{}, cli.Errorf(cli.CodeOperationFailed, pointField, "the end cannot be placed")
	}
	if req.freePoint {
		return edit.GlueEnd{X: x, Y: y}, nil
	}
	if req.isWhole() {
		return edit.GlueEnd{X: x, Y: y, Glue: true, Whole: true, Target: req.glue.id, Formula: walkFormula(begin)}, nil
	}
	return edit.GlueEnd{X: x, Y: y, Glue: true, Target: req.glue.id, Point: req.glue.point, Formula: glueFormula(req.glue.id, req.glue.point)}, nil
}

// glueFormula returns the point-glue formula of one end:
// PAR(PNT(Sheet.Target!Connections.XPoint,Sheet.Target!Connections.YPoint)),
// the create form of emit.ConnectorXML.
func glueFormula(target uint32, point int) string {
	id := strconv.FormatUint(uint64(target), 10)
	p := strconv.Itoa(point)
	return "PAR(PNT(Sheet." + id + "!Connections.X" + p + ",Sheet." + id + "!Connections.Y" + p + "))"
}

// isGlueFormula reports whether f is a glue formula this chunk
// replaces: PAR(PNT(...)) or _WALKGLUE(...).
func isGlueFormula(f string) bool {
	return strings.HasPrefix(f, "PAR(PNT(") || strings.HasPrefix(f, walkGlueFormula)
}

// checkEndFormulas refuses writing an end whose current X or Y
// formula is outside the allow list and is not a glue formula this
// chunk replaces, naming the cell and formula. An end whose
// desired value and formula already match is not written, so it
// passes as an identity.
func checkEndFormulas(end edit.GlueEnd, cur endsCurrent, xName, yName string) error {
	if end.KeepFormula {
		return nil
	}
	for _, tc := range []struct {
		name  string
		value float64
	}{
		{xName, end.X}, {yName, end.Y},
	} {
		current, _ := cur.number(tc.name)
		f, has := cur.formula(tc.name)
		if current == tc.value && ((has && f == end.Formula) || (!has && end.Formula == "")) && !end.KeepFormula {
			continue
		}
		if !has || f == "" || formula.Allowed(f) || isGlueFormula(f) {
			continue
		}
		return cli.Errorf(cli.CodeOperationFailed, "at", "%s F=%s is outside the allow list", tc.name, f)
	}
	return nil
}

// endsStale names the connector's cells (other than the ends, the
// transform cells and the Geometry X/Y cells this write rewrites)
// whose formula reads a moved end or transform cell, for the
// stale-values report, or "" when there are none.
func endsStale(node *tree.Node, shapeAddr string) string {
	var out []string
	node.Walk(func(n *tree.Node) bool {
		if len(out) >= 10 || n.Kind() != tree.ElementNode || n.Local() != vsd.ElemCell || !vsd.IsMain(n.Space()) {
			return true
		}
		name, _ := n.Attr("N")
		switch name {
		case vsd.CellBeginX, vsd.CellBeginY, vsd.CellEndX, vsd.CellEndY,
			vsd.CellPinX, vsd.CellPinY, vsd.CellWidth, vsd.CellHeight,
			vsd.CellLocPinX, vsd.CellLocPinY, vsd.CellAngle, "X", "Y":
			return true
		}
		f, ok := n.Attr("F")
		if !ok {
			return true
		}
		for _, ref := range []string{"BeginX", "BeginY", "EndX", "EndY", "PinX", "PinY", "Width", "Angle"} {
			if strings.Contains(f, ref) {
				out = append(out, shapeAddr+" "+name)
				break
			}
		}
		return true
	})
	if len(out) == 0 {
		return ""
	}
	return strings.Join(out, ", ") + " left stale"
}
