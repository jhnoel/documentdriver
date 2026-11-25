package edit

import (
	"errors"
	"fmt"
	"strings"

	core "github.com/jhnoel/documentdriver/internal/ooxml/edit"
	"github.com/jhnoel/documentdriver/internal/ooxml/tree"
	"github.com/jhnoel/documentdriver/internal/ooxml/xmlscan"
	"github.com/jhnoel/documentdriver/internal/vsdx/vsd"
)

// SetRowCells sets cells (name, V, U, F; an empty U or F removes the
// attribute) in the named row row of section on the shape at shape
// (Controls.TextPosition is section "Controls", row "TextPosition"):
// the shape's own row when it stores one (and has not deleted it),
// else a new local row holding only these cells, in a section created
// in schema order when the shape has none, as Visio writes an
// instance's override of its master's row. A missing shape is an
// ErrNotFound error.
func (w *Workspace) SetRowCells(shape, section, row string, cells []Cell) error {
	if section == "" || row == "" {
		return errors.New("vsdx edit: a row needs a section and a name")
	}
	for _, c := range cells {
		if c.N == "" {
			return errors.New("vsdx edit: a cell has no name")
		}
	}
	part, shapeNode, err := w.shapeForProp(shape)
	if err != nil {
		return err
	}
	if sec := findChild(shapeNode, vsd.ElemSection, section); sec != nil {
		if local := findChild(sec, vsd.ElemRow, row); local != nil {
			if del, _ := local.Attr("Del"); del != "1" {
				h := w.core.Target(part, shape, []*tree.Node{shapeNode})
				for _, c := range cells {
					if err := w.setRowCell(part, h, local, c); err != nil {
						return err
					}
				}
				return nil
			}
		}
	}
	sec, err := w.ensureSection(part, shapeNode, section)
	if err != nil {
		return err
	}
	var b strings.Builder
	b.WriteString("<Row N='" + xmlscan.EscapeAttr(row, '\'') + "'>")
	for _, c := range cells {
		b.WriteString("<Cell N='" + xmlscan.EscapeAttr(c.N, '\'') + "' V='" + xmlscan.EscapeAttr(c.V, '\'') + "'")
		if c.U != "" {
			b.WriteString(" U='" + xmlscan.EscapeAttr(c.U, '\'') + "'")
		}
		if c.F != "" {
			b.WriteString(" F='" + xmlscan.EscapeAttr(c.F, '\'') + "'")
		}
		b.WriteString("/>")
	}
	b.WriteString("</Row>")
	nodes, err := w.core.Parse(sec, []byte(b.String()), nil)
	if err != nil {
		return fmt.Errorf("vsdx edit: row %s: %w", row, err)
	}
	if len(nodes) != 1 || nodes[0].Kind() != tree.ElementNode || !isElem(nodes[0], vsd.ElemRow) {
		return errors.New("vsdx edit: row fragment is not a single Row element")
	}
	pos, err := w.core.SlotEnd(sec, "v:Section", vsdxNS.Name(vsd.ElemRow), true, nil, nil)
	if err != nil {
		return fmt.Errorf("vsdx edit: %w", err)
	}
	if err := w.core.Insert(part, pos, nodes[0], nil); err != nil {
		return fmt.Errorf("vsdx edit: %w", err)
	}
	return nil
}

// setRowCell sets cell c in the stored row: the existing Cell's V, U
// and F, or a new Cell at the end of the row's Cell slot.
func (w *Workspace) setRowCell(part string, h *core.Handle, row *tree.Node, c Cell) error {
	if existing := findCell(row, c.N); existing != nil {
		if err := h.SetNodeAttr(existing, "V", c.V); err != nil {
			return fmt.Errorf("vsdx edit: %w", err)
		}
		if err := setOrRemove(h, existing, "U", c.U); err != nil {
			return fmt.Errorf("vsdx edit: %w", err)
		}
		return setOrRemove(h, existing, "F", c.F)
	}
	frag := "<Cell N='" + xmlscan.EscapeAttr(c.N, '\'') + "' V='" + xmlscan.EscapeAttr(c.V, '\'') + "'"
	for _, a := range [2][2]string{{"U", c.U}, {"F", c.F}} {
		if a[1] != "" {
			frag += " " + a[0] + "='" + xmlscan.EscapeAttr(a[1], '\'') + "'"
		}
	}
	nodes, err := w.core.Parse(row, []byte(frag+"/>"), nil)
	if err != nil || len(nodes) != 1 || !isElem(nodes[0], vsd.ElemCell) {
		return fmt.Errorf("vsdx edit: cell %s: not a single Cell element (%v)", c.N, err)
	}
	at, err := w.core.SlotEnd(row, "v:Row", vsdxNS.Name(vsd.ElemCell), true, nil, nil)
	if err != nil {
		return fmt.Errorf("vsdx edit: %w", err)
	}
	return w.core.Insert(part, at, nodes[0], nil)
}
