package formula

import (
	"math"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/jhnoel/documentdriver/internal/ascii"
)

// Kind is the class of a token.
type Kind uint8

// The token kinds. Every byte of a formula belongs to exactly one token.
const (
	// Invalid is text the lexer cannot classify: an unterminated string
	// or a stray character. Later layers refuse it; Lex only classifies.
	Invalid Kind = iota
	// Space is a run of spaces, tabs, carriage returns and line feeds.
	Space
	// Number is a numeric literal with an optional unit suffix: 0.5, 1IN,
	// 0.5DL, 25.4MM, 1E-4, 100 (the % after 100 is a separate Op).
	Number
	// String is a double-quoted literal with "" for a quote.
	String
	// Ref is a cell reference: Width, Prop.Cost, Geometry1.X2,
	// Char.Size[2], Sheet.5!Width, ThePage!PageHeight, Connections.X1,
	// Sheet.5!Actions.Start.Checked, Inh, TRUE.
	Ref
	// Func is a function name immediately followed by "("; the "(" is
	// the next token. GUARD, PAR, PNT, _WALKGLUE, ATAN2.
	Func
	// Op is an operator: + - * / ^ & = <> < > <= >= %.
	Op
	// Comma separates function arguments.
	Comma
	// Semicolon separates list items, as in INDEX(User.HSide,"0;3").
	Semicolon
	// LParen opens a call or a group.
	LParen
	// RParen closes a call or a group.
	RParen
)

// String names the kind.
func (k Kind) String() string {
	switch k {
	case Invalid:
		return "invalid"
	case Space:
		return "space"
	case Number:
		return "num"
	case String:
		return "string"
	case Ref:
		return "ref"
	case Func:
		return "func"
	case Op:
		return "op"
	case Comma:
		return ","
	case Semicolon:
		return ";"
	case LParen:
		return "("
	case RParen:
		return ")"
	}
	return "kind?"
}

// Token is one lexed token: its kind, its text (a substring of the
// input, never a copy) and the byte offset where it starts.
type Token struct {
	Kind Kind
	Text string
	Pos  int
}

// Lex splits a stored formula into tokens. It never fails and never
// panics: text it cannot classify becomes an Invalid token, and the
// token texts always concatenate back to s. It is a single forward
// pass with no recursion, allocating the token slice in proportion to
// the input and copying no token text.
func Lex(s string) []Token {
	toks := make([]Token, 0, len(s)/3+1)
	for i := 0; i < len(s); {
		kind, end := next(s, i)
		toks = append(toks, Token{Kind: kind, Text: s[i:end], Pos: i})
		i = end
	}
	return toks
}

// Join concatenates the token texts: for the tokens of Lex(s) it is s.
// It sizes its buffer from the last token's end, which for Lex tokens
// is the input length; for other tokens that is only a size hint.
func Join(toks []Token) string {
	if len(toks) == 0 {
		return ""
	}
	var b strings.Builder
	b.Grow(toks[len(toks)-1].Pos + len(toks[len(toks)-1].Text))
	for i := range toks {
		b.WriteString(toks[i].Text)
	}
	return b.String()
}

// NumberValue returns the value and unit of a Number token: the value
// is the numeric part parsed as a finite float64 and the unit is the
// letter suffix after it ("" for a plain number), so 1IN gives 1 and
// "IN", 0.5DL gives 0.5 and "DL" and 1E-4 gives 0.0001 and "". ok is
// false for a token that is not a Number and for a number that does
// not parse to a finite value (1E400 overflows). The unit is not
// validated here: the closed unit list is the later layers' decision.
func (t Token) NumberValue() (float64, string, bool) {
	if t.Kind != Number {
		return 0, "", false
	}
	end := numberCoreEnd(t.Text, 0)
	v, err := strconv.ParseFloat(t.Text[:end], 64)
	if err != nil || math.IsNaN(v) || math.IsInf(v, 0) {
		return 0, "", false
	}
	return v, t.Text[end:], true
}

// next classifies the token at s[i:] and returns its kind and end,
// which is always past i.
func next(s string, i int) (Kind, int) {
	switch s[i] {
	case ' ', '\t', '\r', '\n':
		j := i + 1
		for j < len(s) && isSpace(s[j]) {
			j++
		}
		return Space, j
	case '"':
		return lexString(s, i)
	case '(':
		return LParen, i + 1
	case ')':
		return RParen, i + 1
	case ',':
		return Comma, i + 1
	case ';':
		return Semicolon, i + 1
	case '+', '-', '*', '/', '^', '&', '=', '%':
		return Op, i + 1
	case '<':
		if i+1 < len(s) && (s[i+1] == '=' || s[i+1] == '>') {
			return Op, i + 2
		}
		return Op, i + 1
	case '>':
		if i+1 < len(s) && s[i+1] == '=' {
			return Op, i + 2
		}
		return Op, i + 1
	}
	if ascii.IsDigit(rune(s[i])) || s[i] == '.' && i+1 < len(s) && ascii.IsDigit(rune(s[i+1])) {
		return Number, numberEnd(s, i)
	}
	if isNameStart(s[i]) {
		return nameToken(s, i)
	}
	_, n := utf8.DecodeRuneInString(s[i:])
	if n <= 0 {
		n = 1
	}
	return Invalid, i + n
}

// lexString returns the kind and end of the string literal at s[i:]:
// String past the closing quote, or Invalid to the end of s when the
// string is unterminated. A doubled quote ("") is an escaped quote and
// does not close.
func lexString(s string, i int) (Kind, int) {
	for j := i + 1; j < len(s); j++ {
		if s[j] != '"' {
			continue
		}
		if j+1 < len(s) && s[j+1] == '"' {
			j++
			continue
		}
		return String, j + 1
	}
	return Invalid, len(s)
}

// nameToken returns the kind and end of the name starting at s[i:]: a
// Func when a single name is immediately followed by "(", otherwise a
// Ref, including a reference with a Sheet.N! or Name! prefix.
func nameToken(s string, i int) (Kind, int) {
	first := nameEnd(s, i)
	if rest, ok := refPrefix(s, first); ok {
		if rest < len(s) && isNameStart(s[rest]) {
			return Ref, refEnd(s, rest)
		}
		return Ref, rest
	}
	end := refEnd(s, i)
	if end == first && end < len(s) && s[end] == '(' {
		return Func, end
	}
	return Ref, end
}

// refPrefix returns the index just past a reference prefix ending at
// the first name's end j, and whether there is one: Name! or
// Sheet.5! (a name, a dot, digits, "!").
func refPrefix(s string, j int) (int, bool) {
	if j < len(s) && s[j] == '!' {
		return j + 1, true
	}
	if j < len(s) && s[j] == '.' {
		k := j + 1
		for k < len(s) && ascii.IsDigit(rune(s[k])) {
			k++
		}
		if k > j+1 && k < len(s) && s[k] == '!' {
			return k + 1, true
		}
	}
	return 0, false
}

// refEnd returns the end of the reference at s[i:], whose first name
// starts at i: dot-separated names where each name may carry a
// [digits] index, as Char.Size[2] and Controls.Row_1.Y. A dot or
// bracket that does not continue the reference is left for the next
// token, so the scan stays lossless on malformed references.
func refEnd(s string, i int) int {
	j := bracketEnd(s, nameEnd(s, i))
	for j < len(s) && s[j] == '.' && j+1 < len(s) && isNameStart(s[j+1]) {
		j = bracketEnd(s, nameEnd(s, j+1))
	}
	return j
}

// nameEnd returns the end of the name starting at s[i:].
func nameEnd(s string, i int) int {
	j := i
	for j < len(s) && isNameChar(s[j]) {
		j++
	}
	return j
}

// bracketEnd returns the end of a [digits] index at s[j:], or j when
// there is none.
func bracketEnd(s string, j int) int {
	if j >= len(s) || s[j] != '[' {
		return j
	}
	k := j + 1
	for k < len(s) && ascii.IsDigit(rune(s[k])) {
		k++
	}
	if k > j+1 && k < len(s) && s[k] == ']' {
		return k + 1
	}
	return j
}

// numberEnd returns the end of the number at s[i:], unit included.
func numberEnd(s string, i int) int {
	j := numberCoreEnd(s, i)
	for j < len(s) && ascii.IsAlpha(rune(s[j])) {
		j++
	}
	return j
}

// numberCoreEnd returns the end of the numeric part of the number at
// s[i:]: digits, an optional fraction and an optional exponent. The
// exponent is taken only when an e or E is followed by an optional
// sign and a digit, so 1E-4 is one number and a trailing E is a unit.
func numberCoreEnd(s string, i int) int {
	j := i
	for j < len(s) && ascii.IsDigit(rune(s[j])) {
		j++
	}
	if j < len(s) && s[j] == '.' {
		j++
		for j < len(s) && ascii.IsDigit(rune(s[j])) {
			j++
		}
	}
	if j < len(s) && (s[j] == 'e' || s[j] == 'E') {
		k := j + 1
		if k < len(s) && (s[k] == '+' || s[k] == '-') {
			k++
		}
		if k < len(s) && ascii.IsDigit(rune(s[k])) {
			for k < len(s) && ascii.IsDigit(rune(s[k])) {
				k++
			}
			j = k
		}
	}
	return j
}

// isSpace reports whether c is a space, tab, carriage return or line
// feed.
func isSpace(c byte) bool { return c == ' ' || c == '\t' || c == '\r' || c == '\n' }

// isNameStart reports whether c can start a name: an ASCII letter, "_"
// (function names such as _WALKGLUE start with it), or a byte of a
// multi-byte character, so non-ASCII names tokenize as one reference.
func isNameStart(c byte) bool {
	return ascii.IsAlpha(rune(c)) || c == '_' || c >= 0x80
}

// isNameChar reports whether c can continue a name.
func isNameChar(c byte) bool {
	return isNameStart(c) || ascii.IsDigit(rune(c))
}
