package treeoracle

import (
	"bytes"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"
)

// ErrNotElement is returned when an element operation is applied to another
// kind of node.
var ErrNotElement = errors.New("tree: not an element")

// Attribute is an attribute's qualified name and decoded value.
type Attribute struct {
	Name  string
	Value string
}

// Attrs returns the attributes of an element in document order, namespace
// declarations included, with decoded values.
func (n *Node) Attrs() []Attribute {
	out := make([]Attribute, len(n.attrs))
	for i, a := range n.attrs {
		out[i] = Attribute{Name: a.name, Value: decodeAttr(a.raw)}
	}
	return out
}

func (n *Node) findAttr(qname string) int {
	for i, a := range n.attrs {
		if a.name == qname {
			return i
		}
	}
	return -1
}

func (n *Node) findAttrNS(uri, local string) int {
	for i, a := range n.attrs {
		if _, ok := declOf(a.name); ok {
			continue
		}
		p, l := splitQName(a.name)
		if l != local {
			continue
		}
		if p == "" {
			if uri == "" {
				return i
			}
			continue
		}
		if got, _ := n.LookupPrefix(p); got == uri {
			return i
		}
	}
	return -1
}

// Attr returns the decoded value of the attribute with qualified name qname
// as written (for example "r:id").
func (n *Node) Attr(qname string) (string, bool) {
	if i := n.findAttr(qname); i >= 0 {
		return decodeAttr(n.attrs[i].raw), true
	}
	return "", false
}

// AttrNS returns the decoded value of the attribute with namespace URI uri
// ("" for an unprefixed attribute) and local name local.
func (n *Node) AttrNS(uri, local string) (string, bool) {
	if i := n.findAttrNS(uri, local); i >= 0 {
		return decodeAttr(n.attrs[i].raw), true
	}
	return "", false
}

// AttrRaw returns an attribute's value exactly as written between its
// quotes, references undecoded.
func (n *Node) AttrRaw(qname string) ([]byte, bool) {
	if i := n.findAttr(qname); i >= 0 {
		return n.attrs[i].raw, true
	}
	return nil, false
}

// SetAttr sets the attribute qname to value. An existing attribute keeps
// its position, leading whitespace and quote character; only its value is
// re-escaped. A new attribute is appended after the last one, preceded by a
// single space and quoted like the element's existing attributes. Setting a
// value equal to the current decoded value changes nothing. A prefixed name
// must use a prefix declared in scope.
func (n *Node) SetAttr(qname, value string) error {
	if n.kind != ElementNode {
		return ErrNotElement
	}
	if !validQName(qname) {
		return fmt.Errorf("tree: invalid attribute name %q", qname)
	}
	if off := checkChars([]byte(value)); off >= 0 {
		return fmt.Errorf("tree: attribute %s: value has a character XML cannot represent at byte %d", qname, off)
	}
	if i := n.findAttr(qname); i >= 0 {
		a := n.attrs[i]
		if decodeAttr(a.raw) == value {
			return nil
		}
		a.raw = escapeAttr(value, a.quote)
		n.tagDirty = true
		n.markDirty()
		return nil
	}
	if p, _ := splitQName(qname); p != "" {
		if _, isDecl := declOf(qname); !isDecl {
			if _, ok := n.LookupPrefix(p); !ok && n.hints()[p] == "" {
				return fmt.Errorf("tree: attribute %s on <%s>: %w %q", qname, n.name, ErrUndeclaredPrefix, p)
			}
		}
	}
	q := preferredQuote(n)
	n.attrs = append(n.attrs, builtAttr(qname, q, escapeAttr(value, q)))
	n.tagDirty = true
	n.markDirty()
	return nil
}

// SetAttrNS sets the attribute with namespace URI uri and local name local.
// It reuses the existing attribute if there is one, else a prefix already
// bound to uri in scope; failing both it declares prefix on n itself.
func (n *Node) SetAttrNS(uri, prefix, local, value string) error {
	if n.kind != ElementNode {
		return ErrNotElement
	}
	if i := n.findAttrNS(uri, local); i >= 0 {
		return n.SetAttr(n.attrs[i].name, value)
	}
	if uri == "" {
		return n.SetAttr(local, value)
	}
	if p, ok := n.LookupNamespace(uri); ok && p != "" {
		return n.SetAttr(p+":"+local, value)
	}
	if prefix == "" {
		return fmt.Errorf("tree: no prefix for namespace %q on <%s>", uri, n.name)
	}
	if got, ok := n.LookupPrefix(prefix); ok && got != uri && n.findAttr("xmlns:"+prefix) >= 0 {
		return fmt.Errorf("tree: prefix %q already bound to %q on <%s>", prefix, got, n.name)
	}
	if err := n.SetAttr("xmlns:"+prefix, uri); err != nil {
		return err
	}
	return n.SetAttr(prefix+":"+local, value)
}

// RemoveAttr removes the attribute qname together with its leading
// whitespace. It reports whether the attribute existed.
func (n *Node) RemoveAttr(qname string) bool {
	i := n.findAttr(qname)
	if i < 0 {
		return false
	}
	n.attrs = slices.Delete(n.attrs, i, i+1)
	n.tagDirty = true
	n.markDirty()
	return true
}

// preferredQuote returns the quote character of n's first attribute, or the
// nearest ancestor's, defaulting to '"'.
func preferredQuote(n *Node) byte {
	for e := n; e != nil; e = e.parent {
		if e.kind == ElementNode && len(e.attrs) > 0 {
			return e.attrs[0].quote
		}
	}
	return '"'
}

// NewElement builds a detached, empty element. prefix is the prefix to use
// if no prefix for uri is in scope where it is inserted; when one is, the
// element takes that prefix instead. Pass uri "" for an element in no
// namespace.
func NewElement(uri, prefix, local string) (*Node, error) {
	name := local
	if prefix != "" {
		name = prefix + ":" + local
	}
	if !validQName(name) {
		return nil, fmt.Errorf("tree: invalid element name %q", name)
	}
	return &Node{
		kind:      ElementNode,
		name:      name,
		selfClose: true,
		tagDirty:  true,
		dirty:     true,
		endAt:     -1,
		x:         &extra{nsHints: map[string]string{prefix: uri}},
	}, nil
}

// NewText builds a detached text node holding s, escaped. An empty s
// gives an empty text node (its raw bytes are empty, not missing).
func NewText(s string) *Node {
	raw := EscapeText(s)
	if raw == nil {
		raw = []byte{}
	}
	return &Node{kind: TextNode, x: &extra{raw: raw}, dirty: true}
}

// NewRaw builds a detached comment, PI, CDATA or text node from its raw
// bytes (delimiters included), checking that they tokenise as one node of
// that kind.
func NewRaw(kind Kind, raw []byte) (*Node, error) {
	switch kind {
	case TextNode, CommentNode, PINode, CDataNode:
	default:
		return nil, fmt.Errorf("tree: NewRaw cannot build a %s node", kind)
	}
	nodes, err := ParseFragment(raw, nil)
	if err != nil {
		return nil, err
	}
	if len(nodes) != 1 || nodes[0].kind != kind {
		return nil, fmt.Errorf("tree: %q is not a single %s node", raw, kind)
	}
	nodes[0].ext().raw = bytes.Clone(raw)
	nodes[0].src = nil
	nodes[0].dirty = true
	return nodes[0], nil
}

// Text returns the decoded character data of n's text and CDATA children.
func (n *Node) Text() string {
	var b []byte
	for _, c := range n.children {
		switch c.kind {
		case TextNode:
			b = append(b, decodeText(c.Raw())...)
		case CDataNode:
			r := c.Raw()
			b = append(b, r[9:len(r)-3]...)
		}
	}
	return string(b)
}

// Data returns the decoded character data of a text or CDATA node, and ""
// for any other kind of node.
func (n *Node) Data() string {
	switch n.kind {
	case TextNode:
		return decodeText(n.Raw())
	case CDataNode:
		r := n.Raw()
		return string(r[9 : len(r)-3])
	}
	return ""
}

// SetText replaces all children of element n with a single text node
// holding s. If n already holds exactly that text, nothing changes.
func (n *Node) SetText(s string) error {
	if n.kind != ElementNode {
		return ErrNotElement
	}
	if off := checkChars([]byte(s)); off >= 0 {
		return fmt.Errorf("tree: text has a character XML cannot represent at byte %d", off)
	}
	if n.Text() == s && len(n.Elements()) == 0 {
		return nil
	}
	for _, c := range n.children {
		c.parent = nil
	}
	n.children = nil
	if s != "" {
		t := NewText(s)
		t.parent = n
		n.children = []*Node{t}
	}
	n.markDirty()
	return nil
}

// indexOf returns c's index among n's children, or -1. It searches from
// the end: edits mostly touch the last children (appends, the body's
// content before its final w:sectPr).
func (n *Node) indexOf(c *Node) int {
	for i := len(n.children) - 1; i >= 0; i-- {
		if n.children[i] == c {
			return i
		}
	}
	return -1
}

func (n *Node) canHold(c *Node) error {
	if n.kind != ElementNode {
		return ErrNotElement
	}
	if c.parent != nil {
		return errors.New("tree: node is already attached; remove it first")
	}
	if c.kind == DocumentNode || c.kind == DoctypeNode {
		return fmt.Errorf("tree: cannot insert a %s node", c.kind)
	}
	for p := n; p != nil; p = p.parent {
		if p == c {
			return errors.New("tree: cannot insert a node inside itself")
		}
	}
	return nil
}

// InsertAt inserts the detached node c as the i-th child of n (counting all
// child nodes, text included). Namespace hints on c are resolved against
// the declarations in scope at n.
func (n *Node) InsertAt(i int, c *Node) error {
	if err := n.canHold(c); err != nil {
		return err
	}
	if i < 0 || i > len(n.children) {
		return fmt.Errorf("tree: index %d out of range", i)
	}
	if err := adopt(n, c); err != nil {
		return err
	}
	c.parent = n
	n.children = slices.Insert(n.children, i, c)
	n.markDirty()
	return nil
}

// AppendChild inserts c as the last child of n.
func (n *Node) AppendChild(c *Node) error { return n.InsertAt(len(n.children), c) }

// InsertBefore inserts c immediately before ref, a child of n. A nil ref
// appends.
func (n *Node) InsertBefore(c, ref *Node) error {
	if ref == nil {
		return n.AppendChild(c)
	}
	i := n.indexOf(ref)
	if i < 0 {
		return errors.New("tree: reference node is not a child")
	}
	return n.InsertAt(i, c)
}

// InsertAfter inserts c immediately after ref, a child of n.
func (n *Node) InsertAfter(c, ref *Node) error {
	i := n.indexOf(ref)
	if i < 0 {
		return errors.New("tree: reference node is not a child")
	}
	return n.InsertAt(i+1, c)
}

// RemoveChild detaches c from n. Surrounding whitespace text is left as it
// is.
func (n *Node) RemoveChild(c *Node) error {
	i := n.indexOf(c)
	if i < 0 {
		return errors.New("tree: node is not a child")
	}
	n.children = slices.Delete(n.children, i, i+1)
	c.parent = nil
	n.markDirty()
	return nil
}

// Remove detaches n from its parent.
func (n *Node) Remove() error {
	if n.parent == nil {
		return errors.New("tree: node has no parent")
	}
	return n.parent.RemoveChild(n)
}

// ReplaceChild puts the detached node c where old is and detaches old.
func (n *Node) ReplaceChild(c, old *Node) error {
	i := n.indexOf(old)
	if i < 0 {
		return errors.New("tree: node is not a child")
	}
	if err := n.InsertAt(i, c); err != nil {
		return err
	}
	return n.RemoveChild(old)
}

// InsertFragment parses frag (see ParseFragment) and inserts its nodes as
// children of n starting at index i. It returns the inserted nodes. Either
// all nodes are inserted or none.
func (n *Node) InsertFragment(i int, frag []byte, ns map[string]string) ([]*Node, error) {
	nodes, err := ParseFragment(frag, ns)
	if err != nil {
		return nil, err
	}
	if n.kind != ElementNode {
		return nil, ErrNotElement
	}
	if i < 0 || i > len(n.children) {
		return nil, fmt.Errorf("tree: index %d out of range", i)
	}
	for _, c := range nodes {
		if err := adoptCheck(n, c); err != nil {
			return nil, err
		}
	}
	for k, c := range nodes {
		if err := n.InsertAt(i+k, c); err != nil {
			return nil, err // unreachable after adoptCheck
		}
	}
	return nodes, nil
}

// ReplaceWithFragment replaces child old of n with the nodes parsed from
// frag and returns them.
func (n *Node) ReplaceWithFragment(old *Node, frag []byte, ns map[string]string) ([]*Node, error) {
	i := n.indexOf(old)
	if i < 0 {
		return nil, errors.New("tree: node is not a child")
	}
	nodes, err := n.InsertFragment(i, frag, ns)
	if err != nil {
		return nil, err
	}
	return nodes, n.RemoveChild(old)
}

// adoptCheck reports whether adopt would fail, without changing c.
func adoptCheck(parent, c *Node) error {
	if c.kind != ElementNode {
		return nil
	}
	used := map[string]bool{}
	collectUndeclared(c, nil, used)
	for _, p := range slices.Sorted(maps.Keys(used)) {
		if _, ok := c.hints()[p]; ok || p == "" {
			continue
		}
		if _, ok := parent.LookupPrefix(p); !ok {
			return fmt.Errorf("tree: inserting <%s>: %w %q", c.name, ErrUndeclaredPrefix, p)
		}
	}
	return nil
}

// Rename changes element n's local name, keeping its prefix (and so its
// namespace), its attributes and its children: w:t becomes w:delText.
// Renaming to the current name changes nothing.
func (n *Node) Rename(local string) error {
	if n.kind != ElementNode {
		return ErrNotElement
	}
	prefix, cur := splitQName(n.name)
	if local == cur {
		return nil
	}
	name := local
	if prefix != "" {
		name = prefix + ":" + local
	}
	if !validQName(name) || strings.Contains(local, ":") {
		return fmt.Errorf("tree: invalid element name %q", name)
	}
	n.name = name
	n.endAt = -1
	n.tagDirty = true
	n.markDirty()
	return nil
}
