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
// F="Inh". A LocPin the master (or a style) holds as an evaluable
// dependent is materialised directly as F="Inh" with its new value,
// whatever its unit, so a RecomputeShape failure on another
// dependent cannot leave it stale and the box wrong; a master-held
// LocPin whose formula is not evaluable is refused instead. A cell
// outside formula.Allowed is refused with operation_failed at the
// field written (box, rot, flip_h or flip_v), in the formula refusal
// form, before anything is written. A 1-D shape, whose transform is
// its ends, is operation_failed at at. Stale names dependents
// RecomputeShape left stale and connector ends MoveShape left stale,
// for apply's stale-values report. A write whose given fields
// already equal the current box in read's form writes nothing.
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
	if transformIdentity(curBox, s, scale) {
		return nil, "", nil
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
	// Materialise wants are held back from the first write so
	// RecomputeShape can materialise them itself (with its own
	// fragment) when every dependent is evaluable; a want whose
	// formula is not evaluable is refused before anything is
	// written, and a want RecomputeShape leaves stale is written
	// directly below.
	var held []transformWant
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
		if want.materialise {
			if !formula.Evaluable(want.formula) {
				return nil, "", formulaRefusal(shapeAddr, transformField(s), []formulaCell{{name: want.name, formula: want.formula}})
			}
			held = append(held, want)
			continue
		}
		if c, ok := transformRefused(want); ok {
			return nil, "", formulaRefusal(shapeAddr, transformField(s), []formulaCell{c})
		}
		cells = append(cells, edit.Cell{N: want.name, V: formatted})
		changedNames = append(changedNames, want.name)
	}
	if len(cells) == 0 && len(held) == 0 {
		return nil, "", nil
	}
	staleEnds, err := ws.MoveShape(shapeAddr, cells)
	if err != nil {
		return nil, "", moveEditError(err)
	}
	var staleRe []string
	if len(cells) > 0 {
		staleRe, err = ws.RecomputeShape(shapeAddr, changedNames)
		if err != nil {
			return nil, "", moveEditError(err)
		}
	}
	// A held LocPin that RecomputeShape left stale (because
	// another dependent failed) is materialised directly with
	// its new value, whatever its unit, so the box reads back
	// as written and the LocPin is not stale.
	if len(held) > 0 {
		stale := map[string]bool{}
		for _, entry := range staleRe {
			if i := strings.LastIndexByte(entry, ' '); i >= 0 {
				stale[entry[i+1:]] = true
			}
		}
		var fallback []edit.Cell
		for _, want := range held {
			if len(cells) == 0 || stale[want.name] {
				fallback = append(fallback, edit.Cell{N: want.name, V: vsd.FormatInches(want.value), U: want.cell.U, F: "Inh"})
			}
		}
		if len(fallback) > 0 {
			if err := ws.SetCells(shapeAddr, fallback); err != nil {
				return nil, "", moveEditError(err)
			}
			written := make(map[string]bool, len(fallback))
			for _, c := range fallback {
				written[c.N] = true
			}
			kept := staleRe[:0]
			for _, entry := range staleRe {
				name := entry
				if i := strings.LastIndexByte(entry, ' '); i >= 0 {
					name = entry[i+1:]
				}
				if written[name] {
					continue
				}
				kept = append(kept, entry)
			}
			staleRe = kept
		}
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

// transformIdentity reports whether every transform field s gives
// already equals the current box curBox in read's form: box and rot
// as geom.FormatNum points and degrees (the numbers read shows),
// flips as booleans. A field that matches in read's form writes
// nothing, even when the stored Pin, Width or Angle cells would
// reformat to a different last digit on the round trip through
// place.Unplace, so read's output sent back is a no-op (CC4).
func transformIdentity(curBox geom.Box, s *ObjectSetSpec, scale float64) bool {
	if s.Box != nil {
		cur := [4]float64{curBox.X, curBox.Y, curBox.W, curBox.H}
		for i := range cur {
			if geom.FormatNum(s.Box[i]*scale) != geom.FormatNum(cur[i]) {
				return false
			}
		}
	}
	if s.Rot != nil && geom.FormatNum(geom.NormDeg(*s.Rot)) != geom.FormatNum(curBox.Rot) {
		return false
	}
	if s.FlipH != nil && *s.FlipH != curBox.FlipH {
		return false
	}
	if s.FlipV != nil && *s.FlipV != curBox.FlipV {
		return false
	}
	return true
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
// flip), and the effective cell it would replace. A materialise want
// is a master, style or inherited dependent (a LocPin whose effective
// formula is evaluable but not allowed): it is written as a local
// F="Inh" cell with its unit kept, and refused when its effective
// formula is not evaluable, instead of being left to RecomputeShape
// whose all-or-nothing failure on another dependent would leave it
// stale and the box wrong (CC4).
type transformWant struct {
	name        string
	value       float64
	cell        sheet.Cell
	present     bool
	materialise bool
	formula     string
}

// transformWants returns the cells a transform write to desired
// would write, in check order: PinX, PinY, Width, Height, Angle,
// FlipX, FlipY, then LocPinX and LocPinY when their effective formula
// is an allowed constant (so a constant LocPin scales with the size)
// or, when the master (or a style) holds them as an evaluable
// dependent, as materialise wants. A master-held LocPin whose
// formula is not evaluable is a materialise want too, so the write
// is refused in the formula form rather than left stale. A locally
// held LocPin with a dependent formula is left to RecomputeShape,
// which rewrites it keeping its formula or reports it stale.
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
		want, ok := transformLocWant(r, h, v.name, v.value, cell, origin, present)
		if !ok {
			continue
		}
		wants = append(wants, want)
	}
	return wants
}

// transformLocWant returns the want for a LocPin cell, and whether
// it may be written directly. An allowed effective formula (absent,
// a constant, or Inh resolving to an allowed formula) is a plain
// want, so a constant LocPin scales with the size. Otherwise, when
// the cell is held by the master, a style or an Inh cell (not
// locally), it is a materialise want carrying its effective
// formula: the caller writes it as F="Inh" when that formula is
// evaluable and refuses when it is not. A locally held dependent
// LocPin is not a want: RecomputeShape rewrites it keeping its
// formula or reports it stale.
func transformLocWant(r *inherit.Resolver, h *inherit.Shape, name string, value float64, cell sheet.Cell, origin inherit.Origin, present bool) (transformWant, bool) {
	want := transformWant{name: name, value: value, cell: cell, present: present}
	if !present {
		return transformWant{}, false
	}
	f := cell.F
	if origin == inherit.Inherited {
		master, ok := r.MasterShape(h)
		if !ok || master == nil || master.Sheet == nil {
			return want, true
		}
		mc, ok := master.Sheet.Cell(sheet.ShapeCell(name))
		if !ok {
			return want, true
		}
		f = mc.F
	}
	if formula.Allowed(f) {
		return want, true
	}
	if origin == inherit.Local {
		return transformWant{}, false
	}
	want.materialise = true
	want.formula = f
	return want, true
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
