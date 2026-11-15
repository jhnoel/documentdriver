package integrity

import (
	"bytes"
	"errors"
	"strconv"
	"strings"

	"github.com/jhnoel/documentdriver/internal/ooxml/tree"
	"github.com/jhnoel/documentdriver/internal/ooxml/xmlscan"
)

// maxFixups bounds how often parseXML rewrites a well-formed construct
// the tokenizer rejects (a character reference longer than it reads) and
// parses again.
const maxFixups = 64

// classify maps a tokenizer error to the error category reported for the
// input: docxdriver's category where docxdriver was right (from quick-xml:
// the leading clause of its message), a precise one where it accepted
// what XML 1.0 forbids or named the fault wrongly. src belongs to the
// caller's failed parse. When the input is in fact well-formed (a
// character reference the tokenizer does not read), it returns src with
// that construct rewritten, to be parsed again.
func classify(src []byte, err error) ([]byte, string) {
	var se *tree.SyntaxError
	if !errors.As(err, &se) {
		return nil, classifyNamespace(src, err)
	}
	off := min(max(se.Offset, 0), len(src))
	// docxdriver stopped at a DTD before reaching anything after it.
	if i := bytes.Index(src[:off], []byte("<!DOCTYPE")); i >= 0 {
		return nil, "contains a DTD"
	}
	msg := se.Msg
	has := func(p string) bool { return strings.HasPrefix(msg, p) }
	switch {
	// docxdriver's parser (quick-xml) accepted the constructs of the next
	// four cases, which XML 1.0 forbids; they are reported here
	// (docs/docx-integrity.md).
	case has("XML declaration not at start"):
		return nil, "not well-formed: XML declaration not at the start"
	case has("malformed XML declaration"), has("unsupported XML version"), has("unsupported encoding"),
		has("invalid standalone value"), has("unknown XML declaration attribute"), has("XML declaration without version"):
		return nil, "not well-formed: malformed XML declaration"
	case has("invalid processing instruction target"), has("processing instruction target"):
		return nil, "not well-formed: invalid processing instruction target"
	case has("missing whitespace before attribute"):
		return nil, "not well-formed: missing white space between attributes"
	case has("< in value of attribute"):
		return nil, "not well-formed: < in an attribute value"
	case has("invalid or undefined reference"):
		return classifyRef(src, off)
	case has("CDATA section outside the document element"):
		return nil, "not well-formed: CDATA section outside the root element"
	case has("character data outside the document element"):
		return nil, "not well-formed: text outside the root element"
	case has("unterminated"), has("unsupported markup declaration"):
		return nil, "not well-formed: syntax error"
	case has(`"--" inside comment`), has("unexpected end tag"), has("end tag "), has("expected > in end tag"):
		return nil, "not well-formed: ill-formed document"
	case has(`"]]>" in character data`):
		return nil, "not well-formed: ']]>' in text"
	// docxdriver reported these as "position " (the clause before the
	// colon of quick-xml's "position N: ..." with the digits removed).
	case has("duplicate attribute"):
		return nil, "not well-formed: duplicate attribute"
	case has("expected = after attribute"), has("expected quoted value"):
		return nil, "not well-formed: malformed attribute"
	case has("invalid name"):
		return nil, classifyName(src, off)
	case has("expected /> in"):
		return nil, "not well-formed: invalid element name"
	case has("element <") && strings.HasSuffix(msg, "not closed"):
		return nil, "not well-formed: unclosed element"
	case has("no document element"):
		return nil, "not well-formed: no root element"
	case has("more than one document element"):
		return nil, "not well-formed: more than one root element"
	case has("misplaced DOCTYPE"):
		return nil, "contains a DTD"
	case has("elements nested more than"):
		// docxdriver had no depth limit; the engine's tokenizer does.
		return nil, "not well-formed: elements nested more than " + strconv.Itoa(tree.MaxDepth) + " deep"
	case has("invalid character or UTF-8 encoding"):
		return nil, "invalid UTF-8"
	}
	return nil, "not well-formed: syntax error"
}

// classifyNamespace maps a namespace well-formedness error.
func classifyNamespace(src []byte, err error) string {
	msg := err.Error()
	switch {
	case errors.Is(err, tree.ErrUndeclaredPrefix):
		i := strings.LastIndex(msg, tree.ErrUndeclaredPrefix.Error())
		p := strings.TrimSpace(msg[i+len(tree.ErrUndeclaredPrefix.Error()):])
		if u, err := strconv.Unquote(p); err == nil {
			p = u
		}
		return "undeclared namespace prefix " + p
	case strings.Contains(msg, "the xmlns prefix cannot be declared"):
		return "reserved namespace prefix redeclared"
	case strings.Contains(msg, "invalid binding of the xml namespace"), strings.Contains(msg, "the xmlns namespace cannot be bound"):
		// Namespaces in XML forbids binding another prefix to the xml
		// namespace and any prefix to the xmlns namespace; docxdriver
		// allowed both.
		if bindsReserved(src) {
			return "reserved namespace bound to a prefix"
		}
		return "reserved namespace prefix redeclared"
	case strings.Contains(msg, "bound to empty namespace"):
		return "namespace prefix bound to the empty name"
	case strings.Contains(msg, "duplicate attribute"):
		return "not well-formed: duplicate attribute"
	}
	return "not well-formed: syntax error"
}

// classifyName maps an invalid element or attribute name at off.
func classifyName(src []byte, off int) string {
	switch {
	case off >= 2 && src[off-2] == '<' && src[off-1] == '/':
		return "not well-formed: ill-formed document"
	case off >= 1 && src[off-1] == '<':
		return "not well-formed: invalid element name"
	}
	return "not well-formed: invalid attribute name"
}

// isSpaceByte reports whether c is ASCII whitespace.
func isSpaceByte(c byte) bool { return c == ' ' || c == '\t' || c == '\n' || c == '\r' }

// classifyRef maps a reference the tokenizer rejected at off (src[off] is
// '&'). A character reference docxdriver accepted is rewritten in decimal.
func classifyRef(src []byte, off int) ([]byte, string) {
	inAttr := false
	for i := off - 1; i >= 0; i-- {
		if src[i] == '>' {
			break
		}
		if src[i] == '<' {
			inAttr = true
			break
		}
	}
	end := -1
	for i := off + 1; i < len(src); i++ {
		if src[i] == ';' {
			end = i
			break
		}
		if src[i] == '<' {
			break
		}
	}
	if end < 0 {
		if inAttr {
			return nil, "not well-formed: invalid reference in attribute value"
		}
		return nil, "not well-formed: ill-formed document"
	}
	body := string(src[off+1 : end])
	if r, ok := charRef(body); ok {
		if !xmlscan.IsXMLChar(r) {
			return nil, charNotAllowed(r)
		}
		canon := "&#" + strconv.Itoa(int(r)) + ";"
		if canon == string(src[off:end+1]) {
			return nil, "not well-formed: syntax error" // cannot happen: the tokenizer reads this form
		}
		out := make([]byte, 0, len(src))
		out = append(append(append(out, src[:off]...), canon...), src[end+1:]...)
		return out, ""
	}
	switch {
	case inAttr:
		return nil, "not well-formed: invalid reference in attribute value"
	case strings.HasPrefix(body, "#"):
		return nil, "not well-formed: invalid character reference"
	}
	return nil, "not well-formed: undefined entity"
}

// bindsReserved reports whether a declaration in src binds a prefix other
// than xml to the xml namespace, or any prefix to the xmlns namespace.
func bindsReserved(src []byte) bool {
	for _, r := range []struct{ uri, allowed string }{{xmlNS, "xml"}, {xmlnsNS, ""}} {
		for _, q := range []string{`"`, "'"} {
			quoted := []byte(q + r.uri + q)
			for rest, off := src, 0; ; {
				i := bytes.Index(rest, quoted)
				if i < 0 {
					break
				}
				if p, ok := declaredPrefix(src[:off+i]); ok && p != r.allowed {
					return true
				}
				off += i + len(quoted)
				rest = src[off:]
			}
		}
	}
	return false
}

// declaredPrefix returns the prefix of the namespace declaration whose
// value starts right after head (head ends in xmlns:p= and white space).
func declaredPrefix(head []byte) (string, bool) {
	h := bytes.TrimRight(head, " \t\r\n")
	h, ok := bytes.CutSuffix(h, []byte("="))
	if !ok {
		return "", false
	}
	h = bytes.TrimRight(h, " \t\r\n")
	i := bytes.LastIndexAny(h, " \t\r\n")
	name := string(h[i+1:])
	p, ok := strings.CutPrefix(name, "xmlns:")
	return p, ok
}
