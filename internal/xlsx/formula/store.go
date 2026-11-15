package formula

import "strings"

// Store converts a formula as Excel shows it and a plan writes it (no
// leading "=") to the form a workbook stores (chunk K4a, this file: the
// prefixes and Spills; StoreAt, store_at.go, chunk K4b, does "@" and
// "[#This Row]" and Store calls it last). It is Display's inverse on
// what Display produces, and like Display it rewrites only tokens and
// copies every other byte:
//
//   - a function the corpus table knows with a stored prefix
//     (LookupFunction: Prefix PrefixFuture or PrefixWorksheet) gets it,
//     whatever case it is written in; a name already prefixed keeps its
//     prefix. Functions the table does not know are written as given
//     (an open question: Microsoft's published list of future functions
//     is not in the table yet).
//   - LAMBDA and LET parameter names get PrefixParam.
//   - the implicit-intersection operator "@" before an operand becomes
//     _xlfn.SINGLE(operand); "@" inside a structured reference
//     ("Tbl[@Col]", "[@[Col 2]]", "Tbl[@]") becomes "[#This Row]"
//     ("Tbl[[#This Row],[Col]]").
//   - the spill operator "A1#" is refused: it reads a dynamic array,
//     which v1 does not write (decision 8).
//
// The result lexes and is Validate's input; Display(Store(s)) == s for
// every s Store accepts that Display could have produced. The error is a
// *Refusal (a formula that does not lex cleanly, a spill operator).
func Store(display string) (string, error) {
	toks := Lex(display)
	var stack []Kind
	for _, t := range toks {
		switch t.Kind {
		case Invalid:
			return "", invalidReason(t)
		case Hash:
			return "", refuse(t.Pos, "spill operator is not supported")
		case LParen, LBrace:
			stack = append(stack, t.Kind)
		case RParen, RBrace:
			if len(stack) == 0 || t.Kind == RParen && stack[len(stack)-1] != LParen || t.Kind == RBrace && stack[len(stack)-1] != LBrace {
				return "", refuse(t.Pos, "unmatched bracket %q", t.Text)
			}
			stack = stack[:len(stack)-1]
		}
	}
	if len(stack) != 0 {
		return "", refuse(len(display), "unclosed bracket")
	}
	type binding struct {
		name       string
		start, end int
	}
	var bindings []binding
	for i, t := range toks {
		if t.Kind != Func || i+1 >= len(toks) || toks[i+1].Kind != LParen {
			continue
		}
		_, base := SplitPrefix(t.Text)
		lambda, let := strings.EqualFold(base, "LAMBDA"), strings.EqualFold(base, "LET")
		if !lambda && !let {
			continue
		}
		end := closesAt(toks, i+1)
		if end < 0 {
			continue
		}
		starts := []int{i + 2}
		depth := 0
		for j := i + 2; j < end; j++ {
			switch toks[j].Kind {
			case LParen, LBrace:
				depth++
			case RParen, RBrace:
				depth--
			case Comma:
				if depth == 0 {
					starts = append(starts, j+1)
				}
			}
		}
		for arg, start := range starts {
			if arg == len(starts)-1 || let && arg%2 != 0 {
				continue
			}
			for start < end && toks[start].Kind == Space {
				start++
			}
			if start >= end || toks[start].Kind != Name {
				continue
			}
			next := start + 1
			for next < end && toks[next].Kind == Space {
				next++
			}
			if next >= end || toks[next].Kind != Comma {
				continue
			}
			_, name := SplitPrefix(toks[start].Text)
			bindings = append(bindings, binding{name, start, end})
		}
	}
	var b strings.Builder
	for i, t := range toks {
		text := t.Text
		if t.Kind == Func {
			pre, rest, ok := SplitQualified(text)
			if ok {
				prefix, _ := SplitPrefix(rest)
				if prefix == "" && stripFuncPrefix(rest) == rest {
					if f, found := LookupFunction(rest); found {
						text = pre.Text + f.Prefix + rest
					}
				}
			}
		} else if t.Kind == Name {
			pre, rest, ok := SplitQualified(text)
			if ok && pre.Text == "" {
				prefix, _ := SplitPrefix(rest)
				if prefix == "" {
					for _, binding := range bindings {
						if i >= binding.start && i < binding.end && strings.EqualFold(rest, binding.name) {
							text = PrefixParam + rest
							break
						}
					}
				}
			}
		}
		b.WriteString(text)
	}
	return StoreAt(b.String())
}

// Spills returns the first function in the stored formula that
// IsDynamicArray says spills on its own, and whether there is one:
// writes refuse such a formula (decision 8). It looks at Func tokens
// only, so a string literal "SEQUENCE(" is not a call.
func Spills(stored string) (name string, ok bool) {
	for _, t := range Lex(stored) {
		if t.Kind != Func {
			continue
		}
		_, name, ok := SplitQualified(t.Text)
		if ok && IsDynamicArray(name) {
			return name, true
		}
	}
	return "", false
}
