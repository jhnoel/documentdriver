package treeoracle

import "bytes"

// Bytes serialises the document. An unmodified document is returned as the
// exact bytes it was parsed from.
func (d *Document) Bytes() []byte {
	if !d.node.dirty {
		return bytes.Clone(d.node.source())
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

func (n *Node) writeTo(buf *bytes.Buffer) {
	switch n.kind {
	case DocumentNode:
		if !n.dirty {
			buf.Write(n.spanBytes())
			return
		}
		for _, c := range n.children {
			c.writeTo(buf)
		}
	case ElementNode:
		n.writeElement(buf)
	default:
		buf.Write(n.Raw())
	}
}

func (n *Node) writeElement(buf *bytes.Buffer) {
	if !n.dirty && n.src != nil {
		buf.Write(n.spanBytes())
		return
	}
	empty := n.selfClose && len(n.children) == 0
	if !n.tagDirty && n.src != nil && empty == n.selfClose {
		buf.Write(n.startTagBytes())
	} else {
		n.writeStartTag(buf, empty)
	}
	if empty {
		return
	}
	for _, c := range n.children {
		c.writeTo(buf)
	}
	if et := n.endTag(); et != nil {
		buf.Write(et)
		return
	}
	buf.WriteString("</")
	buf.WriteString(n.name)
	buf.WriteByte('>')
}

// StartTag returns an element's start tag exactly as it will be written:
// "<p:sp>", or the whole "<a:off .../>" for an element written in the
// empty form. It returns nil for other kinds of node.
func (n *Node) StartTag() []byte {
	if n.kind != ElementNode {
		return nil
	}
	empty := n.selfClose && len(n.children) == 0
	if !n.tagDirty && n.src != nil && empty == n.selfClose {
		return bytes.Clone(n.startTagBytes())
	}
	var buf bytes.Buffer
	n.writeStartTag(&buf, empty)
	return buf.Bytes()
}

// EndTag returns an element's end tag exactly as it will be written, or
// nil when the element is written in the empty form.
func (n *Node) EndTag() []byte {
	if n.kind != ElementNode || (n.selfClose && len(n.children) == 0) {
		return nil
	}
	if et := n.endTag(); et != nil {
		return bytes.Clone(et)
	}
	return []byte("</" + n.name + ">")
}

// StartTagModified reports whether the start tag as it will be written
// differs from the one parsed. Unlike Modified, it compares bytes, so a
// value set and then set back reports false. A node built in code always
// reports true.
func (n *Node) StartTagModified() bool {
	if n.kind != ElementNode || n.src == nil {
		return true
	}
	return !bytes.Equal(n.StartTag(), n.startTagBytes())
}

// writeStartTag rebuilds the start tag from its parts: each attribute keeps
// its leading whitespace, "=" spacing, quote and raw value.
func (n *Node) writeStartTag(buf *bytes.Buffer, empty bool) {
	buf.WriteByte('<')
	buf.WriteString(n.name)
	for _, a := range n.attrs {
		a.write(buf, n.source())
	}
	buf.Write(n.tagTrail())
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
	for _, a := range n.attrs {
		if skip(a.name) {
			drop = true
			break
		}
	}
	if !drop {
		return n.StartTag()
	}
	empty := n.selfClose && len(n.children) == 0
	var buf bytes.Buffer
	buf.WriteByte('<')
	buf.WriteString(n.name)
	for _, a := range n.attrs {
		if skip(a.name) {
			continue
		}
		a.write(&buf, n.source())
	}
	buf.Write(n.tagTrail())
	if empty {
		buf.WriteString("/>")
	} else {
		buf.WriteByte('>')
	}
	return buf.Bytes()
}
