package tree

import (
	"strings"

	"github.com/jhnoel/documentdriver/internal/ooxml/xmlscan"
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

// String returns the name of kind k, or "unknown" for an unrecognised kind.
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
	parent *Node
	// first is the first child; next and prev link the siblings (links.go).
	first, next, prev *Node

	// src is the source the node was parsed from, shared by every node of
	// one parse (nil for nodes built in code); [start, end) is the node's
	// full span in it.
	src *source
	// x holds what few nodes need, so that every node does not carry it.
	x          *extra
	start, end int32

	// Element fields, packed so that a node is 64 bytes on 64-bit
	// platforms (TestBytesPerNode). The name is the nameLen bytes after the start tag's
	// "<", or x.name (renamed, built, or longer than 65,534 bytes); the
	// start tag ends tagLen bytes after start, or at x.tagEnd. The
	// whitespace before the tag's > and the end tag's position are read
	// from the source when needed (trailAt, endTagAt). An element's
	// attributes are read from its start tag, unless it owns them
	// (fOwnAttrs): then they are x.attrs.
	tagLen  uint16
	nameLen uint16
	kind    Kind
	flags   flags
	// local is where the local part of the name starts in it: past the
	// ':', or 0 for none; localFar when the prefix is too long to count
	// here, and x.local holds it.
	local uint16
}

// flags are a node's boolean state.
type flags uint8

const (
	fSelfClose flags = 1 << iota // original (or, for new elements, preferred) empty form
	fTagDirty                    // start tag must be rebuilt from its parts
	fDirty                       // this node or a descendant changed
	fOwnAttrs                    // the attributes are x.attrs, not the start tag's
	fDecls                       // the element declares namespaces: x.ns lists them
	fEndLost                     // the end tag as parsed no longer applies (renamed)
)

// has reports whether all of f are set on n.
func (n *Node) has(f flags) bool { return n.flags&f == f }

// set sets or clears f on n.
func (n *Node) set(f flags, on bool) {
	if on {
		n.flags |= f
	} else {
		n.flags &^= f
	}
}

const (
	tagLenFar = 1<<16 - 1 // tagLen when the tag is longer: x.tagEnd holds its end
	localFar  = 1<<16 - 1 // local when the prefix is longer: x.local holds it
)

// extra is the rarely needed part of a node.
type extra struct {
	// name is an element's qualified name when it is not in the source
	// (nameLen is 0): renamed, built, or too long to count in nameLen.
	name string
	// tagEnd is the end of the start tag when tagLen cannot hold it, and
	// local where the name's local part starts when local cannot.
	tagEnd, local int32
	// raw replaces the span for a non-element node that was created or
	// rewritten, when hasRaw is set.
	raw    string
	hasRaw bool
	// nsHints maps prefixes used but not declared in a detached subtree
	// to the namespace URIs they are meant to denote. They are resolved,
	// and cleared, when the subtree is inserted.
	nsHints map[string]string
	// attrs are the element's attributes once they are its own
	// (ownAttrs).
	attrs []attr
	// ns are the element's namespace declarations in document order
	// (decls), made when it is parsed and remade when its attributes
	// change, so that a lookup does not read the start tag. Reading a
	// tree changes nothing in it: reads are safe for concurrent use.
	ns []nsDecl
}

// nsDecl is one namespace declaration: the prefix ("" for the default
// namespace) and the URI, decoded.
type nsDecl struct {
	prefix, uri string
}

// source is the input of one parse, shared by its nodes. Names, values
// and text without references are substrings of it, so reading them
// copies nothing.
type source struct {
	s string
}

// ext returns n's extra part, creating it.
func (n *Node) ext() *extra {
	if n.x == nil {
		n.x = &extra{}
	}
	return n.x
}

// rawOverride returns the text replacing n's span, if any.
func (n *Node) rawOverride() (string, bool) {
	if n.x == nil || !n.x.hasRaw {
		return "", false
	}
	return n.x.raw, true
}

// hints returns n's namespace hints (nil when it has none).
func (n *Node) hints() map[string]string {
	if n.x == nil {
		return nil
	}
	return n.x.nsHints
}

// source returns the text n was parsed from, or "".
func (n *Node) source() string {
	if n.src == nil {
		return ""
	}
	return n.src.s
}

// span returns n's original text; n must have been parsed.
func (n *Node) span() string { return n.src.s[n.start:n.end] }

// startTagText returns n's original start tag; n must have been parsed.
func (n *Node) startTagText() string { return n.src.s[n.start:n.tagEnd()] }

// tagEnd returns the end of parsed element n's start tag in the source.
func (n *Node) tagEnd() int32 {
	if n.tagLen == tagLenFar {
		return n.x.tagEnd
	}
	return n.start + int32(n.tagLen)
}

// setTagEnd records the end of parsed element n's start tag.
func (n *Node) setTagEnd(end int32) {
	if l := end - n.start; l < tagLenFar {
		n.tagLen = uint16(l) //nolint:gosec // G115: checked just above
		return
	}
	n.tagLen = tagLenFar
	n.ext().tagEnd = end
}

// tagClose returns where the start tag's > (or />) begins.
func (n *Node) tagClose() int32 {
	end := n.tagEnd() - 1
	if n.has(fSelfClose) {
		end--
	}
	return end
}

// trailAt returns the start of the whitespace between parsed element
// n's last attribute (or name) and its > or />.
func (n *Node) trailAt() int32 {
	end := n.tagClose()
	for end > n.start && xmlscan.IsSpace(n.src.s[end-1]) {
		end--
	}
	return end
}

// tagTrail returns the whitespace between the last attribute and > or />
// as parsed, "" for a built element.
func (n *Node) tagTrail() string {
	if n.src == nil {
		return ""
	}
	return n.src.s[n.trailAt():n.tagClose()]
}

// endTagAt returns the start of parsed element n's end tag in the
// source, -1 if it is self-closing, renamed or built: the end tag is
// "</", the name as parsed, optional whitespace and ">", ending at end.
func (n *Node) endTagAt() int32 {
	if n.src == nil || n.kind != ElementNode || n.flags&(fSelfClose|fEndLost) != 0 {
		return -1
	}
	e := n.end - 1
	for xmlscan.IsSpace(n.src.s[e-1]) {
		e--
	}
	return e - off(len(n.qname())) - 2
}

// endTag returns the end tag as written, "" if self-closing, renamed or
// built.
func (n *Node) endTag() string {
	at := n.endTagAt()
	if at < 0 {
		return ""
	}
	return n.src.s[at:n.end]
}

// Kind reports the node's kind.
func (n *Node) Kind() Kind { return n.kind }

// Parent returns the node's parent, or nil for a document or detached node.
func (n *Node) Parent() *Node { return n.parent }

// Children returns the node's children in document order, in a new
// slice. ChildrenSeq walks them without one.
func (n *Node) Children() []*Node {
	var out []*Node
	for c := n.first; c != nil; c = c.next {
		out = append(out, c)
	}
	return out
}

// Elements returns the element children of n, in a new slice.
// ElementsSeq walks them without one.
func (n *Node) Elements() []*Node {
	var out []*Node
	for c := n.first; c != nil; c = c.next {
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
		return n.piName()
	}
	return n.qname()
}

// piName returns the target of processing instruction node n.
func (n *Node) piName() string { return piTarget(n.RawString()) }

// qname returns element n's qualified name ("" for other kinds of node
// but processing instructions, whose target is Name's).
func (n *Node) qname() string {
	if n.nameLen > 0 {
		return n.src.s[n.start+1 : n.start+1+int32(n.nameLen)]
	}
	if n.x != nil {
		return n.x.name
	}
	return ""
}

// Prefix returns the prefix of an element's name, or "" if unprefixed.
func (n *Node) Prefix() string {
	l := n.localAt()
	if l == 0 {
		return ""
	}
	return n.qname()[:l-1]
}

// Local returns the local part of an element's name.
func (n *Node) Local() string {
	q := n.qname()
	if n.local == localFar {
		return q[n.x.local:]
	}
	return q[n.local:]
}

// localAt returns where the local part of n's name starts in it.
func (n *Node) localAt() int32 {
	if n.local == localFar {
		return n.x.local
	}
	return int32(n.local)
}

// setName sets element n's qualified name (x.name), and where its local
// part starts, so that Prefix and Local do not search it.
func (n *Node) setName(name string) {
	n.ext().name = name
	n.nameLen = 0
	n.setLocal(name)
}

// setLocal records where the local part of name, n's, starts.
func (n *Node) setLocal(name string) {
	if l := strings.IndexByte(name, ':') + 1; l < localFar {
		n.local = uint16(l) //nolint:gosec // G115: checked just above
	} else {
		n.local = localFar
		n.ext().local = off(l)
	}
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
// undecoded). For an element it returns its serialisation. The slice is
// the caller's.
func (n *Node) Raw() []byte {
	if n.kind == ElementNode || n.kind == DocumentNode {
		return n.Bytes()
	}
	return []byte(n.RawString())
}

// RawString is Raw as a string. For a node other than an element or
// the document it copies nothing.
func (n *Node) RawString() string {
	if n.kind == ElementNode || n.kind == DocumentNode {
		return string(n.Bytes())
	}
	if raw, ok := n.rawOverride(); ok {
		return raw
	}
	return n.span()
}

// Modified reports whether the node or any descendant changed since it was
// parsed. Newly built nodes count as modified.
func (n *Node) Modified() bool { return n.has(fDirty) }

// Span returns the node's byte span in the buffer it was parsed from, and
// false if the node was built in code.
func (n *Node) Span() (start, end int, ok bool) {
	if n.src == nil {
		return 0, 0, false
	}
	return int(n.start), int(n.end), true
}

// SpanIn returns the node's byte span in the bytes d was parsed from, and
// false if the node was not parsed with d (built in code, or parsed from
// a fragment or by another Parse, of the same bytes or not). Nodes keep
// their span when they are modified or moved, so it identifies an
// original node across edits.
func (n *Node) SpanIn(d *Document) (start, end int, ok bool) {
	if n.src == nil || n.src != d.node.src {
		return 0, 0, false
	}
	return int(n.start), int(n.end), true
}

// IsDecl reports whether n is the XML declaration.
func (n *Node) IsDecl() bool {
	return n.kind == PINode && strings.EqualFold(n.Name(), "xml")
}

// piTarget returns the processing instruction target in raw markup.
func piTarget(raw string) string {
	i := 2
	j := i
	for j < len(raw) && !xmlscan.IsSpace(raw[j]) && raw[j] != '?' {
		j++
	}
	return raw[i:j]
}

// splitQName splits qualified name q into its prefix and local parts.
func splitQName(q string) (prefix, local string) {
	if before, after, ok := strings.Cut(q, ":"); ok {
		return before, after
	}
	return "", q
}

// markDirty records that n changed, and with it every ancestor.
func (n *Node) markDirty() {
	for p := n; p != nil && !p.has(fDirty); p = p.parent {
		p.flags |= fDirty
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
	for c := d.node.first; c != nil; c = c.next {
		if c.kind == ElementNode {
			return c
		}
	}
	return nil
}

// Modified reports whether anything in the document changed.
func (d *Document) Modified() bool { return d.node.has(fDirty) }

// Original returns the bytes the document was parsed from, a copy that
// is the caller's.
func (d *Document) Original() []byte { return []byte(d.node.source()) }

// OriginalString returns the text the document was parsed from, without
// a copy.
func (d *Document) OriginalString() string { return d.node.source() }
