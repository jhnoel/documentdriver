package vsd

import (
	"math"
	"strconv"

	"github.com/jhnoel/documentdriver/internal/ascii"
	"github.com/jhnoel/documentdriver/internal/geom"
)

// PointsPerInch converts the stored inches to the points the views show
// by default (exact: 1 in = 72 pt).
const PointsPerInch = 72

// Number parses a stored V that holds a number ("2.5", "-1E-3",
// "1.#INF" is refused): ok is false for anything that is not a finite
// decimal number. Chunk V1.
func Number(v string) (f float64, ok bool) {
	if v == "" {
		return 0, false
	}
	for i := 0; i < len(v); i++ {
		c := v[i]
		if !(c >= '0' && c <= '9' || c == '.' || c == 'e' || c == 'E' || c == '+' || c == '-') {
			return 0, false
		}
	}
	f, err := strconv.ParseFloat(v, 64)
	if err != nil || math.IsNaN(f) || math.IsInf(f, 0) {
		return 0, false
	}
	return f, true
}

// Bool parses a stored boolean V ("0", "1", "FALSE", "TRUE" as Visio
// writes them; format notes section 2): ok is false for anything else.
// Chunk V1.
func Bool(v string) (b, ok bool) {
	switch v {
	case "0":
		return false, true
	case "1":
		return true, true
	}
	if ascii.EqualFold(v, "TRUE") {
		return true, true
	}
	if ascii.EqualFold(v, "false") {
		return false, true
	}
	return false, false
}

// Color parses a stored colour V written as "#RRGGBB" into the
// upper-case form "#RRGGBB". An index ("0", "24"), "Themed" and anything
// else give ok false: indexes are resolved by doc.Document.Color, which
// uses the document's Colors and BuiltinColor. Chunk V1.
func Color(v string) (hex string, ok bool) {
	if len(v) != 7 || v[0] != '#' {
		return "", false
	}
	var b [7]byte
	b[0] = '#'
	for i := 1; i < 7; i++ {
		c := v[i]
		if !ascii.IsHex(rune(c)) {
			return "", false
		}
		if c >= 'a' && c <= 'f' {
			c -= 'a' - 'A'
		}
		b[i] = c
	}
	return string(b[:]), true
}

// FormatInches writes an inch value as the shortest decimal that reads
// back to the same double, with no exponent:
// strconv.FormatFloat(f, 'f', -1, 64), with "-0" written "0". Visio
// writes up to 17 significant digits (0.01041666666666667), so no digit
// cap applies. Used by --units in and by P2 writes. Chunk V1.
func FormatInches(f float64) string {
	s := strconv.FormatFloat(f, 'f', -1, 64)
	if s == "-0" {
		return "0"
	}
	return s
}

// Degrees converts a stored angle (radians, counter-clockwise, y up) to
// the view's degrees clockwise on a y-down page in [0, 360). Chunk V1.
func Degrees(rad float64) float64 {
	return geom.NormDeg(-rad * 180 / math.Pi)
}

// TextBackground interprets a stored TextBkgnd value: a number below 1
// is no background (none), #RRGGBB is that colour (hex), and a number
// n of 1 or more is the colour of index n-1 (index; Visio stores the
// index plus one so that 0 is transparent, as libvisio reads it). ok
// is false for anything else, Themed included.
func TextBackground(v string) (none bool, hex string, index int, ok bool) {
	if h, isHex := Color(v); isHex {
		return false, h, -1, true
	}
	n, isNum := Number(v)
	if !isNum {
		return false, "", -1, false
	}
	if n < 1 {
		return true, "", -1, true
	}
	if n > 255 {
		return false, "", -1, false
	}
	return false, "", int(n) - 1, true
}
