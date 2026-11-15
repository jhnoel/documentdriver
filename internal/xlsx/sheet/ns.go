package sheet

import (
	"strings"

	"github.com/jhnoel/documentdriver/internal/ooxml/tree"
)

// frame is an open element of a scan. Namespace bindings live in
// tree.NSStack, pushed and popped beside these frames by the visitor;
// the stack is the namespace resolution, these frames are the scan's
// own per-element state (see internal/xlsx/integrity/scan.go).
type frame struct {
	local   string
	main    bool
	skipped bool // this element is inside a subtree the scan ignores
}

// isText reports whether n carries character data.
func isText(n *tree.Node) bool {
	return n.Kind() == tree.TextNode || n.Kind() == tree.CDataNode
}

// textAcc accumulates the character data of one element without a copy
// when there is a single text node (the common case).
type textAcc struct {
	s     string
	b     strings.Builder
	multi bool
	has   bool
}

// reset clears the accumulated text.
func (t *textAcc) reset() {
	t.s, t.has, t.multi = "", false, false
	t.b.Reset()
}

// add appends character data to the accumulated text.
func (t *textAcc) add(data string) {
	switch {
	case !t.has:
		t.s, t.has = data, true
	case !t.multi:
		t.b.Reset()
		t.b.WriteString(t.s)
		t.b.WriteString(data)
		t.multi = true
	default:
		t.b.WriteString(data)
	}
}

// String returns the accumulated text.
func (t *textAcc) String() string {
	if t.multi {
		return t.b.String()
	}
	return t.s
}
