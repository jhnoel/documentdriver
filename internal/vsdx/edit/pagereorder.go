package edit

import (
	"fmt"

	core "github.com/jhnoel/documentdriver/internal/ooxml/edit"
	"github.com/jhnoel/documentdriver/internal/ooxml/tree"
	"github.com/jhnoel/documentdriver/internal/vsdx/doc"
	"github.com/jhnoel/documentdriver/internal/vsdx/vsd"
)

// ReorderPage moves the page at before or after the page at to in
// pages.xml order (position "before" or "after"): the Page element
// moves, its ID, part, relationship and bytes do not. It returns
// whether the page moved: a move to where the page already stands
// (including to itself) changes and declares nothing. A page the
// drawing does not have is an ErrNotFound error, and a pages part
// that cannot be read is an Unreadable error. Chunk PGR.
func (w *Workspace) ReorderPage(at, to, position string) (bool, error) {
	a, err := vsd.Parse(at)
	if err != nil {
		return false, fmt.Errorf("vsdx edit: %w", err)
	}
	if a.Kind != vsd.KindPage || a.InMaster {
		return false, fmt.Errorf("vsdx edit: %s: want a page address (page0)", at)
	}
	b, err := vsd.Parse(to)
	if err != nil {
		return false, fmt.Errorf("vsdx edit: %w", err)
	}
	if b.Kind != vsd.KindPage || b.InMaster {
		return false, fmt.Errorf("vsdx edit: %s: want a page address (page0)", to)
	}
	if position != "before" && position != "after" {
		return false, fmt.Errorf("vsdx edit: position must be before or after, not %q", position)
	}
	if w.pagesPart == "" {
		return false, fmt.Errorf("%w: %s", ErrNotFound, at)
	}
	root, err := w.pagesRoot(at)
	if err != nil {
		return false, err
	}
	moving, anchor := pageElems(root, a, b)
	if moving == nil {
		return false, fmt.Errorf("%w: %s", ErrNotFound, at)
	}
	if anchor == nil {
		return false, fmt.Errorf("%w: %s", ErrNotFound, to)
	}
	if moving == anchor {
		return false, nil
	}
	var pos core.Position
	if position == "before" {
		pos = core.Before(anchor)
	} else {
		pos = core.After(anchor, moving)
	}
	if pos.Holds(moving) {
		return false, nil
	}
	if err := w.core.Move(w.pagesPart, moving, pos); err != nil {
		return false, fmt.Errorf("vsdx edit: move %s: %w", at, err)
	}
	movePageEntry(w.doc.Pages, a, b, position)
	return true, nil
}

// pageElems returns the dup-th Page elements named by a and b in
// root (the root's Page children, in stored order), either of which
// may be nil. It walks the children once and stops at the second
// match, so it holds no list of the pages.
func pageElems(root *tree.Node, a, b vsd.Address) (moving, anchor *tree.Node) {
	seenA, seenB := 0, 0
	for p := range root.ElementsSeq() {
		if !isElem(p, vsd.ElemPage) {
			continue
		}
		id, ok := shapeID(p)
		if !ok {
			continue
		}
		if id == a.Container {
			seenA++
			if seenA == a.PageDup {
				moving = p
			}
		}
		if id == b.Container {
			seenB++
			if seenB == b.PageDup {
				anchor = p
			}
		}
		if moving != nil && anchor != nil {
			break
		}
	}
	return moving, anchor
}

// movePageEntry moves the entry of a in pages to its new place
// relative to the entry of b (before or after, as position says),
// in the slice the workspace keeps in pages.xml order. It shifts
// entries in place, allocating nothing.
func movePageEntry(pages []doc.PageEntry, a, b vsd.Address, position string) {
	at, to := pageEntryIndexes(pages, a, b)
	if at < 0 || to < 0 || at == to {
		return
	}
	place := to
	if position == "before" {
		if at < to {
			place = to - 1
		}
	} else if at < to {
		place = to
	} else {
		place = to + 1
	}
	if place == at {
		return
	}
	entry := pages[at]
	if at < place {
		copy(pages[at:], pages[at+1:place+1])
	} else {
		copy(pages[place+1:], pages[place:at])
	}
	pages[place] = entry
}

// pageEntryIndexes returns the indexes in pages of the dup-th
// entries named by a and b, either of which may be -1. It walks
// the slice once, counting duplicates of each ID separately.
func pageEntryIndexes(pages []doc.PageEntry, a, b vsd.Address) (at, to int) {
	at, to = -1, -1
	seenA, seenB := 0, 0
	for i := range pages {
		if pages[i].ID == a.Container {
			seenA++
			if seenA == a.PageDup {
				at = i
			}
		}
		if pages[i].ID == b.Container {
			seenB++
			if seenB == b.PageDup {
				to = i
			}
		}
		if at >= 0 && to >= 0 {
			break
		}
	}
	return at, to
}
