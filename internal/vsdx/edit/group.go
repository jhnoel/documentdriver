package edit

import (
	"errors"
	"fmt"
	"slices"
	"strconv"

	core "github.com/jhnoel/documentdriver/internal/ooxml/edit"
	"github.com/jhnoel/documentdriver/internal/ooxml/tree"
	"github.com/jhnoel/documentdriver/internal/vsdx/vsd"
)

// ErrGlued reports a group that a Connect row names: ungrouping it
// would leave that glue pointing at a removed shape.
var ErrGlued = errors.New("vsdx edit: the group is glued to or glued from")

// GroupShapes wraps the shapes at addrs, all children of one Shapes
// element (the page's or one group's), in a new group: frag is the
// group's Shape element (its cells, no ID and an empty Shapes child),
// inserted just after the frontmost of the shapes, with the next free
// ID; the shapes then move into its Shapes in their paint order,
// bytes unchanged. The shapes keep their IDs, so their addresses and
// any Connect rows stay valid; their cells are the caller's to rewrite
// into the group's space. It returns the group's address. A shape
// named twice, shapes of different containers or pages, and a missing
// shape are errors.
func (w *Workspace) GroupShapes(addrs []string, frag []byte) (string, error) {
	if len(addrs) == 0 {
		return "", errors.New("vsdx edit: no shapes to group")
	}
	var page vsd.Address
	var nodes []*tree.Node
	for i, at := range addrs {
		a, err := vsd.Parse(at)
		if err != nil {
			return "", fmt.Errorf("vsdx edit: %w", err)
		}
		if a.Kind != vsd.KindShape || a.InMaster {
			return "", fmt.Errorf("vsdx edit: %s: want a page shape address (page0/s5)", at)
		}
		pa := vsd.Address{Kind: vsd.KindPage, Container: a.Container, PageDup: a.PageDup}
		if i == 0 {
			page = pa
		} else if pa != page {
			return "", fmt.Errorf("vsdx edit: %s is on another page than %s", at, addrs[0])
		}
		_, part, d, err := w.pageTree(page)
		if err != nil {
			return "", err
		}
		n := w.shapeIndexFor(part, d.Root()).find(a.Shape, a.Dup)
		if n == nil {
			return "", fmt.Errorf("%w: %s", ErrNotFound, at)
		}
		if slices.Contains(nodes, n) {
			return "", fmt.Errorf("vsdx edit: %s is named twice", at)
		}
		if len(nodes) > 0 && n.Parent() != nodes[0].Parent() {
			return "", fmt.Errorf("vsdx edit: %s is not in the same container as %s; group shapes of one container (the page or one group)", at, addrs[0])
		}
		nodes = append(nodes, n)
	}
	_, part, d, err := w.pageTree(page)
	if err != nil {
		return "", err
	}
	idx := w.shapeIndexFor(part, d.Root())
	container := nodes[0].Parent()
	// Paint order: document order within the container.
	pos := map[*tree.Node]int{}
	k := 0
	for c := range container.ElementsSeq() {
		pos[c] = k
		k++
	}
	slices.SortFunc(nodes, func(a, b *tree.Node) int { return pos[a] - pos[b] })
	id, err := idx.next()
	if err != nil {
		return "", err
	}
	parsed, err := w.core.Parse(container, frag, nil)
	if err != nil {
		return "", fmt.Errorf("vsdx edit: group fragment: %w", err)
	}
	if len(parsed) != 1 || !isElem(parsed[0], vsd.ElemShape) {
		return "", errors.New("vsdx edit: group fragment is not a single Shape element")
	}
	g := parsed[0]
	shapes := child(g, vsd.ElemShapes)
	if shapes == nil {
		return "", errors.New("vsdx edit: group fragment has no Shapes element")
	}
	if err := w.core.Insert(part, core.JustAfter(nodes[len(nodes)-1]), g, []core.NodeAttr{{Node: g, Name: "ID", Value: strconv.FormatUint(uint64(id), 10)}}); err != nil {
		return "", err
	}
	for _, n := range nodes {
		at, err := w.core.SlotEnd(shapes, "v:Shapes", vsdxNS.Name(vsd.ElemShape), true, nil, nil)
		if err != nil {
			return "", fmt.Errorf("vsdx edit: %w", err)
		}
		if err := w.core.Move(part, n, at); err != nil {
			return "", err
		}
	}
	// Shapes moved: the index's order of duplicates and last children
	// are rebuilt on the next lookup.
	delete(w.shapes, part)
	return vsd.ShapeID(vsd.PageID(page.Container, page.PageDup), id, 1), nil
}

// Ungroup moves the children of the group at shape, bytes unchanged
// and in paint order, into the group's container where the group was,
// and removes the group. Their cells are the caller's to rewrite into
// the container's space. It returns the children's addresses in paint
// order. A group that a Connect row names (ErrGlued), a shape that is
// not a group and a missing shape are errors.
func (w *Workspace) Ungroup(shape string) ([]string, error) {
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
	g := idx.find(a.Shape, a.Dup)
	if g == nil {
		return nil, fmt.Errorf("%w: %s", ErrNotFound, shape)
	}
	if typ, _ := g.Attr("Type"); typ != vsd.ShapeTypeGroup {
		return nil, fmt.Errorf("vsdx edit: %s is not a group", shape)
	}
	for cs := range root.ElementsSeq() {
		if !isElem(cs, vsd.ElemConnects) {
			continue
		}
		for c := range cs.ElementsSeq() {
			from, fromOK := connectID(c, "FromSheet")
			to, toOK := connectID(c, "ToSheet")
			if (fromOK && from == a.Shape) || (toOK && to == a.Shape) {
				return nil, fmt.Errorf("%w: %s", ErrGlued, shape)
			}
		}
	}
	pageAddr := vsd.PageID(a.Container, a.PageDup)
	var kids []*tree.Node
	var addrs []string
	if shapes := child(g, vsd.ElemShapes); shapes != nil {
		for c := range shapes.ElementsSeq() {
			if !isElem(c, vsd.ElemShape) {
				continue
			}
			kids = append(kids, c)
			cid, _ := shapeID(c)
			dup := 1
			for i, x := range idx.byID[cid] {
				if x == c {
					dup = i + 1
					break
				}
			}
			addrs = append(addrs, vsd.ShapeID(pageAddr, cid, dup))
		}
	}
	for _, c := range kids {
		if err := w.core.Move(part, c, core.Before(g)); err != nil {
			return nil, err
		}
	}
	if err := w.core.Delete(part, g); err != nil {
		return nil, err
	}
	delete(w.shapes, part)
	return addrs, nil
}
