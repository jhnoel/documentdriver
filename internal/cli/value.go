package cli

import (
	"strconv"
	"strings"
	"unicode/utf8"
)

// Kind is a JSON value's kind.
type Kind uint8

// The kinds.
const (
	Null Kind = iota
	Bool
	Number
	String
	Array
	Object
)

// String names the kind as error messages do.
func (k Kind) String() string {
	return [...]string{"null", "a boolean", "a number", "a string", "a list", "an object"}[k]
}

// Value is one parsed JSON value with its place in the text.
type Value struct {
	Kind Kind
	// Bool is the value of a Bool; Str of a String; Num the text of a
	// Number as written.
	Bool bool
	Str  string
	Num  string
	// Items are an Array's elements; Members an Object's, in text order.
	Items   []*Value
	Members []Member
	// Off and End are the byte range of the value in the text.
	Off, End int
}

// Member is one entry of an object.
type Member struct {
	Key string
	// KeyOff is the byte offset of the key's opening quote.
	KeyOff int
	Value  *Value
}

// Get returns the member named key, or nil.
func (v *Value) Get(key string) *Value {
	if v == nil {
		return nil
	}
	for _, m := range v.Members {
		if m.Key == key {
			return m.Value
		}
	}
	return nil
}

// MaxDepth is the deepest nesting ParseJSON accepts.
const MaxDepth = 128

type jsonParser struct {
	s     []byte
	i     int
	depth int
}

// ParseJSON parses one JSON document strictly: RFC 8259 grammar, valid
// UTF-8, no duplicate keys, at most MaxDepth levels. The error is an
// invalid_json Error with a span.
func ParseJSON(text []byte) (*Value, *Error) {
	if !utf8.Valid(text) {
		return nil, &Error{Code: CodeInvalidJSON, Message: "the plan is not valid UTF-8"}
	}
	p := &jsonParser{s: text}
	p.space()
	v, err := p.value()
	if err == nil {
		p.space()
		if p.i < len(p.s) {
			err = p.fail(p.i, "unexpected text after the JSON value")
		}
	}
	if err != nil {
		return nil, err
	}
	return v, nil
}

// fail returns an invalid_json error at byte offset at with message msg.
func (p *jsonParser) fail(at int, msg string) *Error {
	at = min(at, len(p.s))
	return &Error{Code: CodeInvalidJSON, Message: msg, Span: SpanOf(p.s, at, min(at+1, len(p.s)))}
}

// space skips JSON whitespace at the parser's current position.
func (p *jsonParser) space() {
	for p.i < len(p.s) {
		switch p.s[p.i] {
		case ' ', '\t', '\n', '\r':
			p.i++
		default:
			return
		}
	}
}

// value parses the JSON value at the current position and returns it.
func (p *jsonParser) value() (*Value, *Error) {
	if p.i >= len(p.s) {
		return nil, p.fail(p.i, "the JSON ends where a value should be")
	}
	start := p.i
	switch c := p.s[p.i]; {
	case c == '{':
		return p.object()
	case c == '[':
		return p.array()
	case c == '"':
		s, err := p.str()
		if err != nil {
			return nil, err
		}
		return &Value{Kind: String, Str: s, Off: start, End: p.i}, nil
	case c == '-' || c >= '0' && c <= '9':
		return p.number()
	}
	for _, lit := range []struct {
		text string
		v    Value
	}{{"true", Value{Kind: Bool, Bool: true}}, {"false", Value{Kind: Bool}}, {"null", Value{Kind: Null}}} {
		if strings.HasPrefix(string(p.s[p.i:min(len(p.s), p.i+len(lit.text))]), lit.text) {
			p.i += len(lit.text)
			v := lit.v
			v.Off, v.End = start, p.i
			return &v, nil
		}
	}
	return nil, p.fail(p.i, "expected a JSON value")
}

// enter records entering a nesting level, failing when MaxDepth is exceeded.
func (p *jsonParser) enter(at int) *Error {
	p.depth++
	if p.depth > MaxDepth {
		return p.fail(at, "the JSON is nested more than "+strconv.Itoa(MaxDepth)+" levels deep")
	}
	return nil
}

// object parses a JSON object at the current position and returns it.
func (p *jsonParser) object() (*Value, *Error) {
	v := &Value{Kind: Object, Off: p.i}
	if err := p.enter(p.i); err != nil {
		return nil, err
	}
	defer func() { p.depth-- }()
	p.i++
	p.space()
	if p.i < len(p.s) && p.s[p.i] == '}' {
		p.i++
		v.End = p.i
		return v, nil
	}
	seen := map[string]bool{}
	for {
		p.space()
		if p.i >= len(p.s) || p.s[p.i] != '"' {
			return nil, p.fail(p.i, "expected a key in double quotes")
		}
		keyOff := p.i
		key, err := p.str()
		if err != nil {
			return nil, err
		}
		if seen[key] {
			e := p.fail(keyOff, "duplicate key "+strconv.Quote(key))
			e.Span = SpanOf(p.s, keyOff, p.i)
			return nil, e
		}
		seen[key] = true
		p.space()
		if p.i >= len(p.s) || p.s[p.i] != ':' {
			return nil, p.fail(p.i, "expected : after the key")
		}
		p.i++
		p.space()
		val, err := p.value()
		if err != nil {
			return nil, err
		}
		v.Members = append(v.Members, Member{Key: key, KeyOff: keyOff, Value: val})
		p.space()
		if p.i >= len(p.s) {
			return nil, p.fail(p.i, "the JSON ends inside an object")
		}
		switch p.s[p.i] {
		case ',':
			p.i++
		case '}':
			p.i++
			v.End = p.i
			return v, nil
		default:
			return nil, p.fail(p.i, "expected , or } in the object")
		}
	}
}

// array parses a JSON array at the current position and returns it.
func (p *jsonParser) array() (*Value, *Error) {
	v := &Value{Kind: Array, Off: p.i}
	if err := p.enter(p.i); err != nil {
		return nil, err
	}
	defer func() { p.depth-- }()
	p.i++
	p.space()
	if p.i < len(p.s) && p.s[p.i] == ']' {
		p.i++
		v.End = p.i
		return v, nil
	}
	for {
		p.space()
		item, err := p.value()
		if err != nil {
			return nil, err
		}
		v.Items = append(v.Items, item)
		p.space()
		if p.i >= len(p.s) {
			return nil, p.fail(p.i, "the JSON ends inside a list")
		}
		switch p.s[p.i] {
		case ',':
			p.i++
		case ']':
			p.i++
			v.End = p.i
			return v, nil
		default:
			return nil, p.fail(p.i, "expected , or ] in the list")
		}
	}
}

// number parses a JSON number at the current position and returns it.
func (p *jsonParser) number() (*Value, *Error) {
	start := p.i
	digits := func() int {
		n := 0
		for p.i < len(p.s) && p.s[p.i] >= '0' && p.s[p.i] <= '9' {
			p.i++
			n++
		}
		return n
	}
	if p.s[p.i] == '-' {
		p.i++
	}
	intStart := p.i
	if digits() == 0 {
		return nil, p.fail(start, "malformed number")
	}
	if p.s[intStart] == '0' && p.i-intStart > 1 {
		return nil, p.fail(start, "malformed number: leading zero")
	}
	if p.i < len(p.s) && p.s[p.i] == '.' {
		p.i++
		if digits() == 0 {
			return nil, p.fail(start, "malformed number")
		}
	}
	if p.i < len(p.s) && (p.s[p.i] == 'e' || p.s[p.i] == 'E') {
		p.i++
		if p.i < len(p.s) && (p.s[p.i] == '+' || p.s[p.i] == '-') {
			p.i++
		}
		if digits() == 0 {
			return nil, p.fail(start, "malformed number")
		}
	}
	return &Value{Kind: Number, Num: string(p.s[start:p.i]), Off: start, End: p.i}, nil
}

// str parses a JSON string at the current position and returns its value.
func (p *jsonParser) str() (string, *Error) {
	start := p.i
	p.i++
	var b strings.Builder
	for {
		if p.i >= len(p.s) {
			return "", p.fail(start, "the JSON ends inside a string")
		}
		c := p.s[p.i]
		switch {
		case c == '"':
			p.i++
			return b.String(), nil
		case c < 0x20:
			return "", p.fail(p.i, "control character in a string; write it as an escape")
		case c != '\\':
			b.WriteByte(c)
			p.i++
		default:
			if err := p.escape(&b); err != nil {
				return "", err
			}
		}
	}
}

// escape decodes one escape sequence into b, returning an error when malformed.
func (p *jsonParser) escape(b *strings.Builder) *Error {
	at := p.i
	p.i++
	if p.i >= len(p.s) {
		return p.fail(at, "the JSON ends inside a string")
	}
	c := p.s[p.i]
	p.i++
	switch c {
	case '"', '\\', '/':
		b.WriteByte(c)
	case 'b':
		b.WriteByte('\b')
	case 'f':
		b.WriteByte('\f')
	case 'n':
		b.WriteByte('\n')
	case 'r':
		b.WriteByte('\r')
	case 't':
		b.WriteByte('\t')
	case 'u':
		r, ok := p.hex4()
		if !ok {
			return p.fail(at, `malformed \u escape`)
		}
		switch {
		case r >= 0xD800 && r < 0xDC00:
			if p.i+1 < len(p.s) && p.s[p.i] == '\\' && p.s[p.i+1] == 'u' {
				p.i += 2
				if lo, ok := p.hex4(); ok && lo >= 0xDC00 && lo < 0xE000 {
					b.WriteRune(0x10000 + (r-0xD800)<<10 + (lo - 0xDC00))
					return nil
				}
			}
			return p.fail(at, `unpaired surrogate in a \u escape`)
		case r >= 0xDC00 && r < 0xE000:
			return p.fail(at, `unpaired surrogate in a \u escape`)
		}
		b.WriteRune(r)
	default:
		return p.fail(at, "unknown escape in a string")
	}
	return nil
}

// hex4 parses the next four hex digits as a rune, reporting success.
func (p *jsonParser) hex4() (rune, bool) {
	if p.i+4 > len(p.s) {
		return 0, false
	}
	n, err := strconv.ParseUint(string(p.s[p.i:p.i+4]), 16, 16)
	if err != nil {
		return 0, false
	}
	p.i += 4
	return rune(n), true
}

// SpanOf is the span of the byte range [start, end) of text.
func SpanOf(text []byte, start, end int) *Span {
	lc := func(at int) (int, int) {
		at = max(0, min(at, len(text)))
		line := 1
		last := 0
		for i := range at {
			if text[i] == '\n' {
				line++
				last = i + 1
			}
		}
		return line, utf8.RuneCount(text[last:at]) + 1
	}
	l, c := lc(start)
	el, ec := lc(end)
	return &Span{Line: l, Column: c, EndLine: el, EndColumn: ec}
}
