package formula

import "strings"

// Display converts a formula as a workbook stores it (no leading "=") to
// the form Excel shows the user. It is total: it never panics, and it
// returns the input unchanged when the formula cannot be lexed cleanly
// or does not validate (Validate).
//
// The rewrites, all built on the tokens of Lex:
//
//   - The stored function prefixes "_xlfn._xlws.", "_xlfn." and "_xlws."
//     are stripped from function names, and "_xlpm." from LAMBDA and LET
//     parameter names, wherever those appear. Matching ignores case; the
//     rest of the name keeps its case.
//   - ANCHORARRAY with one reference argument becomes that reference
//     followed by the spill operator "#": "_xlfn.ANCHORARRAY(A1)" is
//     "A1#".
//   - SINGLE (stored as "_xlfn.SINGLE") with one argument becomes the
//     implicit-intersection operator "@" before the argument, which is
//     parenthesised unless it is already a self-contained operand: a
//     reference, a name, a function call, a structured reference, a
//     parenthesised expression or an array constant.
//   - A table reference's "#This Row" item is written "@":
//     "Tbl[[#This Row],[Col]]" is "Tbl[@Col]",
//     "[[#This Row],[Col 2]]" is "[@[Col 2]]" (the brackets stay when
//     the column name needs them), and "Tbl[#This Row]" is "Tbl[@]".
//
// Everything else — strings, whitespace and case — is byte-identical.
// Display is idempotent: Display(Display(s)) == Display(s).
func Display(stored string) string {
	out, _ := displayWithCounts(stored)
	return out
}

// displayWithCounts is Display that also reports how many times each
// rewrite fired, for the corpus test.
func displayWithCounts(stored string) (string, map[string]int) {
	toks := Lex(stored)
	for _, t := range toks {
		if t.Kind == Invalid {
			return stored, nil
		}
	}
	d := disp{toks: toks, counts: map[string]int{}}
	d.run()
	if !d.changed {
		return stored, d.counts
	}
	// A rewrite of a malformed formula can join tokens ("0_xlfn.E0" to
	// the number "0E0"): one that does not validate is shown as stored.
	// Only formulas a rewrite changed pay for the check.
	if Validate(stored) != nil {
		return stored, nil
	}
	return d.b.String(), d.counts
}

// functionPrefixes are the stored function-name prefixes Display strips,
// longest first so "_xlfn._xlws." wins over "_xlfn." and "_xlws.".
var functionPrefixes = [...]string{PrefixWorksheet, PrefixFuture, "_xlws."}

// stripFuncPrefix strips the stored function prefixes, ignoring case:
// all of them, so that a repeated one ("_xlfn._xlfn.X", which no file
// Excel wrote has) leaves nothing for a second Display to strip. A
// prefix is kept when what follows it does not start a name
// ("_xlfn.0X"): stripped, it would lex as other tokens.
func stripFuncPrefix(name string) string {
	for {
		stripped := name
		for _, p := range functionPrefixes {
			if len(name) > len(p) && strings.EqualFold(name[:len(p)], p) && startsName(name[len(p):]) {
				stripped = name[len(p):]
				break
			}
		}
		if stripped == name {
			return name
		}
		name = stripped
	}
}

// stripParamPrefix strips the LAMBDA/LET parameter prefix, ignoring case,
// repeated ones too (as stripFuncPrefix).
func stripParamPrefix(name string) string {
	for len(name) > len(PrefixParam) && strings.EqualFold(name[:len(PrefixParam)], PrefixParam) && startsName(name[len(PrefixParam):]) {
		name = name[len(PrefixParam):]
	}
	return name
}

// startsName reports whether s starts with a character a name may start
// with.
func startsName(s string) bool {
	for _, r := range s {
		return isNameStart(r)
	}
	return false
}

// disp renders a token slice, rewriting as it goes. Its fields are all
// set before use; changed reports whether any token differs from its
// input text, and counts tallies the rewrites.
type disp struct {
	toks    []Token
	i       int
	b       strings.Builder
	changed bool
	counts  map[string]int
}

// count records one rewrite of kind key in d.counts, when present.
func (d *disp) count(key string) {
	if d.counts != nil {
		d.counts[key]++
	}
}

// run renders the remaining tokens into d.b.
func (d *disp) run() {
	for d.i < len(d.toks) {
		d.step()
	}
}

// step renders one token (or one whole ANCHORARRAY/SINGLE call).
func (d *disp) step() {
	t := d.toks[d.i]
	switch t.Kind {
	case Func:
		d.funcTok()
	case Name:
		pre, rest, ok := SplitQualified(t.Text)
		if !ok {
			d.b.WriteString(t.Text)
		} else if s := stripParamPrefix(rest); s != rest {
			d.changed = true
			d.count("param prefix")
			d.b.WriteString(pre.Text)
			d.b.WriteString(s)
		} else {
			d.b.WriteString(t.Text)
		}
		d.i++
	case StructRef:
		if s := displayStructRef(t.Text); s != t.Text {
			d.changed = true
			d.count("struct ref")
			d.b.WriteString(s)
		} else {
			d.b.WriteString(t.Text)
		}
		d.i++
	default:
		d.b.WriteString(t.Text)
		d.i++
	}
}

// funcTok renders a function token, rewriting ANCHORARRAY and SINGLE
// calls of the right shape and stripping the stored prefix otherwise.
func (d *disp) funcTok() {
	t := d.toks[d.i]
	pre, rest, ok := SplitQualified(t.Text)
	if !ok {
		d.b.WriteString(t.Text)
		d.i++
		return
	}
	base := stripFuncPrefix(rest)
	if pre.Text == "" && (strings.EqualFold(base, "ANCHORARRAY") || strings.EqualFold(base, "SINGLE")) {
		if d.i+1 < len(d.toks) && d.toks[d.i+1].Kind == LParen {
			if cl := closesAt(d.toks, d.i+1); cl >= 0 {
				arg := d.toks[d.i+2 : cl]
				if singleArg(arg) {
					if strings.EqualFold(base, "ANCHORARRAY") && len(arg) == 1 && isRefArg(arg[0]) {
						d.changed = true
						d.count("anchorarray")
						d.b.WriteString(arg[0].Text)
						d.b.WriteByte('#')
						d.i = cl + 1
						return
					}
					if strings.EqualFold(base, "SINGLE") {
						d.changed = true
						d.count("single")
						argStr := d.renderSlice(arg)
						d.b.WriteByte('@')
						if !selfContained(arg) {
							d.b.WriteByte('(')
							d.b.WriteString(argStr)
							d.b.WriteByte(')')
						} else {
							d.b.WriteString(argStr)
						}
						d.i = cl + 1
						return
					}
				}
			}
		}
	}
	if base != rest {
		d.changed = true
		d.count("func prefix")
	}
	d.b.WriteString(pre.Text)
	d.b.WriteString(base)
	d.i++
}

// renderSlice renders a token slice on its own, sharing d.counts so the
// rewrites inside a SINGLE argument are tallied too.
func (d *disp) renderSlice(toks []Token) string {
	sub := disp{toks: toks, counts: d.counts}
	sub.run()
	return sub.b.String()
}

// singleArg reports whether toks holds exactly one function argument: it
// is not empty and has no top-level comma.
func singleArg(toks []Token) bool {
	if len(toks) == 0 {
		return false
	}
	depth := 0
	for _, t := range toks {
		switch t.Kind {
		case LParen, LBrace:
			depth++
		case RParen, RBrace:
			depth--
		case Comma:
			if depth == 0 {
				return false
			}
		}
	}
	return true
}

// closesAt returns the index of the bracket matching the LParen or LBrace
// at i, or -1. It counts only that bracket's kind.
func closesAt(toks []Token, i int) int {
	if i >= len(toks) {
		return -1
	}
	var open, closing Kind
	switch toks[i].Kind {
	case LParen:
		open, closing = LParen, RParen
	case LBrace:
		open, closing = LBrace, RBrace
	default:
		return -1
	}
	depth := 0
	for j := i; j < len(toks); j++ {
		switch toks[j].Kind {
		case open:
			depth++
		case closing:
			depth--
			if depth == 0 {
				return j
			}
		}
	}
	return -1
}

// selfContained reports whether the argument needs no parentheses after
// "@": a single reference, name or structured reference, a function call,
// a parenthesised expression or an array constant.
func selfContained(toks []Token) bool {
	if len(toks) == 0 {
		return false
	}
	switch toks[0].Kind {
	case Ref, Name, StructRef:
		return len(toks) == 1
	case LParen, LBrace:
		return closesAt(toks, 0) == len(toks)-1
	case Func:
		return len(toks) >= 2 && toks[1].Kind == LParen && closesAt(toks, 1) == len(toks)-1
	}
	return false
}

// isRefArg reports whether t is a reference token that ANCHORARRAY may
// turn into "t#": a real reference, not #REF!.
func isRefArg(t Token) bool {
	if t.Kind != Ref {
		return false
	}
	_, body, ok := SplitQualified(t.Text)
	if !ok {
		return false
	}
	_, isErr, ok := ParseRefBody(body)
	return ok && !isErr
}

// displayStructRef rewrites a table reference's "#This Row" item to the
// "@" display form.
func displayStructRef(text string) string {
	open := strings.IndexByte(text, '[')
	if open < 0 {
		return text
	}
	table, spec := text[:open], text[open:]

	// A lone "#This Row" item: "Tbl[#This Row]" -> "Tbl[@]".
	if strings.EqualFold(spec, "[#This Row]") {
		return table + "[@]"
	}

	// "#This Row" as the first item of a multi-item specifier:
	// "Tbl[[#This Row],[Col]]" -> "Tbl[@Col]".
	const kw = "[#This Row],"
	idx := indexFold(spec, kw)
	if idx != 1 {
		return text
	}
	// Excel's "space after comma" option stores "[#This Row], [Col]".
	rest := strings.TrimLeft(spec[idx+len(kw):], " ")
	if rest == "" || rest[len(rest)-1] != ']' {
		return text
	}
	inner := rest[:len(rest)-1]
	if isSimpleColumnItem(inner) {
		return table + "[@" + inner[1:len(inner)-1] + "]"
	}
	return table + "[@" + inner + "]"
}

// isSimpleColumnItem reports whether inner is a single bracketed column
// whose name needs no brackets after "@": "[Col]" is simple, "[Col 2]"
// is not.
func isSimpleColumnItem(inner string) bool {
	if len(inner) < 2 || inner[0] != '[' || inner[len(inner)-1] != ']' {
		return false
	}
	return simpleColumnName(inner[1 : len(inner)-1])
}

// simpleColumnName reports whether s is a bare defined-name-shaped
// identifier (letters, digits, "_", "\", "." and "?").
func simpleColumnName(s string) bool {
	if s == "" {
		return false
	}
	for i, r := range s {
		if i == 0 {
			if !isNameStart(r) {
				return false
			}
		} else if !isNameChar(r) {
			return false
		}
	}
	return true
}

// indexFold returns the index of the first occurrence of sub in s,
// ignoring case, or -1.
func indexFold(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if strings.EqualFold(s[i:i+len(sub)], sub) {
			return i
		}
	}
	return -1
}
