package tree

import (
	"strconv"
	"unicode/utf8"

	"github.com/jhnoel/documentdriver/internal/ooxml/xmlscan"
)

// isXMLChar reports whether r is a legal XML 1.0 character. It stays
// private here rather than calling xmlscan, which does not export the
// class: checkChars, the tokenizer's whole-source scan, needs the
// offset of the first bad character, which xmlscan.ValidChars (a bool)
// cannot give, and it runs on every byte of every document, so the
// table is a single inlinable predicate there. BenchmarkCheckChars
// pins that cost; the S1b commit message carries BenchmarkParse before
// and after (unchanged: this path is untouched).
func isXMLChar(r rune) bool {
	return r == 0x9 || r == 0xA || r == 0xD || (0x20 <= r && r <= 0xD7FF) ||
		(0xE000 <= r && r <= 0xFFFD) || (0x10000 <= r && r <= 0x10FFFF)
}

// checkChars returns the offset of the first byte in b that is not part of
// a legal, UTF-8 encoded XML character, or -1.
func checkChars(b string) int {
	for i := 0; i < len(b); {
		c := b[i]
		if c < 0x80 {
			if c < 0x20 && c != '\t' && c != '\n' && c != '\r' {
				return i
			}
			i++
			continue
		}
		r, size := utf8.DecodeRuneInString(b[i:])
		if r == utf8.RuneError && size <= 1 || !isXMLChar(r) {
			return i
		}
		i += size
	}
	return -1
}

// refAt parses the reference starting at b[0] == '&'. It returns its length
// including the ';', the character(s) it denotes, and ok=false if it is not
// a predefined entity or a legal character reference. checkRefs uses it to
// validate references in text and attribute values: xmlscan's decoders keep
// a malformed reference as written and cannot validate one, so this stays
// with the tokenizer (it decodes nothing callers read).
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
	if err != nil || !isXMLChar(rune(v)) { //nolint:gosec // G115: ParseUint with bitSize 32 bounds v; a value that wraps to a negative rune fails isXMLChar
		return 0, "", false
	}
	return end + 1, string(rune(v)), true //nolint:gosec // G115: ParseUint with bitSize 32 bounds v; a value that wraps to a negative rune fails isXMLChar
}

// allHex reports whether every byte of s is a hexadecimal digit.
func allHex(s string) bool {
	for i := 0; i < len(s); i++ {
		c := s[i]
		if !('0' <= c && c <= '9' || 'a' <= c && c <= 'f' || 'A' <= c && c <= 'F') {
			return false
		}
	}
	return true
}

// allDigits reports whether every byte of s is a decimal digit.
func allDigits(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return true
}

// EscapeText escapes s for use as character data.
func EscapeText(s string) []byte {
	return []byte(xmlscan.EscapeText(s))
}
