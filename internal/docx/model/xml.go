package model

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"unicode/utf16"
	"unicode/utf8"

	core "github.com/jhnoel/documentdriver/internal/ooxml/edit"
	"github.com/jhnoel/documentdriver/internal/ooxml/tree"
)

// Namespaces the read model checks.
const (
	// NSW is the WordprocessingML main namespace (transitional).
	NSW = "http://schemas.openxmlformats.org/wordprocessingml/2006/main"
	// NSW14 is the Office 2010 wordprocessing extension namespace that
	// carries w14:paraId.
	NSW14 = "http://schemas.microsoft.com/office/word/2010/wordml"
	// NSMC is the markup-compatibility namespace.
	NSMC = "http://schemas.openxmlformats.org/markup-compatibility/2006"
	// NSStrictW is the ISO Strict WordprocessingML namespace. The read
	// model reads it as WordprocessingML (WordML); editing a Strict
	// document is refused by the operations (ErrStrict), never by read.
	NSStrictW = "http://purl.oclc.org/ooxml/wordprocessingml/main"
)

// WordML is the WordprocessingML main namespace family, transitional then
// Strict, as the edit core spells families.
var WordML = core.NS{URIs: []string{NSW, NSStrictW}, Prefix: "w", Table: "w"}

// XML is a parsed part.
type XML struct {
	Doc *tree.Document
}

// ParseXML parses a part into a private tree, from its bytes as written:
// nothing is normalised in the tree, and the text accessor (TextContent)
// normalises line ends as an XML processor would, so text is the same
// as from the opc package's tree of the same part. A part that is not
// well formed is an error reading "document.xml parse failed: parse error
// at <line>:<column>: <what>", docxdriver's wording, positioned in the
// line-end-normalised text as docxdriver's parser sees it.
//
// UTF-16 rule: a part with a UTF-16 byte order mark is transcoded to UTF-8
// first (its declared encoding then says UTF-8), as docxdriver's parser
// reads UTF-16. Such a tree is the read model's own; the edit side's
// working tree reads UTF-8 only and refuses the part (no corpus/lo part is
// UTF-16).
func ParseXML(b []byte) (*XML, error) {
	b = utf16ToUTF8(b)
	d, err := tree.Parse(b)
	if err != nil {
		return nil, parseFailure(b)
	}
	return &XML{Doc: d}, nil
}

// parseFailure words the syntax error of b, a part that does not parse,
// as docxdriver does: it parses the line-end-normalised bytes, so the
// position is counted there.
func parseFailure(b []byte) error {
	n := normalizeLineEnds(utf16ToUTF8(b))
	_, err := tree.Parse(n)
	if err == nil {
		// Unreachable: line ends do not change well-formedness.
		return &ParseError{Msg: "document.xml parse failed", Err: errors.New("tree: parse failed")}
	}
	return parseError(n, err)
}

// isUTF16 reports whether b starts with a UTF-16 byte order mark.
func isUTF16(b []byte) bool {
	return bytes.HasPrefix(b, []byte{0xFF, 0xFE}) || bytes.HasPrefix(b, []byte{0xFE, 0xFF})
}

// utf16ToUTF8 transcodes a part with a UTF-16 byte order mark to UTF-8
// and rewrites its XML declaration's encoding; other input is returned
// unchanged. An odd trailing byte is dropped.
func utf16ToUTF8(b []byte) []byte {
	var order binary.ByteOrder
	switch {
	case bytes.HasPrefix(b, []byte{0xFF, 0xFE}):
		order = binary.LittleEndian
	case bytes.HasPrefix(b, []byte{0xFE, 0xFF}):
		order = binary.BigEndian
	default:
		return b
	}
	units := make([]uint16, 0, len(b)/2)
	for i := 2; i+1 < len(b); i += 2 {
		units = append(units, order.Uint16(b[i:]))
	}
	out := []byte(string(utf16.Decode(units)))
	if bytes.HasPrefix(out, []byte("<?xml")) {
		if end := bytes.Index(out, []byte("?>")); end > 0 {
			decl := encodingDecl.ReplaceAll(out[:end], []byte("${1}UTF-8$2"))
			out = append(decl, out[end:]...)
		}
	}
	return out
}

var encodingDecl = regexp.MustCompile(`(encoding\s*=\s*["'])[^"']*(["'])`)

// normalizeLineEnds applies XML end-of-line handling to raw bytes (for
// error positions and CDATA text), except for UTF-16 input (by its byte
// order mark).
func normalizeLineEnds(b []byte) []byte {
	if isUTF16(b) || bytes.IndexByte(b, '\r') < 0 {
		return b
	}
	out := make([]byte, 0, len(b))
	for i := 0; i < len(b); i++ {
		if b[i] == '\r' {
			if i+1 < len(b) && b[i+1] == '\n' {
				i++
			}
			out = append(out, '\n')
			continue
		}
		out = append(out, b[i])
	}
	return out
}

// Root returns the document element, or nil.
func (x *XML) Root() *tree.Node { return x.Doc.Root() }

// Body returns the w:body child of the document element, or nil.
func (x *XML) Body() *tree.Node {
	r := x.Root()
	if r == nil {
		return nil
	}
	return ChildW(r, "body")
}

// IsElement reports whether n is an element.
func IsElement(n *tree.Node) bool { return n != nil && n.Kind() == tree.ElementNode }

// IsW reports whether n is the WordprocessingML element local: its local
// name is local and it is in the WordML family (transitional or Strict)
// or in no namespace.
func IsW(n *tree.Node, local string) bool {
	if !IsElement(n) || n.Local() != local {
		return false
	}
	return n.Space() == "" || WordML.Has(n)
}

// IsLocal reports whether n is an element with local name local, in any
// namespace.
func IsLocal(n *tree.Node, local string) bool {
	return IsElement(n) && n.Local() == local
}

// Attr returns the value of n's first attribute whose local name is local,
// whatever its prefix (namespace declarations count, by the declared
// prefix: xmlns:w has local name w).
func Attr(n *tree.Node, local string) (string, bool) {
	if !IsElement(n) {
		return "", false
	}
	for name, value := range n.AttrsSeq() {
		if attrLocal(name) == local {
			return value, true
		}
	}
	return "", false
}

// AttrOr returns Attr's value, or "" when the attribute is absent.
func AttrOr(n *tree.Node, local string) string {
	v, _ := Attr(n, local)
	return v
}

// attrLocal returns the local part of qualified name q after its last colon.
func attrLocal(q string) string {
	if i := strings.LastIndexByte(q, ':'); i >= 0 {
		return q[i+1:]
	}
	return q
}

// ChildW returns the first element child of n that IsW local, or nil.
func ChildW(n *tree.Node, local string) *tree.Node {
	for c := range n.ChildrenSeq() {
		if IsW(c, local) {
			return c
		}
	}
	return nil
}

// ChildLocal returns the first element child of n with local name local,
// or nil.
func ChildLocal(n *tree.Node, local string) *tree.Node {
	for c := range n.ChildrenSeq() {
		if IsLocal(c, local) {
			return c
		}
	}
	return nil
}

// TextContent returns the character data of n and all its descendants, in
// document order, with line ends normalised (CRLF and lone CR become LF)
// as an XML processor would: the tree decodes text nodes so, and CDATA is
// normalised here. Trees hold the original bytes; this is the one place
// the read model normalises.
func TextContent(n *tree.Node) string {
	switch n.Kind() {
	case tree.TextNode:
		return n.Data()
	case tree.CDataNode:
		return normalizeCR(n.Data())
	}
	var b strings.Builder
	n.Walk(func(c *tree.Node) bool {
		switch c.Kind() {
		case tree.TextNode:
			b.WriteString(c.Data())
		case tree.CDataNode:
			b.WriteString(normalizeCR(c.Data()))
		}
		return true
	})
	return b.String()
}

// normalizeCR turns CRLF and lone CR into LF.
func normalizeCR(s string) string {
	if !strings.Contains(s, "\r") {
		return s
	}
	return string(normalizeLineEnds([]byte(s)))
}

// Descendants returns the descendants of n (excluding n) in document
// order that keep reports true for.
func Descendants(n *tree.Node, keep func(*tree.Node) bool) []*tree.Node {
	var out []*tree.Node
	n.Walk(func(c *tree.Node) bool {
		if c != n && keep(c) {
			out = append(out, c)
		}
		return true
	})
	return out
}

// Within reports whether n is ancestor or inside it.
func Within(n, ancestor *tree.Node) bool {
	for c := n; c != nil; c = c.Parent() {
		if c == ancestor {
			return true
		}
	}
	return false
}

// NextElement returns the first element sibling after n, or nil (also
// when n has no parent). It follows the tree's sibling links: time
// proportional to the nodes it skips, no allocation.
func NextElement(n *tree.Node) *tree.Node {
	for s := n.NextSibling(); s != nil; s = s.NextSibling() {
		if IsElement(s) {
			return s
		}
	}
	return nil
}

// PrevElement returns the last element sibling before n, or nil (also
// when n has no parent). It follows the tree's sibling links: time
// proportional to the nodes it skips, no allocation.
func PrevElement(n *tree.Node) *tree.Node {
	for s := n.PrevSibling(); s != nil; s = s.PrevSibling() {
		if IsElement(s) {
			return s
		}
	}
	return nil
}

// parseError words a tree syntax error as docxdriver's parser does for the
// two failures the corpus shows (a duplicate attribute and a mismatched end
// tag); other failures keep the tree's description. The position is the
// 1-based line and column (in characters) of the offending markup.
func parseError(src []byte, err error) error {
	var se *tree.SyntaxError
	if !errors.As(err, &se) {
		return fmt.Errorf("document.xml parse failed: %w", err)
	}
	msg, off := se.Msg, se.Offset
	if name, ok := strings.CutPrefix(msg, "duplicate attribute "); ok {
		// Reported after the duplicate attribute's closing quote.
		msg = "duplicate attribute: '" + name + "'"
		if q := bytes.IndexAny(src[min(off, len(src)):], "\"'"); q >= 0 {
			quote := src[off+q]
			if e := bytes.IndexByte(src[off+q+1:], quote); e >= 0 {
				off += q + 1 + e + 1
			}
		}
	} else if rest, ok := strings.CutPrefix(msg, "end tag </"); ok {
		if found, open, ok := strings.Cut(rest, "> does not match <"); ok {
			// Reported after the end tag's name.
			msg = "mismatched end tag: expected </" + strings.TrimSuffix(open, ">") + ">, found </" + found + ">"
			off += len("</") + len(found)
		}
	}
	line, col := position(src, off)
	return &ParseError{Msg: fmt.Sprintf("document.xml parse failed: parse error at %d:%d: %s", line, col, msg), Err: err}
}

// position returns the 1-based line and character column of offset off.
func position(src []byte, off int) (line, col int) {
	off = min(max(off, 0), len(src))
	line = 1 + bytes.Count(src[:off], []byte{'\n'})
	start := bytes.LastIndexByte(src[:off], '\n') + 1
	return line, utf8.RuneCount(src[start:off]) + 1
}

// ParseError is a part that is not well formed.
type ParseError struct {
	Msg string // docxdriver's wording
	Err error  // the tree's syntax error
}

// Error returns the parse error's message.
func (e *ParseError) Error() string { return e.Msg }

// Unwrap returns the parse error's wrapped cause.
func (e *ParseError) Unwrap() error { return e.Err }
