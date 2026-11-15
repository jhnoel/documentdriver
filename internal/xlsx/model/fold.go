package model

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

// Fold maps s to its simple case fold for `find --ignore-case` (approved
// shape: simple Unicode case folding, no locale tailoring): every rune is
// replaced by the smallest rune of its unicode.SimpleFold orbit, so two
// strings are equal under strings.EqualFold exactly when their Folds are
// equal, and Contains(Fold(text), Fold(query)) is a case-insensitive
// substring test. Invalid UTF-8 bytes are kept as they are (they match
// only themselves). Fold returns s itself when nothing changes, so an
// all-lowercase ASCII text costs no allocation.
//
// Simple folding is one rune to one rune: "ß" does not match "ss", and
// the Turkish dotless i folds only with itself.
func Fold(s string) string {
	i := 0
	for i < len(s) {
		c := s[i]
		if c < utf8.RuneSelf {
			if 'A' <= c && c <= 'Z' {
				break
			}
			i++
			continue
		}
		r, w := utf8.DecodeRuneInString(s[i:])
		if r != utf8.RuneError || w != 1 {
			if foldRune(r) != r {
				break
			}
		}
		i += w
	}
	if i == len(s) {
		return s
	}
	var b strings.Builder
	b.Grow(len(s))
	b.WriteString(s[:i])
	for i < len(s) {
		c := s[i]
		if c < utf8.RuneSelf {
			if 'A' <= c && c <= 'Z' {
				c += 'a' - 'A'
			}
			b.WriteByte(c)
			i++
			continue
		}
		r, w := utf8.DecodeRuneInString(s[i:])
		if r == utf8.RuneError && w == 1 {
			b.WriteByte(s[i])
		} else {
			b.WriteRune(foldRune(r))
		}
		i += w
	}
	return b.String()
}

// foldRune is the canonical member of r's SimpleFold orbit: the
// lower-case ASCII letter when the orbit holds an ASCII letter (so the
// Kelvin sign and the long s land with k and s, as Fold's ASCII fast path
// lowers), else the orbit's smallest rune.
func foldRune(r rune) rune {
	lo := r
	ascii := rune(-1)
	for f := unicode.SimpleFold(r); f != r; f = unicode.SimpleFold(f) {
		if f < lo {
			lo = f
		}
		if f < utf8.RuneSelf {
			ascii = f
		}
	}
	if r < utf8.RuneSelf {
		ascii = r
	}
	if ascii >= 0 {
		return unicode.ToLower(ascii)
	}
	return lo
}
