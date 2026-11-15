package formula

import "strings"

// StoreAt rewrites the implicit-intersection forms of a formula to their
// stored forms (chunk K4b): "@" before an operand becomes
// _xlfn.SINGLE(operand) (the operand's extent as Display's SINGLE rule
// reads it: a reference, a name, a call, a structured reference, a
// parenthesised expression or an array constant; anything else is
// parenthesised), and "@" inside a structured reference becomes
// "[#This Row]" ("Tbl[@Col]" is "Tbl[[#This Row],[Col]]", "[@[Col 2]]"
// is "[[#This Row],[Col 2]]", "Tbl[@]" is "Tbl[#This Row]"). Every other
// byte is copied. The error is a *Refusal ("@" with no operand).
func StoreAt(s string) (string, error) {
	toks := Lex(s)
	ends := make([]int, len(toks)+1)
	var b strings.Builder
	for i, t := range toks {
		switch t.Kind {
		case At:
			j := i + 1
			for j < len(toks) && toks[j].Kind == Space {
				j++
			}
			start := j
			for j < len(toks) && toks[j].Kind == Op && (toks[j].Text == "+" || toks[j].Text == "-") {
				j++
				for j < len(toks) && toks[j].Kind == Space {
					j++
				}
			}
			if j == len(toks) || !startsOperand(toks[j].Kind) {
				return "", refuse(t.Pos, "@ without an operand")
			}
			end := j + 1
			switch toks[j].Kind {
			case Func:
				if end < len(toks) && toks[end].Kind == LParen {
					if cl := closesAt(toks, end); cl >= 0 {
						end = cl + 1
					}
				}
			case LParen, LBrace:
				if cl := closesAt(toks, j); cl >= 0 {
					end = cl + 1
				}
			}
			b.WriteString("_xlfn.SINGLE(")
			if start != j || !selfContained(toks[j:end]) {
				b.WriteByte('(')
				ends[end]++
			}
			ends[end]++
		case StructRef:
			b.WriteString(storeAtStruct(t.Text))
		default:
			b.WriteString(t.Text)
		}
		for range ends[i+1] {
			b.WriteByte(')')
		}
	}
	return b.String(), nil
}

// storeAtStruct reverses Display's leading this-row shorthand only.
func storeAtStruct(text string) string {
	open := strings.IndexByte(text, '[')
	if open < 0 || open+2 >= len(text) || text[open+1] != '@' {
		return text
	}
	table, inner := text[:open], text[open+2:len(text)-1]
	if inner == "" {
		return table + "[#This Row]"
	}
	if inner[0] != '[' {
		inner = "[" + inner + "]"
	}
	return table + "[[#This Row]," + inner + "]"
}
