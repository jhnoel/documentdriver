package formula

import (
	"errors"
	"fmt"
	"math"
	"strings"

	"github.com/jhnoel/documentdriver/internal/ascii"
)

// ErrOutsideGrammar reports that a formula is outside the closed
// grammar of decision (c) (docs/vsdx-design.md): anything other than
// numbers with the closed unit list (IN, FT, MM, CM, M, PT, DEG, RAD,
// the implicit DL, DP, DT and DA, and none), the operators + - * / and
// ^ with a constant exponent, unary minus, parentheses, same-shape
// references, and the functions GUARD(x), SQRT(x) and ATAN2(y,x).
// The error names the token that puts it outside.
var ErrOutsideGrammar = errors.New("formula outside closed grammar")

// ErrUnknownCell reports that a formula in the closed grammar names
// a same-shape reference the cell callback does not know. The error
// names the reference.
var ErrUnknownCell = errors.New("formula cell unknown")

// maxEvalDepth is the nesting limit of the closed evaluator: 64
// nested parentheses, unary minuses or function calls evaluate, the
// 65th is outside the grammar. A flat chain of operators is
// iterative and does not count towards it.
const maxEvalDepth = 64

// Eval evaluates f in the closed grammar of decision (c): numbers
// with units from the closed list, + - * /, ^ with a constant
// exponent, unary minus, parentheses, same-shape references (including
// the 1-D names BeginX, BeginY, EndX and EndY), and GUARD(x) (the
// value of x), SQRT(x) and ATAN2(y,x). A reference is looked up through cell
// with the reference as stored: the callback decides, case
// insensitively as Visio reads names, whether it knows it. Units
// convert to internal units with the exact factors of Constant:
// inches for a length, radians for an angle. Nesting deeper than 64
// is an error. Anything outside the grammar is ErrOutsideGrammar
// naming the token; a reference cell does not know is ErrUnknownCell
// naming the reference; a non-finite intermediate or cell value is a
// plain error. It never panics, including for a nil cell when f
// names no reference.
func Eval(f string, cell func(ref string) (float64, bool)) (float64, error) {
	p := &parser{toks: Lex(f), cell: cell}
	return p.parse()
}

// Refs returns the same-shape references of f in the order they
// occur, duplicates included, when f is in the closed grammar: the
// references a dependency closure walks, including those inside
// GUARD, SQRT and ATAN2 and the base of ^. It returns nil when f is
// outside the grammar, including when it names a Sheet.N! or
// ThePage! reference, a function other than GUARD, SQRT and ATAN2,
// or a ^ with a non-constant exponent, and nil when f names none.
func Refs(f string) []string {
	p := &parser{
		toks:     Lex(f),
		cell:     func(string) (float64, bool) { return 0, true },
		syntact:  true,
		wantRefs: true,
	}
	if _, err := p.parse(); err != nil {
		return nil
	}
	return p.refs
}

// parser is a recursive-descent parser over the tokens of one
// formula: expr over term over power over factor over primary. The
// + -, * /, and ^ loops are iterative, so a flat formula of any
// length recurses at most a fixed depth; only parentheses, unary
// minuses and function calls recurse, and depth bounds all three.
// It evaluates as it parses, so it holds no tree and copies no token
// text.
type parser struct {
	// toks are the tokens Lex(f) produced for the formula.
	toks []Token
	// pos is the index of the next token to consider; peek advances
	// it past Space tokens.
	pos int
	// cell looks up a same-shape reference; nil knows none.
	cell func(ref string) (float64, bool)
	// syntact validates the grammar only, for Refs: cell supplies a
	// dummy value and non-finite results do not fail.
	syntact bool
	// wantRefs collects the references in order of occurrence.
	wantRefs bool
	// refs are the references collected, in order of occurrence.
	refs []string
	// depth is the current parenthesis, unary-minus and call nesting.
	depth int
	// refCount counts the references parsed so far, so power can tell
	// whether an exponent named one.
	refCount int
}

// parse parses a whole formula, refusing any token left over.
func (p *parser) parse() (float64, error) {
	v, err := p.expr()
	if err != nil {
		return 0, err
	}
	if tok, ok := p.peek(); ok {
		return 0, outsideGrammar(tok.Text)
	}
	return v, nil
}

// expr parses terms joined by + or -.
func (p *parser) expr() (float64, error) {
	left, err := p.term()
	if err != nil {
		return 0, err
	}
	for {
		tok, ok := p.peek()
		if !ok || tok.Kind != Op || (tok.Text != "+" && tok.Text != "-") {
			return left, nil
		}
		p.consume()
		right, err := p.term()
		if err != nil {
			return 0, err
		}
		if tok.Text == "+" {
			left += right
		} else {
			left -= right
		}
		if left, err = p.finite(left); err != nil {
			return 0, err
		}
	}
}

// term parses powers joined by * or /.
func (p *parser) term() (float64, error) {
	left, err := p.power()
	if err != nil {
		return 0, err
	}
	for {
		tok, ok := p.peek()
		if !ok || tok.Kind != Op || (tok.Text != "*" && tok.Text != "/") {
			return left, nil
		}
		p.consume()
		right, err := p.power()
		if err != nil {
			return 0, err
		}
		if tok.Text == "*" {
			left *= right
		} else {
			left /= right
		}
		if left, err = p.finite(left); err != nil {
			return 0, err
		}
	}
}

// power parses a factor raised to a constant exponent by ^: the
// exponent is a factor that names no reference, as the corpus's
// ^2 does. A chain a^2^3 reads as (a^2)^3. The ^ loop is iterative.
func (p *parser) power() (float64, error) {
	left, err := p.factor()
	if err != nil {
		return 0, err
	}
	for {
		tok, ok := p.peek()
		if !ok || tok.Kind != Op || tok.Text != "^" {
			return left, nil
		}
		p.consume()
		start := p.pos
		refs := p.refCount
		exp, err := p.factor()
		if err != nil {
			return 0, err
		}
		if p.refCount != refs {
			for i := start; i < p.pos && i < len(p.toks); i++ {
				if p.toks[i].Kind == Ref {
					return 0, outsideGrammar(p.toks[i].Text)
				}
			}
			return 0, outsideGrammar(tok.Text)
		}
		left = math.Pow(left, exp)
		if left, err = p.finite(left); err != nil {
			return 0, err
		}
	}
}

// factor parses an optional unary minus before a primary. A unary
// plus is not in the grammar.
func (p *parser) factor() (float64, error) {
	tok, ok := p.peek()
	if ok && tok.Kind == Op && tok.Text == "-" {
		p.consume()
		p.depth++
		if p.depth > maxEvalDepth {
			return 0, outsideGrammar(tok.Text)
		}
		v, err := p.factor()
		p.depth--
		if err != nil {
			return 0, err
		}
		return p.finite(-v)
	}
	if ok && tok.Kind == Op && tok.Text == "+" {
		return 0, outsideGrammar(tok.Text)
	}
	return p.primary()
}

// primary parses a number, a same-shape reference, a parenthesised
// expression or a GUARD, SQRT or ATAN2 call.
func (p *parser) primary() (float64, error) {
	tok, ok := p.peek()
	if !ok {
		return 0, outsideGrammar("")
	}
	switch tok.Kind {
	case Func:
		return p.call(tok)
	case Number:
		p.consume()
		v, unit, ok := tok.NumberValue()
		if !ok {
			return 0, outsideGrammar(tok.Text)
		}
		if v, ok = convertUnit(v, unit); !ok {
			return 0, outsideGrammar(tok.Text)
		}
		return p.finite(v)
	case Ref:
		if strings.Contains(tok.Text, "!") {
			return 0, outsideGrammar(tok.Text)
		}
		p.consume()
		p.refCount++
		if p.wantRefs {
			p.refs = append(p.refs, tok.Text)
		}
		if p.cell == nil {
			return 0, fmt.Errorf("%w: %q", ErrUnknownCell, tok.Text)
		}
		v, known := p.cell(tok.Text)
		if !known {
			return 0, fmt.Errorf("%w: %q", ErrUnknownCell, tok.Text)
		}
		if !p.syntact && (math.IsNaN(v) || math.IsInf(v, 0)) {
			return 0, fmt.Errorf("formula cell %q is not finite: %v", tok.Text, v)
		}
		return v, nil
	case LParen:
		p.consume()
		p.depth++
		if p.depth > maxEvalDepth {
			return 0, outsideGrammar(tok.Text)
		}
		v, err := p.expr()
		p.depth--
		if err != nil {
			return 0, err
		}
		tok, ok := p.peek()
		if !ok || tok.Kind != RParen {
			if ok {
				return 0, outsideGrammar(tok.Text)
			}
			return 0, outsideGrammar("")
		}
		p.consume()
		return v, nil
	}
	return 0, outsideGrammar(tok.Text)
}

// call parses a GUARD(x), SQRT(x) or ATAN2(y,x) call whose Func
// token is tok: GUARD is the value of its argument, SQRT its square
// root and ATAN2 the angle of (y,x). Any other function, a missing
// "(", a wrong argument count or a missing "," or ")" is outside
// the grammar naming the token that puts it there. Depth counts the
// call as it counts parentheses.
func (p *parser) call(tok Token) (float64, error) {
	name := tok.Text
	one := ascii.EqualFold(name, "GUARD") || ascii.EqualFold(name, "SQRT")
	two := ascii.EqualFold(name, "ATAN2")
	if !one && !two {
		return 0, outsideGrammar(tok.Text)
	}
	p.consume()
	t, ok := p.peek()
	if !ok || t.Kind != LParen {
		if ok {
			return 0, outsideGrammar(t.Text)
		}
		return 0, outsideGrammar("")
	}
	p.consume()
	p.depth++
	if p.depth > maxEvalDepth {
		p.depth--
		return 0, outsideGrammar(t.Text)
	}
	defer func() { p.depth-- }()
	first, err := p.expr()
	if err != nil {
		return 0, err
	}
	if one {
		if err := p.closeParen(); err != nil {
			return 0, err
		}
		if ascii.EqualFold(name, "GUARD") {
			return first, nil
		}
		return p.finite(math.Sqrt(first))
	}
	t, ok = p.peek()
	if !ok || t.Kind != Comma {
		if ok {
			return 0, outsideGrammar(t.Text)
		}
		return 0, outsideGrammar("")
	}
	p.consume()
	second, err := p.expr()
	if err != nil {
		return 0, err
	}
	if err := p.closeParen(); err != nil {
		return 0, err
	}
	return p.finite(math.Atan2(first, second))
}

// closeParen consumes the ")" that closes a call, or reports the
// token that should have been it as outside the grammar.
func (p *parser) closeParen() error {
	tok, ok := p.peek()
	if !ok || tok.Kind != RParen {
		if ok {
			return outsideGrammar(tok.Text)
		}
		return outsideGrammar("")
	}
	p.consume()
	return nil
}

// finite reports a non-finite intermediate or result as an error,
// except while validating the grammar only for Refs.
func (p *parser) finite(v float64) (float64, error) {
	if p.syntact || (!math.IsNaN(v) && !math.IsInf(v, 0)) {
		return v, nil
	}
	return 0, fmt.Errorf("formula result is not finite: %v", v)
}

// peek returns the next non-space token without consuming it.
func (p *parser) peek() (Token, bool) {
	p.pos = skipSpaces(p.toks, p.pos)
	if p.pos >= len(p.toks) {
		return Token{}, false
	}
	return p.toks[p.pos], true
}

// consume drops the token peek last returned.
func (p *parser) consume() {
	p.pos++
}

// outsideGrammar returns an ErrOutsideGrammar naming the token, or
// the end of the formula when the token is empty.
func outsideGrammar(token string) error {
	if token == "" {
		return fmt.Errorf("%w: <end>", ErrOutsideGrammar)
	}
	return fmt.Errorf("%w: %q", ErrOutsideGrammar, token)
}
