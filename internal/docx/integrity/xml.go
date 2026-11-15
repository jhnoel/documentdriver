package integrity

import (
	"errors"
	"fmt"
	"iter"
	"strings"
	"unicode"
	"unicode/utf16"
	"unicode/utf8"

	"github.com/jhnoel/documentdriver/internal/ooxml/tree"
	"github.com/jhnoel/documentdriver/internal/ooxml/xmlscan"
)

// The parts are read with the engine's tokenizer (internal/ooxml/tree) and
// resolved into the small namespace-aware view below. docxdriver read them
// with its own quick-xml parser; parse reproduces that parser's verdicts
// and its short error categories, which become "xml: ..." findings (see
// xmlerr.go for the mapping and the cases it cannot reproduce).

const xmlnsNS = "http://www.w3.org/2000/xmlns/"

// qn is a resolved name: namespace, local part and the prefix written.
// A view interns them, so its elements and attributes share a few dozen.
type qn struct {
	ns, local, prefix string
}

type attribute struct {
	*qn
	value string
}

type decl struct {
	prefix, uri string
}

// textRun is character data in an element (text and CDATA, references
// decoded, line ends normalised), owner the element's index. A
// document's runs are in document order, so a subtree's are the runs
// between its element's t0 and t1.
type textRun struct {
	s     string
	owner int32
}

// none is the index of no element.
const none = -1

// element is a namespace-resolved element of a document's view. The
// view is columnar: elements stand in blocks in document order and refer
// to each other, their attributes and their character data by index.
// Comments and processing instructions are not kept: no check reads
// them.
type element struct {
	*qn
	doc *document
	// index is the element's position in its document's elements in
	// document order: the identity findings are located by. end is one
	// past its last descendant's, so its subtree is [index, end).
	index, end int32
	// parent, first (child) and next (sibling) are element indices, or
	// none.
	parent, first, next int32
	// The attributes are doc.attrs[attr0 : attr0+nattr], the character
	// data of the subtree doc.texts[t0:t1].
	attr0, nattr, t0, t1 int32
	// decls reports namespace declarations on the element, in
	// doc.decls.
	decls bool
}

// document is a parsed part: its root element and every element in
// document order.
type document struct {
	root   *element
	blocks [][]element
	n      int32
	attrs  []attribute
	texts  []textRun
	decls  map[int32][]decl // "" is the default namespace
	// starts are the byte offsets in the part of the elements, by index,
	// when the part was parsed as it is (nil when parsing had to rewrite
	// it: transcoded, or a fault corrected).
	starts []int
}

// blockShift sizes the blocks elements are allocated in.
const blockShift = 10

// el returns the element at index i, or nil for none.
func (d *document) el(i int32) *element {
	if i < 0 {
		return nil
	}
	return &d.blocks[i>>blockShift][i&(1<<blockShift-1)]
}

// at returns the element at index i, or nil when there is none.
func (d *document) at(i int) *element {
	if i < 0 || i >= int(d.n) {
		return nil
	}
	return d.el(int32(i)) //nolint:gosec // G115: i < d.n, an int32
}

// size is the number of elements.
func (d *document) size() int { return int(d.n) }

// elements yields every element in document order.
func (d *document) elements() iter.Seq[*element] {
	return func(yield func(*element) bool) {
		for i := range d.n {
			if !yield(d.el(i)) {
				return
			}
		}
	}
}

// up is the parent element, or nil for the root.
func (e *element) up() *element { return e.doc.el(e.parent) }

// firstEl is the first element child, or nil.
func (e *element) firstEl() *element { return e.doc.el(e.first) }

// nextEl is the next element sibling, or nil.
func (e *element) nextEl() *element { return e.doc.el(e.next) }

// attrs returns the element's attributes, namespace declarations
// excepted, in document order.
func (e *element) attrs() []attribute { return e.doc.attrs[e.attr0 : e.attr0+e.nattr] }

// is reports whether e is the element ns:local.
func (e *element) is(ns, local string) bool { return e.local == local && e.ns == ns }

// attr returns e's attribute ns:local and whether it exists.
func (e *element) attr(ns, local string) (string, bool) {
	for _, a := range e.attrs() {
		if a.local == local && a.ns == ns {
			return a.value, true
		}
	}
	return "", false
}

// attrOr returns the attribute's value, or "" when it is absent.
func (e *element) attrOr(ns, local string) string {
	v, _ := e.attr(ns, local)
	return v
}

// firstChild returns e's first child element ns:local, or nil.
func (e *element) firstChild(ns, local string) *element {
	for c := e.firstEl(); c != nil; c = c.nextEl() {
		if c.is(ns, local) {
			return c
		}
	}
	return nil
}

// lookupPrefix returns the namespace prefix resolves to where e is in
// scope; an empty binding (xmlns="") counts as none.
func (e *element) lookupPrefix(prefix string) (string, bool) {
	if prefix == "xml" {
		return tree.XMLNamespace, true
	}
	for n := e; n != nil; n = n.up() {
		if !n.decls {
			continue
		}
		for _, d := range n.doc.decls[n.index] {
			if d.prefix == prefix {
				return d.uri, d.uri != ""
			}
		}
	}
	return "", false
}

// text returns the concatenated character data of e's subtree.
func (e *element) text() string {
	var b strings.Builder
	for _, t := range e.doc.texts[e.t0:e.t1] {
		b.WriteString(t.s)
	}
	return b.String()
}

// hasText reports whether e has character data that is not all white
// space directly in it.
func (e *element) hasText() bool {
	for _, t := range e.doc.texts[e.t0:e.t1] {
		if t.owner == e.index && strings.TrimSpace(t.s) != "" {
			return true
		}
	}
	return false
}

// decodeXML decodes a part to UTF-8 text. OPC requires UTF-8 or UTF-16,
// and the declaration must agree with the bytes. The error is the
// category docxdriver reports. UTF-8 is returned as it is, not copied.
func decodeXML(b string) (string, string) {
	var text string
	var detected string
	switch {
	case len(b) >= 3 && b[0] == 0xEF && b[1] == 0xBB && b[2] == 0xBF:
		if !utf8.ValidString(b[3:]) {
			return "", "invalid UTF-8"
		}
		text, detected = b[3:], "utf-8"
	case len(b) >= 2 && (b[0] == 0xFF && b[1] == 0xFE || b[0] == 0xFE && b[1] == 0xFF):
		little := b[0] == 0xFF
		body := b[2:]
		if len(body)%2 != 0 {
			return "", "invalid UTF-16"
		}
		units := make([]uint16, len(body)/2)
		for i := range units {
			if little {
				units[i] = uint16(body[2*i]) | uint16(body[2*i+1])<<8
			} else {
				units[i] = uint16(body[2*i])<<8 | uint16(body[2*i+1])
			}
		}
		s, ok := decodeUTF16(units)
		if !ok {
			return "", "invalid UTF-16"
		}
		text, detected = s, "utf-16"
	case len(b) >= 2 && (b[0] == 0 || b[1] == 0):
		return "", "UTF-16 without a byte order mark"
	default:
		if !utf8.ValidString(b) {
			return "", "invalid UTF-8"
		}
		text, detected = b, "utf-8"
	}
	if declared, ok := declaredEncoding(text); ok {
		declared = strings.ToLower(declared) // ASCII lowercase, as docxdriver
		var family string
		switch declared {
		case "utf-8", "utf8":
			family = "utf-8"
		case "utf-16", "utf-16le", "utf-16be":
			family = "utf-16"
		default:
			return "", fmt.Sprintf("declares encoding %s, not UTF-8 or UTF-16", declared)
		}
		if family != detected {
			return "", fmt.Sprintf("declares encoding %s but is %s", declared, detected)
		}
	}
	return text, ""
}

// decodeUTF16 decodes UTF-16 code units, failing on unpaired surrogates.
func decodeUTF16(units []uint16) (string, bool) {
	var b strings.Builder
	for i := 0; i < len(units); i++ {
		u := units[i]
		switch {
		case utf16.IsSurrogate(rune(u)):
			if u >= 0xDC00 || i+1 >= len(units) {
				return "", false
			}
			r := utf16.DecodeRune(rune(u), rune(units[i+1]))
			if r == unicode.ReplacementChar {
				return "", false
			}
			b.WriteRune(r)
			i++
		default:
			b.WriteRune(rune(u))
		}
	}
	return b.String(), true
}

// declaredEncoding returns the encoding an XML declaration at the very
// start of text names.
func declaredEncoding(text string) (string, bool) {
	rest, ok := strings.CutPrefix(text, "<?xml")
	if !ok {
		return "", false
	}
	end := strings.Index(rest, "?>")
	if end < 0 {
		return "", false
	}
	d := rest[:end]
	i := strings.Index(d, "encoding")
	if i < 0 {
		return "", false
	}
	v := strings.TrimLeftFunc(d[i+len("encoding"):], unicode.IsSpace)
	v, ok = strings.CutPrefix(v, "=")
	if !ok {
		return "", false
	}
	v = strings.TrimLeftFunc(v, unicode.IsSpace)
	if v == "" || v[0] != '"' && v[0] != '\'' {
		return "", false
	}
	q := v[0]
	v = v[1:]
	j := strings.IndexByte(v, q)
	if j < 0 {
		return "", false
	}
	return v[:j], true
}

// charNotAllowed returns the disallowed-character message for rune r.
func charNotAllowed(r rune) string {
	return fmt.Sprintf("character U+%04X is not allowed in XML", r)
}

// parsePart parses a part's bytes, held as a string the view keeps (its
// names, values and texts are substrings of it), into a resolved
// document, or returns the error category docxdriver reports for it
// (without the "xml: " prefix).
func parsePart(b string) (*document, string) {
	src, msg := partText(b)
	if msg != "" {
		return nil, msg
	}
	// The view is built as the part is read, without its tree. A part
	// the scan refuses, or whose view it cannot resolve, is parsed again
	// through its tree to name the fault as docxdriver does.
	v := newViewer()
	if tree.ScanString(src, v) == nil && v.msg == "" && v.doc.root != nil {
		v.doc.starts = startsIn(v.doc.starts, b, src)
		return v.doc, ""
	}
	return parseTree(b, src)
}

// parsePartTree is parsePart through the part's tree, without the scan.
func parsePartTree(b string) (*document, string) {
	src, msg := partText(b)
	if msg != "" {
		return nil, msg
	}
	return parseTree(b, src)
}

// partText decodes a part's bytes to the UTF-8 text the tokenizer reads,
// or returns the error category of an encoding or character fault.
func partText(b string) (string, string) {
	text, msg := decodeXML(b)
	if msg != "" {
		return "", msg
	}
	for i := 0; i < len(text); {
		c := text[i]
		if c >= 0x20 && c < utf8.RuneSelf {
			i++
			continue
		}
		r, size := utf8.DecodeRuneInString(text[i:])
		if !xmlscan.IsXMLChar(r) && r != 0xFEFF {
			return "", charNotAllowed(r)
		}
		i += size
	}
	return declareUTF8(strings.TrimPrefix(text, "\uFEFF")), ""
}

// parseTree parses src, a part's text (b its bytes), through its tree,
// rewriting the faults docxdriver's parser accepted.
func parseTree(b, src string) (*document, string) {
	for range maxFixups + 1 {
		d, err := tree.ParseString(src)
		if err == nil {
			doc, msg := build(d)
			if doc != nil {
				doc.starts = startsIn(doc.starts, b, src)
			}
			return doc, msg
		}
		fixed, msg := classify([]byte(src), err)
		if fixed == nil {
			return nil, msg
		}
		src = string(fixed)
	}
	return nil, "not well-formed: syntax error"
}

// declareUTF8 rewrites the encoding an XML declaration at the start
// names to UTF-8: decodeXML has checked it against the bytes and turned
// UTF-16 into UTF-8, and the tokenizer reads UTF-8 only.
func declareUTF8(src string) string {
	if !strings.HasPrefix(src, "<?xml") {
		return src
	}
	end := strings.Index(src, "?>")
	if end < 0 {
		return src
	}
	i := strings.Index(src[:end], "encoding")
	if i < 0 {
		return src
	}
	j := i + len("encoding")
	for j < end && isSpaceByte(src[j]) {
		j++
	}
	if j >= end || src[j] != '=' {
		return src
	}
	j++
	for j < end && isSpaceByte(src[j]) {
		j++
	}
	if j >= end || src[j] != '"' && src[j] != '\'' {
		return src
	}
	k := strings.IndexByte(src[j+1:end], src[j])
	if k < 0 {
		return src
	}
	return src[:j+1] + "UTF-8" + src[j+1+k:]
}

// build resolves a parsed part. It applies the one check docxdriver's
// parser made that the tokenizer does not: no DTD.
func build(d *tree.Document) (*document, string) {
	v := newViewer()
	for c := range d.Node().ChildrenSeq() {
		switch c.Kind() {
		case tree.DoctypeNode:
			return nil, "contains a DTD"
		case tree.ElementNode:
			if msg := v.buildElement(c); msg != "" {
				return nil, msg
			}
		}
	}
	if v.doc.root == nil {
		return nil, "not well-formed: no root element"
	}
	return v.doc, ""
}

// buildElement resolves n and its subtree, whose depth the tokenizer
// bounds at tree.MaxDepth.
func (v *viewer) buildElement(n *tree.Node) string {
	if err := v.Start(n); err != nil {
		return v.msg
	}
	for c := range n.ChildrenSeq() {
		if c.Kind() == tree.ElementNode {
			if msg := v.buildElement(c); msg != "" {
				return msg
			}
			continue
		}
		if v.Leaf(c) != nil {
			return v.msg
		}
	}
	if v.End(n) != nil {
		return v.msg
	}
	return ""
}

// errView stops a scan whose part the view cannot resolve; the message
// is in viewer.msg.
var errView = errors.New("integrity: view refused")

// viewer builds a document's view from its nodes in document order, from
// a tree (build) or as the part is read (tree.Scan). Names are interned.
type viewer struct {
	doc *document
	// stack holds the open elements; last the last element child of
	// each, or none.
	stack, last []int32
	names       map[qn]*qn
	msg         string
}

// newViewer returns an empty XML viewer.
func newViewer() *viewer {
	return &viewer{doc: &document{decls: map[int32][]decl{}}, names: map[qn]*qn{}}
}

// intern returns the canonical qualified name for ns, local and prefix.
func (v *viewer) intern(ns, local, prefix string) *qn {
	k := qn{ns, local, prefix}
	if q, ok := v.names[k]; ok {
		return q
	}
	q := &k
	v.names[k] = q
	return q
}

// fail records msg and returns the viewer error.
func (v *viewer) fail(msg string) error {
	v.msg = msg
	return errView
}

// newElement appends an element to the document.
func (v *viewer) newElement() *element {
	d := v.doc
	if int(d.n>>blockShift) == len(d.blocks) {
		d.blocks = append(d.blocks, make([]element, 1<<blockShift))
	}
	i := d.n
	d.n++
	e := d.el(i)
	*e = element{doc: d, index: i, parent: none, first: none, next: none}
	return e
}

// Start opens element n.
func (v *viewer) Start(n *tree.Node) error {
	d := v.doc
	parent := int32(none)
	if k := len(v.stack); k > 0 {
		parent = v.stack[k-1]
	} else if d.root != nil {
		return v.fail("not well-formed: more than one root element")
	}
	e := v.newElement()
	e.parent = parent
	start, _, _ := n.Span()
	d.starts = append(d.starts, start)
	// The strings the view keeps (values, texts, names) are those the
	// tokenizer gives: substrings of its text of the part where they
	// need no decoding, which the view holds instead of copying each.
	for name, value := range n.AttrsSeq() {
		switch {
		case name == "xmlns":
			d.decls[e.index] = append(d.decls[e.index], decl{"", value})
			e.decls = true
		case strings.HasPrefix(name, "xmlns:"):
			d.decls[e.index] = append(d.decls[e.index], decl{name[len("xmlns:"):], value})
			e.decls = true
		}
	}
	prefix, local, _ := strings.Cut(n.Name(), ":")
	if local == "" {
		prefix, local = "", prefix
	}
	ns, _ := e.lookupPrefix(prefix)
	e.qn = v.intern(ns, local, prefix)
	e.attr0 = int32(len(d.attrs)) //nolint:gosec // G115: a part has fewer than 2^31 attributes (its size is below 2 GiB)
	for name, value := range n.AttrsSeq() {
		if name == "xmlns" || strings.HasPrefix(name, "xmlns:") {
			continue
		}
		ap, al, _ := strings.Cut(name, ":")
		if al == "" {
			ap, al = "", ap
		}
		var ns string
		if ap != "" {
			uri, ok := e.lookupPrefix(ap)
			if !ok {
				return v.fail("undeclared namespace prefix " + ap)
			}
			ns = uri
		}
		d.attrs = append(d.attrs, attribute{qn: v.intern(ns, al, ap), value: value})
	}
	e.nattr = int32(len(d.attrs)) - e.attr0 //nolint:gosec // G115: as above
	e.t0 = int32(len(d.texts))              //nolint:gosec // G115: a part has fewer than 2^31 text nodes
	if parent == none {
		d.root = e
	} else if k := len(v.last) - 1; v.last[k] == none {
		d.el(parent).first = e.index
		v.last[k] = e.index
	} else {
		d.el(v.last[k]).next = e.index
		v.last[k] = e.index
	}
	v.stack = append(v.stack, e.index)
	v.last = append(v.last, none)
	return nil
}

// End closes the open element.
func (v *viewer) End(*tree.Node) error {
	k := len(v.stack) - 1
	e := v.doc.el(v.stack[k])
	e.end = v.doc.n
	e.t1 = int32(len(v.doc.texts)) //nolint:gosec // G115: as in Start
	v.stack, v.last = v.stack[:k], v.last[:k]
	return nil
}

// Leaf adds character data to the open element; a DTD refuses the part.
func (v *viewer) Leaf(n *tree.Node) error {
	if n.Kind() == tree.DoctypeNode {
		return v.fail("contains a DTD")
	}
	k := len(v.stack)
	if k == 0 {
		return nil
	}
	var s string
	switch n.Kind() {
	case tree.TextNode:
		s = decodeText(n.RawString())
	case tree.CDataNode:
		r := n.RawString()
		s = normalizeEOL(r[9 : len(r)-3])
	default:
		return nil
	}
	v.doc.texts = append(v.doc.texts, textRun{s: s, owner: v.stack[k-1]})
	return nil
}

// startsIn returns the element offsets found in parsed, the bytes the
// tokenizer read, as offsets in raw, the part's bytes: the same when the
// two are equal, shifted past a byte order mark it lacks, and nil when
// parsing rewrote the part.
func startsIn(starts []int, raw, parsed string) []int {
	switch {
	case raw == parsed:
		return starts
	case strings.HasPrefix(raw, "\uFEFF") && raw[3:] == parsed:
		for i := range starts {
			starts[i] += 3
		}
		return starts
	}
	return nil
}

// normalizeEOL converts CRLF and CR line ends in s to LF.
func normalizeEOL(s string) string {
	if !strings.Contains(s, "\r") {
		return s
	}
	return strings.ReplaceAll(strings.ReplaceAll(s, "\r\n", "\n"), "\r", "\n")
}

// decodeText decodes the references in well-formed character data and
// normalises its line ends. Text with neither is returned as it is.
func decodeText(raw string) string {
	if strings.IndexByte(raw, '&') < 0 && strings.IndexByte(raw, '\r') < 0 {
		return raw
	}
	var b strings.Builder
	b.Grow(len(raw))
	for i := 0; i < len(raw); i++ {
		c := raw[i]
		switch c {
		case '&':
			end := strings.IndexByte(raw[i:], ';')
			if end < 0 {
				b.WriteByte(c)
				continue
			}
			if r, ok := resolveRef(raw[i+1 : i+end]); ok {
				b.WriteString(r)
				i += end
				continue
			}
			b.WriteByte(c)
		case '\r':
			if i+1 < len(raw) && raw[i+1] == '\n' {
				i++
			}
			b.WriteByte('\n')
		default:
			b.WriteByte(c)
		}
	}
	return b.String()
}

// resolveRef resolves the body of a reference (between & and ;).
func resolveRef(body string) (string, bool) {
	switch body {
	case "amp":
		return "&", true
	case "lt":
		return "<", true
	case "gt":
		return ">", true
	case "quot":
		return `"`, true
	case "apos":
		return "'", true
	}
	if r, ok := charRef(body); ok {
		return string(r), true
	}
	return "", false
}

// charRef parses a character reference body ("#65", "#x41") as Rust's
// quick-xml does: the value must be a Unicode scalar value other than 0.
func charRef(body string) (rune, bool) {
	digits, ok := strings.CutPrefix(body, "#")
	if !ok {
		return 0, false
	}
	base := uint64(10)
	if h, ok := strings.CutPrefix(digits, "x"); ok {
		digits, base = h, 16
	}
	if strings.HasPrefix(digits, "+") {
		return 0, false
	}
	v, ok := parseRustUint(digits, base, 0x10FFFF)
	if !ok || v == 0 || v >= 0xD800 && v <= 0xDFFF {
		return 0, false
	}
	return rune(v), true //nolint:gosec // G115: v <= 0x10FFFF, checked by parseRustUint
}

// parseRustUint parses digits as Rust's from_str_radix does for an
// unsigned type (an optional leading +), failing above limit.
func parseRustUint(s string, base, limit uint64) (uint64, bool) {
	s = strings.TrimPrefix(s, "+")
	if s == "" {
		return 0, false
	}
	var v uint64
	for i := 0; i < len(s); i++ {
		d, ok := digitValue(s[i])
		if !ok || d >= base {
			return 0, false
		}
		v = v*base + d
		if v > limit {
			return 0, false
		}
	}
	return v, true
}

// digitValue returns the value of c as a hexadecimal digit, or false.
func digitValue(c byte) (uint64, bool) {
	switch {
	case '0' <= c && c <= '9':
		return uint64(c - '0'), true
	case 'a' <= c && c <= 'z':
		return uint64(c-'a') + 10, true
	case 'A' <= c && c <= 'Z':
		return uint64(c-'A') + 10, true
	}
	return 0, false
}

// isNCName reports whether s is an XML NCName (Namespaces in XML). docxdriver
// tested Rust's alphabetic and alphanumeric classes instead, which refuse
// valid names with combining marks and admit some invalid ones.
func isNCName(s string) bool {
	for i, r := range s {
		if i == 0 && !(xmlscan.IsNameStart(r) && r != ':') || i > 0 && !(xmlscan.IsNameChar(r) && r != ':') {
			return false
		}
	}
	return s != ""
}
