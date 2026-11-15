package prop

import (
	"errors"
	"fmt"
	"math"
	"strconv"

	"github.com/jhnoel/documentdriver/internal/ooxml/tree"
	"github.com/jhnoel/documentdriver/internal/pptx/derive"
	"github.com/jhnoel/documentdriver/internal/pptx/ops/edit"
)

// Bounds is an axis-aligned box in whole EMU of some coordinate space.
type Bounds struct{ X0, Y0, X1, Y1 int64 }

// FrameBounds returns the frame that encloses the objects' stored boxes in
// their (common) parent's coordinate space: the union of each box with
// width and height exchanged about its centre when its rotation is within
// 45 degrees of 90 or 270 (derive.BoundsBox, the rule PowerPoint's saved
// groups follow and the group-frame hazard checks), widened to whole EMU.
func FrameBounds(hs []*edit.Handle) (Bounds, error) {
	var xs []derive.Xfrm
	for _, h := range hs {
		tr := transformRead(h, 0)
		if tr.StoredErr != nil {
			return Bounds{}, fmt.Errorf("%s: %w: %v", h.Ref(), ErrNotDerived, tr.StoredErr)
		}
		xs = append(xs, tr.Stored.Float())
	}
	return boundsOf(xs)
}

// boundsOf returns the widened union bounds of the given transforms.
func boundsOf(xs []derive.Xfrm) (Bounds, error) {
	if len(xs) == 0 {
		return Bounds{}, errors.New("no children")
	}
	x0, y0 := math.Inf(1), math.Inf(1)
	x1, y1 := math.Inf(-1), math.Inf(-1)
	for _, x := range xs {
		b := derive.BoundsBox(x)
		x0, y0 = math.Min(x0, b.X), math.Min(y0, b.Y)
		x1, y1 = math.Max(x1, b.X+b.W), math.Max(y1, b.Y+b.H)
	}
	return Bounds{int64(math.Floor(x0)), int64(math.Floor(y0)), int64(math.Ceil(x1)), int64(math.Ceil(y1))}, nil
}

var (
	pathGrpXfrm = edit.Path{edit.P("grpSpPr"), edit.A("xfrm")}
)

// SetGroupFrame makes branch i of a group store frames[i] (offset,
// extent and child space; rotation and flips are not touched), writing
// only the attributes whose values differ. Every value is checked
// against its schema type.
func SetGroupFrame(h *edit.Handle, frames []derive.StoredGroupXfrm) error {
	if len(frames) != h.Branches() {
		return fmt.Errorf("%w: %d frames for %d branches", edit.ErrBranch, len(frames), h.Branches())
	}
	edits := make([][]edit.AttrEdit, h.Branches())
	for i, f := range frames {
		x := h.FindIn(i, pathGrpXfrm)
		if x == nil {
			return errGroupNoXfrm
		}
		for _, a := range []struct {
			el, attr, typ string
			v             derive.EMU
		}{
			{"off", "x", "a:CT_Point2D", f.X}, {"off", "y", "a:CT_Point2D", f.Y},
			{"ext", "cx", "a:CT_PositiveSize2D", f.W}, {"ext", "cy", "a:CT_PositiveSize2D", f.H},
			{"chOff", "x", "a:CT_Point2D", f.ChX}, {"chOff", "y", "a:CT_Point2D", f.ChY},
			{"chExt", "cx", "a:CT_PositiveSize2D", f.ChW}, {"chExt", "cy", "a:CT_PositiveSize2D", f.ChH},
		} {
			v := strconv.FormatInt(int64(a.v), 10)
			if err := edit.CheckAttr(a.typ, a.attr, v); err != nil {
				return fmt.Errorf("%w: group frame %s@%s: %w", ErrInexpressible, a.el, a.attr, err)
			}
			el := derive.Child(x, derive.DML, a.el)
			if el == nil {
				return fmt.Errorf("group transform has no a:%s", a.el)
			}
			if cur, ok := el.Attr(a.attr); ok && cur == v {
				continue
			}
			p := append(append(edit.Path{}, pathGrpXfrm...), edit.A(a.el))
			edits[i] = append(edits[i], edit.AttrEdit{Path: p, Name: a.attr, Value: v})
		}
	}
	return h.ApplyEach(edits)
}

// Bake returns, for each branch of an object about to leave its group
// for a container whose child-to-slide map is to, the stored transform
// that keeps its slide-space box, rotation and flips (derive.Place under
// its current groups, then derive.Unplace under to, rounded to whole
// EMU), and whether every branch already stores (or inherits) exactly
// that, so nothing need be written. It fails with ErrInexpressible when
// the object's image is not a box (skew) or cannot be stored under to.
func Bake(h *edit.Handle, to derive.Parent) ([]derive.StoredXfrm, bool, error) {
	out := make([]derive.StoredXfrm, h.Branches())
	same, bare := true, false
	for i := range h.Branches() {
		tr := transformRead(h, i)
		switch {
		case tr.StoredErr != nil:
			// No transform: nothing to write when the group maps its
			// child space onto its container's exactly; otherwise where
			// the object is cannot be stated in the container's space.
			if pm, err := h.ParentMap(i); err == nil && pm == to {
				bare = true
				continue
			}
			return nil, false, fmt.Errorf("%s: %w (%w: %v)", h.Ref(), ErrInexpressible, ErrNotDerived, tr.StoredErr)
		case tr.PlaceErr != nil:
			return nil, false, fmt.Errorf("%s: %w: %v", h.Ref(), ErrInexpressible, tr.PlaceErr)
		}
		local, err := derive.Unplace(tr.Placed.Box, to)
		if err != nil {
			return nil, false, fmt.Errorf("%s: %w", h.Ref(), err)
		}
		out[i] = round(local)
		same = same && out[i] == tr.Stored
		// The stored box must draw where the object is drawn now: a
		// child whose image under its groups is slightly skewed (within
		// derive.SkewTolerance, which reading accepts) would be squared.
		old, nw := tr.Stored.Float(), out[i].Float()
		m2 := nw.Matrix().Then(to.M)
		for k, c := range [][2]float64{{0, 0}, {1, 0}, {1, 1}, {0, 1}} {
			a := tr.Placed.M.Apply(derive.Pt{X: c[0] * old.W, Y: c[1] * old.H})
			b := m2.Apply(derive.Pt{X: c[0] * nw.W, Y: c[1] * nw.H})
			if d := math.Hypot(a.X-b.X, a.Y-b.Y); d > BakeTolerance {
				return nil, false, fmt.Errorf("%s: %w: outside its group it would move by %.3f pt at corner %d (its image under the group is not exactly a box: skew %.3g degrees)",
					h.Ref(), ErrInexpressible, d/derive.EMUPerPoint, k, tr.Placed.Skew)
			}
		}
	}
	if bare && !same {
		return nil, false, fmt.Errorf("%s: %w: a branch has no transform while another moves", h.Ref(), ErrInexpressible)
	}
	return out, same, nil
}

// BakeTolerance is how far, in EMU, a corner of an object may move when
// group.ungroup stores its transform in the container's space: half of
// what the views show (0.01 pt), so that rounding to whole EMU passes and
// squaring a skewed image does not.
const BakeTolerance = 0.005 * derive.EMUPerPoint

// WriteStored makes branch i of the object store stored[i] (offset,
// extent, rotation and flips), writing only what changes; an object
// without a transform of its own gets one.
func WriteStored(h *edit.Handle, stored []derive.StoredXfrm) error { return writeStored(h, stored) }

// FitTolerance is how far, in EMU, group.fit may move a child: the new
// frame is stored in whole EMU, so its map differs from the old one by
// rounding (at most half an EMU per coordinate, at the frame's far
// corner). It is far below what the views show (0.01 pt is 127 EMU).
const FitTolerance = 2.0

// ErrNoChildren is returned for a group without child objects.
var ErrNoChildren = errors.New("prop: the group has no child objects")

// FitFrames computes, for each branch of a group, the frame group.fit
// stores: the child space becomes the bounds of the children (FrameBounds'
// rule, in the group's child space), and the offset and extent the image
// of those bounds under the group's current map, so that every child
// keeps its slide-space place (within FitTolerance). Rotation and flips
// stay. It also reports whether any branch changes.
func FitFrames(h *edit.Handle) ([]derive.StoredGroupXfrm, bool, error) {
	out := make([]derive.StoredGroupXfrm, h.Branches())
	changed := false
	for i := range h.Branches() {
		g := h.Elem(i)
		old, err := derive.ReadGroupXfrm(derive.XfrmElem(g))
		if err != nil {
			return nil, false, fmt.Errorf("%w: %v", ErrNotDerived, err)
		}
		xs, err := childXfrms(g)
		if err != nil {
			return nil, false, err
		}
		if len(xs) == 0 {
			return nil, false, ErrNoChildren
		}
		b, _ := boundsOf(xs)
		gf := old.Float()
		m, err := gf.ChildMatrix()
		if err != nil {
			return nil, false, fmt.Errorf("%w: %v", ErrInexpressible, err)
		}
		sx, sy := 1.0, 1.0
		if gf.ChW != 0 {
			sx = gf.W / gf.ChW
		}
		if gf.ChH != 0 {
			sy = gf.H / gf.ChH
		}
		bw, bh := float64(b.X1-b.X0), float64(b.Y1-b.Y0)
		c := m.Apply(derive.Pt{X: float64(b.X0) + bw/2, Y: float64(b.Y0) + bh/2})
		w := derive.RoundEMU(bw * sx)
		hh := derive.RoundEMU(bh * sy)
		nf := old
		nf.X = derive.RoundEMU(c.X - float64(w)/2)
		nf.Y = derive.RoundEMU(c.Y - float64(hh)/2)
		nf.W, nf.H = w, hh
		nf.ChX, nf.ChY = derive.EMU(b.X0), derive.EMU(b.Y0)
		nf.ChW, nf.ChH = derive.EMU(b.X1-b.X0), derive.EMU(b.Y1-b.Y0)
		m2, err := nf.Float().ChildMatrix()
		if err != nil {
			return nil, false, fmt.Errorf("%w: %v", ErrInexpressible, err)
		}
		for _, p := range []derive.Pt{{X: float64(b.X0), Y: float64(b.Y0)}, {X: float64(b.X1), Y: float64(b.Y0)}, {X: float64(b.X1), Y: float64(b.Y1)}, {X: float64(b.X0), Y: float64(b.Y1)}} {
			a, z := m.Apply(p), m2.Apply(p)
			if d := math.Hypot(a.X-z.X, a.Y-z.Y); d > FitTolerance {
				return nil, false, fmt.Errorf("%w: refitting would move children by %.2f EMU", ErrInexpressible, d)
			}
		}
		if nf != old {
			changed = true
		}
		out[i] = nf
	}
	return out, changed, nil
}

// childXfrms reads the stored transforms of a group element's direct
// children (the projected branch of a child mc:AlternateContent).
func childXfrms(g *tree.Node) ([]derive.Xfrm, error) {
	var out []derive.Xfrm
	add := func(e *tree.Node) error {
		s, err := derive.ReadXfrm(derive.XfrmElem(e))
		if err != nil {
			return fmt.Errorf("%w: a child %s: %v", ErrNotDerived, e.Name(), err)
		}
		out = append(out, s.Float())
		return nil
	}
	for _, e := range g.Elements() {
		switch {
		case derive.Is(e, derive.MC, "AlternateContent"):
			b := derive.SelectBranch(e)
			if b.Node == nil {
				continue
			}
			for _, c := range b.Node.Elements() {
				if isObjectElem(c) {
					if err := add(c); err != nil {
						return nil, err
					}
				}
			}
		case isObjectElem(e):
			if err := add(e); err != nil {
				return nil, err
			}
		}
	}
	return out, nil
}

// isObjectElem reports whether e is a slide object element.
func isObjectElem(e *tree.Node) bool {
	switch e.Local() {
	case "sp", "cxnSp", "grpSp", "pic", "graphicFrame":
		return derive.In(e, derive.PML)
	case "contentPart":
		return true
	}
	return false
}
