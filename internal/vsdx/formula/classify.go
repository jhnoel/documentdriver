package formula

import (
	"math"

	"github.com/jhnoel/documentdriver/internal/ascii"
)

// Allowed reports whether f is on the allow list of decision (c),
// rule 2 (docs/vsdx-design.md): absent (""), Inh, No Formula, a
// constant (one number with an optional unit of the closed list
// below), THEMEVAL() or THEMEGUARD(...). Anything else (GUARD(...),
// a reference, an expression, a string, an unknown unit) is refused.
// Names and units match case-insensitively, as Visio reads them, and
// spaces between tokens do not matter.
func Allowed(f string) bool {
	toks := Lex(f)
	i := skipSpaces(toks, 0)
	if i == len(toks) {
		return true
	}
	t := toks[i]
	switch t.Kind {
	case Ref:
		if ascii.EqualFold(t.Text, "Inh") {
			return skipSpaces(toks, i+1) == len(toks)
		}
		if ascii.EqualFold(t.Text, "No") {
			j := skipSpaces(toks, i+1)
			return j < len(toks) && toks[j].Kind == Ref &&
				ascii.EqualFold(toks[j].Text, "Formula") &&
				skipSpaces(toks, j+1) == len(toks)
		}
	case Number:
		_, ok := constantValue(toks)
		return ok
	case Op:
		if t.Text == "+" || t.Text == "-" {
			_, ok := constantValue(toks)
			return ok
		}
	case Func:
		if ascii.EqualFold(t.Text, "THEMEVAL") {
			j := skipSpaces(toks, i+1)
			if j >= len(toks) || toks[j].Kind != LParen {
				return false
			}
			j = skipSpaces(toks, j+1)
			return j < len(toks) && toks[j].Kind == RParen &&
				skipSpaces(toks, j+1) == len(toks)
		}
		if ascii.EqualFold(t.Text, "THEMEGUARD") {
			return themeguard(toks, i)
		}
	}
	return false
}

// Evaluable reports whether f is a formula a write may recompute:
// on the allow list (Allowed), or in the closed evaluator grammar of
// Eval (numbers, + - * / and ^ with a constant exponent, unary minus,
// parentheses, same-shape references including BeginX, BeginY, EndX
// and EndY, and GUARD, SQRT and ATAN2) without reading a User cell
// (User.DefaultWidth in decision (g)). A write that changes a cell
// such a formula reads rewrites its cached value and keeps the
// formula; a write never replaces such a formula with a value of its
// own (Allowed is that list). A lone reference (Width, a dependent
// copy) and a lone number Constant rejects are not evaluable.
// Chunk CC1, split from Allowed by the gate ruling of 2026-11-17.
func Evaluable(f string) bool {
	if Allowed(f) {
		return true
	}
	toks := Lex(f)
	i := skipSpaces(toks, 0)
	if i < len(toks) {
		t := toks[i]
		if (t.Kind == Ref || t.Kind == Number) && skipSpaces(toks, i+1) == len(toks) {
			return false
		}
		if t.Kind == Op && (t.Text == "+" || t.Text == "-") {
			j := skipSpaces(toks, i+1)
			if j < len(toks) && toks[j].Kind == Number && skipSpaces(toks, j+1) == len(toks) {
				return false
			}
		}
	}
	if !inGrammar(f) {
		return false
	}
	for _, ref := range Refs(f) {
		if len(ref) >= 5 && ascii.EqualFold(ref[:5], "User.") {
			return false
		}
	}
	return true
}

// inGrammar reports whether f is in the closed evaluator grammar
// of Eval, syntactically: Eval with a dummy cell does not report
// ErrOutsideGrammar. It is the grammar check Evaluable and the
// corpus sweep share, so they cannot drift apart.
func inGrammar(f string) bool {
	p := &parser{
		toks:    Lex(f),
		cell:    func(string) (float64, bool) { return 0, true },
		syntact: true,
	}
	_, err := p.parse()
	return err == nil
}

// Constant returns the internal value of f when f is a constant (one
// number with an optional unit of the closed list): inches for a
// length, radians for an angle. ok is false for anything that is not
// a constant, for an unknown unit (1XX), and when the converted
// value is not finite.
func Constant(f string) (float64, bool) {
	return constantValue(Lex(f))
}

// constantValue returns the internal value of the tokens as a
// constant: an optional + or - sign and one Number token, and
// nothing else.
func constantValue(toks []Token) (float64, bool) {
	i := skipSpaces(toks, 0)
	neg := false
	if i < len(toks) && toks[i].Kind == Op {
		if toks[i].Text != "+" && toks[i].Text != "-" {
			return 0, false
		}
		neg = toks[i].Text == "-"
		i = skipSpaces(toks, i+1)
	}
	if i >= len(toks) || toks[i].Kind != Number {
		return 0, false
	}
	if skipSpaces(toks, i+1) != len(toks) {
		return 0, false
	}
	v, unit, ok := toks[i].NumberValue()
	if !ok {
		return 0, false
	}
	v, ok = convertUnit(v, unit)
	if !ok || math.IsNaN(v) || math.IsInf(v, 0) {
		return 0, false
	}
	if neg {
		v = -v
	}
	return v, true
}

// convertUnit converts v in the given unit to internal units (inches
// for a length, radians for an angle): the closed list of decision
// (c) (IN, FT, MM, CM, M, PT, DEG, RAD, and none) plus the implicit
// units DL, DP, DT (drawing, page and text lengths, already in
// internal inches) and DA (angle, already in internal radians).
// Division by the exact per-inch count keeps 25.4MM exactly 1.
func convertUnit(v float64, unit string) (float64, bool) {
	switch {
	case unit == "" || ascii.EqualFold(unit, "IN") ||
		ascii.EqualFold(unit, "DL") || ascii.EqualFold(unit, "DP") ||
		ascii.EqualFold(unit, "DT") || ascii.EqualFold(unit, "RAD") ||
		ascii.EqualFold(unit, "DA"):
		return v, true
	case ascii.EqualFold(unit, "FT"):
		return v * 12, true
	case ascii.EqualFold(unit, "MM"):
		return v / 25.4, true
	case ascii.EqualFold(unit, "CM"):
		return v / 2.54, true
	case ascii.EqualFold(unit, "M"):
		return v / 0.0254, true
	case ascii.EqualFold(unit, "PT"):
		return v / 72, true
	case ascii.EqualFold(unit, "DEG"):
		return v * math.Pi / 180, true
	}
	return 0, false
}

// themeguard reports whether the Func token at toks[i] opens a
// THEMEGUARD(...) call that spans the rest of the tokens: balanced
// parentheses, no Invalid token, and the closing ")" is the last
// significant token.
func themeguard(toks []Token, i int) bool {
	j := skipSpaces(toks, i+1)
	if j >= len(toks) || toks[j].Kind != LParen {
		return false
	}
	depth := 0
	for k := j; k < len(toks); k++ {
		switch toks[k].Kind {
		case Invalid:
			return false
		case LParen:
			depth++
		case RParen:
			depth--
			if depth == 0 {
				return skipSpaces(toks, k+1) == len(toks)
			}
			if depth < 0 {
				return false
			}
		}
	}
	return false
}

// skipSpaces returns the index of the first token at or after i that
// is not a Space token, or len(toks) when there is none.
func skipSpaces(toks []Token, i int) int {
	for i < len(toks) && toks[i].Kind == Space {
		i++
	}
	return i
}
