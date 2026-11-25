package model

import (
	"strconv"

	"github.com/jhnoel/documentdriver/internal/vsdx/inherit"
	"github.com/jhnoel/documentdriver/internal/vsdx/sheet"
	"github.com/jhnoel/documentdriver/internal/vsdx/vsd"
)

// textBlockMargins are the margin cells in TextBlockInfo.Margins order.
var textBlockMargins = [4]string{vsd.CellLeftMargin, vsd.CellTopMargin, vsd.CellRightMargin, vsd.CellBottomMargin}

// textBlockCells reads one object's text block cells: the effective
// cell of name, with Local set when the shape or its master stores it
// and best the most inherited origin read so far.
type textBlockCells struct {
	r     *inherit.Resolver
	s     *inherit.Shape
	local bool
	best  inherit.Origin
}

// cell returns the effective cell name and whether a sheet stores it.
func (c *textBlockCells) cell(name string) (sheet.Cell, bool) {
	v, origin, ok := c.r.Cell(c.s, sheet.ShapeCell(name))
	if !ok {
		return v, false
	}
	if origin == inherit.Local || origin == inherit.Master {
		c.local = true
	}
	c.best = boxOrigin(c.best, origin)
	return v, true
}

// textBlockOmit records that member of the text block, stored as v,
// is not shown.
func textBlockOmit(o *Object, member, v string) {
	reason := "not a number"
	if v == vsd.ValueThemed {
		reason = "themed value not derived"
	}
	o.Omitted = append(o.Omitted, Omission{Prop: "text_block." + member, Reason: reason})
}

// textBlockInfoOf sets o.TextBlock on a shape, group or connector from
// its effective VerticalAlign, margin cells and TextBkgnd and
// TextBkgndTrans (docs/contract.md §3, Text blocks): VerticalAlign 0,
// 1, 2 as top, middle, bottom; margins in points (all four, or none);
// TextBkgnd through vsd.TextBackground, an index resolved by the
// document's colours. It is set when the shape or its master stores
// one of the cells (Local; values from the style sheets alone are
// not the object's, as the HTML shows only the object's own
// formatting), or, with all (Options.Draw or Full), on an object with
// text. The origin is in origins.text_block when Local. A member that
// is themed or does not parse is left out with text_block.<member> in
// omitted, and one no sheet stores is left out silently.
func textBlockInfoOf(o *Object, r *inherit.Resolver, s *inherit.Shape, all bool) {
	if o.Kind != Shape && o.Kind != Group && o.Kind != Connector {
		return
	}
	var tb TextBlockInfo
	c := textBlockCells{r: r, s: s}
	if v, ok := c.cell(vsd.CellVerticalAlign); ok {
		switch n, isNum := vsd.Number(v.V); {
		case isNum && n == 0:
			tb.VAlign, tb.HasVAlign = "top", true
		case isNum && n == 1:
			tb.VAlign, tb.HasVAlign = "middle", true
		case isNum && n == 2:
			tb.VAlign, tb.HasVAlign = "bottom", true
		default:
			textBlockOmit(o, "valign", v.V)
		}
	}
	got := 0
	for i, name := range textBlockMargins {
		v, ok := c.cell(name)
		if !ok {
			continue
		}
		n, isNum := vsd.Number(v.V)
		if !isNum {
			textBlockOmit(o, "margins", v.V)
			continue
		}
		tb.Margins[i] = n * vsd.PointsPerInch
		got++
	}
	tb.HasMargins = got == 4
	if v, ok := c.cell(vsd.CellTextBkgnd); ok {
		none, hex, index, isBg := vsd.TextBackground(v.V)
		if isBg && index >= 0 {
			hex, isBg = r.Doc().Color(strconv.Itoa(index))
		}
		switch {
		case !isBg:
			textBlockOmit(o, "background", v.V)
		case none:
			tb.HasBg, tb.BgNone = true, true
		default:
			tb.HasBg, tb.BgHex = true, hex
			if t, ok := c.cell(vsd.CellTextBkgndTrans); ok {
				if n, isNum := vsd.Number(t.V); isNum {
					tb.BgTrans = n * 100
				} else {
					textBlockOmit(o, "background", t.V)
					tb.HasBg = false
				}
			}
		}
	}
	tb.Local = c.local
	if !tb.Local && !(all && o.HasText) || (!tb.HasVAlign && !tb.HasMargins && !tb.HasBg) {
		return
	}
	o.TextBlock = &tb
	if tb.Local {
		// Only a shown text block has an origin (read shows it only
		// when Local).
		setOrigin(o, "text_block", c.best)
	}
}
