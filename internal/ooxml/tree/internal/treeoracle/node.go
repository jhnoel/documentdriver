package treeoracle

import (
	"bytes"
	"strings"
)

// Kind identifies the type of a Node.
type Kind uint8

// Node kinds.
const (
	DocumentNode Kind = iota // the document itself; its children are the prolog, root and trailing misc
	ElementNode              // <name ...>...</name> or <name .../>
	TextNode                 // character data, entity and character references as written
	CDataNode                // <![CDATA[...]]>
	CommentNode              // <!--...-->
	PINode                   // <?target ...?>, including the XML declaration
	DoctypeNode              // <!DOCTYPE ...>
)

func (k Kind) String() string {
	switch k {
	case DocumentNode:
		return "document"
	case ElementNode:
		return "element"
	case TextNode:
		return "text"
	case CDataNode:
		return "cdata"
	case CommentNode:
		return "comment"
	case PINode:
		return "pi"
	case DoctypeNode:
		return "doctype"
	}
	return "unknown"
}

// Node is one node of a lossless XML tree. A node remembers the source
// buffer it was parsed from and its byte span there; until it or one of its
// descendants changes, it is written back by copying that span.
type Node struct {
	parent   *Node
	children []*Node

	// src is the buffer the node was parsed from, shared by every node of
	// one parse (nil for nodes built in code); [start, end) is the node's
	// full span in it.
	src        *[]byte
	start, end int32

	// Element fields.
	name      string
	attrs     []*attr
	tagEnd    int32 // end of the start tag in src
	trail     int32 // start of the whitespace between the last attribute and > or />
	endAt     int32 // start of the end tag in src; -1 if self-closing, renamed or built
	kind      Kind
	selfClose bool // original (or, for new elements, preferred) empty form
	tagDirty  bool // start tag must be rebuilt from its parts
	dirty     bool // this node or a descendant changed

	// x holds what few nodes need, so that every node does not carry it.
	x *extra
}

// extra is the rarely needed part of a node.
type extra struct {
	// raw replaces the span for a non-element node that was created or
	// rewritten.
	raw []byte
	// nsHints maps prefixes used but not declared in a detached subtree
	// to the namespace URIs they are meant to denote. They are resolved,
	// and cleared, when the subtree is inserted.
	nsHints map[string]string
	// nsCache holds the URIs of this element's namespace declarations
	// once LookupPrefix has decoded them, keyed by the declaring
	// attribute and valid while that attribute keeps the value it was
	// decoded from. Nodes are not safe for concurrent use, reads
	// included.
	nsCache map[*attr]nsEntry
}

// ext returns n's extra part, creating it.
func (n *Node) ext() *extra {
	if n.x == nil {
		n.x = &extra{}
	}
	return n.x
}

// rawBytes returns the bytes replacing n's span, or nil.
func (n *Node) rawBytes() []byte {
	if n.x == nil {
		return nil
	}
	return n.x.raw
}

// hints returns n's namespace hints (nil when it has none).
func (n *Node) hints() map[string]string {
	if n.x == nil {
		return nil
	}
	return n.x.nsHints
}

// source returns the buffer n was parsed from, or nil.
func (n *Node) source() []byte {
	if n.src == nil {
		return nil
	}
	return *n.src
}

// spanBytes returns n's original bytes; n must have been parsed.
func (n *Node) spanBytes() []byte { return (*n.src)[n.start:n.end] }

// startTagBytes returns n's original start tag; n must have been parsed.
func (n *Node) startTagBytes() []byte { return (*n.src)[n.start:n.tagEnd] }

// tagTrail returns the whitespace between the last attribute and > or />
// as parsed, nil for a built element.
func (n *Node) tagTrail() []byte {
	if n.src == nil {
		return nil
	}
	end := n.tagEnd - 1
	if n.selfClose {
		end--
	}
	return (*n.src)[n.trail:end]
}

// endTag returns the end tag as written, nil if self-closing, renamed or
// built.
func (n *Node) endTag() []byte {
	if n.src == nil || n.endAt < 0 {
		return nil
	}
	return (*n.src)[n.endAt:n.end]
}

// attr is one attribute as written: quote and raw, the value between the
// quotes exactly as written (references undecoded); the whitespace before
// the name and the "=" with any surrounding whitespace are spans of the
// element's source, [lead0, lead1) and [eq0, eq1), or " " and "=" for an
// attribute built in code (lead0 < 0), so a parsed attribute holds no
// slices of its own beyond its value.
type attr struct {
	name                   string
	raw                    []byte
	lead0, lead1, eq0, eq1 int32
	quote                  byte
}

// builtAttr is an attribute set in code: one space before it, a bare "=".
func builtAttr(name string, quote byte, raw []byte) *attr {
	return &attr{name: name, raw: raw, quote: quote, lead0: -1}
}

// write writes a as it stands in a start tag of an element whose source
// is src.
func (a *attr) write(buf *bytes.Buffer, src []byte) {
	if a.lead0 < 0 {
		buf.WriteByte(' ')
		buf.WriteString(a.name)
		buf.WriteByte('=')
	} else {
		buf.Write(src[a.lead0:a.lead1])
		buf.WriteString(a.name)
		buf.Write(src[a.eq0:a.eq1])
	}
	buf.WriteByte(a.quote)
	buf.Write(a.raw)
	buf.WriteByte(a.quote)
}

// Kind reports the node's kind.
func (n *Node) Kind() Kind { return n.kind }

// Parent returns the node's parent, or nil for a document or detached node.
func (n *Node) Parent() *Node { return n.parent }

// Children returns the node's children in document order. The slice must
// not be modified; use the editing methods instead.
func (n *Node) Children() []*Node { return n.children }

// Elements returns the element children of n.
func (n *Node) Elements() []*Node {
	var out []*Node
	for _, c := range n.children {
		if c.kind == ElementNode {
			out = append(out, c)
		}
	}
	return out
}

// Name returns an element's qualified name as written, such as "p:sp".
// For a processing instruction it returns the target.
func (n *Node) Name() string {
	if n.kind == PINode {
		return piTarget(n.Raw())
	}
	return n.name
}

// Prefix returns the prefix of an element's name, or "" if unprefixed.
func (n *Node) Prefix() string {
	p, _ := splitQName(n.name)
	return p
}

// Local returns the local part of an element's name.
func (n *Node) Local() string {
	_, l := splitQName(n.name)
	return l
}

// Space returns the namespace URI of an element's name, resolved through
// the declarations in scope. It returns "" for an element in no namespace
// or with an undeclared prefix.
func (n *Node) Space() string {
	uri, _ := n.LookupPrefix(n.Prefix())
	return uri
}

// Is reports whether n is an element with namespace URI uri and local name
// local.
func (n *Node) Is(uri, local string) bool {
	return n.kind == ElementNode && n.Local() == local && n.Space() == uri
}

// Raw returns the bytes of a non-element node as they will be written
// (comment, PI, CDATA and doctype nodes include their delimiters; text is
// undecoded). For an element it returns its serialisation.
func (n *Node) Raw() []byte {
	if n.kind == ElementNode || n.kind == DocumentNode {
		return n.Bytes()
	}
	if raw := n.rawBytes(); raw != nil {
		return raw
	}
	return n.spanBytes()
}

// Modified reports whether the node or any descendant changed since it was
// parsed. Newly built nodes count as modified.
func (n *Node) Modified() bool { return n.dirty }

// Span returns the node's byte span in the buffer it was parsed from, and
// false if the node was built in code.
func (n *Node) Span() (start, end int, ok bool) {
	if n.src == nil {
		return 0, 0, false
	}
	return int(n.start), int(n.end), true
}

// SpanIn returns the node's byte span in the bytes d was parsed from, and
// false if the node was not parsed from them (built in code, or parsed
// from a fragment or another document). Nodes keep their span when they
// are modified or moved, so it identifies an original node across edits.
func (n *Node) SpanIn(d *Document) (start, end int, ok bool) {
	a, b := n.source(), d.node.source()
	if len(a) == 0 || len(a) != len(b) || &a[0] != &b[0] {
		return 0, 0, false
	}
	return int(n.start), int(n.end), true
}

// IsDecl reports whether n is the XML declaration.
func (n *Node) IsDecl() bool {
	return n.kind == PINode && strings.EqualFold(n.Name(), "xml")
}

func piTarget(raw []byte) string {
	i := 2
	j := i
	for j < len(raw) && !isSpace(raw[j]) && raw[j] != '?' {
		j++
	}
	return string(raw[i:j])
}

func splitQName(q string) (prefix, local string) {
	if i := strings.IndexByte(q, ':'); i >= 0 {
		return q[:i], q[i+1:]
	}
	return "", q
}

// markDirty records that n changed, and with it every ancestor.
func (n *Node) markDirty() {
	for p := n; p != nil && !p.dirty; p = p.parent {
		p.dirty = true
	}
}

// Document is a parsed XML part.
type Document struct {
	node *Node
}

// Node returns the document node, whose children are the prolog, the root
// element and any trailing comments or PIs.
func (d *Document) Node() *Node { return d.node }

// Root returns the document element.
func (d *Document) Root() *Node {
	for _, c := range d.node.children {
		if c.kind == ElementNode {
			return c
		}
	}
	return nil
}

// Modified reports whether anything in the document changed.
func (d *Document) Modified() bool { return d.node.dirty }

// Original returns the bytes the document was parsed from.
func (d *Document) Original() []byte { return d.node.source() }

// nsEntry is a decoded namespace declaration: the raw value it was
// decoded from (by identity) and the URI.
type nsEntry struct {
	raw []byte
	uri string
}

// sameBytes reports whether a and b are the same slice of the same
// array, not merely equal.
func sameBytes(a, b []byte) bool {
	return len(a) == len(b) && (len(a) == 0 || &a[0] == &b[0])
}

// declValue returns the decoded value of a, a namespace declaration of
// element n, decoding it once while its value stays the same.
func (n *Node) declValue(a *attr) string {
	if n.x != nil {
		if c, ok := n.x.nsCache[a]; ok && sameBytes(c.raw, a.raw) {
			return c.uri
		}
	}
	uri := decodeAttr(a.raw)
	x := n.ext()
	if x.nsCache == nil {
		x.nsCache = map[*attr]nsEntry{}
	}
	x.nsCache[a] = nsEntry{raw: a.raw, uri: uri}
	return uri
}
