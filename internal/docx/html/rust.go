package html

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"unicode"
)

// Helpers that reproduce Rust standard-library semantics the ported code
// depends on (str::trim and char::is_whitespace match Go's unicode.IsSpace
// and strings.TrimSpace: both use the Unicode White_Space property).

// errorf returns an error formatted from format and args.
func errorf(format string, args ...any) error { return fmt.Errorf(format, args...) }

// rustDebugString approximates Rust's {:?} for a str: quoted, with
// backslash, quote, \t, \r, \n and \0 escaped and other control characters
// written as \u{…}. Rust also escapes some non-control unprintables; those
// are not reproduced.
func rustDebugString(s string) string {
	var b strings.Builder
	b.WriteByte('"')
	for _, r := range s {
		switch r {
		case '"':
			b.WriteString(`\"`)
		case '\\':
			b.WriteString(`\\`)
		case '\n':
			b.WriteString(`\n`)
		case '\r':
			b.WriteString(`\r`)
		case '\t':
			b.WriteString(`\t`)
		case 0:
			b.WriteString(`\0`)
		default:
			if unicode.IsControl(r) {
				fmt.Fprintf(&b, `\u{%x}`, r)
			} else {
				b.WriteRune(r)
			}
		}
	}
	b.WriteByte('"')
	return b.String()
}

// parseU32 is str::parse::<u32>: an optional '+' then ASCII digits, no
// overflow.
func parseU32(s string) (uint32, bool) {
	s = strings.TrimPrefix(s, "+")
	if s == "" {
		return 0, false
	}
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return 0, false
		}
	}
	v, err := strconv.ParseUint(s, 10, 32)
	if err != nil {
		return 0, false
	}
	return uint32(v), true
}

// parseHexU32 is u32::from_str_radix(s, 16), which also takes a '+'.
func parseHexU32(s string) (uint32, bool) {
	s = strings.TrimPrefix(s, "+")
	if s == "" {
		return 0, false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f' || c >= 'A' && c <= 'F') {
			return 0, false
		}
	}
	v, err := strconv.ParseUint(s, 16, 32)
	if err != nil {
		return 0, false
	}
	return uint32(v), true
}

// parseF64 is str::parse::<f64>: decimal (optionally signed, with an
// exponent), inf, infinity or nan in any case. Out-of-range values become
// infinities rather than errors; hex floats and underscores are rejected.
func parseF64(s string) (float64, bool) {
	if s == "" || strings.ContainsAny(s, "xX_ \t\n") {
		return 0, false
	}
	v, err := strconv.ParseFloat(s, 64)
	if err != nil {
		if errors.Is(err, strconv.ErrRange) {
			return v, true
		}
		return 0, false
	}
	return v, true
}
