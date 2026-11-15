package formula

import (
	"fmt"
	"strings"
)

// MaxDepth bounds the nesting of parentheses and function calls Validate
// follows. Excel's own limit is 64 nested functions; this one is higher so
// that it only bounds the recursion, and a formula past it is refused with
// a reason.
const MaxDepth = 256

// Refusal is why a formula was refused, or why a rewrite cannot be done:
// a byte offset into the formula and a sentence. It carries no contract
// error code or path; the caller maps it.
type Refusal struct {
	Pos    int
	Reason string
}

// Error implements error.
func (r *Refusal) Error() string { return fmt.Sprintf("at %d: %s", r.Pos, r.Reason) }

// refuse returns a Refusal at pos with the formatted reason.
func refuse(pos int, format string, args ...any) *Refusal {
	return &Refusal{Pos: pos, Reason: fmt.Sprintf(format, args...)}
}

// Validate checks that src is a well-formed stored formula: operands and
// operators alternate (a space between two operands is the intersection
// operator), brackets balance, function arguments and array constants are
// well formed, references are inside the grid, and table references are
// bracketed specifiers. It refuses the display forms "#" and "@", which a
// file stores as ANCHORARRAY and _xlfn.SINGLE. It does not check function
// names, argument counts or types. The error is a *Refusal.
func Validate(src string) error {
	return ValidateTokens(Lex(src))
}

// ValidateTokens is Validate of tokens Lex returned.
func ValidateTokens(toks []Token) error {
	p := &parser{toks: toks}
	p.skipSpace()
	if p.done() {
		return refuse(p.pos(), "empty formula")
	}
	if err := p.expr(0, true); err != nil {
		return err
	}
	p.skipSpace()
	if !p.done() {
		return p.unexpected()
	}
	return nil
}

type parser struct {
	toks []Token
	i    int
	// refish says the last operand parsed can be a reference (a
	// reference, name, table reference, call or parenthesised
	// expression): only those take the intersection operator.
	refish bool
}

// refishStart reports whether a token can begin an operand that may be a
// reference.
func refishStart(k Kind) bool {
	switch k {
	case Ref, Name, StructRef, Func, LParen:
		return true
	}
	return false
}

// done reports whether the parser has consumed every token.
func (p *parser) done() bool { return p.i >= len(p.toks) }

// cur returns the current token; the caller must ensure the parser is not done.
func (p *parser) cur() Token { return p.toks[p.i] }

// pos returns the current token's offset, or the end of the final token.
func (p *parser) pos() int {
	if p.done() {
		if len(p.toks) == 0 {
			return 0
		}
		t := p.toks[len(p.toks)-1]
		return t.Pos + len(t.Text)
	}
	return p.toks[p.i].Pos
}

// skipSpace skips Space tokens and reports whether there were any.
func (p *parser) skipSpace() bool {
	n := p.i
	for !p.done() && p.cur().Kind == Space {
		p.i++
	}
	return p.i > n
}

// unexpected returns a refusal for a token that cannot appear here, or the end of formula.
func (p *parser) unexpected() *Refusal {
	if p.done() {
		return refuse(p.pos(), "unexpected end of formula")
	}
	t := p.cur()
	switch t.Kind {
	case Invalid:
		return invalidReason(t)
	case Hash:
		return refuse(t.Pos, "display form %q: a stored formula writes the spill operator as ANCHORARRAY", "#")
	case At:
		return refuse(t.Pos, "display form %q: a stored formula writes implicit intersection as _xlfn.SINGLE", "@")
	}
	return refuse(t.Pos, "unexpected %s %q", t.Kind, t.Text)
}

// invalidReason returns a refusal explaining an Invalid token's malformed text.
func invalidReason(t Token) *Refusal {
	switch t.Text[0] {
	case '"':
		return refuse(t.Pos, "unterminated string")
	case '\'':
		return refuse(t.Pos, "quoted sheet name %q not followed by a reference", t.Text)
	case '[':
		return refuse(t.Pos, "unbalanced bracket in %q", t.Text)
	}
	return refuse(t.Pos, "unexpected %q", t.Text)
}

// startsOperand reports whether a token can begin an operand.
func startsOperand(k Kind) bool {
	switch k {
	case Number, String, Bool, Error, Ref, Name, Func, StructRef, LParen, LBrace:
		return true
	}
	return false
}

// expr parses operands joined by binary operators. commaIsUnion is true
// at the top level and inside plain parentheses, where "," is the union
// operator; inside a call it separates arguments and ends the expression.
func (p *parser) expr(depth int, commaIsUnion bool) error {
	for {
		if err := p.operand(depth); err != nil {
			return err
		}
		spaced := p.skipSpace()
		for !p.done() && p.cur().Kind == Op && p.cur().Text == "%" {
			p.i++
			p.refish = false
			spaced = p.skipSpace()
		}
		if p.done() {
			return nil
		}
		t := p.cur()
		switch {
		case t.Kind == Op:
			p.i++
		case t.Kind == Comma && commaIsUnion:
			p.i++
		case spaced && p.refish && refishStart(t.Kind):
			// Intersection: the space is the operator.
		case startsOperand(t.Kind) || t.Kind == Hash || t.Kind == At || t.Kind == Invalid:
			if t.Kind == Hash || t.Kind == At || t.Kind == Invalid {
				return p.unexpected()
			}
			return refuse(t.Pos, "missing operator before %q", t.Text)
		default:
			return nil
		}
	}
}

// operand parses prefix signs and one primary.
func (p *parser) operand(depth int) error {
	for {
		p.skipSpace()
		if p.done() {
			return refuse(p.pos(), "expected an operand at end of formula")
		}
		if t := p.cur(); t.Kind == Op && (t.Text == "-" || t.Text == "+") {
			p.i++
			continue
		}
		break
	}
	t := p.cur()
	// Set after the primary: a call or parentheses parse operands of
	// their own.
	refish := refishStart(t.Kind)
	defer func() { p.refish = refish }()
	switch t.Kind {
	case Number, String, Bool, Error, Name:
		if err := checkQualified(t); err != nil {
			return err
		}
		p.i++
		return nil
	case Ref:
		if err := checkRef(t); err != nil {
			return err
		}
		p.i++
		return nil
	case StructRef:
		if err := checkStructRef(t); err != nil {
			return err
		}
		p.i++
		return nil
	case Func:
		if err := checkQualified(t); err != nil {
			return err
		}
		return p.call(depth)
	case LParen:
		if depth >= MaxDepth {
			return refuse(t.Pos, "nesting deeper than %d", MaxDepth)
		}
		p.i++
		if err := p.expr(depth+1, true); err != nil {
			return err
		}
		p.skipSpace()
		if p.done() || p.cur().Kind != RParen {
			return refuse(p.pos(), "missing %q", ")")
		}
		p.i++
		return nil
	case LBrace:
		return p.array()
	}
	if t.Kind == Op || t.Kind == Comma || t.Kind == Semicolon || t.Kind == RParen || t.Kind == RBrace {
		return refuse(t.Pos, "expected an operand before %q", t.Text)
	}
	return p.unexpected()
}

// call parses a function name, "(" and its arguments, any of which may be
// empty (IF(A1,,1)).
func (p *parser) call(depth int) error {
	name := p.cur()
	if depth >= MaxDepth {
		return refuse(name.Pos, "nesting deeper than %d", MaxDepth)
	}
	p.i++
	if p.done() || p.cur().Kind != LParen {
		return refuse(p.pos(), "function %q without %q", name.Text, "(")
	}
	p.i++
	p.skipSpace()
	if !p.done() && p.cur().Kind == RParen {
		p.i++
		return nil
	}
	for {
		p.skipSpace()
		if p.done() {
			return refuse(p.pos(), "missing %q after the arguments of %s", ")", name.Text)
		}
		if k := p.cur().Kind; k != Comma && k != RParen {
			if err := p.expr(depth+1, false); err != nil {
				return err
			}
			p.skipSpace()
		}
		if p.done() {
			return refuse(p.pos(), "missing %q after the arguments of %s", ")", name.Text)
		}
		switch p.cur().Kind {
		case Comma:
			p.i++
		case RParen:
			p.i++
			return nil
		default:
			return p.unexpected()
		}
	}
}

// array parses an array constant: rows of constants of equal length.
func (p *parser) array() error {
	open := p.cur()
	p.i++
	cols, rowCols := -1, 0
	for {
		p.skipSpace()
		for !p.done() && p.cur().Kind == Op && (p.cur().Text == "-" || p.cur().Text == "+") {
			p.i++
			p.skipSpace()
		}
		if p.done() {
			return refuse(open.Pos, "unterminated array constant")
		}
		switch t := p.cur(); t.Kind {
		case Number, String, Bool, Error:
			p.i++
		default:
			return refuse(t.Pos, "array constant element %q is not a constant", t.Text)
		}
		rowCols++
		p.skipSpace()
		if p.done() {
			return refuse(open.Pos, "unterminated array constant")
		}
		switch t := p.cur(); t.Kind {
		case Comma:
			p.i++
		case Semicolon, RBrace:
			if cols >= 0 && rowCols != cols {
				return refuse(t.Pos, "array constant rows differ in length")
			}
			cols, rowCols = rowCols, 0
			p.i++
			if t.Kind == RBrace {
				return nil
			}
		default:
			return refuse(t.Pos, "unexpected %q in an array constant", t.Text)
		}
	}
}

// checkQualified checks the prefix of a Name or Func token.
func checkQualified(t Token) error {
	if t.Kind != Name && t.Kind != Func {
		return nil
	}
	if _, _, ok := SplitQualified(t.Text); !ok {
		return refuse(t.Pos, "malformed prefix in %q", t.Text)
	}
	return nil
}

// checkRef returns a refusal when a Ref token's prefix or reference body is malformed.
func checkRef(t Token) error {
	pre, body, ok := SplitQualified(t.Text)
	if !ok {
		return refuse(t.Pos, "malformed prefix in %q", t.Text)
	}
	if _, isErr, ok := ParseRefBody(body); !ok || isErr && pre.Text == "" {
		return refuse(t.Pos, "malformed reference %q", t.Text)
	}
	return nil
}

// structKeywords are the special items of a table reference (ECMA-376
// Part 1 §18.17.6.2, intra-table-reference keywords).
var structKeywords = [...]string{"#All", "#Data", "#Headers", "#Totals", "#This Row"}

// checkStructRef checks a table reference's bracketed specifier: empty, a
// column or keyword, or bracketed items joined by "," or ":".
func checkStructRef(t Token) error {
	open := strings.IndexByte(t.Text, '[')
	inner := t.Text[open+1 : len(t.Text)-1]
	pos := t.Pos + open + 1
	if !strings.Contains(unescapedOnly(inner), "[") {
		return checkStructItem(inner, pos)
	}
	i := 0
	expectItem := true
	for i < len(inner) {
		c := inner[i]
		switch {
		case c == ' ':
			i++
		case c == '[' && expectItem:
			e := bracketEnd(inner, i)
			if e < 0 {
				return refuse(pos+i, "unbalanced bracket in %q", t.Text)
			}
			if err := checkStructItem(inner[i+1:e-1], pos+i+1); err != nil {
				return err
			}
			i = e
			expectItem = false
		case (c == ',' || c == ':') && !expectItem:
			i++
			expectItem = true
		default:
			return refuse(pos+i, "unexpected %q in table reference %q", string(c), t.Text)
		}
	}
	if expectItem {
		return refuse(pos+len(inner), "table reference %q ends without an item", t.Text)
	}
	return nil
}

// checkStructItem checks one item: a keyword or a column name with "'"
// escapes and no bare brackets.
func checkStructItem(item string, pos int) error {
	if strings.HasPrefix(item, "#") {
		for _, k := range structKeywords {
			if strings.EqualFold(item, k) {
				return nil
			}
		}
		return refuse(pos, "unknown table keyword %q", item)
	}
	if strings.HasPrefix(item, "@") {
		return refuse(pos, "display form %q: a stored table reference writes [#This Row]", "@")
	}
	u := unescapedOnly(item)
	if strings.ContainsAny(u, "[]") {
		return refuse(pos, "unescaped bracket in table column %q", item)
	}
	return nil
}

// unescapedOnly drops every "'"-escaped character pair, leaving the text
// a bracket check reads.
func unescapedOnly(s string) string {
	if !strings.Contains(s, "'") {
		return s
	}
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] == '\'' {
			i++
			continue
		}
		b.WriteByte(s[i])
	}
	return b.String()
}
