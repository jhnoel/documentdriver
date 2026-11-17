package edit

import (
	"fmt"
	"strconv"
	"strings"

	core "github.com/jhnoel/documentdriver/internal/ooxml/edit"
	"github.com/jhnoel/documentdriver/internal/ooxml/tree"
	"github.com/jhnoel/documentdriver/internal/vsdx/vsd"
)

// cellBegTrigger is the begin trigger cell of a dynamic connector.
const cellBegTrigger = "BegTrigger"

// cellEndTrigger is the end trigger cell of a dynamic connector.
const cellEndTrigger = "EndTrigger"

// walkGluePrefix is the _WALKGLUE formula prefix of a dynamic
// connector end.
const walkGluePrefix = "_WALKGLUE"

// DeleteShape deletes the shape at shape: the Shape element with its
// children, every Connect row naming a shape whose first occurrence
// is deleted (FromSheet or ToSheet, end or control), and the glue
// formulas of surviving connectors glued to a deleted shape (the
// PAR(PNT(Sheet.N!...)) end formula, the _WALKGLUE end formula and
// the _XFTRIGGER trigger formula), keeping each cell's V. It returns
// the deleted addresses in document order and the freed survivors'
// addresses. A missing shape is an ErrNotFound error.
func (w *Workspace) DeleteShape(shape string) ([]string, []string, error) {
	a, err := vsd.Parse(shape)
	if err != nil {
		return nil, nil, fmt.Errorf("vsdx edit: %w", err)
	}
	if a.Kind != vsd.KindShape || a.InMaster {
		return nil, nil, fmt.Errorf("vsdx edit: %s: want a page shape address (page0/s5)", shape)
	}
	page := vsd.Address{Kind: vsd.KindPage, Container: a.Container, PageDup: a.PageDup}
	_, part, d, err := w.pageTree(page)
	if err != nil {
		return nil, nil, err
	}
	root := d.Root()
	idx := w.shapeIndexFor(part, root)
	target := idx.find(a.Shape, a.Dup)
	if target == nil {
		return nil, nil, fmt.Errorf("%w: %s", ErrNotFound, shape)
	}
	pageAddr := vsd.PageID(a.Container, a.PageDup)
	subtree := map[*tree.Node]bool{}
	target.Walk(func(n *tree.Node) bool {
		if isElem(n, vsd.ElemShape) {
			subtree[n] = true
		}
		return true
	})
	deletedFirst := map[uint32]bool{}
	for id, list := range idx.byID {
		if len(list) > 0 && subtree[list[0]] {
			deletedFirst[id] = true
		}
	}
	var deleted []string
	target.Walk(func(n *tree.Node) bool {
		if !isElem(n, vsd.ElemShape) {
			return true
		}
		id, ok := shapeID(n)
		if !ok {
			return true
		}
		dup := 1
		for i, x := range idx.byID[id] {
			if x == n {
				dup = i + 1
				break
			}
		}
		deleted = append(deleted, vsd.ShapeID(pageAddr, id, dup))
		return true
	})
	type freeEnd struct {
		shape *tree.Node
		begin bool
	}
	var frees []freeEnd
	seenFree := map[freeEnd]bool{}
	var changedShapes []*tree.Node
	seenChanged := map[*tree.Node]bool{}
	var removals []*tree.Node
	for childNode := range root.ElementsSeq() {
		if !isElem(childNode, vsd.ElemConnects) {
			continue
		}
		for c := range childNode.ElementsSeq() {
			if !isElem(c, vsd.ElemConnect) {
				continue
			}
			fromID, fromOk := connectID(c, "FromSheet")
			toID, toOk := connectID(c, "ToSheet")
			fromDeleted := fromOk && deletedFirst[fromID]
			toDeleted := toOk && deletedFirst[toID]
			if !fromDeleted && !toDeleted {
				continue
			}
			removals = append(removals, c)
			if !toDeleted || fromDeleted {
				continue
			}
			fromShape := idx.find(fromID, 1)
			if fromShape == nil {
				continue
			}
			if !seenChanged[fromShape] {
				seenChanged[fromShape] = true
				changedShapes = append(changedShapes, fromShape)
			}
			fromCell, _ := c.Attr("FromCell")
			if fromCell != vsd.FromCellBegin && fromCell != vsd.FromCellEnd {
				continue
			}
			fe := freeEnd{fromShape, fromCell == vsd.FromCellBegin}
			if !seenFree[fe] {
				seenFree[fe] = true
				frees = append(frees, fe)
			}
		}
	}
	for _, r := range removals {
		if err := w.core.Delete(part, r); err != nil {
			return nil, nil, fmt.Errorf("vsdx edit: %w", err)
		}
	}
	shapeHandles := map[*tree.Node]*core.Handle{}
	for _, fe := range frees {
		h := shapeHandles[fe.shape]
		if h == nil {
			label := pageAddr
			if id, ok := shapeID(fe.shape); ok {
				label = vsd.ShapeID(pageAddr, id, 1)
			}
			h = w.core.Target(part, label, []*tree.Node{fe.shape})
			shapeHandles[fe.shape] = h
		}
		var names []string
		if fe.begin {
			names = []string{vsd.CellBeginX, vsd.CellBeginY, cellBegTrigger}
		} else {
			names = []string{vsd.CellEndX, vsd.CellEndY, cellEndTrigger}
		}
		for _, name := range names {
			cell := findCell(fe.shape, name)
			if cell == nil {
				continue
			}
			f, has := cell.Attr("F")
			if !has {
				continue
			}
			strip := false
			if name == cellBegTrigger || name == cellEndTrigger {
				strip = formulaRefsDeleted(f, deletedFirst)
			} else {
				strip = formulaRefsDeleted(f, deletedFirst) || strings.HasPrefix(f, walkGluePrefix)
			}
			if strip {
				if err := h.RemoveNodeAttr(cell, "F"); err != nil {
					return nil, nil, fmt.Errorf("vsdx edit: %w", err)
				}
			}
		}
	}
	if err := w.core.Delete(part, target); err != nil {
		return nil, nil, fmt.Errorf("vsdx edit: %w", err)
	}
	delete(w.shapes, part)
	var changed []string
	for _, sn := range changedShapes {
		if id, ok := shapeID(sn); ok {
			changed = append(changed, vsd.ShapeID(pageAddr, id, 1))
		}
	}
	return deleted, changed, nil
}

// connectID parses element c's attribute name as a uint32, reporting
// false when it is absent or does not parse.
func connectID(c *tree.Node, name string) (uint32, bool) {
	raw, _ := c.Attr(name)
	u, err := strconv.ParseUint(raw, 10, 32)
	return uint32(u), err == nil
}

// formulaRefsDeleted reports whether formula f names a deleted shape
// through a Sheet.N! reference (the PAR(PNT(Sheet.N!...) and
// _XFTRIGGER(Sheet.N!...) forms).
func formulaRefsDeleted(f string, deleted map[uint32]bool) bool {
	for i := 0; i < len(f); {
		at := strings.Index(f[i:], "Sheet.")
		if at < 0 {
			return false
		}
		pos := i + at
		if pos > 0 {
			if b := f[pos-1]; b == '_' || (b >= 'A' && b <= 'Z') || (b >= 'a' && b <= 'z') || (b >= '0' && b <= '9') {
				i = pos + 6
				continue
			}
		}
		start := pos + 6
		j := start
		for j < len(f) && f[j] >= '0' && f[j] <= '9' {
			j++
		}
		if j == start {
			i = start
			continue
		}
		if u, err := strconv.ParseUint(f[start:j], 10, 32); err == nil && deleted[uint32(u)] {
			return true
		}
		i = j
	}
	return false
}
