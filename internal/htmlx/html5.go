package htmlx

import (
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/jhnoel/documentdriver/internal/ascii"
)

// The HTML dialect: what docxdriver 0.5.2 accepts through xmloxide 0.4.3's
// WHATWG parser, for the element subset its HTML input lowers.
//
// The tokenizer (tokenizer.go) is a port of xmloxide's, so ParseHTML fails
// exactly where docxdriver fails on a tokenizer error, with the same code
// and position. xmloxide records no tree-construction errors: docxdriver
// silently accepts every structural repair. The tree builder here applies
// the repairs of the specification that cannot change the meaning of the
// subset — optional end tags (p, li, td, th, tr, tbody/thead/tfoot, h1-h6),
// implied html/head/body and implied table sections and rows, ignored stray
// end tags, an empty <p> for a stray </p>, <image> as <img>, and closing
// all elements at the end of input — and rejects with an UnsupportedError
// the ones that move or duplicate content: misnested end tags, formatting
// elements left open across a block boundary (which the specification
// reopens), content foster-parented out of a table, and elements whose parsing
// rules are not ported (script, template, select, form controls, <math>
// and SVG, and the other special elements docx input never lowers).

// SyntaxError is a tokenizer parse error. Its message reproduces the Rust
// Debug form of xmloxide's ParseDiagnostic, which docxdriver embeds in its
// error text.
type SyntaxError struct {
	Code   string // WHATWG error code, such as "eof-in-tag"
	Line   int    // 1-based line
	Column int    // 1-based column, counted in bytes as xmloxide counts it
	Offset int    // byte offset into the input
}

// Error returns the syntax error in xmloxide ParseDiagnostic Debug form.
func (e *SyntaxError) Error() string {
	return fmt.Sprintf("ParseDiagnostic { severity: Error, message: %q, location: SourceLocation { line: %d, column: %d, byte_offset: %d } }",
		e.Code, e.Line, e.Column, e.Offset)
}

// UnsupportedError reports HTML that docxdriver would accept by repairing
// it in ways this parser does not reproduce, or an element whose parsing
// rules are not implemented.
type UnsupportedError struct {
	Msg string
	// Off is the byte offset in the input near which the parser was when
	// it refused (0 when the refusal is about the whole input).
	Off int
}

// Error returns the unsupported error message.
func (e *UnsupportedError) Error() string { return e.Msg }

// unsupportedElement returns an UnsupportedError naming the unsupported element.
func unsupportedElement(name string) error {
	return &UnsupportedError{Msg: fmt.Sprintf("unsupported HTML element <%s>", name)}
}

// unsupportedStructure returns an UnsupportedError with the formatted unsupported-structure message.
func unsupportedStructure(format string, args ...any) error {
	return &UnsupportedError{Msg: "unsupported HTML structure: " + fmt.Sprintf(format, args...)}
}

// Options configures ParseHTML.
type Options struct {
	// Document parses a whole document rather than the contents of <body>.
	Document bool
	// SelfClosing names HTML elements for which <name/> is an empty element
	// (HTML ignores the slash on non-void elements and leaves them open).
	SelfClosing map[string]bool
	// Strict is the contract's input policy (docs/contract.md, section 7):
	// HTML5 syntax, strict meaning. Only the repairs that cannot change
	// what the author wrote are applied: end tags HTML makes optional (p,
	// li, dd, dt, td, th, tr and the table sections), void elements, and
	// character references. Everything the lenient builder would ignore
	// or invent is refused with an UnsupportedError: an end tag with no
	// open element to close (including </p>, which HTML turns into an
	// empty paragraph, and </br>, which it turns into a line break), an
	// element left open at the end whose end tag is required, and
	// comments, which nothing stores.
	Strict bool
}

// ParseHTML parses input as a whole document (opts.Document) or as the
// contents of <body>, and returns the root html element. In a document the root
// holds head and body (and any whitespace between them); in a fragment it
// holds the parsed nodes.
func ParseHTML(input string, opts Options) (*Node, error) {
	document := opts.Document
	if !utf8.ValidString(input) {
		return nil, &UnsupportedError{Msg: "HTML input is not valid UTF-8"}
	}
	b := &h5{tok: newTokenizer(input), document: document, selfClose: opts.SelfClosing, strict: opts.Strict}
	if document {
		b.mode = hInitial
	} else {
		b.root = &Node{Tag: "html"}
		b.stack = []*Node{b.root}
		b.mode = hInBody
	}
	b.run()
	if e := b.tok.err; e != nil {
		off := originalOffset(input, e.span)
		line, col := byteOffsetToLineCol(input, off)
		return nil, &SyntaxError{Code: e.code, Line: line, Column: col, Offset: off}
	}
	if b.err != nil {
		return nil, b.err
	}
	return b.root, nil
}

// originalOffset maps an offset into the newline-normalised input (CR and
// CRLF become LF) back to the input. xmloxide reports the normalised offset
// and counts lines over the original bytes, which misplaces errors after a
// CR; Go reports the position in the input as written.
func originalOffset(input string, norm int) int {
	i, j := 0, 0
	for i < len(input) && j < norm {
		if input[i] == '\r' && i+1 < len(input) && input[i+1] == '\n' {
			i++
		}
		i++
		j++
	}
	return i
}

// byteOffsetToLineCol counts 1-based lines and byte columns up to offset,
// as xmloxide does.
func byteOffsetToLineCol(input string, offset int) (int, int) {
	end := min(offset, len(input))
	line, col := 1, 1
	for i := range end {
		if input[i] == '\n' {
			line++
			col = 1
		} else {
			col++
		}
	}
	return line, col
}

type hmode uint8

const (
	hInitial hmode = iota
	hBeforeHTML
	hBeforeHead
	hInHead
	hAfterHead
	hInBody
	hText
	hAfterBody
	hAfterAfterBody
)

type h5 struct {
	tok          *tokenizer
	document     bool
	selfClose    map[string]bool
	strict       bool
	quirks       bool
	mode         hmode
	originalMode hmode
	root, head   *Node
	stack        []*Node
	err          error

	text       strings.Builder // pending character tokens
	textTarget *Node
}

// failed reports whether the builder or tokenizer has already recorded an error.
func (b *h5) failed() bool { return b.err != nil || b.tok.err != nil }

// fail records the first builder error, filling an empty offset with the tokenizer position.
func (b *h5) fail(err error) {
	if b.err == nil {
		if u, ok := err.(*UnsupportedError); ok && u.Off == 0 { //nolint:errorlint // err is the value just built, never wrapped
			u.Off = b.tok.pos
		}
		b.err = err
	}
}

// run consumes tokens until failure or EOF, building the tree and checking strict unclosed elements.
func (b *h5) run() {
	for !b.failed() {
		t := b.tok.next()
		if d := b.tok.duplicate; d != "" {
			b.fail(&UnsupportedError{Msg: fmt.Sprintf("duplicate HTML attribute %s; write it once", d)})
			return
		}
		if t.kind == tokChar {
			b.char(t.r)
			continue
		}
		if t.kind == tokComment {
			if b.strict {
				b.fail(unsupportedStructure("a comment; nothing stores comments, remove it"))
				return
			}
			continue // dropped in every mode; text around it stays one node
		}
		b.flush()
		b.process(t)
		if t.kind == tokEOF {
			break
		}
	}
	b.flush()
	if b.strict && !b.failed() {
		for _, n := range b.stack {
			if !optionalEnd[n.Tag] {
				b.fail(unsupportedStructure("<%s> is not closed", n.Tag))
				break
			}
		}
	}
	if b.root == nil && !b.failed() {
		b.root = &Node{Tag: "html"}
	}
}

// optionalEnd are the elements whose end tag HTML lets an author leave
// out, so that the end of the input (or the next element) closes them.
var optionalEnd = map[string]bool{
	"html": true, "head": true, "body": true, "p": true, "li": true, "dd": true, "dt": true,
	"td": true, "th": true, "tr": true, "tbody": true, "thead": true, "tfoot": true,
}

// current returns the innermost open element, or nil when the stack is empty.
func (b *h5) current() *Node {
	if len(b.stack) == 0 {
		return nil
	}
	return b.stack[len(b.stack)-1]
}

// currentTag returns the innermost open tag name, or empty when the stack is empty.
func (b *h5) currentTag() string {
	if c := b.current(); c != nil {
		return c.Tag
	}
	return ""
}

// pop removes the innermost open element from the stack, if any.
func (b *h5) pop() {
	if len(b.stack) > 0 {
		b.stack = b.stack[:len(b.stack)-1]
	}
}

// flush appends the pending characters to the node they were inserted at.
func (b *h5) flush() {
	if b.text.Len() == 0 {
		return
	}
	s := b.text.String()
	b.text.Reset()
	t := b.textTarget
	if isTableSection(t.Tag) && strings.TrimLeft(s, " \t\n\f\r") != "" {
		b.fail(unsupportedStructure("text directly inside <%s> (docxdriver moves it before the table)", t.Tag))
		return
	}
	t.appendText(s, b.tok.pos)
}

// insertChar buffers a character for the current element, flushing when the target changes.
func (b *h5) insertChar(r rune) {
	t := b.current()
	if t != b.textTarget {
		b.flush()
		b.textTarget = t
	}
	b.text.WriteRune(r)
}

// isWS reports whether r is ASCII whitespace.
func isWS(r rune) bool { return r == ' ' || r == '\t' || r == '\n' || r == '\f' || r == '\r' }

// char routes a character token to whitespace handling or body and text insertion by mode.
func (b *h5) char(r rune) {
	switch b.mode {
	case hInitial, hBeforeHTML, hBeforeHead:
		if isWS(r) {
			return
		}
	case hInHead, hAfterHead:
		if isWS(r) {
			b.insertChar(r)
			return
		}
	case hText:
		b.insertChar(r)
		return
	case hAfterAfterBody, hAfterBody:
		if isWS(r) {
			b.insertBodyChar(r)
			return
		}
	case hInBody:
		b.insertBodyChar(r)
		return
	}
	b.flush()
	b.process(token{kind: tokChar, r: r})
}

// insertBodyChar buffers a body character, dropping null characters.
func (b *h5) insertBodyChar(r rune) {
	if r == 0 {
		return // dropped in body, as HTML does (the tokenizer has already failed)
	}
	b.insertChar(r)
}

// insert creates and pushes an element under the current node unless the depth limit is exceeded.
func (b *h5) insert(name string, attrs []Attr) *Node {
	n := &Node{Tag: name, Attrs: attrs, Off: b.tok.pos}
	if len(b.stack) >= MaxDepth {
		b.fail(&UnsupportedError{Msg: fmt.Sprintf("HTML nesting exceeds %d elements", MaxDepth)})
		return n
	}
	p := b.current()
	p.Kids = append(p.Kids, n)
	b.stack = append(b.stack, n)
	return n
}

// insertVoid inserts an element and immediately pops it, leaving it as a leaf.
func (b *h5) insertVoid(name string, attrs []Attr) {
	b.insert(name, attrs)
	b.pop()
}

// mergeAttrs appends attributes not already present to n, doing nothing for nil n.
func (b *h5) mergeAttrs(n *Node, attrs []Attr) {
	if n == nil {
		return
	}
	for _, a := range attrs {
		if _, ok := n.Attr(a.Name); !ok {
			n.Attrs = append(n.Attrs, a)
		}
	}
}

// process dispatches a non-character token to the handler for the current insertion mode.
func (b *h5) process(t token) {
	if b.failed() {
		return
	}
	switch b.mode {
	case hInitial:
		b.initial(t)
	case hBeforeHTML:
		b.beforeHTML(t)
	case hBeforeHead:
		b.beforeHead(t)
	case hInHead:
		b.inHead(t)
	case hAfterHead:
		b.afterHead(t)
	case hInBody:
		b.inBody(t)
	case hText:
		if t.kind == tokEndTag || t.kind == tokEOF {
			b.pop()
			b.mode = b.originalMode
			if t.kind == tokEOF {
				b.process(t)
			}
		}
	case hAfterBody:
		switch {
		case t.kind == tokEndTag && t.name == "html":
			b.mode = hAfterAfterBody
		case t.kind == tokComment, t.kind == tokDoctype, t.kind == tokEOF:
		case t.kind == tokStartTag && t.name == "html":
			b.inBody(t)
		default:
			b.mode = hInBody
			b.process(t)
		}
	case hAfterAfterBody:
		switch {
		case t.kind == tokComment, t.kind == tokDoctype, t.kind == tokEOF:
		case t.kind == tokStartTag && t.name == "html":
			b.inBody(t)
		default:
			b.mode = hInBody
			b.process(t)
		}
	}
}

// initial handles the first document token, setting quirks from the doctype or defaulting to quirky.
func (b *h5) initial(t token) {
	switch t.kind {
	case tokComment:
	case tokDoctype:
		b.quirks = t.forceQuirks || !ascii.EqualFold(t.name, "html")
		b.mode = hBeforeHTML
	default:
		b.quirks = true
		b.mode = hBeforeHTML
		b.process(t)
	}
}

// newRoot creates the html root element and moves the builder to the before-head mode.
func (b *h5) newRoot(attrs []Attr) {
	b.root = &Node{Tag: "html", Attrs: attrs}
	b.stack = []*Node{b.root}
	b.mode = hBeforeHead
}

// isHeadBodyHTMLBr reports whether name is head, body, html, or br.
func isHeadBodyHTMLBr(name string) bool {
	return name == "head" || name == "body" || name == "html" || name == "br"
}

// beforeHTML handles tokens before html, creating the root when necessary.
func (b *h5) beforeHTML(t token) {
	switch {
	case t.kind == tokComment, t.kind == tokDoctype:
	case t.kind == tokStartTag && t.name == "html":
		b.newRoot(t.attrs)
	case t.kind == tokEndTag && !isHeadBodyHTMLBr(t.name):
	default:
		b.newRoot(nil)
		b.process(t)
	}
}

// beforeHead handles tokens before head, creating the head element when necessary.
func (b *h5) beforeHead(t token) {
	switch {
	case t.kind == tokComment, t.kind == tokDoctype:
	case t.kind == tokStartTag && t.name == "html":
		b.mergeAttrs(b.root, t.attrs)
	case t.kind == tokStartTag && t.name == "head":
		b.head = b.insert("head", t.attrs)
		b.mode = hInHead
	case t.kind == tokEndTag && !isHeadBodyHTMLBr(t.name):
	default:
		b.head = b.insert("head", nil)
		b.mode = hInHead
		b.process(t)
	}
}

// headStart handles the head-content start tags; it reports false for
// anything else.
func (b *h5) headStart(t token) bool {
	switch t.name {
	case "meta", "link", "base":
		b.insertVoid(t.name, t.attrs)
	case "title":
		b.rawText(t, sRCData)
	case "style":
		b.rawText(t, sRawText)
	case "script", "template", "noscript", "noframes", "basefont", "bgsound":
		b.fail(unsupportedElement(t.name))
	default:
		return false
	}
	return true
}

// rawText inserts an element and switches the tokenizer and builder to its raw-text mode.
func (b *h5) rawText(t token, st state) {
	b.insert(t.name, t.attrs)
	b.tok.setState(st)
	b.tok.setLastStartTag(t.name)
	b.originalMode = b.mode
	b.mode = hText
}

// inHead handles a token inside head, inserting head content or leaving head.
func (b *h5) inHead(t token) {
	switch {
	case t.kind == tokComment, t.kind == tokDoctype:
	case t.kind == tokStartTag && t.name == "html":
		b.mergeAttrs(b.root, t.attrs)
	case t.kind == tokStartTag && t.name == "head":
	case t.kind == tokStartTag && b.headStart(t):
	case t.kind == tokEndTag && t.name == "head":
		b.pop()
		b.mode = hAfterHead
	case t.kind == tokEndTag && (t.name == "body" || t.name == "html" || t.name == "br"):
		b.pop()
		b.mode = hAfterHead
		b.process(t)
	case t.kind == tokEndTag:
	default:
		b.pop()
		b.mode = hAfterHead
		b.process(t)
	}
}

// afterHead handles a token after </head>, inserting body or routing head content back to head.
func (b *h5) afterHead(t token) {
	switch {
	case t.kind == tokComment, t.kind == tokDoctype:
	case t.kind == tokStartTag && t.name == "html":
		b.mergeAttrs(b.root, t.attrs)
	case t.kind == tokStartTag && t.name == "body":
		b.insert("body", t.attrs)
		b.mode = hInBody
	case t.kind == tokStartTag && t.name == "frameset":
		b.fail(unsupportedElement(t.name))
	case t.kind == tokStartTag && isHeadContent(t.name):
		// Head content after </head> goes back into the head.
		b.stack = append(b.stack, b.head)
		b.headStart(t)
		for i, n := range b.stack {
			if n == b.head {
				b.stack = append(b.stack[:i], b.stack[i+1:]...)
				break
			}
		}
	case t.kind == tokStartTag && t.name == "head":
	case t.kind == tokEndTag && !(t.name == "body" || t.name == "html" || t.name == "br"):
	default:
		b.insert("body", nil)
		b.mode = hInBody
		b.process(t)
	}
}

// isHeadContent reports whether name is an element that belongs in head.
func isHeadContent(name string) bool {
	switch name {
	case "base", "basefont", "bgsound", "link", "meta", "noframes", "script", "style", "template", "title":
		return true
	}
	return false
}
