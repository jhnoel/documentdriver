// Package ascii holds ASCII-only case mapping and character classes:
// the semantics of Rust's to_ascii_lowercase, eq_ignore_ascii_case and
// is_ascii_digit that the docx port and the HTML tokenizer depend on,
// where the Unicode-aware strings and unicode functions answer
// differently (strings.ToLower maps 'İ'; strings.EqualFold folds
// U+212A KELVIN SIGN to 'k'). It imports only the standard library.
package ascii

import "strings"

// Lower returns s with A-Z mapped to a-z and every other byte
// unchanged. It returns s itself, without allocating, when s has no
// ASCII uppercase letter; otherwise it allocates once.
func Lower(s string) string {
	i := 0
	for ; i < len(s); i++ {
		if c := s[i]; c >= 'A' && c <= 'Z' {
			break
		}
	}
	if i == len(s) {
		return s
	}
	var b strings.Builder
	b.Grow(len(s))
	b.WriteString(s[:i])
	for ; i < len(s); i++ {
		b.WriteByte(LowerByte(s[i]))
	}
	return b.String()
}

// LowerByte returns b mapped to lowercase when it is A-Z, else b.
func LowerByte(b byte) byte {
	if b >= 'A' && b <= 'Z' {
		return b + 32
	}
	return b
}

// LowerRune returns r mapped to lowercase when it is A-Z, else r.
func LowerRune(r rune) rune {
	if r >= 'A' && r <= 'Z' {
		return r + 32
	}
	return r
}

// EqualFold reports whether a and b are equal when A-Z and a-z are
// identified; every other byte must match exactly. It does not
// allocate.
func EqualFold(a, b string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range len(a) {
		if LowerByte(a[i]) != LowerByte(b[i]) {
			return false
		}
	}
	return true
}

// IsAlpha reports whether r is an ASCII letter (A-Z, a-z).
func IsAlpha(r rune) bool { return (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') }

// IsDigit reports whether r is an ASCII digit (0-9).
func IsDigit(r rune) bool { return r >= '0' && r <= '9' }

// IsAlnum reports whether r is an ASCII letter or digit.
func IsAlnum(r rune) bool { return IsAlpha(r) || IsDigit(r) }

// IsHex reports whether r is an ASCII hexadecimal digit.
func IsHex(r rune) bool {
	return IsDigit(r) || (r >= 'a' && r <= 'f') || (r >= 'A' && r <= 'F')
}

// Digits reports whether s is non-empty and every byte of it is an
// ASCII digit.
func Digits(s string) bool {
	if s == "" {
		return false
	}
	for i := range len(s) {
		if c := s[i]; c < '0' || c > '9' {
			return false
		}
	}
	return true
}

// Valid reports whether every byte of s is below 0x80 (true for "").
func Valid(s string) bool {
	for i := range len(s) {
		if s[i] >= 0x80 {
			return false
		}
	}
	return true
}
