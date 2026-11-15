package derive

import (
	"errors"
	"fmt"

	"github.com/jhnoel/documentdriver/internal/ooxml/tree"
)

// StoredXfrm is a transform exactly as stored: offset and extent in whole
// EMU, rotation in 60000ths of a degree, and flips.
type StoredXfrm struct {
	X, Y, W, H   EMU
	Rot          Angle
	FlipH, FlipV bool
}

// Float returns the transform for arithmetic, rotation in degrees.
func (s StoredXfrm) Float() Xfrm {
	return Xfrm{X: float64(s.X), Y: float64(s.Y), W: float64(s.W), H: float64(s.H),
		Rot: s.Rot.Degrees(), FlipH: s.FlipH, FlipV: s.FlipV}
}

// StoredGroupXfrm is a group's stored transform with its child space.
type StoredGroupXfrm struct {
	StoredXfrm
	ChX, ChY, ChW, ChH EMU
}

// Float returns the group transform for arithmetic.
func (g StoredGroupXfrm) Float() GroupXfrm {
	return GroupXfrm{Xfrm: g.StoredXfrm.Float(), ChX: float64(g.ChX), ChY: float64(g.ChY), ChW: float64(g.ChW), ChH: float64(g.ChH)}
}

// XfrmElem returns an object's own transform element: a:xfrm in p:spPr or
// p:grpSpPr, else a direct xfrm child (p:xfrm of a graphic frame, p14:xfrm
// of a content part). It returns nil when the object stores none.
func XfrmElem(obj *tree.Node) *tree.Node {
	if obj == nil {
		return nil
	}
	for _, pr := range []string{"spPr", "grpSpPr"} {
		if sp := Child(obj, PML, pr); sp != nil {
			return Child(sp, DML, "xfrm")
		}
	}
	for c := range obj.ElementsSeq() {
		if c.Local() == "xfrm" {
			return c
		}
	}
	return nil
}

// localChild returns the first child with the given local name.
func localChild(n *tree.Node, local string) *tree.Node {
	for c := range n.ElementsSeq() {
		if c.Local() == local {
			return c
		}
	}
	return nil
}

// ReadXfrm reads a transform element (CT_Transform2D or
// CT_GroupTransform2D). Offset and extent must be written and be whole
// numbers; rot, flipH and flipV take their schema defaults (0, false,
// false) only when not written, and fail when written but unreadable.
func ReadXfrm(x *tree.Node) (StoredXfrm, error) {
	var s StoredXfrm
	if x == nil {
		return s, fmt.Errorf("transform %w", ErrAbsent)
	}
	off, ext := localChild(x, "off"), localChild(x, "ext")
	if off == nil || ext == nil {
		return s, fmt.Errorf("%s has no a:off or a:ext", x.Name())
	}
	var err error
	if s.X, err = AttrEMU(off, "x"); err != nil {
		return s, err
	}
	if s.Y, err = AttrEMU(off, "y"); err != nil {
		return s, err
	}
	if s.W, err = AttrEMU(ext, "cx"); err != nil {
		return s, err
	}
	if s.H, err = AttrEMU(ext, "cy"); err != nil {
		return s, err
	}
	if s.W < 0 || s.H < 0 {
		return s, fmt.Errorf("%s has a negative extent", ext.Name())
	}
	rot, err := AttrAngle(x, "rot")
	if s.Rot, _, err = OrDefault(rot, err, 0); err != nil {
		return s, err
	}
	fh, err := AttrBool(x, "flipH")
	if s.FlipH, _, err = OrDefault(fh, err, false); err != nil {
		return s, err
	}
	fv, err := AttrBool(x, "flipV")
	if s.FlipV, _, err = OrDefault(fv, err, false); err != nil {
		return s, err
	}
	return s, nil
}

// ReadGroupXfrm reads a group's a:xfrm, child offset and extent included.
func ReadGroupXfrm(x *tree.Node) (StoredGroupXfrm, error) {
	var g StoredGroupXfrm
	var err error
	if g.StoredXfrm, err = ReadXfrm(x); err != nil {
		return g, err
	}
	cOff, cExt := localChild(x, "chOff"), localChild(x, "chExt")
	if cOff == nil || cExt == nil {
		return g, fmt.Errorf("group %s has no a:chOff or a:chExt", x.Name())
	}
	for _, a := range []struct {
		n    *tree.Node
		name string
		dst  *EMU
	}{{cOff, "x", &g.ChX}, {cOff, "y", &g.ChY}, {cExt, "cx", &g.ChW}, {cExt, "cy", &g.ChH}} {
		if *a.dst, err = AttrEMU(a.n, a.name); err != nil {
			return g, err
		}
	}
	return g, nil
}

// Link is one level of an object's inheritance chain: the object itself
// (FromSlide), then its matching layout and master placeholders.
type Link struct {
	Elem   *tree.Node
	Origin Origin
}

// TransformRead is an object's transform as read: the read half shared by
// the read model and the transform property codec, so the value shown
// and the value compared on write cannot diverge.
type TransformRead struct {
	// Stored is the stored transform in the parent's space, valid when
	// StoredErr is nil; Origin says which link of the chain it came from.
	Stored    StoredXfrm
	Origin    Origin
	StoredErr error
	// Placed is the slide-space transform, valid when both errors are
	// nil.
	Placed   Placed
	PlaceErr error
}

// ErrNoTransform is the reason given when neither an object nor its
// placeholders store a transform.
var ErrNoTransform = errors.New("no transform stored on the object or its placeholders")

// ReadTransform reads the transform of the object at chain[0] under the
// parent map p (pErr set when the parent map could not be derived). An
// object without a transform element inherits one from the first later
// link that has one (placeholders); a transform element that is present
// but unreadable is reported, never skipped.
func ReadTransform(chain []Link, p Parent, pErr error) TransformRead {
	var r TransformRead
	if len(chain) == 0 {
		r.StoredErr = ErrNoTransform
		return r
	}
	r.StoredErr = ErrNoTransform
	for _, l := range chain {
		x := XfrmElem(l.Elem)
		if x == nil {
			continue
		}
		r.Stored, r.StoredErr = ReadXfrm(x)
		r.Origin = l.Origin
		break
	}
	if r.StoredErr != nil {
		return r
	}
	if pErr != nil {
		r.PlaceErr = fmt.Errorf("enclosing group: %w", pErr)
		return r
	}
	r.Placed, r.PlaceErr = Place(r.Stored.Float(), p)
	return r
}

// Box returns the slide-space box, or why there is none.
func (r TransformRead) Box() Val[Xfrm] {
	switch {
	case r.StoredErr != nil:
		return Missing[Xfrm](r.StoredErr.Error())
	case r.PlaceErr != nil:
		return Missing[Xfrm](r.PlaceErr.Error())
	}
	return Got(r.Placed.Box, r.Origin)
}
