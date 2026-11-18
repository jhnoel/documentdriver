package ops

import (
	"math"
	"strings"

	"github.com/jhnoel/documentdriver/internal/cli"
	"github.com/jhnoel/documentdriver/internal/geom"
	"github.com/jhnoel/documentdriver/internal/vsdx/doc"
	"github.com/jhnoel/documentdriver/internal/vsdx/edit"
	"github.com/jhnoel/documentdriver/internal/vsdx/formula"
	"github.com/jhnoel/documentdriver/internal/vsdx/inherit"
	"github.com/jhnoel/documentdriver/internal/vsdx/place"
	"github.com/jhnoel/documentdriver/internal/vsdx/sheet"
	"github.com/jhnoel/documentdriver/internal/vsdx/vsd"
)

// setTransform applies the transform fields of s (box, rot, flip_h and
// flip_v) to the shape at s.At, which it resolves through aliases, and
// returns the changed address when it wrote anything. The desired view
// box is the shape's current box with the given fields replaced, and
// place.Unplace inverts it in the shape's parent frame, preserving the
// shape's LocPin fractions. It writes PinX, PinY, Width, Height, Angle,
// FlipX and FlipY (and LocPinX and LocPinY when their effective formula
// is an allowed constant, so a constant LocPin scales with the size),
// then runs edit.RecomputeShape over the written cells so dependents
// in the closed grammar (LocPin formulas, Geometry and Connections
// rows) are rewritten, materialising master dependents as
// F="Inh". A cell outside formula.Allowed is refused with
// operation_failed at the field written (box, rot, flip_h or
// flip_v), in the formula refusal form, before anything is
// written. A 1-D shape, whose transform is its ends, is
// operation_failed at at. Stale names dependents RecomputeShape left
// stale and connector ends MoveShape left stale, for apply's
// stale-values report. A write whose stored cells already show the
// wanted values writes nothing.
func setTransform(ws *edit.Workspace, s *ObjectSetSpec, aliases map[string]string, units string) ([]string, string, error) {
	if s.Box == nil && s.Rot == nil && s.FlipH == nil && s.FlipV == nil {
		return nil, "", nil
	}
	if s.Box != nil && !addFinite(s.Box) {
		return nil, "", cli.Errorf(cli.CodeOperationFailed, "box", "box must hold finite numbers")
	}
	if s.Rot != nil && (math.IsNaN(*s.Rot) || math.IsInf(*s.Rot, 0)) {
		return nil, "", cli.Errorf(cli.CodeOperationFailed, "rot", "rot must hold finite numbers")
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
	scale := 1.0
	if units == "in" {
		scale = vsd.PointsPerInch
	}
	r := inherit.New(d)
	pg, err := addLoadPage(ws, r, a.Container, a.PageDup)
	if err != nil {
		return nil, "", err
	}
	pageAddr := vsd.PageID(a.Container, a.PageDup)
	located := addLocate(pg.root, pageAddr, map[string]bool{shapeAddr: true}, r)
	loc, ok := located[shapeAddr]
	if !ok {
		return nil, "", cli.Errorf(cli.CodeNotFound, "at", "no shape %s: not found", shapeAddr)
	}
	if !loc.ok || !(loc.t.Width > 0) || !(loc.t.Height > 0) {
		return nil, "", cli.Errorf(cli.CodeOperationFailed, "at", "%s has no readable transform", shapeAddr)
	}
	node := findTextShape(pg.root, a.Shape, a.Dup)
	if node == nil {
		return nil, "", cli.Errorf(cli.CodeNotFound, "at", "no shape %s: not found", shapeAddr)
	}
	if err := nodeRefusal(r, node, shapeAddr, "at", true); err != nil {
		return nil, "", err
	}
	if err := scaledRefusal(r, d, a.Container, a.PageDup, pageAddr, transformField(s)); err != nil {
		return nil, "", err
	}
	h := textShapeOf(node)
	if _, _, has := r.Cell(h, sheet.ShapeCell(vsd.CellBeginX)); has {
		return nil, "", cli.Errorf(cli.CodeOperationFailed, "at", "%s is a 1-D shape: box, rot and flips apply to a 2-D shape", shapeAddr)
	}
	if !moveAncestorsOK(r, node) {
		return nil, "", cli.Errorf(cli.CodeOperationFailed, "at", "%s has no readable transform", shapeAddr)
	}
	curBox, err := place.Box(loc.f, loc.t, pg.height)
	if err != nil {
		return nil, "", cli.Errorf(cli.CodeOperationFailed, "at", "%s cannot be placed: %s", shapeAddr, err.Error())
	}
	want := transformWantBox(curBox, s, scale)
	locX := loc.t.LocPinX / loc.t.Width
	locY := loc.t.LocPinY / loc.t.Height
	desired, err := place.Unplace(loc.f, want, pg.height, locX, locY)
	if err != nil {
		return nil, "", cli.Errorf(cli.CodeOperationFailed, transformField(s), "the box cannot be placed: %s", err.Error())
	}
	wants := transformWants(r, h, desired)
	if len(wants) == 0 {
		return nil, "", nil
	}
	var cells []edit.Cell
	changedNames := make([]string, 0, len(wants))
	for _, want := range wants {
		formatted := vsd.FormatInches(want.value)
		if want.present {
			if old, numOK := vsd.Number(want.cell.V); numOK && vsd.FormatInches(old) == formatted {
				continue
			}
			if want.name == vsd.CellFlipX || want.name == vsd.CellFlipY {
				if old, ok := vsd.Bool(want.cell.V); ok {
					oldF := 0.0
					if old {
						oldF = 1
					}
					if vsd.FormatInches(oldF) == formatted {
						continue
					}
				}
			}
		}
		if c, ok := transformRefused(want); ok {
			return nil, "", formulaRefusal(shapeAddr, transformField(s), []formulaCell{c})
		}
		cells = append(cells, edit.Cell{N: want.name, V: formatted})
		changedNames = append(changedNames, want.name)
	}
	if len(cells) == 0 {
		return nil, "", nil
	}
	staleEnds, err := ws.MoveShape(shapeAddr, cells)
	if err != nil {
		return nil, "", moveEditError(err)
	}
	staleRe, err := ws.RecomputeShape(shapeAddr, changedNames)
	if err != nil {
		return nil, "", moveEditError(err)
	}
	var staleParts []string
	if len(staleEnds) > 0 {
		staleParts = append(staleParts, strings.Join(staleEnds, ", ")+" left stale (stale-connector)")
	}
	if len(staleRe) > 0 {
		staleParts = append(staleParts, strings.Join(staleRe, ", ")+" left stale")
	}
	return []string{shapeAddr}, strings.Join(staleParts, "; "), nil
}

// transformField returns the plan field a placement failure is
// reported at: box when a box is given, otherwise rot, flip_h or
// flip_v, whichever is given first.
func transformField(s *ObjectSetSpec) string {
	switch {
	case s.Box != nil:
		return "box"
	case s.Rot != nil:
		return "rot"
	case s.FlipH != nil:
		return "flip_h"
	default:
		return "flip_v"
	}
}

// transformWantBox returns curBox with the box, rotation and flips of
// s applied, in view points (rot normalised to [0, 360)).
func transformWantBox(cur geom.Box, s *ObjectSetSpec, scale float64) geom.Box {
	want := cur
	if s.Box != nil {
		want.X, want.Y, want.W, want.H = s.Box[0]*scale, s.Box[1]*scale, s.Box[2]*scale, s.Box[3]*scale
	}
	if s.Rot != nil {
		want.Rot = geom.NormDeg(*s.Rot)
	}
	if s.FlipH != nil {
		want.FlipH = *s.FlipH
	}
	if s.FlipV != nil {
		want.FlipV = *s.FlipV
	}
	return want
}

// transformWant is one transform cell setTransform would write: its
// name, its new value in stored units (inches, radians, or 0/1 for a
// flip), and the effective cell it would replace.
type transformWant struct {
	name    string
	value   float64
	cell    sheet.Cell
	present bool
}

// transformWants returns the cells a transform write to desired
// would write, in check order: PinX, PinY, Width, Height, Angle,
// FlipX, FlipY, then LocPinX and LocPinY when their effective formula
// is an allowed constant (so a constant LocPin scales with the size).
// LocPin cells whose formula is a dependent are left to
// RecomputeShape, which rewrites them in the closed grammar or
// reports them stale.
func transformWants(r *inherit.Resolver, h *inherit.Shape, desired place.Transform) []transformWant {
	flipX, flipY := 0.0, 0.0
	if desired.FlipX {
		flipX = 1
	}
	if desired.FlipY {
		flipY = 1
	}
	values := []struct {
		name  string
		value float64
	}{
		{vsd.CellPinX, desired.PinX},
		{vsd.CellPinY, desired.PinY},
		{vsd.CellWidth, desired.Width},
		{vsd.CellHeight, desired.Height},
		{vsd.CellAngle, desired.Angle},
		{vsd.CellFlipX, flipX},
		{vsd.CellFlipY, flipY},
	}
	var wants []transformWant
	for _, v := range values {
		cell, _, present := r.Cell(h, sheet.ShapeCell(v.name))
		wants = append(wants, transformWant{name: v.name, value: v.value, cell: cell, present: present})
	}
	for _, v := range []struct {
		name  string
		value float64
	}{
		{vsd.CellLocPinX, desired.LocPinX},
		{vsd.CellLocPinY, desired.LocPinY},
	} {
		cell, origin, present := r.Cell(h, sheet.ShapeCell(v.name))
		if !transformLocWrite(r, h, v.name, cell, origin) {
			continue
		}
		wants = append(wants, transformWant{name: v.name, value: v.value, cell: cell, present: present})
	}
	return wants
}

// transformLocWrite reports whether a LocPin cell may be written
// directly: its effective formula is allowed (absent, a constant, or
// Inh resolving to an allowed formula), not a dependent formula
// RecomputeShape rewrites. An Inh cell whose master formula is a
// dependent is left to RecomputeShape, which materialises it.
func transformLocWrite(r *inherit.Resolver, h *inherit.Shape, name string, cell sheet.Cell, origin inherit.Origin) bool {
	if origin == inherit.Inherited {
		master, ok := r.MasterShape(h)
		if !ok || master == nil || master.Sheet == nil {
			return true
		}
		mc, ok := master.Sheet.Cell(sheet.ShapeCell(name))
		if !ok {
			return true
		}
		return formula.Allowed(mc.F)
	}
	return formula.Allowed(cell.F)
}

// transformRefused reports whether writing want's cell is outside
// the allow list: its effective formula must be on the decision (c)
// list (formula.Allowed). It returns the refused cell for the
// formula refusal form.
func transformRefused(want transformWant) (formulaCell, bool) {
	if !want.present {
		return formulaCell{}, false
	}
	if formula.Allowed(want.cell.F) {
		return formulaCell{}, false
	}
	return formulaCell{name: want.name, formula: want.cell.F}, true
}
