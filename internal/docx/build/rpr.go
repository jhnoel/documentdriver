package build

import (
	"fmt"
	"strconv"

	docxedit "github.com/jhnoel/documentdriver/internal/docx/edit"
	"github.com/jhnoel/documentdriver/internal/docx/html"
	core "github.com/jhnoel/documentdriver/internal/ooxml/edit"
	"github.com/jhnoel/documentdriver/internal/ooxml/tree"
)

// runProperty is one w:rPr child a formatting asks for: its name and its
// attributes (local name, value), "val" when it has one.
type runProperty struct {
	local string
	attrs [][2]string
}

// runPropertiesOf lists the run properties formatting f writes (docxdriver's
// styles.rs append_rpr): bold, italic, underline (single), strike,
// vertical alignment, highlight, explicit resets (w:b w:val="0" and so on
// for a property the HTML switched off), colour, size in half-points and
// font family (ascii and hAnsi). The list's order is irrelevant: each goes
// where the schema's declaration order puts it.
func runPropertiesOf(f html.Fmt) []runProperty {
	var out []runProperty
	val := func(local, v string) { out = append(out, runProperty{local, [][2]string{{"val", v}}}) }
	if f.Bold {
		out = append(out, runProperty{local: "b"})
	}
	if f.Italic {
		out = append(out, runProperty{local: "i"})
	}
	if f.Underline {
		val("u", "single")
	}
	if f.Strike {
		out = append(out, runProperty{local: "strike"})
	}
	if f.VertAlign.OK {
		val("vertAlign", f.VertAlign.V.DocxVal())
	}
	if f.Highlight.OK {
		val("highlight", f.Highlight.V.String())
	}
	for _, r := range []struct {
		bit    uint8
		on     bool
		local  string
		action string
	}{{1, f.Bold, "b", "0"}, {2, f.Italic, "i", "0"}, {4, f.Underline, "u", "none"}, {8, f.Strike, "strike", "0"}, {16, f.VertAlign.OK, "vertAlign", "baseline"}} {
		if f.Resets&r.bit != 0 && !r.on {
			val(r.local, r.action)
		}
	}
	if f.Color.OK {
		val("color", fmt.Sprintf("%06X", f.Color.V))
	}
	if f.FontSize.OK {
		val("sz", strconv.FormatUint(uint64(f.FontSize.V), 10))
	}
	if f.FontFamily.OK {
		out = append(out, runProperty{"rFonts", [][2]string{{"ascii", string(f.FontFamily.V)}, {"hAnsi", string(f.FontFamily.V)}}})
	}
	return out
}

// runProperties gives run r (placed, held by h) the run properties of f:
// a w:rPr where the schema puts it, and each property in it through
// EnsureIn, so the children stand in the schema's declaration order.
// The default formatting writes no w:rPr.
func runProperties(h *core.Handle, r *tree.Node, f html.Fmt) error {
	props := runPropertiesOf(f)
	if len(props) == 0 {
		return nil
	}
	rpr, err := h.EnsureIn(r, docxedit.W("rPr"))
	if err != nil {
		return err
	}
	for _, p := range props {
		c, err := h.EnsureIn(rpr, docxedit.W(p.local))
		if err != nil {
			return err
		}
		for _, a := range p.attrs {
			if err := h.SetNodeAttr(c, wq(c, a[0]), a[1]); err != nil {
				return err
			}
		}
	}
	return nil
}
