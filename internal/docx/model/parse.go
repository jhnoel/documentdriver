package model

import (
	"strconv"

	"github.com/jhnoel/documentdriver/internal/ascii"
)

// The integer parsers read attribute values the way docxdriver's Rust
// str::parse does: an optional sign ("+" for unsigned types, "+" or "-"
// for signed ones), then one or more ASCII digits and nothing else, and no
// overflow.

// ParseU32 parses a u32.
func ParseU32(s string) (uint32, bool) {
	s, ok := unsignedDigits(s)
	if !ok {
		return 0, false
	}
	v, err := strconv.ParseUint(s, 10, 32)
	return uint32(v), err == nil
}

// ParseU64 parses a u64.
func ParseU64(s string) (uint64, bool) {
	s, ok := unsignedDigits(s)
	if !ok {
		return 0, false
	}
	v, err := strconv.ParseUint(s, 10, 64)
	return v, err == nil
}

// ParseI64 parses an i64.
func ParseI64(s string) (int64, bool) {
	neg := false
	if s != "" && (s[0] == '+' || s[0] == '-') {
		neg = s[0] == '-'
		s = s[1:]
	}
	if !ascii.Digits(s) {
		return 0, false
	}
	if neg {
		s = "-" + s
	}
	v, err := strconv.ParseInt(s, 10, 64)
	return v, err == nil
}

// unsignedDigits strips an optional '+' from s and reports whether the rest is all digits.
func unsignedDigits(s string) (string, bool) {
	if s != "" && s[0] == '+' {
		s = s[1:]
	}
	return s, ascii.Digits(s)
}
