package xmlscan

import (
	"errors"
	"strings"
)

// ErrSyntax is returned when the bytes at a position are not a token
// this package can read: the source is not well formed there, or the
// position is not a token boundary.
var ErrSyntax = errors.New("xmlscan: not a token boundary")

// Kind is the kind of a Token.
type Kind uint8

// The token kinds.
const (
	Text     Kind = iota // character data up to the next '<' or the end
	StartTag             // <name ...>
	EmptyTag             // <name .../>
	EndTag               // </name>
	Comment              // <!-- ... -->
	CData                // <![CDATA[ ... ]]>
	PI                   // <? ... ?>, the XML declaration included
	Doctype              // <!DOCTYPE ...>, an internal subset included
)

// String returns the kind's name, for messages.
func (k Kind) String() string {
	switch k {
	case Text:
		return "text"
	case StartTag:
		return "start tag"
	case EmptyTag:
		return "empty tag"
	case EndTag:
		return "end tag"
	case Comment:
		return "comment"
	case CData:
		return "CDATA"
	case PI:
		return "processing instruction"
	case Doctype:
		return "doctype"
	}
	return "Kind(?)"
}

// Token is one lexed node of a source string: src[Start:End]. For a tag,
// NameEnd is the end of its qualified name, which starts at Start+1
// (Start+2 for an end tag); it is 0 for the other kinds.
type Token struct {
	Kind       Kind
	Start, End int
	NameEnd    int
}

// IsElement reports whether t starts an element: a start or empty tag.
func (t Token) IsElement() bool { return t.Kind == StartTag || t.Kind == EmptyTag }

// Name returns the qualified name of tag t as written ("" unless t is a
// tag). It does not allocate.
func (t Token) Name(src string) string {
	var s int
	switch t.Kind {
	case StartTag, EmptyTag:
		s = t.Start + 1
	case EndTag:
		s = t.Start + 2
	default:
		return ""
	}
	if s < 0 || s > t.NameEnd || t.NameEnd > len(src) {
		return ""
	}
	return src[s:t.NameEnd]
}

// Local returns the local part of tag t's name (the part after the
// prefix; "" unless t is a tag). It does not allocate.
func (t Token) Local(src string) string {
	n := t.Name(src)
	if i := strings.IndexByte(n, ':'); i >= 0 {
		return n[i+1:]
	}
	return n
}

// Next returns the token that starts at offset i of src. src should be
// bytes tree.Check accepts (or a run of whole nodes of such bytes); a
// position that does not start a token Next can read, or i outside src,
// is ErrSyntax. Next does not allocate; the token is a span of src.
//
// A walk over a part is
//
//	for i := 0; i < len(src); {
//		t, err := xmlscan.Next(src, i)
//		if err != nil { ... }
//		...
//		i = t.End
//	}
//
// Text runs to the next '<'. A tag's name, attributes (whitespace before
// each, a name, '=', a quoted value without '<') and close are checked;
// a comment, CDATA section or processing instruction runs to its
// terminator; a DOCTYPE skips quoted strings and an internal subset as
// tree does. Any other "<!" is ErrSyntax, as tree refuses it.
func Next(src string, i int) (Token, error) {
	if i < 0 || i >= len(src) {
		return Token{}, ErrSyntax
	}
	if src[i] != '<' {
		j := strings.IndexByte(src[i:], '<')
		if j < 0 {
			return Token{Kind: Text, Start: i, End: len(src)}, nil
		}
		return Token{Kind: Text, Start: i, End: i + j}, nil
	}
	rest := src[i:]
	if len(rest) < 2 {
		return Token{}, ErrSyntax
	}
	switch rest[1] {
	case '!':
		switch {
		case strings.HasPrefix(rest, "<!--"):
			return until(src, i, 4, "-->", Comment)
		case strings.HasPrefix(rest, "<![CDATA["):
			return until(src, i, 9, "]]>", CData)
		case strings.HasPrefix(rest, "<!DOCTYPE"):
			return doctype(src, i)
		}
		return Token{}, ErrSyntax
	case '?':
		return until(src, i, 2, "?>", PI)
	case '/':
		return endTag(src, i)
	}
	return startTag(src, i)
}

// until reads a token of kind k that starts at i, whose body begins skip
// bytes in and ends with end.
func until(src string, i, skip int, end string, k Kind) (Token, error) {
	j := strings.Index(src[i+skip:], end)
	if j < 0 {
		return Token{}, ErrSyntax
	}
	return Token{Kind: k, Start: i, End: i + skip + j + len(end)}, nil
}

// doctype reads a DOCTYPE declaration at i, skipping quoted strings and
// an internal subset (with its comments), as tree's scanDoctype does.
func doctype(src string, i int) (Token, error) {
	depth := 0
	var quote byte
	for k := i + len("<!DOCTYPE"); k < len(src); k++ {
		c := src[k]
		switch {
		case quote != 0:
			if c == quote {
				quote = 0
			}
		case c == '"' || c == '\'':
			quote = c
		case depth > 0 && strings.HasPrefix(src[k:], "<!--"):
			end := strings.Index(src[k+4:], "-->")
			if end < 0 {
				return Token{}, ErrSyntax
			}
			k += 4 + end + 2
		case c == '[':
			depth++
		case c == ']':
			depth--
		case c == '>' && depth == 0:
			return Token{Kind: Doctype, Start: i, End: k + 1}, nil
		}
	}
	return Token{}, ErrSyntax
}

// nameAt returns the end of the name that starts at k: the first byte
// that is whitespace, '>' or '/', or -1 when the name is empty, runs off
// the end, or holds a byte no tag name can ('<', '=', a quote).
func nameAt(src string, k int) int {
	s := k
	for k < len(src) {
		switch c := src[k]; c {
		case ' ', '\t', '\n', '\r', '>', '/':
			if k == s {
				return -1
			}
			return k
		case '<', '=', '"', '\'':
			return -1
		}
		k++
	}
	return -1
}

// endTag reads "</name S?>" at i.
func endTag(src string, i int) (Token, error) {
	ne := nameAt(src, i+2)
	if ne < 0 || src[ne] == '/' {
		return Token{}, ErrSyntax
	}
	k := ne
	for k < len(src) && IsSpace(src[k]) {
		k++
	}
	if k >= len(src) || src[k] != '>' {
		return Token{}, ErrSyntax
	}
	return Token{Kind: EndTag, Start: i, End: k + 1, NameEnd: ne}, nil
}

// startTag reads a start or empty-element tag at i, checking each
// attribute.
func startTag(src string, i int) (Token, error) {
	ne := nameAt(src, i+1)
	if ne < 0 {
		return Token{}, ErrSyntax
	}
	for k := ne; ; {
		ws := k
		for k < len(src) && IsSpace(src[k]) {
			k++
		}
		if k >= len(src) {
			return Token{}, ErrSyntax
		}
		switch src[k] {
		case '>':
			return Token{Kind: StartTag, Start: i, End: k + 1, NameEnd: ne}, nil
		case '/':
			if k+1 < len(src) && src[k+1] == '>' {
				return Token{Kind: EmptyTag, Start: i, End: k + 2, NameEnd: ne}, nil
			}
			return Token{}, ErrSyntax
		}
		if k == ws {
			return Token{}, ErrSyntax // attributes are separated by whitespace
		}
		a, ok := attrAt(src, ws, k)
		if !ok {
			return Token{}, ErrSyntax
		}
		k = a.End()
	}
}

// ElementEnd returns, for the start tag t of src, the offset where its
// matching end tag starts (the end of its content) and the offset just
// past that end tag, counting nested elements; for an empty tag, t.End
// twice. Any other token, an unclosed element or an end tag whose name
// is not t's is ErrSyntax. It does not allocate.
func ElementEnd(src string, t Token) (contentEnd, end int, err error) {
	switch t.Kind {
	case EmptyTag:
		return t.End, t.End, nil
	case StartTag:
	default:
		return 0, 0, ErrSyntax
	}
	depth := 1
	for i := t.End; i < len(src); {
		c, err := Next(src, i)
		if err != nil {
			return 0, 0, err
		}
		switch c.Kind {
		case StartTag:
			depth++
		case EndTag:
			depth--
			if depth == 0 {
				if c.Name(src) != t.Name(src) {
					return 0, 0, ErrSyntax
				}
				return c.Start, c.End, nil
			}
		}
		i = c.End
	}
	return 0, 0, ErrSyntax
}
