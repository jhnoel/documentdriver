package xmlscan

import (
	"strconv"
	"strings"
)

// DecodeText decodes raw character data as an XML processor does: the
// five predefined entities and character references are expanded, and
// CRLF and a lone CR become LF. A reference that is not one of those is
// kept as written. It returns raw itself, without a copy, when there is
// nothing to decode; otherwise it allocates the result once.
func DecodeText(raw string) string { return decode(raw, false) }

// DecodeAttr decodes a raw attribute value as an XML processor does:
// as DecodeText, and literal tab, line feed and carriage return become
// spaces (CDATA attribute-value normalisation; CRLF is one space). It
// returns raw itself when there is nothing to decode; otherwise it
// allocates the result once.
func DecodeAttr(raw string) string { return decode(raw, true) }

// decode is DecodeText (attr false) and DecodeAttr (attr true): it expands
// references and normalises line ends, and for an attribute also turns tab,
// line feed and carriage return into spaces. It returns raw itself when
// nothing changes.
func decode(raw string, attr bool) string {
	if strings.IndexByte(raw, '&') < 0 && strings.IndexByte(raw, '\r') < 0 &&
		(!attr || strings.IndexByte(raw, '\n') < 0 && strings.IndexByte(raw, '\t') < 0) {
		return raw
	}
	var sb strings.Builder
	sb.Grow(len(raw))
	for i := 0; i < len(raw); {
		c := raw[i]
		switch {
		case c == '&':
			n, v, ok := refAt(raw[i:])
			if !ok {
				sb.WriteByte(c)
				i++
				continue
			}
			sb.WriteString(v)
			i += n
		case c == '\r':
			if i+1 < len(raw) && raw[i+1] == '\n' {
				i++
			}
			if attr {
				sb.WriteByte(' ')
			} else {
				sb.WriteByte('\n')
			}
			i++
		case attr && (c == '\n' || c == '\t'):
			sb.WriteByte(' ')
			i++
		default:
			sb.WriteByte(c)
			i++
		}
	}
	return sb.String()
}

// refAt parses the reference starting at b[0] == '&': its length with
// the ';', the character it denotes, and false when it is not a
// predefined entity or a character reference to a legal XML character.
func refAt(b string) (n int, val string, ok bool) {
	end := -1
	for i := 1; i < len(b) && i < 64; i++ {
		if b[i] == ';' {
			end = i
			break
		}
	}
	if end < 0 {
		return 0, "", false
	}
	body := b[1:end]
	switch body {
	case "amp":
		return end + 1, "&", true
	case "lt":
		return end + 1, "<", true
	case "gt":
		return end + 1, ">", true
	case "quot":
		return end + 1, `"`, true
	case "apos":
		return end + 1, "'", true
	}
	if len(body) < 2 || body[0] != '#' {
		return 0, "", false
	}
	var v uint64
	var err error
	if body[1] == 'x' {
		if len(body) < 3 || !allHex(body[2:]) {
			return 0, "", false
		}
		v, err = strconv.ParseUint(body[2:], 16, 32)
	} else {
		if !allDigits(body[1:]) {
			return 0, "", false
		}
		v, err = strconv.ParseUint(body[1:], 10, 32)
	}
	if err != nil || !IsXMLChar(rune(v)) { //nolint:gosec // G115: ParseUint with bitSize 32 bounds v; a value that wraps to a negative rune fails IsXMLChar
		return 0, "", false
	}
	return end + 1, string(rune(v)), true //nolint:gosec // G115: ParseUint with bitSize 32 bounds v; a value that wraps to a negative rune fails IsXMLChar
}

// allHex reports whether every byte of s is an ASCII hexadecimal digit
// (true for "": callers check the length).
func allHex(s string) bool {
	for i := 0; i < len(s); i++ {
		c := s[i]
		if !('0' <= c && c <= '9' || 'a' <= c && c <= 'f' || 'A' <= c && c <= 'F') {
			return false
		}
	}
	return true
}

// allDigits reports whether every byte of s is an ASCII decimal digit
// (true for "": callers check the length).
func allDigits(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return true
}

// EscapeText escapes s for use as character data: '&', '<' and '>' as
// entities, and a carriage return as "&#13;" (a parser would otherwise
// normalise it away). It returns s itself when nothing needs escaping;
// otherwise it allocates the result once.
func EscapeText(s string) string {
	if !strings.ContainsAny(s, "&<>\r") {
		return s
	}
	b := make([]byte, 0, len(s)+16)
	for i := 0; i < len(s); i++ {
		switch c := s[i]; c {
		case '&':
			b = append(b, "&amp;"...)
		case '<':
			b = append(b, "&lt;"...)
		case '>':
			b = append(b, "&gt;"...)
		case '\r':
			b = append(b, "&#13;"...)
		default:
			b = append(b, c)
		}
	}
	return string(b)
}

// EscapeAttr escapes s for use as an attribute value delimited by quote
// (a double or a single quote): '&', '<', '>' and the quote as entities, and tab, line
// feed and carriage return as character references, so that they
// survive attribute-value normalisation. The other quote is kept. It
// returns s itself when nothing needs escaping; otherwise it allocates
// the result once.
func EscapeAttr(s string, quote byte) string { return escapeAttr(s, quote, true) }

// EscapeAttrMinimal is EscapeAttr without escaping '>', which an
// attribute value may hold as it is: for rewriting values whose '>'
// the input wrote bare. It returns s itself when nothing needs
// escaping; otherwise it allocates the result once.
func EscapeAttrMinimal(s string, quote byte) string { return escapeAttr(s, quote, false) }

// escapeAttr is EscapeAttr (gt true) and EscapeAttrMinimal (gt false): it
// escapes s for an attribute value delimited by quote, writing '>' as an
// entity only when gt is set. It returns s itself when nothing needs
// escaping.
func escapeAttr(s string, quote byte, gt bool) string {
	special := "&<\t\n\r"
	if gt {
		special = "&<>\t\n\r"
	}
	if !strings.ContainsAny(s, special) && strings.IndexByte(s, quote) < 0 {
		return s
	}
	b := make([]byte, 0, len(s)+16)
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c == '&':
			b = append(b, "&amp;"...)
		case c == '<':
			b = append(b, "&lt;"...)
		case c == '>' && gt:
			b = append(b, "&gt;"...)
		case c == '"' && quote == '"':
			b = append(b, "&quot;"...)
		case c == '\'' && quote == '\'':
			b = append(b, "&apos;"...)
		case c == '\t':
			b = append(b, "&#9;"...)
		case c == '\n':
			b = append(b, "&#10;"...)
		case c == '\r':
			b = append(b, "&#13;"...)
		default:
			b = append(b, c)
		}
	}
	return string(b)
}
