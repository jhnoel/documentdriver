package ops

import (
	_ "embed"
	"fmt"

	"github.com/jhnoel/documentdriver/internal/ooxml/tree"
	"github.com/jhnoel/documentdriver/internal/pptx/derive"
	"github.com/jhnoel/documentdriver/internal/pptx/ops/edit"
	"github.com/jhnoel/documentdriver/internal/pptx/ops/prop"
)

// The group template, trimmed from a PowerPoint-authored deck: the
// LibreOffice test file sd/qa/unit/data/pptx/group.pptx (MPL-2.0; written
// by Microsoft Office PowerPoint 16.0000, listed powerpoint_native in
// $PPTX_CORPUS/manifest.jsonl, sha256 b8aedd7a...16bd), slide1: the p:grpSp
// "Group 1" (id 2), whose frame PowerPoint wrote with chOff equal to off
// and chExt equal to ext. Kept: the non-visual properties (cNvPr,
// cNvGrpSpPr, nvPr) and grpSpPr with its a:xfrm (off, ext, chOff,
// chExt). Removed: the children; the id, name and the frame's values are
// set by group.create.
//
//go:embed fragments/grpSp.xml
var grpSpTemplate []byte

// ErrInexpressible is returned when an operation's result cannot be
// stored so that it shows what the operation promises (a child of a
// skewing group being ungrouped, a group fill a child depends on).
var ErrInexpressible = prop.ErrInexpressible

// GroupCreate is group.create: the named objects, all in one container
// (the slide or one group), become the children of a new group, placed
// where the frontmost of them was. The group's frame encloses the
// children's boxes (derive.BoundsBox, the group-frame hazard's rule) with
// chOff and chExt equal to off and ext, so every child keeps its stored
// transform and its slide-space box exactly; children keep their ids, so
// glue to them is unchanged.
type GroupCreate struct {
	Slide string
	Spec  GroupCreateSpec
}

// Name implements Op.
func (*GroupCreate) Name() string { return "group.create" }

// apply groups the named objects into a new group with the enclosing frame.
func (o *GroupCreate) apply(t *Tx) error {
	part, err := t.slidePart(o.Slide)
	if err != nil {
		return err
	}
	var refs []edit.Ref
	var hs []*edit.Handle
	for _, id := range o.Spec.Targets {
		ref, err := t.resolve(o.Slide, id)
		if err != nil {
			return err
		}
		h, err := t.ws.Peek(ref)
		if err != nil {
			return err
		}
		refs = append(refs, ref)
		hs = append(hs, h)
	}
	for _, h := range hs {
		for i := range h.Branches() {
			if takesGroupFill(h.Elem(i)) {
				return fmt.Errorf("%w: %s takes its group's fill (a:grpFill), which a new group would change", ErrInexpressible, h.Ref().Short())
			}
		}
	}
	b, err := prop.FrameBounds(hs)
	if err != nil {
		return err
	}
	g, err := t.ws.GroupObjects(part, refs, grpSpTemplate, templateNS)
	if err != nil {
		return err
	}
	if err := t.bind(o.Spec.Alias, g); err != nil {
		return err
	}
	h, err := t.ws.Target(g)
	if err != nil {
		return err
	}
	if err := prop.Set(h, prop.Name(), o.Spec.Name.V); err != nil {
		return err
	}
	f := derive.StoredGroupXfrm{
		StoredXfrm: derive.StoredXfrm{X: derive.EMU(b.X0), Y: derive.EMU(b.Y0), W: derive.EMU(b.X1 - b.X0), H: derive.EMU(b.Y1 - b.Y0)},
		ChX:        derive.EMU(b.X0), ChY: derive.EMU(b.Y0), ChW: derive.EMU(b.X1 - b.X0), ChH: derive.EMU(b.Y1 - b.Y0),
	}
	return prop.SetGroupFrame(h, []derive.StoredGroupXfrm{f})
}

// GroupUngroup is group.ungroup: the group's children move to the
// group's container, at the group's place in the paint order, and the
// group is removed. Each child's transform is baked into the container's
// space (derive.Place under the group, derive.Unplace under the
// container), so it keeps its slide-space box, rotation and flips; ids
// are unchanged. It fails with ErrInexpressible when a child cannot be
// expressed (a skewed image) or when removing the group would change
// what is drawn: a hidden group, group effects or 3D, or a child that
// takes the group's fill (a:grpFill).
type GroupUngroup struct {
	Slide, Target string
}

// Name implements Op.
func (*GroupUngroup) Name() string { return "group.ungroup" }

// apply dissolves the group, moving its children to the group's container.
func (o *GroupUngroup) apply(t *Tx) error {
	ref, err := t.writable(o.Slide, o.Target)
	if err != nil {
		return err
	}
	if k, _ := t.ws.Kind(ref); k != "grpSp" {
		return fmt.Errorf("%s is not a group", ref.Short())
	}
	gh, err := t.ws.Target(ref)
	if err != nil {
		return err
	}
	if err := ungroupable(gh.Elem(0)); err != nil {
		return fmt.Errorf("%s: %w", ref.Short(), err)
	}
	to, err := gh.ParentMap(0)
	if err != nil {
		// The container's own space is not derived (a degenerate
		// enclosing group): the children cannot be stated in it.
		return fmt.Errorf("%s: %w: %v", ref.Short(), ErrInexpressible, err)
	}
	kids, err := t.ws.Children(ref)
	if err != nil {
		return err
	}
	type bake struct {
		h      *edit.Handle
		stored []derive.StoredXfrm
	}
	var bakes []bake
	for _, k := range kids {
		kind, _ := t.ws.Kind(k)
		ch, err := t.ws.Target(k)
		if err != nil {
			return err
		}
		stored, same, err := prop.Bake(ch, to)
		if err != nil {
			return err
		}
		if same {
			// Already what it would store (an identity group map):
			// nothing written, not even an inherited transform.
			continue
		}
		if what, ro := readOnlyKinds[kind]; ro {
			// A read-only child may move with its container, but its
			// own XML is never written.
			return fmt.Errorf("%w: %s is %s, whose transform ungrouping would rewrite", ErrReadOnlyObject, k.Short(), what)
		}
		bakes = append(bakes, bake{ch, stored})
	}
	for _, b := range bakes {
		if err := prop.WriteStored(b.h, b.stored); err != nil {
			return fmt.Errorf("%s: %w", b.h.Ref().Short(), err)
		}
	}
	return t.ws.Ungroup(ref)
}

// ungroupable refuses groups whose removal would change what is drawn,
// which the tool cannot carry over to the children without deciding.
func ungroupable(g *tree.Node) error {
	if nv := derive.Child(g, derive.PML, "nvGrpSpPr"); nv != nil {
		if c := derive.Child(nv, derive.PML, "cNvPr"); c != nil {
			if v, err := derive.AttrBool(c, "hidden"); err == nil && v {
				return fmt.Errorf("%w: the group is hidden; ungrouping would show its children", ErrInexpressible)
			}
		}
	}
	if pr := derive.Child(g, derive.PML, "grpSpPr"); pr != nil {
		for _, e := range []string{"effectLst", "effectDag", "scene3d"} {
			if el := derive.Child(pr, derive.DML, e); el != nil && (e != "effectLst" || len(el.Elements()) > 0) {
				return fmt.Errorf("%w: the group has a:%s, which applies to the group as a whole", ErrInexpressible, e)
			}
		}
	}
	// A child (or its text) that takes the group's fill.
	for _, c := range g.Elements() {
		objs := []*tree.Node{c}
		if derive.Is(c, derive.MC, "AlternateContent") {
			objs = nil
			for _, b := range c.Elements() {
				objs = append(objs, b.Elements()...)
			}
		}
		for _, o := range objs {
			if derive.In(o, derive.PML) && takesGroupFill(o) {
				return fmt.Errorf("%w: a child takes the group's fill (a:grpFill); ungrouping would change it", ErrInexpressible)
			}
		}
	}
	return nil
}

// takesGroupFill reports whether an object takes its enclosing group's
// fill: a group whose own fill is a:grpFill, or any other object with an
// a:grpFill anywhere (shape fill, text). Children of a nested group refer
// to that group, not to the one around it.
func takesGroupFill(o *tree.Node) bool {
	if derive.Is(o, derive.PML, "grpSp") {
		pr := derive.Child(o, derive.PML, "grpSpPr")
		return pr != nil && derive.Child(pr, derive.DML, "grpFill") != nil
	}
	found := false
	o.Walk(func(n *tree.Node) bool {
		if n.Kind() == tree.ElementNode && derive.Is(n, derive.DML, "grpFill") {
			found = true
		}
		return !found
	})
	return found
}

// GroupFit is group.fit: the group's frame is recomputed from its
// children (their bounds in the group's child space become chOff and
// chExt; off and ext become those bounds' image under the group's
// current map), so that no child moves (within prop.FitTolerance, the
// rounding of the frame to whole EMU). Rotation and flips stay. A frame
// that already fits is left as it is.
type GroupFit struct {
	Slide, Target string
}

// Name implements Op.
func (*GroupFit) Name() string { return "group.fit" }

// apply refits the group's frame to its children, leaving a fitting frame unchanged.
func (o *GroupFit) apply(t *Tx) error {
	ref, err := t.writable(o.Slide, o.Target)
	if err != nil {
		return err
	}
	if k, _ := t.ws.Kind(ref); k != "grpSp" {
		return fmt.Errorf("%s is not a group", ref.Short())
	}
	h, err := t.ws.Target(ref)
	if err != nil {
		return err
	}
	frames, changed, err := prop.FitFrames(h)
	if err != nil {
		return fmt.Errorf("%s: %w", ref.Short(), err)
	}
	if !changed {
		return nil
	}
	return prop.SetGroupFrame(h, frames)
}
