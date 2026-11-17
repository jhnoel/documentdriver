package edit

import (
	"errors"
	"fmt"

	core "github.com/jhnoel/documentdriver/internal/ooxml/edit"
	"github.com/jhnoel/documentdriver/internal/vsdx/vsd"
)

// ErrDifferentParent is returned (wrapped, with both addresses) when
// a reorder names an anchor in a different Shapes parent than the
// shape being moved: reorder never moves a shape into another group.
var ErrDifferentParent = errors.New("vsdx edit: different parent")

// ReorderShape moves the shape at at in paint order within its own
// Shapes parent: to "front" (last, painted on top), "back" (first),
// or immediately before or after the shape at to (position "before"
// or "after"). It returns whether the shape moved: a move to where
// the shape already stands changes and declares nothing. A shape or
// anchor the page does not have is an ErrNotFound error, an anchor in
// another parent (or on another page) is an ErrDifferentParent error,
// and a page part that cannot be read is an Unreadable error.
func (w *Workspace) ReorderShape(at, to, position string) (bool, error) {
	a, err := vsd.Parse(at)
	if err != nil {
		return false, fmt.Errorf("vsdx edit: %w", err)
	}
	if a.Kind != vsd.KindShape || a.InMaster {
		return false, fmt.Errorf("vsdx edit: %s: want a page shape address (page0/s5)", at)
	}
	page := vsd.Address{Kind: vsd.KindPage, Container: a.Container, PageDup: a.PageDup}
	_, part, d, err := w.pageTree(page)
	if err != nil {
		return false, err
	}
	idx := w.shapeIndexFor(part, d.Root())
	moving := idx.find(a.Shape, a.Dup)
	if moving == nil {
		return false, fmt.Errorf("%w: %s", ErrNotFound, at)
	}
	container := moving.Parent()
	if container == nil || !isElem(container, vsd.ElemShapes) {
		return false, fmt.Errorf("vsdx edit: %s has no Shapes parent", at)
	}
	var pos core.Position
	switch to {
	case "front", "back":
		if position != "" {
			return false, fmt.Errorf("vsdx edit: position goes with a shape in to, not front or back")
		}
		pos, err = w.core.SlotEnd(container, "v:Shapes", vsdxNS.Name(vsd.ElemShape), to == "front", moving, nil)
		if err != nil {
			return false, fmt.Errorf("vsdx edit: %w", err)
		}
	default:
		b, err := vsd.Parse(to)
		if err != nil {
			return false, fmt.Errorf("vsdx edit: %w", err)
		}
		if b.Kind != vsd.KindShape || b.InMaster {
			return false, fmt.Errorf("vsdx edit: %s: want a page shape address (page0/s5)", to)
		}
		if b.Container != a.Container || b.PageDup != a.PageDup {
			return false, fmt.Errorf("%w: %s and %s are in different parents", ErrDifferentParent, at, to)
		}
		anchor := idx.find(b.Shape, b.Dup)
		if anchor == nil {
			return false, fmt.Errorf("%w: %s", ErrNotFound, to)
		}
		if anchor == moving {
			return false, fmt.Errorf("%w: %s cannot be placed relative to itself", ErrDifferentParent, to)
		}
		if anchor.Parent() != container {
			return false, fmt.Errorf("%w: %s and %s are in different parents", ErrDifferentParent, at, to)
		}
		switch position {
		case "before":
			pos = core.Before(anchor)
		case "after":
			pos = core.After(anchor, moving)
		default:
			return false, fmt.Errorf("vsdx edit: position (before or after) is needed with a shape in to")
		}
	}
	if pos.Container() != container {
		return false, fmt.Errorf("%w: %s and %s are in different parents", ErrDifferentParent, at, to)
	}
	if pos.Holds(moving) {
		return false, nil
	}
	if err := w.core.Move(part, moving, pos); err != nil {
		return false, err
	}
	// The move changes the document order (and with it duplicate-ID
	// numbering and the cached last shape of the parent): drop the
	// cached index so the next lookup rebuilds it from the page in
	// one walk, instead of patching its order by hand.
	delete(w.shapes, part)
	return true, nil
}
