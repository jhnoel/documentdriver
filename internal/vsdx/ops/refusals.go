package ops

import (
	"strconv"
	"strings"

	"github.com/jhnoel/documentdriver/internal/cli"
	"github.com/jhnoel/documentdriver/internal/geom"
	"github.com/jhnoel/documentdriver/internal/ooxml/tree"
	"github.com/jhnoel/documentdriver/internal/vsdx/doc"
	"github.com/jhnoel/documentdriver/internal/vsdx/edit"
	"github.com/jhnoel/documentdriver/internal/vsdx/inherit"
	"github.com/jhnoel/documentdriver/internal/vsdx/sheet"
	"github.com/jhnoel/documentdriver/internal/vsdx/vsd"
)

// Chunk CC6: the refusals of docs/vsdx-design.md decision (g) items 2
// and 3, shared by the write ops. A container, list or callout (its
// effective User.msvStructureType, local or inherited) is refused by
// every write op that targets it or adds into it; a member of one (a
// Relationships cell whose formula has DEPENDSON() is refused only
// for geometry writes; and geometry on a page whose PageScale and
// DrawingScale differ is refused, while text, fill, line, name, props
// and glue on such a page stay writable.

// structureType returns the effective User.msvStructureType value of
// shape when it is a structure type (Container, List or Callout), and
// "" otherwise: Heading and 0 are not structured.
func structureType(r *inherit.Resolver, shape *inherit.Shape) string {
	cell, _, ok := r.Cell(shape, sheet.NamedRowCell("User", "msvStructureType", "Value"))
	if !ok {
		return ""
	}
	switch cell.V {
	case "Container", "List", "Callout":
		return cell.V
	}
	return ""
}

// isMember reports whether shape is in a container or list: its
// effective Relationships cell's formula has DEPENDSON(.
func isMember(r *inherit.Resolver, shape *inherit.Shape) bool {
	cell, _, ok := r.Cell(shape, sheet.ShapeCell("Relationships"))
	return ok && strings.Contains(cell.F, "DEPENDSON(")
}

// shapeRefusal reports the refusal of a write to the shape at addr
// through shape: a structure shape is refused at field with the
// container message, and, when member is set, a member of a container
// or list with the member message. It returns nil when neither
// applies.
func shapeRefusal(r *inherit.Resolver, shape *inherit.Shape, addr, field string, member bool) error {
	if structureType(r, shape) != "" {
		return cli.Errorf(cli.CodeOperationFailed, field, "%s is a container (User.msvStructureType): containers, lists and callouts are not edited in v1", addr)
	}
	if member && isMember(r, shape) {
		return cli.Errorf(cli.CodeOperationFailed, field, "%s is in a container or list (Relationships): its geometry and membership are not edited in v1", addr)
	}
	return nil
}

// nodeRefusal reports the refusal of a write to the shape node at
// addr: shapeRefusal of the node's resolver shape.
func nodeRefusal(r *inherit.Resolver, node *tree.Node, addr, field string, member bool) error {
	return shapeRefusal(r, textShapeOf(node), addr, field, member)
}

// isContainerShape reports whether node is a container, list or
// callout, for the page-wide text.replace, which skips such shapes
// instead of refusing.
func isContainerShape(r *inherit.Resolver, node *tree.Node) bool {
	return structureType(r, textShapeOf(node)) != ""
}

// pageScaled reports whether the page container/dup of d is scaled
// (its PageScale and DrawingScale differ through r), and its scale
// text: a scaled page's geometry v1 does not edit. A page with no
// entry or unreadable scale cells is not scaled, so the op's own
// error for the page stands.
func pageScaled(r *inherit.Resolver, d *doc.Document, container uint32, dup int) (string, bool) {
	if d == nil {
		return "", false
	}
	var entry *doc.PageEntry
	seen := 0
	for i := range d.Pages {
		p := &d.Pages[i]
		if p.ID != container {
			continue
		}
		seen++
		if seen == dup {
			entry = p
			break
		}
	}
	if entry == nil || entry.PageSheet == nil {
		return "", false
	}
	shape, _ := sheet.Index(entry.PageSheet)
	h := &inherit.Shape{Sheet: shape}
	for name, value := range entry.PageSheet.AttrsSeq() {
		var i int
		switch name {
		case "LineStyle":
			i = 0
		case "FillStyle":
			i = 1
		case "TextStyle":
			i = 2
		default:
			continue
		}
		if u, err := strconv.ParseUint(value, 10, 32); err == nil {
			h.Styles[i], h.HasStyle[i] = uint32(u), true
		}
	}
	pageScale, _, psOK := r.Cell(h, sheet.ShapeCell("PageScale"))
	drawingScale, _, dsOK := r.Cell(h, sheet.ShapeCell("DrawingScale"))
	if !psOK || !dsOK {
		return "", false
	}
	ps, psNum := vsd.Number(pageScale.V)
	ds, dsNum := vsd.Number(drawingScale.V)
	return refusalScaleText(pageScale, drawingScale), psNum && dsNum && (ps != ds || refusalScaleUnit(pageScale.U) != refusalScaleUnit(drawingScale.U))
}

// scaleUnits maps a stored scale unit to Visio's abbreviation.
var scaleUnits = map[string]string{"IN": "in", "IN_F": "in", "FT": "ft", "FT_I": "ft", "MM": "mm", "CM": "cm", "M": "m"}

// refusalScaleText returns the "<PageScale> = <DrawingScale>" text of two
// scale cells, each its number (geom.FormatNum) and unit as stored.
// It mirrors the page note of model.pageFacts; it is local because
// ops cannot import model (model's tests import ops).
func refusalScaleText(pageScale, drawingScale sheet.Cell) string {
	a, b := refusalScalePair(pageScale), refusalScalePair(drawingScale)
	if a == "" || b == "" {
		return ""
	}
	return a + " = " + b
}

// refusalScalePair returns one scale cell as "<number> <unit>", or
// "" when the cell is not a number.
func refusalScalePair(cell sheet.Cell) string {
	n, ok := vsd.Number(cell.V)
	if !ok {
		return ""
	}
	return strings.TrimSpace(geom.FormatNum(n) + " " + refusalScaleUnit(cell.U))
}

// refusalScaleUnit returns Visio's abbreviation for a stored unit.
func refusalScaleUnit(u string) string {
	if v, ok := scaleUnits[u]; ok {
		return v
	}
	return u
}

// scaledRefusal reports the refusal of a geometry write at field on
// the scaled page pageAddr: operation_failed at field naming the
// page and its scale. It returns nil on an unscaled page.
func scaledRefusal(r *inherit.Resolver, d *doc.Document, container uint32, dup int, pageAddr, field string) error {
	text, scaled := pageScaled(r, d, container, dup)
	if !scaled {
		return nil
	}
	return cli.Errorf(cli.CodeOperationFailed, field, "%s has a drawing scale (%s): geometry is not edited on scaled pages in v1", pageAddr, text)
}

// targetRefusal reports the refusal of a write to the shape at addr
// for the ops that reach the shape through the edit layer (delete,
// reorder, props), which load the shape's page themselves: it loads
// the shape's node from the working tree and reports shapeRefusal of
// it. A missing shape or page, or an unreadable page part, gives
// nil, so the op's own error for the target stands.
func targetRefusal(ws *edit.Workspace, addr, field string, member bool) error {
	d, node, _, ok := targetNode(ws, addr)
	if !ok {
		return nil
	}
	return nodeRefusal(inherit.New(d), node, addr, field, member)
}

// targetNode returns the document and the node of the shape at addr
// in its page's working tree, and the parsed address. It reports
// false when addr is not a page shape, the page or shape is missing,
// or the page part is unreadable, so the caller's own error for the
// target stands.
func targetNode(ws *edit.Workspace, addr string) (*doc.Document, *tree.Node, vsd.Address, bool) {
	a, err := vsd.Parse(addr)
	if err != nil || a.Kind != vsd.KindShape || a.InMaster {
		return nil, nil, vsd.Address{}, false
	}
	d, err := doc.Open(ws.Package())
	if err != nil {
		return nil, nil, vsd.Address{}, false
	}
	var entry *doc.PageEntry
	seen := 0
	for i := range d.Pages {
		p := &d.Pages[i]
		if p.ID != a.Container {
			continue
		}
		seen++
		if seen == a.PageDup {
			entry = p
			break
		}
	}
	if entry == nil || entry.Part == "" {
		return nil, nil, vsd.Address{}, false
	}
	working, err := ws.Core().Tree(entry.Part)
	if err != nil {
		return nil, nil, vsd.Address{}, false
	}
	root := working.Root()
	if root == nil || !vsd.IsMain(root.Space()) || root.Local() != vsd.ElemPageContents {
		return nil, nil, vsd.Address{}, false
	}
	node := findTextShape(root, a.Shape, a.Dup)
	if node == nil {
		return nil, nil, vsd.Address{}, false
	}
	return d, node, a, true
}

// inheritedSubShapeRefusal reports the refusal of deleting the shape
// at addr when it is a master-inherited sub-shape: the shape stores
// MasterShape and sits inside a master instance (an enclosing shape
// that stores Master). Deleting the instance deletes its sub-shapes
// with it; deleting one sub-shape alone would strand the instance.
// It returns nil for every other shape, including the instance
// itself and a plain group child with no MasterShape.
func inheritedSubShapeRefusal(ws *edit.Workspace, addr, field string) error {
	_, node, a, ok := targetNode(ws, addr)
	if !ok {
		return nil
	}
	sh := textShapeOf(node)
	if sh == nil || !sh.HasMShape || !sh.HasMaster {
		return nil
	}
	var instance *tree.Node
	for p := node.Parent(); p != nil; p = p.Parent() {
		if p.Kind() != tree.ElementNode || !vsd.IsMain(p.Space()) || p.Local() != vsd.ElemShape {
			continue
		}
		if _, ok := p.Attr("Master"); ok {
			instance = p
			break
		}
	}
	if instance == nil {
		return nil
	}
	raw, ok := instance.Attr("ID")
	id, err := strconv.ParseUint(raw, 10, 32)
	if !ok || err != nil {
		return nil
	}
	at := vsd.ShapeID(vsd.PageID(a.Container, a.PageDup), uint32(id), 1)
	return cli.Errorf(cli.CodeOperationFailed, field, "%s is inherited from %s (master shape %d): delete the instance %s, or edit the master in Visio", addr, vsd.MasterID(sh.Master), sh.MasterShape, at)
}
