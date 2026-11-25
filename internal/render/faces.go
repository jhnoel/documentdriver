package render

import (
	"sync"

	"github.com/jhnoel/documentdriver/internal/render/fonts"
	"github.com/jhnoel/documentdriver/internal/render/ttf"
)

// faceFiles are the embedded fonts by family (metricClass) and style
// (bold 1, italic 2).
var faceFiles = [3][4]string{
	classCalibri: {"Carlito-Regular.ttf", "Carlito-Bold.ttf", "Carlito-Italic.ttf", "Carlito-BoldItalic.ttf"},
	classSans:    {"LiberationSans-Regular.ttf", "LiberationSans-Bold.ttf", "LiberationSans-Italic.ttf", "LiberationSans-BoldItalic.ttf"},
	classSerif:   {"LiberationSerif-Regular.ttf", "LiberationSerif-Bold.ttf", "LiberationSerif-Italic.ttf", "LiberationSerif-BoldItalic.ttf"},
}

// fallbackFile is the font for characters the families lack.
const fallbackFile = "DejaVuSans.ttf"

// faceSet is the parsed embedded fonts.
type faceSet struct {
	fonts    [3][4]*ttf.Font
	fallback *ttf.Font
}

// loadFaces parses the embedded fonts' headers once, on first use; the
// set never changes after. A font that does not parse is left nil (the
// embedded files are checked by TestEmbeddedFonts).
var loadFaces = sync.OnceValue(func() *faceSet {
	var fs faceSet
	parse := func(name string) *ttf.Font {
		b, err := fonts.Bytes(name)
		if err != nil {
			return nil
		}
		f, err := ttf.Parse(b)
		if err != nil {
			return nil
		}
		return f
	}
	for c := range faceFiles {
		for st, name := range faceFiles[c] {
			fs.fonts[c][st] = parse(name)
		}
	}
	fs.fallback = parse(fallbackFile)
	return &fs
})

// style is the index of a weight and slant in faceFiles.
func style(bold, italic bool) int {
	s := 0
	if bold {
		s |= 1
	}
	if italic {
		s |= 2
	}
	return s
}

// glyphFor returns the font and glyph that draw r in a family, weight
// and slant: the face's own font when it has r, else the other
// families in the same style (Liberation Sans, Liberation Serif,
// Carlito), else DejaVu Sans; nil when none has it. Control characters
// and other characters with no glyph anywhere return nil.
func glyphFor(c metricClass, bold, italic bool, r rune) (*ttf.Font, int) {
	fs := loadFaces()
	st := style(bold, italic)
	try := func(f *ttf.Font) (*ttf.Font, int, bool) {
		if f == nil {
			return nil, 0, false
		}
		if g := f.Index(r); g != 0 {
			return f, g, true
		}
		return nil, 0, false
	}
	if f, g, ok := try(fs.fonts[c][st]); ok {
		return f, g
	}
	for _, o := range [...]metricClass{classSans, classSerif, classCalibri} {
		if o == c {
			continue
		}
		if f, g, ok := try(fs.fonts[o][st]); ok {
			return f, g
		}
	}
	if f, g, ok := try(fs.fallback); ok {
		return f, g
	}
	return nil, 0
}

// HasGlyph reports whether some embedded font draws r for a face,
// weight and slant (glyphFor).
func HasGlyph(face string, bold, italic bool, r rune) bool {
	f, _ := glyphFor(faceClass(face), bold, italic, r)
	return f != nil
}
