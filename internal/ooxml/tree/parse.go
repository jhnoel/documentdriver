package tree

import (
	"fmt"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/jhnoel/documentdriver/internal/ooxml/xmlscan"
)

// SyntaxError reports XML that is not well formed.
type SyntaxError struct {
	Offset int // byte offset into the input
	Line   int // 1-based line number
	Msg    string
}

// Error returns the syntax error formatted with its line and offset.
func (e *SyntaxError) Error() string {
	return fmt.Sprintf("xml: line %d (offset %d): %s", e.Line, e.Offset, e.Msg)
}

const bom = "\xEF\xBB\xBF"

// MaxDepth is the deepest element nesting Parse and ParseFragment accept;
// the document element is at depth 1. Deeper input is a SyntaxError. The
// deepest part in the corpus (corpus/repos and corpus/web, about 53,000
// XML parts, measured 2026-11-13) nests 25 elements, so 512 leaves a wide
// margin, also for Word's nested tables. It bounds every recursive walk
// over a parsed tree.
const MaxDepth = 512

// Parse tokenises src into a lossless tree. It keeps a copy of src
// (ParseString keeps the string it is given). Parse enforces well-formedness,
// including namespace well-formedness; it does not read a DTD or expand
// entities other than the predefined ones.
func Parse(src []byte) (*Document, error) {
	if len(src) > maxSource {
		return nil, &SyntaxError{Offset: 0, Line: 1, Msg: "document larger than 2 GiB"}
	}
	return ParseString(string(src))
}

// ParseString is Parse of the text src, which the tree keeps without a
// copy: names, attribute values and text the tree returns are substrings
// of it where they need no decoding.
func ParseString(src string) (*Document, error) {
	if len(src) > maxSource {
		return nil, &SyntaxError{Offset: 0, Line: 1, Msg: "document larger than 2 GiB"}
	}
	p := newParser(src)
	doc := &Node{kind: DocumentNode, src: p.srcp, start: 0, end: off(len(src))}
	if err := p.run(doc, false); err != nil {
		return nil, err
	}
	if err := checkNamespaces(doc, nil, false); err != nil {
		return nil, err
	}
	return &Document{node: doc}, nil
}

// Check reports whether b is a well-formed XML document, tokenising it as
// Parse does but without keeping a tree: it holds only the open elements,
// so checking a part costs memory in its depth, not its size. When b has
// several faults, the one reported may differ from Parse's (namespace
// faults are found as their element is read rather than after the whole
// document).
func Check(b []byte) error {
	if len(b) > maxSource {
		return &SyntaxError{Offset: 0, Line: 1, Msg: "document larger than 2 GiB"}
	}
	p := newParser(string(b))
	p.check = true
	doc := &Node{kind: DocumentNode, src: p.srcp, start: 0, end: off(len(b))}
	return p.run(doc, false)
}

// Visitor receives the nodes Scan reads, in document order. A node it is
// given is not attached to a tree (Parent and Children are nil) and is
// valid as long as the source is; an error stops the scan and is
// returned.
type Visitor interface {
	// Start is called for an element once its start tag is read, End
	// when its end tag is (right after Start for a self-closing one).
	Start(n *Node) error
	End(n *Node) error
	// Leaf is called for every other node: text, CDATA, comment,
	// processing instruction and doctype, the prolog's included.
	Leaf(n *Node) error
}

// Scan tokenises src as Check does and reports each node to v, without
// keeping a tree: memory is in the depth, not the size, so a caller can
// build a view of a large part without holding its tree too. It accepts
// what Check accepts; the fault reported for a document with several
// may differ from Parse's.
func Scan(src []byte, v Visitor) error {
	if len(src) > maxSource {
		return &SyntaxError{Offset: 0, Line: 1, Msg: "document larger than 2 GiB"}
	}
	return ScanString(string(src), v)
}

// ScanString is Scan of the text src, without a copy.
func ScanString(src string, v Visitor) error {
	if len(src) > maxSource {
		return &SyntaxError{Offset: 0, Line: 1, Msg: "document larger than 2 GiB"}
	}
	p := newParser(src)
	p.check, p.visit = true, v
	doc := &Node{kind: DocumentNode, src: p.srcp, start: 0, end: off(len(src))}
	return p.run(doc, false)
}

// ParseFragment parses a sequence of elements and character data, such as
// `<a:off x="0" y="0"/>`, into detached nodes ready to be inserted. The
// fragment may use prefixes it does not declare; ns maps such a prefix ("" for
// the default namespace) to the namespace URI it means. On insertion the
// prefix is resolved against the declarations in scope at the insertion
// point: an in-scope prefix bound to the same URI is reused, and otherwise a
// declaration is added on the inserted element. A prefix that is neither in
// ns nor declared in scope at the insertion point is an error. The nodes
// keep ns, not a copy, until they are inserted: the caller must not
// modify it meanwhile.
func ParseFragment(b []byte, ns map[string]string) ([]*Node, error) {
	if len(b) > maxSource {
		return nil, &SyntaxError{Offset: 0, Line: 1, Msg: "fragment larger than 2 GiB"}
	}
	src := string(b)
	p := newParser(src)
	holder := &Node{kind: DocumentNode, src: p.srcp, start: 0, end: off(len(src))}
	if err := p.run(holder, true); err != nil {
		return nil, err
	}
	if err := checkNamespaces(holder, nil, true); err != nil {
		return nil, err
	}
	out := holder.Children()
	for _, c := range out {
		holder.unlink(c)
		if c.kind == ElementNode && len(ns) > 0 {
			c.ext().nsHints = ns
		}
	}
	return out, nil
}

// maxSource is the largest input Parse takes: node offsets are int32.
const maxSource = 1<<31 - 1

// off is an offset into a source no larger than maxSource, which every
// entry point checks first.
func off(i int) int32 {
	return int32(i) //nolint:gosec // G115: sources are at most maxSource bytes (Parse, Check, ParseFragment)
}

type parser struct {
	src   string
	srcp  *source // shared by the nodes
	pos   int
	stack []*Node
	// slab holds the nodes of the tree being built (not when the parser
	// only checks: those nodes are the visitor's).
	slab slab
	// tagAttrs are the attribute names of the start tag being read, to
	// find duplicates, and tagNS its namespace declarations.
	tagAttrs []string
	tagNS    []nsDecl
	// check builds no tree (Check): elements are checked for namespace
	// well-formedness as they are read, in scopes, the stack's
	// namespace scopes, and nothing is attached to its parent.
	check  bool
	scopes []*scope
	// visit, when the parser only checks, is given every node (Scan).
	visit Visitor
}

// newParser returns a parser for source src ready to run.
func newParser(src string) *parser {
	return &parser{src: src, srcp: &source{s: src}, slab: newSlab(src)}
}

// add attaches leaf c to parent, unless the parser only checks; then it
// gives c to the visitor, if any.
func (p *parser) add(parent, c *Node) error {
	if !p.check {
		appendChild(parent, c)
		return nil
	}
	if p.visit != nil {
		return p.visit.Leaf(c)
	}
	return nil
}

// alloc returns a new zeroed node: from the slab when building a tree,
// alone when only checking.
func (p *parser) alloc() *Node {
	if p.check {
		return &Node{}
	}
	return p.slab.newNode()
}

// node builds a parsed non-element node spanning [start, end).
func (p *parser) node(kind Kind, start, end int) *Node {
	n := p.alloc()
	n.kind, n.src, n.start, n.end = kind, p.srcp, off(start), off(end)
	return n
}

// errorf returns a SyntaxError at byte offset off with the formatted message.
func (p *parser) errorf(off int, format string, args ...any) error {
	line := 1 + strings.Count(p.src[:min(off, len(p.src))], "\n")
	return &SyntaxError{Offset: off, Line: line, Msg: fmt.Sprintf(format, args...)}
}

// top returns the innermost open element, or doc when none is open.
func (p *parser) top(doc *Node) *Node {
	if len(p.stack) == 0 {
		return doc
	}
	return p.stack[len(p.stack)-1]
}

// appendChild attaches c as the last child of parent.
func appendChild(parent, c *Node) {
	parent.link(c, nil)
}

// run tokenises the whole source building the tree or checking it, returning any syntax error.
func (p *parser) run(doc *Node, fragment bool) error {
	if off := checkChars(p.src); off >= 0 {
		// A leading BOM is a legal character, so checkChars accepts it.
		return p.errorf(off, "invalid character or UTF-8 encoding")
	}
	sawRoot, sawDoctype := false, false
	for p.pos < len(p.src) {
		start := p.pos
		parent := p.top(doc)
		atTop := len(p.stack) == 0
		if p.src[p.pos] != '<' {
			end := strings.IndexByte(p.src[p.pos:], '<')
			if end < 0 {
				end = len(p.src)
			} else {
				end += p.pos
			}
			text := p.src[start:end]
			if atTop && !fragment {
				t := text
				if start == 0 {
					t = strings.TrimPrefix(t, bom)
				}
				if len(strings.TrimLeft(t, " \t\r\n")) != 0 {
					return p.errorf(start, "character data outside the document element")
				}
			} else if err := p.checkCharData(start, text); err != nil {
				return err
			}
			if err := p.add(parent, p.node(TextNode, start, end)); err != nil {
				return err
			}
			p.pos = end
			continue
		}
		rest := p.src[p.pos:]
		switch {
		case strings.HasPrefix(rest, "<?"):
			end := strings.Index(rest[2:], "?>")
			if end < 0 {
				return p.errorf(start, "unterminated processing instruction")
			}
			end += start + 4
			n := p.node(PINode, start, end)
			target := piTarget(p.src[start:end])
			if !xmlscan.ValidName(target) || strings.IndexByte(target, ':') >= 0 {
				return p.errorf(start, "invalid processing instruction target %q", target)
			}
			body := p.src[start+2+len(target) : end-2]
			if len(body) > 0 && !xmlscan.IsSpace(body[0]) {
				return p.errorf(start, "processing instruction target %q not followed by space", target)
			}
			if strings.EqualFold(target, "xml") {
				if target != "xml" || fragment || !(start == 0 || start == len(bom) && strings.HasPrefix(p.src, bom)) {
					return p.errorf(start, "XML declaration not at start of document")
				}
				if err := p.checkDecl(start, body); err != nil {
					return err
				}
			}
			if err := p.add(parent, n); err != nil {
				return err
			}
			p.pos = end
		case strings.HasPrefix(rest, "<!--"):
			end := strings.Index(rest[4:], "--")
			if end < 0 {
				return p.errorf(start, "unterminated comment")
			}
			end += start + 4
			if end+2 >= len(p.src) || p.src[end+2] != '>' {
				return p.errorf(end, `"--" inside comment`)
			}
			end += 3
			if err := p.add(parent, p.node(CommentNode, start, end)); err != nil {
				return err
			}
			p.pos = end
		case strings.HasPrefix(rest, "<![CDATA["):
			if atTop {
				return p.errorf(start, "CDATA section outside the document element")
			}
			end := strings.Index(rest[9:], "]]>")
			if end < 0 {
				return p.errorf(start, "unterminated CDATA section")
			}
			end += start + 12
			if err := p.add(parent, p.node(CDataNode, start, end)); err != nil {
				return err
			}
			p.pos = end
		case strings.HasPrefix(rest, "<!DOCTYPE"):
			if !atTop || fragment || sawRoot || sawDoctype {
				return p.errorf(start, "misplaced DOCTYPE")
			}
			end, err := p.scanDoctype(start)
			if err != nil {
				return err
			}
			sawDoctype = true
			if err := p.add(parent, p.node(DoctypeNode, start, end)); err != nil {
				return err
			}
			p.pos = end
		case strings.HasPrefix(rest, "</"):
			if err := p.endTag(start); err != nil {
				return err
			}
		case strings.HasPrefix(rest, "<!"):
			return p.errorf(start, "unsupported markup declaration")
		default:
			if atTop && !fragment && sawRoot {
				return p.errorf(start, "more than one document element")
			}
			if err := p.startTag(parent); err != nil {
				return err
			}
			if atTop {
				sawRoot = true
			}
		}
	}
	if len(p.stack) > 0 {
		e := p.stack[len(p.stack)-1]
		return p.errorf(int(e.start), "element <%s> not closed", e.qname())
	}
	if !fragment && !sawRoot {
		return p.errorf(len(p.src), "no document element")
	}
	return nil
}

// checkCharData validates references and the "]]>" ban in text.
func (p *parser) checkCharData(off int, b string) error {
	if i := strings.Index(b, "]]>"); i >= 0 {
		return p.errorf(off+i, `"]]>" in character data`)
	}
	return p.checkRefs(off, b)
}

// checkRefs returns a SyntaxError if text b contains an invalid reference.
func (p *parser) checkRefs(off int, b string) error {
	for i := 0; i < len(b); i++ {
		if b[i] != '&' {
			continue
		}
		n, _, ok := refAt(b[i:])
		if !ok {
			return p.errorf(off+i, "invalid or undefined reference")
		}
		i += n - 1
	}
	return nil
}

// checkDecl checks the pseudo-attributes of an XML declaration.
func (p *parser) checkDecl(off int, s string) error {
	names := []string{}
	for {
		s = strings.TrimLeft(s, " \t\r\n")
		if s == "" {
			break
		}
		eq := strings.IndexByte(s, '=')
		if eq < 0 {
			return p.errorf(off, "malformed XML declaration")
		}
		name := strings.TrimRight(s[:eq], " \t\r\n")
		s = strings.TrimLeft(s[eq+1:], " \t\r\n")
		if s == "" || (s[0] != '"' && s[0] != '\'') {
			return p.errorf(off, "malformed XML declaration")
		}
		q := strings.IndexByte(s[1:], s[0])
		if q < 0 {
			return p.errorf(off, "malformed XML declaration")
		}
		val := s[1 : q+1]
		s = s[q+2:]
		names = append(names, name)
		switch name {
		case "version":
			if !strings.HasPrefix(val, "1.") {
				return p.errorf(off, "unsupported XML version %q", val)
			}
		case "encoding":
			if !strings.EqualFold(val, "UTF-8") && !strings.EqualFold(val, "UTF8") {
				return p.errorf(off, "unsupported encoding %q (only UTF-8 is read)", val)
			}
		case "standalone":
			if val != "yes" && val != "no" {
				return p.errorf(off, "invalid standalone value %q", val)
			}
		default:
			return p.errorf(off, "unknown XML declaration attribute %q", name)
		}
	}
	if len(names) == 0 || names[0] != "version" {
		return p.errorf(off, "XML declaration without version")
	}
	return nil
}

// scanDoctype returns the end of a DOCTYPE declaration starting at off,
// skipping quoted strings and an internal subset.
func (p *parser) scanDoctype(off int) (int, error) {
	depth := 0
	var quote byte
	for i := off + 9; i < len(p.src); i++ {
		c := p.src[i]
		switch {
		case quote != 0:
			if c == quote {
				quote = 0
			}
		case c == '"' || c == '\'':
			quote = c
		case depth > 0 && strings.HasPrefix(p.src[i:], "<!--"):
			end := strings.Index(p.src[i+4:], "-->")
			if end < 0 {
				return 0, p.errorf(i, "unterminated comment in DOCTYPE")
			}
			i += 4 + end + 2
		case c == '[':
			depth++
		case c == ']':
			depth--
		case c == '>' && depth == 0:
			return i + 1, nil
		}
	}
	return 0, p.errorf(off, "unterminated DOCTYPE")
}

// readName reads a Name at p.pos.
func (p *parser) readName() (string, error) {
	start := p.pos
	for p.pos < len(p.src) {
		c := p.src[p.pos]
		if c < utf8.RuneSelf {
			if xmlscan.IsSpace(c) || c == '>' || c == '/' || c == '=' || c == '<' || c == '"' || c == '\'' || c == '?' {
				break
			}
			p.pos++
			continue
		}
		_, size := utf8.DecodeRuneInString(p.src[p.pos:])
		p.pos += size
	}
	name := p.src[start:p.pos]
	if !xmlscan.ValidQName(name) {
		return "", p.errorf(start, "invalid name %q", name)
	}
	return name, nil
}

// skipSpace advances the parser position past whitespace.
func (p *parser) skipSpace() {
	for p.pos < len(p.src) && xmlscan.IsSpace(p.src[p.pos]) {
		p.pos++
	}
}

// startTag parses the start tag at the parser position and returns any error.
func (p *parser) startTag(parent *Node) error {
	start := p.pos
	p.pos++ // '<'
	name, err := p.readName()
	if err != nil {
		return err
	}
	if len(p.stack) >= MaxDepth {
		return p.errorf(start, "elements nested more than %d deep", MaxDepth)
	}
	n := p.alloc()
	n.kind, n.src, n.start = ElementNode, p.srcp, off(start)
	if len(name) < 1<<16 {
		n.nameLen = uint16(len(name)) //nolint:gosec // G115: checked just above
		n.setLocal(name)
	} else {
		n.setName(name)
	}
	p.tagAttrs, p.tagNS = p.tagAttrs[:0], p.tagNS[:0]
	for {
		wsStart := p.pos
		p.skipSpace()
		if p.pos >= len(p.src) {
			return p.errorf(start, "unterminated start tag <%s>", name)
		}
		if p.src[p.pos] == '>' {
			p.pos++
			break
		}
		if p.src[p.pos] == '/' {
			if p.pos+1 >= len(p.src) || p.src[p.pos+1] != '>' {
				return p.errorf(p.pos, "expected /> in <%s>", name)
			}
			n.flags |= fSelfClose
			p.pos += 2
			break
		}
		if wsStart == p.pos {
			return p.errorf(p.pos, "missing whitespace before attribute in <%s>", name)
		}
		nameStart := p.pos
		aname, err := p.readName()
		if err != nil {
			return err
		}
		p.skipSpace()
		if p.pos >= len(p.src) || p.src[p.pos] != '=' {
			return p.errorf(p.pos, "expected = after attribute %s", aname)
		}
		p.pos++
		p.skipSpace()
		if p.pos >= len(p.src) || (p.src[p.pos] != '"' && p.src[p.pos] != '\'') {
			return p.errorf(p.pos, "expected quoted value for attribute %s", aname)
		}
		quote := p.src[p.pos]
		p.pos++
		vEnd := strings.IndexByte(p.src[p.pos:], quote)
		if vEnd < 0 {
			return p.errorf(p.pos, "unterminated value for attribute %s", aname)
		}
		val := p.src[p.pos : p.pos+vEnd]
		if i := strings.IndexByte(val, '<'); i >= 0 {
			return p.errorf(p.pos+i, "< in value of attribute %s", aname)
		}
		if err := p.checkRefs(p.pos, val); err != nil {
			return err
		}
		if slices.Contains(p.tagAttrs, aname) {
			return p.errorf(nameStart, "duplicate attribute %s", aname)
		}
		p.tagAttrs = append(p.tagAttrs, aname)
		if prefix, ok := declOf(aname); ok {
			p.tagNS = append(p.tagNS, nsDecl{prefix, xmlscan.DecodeAttr(val)})
		}
		p.pos += vEnd + 1
	}
	n.setTagEnd(off(p.pos))
	if len(p.tagNS) > 0 {
		n.ext().ns = slices.Clone(p.tagNS)
		n.flags |= fDecls
	}
	if p.check {
		var sc *scope
		if k := len(p.scopes); k > 0 {
			sc = p.scopes[k-1]
		}
		inner, err := checkElement(n, sc, false)
		if err != nil {
			return err
		}
		if !n.has(fSelfClose) {
			p.scopes = append(p.scopes, inner)
		}
		if p.visit != nil {
			if err := p.visit.Start(n); err != nil {
				return err
			}
		}
	} else {
		appendChild(parent, n)
	}
	if n.has(fSelfClose) {
		n.end = off(p.pos)
		if p.visit != nil {
			return p.visit.End(n)
		}
	} else {
		p.stack = append(p.stack, n)
	}
	return nil
}

// endTag parses the end tag at start and returns an error if it mismatches.
func (p *parser) endTag(start int) error {
	p.pos += 2
	name, err := p.readName()
	if err != nil {
		return err
	}
	p.skipSpace()
	if p.pos >= len(p.src) || p.src[p.pos] != '>' {
		return p.errorf(p.pos, "expected > in end tag </%s>", name)
	}
	p.pos++
	if len(p.stack) == 0 {
		return p.errorf(start, "unexpected end tag </%s>", name)
	}
	n := p.stack[len(p.stack)-1]
	if n.qname() != name {
		return p.errorf(start, "end tag </%s> does not match <%s>", name, n.qname())
	}
	p.stack = p.stack[:len(p.stack)-1]
	if p.check {
		p.scopes = p.scopes[:len(p.scopes)-1]
	}
	n.end = off(p.pos)
	if p.visit != nil {
		return p.visit.End(n)
	}
	return nil
}
