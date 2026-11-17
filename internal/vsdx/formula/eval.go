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
// numbers with the closed unit list (IN, FT, MM, CM, M, PT, DEG, RAD
// and none), the operators + - * /, unary minus, parentheses and
// same-shape references. The error names the token that puts it
// outside.
var ErrOutsideGrammar = errors.New("formula outside closed grammar")

// ErrUnknownCell reports that a formula in the closed grammar names
// a same-shape reference the cell callback does not know. The error
// names the reference.
var ErrUnknownCell = errors.New("formula cell unknown")

// maxEvalDepth is the nesting limit of the closed evaluator: 64
// nested parentheses or unary minuses evaluate, the 65th is outside
// the grammar. A flat chain of operators is iterative and does not
// count towards it.
const maxEvalDepth = 64

// Eval evaluates f in the closed grammar of decision (c): numbers
// with units from the closed list, + - * /, unary minus, parentheses
// and same-shape references. A reference is looked up through cell
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
// references a dependency closure walks. It returns nil when f is
// outside the grammar, including when it names a Sheet.N! or
// ThePage! reference or a function, and nil when f names none.
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
// formula: expr over term over factor over primary. The + - and * /
// loops are iterative, so a flat formula of any length recurses at
// most a fixed depth; only parentheses and unary minuses recurse,
// and depth bounds both. It evaluates as it parses, so it holds no
// tree and copies no token text.
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
	// depth is the current parenthesis and unary-minus nesting.
	depth int
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

// term parses factors joined by * or /.
func (p *parser) term() (float64, error) {
	left, err := p.factor()
	if err != nil {
		return 0, err
	}
	for {
		tok, ok := p.peek()
		if !ok || tok.Kind != Op || (tok.Text != "*" && tok.Text != "/") {
			return left, nil
		}
		p.consume()
		right, err := p.factor()
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

// primary parses a number, a same-shape reference or a parenthesised
// expression.
func (p *parser) primary() (float64, error) {
	tok, ok := p.peek()
	if !ok {
		return 0, outsideGrammar("")
	}
	switch tok.Kind {
	case Number:
		p.consume()
		v, unit, ok := tok.NumberValue()
		if !ok || implicitUnit(unit) {
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

// implicitUnit reports whether unit is one of the implicit units of
// Constant (DL, DP, DT, DA): they are not in the closed list of the
// evaluator, so a number carrying one is outside its grammar.
func implicitUnit(unit string) bool {
	return ascii.EqualFold(unit, "DL") || ascii.EqualFold(unit, "DP") ||
		ascii.EqualFold(unit, "DT") || ascii.EqualFold(unit, "DA")
}
