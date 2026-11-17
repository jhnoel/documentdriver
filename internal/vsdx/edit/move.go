package edit

import (
	"fmt"

	"github.com/jhnoel/documentdriver/internal/ooxml/tree"
	"github.com/jhnoel/documentdriver/internal/vsdx/vsd"
)

// MoveShape writes cells to the shape at shape (as SetCells does) and
// returns the connector ends that the move leaves stale, in Connect
// document order: a BeginX or EndX Connect whose connector and whose
// glue target move differently, that is, exactly one of them is the
// shape or inside it. A connector and its target inside one moved
// group keep their relative geometry and are not stale. Each entry is
// "<connector address> begin" or "<connector address> end" (the
// address of the connector's first shape with its stored ID). An
// empty cells writes nothing, returns no stale ends, and declares
// nothing. A missing shape is an ErrNotFound error.
func (w *Workspace) MoveShape(shape string, cells []Cell) ([]string, error) {
	if len(cells) == 0 {
		return nil, nil
	}
	a, err := vsd.Parse(shape)
	if err != nil {
		return nil, fmt.Errorf("vsdx edit: %w", err)
	}
	if a.Kind != vsd.KindShape || a.InMaster {
		return nil, fmt.Errorf("vsdx edit: %s: want a page shape address (page0/s5)", shape)
	}
	page := vsd.Address{Kind: vsd.KindPage, Container: a.Container, PageDup: a.PageDup}
	_, part, d, err := w.pageTree(page)
	if err != nil {
		return nil, err
	}
	root := d.Root()
	idx := w.shapeIndexFor(part, root)
	target := idx.find(a.Shape, a.Dup)
	if target == nil {
		return nil, fmt.Errorf("%w: %s", ErrNotFound, shape)
	}
	pageAddr := vsd.PageID(a.Container, a.PageDup)
	stale := moveStaleEnds(root, idx, target, pageAddr)
	if err := w.SetCells(shape, cells); err != nil {
		return nil, err
	}
	return stale, nil
}

// moveStaleEnds returns the ends a move of target leaves stale: for
// each Connect of the page with a BeginX or EndX FromCell, the entry
// "<connector> begin|end" when exactly one of the connector and the
// glue target is target or inside it. Shape lookup is the cached
// shape index and ancestry is the parent chain, so no walk of the
// moved subtree is made.
func moveStaleEnds(root *tree.Node, idx *shapeIndex, target *tree.Node, pageAddr string) []string {
	var stale []string
	seen := map[moveEnd]bool{}
	for child := range root.ElementsSeq() {
		if !isElem(child, vsd.ElemConnects) {
			continue
		}
		for c := range child.ElementsSeq() {
			if !isElem(c, vsd.ElemConnect) {
				continue
			}
			fromCell, _ := c.Attr("FromCell")
			begin := fromCell == vsd.FromCellBegin
			if !begin && fromCell != vsd.FromCellEnd {
				continue
			}
			fromID, ok := connectID(c, "FromSheet")
			if !ok {
				continue
			}
			toID, toOK := connectID(c, "ToSheet")
			fromMoved := moveInside(idx.find(fromID, 1), target)
			toMoved := toOK && moveInside(idx.find(toID, 1), target)
			if fromMoved == toMoved {
				continue
			}
			end := moveEnd{fromID, begin}
			if seen[end] {
				continue
			}
			seen[end] = true
			endWord := "end"
			if begin {
				endWord = "begin"
			}
			stale = append(stale, moveAddr(pageAddr, fromID)+" "+endWord)
		}
	}
	return stale
}

// moveEnd is one end of one connector, for deduplication.
type moveEnd struct {
	id    uint32
	begin bool
}

// moveInside reports whether node is target or inside it, by walking
// the parent chain, bounded at 512 steps (the tokenizer's depth cap).
func moveInside(node, target *tree.Node) bool {
	for depth := 0; node != nil && depth <= 512; depth++ {
		if node == target {
			return true
		}
		node = node.Parent()
	}
	return false
}

// moveAddr returns the address of the first shape with stored ID id
// ("page0/s3"), the shape moveStaleEnds looked up.
func moveAddr(pageAddr string, id uint32) string {
	return vsd.ShapeID(pageAddr, id, 1)
}
