package xmlscan

import (
	"strings"
	"unicode/utf8"
)

// IsSpace reports whether c is XML whitespace (the S production: space,
// tab, line feed, carriage return).
func IsSpace(c byte) bool {
	return c == ' ' || c == '\t' || c == '\n' || c == '\r'
}

// IsNameStart reports whether r may start an XML Name (NameStartChar,
// XML 1.0 fifth edition).
func IsNameStart(r rune) bool {
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

// IsNameChar reports whether r may continue an XML Name (NameChar).
func IsNameChar(r rune) bool {
	return IsNameStart(r) || r == '-' || r == '.' || ('0' <= r && r <= '9') ||
		r == 0xB7 || (0x300 <= r && r <= 0x36F) || (0x203F <= r && r <= 0x2040)
}

// IsXMLChar reports whether r is a legal XML 1.0 character.
func IsXMLChar(r rune) bool {
	return r == 0x9 || r == 0xA || r == 0xD || (0x20 <= r && r <= 0xD7FF) ||
		(0xE000 <= r && r <= 0xFFFD) || (0x10000 <= r && r <= 0x10FFFF)
}

// ValidChars reports whether s is valid UTF-8 made only of characters
// XML 1.0 allows (tab, line feed, carriage return and the ranges from
// U+0020; no other control characters, surrogates, U+FFFE or U+FFFF).
func ValidChars(s string) bool {
	for i := 0; i < len(s); {
		c := s[i]
		if c < utf8.RuneSelf {
			if c < 0x20 && c != '\t' && c != '\n' && c != '\r' {
				return false
			}
			i++
			continue
		}
		r, size := utf8.DecodeRuneInString(s[i:])
		if r == utf8.RuneError && size <= 1 || !IsXMLChar(r) {
			return false
		}
		i += size
	}
	return true
}

// ValidName reports whether s is an XML Name.
func ValidName(s string) bool {
	if s == "" || !utf8.ValidString(s) {
		return false
	}
	for i, r := range s {
		if i == 0 {
			if !IsNameStart(r) {
				return false
			}
		} else if !IsNameChar(r) {
			return false
		}
	}
	return true
}

// ValidQName reports whether s is a qualified name (Namespaces in XML):
// a Name with at most one colon, at neither end.
func ValidQName(s string) bool {
	if !ValidName(s) {
		return false
	}
	i := strings.IndexByte(s, ':')
	if i < 0 {
		return true
	}
	return i > 0 && i < len(s)-1 && strings.IndexByte(s[i+1:], ':') < 0
}
