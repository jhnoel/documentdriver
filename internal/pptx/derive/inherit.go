package derive

import (
	"math"

	"github.com/jhnoel/documentdriver/internal/ooxml/tree"
)

// PhRef is a placeholder reference (p:ph) as written.
type PhRef struct {
	Type    string // "" when absent
	Idx     string // "" when absent
	HasType bool
}

// ReadPh reads the p:ph of a shape's non-visual properties, if any. sp is
// the object element (p:sp, p:pic, p:graphicFrame...).
func ReadPh(sp *tree.Node) (PhRef, bool) {
	for nv := range sp.ElementsSeq() {
		if !In(nv, PML) || len(nv.Local()) < 3 || nv.Local()[:2] != "nv" {
			continue
		}
		ph := Path(nv, PML, "nvPr", "ph")
		if ph == nil {
			return PhRef{}, false
		}
		var r PhRef
		r.Type, r.HasType = ph.Attr("type")
		r.Idx, _ = ph.Attr("idx")
		return r, true
	}
	return PhRef{}, false
}

// EffType is the placeholder type with the schema default applied: a p:ph
// without a type attribute is "obj" (ECMA-376 ST_PlaceholderType).
func (r PhRef) EffType() string {
	if r.Type == "" {
		return "obj"
	}
	return r.Type
}

// matchTypes returns the first and second placeholder types to look for
// when resolving a placeholder of type t against a layout or master.
func matchTypes(t string) (string, string) {
	switch t {
	case "ctrTitle":
		return "ctrTitle", "title"
	case "subTitle":
		return "subTitle", "body"
	case "obj":
		return "obj", "body"
	}
	return t, ""
}

// FindPlaceholder finds the placeholder in spTree (a layout's or master's
// shape tree, searched including groups) that the placeholder ref
// inherits from. A placeholder without a type takes the type of the
// placeholder with the same idx in spTree, if there is one.
//
// The priority follows LibreOffice's importer (oox/source/ppt,
// PPTShape::findPlaceholder); PowerPoint's own rule is not documented:
// same idx and first type, then first type, then same idx and second type,
// then second type, then same idx alone. First and second types are
// ctrTitle/title, subTitle/body, obj/body, otherwise the type itself.
func FindPlaceholder(spTree *tree.Node, ref PhRef) *tree.Node {
	return findPlaceholder(spTree, ref)
}

// PlaceholderChain returns an object's inheritance chain: the object
// itself (FromSlide), then for a placeholder its matching layout
// placeholder (FromLayout) and master placeholder (FromMaster), each when
// found. The master is matched with the layout placeholder's own
// reference when there is one. ref is the last reference matched (the
// layout's when found), and ok reports whether the object is a
// placeholder at all.
func PlaceholderChain(obj, layoutTree, masterTree *tree.Node) (chain []Link, ref PhRef, ok bool) {
	chain = []Link{{obj, FromSlide}}
	ref, ok = ReadPh(obj)
	if !ok {
		return chain, ref, false
	}
	if l := FindPlaceholder(layoutTree, ref); l != nil {
		chain = append(chain, Link{l, FromLayout})
		if lr, lok := ReadPh(l); lok {
			ref = lr
		}
	}
	if m := FindPlaceholder(masterTree, ref); m != nil {
		chain = append(chain, Link{m, FromMaster})
	}
	return chain, ref, true
}

// findPlaceholder returns the best-matching placeholder for ref in spTree.
func findPlaceholder(spTree *tree.Node, ref PhRef) *tree.Node {
	if spTree == nil {
		return nil
	}
	type cand struct {
		node *tree.Node
		ph   PhRef
	}
	var all []cand
	var walk func(n *tree.Node)
	walk = func(n *tree.Node) {
		for c := range n.ElementsSeq() {
			if Is(c, PML, "grpSp") {
				walk(c)
				continue
			}
			if Is(c, MC, "AlternateContent") {
				if b := SelectBranch(c); b.Node != nil {
					walk(b.Node)
				}
				continue
			}
			if r, ok := ReadPh(c); ok {
				all = append(all, cand{c, r})
			}
		}
	}
	walk(spTree)
	t := ref.EffType()
	if !ref.HasType && ref.Idx != "" {
		for _, c := range all {
			if c.ph.Idx == ref.Idx {
				t = c.ph.EffType()
				break
			}
		}
	}
	first, second := matchTypes(t)
	var best [5]*tree.Node
	for _, c := range all {
		sameIdx := c.ph.Idx == ref.Idx
		ct := c.ph.EffType()
		prio := -1
		switch {
		case sameIdx && ct == first:
			prio = 0
		case ct == first:
			prio = 1
		case second != "" && sameIdx && ct == second:
			prio = 2
		case second != "" && ct == second:
			prio = 3
		case sameIdx && ref.Idx != "":
			prio = 4
		}
		if prio >= 0 && best[prio] == nil { //nolint:gosec // G602: index is bounds-checked above
			best[prio] = c.node //nolint:gosec // G602: index is bounds-checked above
		}
	}
	for _, n := range best {
		if n != nil {
			return n
		}
	}
	return nil
}

// MasterTextStyle names the master p:txStyles child that holds the text
// defaults of a placeholder type: titleStyle for title and ctrTitle,
// bodyStyle for body, subTitle and obj (and the content types chart, tbl,
// dgm, media, clipArt, pic, which are body placeholders), otherStyle for
// the rest (dt, ftr, sldNum, hdr).
func MasterTextStyle(phType string) string {
	switch phType {
	case "title", "ctrTitle":
		return "titleStyle"
	case "body", "subTitle", "obj", "chart", "tbl", "dgm", "media", "clipArt", "pic":
		return "bodyStyle"
	}
	return "otherStyle"
}

// GlueTolerance is the distance, in EMU, within which a glued connector
// end counts as lying on its connection site. See StaleGlue.
const GlueTolerance = 0.5 * 12700

// StaleGlue reports whether a glued end at end is off its site at site by
// more than GlueTolerance.
func StaleGlue(end, site Pt) (float64, bool) {
	d := math.Hypot(end.X-site.X, end.Y-site.Y)
	return d, d > GlueTolerance
}

// GroupFrameTolerance is the distance, in EMU, by which a child may stick
// out of its group's frame before a group-frame hazard is reported.
const GroupFrameTolerance = 0.5 * 12700

// Corners returns the four corners of a placed box in slide space.
func Corners(p Placed, w, h float64) [4]Pt {
	return [4]Pt{p.M.Apply(Pt{0, 0}), p.M.Apply(Pt{w, 0}), p.M.Apply(Pt{w, h}), p.M.Apply(Pt{0, h})}
}

// BoundsBox is the axis-aligned box a rotated object occupies for group
// frame purposes: its stored box, with width and height exchanged about
// the centre when the rotation is within 45 degrees of 90 or 270. This is
// the rule groups saved by PowerPoint follow in the corpus: children at
// 270 degrees fill the frame with their exchanged extents, while children
// at 7 degrees stay inside it only by their unrotated boxes (their
// rotated corners stick out). PowerPoint's rule itself is not documented.
func BoundsBox(x Xfrm) Xfrm {
	r := math.Mod(x.Rot, 180)
	if r < 0 {
		r += 180
	}
	if r >= 45 && r < 135 {
		cx, cy := x.X+x.W/2, x.Y+x.H/2
		return Xfrm{X: cx - x.H/2, Y: cy - x.W/2, W: x.H, H: x.W}
	}
	return Xfrm{X: x.X, Y: x.Y, W: x.W, H: x.H}
}

// OutsideFrame reports how far, in the group's parent units (EMU at the
// top level), the child's BoundsBox (in the group's child space) extends
// beyond the group's child frame (chOff/chExt).
func OutsideFrame(child Xfrm, g GroupXfrm) float64 {
	child = BoundsBox(child)
	sx, err := axisScale(g.W, g.ChW)
	if err != nil {
		return 0
	}
	sy, err := axisScale(g.H, g.ChH)
	if err != nil {
		return 0
	}
	over := func(lo, hi, flo, fhi float64) float64 {
		return math.Max(0, math.Max(flo-lo, hi-fhi))
	}
	dx := over(child.X, child.X+child.W, g.ChX, g.ChX+g.ChW) * sx
	dy := over(child.Y, child.Y+child.H, g.ChY, g.ChY+g.ChH) * sy
	return math.Hypot(dx, dy)
}
