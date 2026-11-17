package inherit

import (
	"github.com/jhnoel/documentdriver/internal/vsdx/sheet"
	"github.com/jhnoel/documentdriver/internal/vsdx/vsd"
)

// styleCell walks the style chain of category c from style id, at most
// MaxStyleDepth steps. Chunk H3.
func (r *Resolver) styleCell(c vsd.Category, id uint32, k sheet.Key) (sheet.Cell, bool) {
	_, _, _ = c, id, k
	return sheet.Cell{}, false
}
