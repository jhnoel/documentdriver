package ops

import (
	"github.com/jhnoel/documentdriver/internal/vsdx/emit"
	"github.com/jhnoel/documentdriver/internal/vsdx/inherit"
	"github.com/jhnoel/documentdriver/internal/vsdx/sheet"
)

// textBase returns the emit.Base of sh: the stored value each of its
// Character and Paragraph rows takes from below the shape (its master
// shape's row of the same IX, else the style chain), the value read
// compares a row's own cells against. A style sheet stores only row 0
// of each section, and Visio formats a row past it with the text
// style's values, so a row above 0 that nothing below stores takes
// the style chain's row 0.
func textBase(r *inherit.Resolver, sh *inherit.Shape) emit.Base {
	below := *sh
	below.Sheet = nil
	style := below
	style.HasMaster, style.HasMShape = false, false
	return func(section string, ix int, cell string) (string, bool) {
		c, _, ok := r.Cell(&below, sheet.IndexedRowCell(section, -1, ix, cell))
		if !ok && ix > 0 {
			c, _, ok = r.Cell(&style, sheet.IndexedRowCell(section, -1, 0, cell))
		}
		if !ok {
			return "", false
		}
		return c.V, true
	}
}
