package edit

import (
	"fmt"
	"strconv"

	core "github.com/jhnoel/documentdriver/internal/ooxml/edit"
	"github.com/jhnoel/documentdriver/internal/ooxml/tree"
	"github.com/jhnoel/documentdriver/internal/vsdx/vsd"
)

// DeleteFieldRows deletes the Field section rows of the shape at shape
// whose row IX is in ixs, and the Field section itself when no row is
// left, so a text.set that drops fields leaves no row an fld could
// point at. The field's Text side is the caller's (SetText); this
// touches only the shape's own Field section and leaves every other
// row. A missing shape is an ErrNotFound error; a shape without a
// Field section has nothing to delete. Chunk CC11.
func (w *Workspace) DeleteFieldRows(shape string, ixs []int) error {
	if len(ixs) == 0 {
		return nil
	}
	a, err := vsd.Parse(shape)
	if err != nil {
		return fmt.Errorf("vsdx edit: %w", err)
	}
	if a.Kind != vsd.KindShape || a.InMaster {
		return fmt.Errorf("vsdx edit: %s: want a page shape address (page0/s5)", shape)
	}
	page := vsd.Address{Kind: vsd.KindPage, Container: a.Container, PageDup: a.PageDup}
	_, part, d, err := w.pageTree(page)
	if err != nil {
		return err
	}
	shapeNode := w.shapeIndexFor(part, d.Root()).find(a.Shape, a.Dup)
	if shapeNode == nil {
		return fmt.Errorf("%w: %s", ErrNotFound, shape)
	}
	section := findChild(shapeNode, vsd.ElemSection, vsd.SectionField)
	if section == nil {
		return nil
	}
	want := make(map[int]bool, len(ixs))
	for _, ix := range ixs {
		want[ix] = true
	}
	h := w.core.Target(part, shape, []*tree.Node{section})
	if err := h.RemoveChildren(core.Path{}, func(c *tree.Node) bool {
		if !isElem(c, vsd.ElemRow) {
			return false
		}
		v, ok := c.Attr("IX")
		if !ok {
			return false
		}
		n, err := strconv.Atoi(v)
		return err == nil && want[n]
	}); err != nil {
		return fmt.Errorf("vsdx edit: %w", err)
	}
	left := false
	for c := range section.ElementsSeq() {
		if isElem(c, vsd.ElemRow) {
			left = true
			break
		}
	}
	if !left {
		h := w.core.Target(part, shape, []*tree.Node{shapeNode})
		if err := h.RemoveNode(section); err != nil {
			return fmt.Errorf("vsdx edit: %w", err)
		}
	}
	return nil
}
