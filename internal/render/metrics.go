package render

import (
	"strings"

	"github.com/jhnoel/documentdriver/internal/ascii"
)

// metricClass is which embedded family draws a face.
type metricClass uint8

// The families: Calibri-like (Carlito), Arial-like (Liberation Sans)
// and Times-like (Liberation Serif).
const (
	classCalibri metricClass = iota
	classSans
	classSerif
)

// faceClass returns the embedded family that draws a face name: Calibri and
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
// then the metric-compatible open face the renderer measures with,
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

// RuneWidth returns the advance of r in ems in a face, weight and
// slant: the advance of the glyph the renderer draws for it
// (glyphFor: the face's font, else the fallback by coverage), a tab
// four spaces, and 0 for a character no embedded font has.
func RuneWidth(face string, bold, italic bool, r rune) float64 {
	if r == '\t' {
		return 4 * RuneWidth(face, bold, italic, ' ')
	}
	f, g := glyphFor(faceClass(face), bold, italic, r)
	if f == nil {
		return 0
	}
	return float64(f.Advance(g)) / float64(f.UnitsPerEm())
}

// TextWidth returns the width of s in points at size points.
func TextWidth(face string, bold, italic bool, size float64, s string) float64 {
	w := 0.0
	for _, r := range s {
		w += RuneWidth(face, bold, italic, r)
	}
	return w * size
}
