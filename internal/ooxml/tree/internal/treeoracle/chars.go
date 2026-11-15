package treeoracle

import (
	"strconv"
	"strings"
	"unicode/utf8"
)

// isSpace reports whether b is XML whitespace (S production).
func isSpace(b byte) bool {
	return b == ' ' || b == '\t' || b == '\n' || b == '\r'
}

// isNameStart reports whether r may start an XML Name (XML 1.0 5th ed.).
func isNameStart(r rune) bool {
	switch {
	case r == ':' || r == '_' || ('A' <= r && r <= 'Z') || ('a' <= r && r <= 'z'):
		return true
	case r < 0xC0:
		return false
	}
	return (0xC0 <= r && r <= 0xD6) || (0xD8 <= r && r <= 0xF6) ||
		(0xF8 <= r && r <= 0x2FF) || (0x370 <= r && r <= 0x37D) ||
		(0x37F <= r && r <= 0x1FFF) || (0x200C <= r && r <= 0x200D) ||
		(0x2070 <= r && r <= 0x218F) || (0x2C00 <= r && r <= 0x2FEF) ||
		(0x3001 <= r && r <= 0xD7FF) || (0xF900 <= r && r <= 0xFDCF) ||
		(0xFDF0 <= r && r <= 0xFFFD) || (0x10000 <= r && r <= 0xEFFFF)
}

// isNameChar reports whether r may continue an XML Name.
func isNameChar(r rune) bool {
	return isNameStart(r) || r == '-' || r == '.' || ('0' <= r && r <= '9') ||
		r == 0xB7 || (0x300 <= r && r <= 0x36F) || (0x203F <= r && r <= 0x2040)
}

// isXMLChar reports whether r is a legal XML 1.0 character.
func isXMLChar(r rune) bool {
	return r == 0x9 || r == 0xA || r == 0xD || (0x20 <= r && r <= 0xD7FF) ||
		(0xE000 <= r && r <= 0xFFFD) || (0x10000 <= r && r <= 0x10FFFF)
}

// checkChars returns the offset of the first byte in b that is not part of
// a legal, UTF-8 encoded XML character, or -1.
func checkChars(b []byte) int {
	for i := 0; i < len(b); {
		c := b[i]
		if c < 0x80 {
			if c < 0x20 && c != '\t' && c != '\n' && c != '\r' {
				return i
			}
			i++
			continue
		}
		r, size := utf8.DecodeRune(b[i:])
		if r == utf8.RuneError && size <= 1 || !isXMLChar(r) {
			return i
		}
		i += size
	}
	return -1
}

// validName reports whether s is a well-formed XML Name.
func validName(s string) bool {
	if s == "" {
		return false
	}
	for i, r := range s {
		if i == 0 {
			if !isNameStart(r) {
				return false
			}
		} else if !isNameChar(r) {
			return false
		}
	}
	return true
}

// validQName reports whether s is a Name with at most one colon that
// neither starts nor ends it (Namespaces in XML QName).
func validQName(s string) bool {
	if !validName(s) {
		return false
	}
	i := strings.IndexByte(s, ':')
	if i < 0 {
		return true
	}
	return i > 0 && i < len(s)-1 && strings.IndexByte(s[i+1:], ':') < 0
}

// refAt parses the reference starting at b[0] == '&'. It returns its length
// including the ';', the character(s) it denotes, and ok=false if it is not
// a predefined entity or a legal character reference.
func refAt(b []byte) (n int, val string, ok bool) {
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
	body := string(b[1:end])
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
	if err != nil || !isXMLChar(rune(v)) { //nolint:gosec // G115: ParseUint with bitSize 32 bounds v; a value that wraps to a negative rune fails isXMLChar
		return 0, "", false
	}
	return end + 1, string(rune(v)), true //nolint:gosec // G115: ParseUint with bitSize 32 bounds v; a value that wraps to a negative rune fails isXMLChar
}

func allHex(s string) bool {
	for i := 0; i < len(s); i++ {
		c := s[i]
		if !('0' <= c && c <= '9' || 'a' <= c && c <= 'f' || 'A' <= c && c <= 'F') {
			return false
		}
	}
	return true
}

func allDigits(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return true
}

// decodeText decodes references in raw character data and normalises line
// ends (CRLF and lone CR become LF), as an XML processor would. The input
// is assumed to be well formed.
func decodeText(raw []byte) string {
	return decode(raw, false)
}

// decodeAttr decodes an attribute value as written: references are
// expanded, line ends normalised and literal whitespace characters become
// spaces (CDATA attribute-value normalisation).
func decodeAttr(raw []byte) string {
	return decode(raw, true)
}

func decode(raw []byte, attr bool) string {
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

// escapeAttr escapes s for use as an attribute value delimited by quote.
// Whitespace other than space is written as character references so that
// it survives attribute-value normalisation.
func escapeAttr(s string, quote byte) []byte {
	var b []byte
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c == '&':
			b = append(b, "&amp;"...)
		case c == '<':
			b = append(b, "&lt;"...)
		case c == '>':
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
	return b
}

// EscapeText escapes s for use as character data.
func EscapeText(s string) []byte {
	var b []byte
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
	return b
}
