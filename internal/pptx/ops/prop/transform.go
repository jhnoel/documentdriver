package prop

import (
	"errors"
	"fmt"
	"maps"
	"slices"
	"strconv"

	"github.com/jhnoel/documentdriver/internal/ooxml/tree"
	"github.com/jhnoel/documentdriver/internal/pptx/derive"
	"github.com/jhnoel/documentdriver/internal/pptx/ops/edit"
)

// Units select how lengths are shown: "pt" (the default) or "emu", as in
// the views.
type Units string

// Units.
const (
	Pt  Units = "pt"
	EMU Units = "emu"
)

// Length formats a length in EMU as the views show it in u.
func (u Units) Length(emu float64) string {
	if u == EMU {
		return derive.FormatNum(emu)
	}
	return derive.FormatNum(emu / derive.EMUPerPoint)
}

// ToEMU converts a length given in u to EMU.
func (u Units) ToEMU(v float64) float64 {
	if u == EMU {
		return v
	}
	return v * derive.EMUPerPoint
}

// ShowRot formats a rotation in degrees as the views show it, with a
// whole turn shown as 0.
func ShowRot(deg float64) string {
	s := derive.FormatNum(deg)
	if s == "360" {
		return "0"
	}
	return s
}

// Transform returns the transform codec: the object's slide-space box,
// rotation and flips (derive.Xfrm, lengths in EMU), shown in units u.
// Writes go through derive.Unplace for each branch's parent map, so they
// are exact under rotated and flipped groups or fail with
// ErrInexpressible.
func Transform(u Units) Codec[derive.Xfrm] {
	return Codec[derive.Xfrm]{
		Name:  "transform",
		Read:  readTransform,
		Write: writeTransform,
		Show: func(x derive.Xfrm) string {
			return "box=[" + u.Length(x.X) + " " + u.Length(x.Y) + " " + u.Length(x.W) + " " + u.Length(x.H) +
				"] rot=" + ShowRot(x.Rot) + " flipH=" + strconv.FormatBool(x.FlipH) + " flipV=" + strconv.FormatBool(x.FlipV)
		},
	}
}

// readTransform is the read half shared with the read model:
// derive.ReadTransform over the branch's inheritance chain and parent map.
func readTransform(h *edit.Handle, i int) derive.Val[derive.Xfrm] {
	return transformRead(h, i).Box()
}

// transformRead reads branch i's transform through its inheritance chain and parent map.
func transformRead(h *edit.Handle, i int) derive.TransformRead {
	chain, err := h.Chain(i)
	if err != nil {
		return derive.TransformRead{StoredErr: err}
	}
	pm, perr := h.ParentMap(i)
	return derive.ReadTransform(chain, pm, perr)
}

// xfrmPath returns the path from an object element to its own transform
// element, whether or not it exists yet.
func xfrmPath(e *tree.Node) (edit.Path, error) {
	for _, c := range e.Elements() {
		switch {
		case derive.Is(c, derive.PML, "spPr"):
			return edit.Path{edit.P("spPr"), edit.A("xfrm")}, nil
		case derive.Is(c, derive.PML, "grpSpPr"):
			return edit.Path{edit.P("grpSpPr"), edit.A("xfrm")}, nil
		case c.Local() == "xfrm": // p:xfrm of a graphic frame, p14:xfrm of a content part
			return edit.Path{edit.Q(c.Space(), c.Prefix(), "xfrm")}, nil
		}
	}
	return nil, fmt.Errorf("%s has no shape properties to hold a transform", e.Name())
}

// DropInvalid removes, from the object's own transform element in every
// branch, each named attribute (rot, flipH, flipV) whose stored text is
// not a value of its schema type. An operation calls it for the
// attributes it is about to set: a value the file already has wrong must
// not stop it from being overwritten, and once it is gone the transform
// reads with the attribute's default until the new value is written.
// Attributes the operation does not set are left as they are.
func DropInvalid(h *edit.Handle, names ...string) error {
	for i := range h.Branches() {
		path, err := xfrmPath(h.Elem(i))
		if err != nil {
			return nil //nolint:nilerr // no transform element: nothing stored to be wrong
		}
		x := h.FindIn(i, path)
		if x == nil {
			continue
		}
		ctype := "a:CT_Transform2D"
		if derive.Is(h.Elem(i), derive.PML, "grpSp") {
			ctype = "a:CT_GroupTransform2D"
		}
		for _, name := range names {
			v, ok := x.Attr(name)
			if !ok || edit.CheckAttr(ctype, name, v) == nil {
				continue
			}
			if err := h.RemoveAttr(path, name); err != nil {
				return err
			}
		}
	}
	return nil
}

// errGroupNoXfrm is returned when a group stores no transform: its child
// space cannot be derived, so none can be inserted.
var errGroupNoXfrm = errors.New("group stores no transform; its child space is unknown, so none is inserted")

// storedEdits returns the attribute edits that make the transform element
// at path store s, leaving every attribute that already reads as its
// target value untouched (including flipH="true" for true, and an absent
// rot, flipH or flipV for its schema default). A fresh element, just
// inserted, gets its offset and extent written in full.
func storedEdits(x *tree.Node, path edit.Path, s derive.StoredXfrm, fresh bool) ([]edit.AttrEdit, error) {
	var cur derive.StoredXfrm
	if !fresh {
		var err error
		if cur, err = derive.ReadXfrm(x); err != nil {
			return nil, err
		}
	}
	off := append(append(edit.Path{}, path...), edit.A("off"))
	ext := append(append(edit.Path{}, path...), edit.A("ext"))
	var out []edit.AttrEdit
	num := func(p edit.Path, name string, have, want int64) {
		if have != want || (fresh && name != "rot") {
			out = append(out, edit.AttrEdit{Path: p, Name: name, Value: strconv.FormatInt(want, 10)})
		}
	}
	num(off, "x", int64(cur.X), int64(s.X))
	num(off, "y", int64(cur.Y), int64(s.Y))
	num(ext, "cx", int64(cur.W), int64(s.W))
	num(ext, "cy", int64(cur.H), int64(s.H))
	num(path, "rot", int64(cur.Rot), int64(s.Rot))
	flag := func(name string, have, want bool) {
		if have != want {
			v := "0"
			if want {
				v = "1"
			}
			out = append(out, edit.AttrEdit{Path: path, Name: name, Value: v})
		}
	}
	flag("flipH", cur.FlipH, s.FlipH)
	flag("flipV", cur.FlipV, s.FlipV)
	// Every stored value in its schema type (a:ST_Coordinate,
	// a:ST_PositiveCoordinate, a:ST_Angle): a box or rotation that
	// rounds outside them cannot be stored.
	for _, e := range out {
		if err := edit.CheckAttr(xfrmAttrTypes[e.Name], e.Name, e.Value); err != nil {
			return nil, fmt.Errorf("%w: %w", ErrInexpressible, err)
		}
	}
	return out, nil
}

// xfrmAttrTypes are the complex types holding each attribute a
// transform write sets.
var xfrmAttrTypes = map[string]string{
	"x": "a:CT_Point2D", "y": "a:CT_Point2D", "cx": "a:CT_PositiveSize2D", "cy": "a:CT_PositiveSize2D",
	"rot": "a:CT_Transform2D", "flipH": "a:CT_Transform2D", "flipV": "a:CT_Transform2D",
}

// round stores a local transform in whole EMU and 60000ths of a degree.
func round(x derive.Xfrm) derive.StoredXfrm {
	rot := derive.RoundAngle(x.Rot) % (360 * derive.AngleUnit)
	if rot < 0 {
		rot += 360 * derive.AngleUnit
	}
	return derive.StoredXfrm{
		X: derive.RoundEMU(x.X), Y: derive.RoundEMU(x.Y), W: derive.RoundEMU(x.W), H: derive.RoundEMU(x.H),
		Rot: rot, FlipH: x.FlipH, FlipV: x.FlipV,
	}
}

// writeTransform writes the wanted slide-space transform by unplacing it into each branch.
func writeTransform(h *edit.Handle, want derive.Xfrm) error {
	if want.W < 0 || want.H < 0 {
		return fmt.Errorf("%w: negative extent", ErrInexpressible)
	}
	stored := make([]derive.StoredXfrm, h.Branches())
	for i := range h.Branches() {
		pm, err := h.ParentMap(i)
		if err != nil {
			return err
		}
		local, err := derive.Unplace(want, pm)
		if err != nil {
			return err
		}
		stored[i] = round(local)
	}
	return writeStored(h, stored)
}

// writeStored makes branch i store stored[i], inserting a transform
// element (schema-ordered, through Handle.Ensure) where the object has
// none of its own, as a placeholder that inherits its box.
func writeStored(h *edit.Handle, stored []derive.StoredXfrm) error {
	n := h.Branches()
	paths := make([]edit.Path, n)
	fresh := make([]bool, n)
	// Branches without a transform element, grouped by the element to
	// insert (a branch may be a different kind of object: an ink
	// contentPart's Fallback is a picture).
	parents := map[string][]edit.Path{}
	names := map[string]edit.Name{}
	for i := range n {
		path, err := xfrmPath(h.Elem(i))
		if err != nil {
			return err
		}
		paths[i] = path
		if h.FindIn(i, path) != nil {
			continue
		}
		if h.Elem(i).Local() == "grpSp" {
			return errGroupNoXfrm
		}
		fresh[i] = true
		x := path[len(path)-1]
		k := x.String()
		if parents[k] == nil {
			parents[k] = make([]edit.Path, n)
		}
		parents[k][i], names[k] = path[:len(path)-1], x
	}
	for _, k := range slices.Sorted(maps.Keys(parents)) {
		ps := parents[k]
		if err := h.EnsureEach(ps, names[k]); err != nil {
			return err
		}
		xs := make([]edit.Path, n)
		for i, p := range ps {
			if p != nil {
				xs[i] = paths[i]
			}
		}
		for _, c := range []edit.Name{edit.A("off"), edit.A("ext")} {
			if err := h.EnsureEach(xs, c); err != nil {
				return err
			}
		}
	}
	edits := make([][]edit.AttrEdit, n)
	for i := range n {
		var err error
		if edits[i], err = storedEdits(h.FindIn(i, paths[i]), paths[i], stored[i], fresh[i]); err != nil {
			return err
		}
	}
	return h.ApplyEach(edits)
}

// Move translates the object by (dx, dy) EMU in slide space, applied to
// the exact stored offset: in each branch the delta is mapped into the
// parent's child space and rounded to whole EMU there, so a move and its
// opposite cancel exactly, and repeated nudges never drift. It fails with
// ErrInexpressible unless every branch then shows the box moved by
// (dx, dy) in c's form.
func Move(h *edit.Handle, c Codec[derive.Xfrm], dx, dy float64) error {
	cur, ok := c.Read(h, 0).Get()
	if !ok {
		return fmt.Errorf("move: %w: %s", ErrNotDerived, c.Read(h, 0).Reason())
	}
	if dx == 0 && dy == 0 {
		return nil
	}
	stored := make([]derive.StoredXfrm, h.Branches())
	for i := range h.Branches() {
		pm, err := h.ParentMap(i)
		if err != nil {
			return err
		}
		inv, ok := pm.M.Invert()
		if !ok {
			return ErrInexpressible
		}
		d := inv.Vec(derive.Pt{X: dx, Y: dy})
		// The stored transform the box is read from: the object's own, or
		// the one it inherits (inserted on the object, moved).
		tr := transformRead(h, i)
		if tr.StoredErr != nil {
			if i == 0 {
				return fmt.Errorf("move: %w: %v", ErrNotDerived, tr.StoredErr)
			}
			// A branch with no transform at all (a stripped mc:Fallback):
			// it gets the moved box, as a set would give it.
			local, err := derive.Unplace(derive.Xfrm{X: cur.X + dx, Y: cur.Y + dy, W: cur.W, H: cur.H, Rot: cur.Rot, FlipH: cur.FlipH, FlipV: cur.FlipV}, pm)
			if err != nil {
				return err
			}
			stored[i] = round(local)
			continue
		}
		s := tr.Stored
		s.X += derive.RoundEMU(d.X)
		s.Y += derive.RoundEMU(d.Y)
		stored[i] = s
	}
	if err := writeStored(h, stored); err != nil {
		return err
	}
	want := cur
	want.X += dx
	want.Y += dy
	return check(h, c, want)
}
