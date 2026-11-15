package html

import (
	"fmt"

	"github.com/jhnoel/documentdriver/internal/ascii"
)

// This file holds the plain-data types of docxdriver's api.rs that the
// input path uses. MOVE AT P3/P4 INTEGRATION: they belong with the typed
// request API (the docx plan model) once that package exists; html should
// then import them from there.

// HighlightColor is the ECMA-376 ST_HighlightColor palette; HighlightNone
// is an explicit off override.
type HighlightColor uint8

// The highlight palette, in api.rs order.
const (
	HighlightBlack HighlightColor = iota
	HighlightBlue
	HighlightCyan
	HighlightGreen
	HighlightMagenta
	HighlightRed
	HighlightYellow
	HighlightWhite
	HighlightDarkBlue
	HighlightDarkCyan
	HighlightDarkGreen
	HighlightDarkMagenta
	HighlightDarkRed
	HighlightDarkYellow
	HighlightDarkGray
	HighlightLightGray
	HighlightNone
)

var highlightNames = [...]string{
	"black", "blue", "cyan", "green", "magenta", "red", "yellow", "white", "darkBlue",
	"darkCyan", "darkGreen", "darkMagenta", "darkRed", "darkYellow", "darkGray", "lightGray", "none",
}

var highlightCSS = [...]string{
	"#000000", "#0000FF", "#00FFFF", "#00FF00", "#FF00FF", "#FF0000", "#FFFF00", "#FFFFFF", "#000080",
	"#008080", "#008000", "#800080", "#800000", "#808000", "#808080", "#C0C0C0", "transparent",
}

// String is the w:highlight value (api.rs as_str).
func (h HighlightColor) String() string { return highlightNames[h] }

// CSS is the CSS color reads write for the highlight.
func (h HighlightColor) CSS() string { return highlightCSS[h] }

// ParseHighlightColor parses a palette name such as "darkBlue".
func ParseHighlightColor(value string) (HighlightColor, error) {
	for i, n := range highlightNames {
		if n == value {
			return HighlightColor(i), nil
		}
	}
	return 0, fmt.Errorf("unknown highlight palette value %s", rustDebugString(value))
}

// HighlightFromCSS returns the palette entry whose CSS color is value,
// compared ASCII case-insensitively, as reads write it.
func HighlightFromCSS(value string) (HighlightColor, error) {
	for i, c := range highlightCSS {
		if ascii.EqualFold(c, value) {
			return HighlightColor(i), nil
		}
	}
	return 0, fmt.Errorf("background-color %s is not a Word highlight color; use one of the highlight palette's colors", rustDebugString(value))
}
