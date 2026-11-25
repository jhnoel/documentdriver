package render

import (
	"strings"

	"github.com/jhnoel/documentdriver/internal/ascii"
)

// metricClass is which width table measures a face.
type metricClass uint8

// The width tables: Calibri-like, Arial-like and Times-like.
const (
	classCalibri metricClass = iota
	classSans
	classSerif
)

// faceClass returns the width table for a face name: Calibri and
// Carlito (and an unnamed face, Visio's default theme font being
// Calibri) measure as Carlito; Times, Times New Roman, Georgia,
// Cambria, Liberation Serif and Garamond-like serif faces as
// Liberation Serif; every other face as Liberation Sans.
// A CSS family list (Family's result) is classed by its first name.
func faceClass(face string) metricClass {
	first, _, _ := strings.Cut(face, ",")
	f := ascii.Lower(strings.Trim(strings.TrimSpace(first), "'\""))
	switch {
	case f == "", f == "calibri", f == "carlito", strings.HasPrefix(f, "calibri "):
		return classCalibri
	case strings.Contains(f, "times"), strings.Contains(f, "serif") && !strings.Contains(f, "sans"),
		f == "georgia", f == "cambria", f == "caladea", strings.Contains(f, "garamond"), f == "book antiqua":
		return classSerif
	}
	return classSans
}

// Family returns the CSS font-family list for a face: the face itself,
// then the metric-compatible open face the width tables come from,
// then the generic family, so a viewer without the face draws text of
// the width the layout assumed.
func Family(face string) string {
	face = strings.TrimSpace(face)
	quoted := func(s string) string {
		if strings.ContainsAny(s, " ,'\"") {
			return "'" + strings.NewReplacer("'", "", "\"", "", ",", "").Replace(s) + "'"
		}
		return s
	}
	switch faceClass(face) {
	case classCalibri:
		if face == "" || ascii.EqualFold(face, "calibri") {
			return "Calibri, Carlito, sans-serif"
		}
		return quoted(face) + ", Calibri, Carlito, sans-serif"
	case classSerif:
		return quoted(face) + ", 'Times New Roman', 'Liberation Serif', serif"
	}
	if ascii.EqualFold(face, "arial") {
		return "Arial, 'Liberation Sans', Helvetica, sans-serif"
	}
	return quoted(face) + ", Arial, 'Liberation Sans', sans-serif"
}

// table returns the width table of a class and weight.
func table(c metricClass, bold bool) *[95]uint16 {
	switch {
	case c == classCalibri && bold:
		return &carlitoBold
	case c == classCalibri:
		return &carlitoRegular
	case c == classSerif && bold:
		return &serifBold
	case c == classSerif:
		return &serifRegular
	case bold:
		return &sansBold
	}
	return &sansRegular
}

// RuneWidth returns the advance of r in ems for a face: the table's
// width for printable ASCII, a full em for CJK and other wide
// characters, the width of "n" for any other character (an
// approximation: the tables hold ASCII only).
func RuneWidth(face string, bold bool, r rune) float64 {
	t := table(faceClass(face), bold)
	switch {
	case r >= ' ' && r <= '~':
		return float64(t[r-' ']) / 1000
	case r == '\t':
		return 4 * float64(t[0]) / 1000
	case r >= 0x1100 && (r <= 0x115F || (r >= 0x2E80 && r <= 0xA4CF) || (r >= 0xAC00 && r <= 0xD7A3) ||
		(r >= 0xF900 && r <= 0xFAFF) || (r >= 0xFF00 && r <= 0xFF60)):
		return 1
	}
	return float64(t['n'-' ']) / 1000
}

// TextWidth returns the width of s in points at size points.
func TextWidth(face string, bold bool, size float64, s string) float64 {
	w := 0.0
	for _, r := range s {
		w += RuneWidth(face, bold, r)
	}
	return w * size
}
