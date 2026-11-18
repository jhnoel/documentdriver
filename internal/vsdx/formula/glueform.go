package formula

import (
	"strconv"

	"github.com/jhnoel/documentdriver/internal/ascii"
)

// GlueKind is the kind of a glue formula: a point glue, a whole-shape
// glue or a transform trigger.
type GlueKind uint8

// The glue formula kinds.
const (
	// GluePoint is PAR(PNT(Sheet.N!Connections.Xk,Sheet.N!Connections.Yk)):
	// the end is glued to connection point k of sheet N.
	GluePoint GlueKind = iota
	// GlueWhole is _WALKGLUE(BegTrigger,EndTrigger,WalkPreference) or
	// its EndTrigger-first spelling: the end is glued to the whole
	// target shape.
	GlueWhole
	// GlueTrigger is _XFTRIGGER(Sheet.N!EventXFMod) or
	// _XFTRIGGER(EventXFMod): a BegTrigger or EndTrigger cell, for
	// sheet N or for the shape itself (sheet 0).
	GlueTrigger
)

// String names the glue kind.
func (k GlueKind) String() string {
	switch k {
	case GluePoint:
		return "point"
	case GlueWhole:
		return "whole"
	case GlueTrigger:
		return "trigger"
	}
	return "glue?"
}

// Glue is a glue formula decoded: its kind, the sheet it names (0
// when it names none or the shape itself) and, for a point glue, the
// 1-based connection point.
type Glue struct {
	Kind  GlueKind
	Sheet uint32
	Point int
}

// GlueForm decodes f as a glue formula: PAR(PNT(Sheet.N!Connections.Xk,
// Sheet.N!Connections.Yk)) is GluePoint{Sheet: N, Point: k},
// _WALKGLUE(BegTrigger,EndTrigger,WalkPreference) (in either trigger
// order) is GlueWhole, and _XFTRIGGER(Sheet.N!EventXFMod) is
// GlueTrigger{Sheet: N} while _XFTRIGGER(EventXFMod) is
// GlueTrigger{Sheet: 0}. Anything else, including PNT(1,2) and
// PAR(PNT(Sheet.N!Width,1)), is not a glue form: ok is false. Names
// match case-insensitively, as Visio reads them, and spaces between
// tokens do not matter. Glue forms are never evaluated: only the
// glue writes replace them. It is a single forward pass over the
// tokens, allocating in proportion to the input and never panicking.
func GlueForm(f string) (Glue, bool) {
	if g, ok := gluePoint(f); ok {
		return g, true
	}
	if g, ok := glueWhole(f); ok {
		return g, true
	}
	return glueTrigger(f)
}

// gluePoint decodes a PAR(PNT(...)) point glue, requiring the two
// Sheet.N!Connections refs to name the same sheet and point, with X
// first and Y second.
func gluePoint(f string) (Glue, bool) {
	toks := Lex(f)
	i := skipSpaces(toks, 0)
	if !funcName(toks, &i, "PAR") || !expect(toks, &i, LParen) ||
		!funcName(toks, &i, "PNT") || !expect(toks, &i, LParen) {
		return Glue{}, false
	}
	xs, xok := parseConnectionRef(toks, &i, "X")
	if !xok || !expect(toks, &i, Comma) {
		return Glue{}, false
	}
	ys, yok := parseConnectionRef(toks, &i, "Y")
	if !yok || !expect(toks, &i, RParen) || !expect(toks, &i, RParen) {
		return Glue{}, false
	}
	if skipSpaces(toks, i) != len(toks) || xs != ys {
		return Glue{}, false
	}
	u, err := strconv.ParseUint(xs.sheet, 10, 32)
	if err != nil {
		return Glue{}, false
	}
	pt, err := strconv.Atoi(xs.point)
	if err != nil || pt < 0 {
		return Glue{}, false
	}
	return Glue{Kind: GluePoint, Sheet: uint32(u), Point: pt}, true
}

// glueWhole decodes a _WALKGLUE whole-shape glue: BegTrigger and
// EndTrigger in either order, then WalkPreference.
func glueWhole(f string) (Glue, bool) {
	toks := Lex(f)
	i := skipSpaces(toks, 0)
	if !funcName(toks, &i, "_WALKGLUE") || !expect(toks, &i, LParen) {
		return Glue{}, false
	}
	first, ok := refName(toks, &i)
	if !ok || !expect(toks, &i, Comma) {
		return Glue{}, false
	}
	second, ok := refName(toks, &i)
	if !ok || !expect(toks, &i, Comma) {
		return Glue{}, false
	}
	third, ok := refName(toks, &i)
	if !ok || !expect(toks, &i, RParen) || skipSpaces(toks, i) != len(toks) {
		return Glue{}, false
	}
	if !ascii.EqualFold(third, "WalkPreference") {
		return Glue{}, false
	}
	begEnd := (ascii.EqualFold(first, "BegTrigger") && ascii.EqualFold(second, "EndTrigger")) ||
		(ascii.EqualFold(first, "EndTrigger") && ascii.EqualFold(second, "BegTrigger"))
	if !begEnd {
		return Glue{}, false
	}
	return Glue{Kind: GlueWhole}, true
}

// glueTrigger decodes an _XFTRIGGER trigger: EventXFMod for the shape
// itself, or Sheet.N!EventXFMod for sheet N.
func glueTrigger(f string) (Glue, bool) {
	toks := Lex(f)
	i := skipSpaces(toks, 0)
	if !funcName(toks, &i, "_XFTRIGGER") || !expect(toks, &i, LParen) {
		return Glue{}, false
	}
	ref, ok := refName(toks, &i)
	if !ok || !expect(toks, &i, RParen) || skipSpaces(toks, i) != len(toks) {
		return Glue{}, false
	}
	if ascii.EqualFold(ref, "EventXFMod") {
		return Glue{Kind: GlueTrigger}, true
	}
	sheet, cell, ok := sheetRef(ref)
	if !ok || !ascii.EqualFold(cell, "EventXFMod") {
		return Glue{}, false
	}
	u, err := strconv.ParseUint(sheet, 10, 32)
	if err != nil {
		return Glue{}, false
	}
	return Glue{Kind: GlueTrigger, Sheet: uint32(u)}, true
}

// connectionRef is a decoded Sheet.N!Connections.Xk or Yk reference.
type connectionRef struct {
	sheet string
	point string
}

// parseConnectionRef consumes a Sheet.N!Connections.Xk (axis X) or Yk
// (axis Y) reference at *i.
func parseConnectionRef(toks []Token, i *int, axis string) (connectionRef, bool) {
	ref, ok := refName(toks, i)
	if !ok {
		return connectionRef{}, false
	}
	sheet, cell, ok := sheetRef(ref)
	if !ok {
		return connectionRef{}, false
	}
	prefix := "Connections." + axis
	if len(cell) <= len(prefix) || !ascii.EqualFold(cell[:len(prefix)], prefix) {
		return connectionRef{}, false
	}
	point := cell[len(prefix):]
	for j := 0; j < len(point); j++ {
		if point[j] < '0' || point[j] > '9' {
			return connectionRef{}, false
		}
	}
	return connectionRef{sheet: sheet, point: point}, true
}

// sheetRef splits a Sheet.N!cell reference into its sheet digits and
// cell name.
func sheetRef(ref string) (string, string, bool) {
	bang := -1
	for j := 0; j < len(ref); j++ {
		if ref[j] == '!' {
			bang = j
			break
		}
	}
	if bang < 0 {
		return "", "", false
	}
	head, cell := ref[:bang], ref[bang+1:]
	if len(head) < 7 || !ascii.EqualFold(head[:5], "Sheet") || head[5] != '.' || cell == "" {
		return "", "", false
	}
	sheet := head[6:]
	for j := 0; j < len(sheet); j++ {
		if sheet[j] < '0' || sheet[j] > '9' {
			return "", "", false
		}
	}
	if sheet == "" {
		return "", "", false
	}
	return sheet, cell, true
}

// funcName reports whether the next significant token is the Func
// name want, consuming it and the index past it if so.
func funcName(toks []Token, i *int, want string) bool {
	j := skipSpaces(toks, *i)
	if j >= len(toks) || toks[j].Kind != Func || !ascii.EqualFold(toks[j].Text, want) {
		return false
	}
	*i = j + 1
	return true
}

// expect reports whether the next significant token is kind, consuming
// it and the index past it if so.
func expect(toks []Token, i *int, kind Kind) bool {
	j := skipSpaces(toks, *i)
	if j >= len(toks) || toks[j].Kind != kind {
		return false
	}
	*i = j + 1
	return true
}

// refName returns the next significant token when it is a Ref,
// consuming it and the index past it, and false otherwise.
func refName(toks []Token, i *int) (string, bool) {
	j := skipSpaces(toks, *i)
	if j >= len(toks) || toks[j].Kind != Ref {
		return "", false
	}
	*i = j + 1
	return toks[j].Text, true
}
