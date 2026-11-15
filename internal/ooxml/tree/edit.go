package tree

import (
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/jhnoel/documentdriver/internal/ooxml/xmlscan"
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
	var out []Attribute
	n.eachAttr(func(_ int, a attr) bool {
		out = append(out, Attribute{Name: a.name, Value: xmlscan.DecodeAttr(a.raw)})
		return true
	})
	if out == nil {
		out = []Attribute{}
	}
	return out
}

// findAttr returns the attribute with qualified name qname and its index.
func (n *Node) findAttr(qname string) (attr, int) {
	var found attr
	at := -1
	n.eachAttr(func(i int, a attr) bool {
		if a.name == qname {
			found, at = a, i
			return false
		}
		return true
	})
	return found, at
}

// findAttrNS returns the attribute with namespace URI uri and local name
// local, and its index.
func (n *Node) findAttrNS(uri, local string) (attr, int) {
	var found attr
	at := -1
	n.eachAttr(func(i int, a attr) bool {
		if _, ok := declOf(a.name); ok {
			return true
		}
		p, l := splitQName(a.name)
		if l != local {
			return true
		}
		if p == "" {
			if uri == "" {
				found, at = a, i
				return false
			}
			return true
		}
		if got, _ := n.LookupPrefix(p); got == uri {
			found, at = a, i
			return false
		}
		return true
	})
	return found, at
}

// Attr returns the decoded value of the attribute with qualified name qname
// as written (for example "r:id").
func (n *Node) Attr(qname string) (string, bool) {
	if a, i := n.findAttr(qname); i >= 0 {
		return xmlscan.DecodeAttr(a.raw), true
	}
	return "", false
}

// AttrNS returns the decoded value of the attribute with namespace URI uri
// ("" for an unprefixed attribute) and local name local.
func (n *Node) AttrNS(uri, local string) (string, bool) {
	if a, i := n.findAttrNS(uri, local); i >= 0 {
		return xmlscan.DecodeAttr(a.raw), true
	}
	return "", false
}

// AttrRaw returns an attribute's value exactly as written between its
// quotes, references undecoded. The slice is the caller's.
func (n *Node) AttrRaw(qname string) ([]byte, bool) {
	if a, i := n.findAttr(qname); i >= 0 {
		return []byte(a.raw), true
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
	if !xmlscan.ValidQName(qname) {
		return fmt.Errorf("tree: invalid attribute name %q", qname)
	}
	if off := checkChars(value); off >= 0 {
		return fmt.Errorf("tree: attribute %s: value has a character XML cannot represent at byte %d", qname, off)
	}
	if a, i := n.findAttr(qname); i >= 0 {
		if xmlscan.DecodeAttr(a.raw) == value {
			return nil
		}
		as := n.ownAttrList()
		as[i].raw = xmlscan.EscapeAttr(value, a.quote)
		n.attrsChanged()
		return nil
	}
	if p, _ := splitQName(qname); p != "" {
		if _, isDecl := declOf(qname); !isDecl {
			if _, ok := n.LookupPrefix(p); !ok && n.hints()[p] == "" {
				return fmt.Errorf("tree: attribute %s on <%s>: %w %q", qname, n.qname(), ErrUndeclaredPrefix, p)
			}
		}
	}
	q := preferredQuote(n)
	as := n.ownAttrList()
	n.x.attrs = append(as, builtAttr(qname, q, xmlscan.EscapeAttr(value, q)))
	n.attrsChanged()
	return nil
}

// SetAttrNS sets the attribute with namespace URI uri and local name local.
// It reuses the existing attribute if there is one, else a prefix already
// bound to uri in scope; failing both it declares prefix on n itself.
func (n *Node) SetAttrNS(uri, prefix, local, value string) error {
	if n.kind != ElementNode {
		return ErrNotElement
	}
	if a, i := n.findAttrNS(uri, local); i >= 0 {
		return n.SetAttr(a.name, value)
	}
	if uri == "" {
		return n.SetAttr(local, value)
	}
	if p, ok := n.LookupNamespace(uri); ok && p != "" {
		return n.SetAttr(p+":"+local, value)
	}
	if prefix == "" {
		return fmt.Errorf("tree: no prefix for namespace %q on <%s>", uri, n.qname())
	}
	if got, ok := n.LookupPrefix(prefix); ok && got != uri && hasAttr(n, "xmlns:"+prefix) {
		return fmt.Errorf("tree: prefix %q already bound to %q on <%s>", prefix, got, n.qname())
	}
	if err := n.SetAttr("xmlns:"+prefix, uri); err != nil {
		return err
	}
	return n.SetAttr(prefix+":"+local, value)
}

// RemoveAttr removes the attribute qname together with its leading
// whitespace. It reports whether the attribute existed.
func (n *Node) RemoveAttr(qname string) bool {
	_, i := n.findAttr(qname)
	if i < 0 {
		return false
	}
	as := n.ownAttrList()
	n.x.attrs = slices.Delete(as, i, i+1)
	n.attrsChanged()
	return true
}

// hasAttr reports whether node n has an attribute with qualified name qname.
func hasAttr(n *Node, qname string) bool {
	_, i := n.findAttr(qname)
	return i >= 0
}

// preferredQuote returns the quote character of n's first attribute, or the
// nearest ancestor's, defaulting to '"'.
func preferredQuote(n *Node) byte {
	for e := n; e != nil; e = e.parent {
		if a, ok := e.firstAttr(); ok {
			return a.quote
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
	if !xmlscan.ValidQName(name) {
		return nil, fmt.Errorf("tree: invalid element name %q", name)
	}
	n := &Node{
		kind:  ElementNode,
		flags: fSelfClose | fTagDirty | fDirty | fOwnAttrs,
		x:     &extra{nsHints: map[string]string{prefix: uri}},
	}
	n.setName(name)
	return n, nil
}

// NewText builds a detached text node holding s, escaped. An empty s
// gives an empty text node (its raw bytes are empty, not missing).
func NewText(s string) *Node {
	return &Node{kind: TextNode, x: &extra{raw: xmlscan.EscapeText(s), hasRaw: true}, flags: fDirty}
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
	x := nodes[0].ext()
	x.raw, x.hasRaw = string(raw), true
	nodes[0].src = nil
	nodes[0].flags |= fDirty
	return nodes[0], nil
}

// Text returns the decoded character data of n's text and CDATA children.
// When one child holds it all and has no references or CRs, it is a
// substring of the source: nothing is copied.
func (n *Node) Text() string {
	var only *Node
	count := 0
	for c := n.first; c != nil; c = c.next {
		if c.kind == TextNode || c.kind == CDataNode {
			only = c
			count++
		}
	}
	switch count {
	case 0:
		return ""
	case 1:
		return only.Data()
	}
	var b strings.Builder
	for c := n.first; c != nil; c = c.next {
		if c.kind == TextNode || c.kind == CDataNode {
			b.WriteString(c.Data())
		}
	}
	return b.String()
}

// Data returns the decoded character data of a text or CDATA node, and ""
// for any other kind of node. Text without references or CRs is not
// copied.
func (n *Node) Data() string {
	switch n.kind {
	case TextNode:
		return xmlscan.DecodeText(n.RawString())
	case CDataNode:
		r := n.RawString()
		return r[9 : len(r)-3]
	}
	return ""
}

// SetText replaces all children of element n with a single text node
// holding s. If n already holds exactly that text, nothing changes.
func (n *Node) SetText(s string) error {
	if n.kind != ElementNode {
		return ErrNotElement
	}
	if off := checkChars(s); off >= 0 {
		return fmt.Errorf("tree: text has a character XML cannot represent at byte %d", off)
	}
	if n.Text() == s && !n.HasElements() {
		return nil
	}
	for c := n.first; c != nil; {
		next := c.next
		c.parent, c.prev, c.next = nil, nil, nil
		c = next
	}
	n.first = nil
	if s != "" {
		n.link(NewText(s), nil)
	}
	n.markDirty()
	return nil
}

// isChild reports whether c is a child of n.
func (n *Node) isChild(c *Node) bool { return c != nil && c.parent == n }

// canHold returns an error unless detached node c may be inserted as a child of n.
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

// insertBefore inserts the detached node c before ref, a child of n (at
// the end when ref is nil), resolving c's namespace hints.
func (n *Node) insertBefore(c, ref *Node) error {
	if err := n.canHold(c); err != nil {
		return err
	}
	if err := adopt(n, c); err != nil {
		return err
	}
	n.link(c, ref)
	n.markDirty()
	return nil
}

// refAt returns the child before which an insert at index i goes (nil at
// the end), and false when i is out of range. It is O(i).
func (n *Node) refAt(i int) (*Node, bool) {
	if i < 0 {
		return nil, false
	}
	c := n.first
	for ; c != nil && i > 0; i-- {
		c = c.next
	}
	return c, i == 0
}

// InsertAt inserts the detached node c as the i-th child of n (counting all
// child nodes, text included). Namespace hints on c are resolved against
// the declarations in scope at n. It walks to the i-th child: O(i).
func (n *Node) InsertAt(i int, c *Node) error {
	if err := n.canHold(c); err != nil {
		return err
	}
	ref, ok := n.refAt(i)
	if !ok {
		return fmt.Errorf("tree: index %d out of range", i)
	}
	return n.insertBefore(c, ref)
}

// AppendChild inserts c as the last child of n.
func (n *Node) AppendChild(c *Node) error { return n.insertBefore(c, nil) }

// InsertBefore inserts c immediately before ref, a child of n. A nil ref
// appends.
func (n *Node) InsertBefore(c, ref *Node) error {
	if ref == nil {
		return n.AppendChild(c)
	}
	if !n.isChild(ref) {
		return errors.New("tree: reference node is not a child")
	}
	return n.insertBefore(c, ref)
}

// InsertAfter inserts c immediately after ref, a child of n.
func (n *Node) InsertAfter(c, ref *Node) error {
	if !n.isChild(ref) {
		return errors.New("tree: reference node is not a child")
	}
	return n.insertBefore(c, ref.next)
}

// RemoveChild detaches c from n. Surrounding whitespace text is left as it
// is.
func (n *Node) RemoveChild(c *Node) error {
	if !n.isChild(c) {
		return errors.New("tree: node is not a child")
	}
	n.unlink(c)
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
	if !n.isChild(old) {
		return errors.New("tree: node is not a child")
	}
	if err := n.insertBefore(c, old); err != nil {
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
	ref, ok := n.refAt(i)
	if !ok {
		return nil, fmt.Errorf("tree: index %d out of range", i)
	}
	return n.insertNodes(nodes, ref)
}

// insertNodes inserts nodes, detached and fresh from a parse, before ref
// (at the end when nil): all of them or, when one would not adopt, none.
func (n *Node) insertNodes(nodes []*Node, ref *Node) ([]*Node, error) {
	for _, c := range nodes {
		if err := adoptCheck(n, c); err != nil {
			return nil, err
		}
	}
	for _, c := range nodes {
		if err := n.insertBefore(c, ref); err != nil {
			return nil, err // unreachable after adoptCheck
		}
	}
	return nodes, nil
}

// ReplaceWithFragment replaces child old of n with the nodes parsed from
// frag and returns them.
func (n *Node) ReplaceWithFragment(old *Node, frag []byte, ns map[string]string) ([]*Node, error) {
	if !n.isChild(old) {
		return nil, errors.New("tree: node is not a child")
	}
	nodes, err := ParseFragment(frag, ns)
	if err != nil {
		return nil, err
	}
	if n.kind != ElementNode {
		return nil, ErrNotElement
	}
	if _, err := n.insertNodes(nodes, old); err != nil {
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
			return fmt.Errorf("tree: inserting <%s>: %w %q", c.qname(), ErrUndeclaredPrefix, p)
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
	prefix, cur := splitQName(n.qname())
	if local == cur {
		return nil
	}
	name := local
	if prefix != "" {
		name = prefix + ":" + local
	}
	if !xmlscan.ValidQName(name) || strings.Contains(local, ":") {
		return fmt.Errorf("tree: invalid element name %q", name)
	}
	n.setName(name)
	n.flags |= fEndLost
	n.flags |= fTagDirty
	n.markDirty()
	return nil
}
