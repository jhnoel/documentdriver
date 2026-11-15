package sml

import (
	"strings"
	"unicode/utf16"
	"unicode/utf8"
)

// UnescapeX decodes the _xHHHH_ escapes SpreadsheetML strings use for
// characters XML cannot carry (ECMA-376 Part 1 §22.9.2.19, ST_Xstring):
// "_x" and exactly four hex digits (either case) and "_" stand for that
// UTF-16 code unit. A literal "_x0041_" is stored as "_x005F_x0041_": the
// first escape decodes to "_" and decoding resumes after it, so the rest
// stays literal. A high surrogate followed by an escaped low surrogate
// is one rune; an unpaired surrogate is U+FFFD. Anything that is not a
// complete escape is kept as it is. UnescapeX returns s itself when it
// holds no "_x", so the common case costs no allocation.
//
// It applies to shared strings, inline strings and t="str" values; never
// to formulas, which do not use the escape.
func UnescapeX(s string) string {
	i := strings.Index(s, "_x")
	if i < 0 {
		return s
	}
	var b strings.Builder
	b.Grow(len(s))
	b.WriteString(s[:i])
	for i < len(s) {
		u, ok := escapeAt(s, i)
		if !ok {
			b.WriteByte(s[i])
			i++
			continue
		}
		i += 7
		if utf16.IsSurrogate(rune(u)) {
			if u < 0xDC00 {
				if lo, ok := escapeAt(s, i); ok && lo >= 0xDC00 && lo <= 0xDFFF {
					b.WriteRune(utf16.DecodeRune(rune(u), rune(lo)))
					i += 7
					continue
				}
			}
			b.WriteRune(utf8.RuneError)
			continue
		}
		b.WriteRune(rune(u))
	}
	return b.String()
}

// escapeAt reads an _xHHHH_ escape at s[i:].
func escapeAt(s string, i int) (uint16, bool) {
	if i+7 > len(s) || s[i] != '_' || s[i+1] != 'x' || s[i+6] != '_' {
		return 0, false
	}
	var u uint16
	for _, c := range []byte(s[i+2 : i+6]) {
		var d byte
		switch {
		case c >= '0' && c <= '9':
			d = c - '0'
		case c >= 'a' && c <= 'f':
			d = c - 'a' + 10
		case c >= 'A' && c <= 'F':
			d = c - 'A' + 10
		default:
			return 0, false
		}
		u = u<<4 | uint16(d)
	}
	return u, true
}

// EscapeX encodes s for a SpreadsheetML string before XML escaping: a
// control character other than tab and line feed, U+FFFE and U+FFFF
// become _xHHHH_ (upper-case hex), and the "_" of anything that would
// read as an escape becomes "_x005F_", so UnescapeX(EscapeX(s)) == s.
// That includes "_xHHHH" followed by a character EscapeX itself escapes,
// whose escape would supply the closing "_" (edit's escapeCellText missed
// it: "_x0000\x01" came back as "\x00x0001_"). Bytes that are not UTF-8
// are kept as they are. It allocates the result once.
func EscapeX(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); {
		if s[i] == '_' && i+7 <= len(s) && s[i+1] == 'x' && isHex4(s[i+2:i+6]) {
			if next, _ := utf8.DecodeRuneInString(s[i+6:]); next == '_' || needsX(next) {
				b.WriteString("_x005F_")
				i++
				continue
			}
		}
		r, n := utf8.DecodeRuneInString(s[i:])
		if needsX(r) {
			const hex = "0123456789ABCDEF"
			b.WriteString("_x")
			b.WriteByte(hex[(r>>12)&15])
			b.WriteByte(hex[(r>>8)&15])
			b.WriteByte(hex[(r>>4)&15])
			b.WriteByte(hex[r&15])
			b.WriteByte('_')
		} else {
			b.WriteString(s[i : i+n])
		}
		i += n
	}
	return b.String()
}

// needsX reports whether EscapeX writes r as _xHHHH_.
func needsX(r rune) bool {
	return (r < 0x20 && r != '\t' && r != '\n') || r == 0xFFFE || r == 0xFFFF
}

// isHex4 reports whether every byte of s, the digits of an _xHHHH_ escape,
// is an ASCII hexadecimal digit (the caller passes exactly four).
func isHex4(s string) bool {
	for i := range len(s) {
		c := s[i]
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f' || c >= 'A' && c <= 'F') {
			return false
		}
	}
	return true
}
