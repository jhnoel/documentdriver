package model

import (
	"math"
	"strconv"
)

// AppendNumber appends f in the form encoding/json writes a float64: the
// shortest decimal that reads back to the same double, in plain notation
// for magnitudes in [1e-6, 1e21) and in exponent notation otherwise, with
// the exponent's leading zero dropped ("1e-7", "1e+21"). This is "the
// shortest form that reads back to the same double" of §7a, and the text
// find matches numbers against, so the two never disagree.
//
// f must be finite; a reader never builds a NaN or an infinity (the
// cell is left out with a note), and AppendNumber writes "null" for one
// rather than invalid JSON.
func AppendNumber(b []byte, f float64) []byte {
	if math.IsNaN(f) || math.IsInf(f, 0) {
		return append(b, "null"...)
	}
	abs := math.Abs(f)
	fmt := byte('f')
	if abs != 0 && (abs < 1e-6 || abs >= 1e21) {
		fmt = 'e'
	}
	b = strconv.AppendFloat(b, f, fmt, -1, 64)
	if fmt == 'e' {
		// Clean up e-09 to e-9, as encoding/json does.
		n := len(b)
		if n >= 4 && b[n-4] == 'e' && b[n-3] == '-' && b[n-2] == '0' {
			b[n-2] = b[n-1]
			b = b[:n-1]
		}
	}
	return b
}

// FormatNumber is AppendNumber as a string.
func FormatNumber(f float64) string { return string(AppendNumber(nil, f)) }

// ParseNumber parses the text of a numeric <v> (xsd:double as Excel
// writes it: "1200", "0.1", "1E-3", "-0"). It refuses what strconv
// accepts but a cell may not hold: NaN and infinities in any spelling,
// hexadecimal floats, underscores, and leading or trailing space. ok is
// false for those, and the reader leaves the cell out with a note.
func ParseNumber(s string) (float64, bool) {
	if s == "" {
		return 0, false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		if !(c >= '0' && c <= '9' || c == '.' || c == 'e' || c == 'E' || c == '+' || c == '-') {
			return 0, false
		}
	}
	f, err := strconv.ParseFloat(s, 64)
	if err != nil || math.IsInf(f, 0) {
		return 0, false
	}
	return f, true
}
