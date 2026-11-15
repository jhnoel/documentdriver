package formula

import (
	"strings"
	"unicode/utf8"
)

// Lex splits a stored formula into tokens. It never fails: text it cannot
// classify becomes an Invalid token, and the token texts always
// concatenate back to s. It is a single forward pass with no recursion.
func Lex(s string) []Token {
	toks := make([]Token, 0, 1+len(s)/4)
	for i := 0; i < len(s); {
		k, end := next(s, i)
		toks = append(toks, Token{Kind: k, Text: s[i:end], Pos: i})
		i = end
	}
	return toks
}

// next classifies the token at s[i:] and returns its kind and end, which
// is always past i.
func next(s string, i int) (Kind, int) {
	switch c := s[i]; c {
	case ' ', '\t', '\r', '\n':
		j := i + 1
		for j < len(s) && isSpace(s[j]) {
			j++
		}
		return Space, j
	case '"':
		return lexString(s, i)
	case '#':
		if n := errorLiteralLen(s[i:]); n > 0 {
			return Error, i + n
		}
		return Hash, i + 1
	case '@':
		return At, i + 1
	case '(':
		return LParen, i + 1
	case ')':
		return RParen, i + 1
	case '{':
		return LBrace, i + 1
	case '}':
		return RBrace, i + 1
	case ',':
		return Comma, i + 1
	case ';':
		return Semicolon, i + 1
	case '+', '-', '*', '/', '^', '&', '=', '%', ':':
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
	case '\'':
		if p := prefixAt(s, i); p > 0 {
			return afterBang(s, p)
		}
		if j := quotedEnd(s, i); j > 0 {
			return Invalid, j
		}
		return Invalid, len(s)
	case '[':
		if p := prefixAt(s, i); p > 0 {
			return afterBang(s, p)
		}
		if e := bracketEnd(s, i); e > 0 {
			return StructRef, e
		}
		return Invalid, len(s)
	case '!':
		return afterBang(s, i+1)
	}
	if p := prefixAt(s, i); p > 0 {
		return afterBang(s, p)
	}
	if end := refEnd(s, i); end > 0 {
		return Ref, end
	}
	if c := s[i]; isDigit(c) || c == '.' && i+1 < len(s) && isDigit(s[i+1]) {
		return Number, numberEnd(s, i)
	}
	if r, _ := utf8.DecodeRuneInString(s[i:]); isNameStart(r) {
		return identifier(s, i)
	}
	_, n := utf8.DecodeRuneInString(s[i:])
	return Invalid, i + n
}

// afterBang lexes what follows a reference prefix ending at j (just past
// "!"). A prefix with nothing it can qualify is Invalid.
func afterBang(s string, j int) (Kind, int) {
	if len(s)-j >= 5 && strings.EqualFold(s[j:j+5], "#REF!") {
		return Ref, j + 5
	}
	if end := refEnd(s, j); end > 0 {
		return Ref, end
	}
	if j < len(s) {
		if r, _ := utf8.DecodeRuneInString(s[j:]); isNameStart(r) {
			return identifier(s, j)
		}
	}
	return Invalid, j
}

// identifier lexes a name starting at s[i:] and classifies it by what
// follows: "(" makes a function, "[" a table reference.
func identifier(s string, i int) (Kind, int) {
	e := nameEnd(s, i)
	if e < len(s) {
		switch s[e] {
		case '(':
			return Func, e
		case '[':
			b := bracketEnd(s, e)
			if b < 0 {
				return Invalid, len(s)
			}
			return StructRef, b
		}
	}
	if w := s[i:e]; strings.EqualFold(w, "TRUE") || strings.EqualFold(w, "FALSE") {
		return Bool, e
	}
	return Name, e
}

// refEnd returns the end of a reference at s[i:] (an area, cell, whole
// columns or whole rows) when the text there is one and is not the start
// of a longer name or a function call; 0 otherwise.
func refEnd(s string, i int) int {
	r, end, ok := scanRange(s, i)
	if !ok {
		return 0
	}
	if refBoundary(s, end) {
		return end
	}
	if r.Kind == Cells && !r.Single {
		// "A1:B2x": the area is not a reference, but A1 may still be.
		if _, e, ok := scanCell(s, i); ok && refBoundary(s, e) {
			return e
		}
	}
	return 0
}

// refBoundary reports whether a reference may end at s[e]: at the end, or
// before a character that cannot continue a name or open a call.
func refBoundary(s string, e int) bool {
	if e >= len(s) {
		return true
	}
	switch s[e] {
	case '(', '[', '!', '$':
		return false
	}
	r, _ := utf8.DecodeRuneInString(s[e:])
	return !isNameChar(r)
}

// prefixAt returns the end (past "!") of a reference prefix at s[i:], or
// 0 when there is none. A prefix is an optional workbook in brackets
// ("[1]", taken literally up to the first "]"), then "!" alone or one or
// two sheet names joined by ":" (a 3-D span), each bare or quoted, then
// "!". A quoted name may hold the workbook itself ("'[1]Sheet 1'!").
// Excel writes a 3-D span quoted as a whole ("'Jan:Mar'!"); two names
// quoted apart ("'a':'b'!") are accepted as Excel-authored corpus files
// hold them.
func prefixAt(s string, i int) int {
	j := i
	if j < len(s) && s[j] == '[' {
		k := strings.IndexByte(s[j:], ']')
		if k < 0 {
			return 0
		}
		j += k + 1
		if j < len(s) && s[j] == '!' {
			return j + 1
		}
	}
	e := sheetPart(s, j)
	if e == 0 || e >= len(s) {
		return 0
	}
	switch s[e] {
	case '!':
		return e + 1
	case ':':
		if f := sheetPart(s, e+1); f > 0 && f < len(s) && s[f] == '!' {
			return f + 1
		}
	}
	return 0
}

// sheetPart returns the end of a quoted or bare sheet name at s[i:], or 0.
func sheetPart(s string, i int) int {
	if i >= len(s) {
		return 0
	}
	if s[i] == '\'' {
		return max(quotedEnd(s, i), 0)
	}
	if e := sheetRunEnd(s, i); e > i {
		return e
	}
	return 0
}

// sheetRunEnd skips the characters an unquoted sheet name may hold.
func sheetRunEnd(s string, i int) int {
	for i < len(s) {
		r, n := utf8.DecodeRuneInString(s[i:])
		if !isNameChar(r) {
			break
		}
		i += n
	}
	return i
}

// nameEnd returns the end of the name starting at s[i:].
func nameEnd(s string, i int) int { return sheetRunEnd(s, i) }

// quotedEnd returns the end of the quoted name starting at s[i] == '\”,
// just past the closing quote, with "”" as an escaped quote; -1 if it is
// not closed.
func quotedEnd(s string, i int) int {
	for j := i + 1; j < len(s); j++ {
		if s[j] == '\'' {
			if j+1 < len(s) && s[j+1] == '\'' {
				j++
				continue
			}
			return j + 1
		}
	}
	return -1
}

// bracketEnd returns the end of the balanced bracket group starting at
// s[i] == '[', where "'" escapes the next character; -1 if it is not
// closed.
func bracketEnd(s string, i int) int {
	depth := 0
	for j := i; j < len(s); j++ {
		switch s[j] {
		case '\'':
			j++
		case '[':
			depth++
		case ']':
			depth--
			if depth == 0 {
				return j + 1
			}
		}
	}
	return -1
}

// lexString returns String and the end past the string literal at s[i:], or Invalid when unterminated.
func lexString(s string, i int) (Kind, int) {
	for j := i + 1; j < len(s); j++ {
		if s[j] == '"' {
			if j+1 < len(s) && s[j+1] == '"' {
				j++
				continue
			}
			return String, j + 1
		}
	}
	return Invalid, len(s)
}

// numberEnd returns the end of the number at s[i:].
func numberEnd(s string, i int) int {
	j := i
	for j < len(s) && isDigit(s[j]) {
		j++
	}
	if j < len(s) && s[j] == '.' {
		j++
		for j < len(s) && isDigit(s[j]) {
			j++
		}
	}
	if j < len(s) && s[j]|0x20 == 'e' {
		k := j + 1
		if k < len(s) && (s[k] == '+' || s[k] == '-') {
			k++
		}
		if k < len(s) && isDigit(s[k]) {
			for k < len(s) && isDigit(s[k]) {
				k++
			}
			j = k
		}
	}
	return j
}

// errorLiteralLen returns the length of the error literal s starts with,
// ignoring case, or 0.
func errorLiteralLen(s string) int {
	best := 0
	for _, e := range errorLiterals {
		if len(e) > best && len(s) >= len(e) && strings.EqualFold(s[:len(e)], e) {
			best = len(e)
		}
	}
	return best
}

// isSpace reports whether c is a space, tab, carriage return, or newline.
func isSpace(c byte) bool { return c == ' ' || c == '\t' || c == '\r' || c == '\n' }

// isNameStart and isNameChar follow the defined-name rules of [MS-XLSX]
// §2.2.2 (name-start-character, name-character) for ASCII: a letter, "_"
// or "\\" to start, then also digits, "." and "?". Every other valid
// non-ASCII character counts as a letter: Excel-authored corpus files
// hold sheet names with punctuation such as U+30FB unquoted and formulas
// with typographic quotes it read as names.
func isNameStart(r rune) bool {
	if r < utf8.RuneSelf {
		return r == '_' || r == '\\' || r|0x20 >= 'a' && r|0x20 <= 'z'
	}
	return r != utf8.RuneError
}

// isNameChar reports whether r can appear in a defined name after its first character.
func isNameChar(r rune) bool {
	if r < utf8.RuneSelf {
		switch r {
		case '_', '\\', '.', '?':
			return true
		}
		return r|0x20 >= 'a' && r|0x20 <= 'z' || r >= '0' && r <= '9'
	}
	return r != utf8.RuneError
}
