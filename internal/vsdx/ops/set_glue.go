package ops

import (
	"errors"
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
// for the fields given). A free end (from/to) is placed at the view
// point and freed: its Connect row and end formula go, its V
// becomes the point. A point glue (from_glue/to_glue "s2:2") writes
// the create form (PAR(PNT(Sheet.N!Connections.Xk, ...)) on the
// end's cells and a Connect row) and moves the end to the site.
// "none" frees the end and keeps its V. Whole-shape glue is
// refused as object.add refuses it: routing a dynamic connector
// is Visio's, not reproduced here (docs/vsdx-design.md, decision
// d). The transform cells are rewritten by formula.OneD
// (coinciding ends give width 0 at the shared point). Changed is
// the shape's address, or nothing for an identity write. Stale
// names a dependent the write did not rewrite ("" when there is
// none), for apply's stale-values report.
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
	if err := checkWholeGlue(beginReq, "from_glue", derefStr(s.FromGlue)); err != nil {
		return nil, "", err
	}
	if err := checkWholeGlue(endReq, "to_glue", derefStr(s.ToGlue)); err != nil {
		return nil, "", err
	}
	beginEnd, err := desiredEnd(beginReq, cur, loc, beginView, pg.height, vsd.CellBeginX, vsd.CellBeginY, "from")
	if err != nil {
		return nil, "", err
	}
	endEnd, err := desiredEnd(endReq, cur, loc, endView, pg.height, vsd.CellEndX, vsd.CellEndY, "to")
	if err != nil {
		return nil, "", err
	}
	pinX, pinY, width, angle, bad := oneDOrCoinciding(beginEnd.X, beginEnd.Y, endEnd.X, endEnd.Y)
	if bad {
		return nil, "", cli.Errorf(cli.CodeOperationFailed, "to", "the connector ends cannot form a 1-D transform")
	}
	desired := edit.ConnectorEnds{
		Begin: beginEnd, End: endEnd,
		PinX: pinX, PinY: pinY, Width: width, Height: 0,
		LocPinX: width * 0.5, LocPinY: 0, Angle: angle,
	}
	if err := checkEndFormulas(beginEnd, cur, vsd.CellBeginX, vsd.CellBeginY); err != nil {
		return nil, "", err
	}
	if err := checkEndFormulas(endEnd, cur, vsd.CellEndX, vsd.CellEndY); err != nil {
		return nil, "", err
	}
	if err := checkTransformFormulas(cur, loc.t, desired); err != nil {
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
	if !changed {
		return nil, "", nil
	}
	stale := ""
	if beginEnd.X != beginX || beginEnd.Y != beginY || endEnd.X != endX || endEnd.Y != endY {
		stale = endsStale(node, shapeAddr)
	}
	return []string{shapeAddr}, stale, nil
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

// checkWholeGlue refuses whole-shape glue, as object.add refuses
// it on a master-less connector: the op writes the create form
// (point glue only) and Visio's dynamic routing is not reproduced.
func checkWholeGlue(req endRequest, field, raw string) error {
	if !req.isWhole() {
		return nil
	}
	return addWholeGlue(field, raw)
}

// derefStr returns the string p points to, or "" for nil.
func derefStr(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}

// walkGlueFormula is the _WALKGLUE formula prefix of a dynamic
// connector end, which a point glue or a free end replaces
// (edit/delete.go carries the same spelling).
const walkGlueFormula = "_WALKGLUE"

// desiredEnd builds one end's desired state: an unchanged end keeps
// its stored coordinates, formula and Connect rows; a free point or
// "none" frees it; a point glue writes the PAR(PNT(...)) formula.
// Coordinates are parent inches.
func desiredEnd(req endRequest, cur endsCurrent, loc addLocated, view geom.Pt, height float64, xName, yName, pointField string) (edit.GlueEnd, error) {
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

// oneDOrCoinciding returns the transform cells formula.OneD derives
// for ends in parent inches. Coinciding ends (width 0) give the
// shared point, width 0 and angle 0 (ATAN2(0,0)); bad is true for
// a non-finite transform.
func oneDOrCoinciding(beginX, beginY, endX, endY float64) (pinX, pinY, width, angle float64, bad bool) {
	pinX, pinY, width, angle, ok := formula.OneD(beginX, beginY, endX, endY)
	if ok {
		return pinX, pinY, width, angle, false
	}
	if beginX == endX && beginY == endY && addFinite([]float64{beginX, beginY}) {
		return beginX, beginY, 0, 0, false
	}
	return 0, 0, 0, 0, true
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

// checkTransformFormulas refuses writing a transform cell whose
// effective formula is neither allowed nor the OneD formula of
// formula.OneD, naming the cell and formula. A cell whose desired
// value already matches is not written, so it passes.
func checkTransformFormulas(cur endsCurrent, t place.Transform, desired edit.ConnectorEnds) error {
	for _, tc := range []struct {
		name     string
		current  float64
		want     float64
		expected string
	}{
		{vsd.CellPinX, t.PinX, desired.PinX, "(BeginX+EndX)/2"},
		{vsd.CellPinY, t.PinY, desired.PinY, "(BeginY+EndY)/2"},
		{vsd.CellWidth, t.Width, desired.Width, "SQRT((EndX-BeginX)^2+(EndY-BeginY)^2)"},
		{vsd.CellHeight, t.Height, desired.Height, ""},
		{vsd.CellLocPinX, t.LocPinX, desired.LocPinX, "Width*0.5"},
		{vsd.CellLocPinY, t.LocPinY, desired.LocPinY, "Height*0.5"},
		{vsd.CellAngle, t.Angle, desired.Angle, "ATAN2(EndY-BeginY,EndX-BeginX)"},
	} {
		if tc.current == tc.want {
			continue
		}
		f, has := cur.formula(tc.name)
		if !has || f == "" || f == tc.expected || formula.Allowed(f) {
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
