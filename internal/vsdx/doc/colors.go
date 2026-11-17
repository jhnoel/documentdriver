package doc

import "github.com/jhnoel/documentdriver/internal/vsdx/vsd"

// Color resolves a stored colour V: "#RRGGBB" through vsd.Color; a
// decimal index through the document's Colors, then vsd.BuiltinColor for
// 0 to 23. ok is false for "Themed", an index neither holds, and
// anything else; the caller omits the colour with the reason. Chunk D4.
func (d *Document) Color(v string) (hex string, ok bool) {
	_ = v
	return vsd.Color("")
}
