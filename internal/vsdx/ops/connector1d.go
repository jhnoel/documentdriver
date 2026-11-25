package ops

import (
	"math"
	"strconv"
	"strings"

	"github.com/jhnoel/documentdriver/internal/ascii"
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

// connector1d holds the CC2 helpers for Visio-authored
// connectors: the transform kernel over the closed evaluator,
// the dynamic/line family test, and the trigger and
// Geometry1 writers. It is used by set_glue.go (object.set
// ends) and run_object_move.go (object.move of a connector).

// cellBegTrigger and cellEndTrigger name the begin and end
// trigger cells of a dynamic connector.
const (
	cellBegTrigger = "BegTrigger"
	cellEndTrigger = "EndTrigger"
)

// walkBeginFormula and walkEndFormula are the _WALKGLUE whole-shape
// glue formulas of the begin and end of a dynamic connector, in
// the corpus spellings.
const (
	walkBeginFormula = "_WALKGLUE(BegTrigger,EndTrigger,WalkPreference)"
	walkEndFormula   = "_WALKGLUE(EndTrigger,BegTrigger,WalkPreference)"
)

// walkFormula returns the whole-shape glue formula of one end:
// the begin spells BegTrigger first, the end EndTrigger first.
func walkFormula(begin bool) string {
	if begin {
		return walkBeginFormula
	}
	return walkEndFormula
}

// triggerFormula returns the point/whole glue trigger formula of
// one end: _XFTRIGGER(Sheet.Target!EventXFMod).
func triggerFormula(target uint32) string {
	return "_XFTRIGGER(Sheet." + strconv.FormatUint(uint64(target), 10) + "!EventXFMod)"
}

// wholeGlueRefusal refuses whole-shape glue on a connector whose
// master lacks Visio's dynamic glue cells. The message and hint
// are CC2's; the hint names the two ways out.
func wholeGlueRefusal(shapeAddr, field string) error {
	e := cli.Errorf(cli.CodeOperationFailed, field, "%s is not from a master with Visio's dynamic glue cells: whole-shape glue needs one", shapeAddr)
	e.Hint = "add the connector with object.add master \"Dynamic connector\", or glue to a connection point"
	return e
}

// isLineTransform reports whether a connector's transform is the
// line family: its Width formula uses SQRT or its Angle ATAN2.
// The other family is the dynamic one (Angle constant).
func isLineTransform(r *inherit.Resolver, h *inherit.Shape) bool {
	if cell, _, ok := r.Cell(h, sheet.ShapeCell(vsd.CellWidth)); ok {
		if formulaHasFunc(cell.F, "SQRT") {
			return true
		}
	}
	if cell, _, ok := r.Cell(h, sheet.ShapeCell(vsd.CellAngle)); ok {
		if formulaHasFunc(cell.F, "ATAN2") {
			return true
		}
	}
	return false
}

// formulaHasFunc reports whether f calls the function name (a
// Func token of that name in formula.Lex).
func formulaHasFunc(f, name string) bool {
	for _, tok := range formula.Lex(f) {
		if tok.Kind == formula.Func && ascii.EqualFold(tok.Text, name) {
			return true
		}
	}
	return false
}

// isDynamicConnector reports whether a connector is dynamic: an
// end's formula is _WALKGLUE, the master uses one, or the shape
// stores ShapeRouteStyle. A dynamic free-end write sets
// RecalcDocument and reports stale-values "<address> route".
func isDynamicConnector(r *inherit.Resolver, h *inherit.Shape) bool {
	for _, name := range []string{vsd.CellBeginX, vsd.CellBeginY, vsd.CellEndX, vsd.CellEndY} {
		if cell, _, ok := r.Cell(h, sheet.ShapeCell(name)); ok {
			if g, ok := formula.GlueForm(cell.F); ok && g.Kind == formula.GlueWhole {
				return true
			}
		}
	}
	if _, _, ok := r.Cell(h, sheet.ShapeCell("ShapeRouteStyle")); ok {
		return true
	}
	if ms, ok := r.MasterShape(h); ok && ms != nil && ms.Sheet != nil {
		if cell, ok := ms.Sheet.Cell(sheet.ShapeCell(vsd.CellWidth)); ok {
			if isDynamicWidthFormula(cell.F) {
				return true
			}
		}
	}
	return false
}

// isDynamicWidthFormula reports whether a Width formula is the
// dynamic GUARD(EndX-BeginX) (spaces and case ignored).
func isDynamicWidthFormula(f string) bool {
	s := strings.ReplaceAll(f, " ", "")
	return strings.EqualFold(s, "GUARD(EndX-BeginX)")
}

// hasDynamicMaster reports whether the connector's master is a
// dynamic connector master: it stores BegTrigger/EndTrigger with
// _XFTRIGGER(EventXFMod), or its Width is GUARD(EndX-BeginX). It
// is false for a master-less connector and for a 2-D master.
func hasDynamicMaster(r *inherit.Resolver, h *inherit.Shape) bool {
	ms, ok := r.MasterShape(h)
	if !ok || ms == nil || ms.Sheet == nil {
		return false
	}
	for _, name := range []string{cellBegTrigger, cellEndTrigger} {
		if cell, ok := ms.Sheet.Cell(sheet.ShapeCell(name)); ok {
			if g, ok := formula.GlueForm(cell.F); ok && g.Kind == formula.GlueTrigger && g.Sheet == 0 {
				return true
			}
		}
	}
	if cell, ok := ms.Sheet.Cell(sheet.ShapeCell(vsd.CellWidth)); ok {
		if isDynamicWidthFormula(cell.F) {
			if _, ok := ms.Sheet.Cell(sheet.ShapeCell(vsd.CellBeginX)); ok {
				return true
			}
		}
	}
	return false
}

// evalTransform re-evaluates every transform cell whose effective
// formula is CC1-evaluable and returns the new transform for new
// ends. A constant, Inh, or absent cell keeps its current value.
// A cell outside those forms is operation_failed at field, in the
// formula refusal form. It allocates in proportion to the
// formula texts only.
func evalTransform(r *inherit.Resolver, h *inherit.Shape, shapeAddr, field string, beginX, beginY, endX, endY float64) (endOfConnector, error) {
	type want struct {
		name    string
		cur     float64
		f       string
		hasF    bool
		present bool
		origin  inherit.Origin
	}
	names := []string{vsd.CellPinX, vsd.CellPinY, vsd.CellWidth, vsd.CellHeight, vsd.CellLocPinX, vsd.CellLocPinY, vsd.CellAngle}
	wants := make([]want, 0, len(names))
	vals := map[string]float64{
		"beginx": beginX, "beginy": beginY, "endx": endX, "endy": endY,
	}
	for _, name := range names {
		cell, origin, ok := r.Cell(h, sheet.ShapeCell(name))
		if !ok {
			continue
		}
		v, numOK := vsd.Number(cell.V)
		if !numOK {
			return endOfConnector{}, cli.Errorf(cli.CodeOperationFailed, "at", "%s %s is not a number", shapeAddr, name)
		}
		f := cell.F
		// An Inherited cell (F="Inh") takes its master's
		// formula; without a master it keeps its cached
		// value.
		if origin == inherit.Inherited && f == vsd.FormulaInherited {
			if ms, mok := r.MasterShape(h); mok && ms != nil && ms.Sheet != nil {
				if mc, mcOK := ms.Sheet.Cell(sheet.ShapeCell(name)); mcOK && mc.HasF && mc.F != "" {
					f = mc.F
				}
			}
		}
		w := want{name: name, cur: v, f: f, hasF: cell.HasF, present: true, origin: origin}
		wants = append(wants, w)
		// The cell's own current value is the fallback for a
		// reference to a transform cell not yet computed: the
		// order below (pin, size, locpin, angle) is the
		// dependency order of the standard forms, so a
		// reference to an earlier cell reads its new value.
		vals[ascii.Lower(name)] = v
	}
	// ends cell lookup for Eval: new ends win, then computed
	// transform cells, then current effective values.
	cellFn := func(ref string) (float64, bool) {
		if v, ok := vals[ascii.Lower(ref)]; ok {
			return v, true
		}
		return 0, false
	}
	out := endOfConnector{}
	for _, w := range wants {
		f := w.f
		if !w.hasF || f == "" {
			vals[ascii.Lower(w.name)] = w.cur
			continue
		}
		if f == vsd.FormulaInherited || f == vsd.FormulaNone {
			// An Inh with no master formula (or No Formula)
			// keeps its cached value.
			vals[ascii.Lower(w.name)] = w.cur
			continue
		}
		if formula.Allowed(f) {
			// A constant, THEMEVAL, Inh already resolved,
			// or No Formula spelling: the cached value is
			// the value, F kept.
			vals[ascii.Lower(w.name)] = w.cur
			continue
		}
		if !formula.Evaluable(f) {
			return endOfConnector{}, formulaRefusal(shapeAddr, field, []formulaCell{{name: w.name, formula: f}})
		}
		v, err := formula.Eval(f, cellFn)
		if err != nil {
			return endOfConnector{}, formulaRefusal(shapeAddr, field, []formulaCell{{name: w.name, formula: f}})
		}
		if math.IsNaN(v) || math.IsInf(v, 0) {
			return endOfConnector{}, cli.Errorf(cli.CodeOperationFailed, "at", "%s cannot be placed", shapeAddr)
		}
		vals[ascii.Lower(w.name)] = v
	}
	// Absent cells (no effective cell) keep the transform's
	// zero; SetConnectorEnds leaves absent cells alone.
	if v, ok := vals["pinx"]; ok {
		out.pinX = v
	}
	if v, ok := vals["piny"]; ok {
		out.pinY = v
	}
	if v, ok := vals["width"]; ok {
		out.width = v
	}
	if v, ok := vals["height"]; ok {
		out.height = v
	}
	if v, ok := vals["locpinx"]; ok {
		out.locPinX = v
	}
	if v, ok := vals["locpiny"]; ok {
		out.locPinY = v
	}
	if v, ok := vals["angle"]; ok {
		out.angle = v
	}
	out.present = true
	return out, nil
}

// endOfConnector is one evaluated transform: the new PinX/PinY,
// Width, Height, LocPinX/Y and Angle for new ends.
type endOfConnector struct {
	pinX, pinY       float64
	width, height    float64
	locPinX, locPinY float64
	angle            float64
	present          bool
}

// checkConnectorEditable refuses a connector whose transform or
// end cells are outside CC1's editability test: every transform
// cell is a constant, Inh, or evaluable, and every end cell is a
// constant or a glue form. An Inh transform cell passes. The
// refusal is at field, in the formula refusal form.
func checkConnectorEditable(r *inherit.Resolver, h *inherit.Shape, shapeAddr, field string) error {
	for _, name := range []string{vsd.CellPinX, vsd.CellPinY, vsd.CellWidth, vsd.CellHeight, vsd.CellLocPinX, vsd.CellLocPinY, vsd.CellAngle} {
		cell, origin, ok := r.Cell(h, sheet.ShapeCell(name))
		if !ok {
			continue
		}
		if origin == inherit.Inherited {
			continue
		}
		f := cell.F
		if !cell.HasF || f == "" || formula.Allowed(f) {
			continue
		}
		if f == vsd.FormulaInherited || f == vsd.FormulaNone {
			continue
		}
		if !formula.Evaluable(f) {
			return formulaRefusal(shapeAddr, field, []formulaCell{{name: name, formula: f}})
		}
	}
	return nil
}

// currentGlue returns the glue of one end from the page's Connect rows:
// the target sheet, the point (0 for whole-shape glue), whole, and
// present (a Connect row of that end exists). A Connect to any
// other cell is present but neither point nor whole.
func currentGlue(root *tree.Node, id uint32, begin bool) (target uint32, point int, whole, present bool) {
	var connects *tree.Node
	for c := range root.ElementsSeq() {
		if c.Kind() == tree.ElementNode && c.Local() == vsd.ElemConnects && vsd.IsMain(c.Space()) {
			connects = c
		}
	}
	if connects == nil {
		return 0, 0, false, false
	}
	fromCell := vsd.FromCellEnd
	if begin {
		fromCell = vsd.FromCellBegin
	}
	for c := range connects.ElementsSeq() {
		if c.Kind() != tree.ElementNode || c.Local() != vsd.ElemConnect {
			continue
		}
		fromID, ok := connectIDOf(c, "FromSheet")
		if !ok || fromID != id {
			continue
		}
		cell, _ := c.Attr("FromCell")
		if cell != fromCell {
			continue
		}
		toID, _ := connectIDOf(c, "ToSheet")
		toCell, _ := c.Attr("ToCell")
		if toCell == vsd.ToCellPin {
			return toID, 0, true, true
		}
		if after, ok0 := strings.CutPrefix(toCell, vsd.ToCellConnectionPfx); ok0 {
			if n, err := strconv.Atoi(after); err == nil {
				return toID, n, false, true
			}
		}
		return toID, 0, false, true
	}
	return 0, 0, false, false
}

// isGluedEnd reports whether one end of the connector at node is
// glued: a Connect row of that end, or a glue formula on the
// end's cells. It walks the page Connects once for both ends.
func isGluedEnds(root *tree.Node, id uint32, r *inherit.Resolver, h *inherit.Shape) (beginGlued, endGlued bool) {
	// Connect rows: BeginX/EndX of shape id.
	var connects *tree.Node
	for c := range root.ElementsSeq() {
		if c.Kind() == tree.ElementNode && c.Local() == vsd.ElemConnects && vsd.IsMain(c.Space()) {
			connects = c
		}
	}
	if connects != nil {
		for c := range connects.ElementsSeq() {
			if c.Kind() != tree.ElementNode || c.Local() != vsd.ElemConnect {
				continue
			}
			fromID, ok := connectIDOf(c, "FromSheet")
			if !ok || fromID != id {
				continue
			}
			cell, _ := c.Attr("FromCell")
			switch cell {
			case vsd.FromCellBegin:
				beginGlued = true
			case vsd.FromCellEnd:
				endGlued = true
			}
		}
	}
	if !beginGlued {
		for _, name := range []string{vsd.CellBeginX, vsd.CellBeginY} {
			if cell, _, ok := r.Cell(h, sheet.ShapeCell(name)); ok && isGlueFormulaF(cell.F) {
				beginGlued = true
				break
			}
		}
	}
	if !endGlued {
		for _, name := range []string{vsd.CellEndX, vsd.CellEndY} {
			if cell, _, ok := r.Cell(h, sheet.ShapeCell(name)); ok && isGlueFormulaF(cell.F) {
				endGlued = true
				break
			}
		}
	}
	return beginGlued, endGlued
}

// isGlueFormulaF reports whether f is a glue formula: PAR(PNT)
// or _WALKGLUE, per formula.GlueForm.
func isGlueFormulaF(f string) bool {
	if f == "" {
		return false
	}
	g, ok := formula.GlueForm(f)
	return ok && (g.Kind == formula.GluePoint || g.Kind == formula.GlueWhole)
}

// wholeEdgeView returns the cached whole-glue end as a view
// point: the midpoint of the edge of the target at loc whose
// outward normal is closest to the direction of other (all in
// view points, y down). The target's local edge midpoints are
// its (0, H/2), (W, H/2), (W/2, 0) and (W/2, H); the centre is
// (LocPinX, LocPinY).
func wholeEdgeView(loc addLocated, other geom.Pt, height float64) geom.Pt {
	t := loc.t
	if !(t.Width > 0) || !(t.Height > 0) || math.IsNaN(t.Width) || math.IsNaN(t.Height) {
		// A degenerate box: its pin view point.
		return place.Site(loc.f, t, t.LocPinX, t.LocPinY, height)
	}
	centre := place.Site(loc.f, t, t.LocPinX, t.LocPinY, height)
	dx := other.X - centre.X
	dy := other.Y - centre.Y
	if dx == 0 && dy == 0 {
		dx = 1
	}
	mid := [4]geom.Pt{
		place.Site(loc.f, t, 0, t.Height*0.5, height),
		place.Site(loc.f, t, t.Width, t.Height*0.5, height),
		place.Site(loc.f, t, t.Width*0.5, 0, height),
		place.Site(loc.f, t, t.Width*0.5, t.Height, height),
	}
	best := 0
	bestDot := math.Inf(-1)
	for i, p := range mid {
		nx := p.X - centre.X
		ny := p.Y - centre.Y
		// Outward normal of the edge through p: p is on the
		// edge, so the normal is p-centre (the edge midpoint
		// minus centre), compared by direction only.
		norm := math.Hypot(nx, ny)
		if norm == 0 {
			continue
		}
		dot := (nx*dx + ny*dy) / norm
		if dot > bestDot {
			bestDot = dot
			best = i
		}
	}
	return mid[best]
}

// findShapeCell returns the direct Cell child of node with N
// name, or nil.
func findShapeCell(node *tree.Node, name string) *tree.Node {
	for c := range node.ElementsSeq() {
		if c.Kind() == tree.ElementNode && c.Local() == vsd.ElemCell && vsd.IsMain(c.Space()) {
			if n, _ := c.Attr("N"); n == name {
				return c
			}
		}
	}
	return nil
}

// connectIDOf parses a Connect row's id attribute as a uint32.
func connectIDOf(c *tree.Node, name string) (uint32, bool) {
	raw, _ := c.Attr(name)
	u, err := strconv.ParseUint(raw, 10, 32)
	return uint32(u), err == nil
}

// connectorPagePart returns the part of the page a names, using the
// document of ws, for core writes on its shapes.
func connectorPagePart(_ *edit.Workspace, d *doc.Document, a vsd.Address) (string, error) {
	depth := 0
	for i := range d.Pages {
		if d.Pages[i].ID != a.Container {
			continue
		}
		depth++
		if depth == a.PageDup {
			if d.Pages[i].Part == "" {
				return "", cli.Errorf(cli.CodeUnreadableTarget, "at", "%s cannot be edited safely", vsd.PageID(a.Container, a.PageDup))
			}
			return d.Pages[i].Part, nil
		}
	}
	return "", cli.Errorf(cli.CodeNotFound, "at", "no page %s: not found", vsd.PageID(a.Container, a.PageDup))
}

// writeTriggerGlue writes the trigger cell of one glued end:
// V 2 and _XFTRIGGER(Sheet.Target!EventXFMod), through SetCells
// (which creates the cell when the shape stores none), when the
// effective trigger differs. It reports whether it changed
// anything.
func writeTriggerGlue(ws *edit.Workspace, r *inherit.Resolver, h *inherit.Shape, shapeAddr, trigger string, target uint32) (bool, error) {
	want := triggerFormula(target)
	if cell, _, ok := r.Cell(h, sheet.ShapeCell(trigger)); ok && cell.V == "2" && cell.F == want {
		return false, nil
	}
	if err := ws.SetCells(shapeAddr, []edit.Cell{{N: trigger, V: "2", F: want}}); err != nil {
		return false, err
	}
	return true, nil
}

// freeTrigger frees the trigger cell of one end freed by "none":
// when the connector is from a master with Visio's dynamic glue
// cells the local cell is removed (the shape inherits the
// master's _XFTRIGGER(EventXFMod)), else the local cell keeps
// its V with its F removed. It reports whether it changed
// anything. part is the shape's page part, for the core deletion.
func freeTrigger(ws *edit.Workspace, part string, r *inherit.Resolver, h *inherit.Shape, shapeAddr, trigger string, node *tree.Node) (bool, error) {
	local := findShapeCell(node, trigger)
	if local == nil {
		return false, nil
	}
	masterStores := hasDynamicMaster(r, h)
	if masterStores {
		handle := ws.Core().Target(part, shapeAddr, []*tree.Node{node})
		if err := handle.RemoveNode(local); err != nil {
			return false, err
		}
		return true, nil
	}
	if f, _ := local.Attr("F"); f == "" {
		return false, nil
	}
	v, _ := local.Attr("V")
	if err := ws.SetCells(shapeAddr, []edit.Cell{{N: trigger, V: v, F: ""}}); err != nil {
		return false, err
	}
	return true, nil
}

// writeGeometry1 rewrites the stored Geometry1 (MoveTo IX 1,
// LineTo IX 2) of node as the straight two-point line for one
// evaluated transform: in the line family both points are at
// Height/2, in the dynamic family at LocPinY+(BeginY-PinY) and
// LocPinY+(EndY-PinY) (MoveTo(0, yb), LineTo(Width, ye)). Each
// of the four cells is written only when the shape stores it
// and its V differs. It reports whether it changed anything.
func writeGeometry1(ws *edit.Workspace, part, shapeAddr string, node *tree.Node, line bool, tr endOfConnector, beginY, endY float64) (bool, error) {
	var section *tree.Node
	for c := range node.ElementsSeq() {
		if c.Kind() == tree.ElementNode && c.Local() == vsd.ElemSection && vsd.IsMain(c.Space()) {
			if n, _ := c.Attr("N"); n == vsd.SectionGeometry {
				if ix, _ := c.Attr("IX"); ix == "0" || ix == "" {
					section = c
					break
				}
			}
		}
	}
	if section == nil {
		return false, nil
	}
	var moveTo, lineTo *tree.Node
	for row := range section.ElementsSeq() {
		if row.Kind() != tree.ElementNode || row.Local() != vsd.ElemRow {
			continue
		}
		typ, _ := row.Attr("T")
		if typ == "MoveTo" && moveTo == nil {
			moveTo = row
		}
		if typ == "LineTo" && lineTo == nil {
			lineTo = row
		}
	}
	yb, ye := 0.0, 0.0
	if line {
		yb = tr.height * 0.5
		ye = yb
	} else {
		yb = tr.locPinY + (beginY - tr.pinY)
		ye = tr.locPinY + (endY - tr.pinY)
	}
	handle := ws.Core().Target(part, shapeAddr, []*tree.Node{node})
	changed := false
	write := func(row *tree.Node, cell string, value float64) error {
		if row == nil {
			return nil
		}
		for c := range row.ElementsSeq() {
			if c.Kind() != tree.ElementNode || c.Local() != vsd.ElemCell {
				continue
			}
			if n, _ := c.Attr("N"); n != cell {
				continue
			}
			cur, _ := c.Attr("V")
			if old, ok := vsd.Number(cur); ok && vsd.FormatInches(old) == vsd.FormatInches(value) {
				return nil
			}
			if err := handle.SetNodeAttr(c, "V", vsd.FormatInches(value)); err != nil {
				return err
			}
			changed = true
			return nil
		}
		return nil
	}
	for _, w := range []struct {
		row   *tree.Node
		cell  string
		value float64
	}{
		{moveTo, "X", 0},
		{moveTo, "Y", yb},
		{lineTo, "X", tr.width},
		{lineTo, "Y", ye},
	} {
		if err := write(w.row, w.cell, w.value); err != nil {
			return false, err
		}
	}
	return changed, nil
}
