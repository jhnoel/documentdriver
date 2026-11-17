package htmlx

import (
	"errors"
	"strconv"
	"strings"
)

// ErrQuotedName is returned by QuotedName for a value that is not one
// quoted CSS string.
var ErrQuotedName = errors.New("want one quoted name")

// QuotedName reads a CSS value that is exactly one quoted string, in
// single or double quotes, with backslash escapes ("\'" is a quote; a
// backslash before any other byte keeps that byte): the form pptx and
// vsdx write font-family in ('Calibri', 'O\'Neil'). Anything before or
// after the closing quote is ErrQuotedName. It does not trim spaces.
func QuotedName(v string) (string, error) {
	if len(v) < 2 || (v[0] != '\'' && v[0] != '"') {
		return "", ErrQuotedName
	}
	q := v[0]
	var b strings.Builder
	for i := 1; i < len(v); i++ {
		c := v[i]
		switch {
		case c == '\\' && i+1 < len(v):
			i++
			b.WriteByte(v[i])
		case c == q:
			if i != len(v)-1 {
				return "", ErrQuotedName
			}
			return b.String(), nil
		default:
			b.WriteByte(c)
		}
	}
	return "", ErrQuotedName
}

// Measure reads a CSS number with the unit suffix unit ("pt", "%", ""):
// decimal digits with at most one point and at least one digit, a
// leading minus only when neg; no exponent, no plus sign, no spaces. ok
// is false for anything else and for a value that does not fit a
// float64.
func Measure(v, unit string, neg bool) (f float64, ok bool) {
	s, found := strings.CutSuffix(v, unit)
	if !found || s == "" {
		return 0, false
	}
	body := s
	if body[0] == '-' {
		if !neg {
			return 0, false
		}
		body = body[1:]
	}
	dot, digit := false, false
	for i := 0; i < len(body); i++ {
		switch c := body[i]; {
		case c == '.' && !dot:
			dot = true
		case c >= '0' && c <= '9':
			digit = true
		default:
			return 0, false
		}
	}
	if !digit {
		return 0, false
	}
	f, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return 0, false
	}
	return f, true
}
