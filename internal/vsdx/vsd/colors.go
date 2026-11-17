package vsd

// builtinColors is Visio's logical palette, indexes 0 to 23, which a
// file never stores: a document's Colors element starts at IX 24 (every
// corpus document that has one, 54 of 57).
//
// Provenance (designer, 2026-11-17):
//   - libvisio, src/lib/VSDXMLParserBase.cpp, initColours(): the same 24
//     values in this order; readColours() then overrides entries from the
//     file's ColorEntry elements, and readExtendedColourData() resolves a
//     cell's integer value through that table
//     (https://github.com/LibreOffice/libvisio).
//   - Microsoft, Visio 2003 SDK, "Colors Element": the example color
//     table lists the same values at IX 0 to 23
//     (https://msdn.microsoft.com/en-us/library/aa197562); Microsoft,
//     "Managing Color in Styles, Shapes, and Files": "the first 24
//     entries in the color table are reserved for the logical color
//     palette", 0 black, 1 white, 2 red
//     (https://learn.microsoft.com/en-us/previous-versions/office/developer/office-xp/aa200996(v=office.10)).
//
// Not covered: TextBkgnd stores index + 1 (0 is no background; libvisio
// looks up TextBkgnd - 1), so a TextBkgnd value is not passed here
// unadjusted.
var builtinColors = [24]string{
	"#000000", "#FFFFFF", "#FF0000", "#00FF00", "#0000FF", "#FFFF00",
	"#FF00FF", "#00FFFF", "#800000", "#008000", "#000080", "#808000",
	"#800080", "#008080", "#C0C0C0", "#E6E6E6", "#CDCDCD", "#B3B3B3",
	"#9A9A9A", "#808080", "#666666", "#4D4D4D", "#333333", "#1A1A1A",
}

// BuiltinColor returns the colour of Visio's built-in palette entry ix
// (0 to 23) as "#RRGGBB" (provenance on builtinColors). ok is false
// outside 0 to 23. A document's Colors entry for the same index (never
// written by Visio below 24) takes precedence: doc.Document.Color.
func BuiltinColor(ix int) (hex string, ok bool) {
	if ix < 0 || ix >= len(builtinColors) {
		return "", false
	}
	return builtinColors[ix], true
}
