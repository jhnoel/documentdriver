package math

import (
	"strconv"
	"strings"
	"unicode"

	"github.com/jhnoel/documentdriver/internal/ooxml/tree"
)

// Namespaces.
const (
	// OMMLNS is the Office Math namespace bound to the m: prefix.
	OMMLNS = "http://schemas.openxmlformats.org/officeDocument/2006/math"
	// wordNS is the WordprocessingML namespace.
	wordNS = "http://schemas.openxmlformats.org/wordprocessingml/2006/main"
)

// The helpers below match elements and attributes by local name, as Word
// and docxdriver read OMML; namespaces are checked where they matter.

// localName returns the part of a qualified name after its last colon.
func localName(qname string) string {
	if i := strings.LastIndexByte(qname, ':'); i >= 0 {
		return qname[i+1:]
	}
	return qname
}

// isElem reports whether n is an element with local name local, in any
// namespace (DocxXml::is_local).
func isElem(n *tree.Node, local string) bool {
	return n != nil && n.Kind() == tree.ElementNode && localName(n.Name()) == local
}

// elemName returns an element's local name, or "" for other nodes.
func elemName(n *tree.Node) string {
	if n.Kind() != tree.ElementNode {
		return ""
	}
	return localName(n.Name())
}

// childElem returns the first child of n (which may be nil) with local name local
// (DocxXml::child_local).
func childElem(n *tree.Node, local string) *tree.Node {
	if n == nil {
		return nil
	}
	for c := range n.ChildrenSeq() {
		if isElem(c, local) {
			return c
		}
	}
	return nil
}

// attr returns the value of the first attribute of n whose local name is
// local, namespace declarations included (DocxXml::attr).
func attr(n *tree.Node, local string) (string, bool) {
	if n == nil || n.Kind() != tree.ElementNode {
		return "", false
	}
	for _, a := range n.Attrs() {
		if localName(a.Name) == local {
			return a.Value, true
		}
	}
	return "", false
}

// elements returns the element children of n.
func elements(n *tree.Node) []*tree.Node { return n.Elements() }

// textContent concatenates the decoded text and CDATA of n and its
// descendants, like the DOM textContent. It stops below depth levels.
func textContent(n *tree.Node) string {
	var b strings.Builder
	appendText(&b, n, 0)
	return b.String()
}

// appendText appends the decoded text and CDATA descendants of n to b, bounded by depth.
func appendText(b *strings.Builder, n *tree.Node, depth int) {
	switch n.Kind() {
	case tree.TextNode:
		decodeText(b, n.RawString())
	case tree.CDataNode:
		r := n.RawString()
		normalizeLineEnds(b, r[len("<![CDATA["):len(r)-len("]]>")])
	case tree.ElementNode, tree.DocumentNode:
		if depth > tree.MaxDepth {
			return
		}
		for c := range n.ChildrenSeq() {
			appendText(b, c, depth+1)
		}
	}
}

// decodeText writes character data as written in a parsed part, decoded:
// the predefined entities and character references are expanded and line
// ends normalised. The tokenizer has checked every reference.
func decodeText(b *strings.Builder, raw string) {
	for i := 0; i < len(raw); {
		c := raw[i]
		switch c {
		case '&':
			end := i + 1
			for end < len(raw) && raw[end] != ';' {
				end++
			}
			if end >= len(raw) {
				b.WriteString(raw[i:])
				return
			}
			b.WriteString(reference(raw[i+1 : end]))
			i = end + 1
		case '\r':
			b.WriteByte('\n')
			i++
			if i < len(raw) && raw[i] == '\n' {
				i++
			}
		default:
			b.WriteByte(c)
			i++
		}
	}
}

// normalizeLineEnds writes raw with CRLF and lone CR as LF, as docxdriver
// does to every part before parsing it.
func normalizeLineEnds(b *strings.Builder, raw string) {
	for i := 0; i < len(raw); i++ {
		if raw[i] != '\r' {
			b.WriteByte(raw[i])
			continue
		}
		b.WriteByte('\n')
		if i+1 < len(raw) && raw[i+1] == '\n' {
			i++
		}
	}
}

// reference expands the body of an entity or character reference.
func reference(name string) string {
	switch name {
	case "lt":
		return "<"
	case "gt":
		return ">"
	case "amp":
		return "&"
	case "quot":
		return `"`
	case "apos":
		return "'"
	}
	if num, ok := strings.CutPrefix(name, "#"); ok {
		base := 10
		if hex, ok := strings.CutPrefix(num, "x"); ok {
			num, base = hex, 16
		}
		if v, err := strconv.ParseInt(num, base, 32); err == nil && v >= 0 && v <= unicode.MaxRune {
			return string(rune(v))
		}
	}
	return "&" + name + ";"
}

// escapeText escapes character data as docxdriver's html::escape_text does.
func escapeText(s string) string {
	if !strings.ContainsAny(s, "&<>") {
		return s
	}
	var b strings.Builder
	for _, r := range s {
		switch r {
		case '&':
			b.WriteString("&amp;")
		case '<':
			b.WriteString("&lt;")
		case '>':
			b.WriteString("&gt;")
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

// escapeAttr also escapes the double quote (html::escape_attr and
// mathml_omml::escape_xml).
func escapeAttr(s string) string {
	if !strings.ContainsAny(s, `&<>"`) {
		return s
	}
	return strings.ReplaceAll(escapeText(s), `"`, "&quot;")
}

// isAlphabetic is Rust's char::is_alphabetic (the Alphabetic property).
func isAlphabetic(r rune) bool {
	return unicode.IsLetter(r) || unicode.Is(unicode.Nl, r) || unicode.Is(unicode.Other_Alphabetic, r)
}

// allRunes reports whether every rune of s satisfies f (true for "").
func allRunes(s string, f func(rune) bool) bool {
	for _, r := range s {
		if !f(r) {
			return false
		}
	}
	return true
}

// onOff reads an ST_OnOff property element (ECMA-376 Part 1, 22.9.2):
// absent is off, present without a value is on, "true", "on" and "1" are
// on and anything else is off.
func onOff(el *tree.Node) bool {
	if el == nil {
		return false
	}
	v, ok := attr(el, "val")
	if !ok {
		return true
	}
	switch v {
	case "true", "on", "1":
		return true
	}
	return false
}
