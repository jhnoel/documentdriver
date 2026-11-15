// Package htmlx is the shared HTML front end of the pptx and docx tools: a
// tokenizer and tree builder for the HTML their text inputs accept. It
// knows nothing about either format; each keeps its own lowering.
//
// ParseHTML parses HTML5 syntax (see html5.go). Its strict mode is the
// agent contract's input policy (docs/contract.md, section 7): only the
// repairs that cannot change meaning are applied, and what the lenient
// builder would ignore or invent is refused. pptx text uses strict mode;
// docx input runs the lenient mode that mirrors docxdriver 0.5.2 until
// its recorded differences are listed.
package htmlx

import (
	"strings"
)

// Attr is an attribute.
type Attr struct {
	Name, Value string
}

// Node is an element (Tag set) or a text node (Tag empty).
type Node struct {
	Tag   string
	Attrs []Attr
	Kids  []*Node
	Text  string
	Off   int // byte offset of the start tag or text in the input
}

// Attr returns the value of the first attribute named name.
func (n *Node) Attr(name string) (string, bool) {
	for _, a := range n.Attrs {
		if a.Name == name {
			return a.Value, true
		}
	}
	return "", false
}

// TextContent concatenates the text of n and its descendants. Trees from
// this package are at most MaxDepth deep, which bounds the recursion.
func (n *Node) TextContent() string {
	var b strings.Builder
	n.textContent(&b)
	return b.String()
}

// textContent appends the text of n and its descendants to b.
func (n *Node) textContent(b *strings.Builder) {
	if n.Tag == "" {
		b.WriteString(n.Text)
		return
	}
	for _, k := range n.Kids {
		k.textContent(b)
	}
}

// appendText adds text to n, merging it into a trailing text node.
func (n *Node) appendText(s string, off int) {
	if k := len(n.Kids); k > 0 && n.Kids[k-1].Tag == "" {
		n.Kids[k-1].Text += s
		return
	}
	n.Kids = append(n.Kids, &Node{Text: s, Off: off})
}

// MaxDepth is the deepest element nesting the parser accepts.
const MaxDepth = 128

// Storable reports whether XML 1.0 can carry c: a format's lowering
// refuses text that holds a character its parts cannot store.
func Storable(c rune) bool {
	return c == '\t' || c == '\n' || c == '\r' || c >= 0x20 && c <= 0xD7FF || c >= 0xE000 && c <= 0xFFFD || c >= 0x10000 && c <= 0x10FFFF
}
