package ops

import (
	"fmt"
	"math"
	"slices"
	"strconv"
	"strings"

	docxedit "github.com/jhnoel/documentdriver/internal/docx/edit"
	"github.com/jhnoel/documentdriver/internal/docx/model"
	core "github.com/jhnoel/documentdriver/internal/ooxml/edit"
	"github.com/jhnoel/documentdriver/internal/ooxml/tree"
)

// Writers of the table, row and cell properties the format operations
// set (docxdriver's tables.rs): each places a new element where the
// schema puts it (Handle.EnsureIn) and sets WordprocessingML attributes
// in the prefix the container uses.

// propWriter edits one property container through its handle.
type propWriter struct {
	h   *core.Handle
	bag *tree.Node
}

// ensure returns bag's child w:tag under parent, created where the
// schema puts it when there is none.
func (w propWriter) ensure(parent *tree.Node, tag string) (*tree.Node, error) {
	if model.IsW(parent, "trPr") {
		return w.ensureRowProperty(parent, tag)
	}
	return w.h.EnsureIn(parent, docxedit.W(tag))
}

// rowPropertyOrder is the order Word writes the row properties in: the
// base properties of CT_TrPrBase are a repeated choice, which fixes no
// order, followed by the row's revision marks and its property change.
var rowPropertyOrder = []string{
	"cnfStyle", "divId", "gridBefore", "gridAfter", "wBefore", "wAfter", "cantSplit", "trHeight",
	"tblHeader", "tblCellSpacing", "jc", "hidden", "ins", "del", "trPrChange",
}

// ensureRowProperty returns w:trPr's child w:tag, placing a new one
// before the first child Word writes after it (the schema's repeated
// choice cannot place it, so the position is chosen here).
func (w propWriter) ensureRowProperty(trPr *tree.Node, tag string) (*tree.Node, error) {
	if c := model.ChildW(trPr, tag); c != nil {
		return c, nil
	}
	rank := slices.Index(rowPropertyOrder, tag)
	var before *tree.Node
	for _, c := range trPr.Elements() {
		if i := slices.IndexFunc(rowPropertyOrder, func(l string) bool { return model.IsW(c, l) }); i > rank {
			before = c
			break
		}
	}
	prefix := strings.TrimSuffix(trPr.Name(), "trPr")
	nodes, err := w.h.Workspace().Parse(trPr, []byte("<"+prefix+tag+"/>"), nil)
	if err != nil || len(nodes) != 1 {
		return nil, fmt.Errorf("docx: a new w:%s does not parse: %w", tag, err)
	}
	if err := w.h.PlaceAt(trPr, nodes[0], before); err != nil {
		return nil, err
	}
	return nodes[0], nil
}

// set sets WordprocessingML attribute local on n.
func (w propWriter) set(n *tree.Node, local, value string) error {
	return w.h.SetNodeAttr(n, qname(w.bag, local), value)
}

// unset removes every attribute of n with local name local, whatever its
// prefix.
func (w propWriter) unset(n *tree.Node, local string) error {
	for _, a := range n.Attrs() {
		if a.Name == local || strings.HasSuffix(a.Name, ":"+local) {
			if err := w.h.RemoveNodeAttr(n, a.Name); err != nil {
				return err
			}
		}
	}
	return nil
}

// val gives child w:tag the value v.
func (w propWriter) val(tag, v string) error {
	n, err := w.ensure(w.bag, tag)
	if err != nil {
		return err
	}
	return w.set(n, "val", v)
}

// remove removes every child w:tag of the container.
func (w propWriter) remove(tag string) error {
	for _, c := range childrenW(w.bag, tag) {
		if err := w.h.RemoveNode(c); err != nil {
			return err
		}
	}
	return nil
}

// twips rounds a length in points to twentieths, as docxdriver writes
// lengths (f64::round of points times 20).
func twips(p Points) string { return strconv.FormatInt(int64(p), 10) }

// width writes a CT_TblWidth child w:tag of parent: auto, points (dxa
// twips) or a percentage (pct fiftieths).
func (w propWriter) width(parent *tree.Node, tag string, tw TableWidth) error {
	n, err := w.ensure(parent, tag)
	if err != nil {
		return err
	}
	kind, value := "auto", "0"
	switch tw.Unit {
	case "points":
		kind, value = "dxa", twips(tw.Points)
	case "percent":
		kind, value = "pct", strconv.FormatInt(int64(math.Round(tw.Percent*50)), 10)
	}
	if err := w.set(n, "w", value); err != nil {
		return err
	}
	return w.set(n, "type", kind)
}

// padding writes the sides of a margins child w:tag (w:tblCellMar,
// w:tcMar).
func (w propWriter) padding(tag string, p CellPadding) error {
	n, err := w.ensure(w.bag, tag)
	if err != nil {
		return err
	}
	for _, s := range []struct {
		side string
		v    *Points
	}{{"top", p.Top}, {"left", p.Left}, {"bottom", p.Bottom}, {"right", p.Right}} {
		if s.v == nil {
			continue
		}
		if err := w.width(n, s.side, TableWidth{Unit: "points", Points: *s.v}); err != nil {
			return err
		}
	}
	return nil
}

// borders writes the sides of a borders child w:tag (w:tblBorders,
// w:tcBorders): style, colour (theme colour attributes go) and width in
// eighths of a point.
func (w propWriter) borders(tag string, b TableBorders) error {
	n, err := w.ensure(w.bag, tag)
	if err != nil {
		return err
	}
	for _, s := range []struct {
		side string
		v    *TableBorder
	}{{"top", b.Top}, {"left", b.Left}, {"bottom", b.Bottom}, {"right", b.Right},
		{"insideH", b.InsideHorizontal}, {"insideV", b.InsideVertical}} {
		if s.v == nil {
			continue
		}
		e, err := w.ensure(n, s.side)
		if err != nil {
			return err
		}
		if err := w.set(e, "val", s.v.Style); err != nil {
			return err
		}
		if s.v.Color != nil {
			if err := w.set(e, "color", strings.TrimPrefix(*s.v.Color, "#")); err != nil {
				return err
			}
			for _, a := range []string{"themeColor", "themeTint", "themeShade"} {
				if err := w.unset(e, a); err != nil {
					return err
				}
			}
		}
		if s.v.Width != nil {
			// Eighths of a point: twips times 8/20, exact for the
			// quarter-point widths the rules allow.
			if err := w.set(e, "sz", strconv.FormatInt(int64(math.Round(float64(*s.v.Width)*0.4)), 10)); err != nil {
				return err
			}
		}
	}
	return nil
}

// onOff writes an on/off row property: Word's bare element for on, "off"
// for off.
func (w propWriter) onOff(tag string, on bool) error {
	n, err := w.ensure(w.bag, tag)
	if err != nil {
		return err
	}
	if on {
		return w.unset(n, "val")
	}
	return w.set(n, "val", "off")
}

// clears removes the children the clear list names, by field.
func (w propWriter) clears(fields []string, tags map[string]string) error {
	for _, field := range fields {
		if tag, ok := tags[field]; ok {
			if err := w.remove(tag); err != nil {
				return err
			}
		}
	}
	return nil
}

// pendingRecord reports whether property container bag (nil: none) has a
// pending property change of its own.
func pendingRecord(bag *tree.Node, record string) bool {
	return bag != nil && model.ChildW(bag, record) != nil
}

// ensureBag returns element e's property container w:tag, creating it
// where the schema puts it. created reports a new one, which the caller
// removes again when the edit leaves it empty.
func ensureBag(h *core.Handle, e *tree.Node, tag string) (bag *tree.Node, created bool, err error) {
	if b := model.ChildW(e, tag); b != nil {
		return b, false, nil
	}
	b, err := h.EnsureIn(e, docxedit.W(tag))
	return b, true, err
}

// dropIfEmpty removes a container the edit created and left empty.
func dropIfEmpty(h *core.Handle, bag *tree.Node, created bool) error {
	if !created || bag.Parent() == nil || len(bag.Elements()) > 0 || len(bag.Attrs()) > 0 {
		return nil
	}
	return h.RemoveNode(bag)
}
