package prop

import (
	"fmt"
	"strconv"

	"github.com/jhnoel/documentdriver/internal/ooxml/tree"
	"github.com/jhnoel/documentdriver/internal/pptx/derive"
	"github.com/jhnoel/documentdriver/internal/pptx/ops/edit"
)

// CNvPrText returns the codec of a cNvPr text attribute shown when not
// empty: "descr" (alternative text) or "title". Its read half is the
// attribute as written, as in the read model; "" is no text, and writing
// "" removes the attribute.
func CNvPrText(attr string) Codec[string] {
	return Codec[string]{
		Name: attr,
		Read: func(h *edit.Handle, i int) derive.Val[string] {
			n := h.CNvPr(i)
			if n == nil {
				return derive.Missing[string]("no cNvPr")
			}
			v, _ := n.Attr(attr)
			return derive.Got(v, derive.FromSlide)
		},
		Write: func(h *edit.Handle, v string) error {
			if v == "" {
				return h.RemoveCNvPrAttr(attr)
			}
			if err := edit.CheckAttr("a:CT_NonVisualDrawingProps", attr, v); err != nil {
				return err
			}
			return h.SetCNvPrAttr(attr, v)
		},
		Show: func(v string) string { return v },
	}
}

// Hidden returns the codec of cNvPr hidden, shown as the views show it
// (true, or nothing for false). Its read half is derive.AttrBool with the
// schema default false, as in the read model; true is written "1", and
// false removes the attribute.
func Hidden() Codec[bool] {
	return Codec[bool]{
		Name: "hidden",
		Read: func(h *edit.Handle, i int) derive.Val[bool] {
			n := h.CNvPr(i)
			if n == nil {
				return derive.Missing[bool]("no cNvPr")
			}
			return readFlag(n, "hidden")
		},
		Write: func(h *edit.Handle, v bool) error {
			if !v {
				return h.RemoveCNvPrAttr("hidden")
			}
			return h.SetCNvPrAttr("hidden", "1")
		},
		Show: strconv.FormatBool,
	}
}

var pathCNvSpPr = edit.Path{edit.P("nvSpPr"), edit.P("cNvSpPr")}

// TxBox returns the codec of a shape's cNvSpPr txBox (PowerPoint's text
// box), read and written as Hidden is.
func TxBox() Codec[bool] {
	return Codec[bool]{
		Name: "tx_box",
		Read: func(h *edit.Handle, i int) derive.Val[bool] {
			if k := h.Elem(i).Local(); k != "sp" {
				return derive.Missing[bool]("a " + k + " is not a shape")
			}
			n := h.FindIn(i, pathCNvSpPr)
			if n == nil {
				return derive.Missing[bool]("no cNvSpPr")
			}
			return readFlag(n, "txBox")
		},
		Write: func(h *edit.Handle, v bool) error {
			if h.Kind() != "sp" {
				return fmt.Errorf("%w: only shapes are text boxes, not a %s", ErrNoProperty, h.Kind())
			}
			if !v {
				return h.RemoveAttr(pathCNvSpPr, "txBox")
			}
			return h.SetAttr(pathCNvSpPr, "txBox", "1")
		},
		Show: strconv.FormatBool,
	}
}

// readFlag reads a boolean attribute with the schema default false, as
// the read model does (derive.AttrBool, derive.OrDefault).
func readFlag(n *tree.Node, attr string) derive.Val[bool] {
	b, err := derive.AttrBool(n, attr)
	v, dflt, err := derive.OrDefault(b, err, false)
	switch {
	case err != nil:
		return derive.Missing[bool](err.Error())
	case dflt:
		return derive.Got(v, derive.FromSpecDflt)
	}
	return derive.Got(v, derive.FromSlide)
}
