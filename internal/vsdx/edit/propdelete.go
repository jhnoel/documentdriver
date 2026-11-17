package edit

import (
	"fmt"
	"strconv"

	"github.com/jhnoel/documentdriver/internal/ooxml/tree"
	"github.com/jhnoel/documentdriver/internal/ooxml/xmlscan"
	"github.com/jhnoel/documentdriver/internal/vsdx/inherit"
	"github.com/jhnoel/documentdriver/internal/vsdx/sheet"
	"github.com/jhnoel/documentdriver/internal/vsdx/vsd"
)

// DeleteProp deletes the effective shape data row row of the shape at
// shape: a row the master does not define is removed, a row the master
// defines is replaced locally by a Del row (the local cells go) so the
// shape stops inheriting it. A row that is already gone is an
// ErrNotFound error, as is a missing shape.
func (w *Workspace) DeleteProp(shape, row string) error {
	if row == "" {
		return fmt.Errorf("vsdx edit: a prop has no name")
	}
	part, shapeNode, err := w.shapeForProp(shape)
	if err != nil {
		return err
	}
	masterDefines := w.masterDefinesRow(shapeNode, row)
	section := findChild(shapeNode, vsd.ElemSection, vsd.SectionProperty)
	var local *tree.Node
	if section != nil {
		local = findChild(section, vsd.ElemRow, row)
	}
	if section != nil {
		if del, _ := section.Attr("Del"); del == "1" {
			return fmt.Errorf("%w: %s", ErrNotFound, vsd.PropID(shape, row))
		}
	}
	if local != nil {
		if del, _ := local.Attr("Del"); del == "1" {
			return fmt.Errorf("%w: %s", ErrNotFound, vsd.PropID(shape, row))
		}
	}
	h := w.core.Target(part, shape, []*tree.Node{shapeNode})
	if !masterDefines {
		if local == nil {
			return fmt.Errorf("%w: %s", ErrNotFound, vsd.PropID(shape, row))
		}
		if err := h.RemoveNode(local); err != nil {
			return fmt.Errorf("vsdx edit: %w", err)
		}
		return nil
	}
	if local != nil {
		frag := []byte(propDelFragment(row))
		nodes, err := w.core.Parse(section, frag, nil)
		if err != nil {
			return fmt.Errorf("vsdx edit: Del row %s: %w", row, err)
		}
		if len(nodes) != 1 || nodes[0].Kind() != tree.ElementNode || !isElem(nodes[0], vsd.ElemRow) {
			return fmt.Errorf("vsdx edit: Del row fragment is not a single Row element")
		}
		if err := h.ReplaceWith(local, nodes[0]); err != nil {
			return fmt.Errorf("vsdx edit: %w", err)
		}
		return nil
	}
	section, err = w.ensurePropertySection(part, shapeNode)
	if err != nil {
		return err
	}
	frag := []byte(propDelFragment(row))
	nodes, err := w.core.Parse(section, frag, nil)
	if err != nil {
		return fmt.Errorf("vsdx edit: Del row %s: %w", row, err)
	}
	if len(nodes) != 1 || nodes[0].Kind() != tree.ElementNode || !isElem(nodes[0], vsd.ElemRow) {
		return fmt.Errorf("vsdx edit: Del row fragment is not a single Row element")
	}
	pos, err := w.core.SlotEnd(section, "v:Section", vsdxNS.Name(vsd.ElemRow), true, nil, nil)
	if err != nil {
		return fmt.Errorf("vsdx edit: %w", err)
	}
	if err := w.core.Insert(part, pos, nodes[0], nil); err != nil {
		return fmt.Errorf("vsdx edit: %w", err)
	}
	return nil
}

// masterDefinesRow reports whether the master shape of the shape
// shapeNode defines the Property row row, as PropState resolves it: the
// master shape's own row, not one it deletes.
func (w *Workspace) masterDefinesRow(shapeNode *tree.Node, row string) bool {
	s, _ := sheet.Index(shapeNode)
	is := &inherit.Shape{Sheet: s}
	if master, ok := shapeNode.Attr("Master"); ok {
		if id, err := strconv.ParseUint(master, 10, 32); err == nil {
			is.Master, is.HasMaster = uint32(id), true
		}
	}
	if mshape, ok := shapeNode.Attr("MasterShape"); ok {
		if id, err := strconv.ParseUint(mshape, 10, 32); err == nil {
			is.MasterShape, is.HasMShape = uint32(id), true
		}
	}
	r := inherit.New(w.doc)
	ms, ok := r.MasterShape(is)
	if !ok || ms == nil || ms.Sheet == nil {
		return false
	}
	for _, key := range ms.Sheet.Rows(vsd.SectionProperty) {
		if key.Row != row {
			continue
		}
		return !ms.Sheet.Deleted(key)
	}
	return false
}

// propDelFragment returns the deletion Row of row as XML: a Row named
// row with Del="1" and no cells, as Visio writes a hidden master row.
func propDelFragment(row string) string {
	return "<Row N='" + xmlscan.EscapeAttr(row, '\'') + "' Del='1'/>"
}
