package vsd

// PointsPerInch converts the stored inches to the points the views show
// by default (exact: 1 in = 72 pt).
const PointsPerInch = 72

// Number parses a stored V that holds a number ("2.5", "-1E-3",
// "1.#INF" is refused): ok is false for anything that is not a finite
// decimal number. Chunk V1.
func Number(v string) (f float64, ok bool) {
	_ = v
	return 0, false
}

// Bool parses a stored boolean V ("0", "1", "FALSE", "TRUE" as Visio
// writes them; format notes section 2): ok is false for anything else.
// Chunk V1.
func Bool(v string) (b, ok bool) {
	_ = v
	return false, false
}

// Color parses a stored colour V: "#RRGGBB" gives the upper-case hex
// form "#RRGGBB"; an index into the document's Colors table, "themed"
// and anything else give ok false (the caller resolves indexes, chunk
// D3). Chunk V1.
func Color(v string) (hex string, ok bool) {
	_ = v
	return "", false
}

// FormatInches writes an inch value as Visio stores numbers: the
// shortest decimal that reads back to the same double, at most 15
// significant digits, no exponent. Used by --units in and by P2
// writes. Chunk V1.
func FormatInches(f float64) string {
	_ = f
	return ""
}

// Degrees converts a stored angle (radians, counter-clockwise, y up) to
// the view's degrees clockwise on a y-down page in [0, 360). Chunk V1.
func Degrees(rad float64) float64 {
	_ = rad
	return 0
}
