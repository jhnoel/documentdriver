package integrity

import (
	"unicode/utf8"

	"github.com/jhnoel/documentdriver/internal/ooxml/tree"
)

// frame is an open element of a scan: its local name and whether the
// scan ignores its subtree. Namespace bindings live in tree.NSStack,
// pushed and popped beside these frames by the visitor.
type frame struct {
	local   string
	skipped bool // this element started a subtree the scan ignores
}

// utf16Len is the length of s in UTF-16 code units, Excel's character
// count.
func utf16Len(s string) int {
	n := 0
	for i := 0; i < len(s); {
		if s[i] < utf8.RuneSelf {
			n++
			i++
			continue
		}
		r, w := utf8.DecodeRuneInString(s[i:])
		if r >= 0x10000 {
			n += 2
		} else {
			n++
		}
		i += w
	}
	return n
}

// isText reports whether n is character data.
func isText(n *tree.Node) bool {
	return n.Kind() == tree.TextNode || n.Kind() == tree.CDataNode
}
