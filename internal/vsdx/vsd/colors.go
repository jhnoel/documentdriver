package vsd

// BuiltinColor returns the colour of Visio's built-in palette entry ix
// (0 to 23) as "#RRGGBB". These entries are never stored in a file: a
// document's Colors element starts at IX 24 (every corpus document that
// has one, 54 of 57). The table is designer-owned with its provenance
// in a comment (docs/vsdx-design.md, decision f; chunk V3). ok is false
// outside 0 to 23.
func BuiltinColor(ix int) (hex string, ok bool) {
	_ = ix
	return "", false
}
