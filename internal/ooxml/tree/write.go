package tree

import "bytes"

// Bytes serialises the document. An unmodified document is returned as the
// exact bytes it was parsed from.
func (d *Document) Bytes() []byte {
	if !d.node.has(fDirty) {
		return []byte(d.node.source())
	}
	return d.node.Bytes()
}

// Bytes serialises the subtree rooted at n. Unmodified nodes are copied
// from their original bytes.
func (n *Node) Bytes() []byte {
	var buf bytes.Buffer
	n.writeTo(&buf)
	return buf.Bytes()
}

// writeTo writes node n's serialisation to buf.
func (n *Node) writeTo(buf *bytes.Buffer) {
	switch n.kind {
	case DocumentNode:
		if !n.has(fDirty) {
			buf.WriteString(n.span())
			return
		}
		for c := n.first; c != nil; c = c.next {
			c.writeTo(buf)
		}
	case ElementNode:
		n.writeElement(buf)
	default:
		buf.WriteString(n.RawString())
	}
}

// writeElement writes element n's tag, children and end tag to buf.
func (n *Node) writeElement(buf *bytes.Buffer) {
	if !n.has(fDirty) && n.src != nil {
		buf.WriteString(n.span())
		return
	}
	empty := n.has(fSelfClose) && n.first == nil
	if !n.has(fTagDirty) && n.src != nil && empty == n.has(fSelfClose) {
		buf.WriteString(n.startTagText())
	} else {
		n.writeStartTag(buf, empty)
	}
	if empty {
		return
	}
	for c := n.first; c != nil; c = c.next {
		c.writeTo(buf)
	}
	if et := n.endTag(); et != "" {
		buf.WriteString(et)
		return
	}
	buf.WriteString("</")
	buf.WriteString(n.qname())
	buf.WriteByte('>')
}

// StartTag returns an element's start tag exactly as it will be written:
// "<p:sp>", or the whole "<a:off .../>" for an element written in the
// empty form. It returns nil for other kinds of node.
func (n *Node) StartTag() []byte {
	if n.kind != ElementNode {
		return nil
	}
	empty := n.has(fSelfClose) && n.first == nil
	if !n.has(fTagDirty) && n.src != nil && empty == n.has(fSelfClose) {
		return []byte(n.startTagText())
	}
	var buf bytes.Buffer
	n.writeStartTag(&buf, empty)
	return buf.Bytes()
}

// EndTag returns an element's end tag exactly as it will be written, or
// nil when the element is written in the empty form.
func (n *Node) EndTag() []byte {
	if n.kind != ElementNode || (n.has(fSelfClose) && n.first == nil) {
		return nil
	}
	if et := n.endTag(); et != "" {
		return []byte(et)
	}
	return []byte("</" + n.qname() + ">")
}

// StartTagModified reports whether the start tag as it will be written
// differs from the one parsed. Unlike Modified, it compares bytes, so a
// value set and then set back reports false. A node built in code always
// reports true.
func (n *Node) StartTagModified() bool {
	if n.kind != ElementNode || n.src == nil {
		return true
	}
	return string(n.StartTag()) != n.startTagText()
}

// writeStartTag rebuilds the start tag from its parts: each attribute keeps
// its leading whitespace, "=" spacing, quote and raw value.
func (n *Node) writeStartTag(buf *bytes.Buffer, empty bool) {
	buf.WriteByte('<')
	buf.WriteString(n.qname())
	switch {
	case n.has(fOwnAttrs):
		for _, a := range n.x.attrs {
			a.write(buf, n.source())
		}
	case n.src != nil:
		// The attributes as parsed: the bytes from the name to the
		// trailing whitespace.
		buf.WriteString(n.src.s[n.nameEnd():n.trailAt()])
	}
	buf.WriteString(n.tagTrail())
	if empty {
		buf.WriteString("/>")
	} else {
		buf.WriteByte('>')
	}
}

// StartTagWithout returns the start tag as StartTag does, with every
// attribute whose qualified name skip reports true left out together with
// its leading whitespace. The verifier compares start tags this way when a
// plan declared that some attributes may change. When skip leaves out
// nothing it equals StartTag.
func (n *Node) StartTagWithout(skip func(qname string) bool) []byte {
	if n.kind != ElementNode {
		return nil
	}
	drop := false
	n.eachAttr(func(_ int, a attr) bool {
		drop = skip(a.name)
		return !drop
	})
	if !drop {
		return n.StartTag()
	}
	empty := n.has(fSelfClose) && n.first == nil
	var buf bytes.Buffer
	buf.WriteByte('<')
	buf.WriteString(n.qname())
	n.eachAttr(func(_ int, a attr) bool {
		if !skip(a.name) {
			a.write(&buf, n.source())
		}
		return true
	})
	buf.WriteString(n.tagTrail())
	if empty {
		buf.WriteString("/>")
	} else {
		buf.WriteByte('>')
	}
	return buf.Bytes()
}
