package tree

import (
	"bytes"
	"strings"

	"github.com/jhnoel/documentdriver/internal/ooxml/xmlscan"
)

// An element's attributes are not kept apart from its start tag: they
// are read from the tag's bytes when asked for (scanAttr), and become
// the element's own (x.attrs) only when one changes. An untouched
// element holds no attribute storage at all.

// attr is one attribute as written: quote and raw, the value between the
// quotes exactly as written (references undecoded); the whitespace before
// the name and the "=" with any surrounding whitespace are spans of the
// element's source, [lead0, lead1) and [eq0, eq1), or " " and "=" for an
// attribute built in code (lead0 < 0).
type attr struct {
	name                   string
	raw                    string
	lead0, lead1, eq0, eq1 int32
	quote                  byte
}

// builtAttr is an attribute set in code: one space before it, a bare "=".
func builtAttr(name string, quote byte, raw string) attr {
	return attr{name: name, raw: raw, quote: quote, lead0: -1}
}

// write writes a as it stands in a start tag of an element whose source
// is src.
func (a *attr) write(buf *bytes.Buffer, src string) {
	if a.lead0 < 0 {
		buf.WriteByte(' ')
		buf.WriteString(a.name)
		buf.WriteByte('=')
	} else {
		buf.WriteString(src[a.lead0:a.lead1])
		buf.WriteString(a.name)
		buf.WriteString(src[a.eq0:a.eq1])
	}
	buf.WriteByte(a.quote)
	buf.WriteString(a.raw)
	buf.WriteByte(a.quote)
}

// nameEnd returns the offset in the source just past the name in parsed
// element n's start tag, where its attributes begin.
func (n *Node) nameEnd() int {
	src := n.src.s
	i := int(n.start) + 1
	for i < int(n.tagEnd()) {
		if c := src[i]; xmlscan.IsSpace(c) || c == '>' || c == '/' {
			break
		}
		i++
	}
	return i
}

// scanAttr reads the attribute of parsed element n's start tag that
// follows offset pos (an attribute's end, or nameEnd). ok is false when
// none does. The tag was checked when it was parsed, so it is well
// formed.
func (n *Node) scanAttr(pos int) (a attr, next int, ok bool) {
	src := n.src.s
	lead := pos
	for xmlscan.IsSpace(src[pos]) {
		pos++
	}
	if c := src[pos]; c == '>' || c == '/' {
		return attr{}, pos, false
	}
	name := pos
	for !xmlscan.IsSpace(src[pos]) && src[pos] != '=' {
		pos++
	}
	eq0 := pos
	for src[pos] != '"' && src[pos] != '\'' {
		pos++
	}
	eq1 := pos
	q := src[pos]
	pos++
	v := strings.IndexByte(src[pos:n.tagEnd()], q)
	a = attr{
		name: src[name:eq0], raw: src[pos : pos+v], quote: q,
		lead0: off(lead), lead1: off(name), eq0: off(eq0), eq1: off(eq1),
	}
	return a, pos + v + 1, true
}

// eachAttr calls f with each attribute of element n in document order
// until f returns false.
func (n *Node) eachAttr(f func(i int, a attr) bool) {
	if n.kind != ElementNode {
		return
	}
	if n.has(fOwnAttrs) {
		for i, a := range n.x.attrs {
			if !f(i, a) {
				return
			}
		}
		return
	}
	if n.src == nil {
		return
	}
	pos := n.nameEnd()
	for i := 0; ; i++ {
		a, next, ok := n.scanAttr(pos)
		if !ok || !f(i, a) {
			return
		}
		pos = next
	}
}

// firstAttr returns element n's first attribute.
func (n *Node) firstAttr() (attr, bool) {
	var first attr
	found := false
	n.eachAttr(func(_ int, a attr) bool {
		first, found = a, true
		return false
	})
	return first, found
}

// ownAttrList makes n's attributes its own, read from its start tag the
// first time, and returns them for changing. The caller records the
// change with attrsChanged.
func (n *Node) ownAttrList() []attr {
	if !n.has(fOwnAttrs) {
		var as []attr
		n.eachAttr(func(_ int, a attr) bool {
			as = append(as, a)
			return true
		})
		n.ext().attrs = as
		n.flags |= fOwnAttrs
	}
	return n.x.attrs
}

// attrsChanged records a change to n's attributes (made in
// ownAttrList's slice, stored back in x.attrs).
func (n *Node) attrsChanged() {
	n.x.ns = n.x.ns[:0:0]
	n.eachAttr(func(_ int, a attr) bool {
		if p, ok := declOf(a.name); ok {
			n.x.ns = append(n.x.ns, nsDecl{p, xmlscan.DecodeAttr(a.raw)})
		}
		return true
	})
	n.set(fDecls, len(n.x.ns) > 0)
	n.flags |= fTagDirty
	n.markDirty()
}
