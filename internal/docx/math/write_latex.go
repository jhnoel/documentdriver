package math

import (
	"strconv"
	"strings"
	"unicode"

	"github.com/jhnoel/documentdriver/internal/ascii"
)

// Writing context for a list.
type writeCtx int

const (
	ctxMath  writeCtx = iota // any math list
	ctxEqArr                 // a row of an equation array: & is an alignment mark
	ctxSplit                 // an m:oMath with line breaks: \\ and & are breaks and alignment points
)

// writeEquation writes the model in the canonical LaTeX of the grammar.
func writeEquation(eq equation) string {
	lines := make([]string, len(eq.lines))
	for i, l := range eq.lines {
		lines[i] = writeLine(l)
	}
	return strings.Join(lines, ` \\ `)
}

// writeLine writes one m:oMath: in a split environment when it holds
// line breaks or alignment points.
func writeLine(l list) string {
	for _, n := range l {
		switch n.(type) {
		case lineBreak, alignPoint:
			return `\begin{split}` + writeList(l, ctxSplit) + `\end{split}`
		}
	}
	return writeList(l, ctxMath)
}

// join concatenates LaTeX pieces, putting a space between a control word
// and a following letter.
func join(parts []string) string {
	var b strings.Builder
	for _, p := range parts {
		if p == "" {
			continue
		}
		if endsWithControlWord(b.String()) && ascii.IsAlpha(rune(p[0])) {
			b.WriteByte(' ')
		}
		b.WriteString(p)
	}
	return b.String()
}

// endsWithControlWord reports whether s ends with an unescaped backslash and letters.
func endsWithControlWord(s string) bool {
	i := len(s)
	for i > 0 && ascii.IsAlpha(rune(s[i-1])) {
		i--
	}
	if i == len(s) || i == 0 || s[i-1] != '\\' {
		return false
	}
	// An even number of backslashes before it means the backslash is
	// itself escaped (\\ then letters).
	k := i - 1
	for k > 0 && s[k-1] == '\\' {
		k--
	}
	return (i-1-k)%2 == 0
}

// writeList writes each node of l to LaTeX, passing the following node's text as context.
func writeList(l list, ctx writeCtx) string {
	parts := make([]string, len(l))
	for i := len(l) - 1; i >= 0; i-- {
		next := ""
		if i+1 < len(l) {
			next = parts[i+1]
		}
		parts[i] = writeNode(l[i], ctx, next)
	}
	return join(parts)
}

// arg returns list l written as a braced LaTeX argument.
func arg(l list) string { return "{" + writeList(l, ctxMath) + "}" }

// writeNode writes node n to LaTeX in context ctx, given the next node's text.
func writeNode(n node, ctx writeCtx, next string) string {
	switch n := n.(type) {
	case run:
		return writeRun(n, ctx, next)
	case frac:
		switch n.kind {
		case fracNoBar:
			return `\genfrac{}{}{0pt}{}` + arg(n.num) + arg(n.den)
		case fracSkw:
			return `\sfrac` + arg(n.num) + arg(n.den)
		case fracLin:
			return `\docxlinfrac` + arg(n.num) + arg(n.den)
		}
		return `\frac` + arg(n.num) + arg(n.den)
	case script:
		return writeScripts(baseOf(n.base), n.sub, n.sup, n.hasSub, n.hasSup)
	case prescript:
		return `\prescript` + arg(n.sup) + arg(n.sub) + arg(n.base)
	case nary:
		name := `\mathop{` + writeChar(firstRune(n.chr), ctxMath) + "}"
		if c, ok := naryNames[n.chr]; ok {
			name = `\` + c
		} else if len([]rune(n.chr)) != 1 {
			name = `\mathop{` + writeRunText(n.chr, ctxMath) + "}"
		}
		switch n.limLoc {
		case "undOvr":
			name += `\limits`
		case "subSup":
			name += `\nolimits`
		}
		s := name
		if n.hasSub {
			s += "_" + arg(n.sub)
		}
		if n.hasSup {
			s += "^" + arg(n.sup)
		}
		return s + arg(n.body)
	case radical:
		if !n.hasDeg {
			return `\sqrt` + arg(n.body)
		}
		deg := writeList(n.deg, ctxMath)
		if strings.Contains(deg, "]") {
			deg = "{" + deg + "}"
		}
		return `\sqrt[` + deg + "]" + arg(n.body)
	case delim:
		return writeDelim(n)
	case matrix:
		return writeMatrix("matrix", n)
	case eqArr:
		return writeEqArr("aligned", n)
	case accent:
		if c, ok := accentNames[n.chr]; ok {
			return `\` + c + arg(n.base)
		}
		return `\docxaccent{` + writeRunText(n.chr, ctxMath) + "}" + arg(n.base)
	case bar:
		if n.top {
			return `\overline` + arg(n.base)
		}
		return `\underline` + arg(n.base)
	case group:
		for name, g := range groupNames {
			if g.chr == n.chr && g.top == n.top {
				return `\` + name + arg(n.base)
			}
		}
		if n.top {
			return `\docxovergroup{` + writeRunText(n.chr, ctxMath) + "}" + arg(n.base)
		}
		return `\docxundergroup{` + writeRunText(n.chr, ctxMath) + "}" + arg(n.base)
	case limit:
		return writeLimit(n)
	case function:
		return writeFunctionName(n.name) + arg(n.arg)
	case phantom:
		return writePhantom(n)
	case borderBox:
		return writeBorderBox(n)
	case box:
		var fl []string
		for _, f := range []struct {
			on   bool
			name string
		}{{n.opEmu, "opEmu"}, {n.noBreak, "noBreak"}, {n.diff, "diff"}} {
			if f.on {
				fl = append(fl, f.name)
			}
		}
		if len(fl) == 0 {
			return arg(n.base)
		}
		return `\docxbox[` + strings.Join(fl, ",") + "]" + arg(n.base)
	case lineBreak:
		if n.alnAt > 0 {
			return ` \\\docxalnat{` + strconv.Itoa(n.alnAt) + "} "
		}
		return ` \\ `
	case alignPoint:
		return "&"
	}
	return ""
}

// firstRune returns the first rune of s, or 0 when s is empty.
func firstRune(s string) rune {
	for _, r := range s {
		return r
	}
	return 0
}

// absorbsScripts reports whether a script after the node's LaTeX would
// belong to the node rather than make a script of it, or whether the
// node's LaTeX does not stand as one atom; such a base is braced.
func singleAtom(l list) bool {
	if len(l) != 1 {
		return false
	}
	switch n := l[0].(type) {
	case run:
		if n.plain || n.lit {
			return true // \text{...} or \docxlit{...}
		}
		segs := segments(n)
		if len(segs) != 1 {
			return false
		}
		g := segs[0]
		switch {
		case len([]rune(g.text)) == 1:
			return true
		case g.sty == "p" && g.scr == "" && limitNames[g.text]:
			return false // \lim takes the script as a limit
		case g.sty == "p" && g.scr == "" && operatorNames[g.text]:
			return true
		}
		return g.sty != "" || g.scr != ""
	case frac, radical, delim, matrix, accent, bar, phantom, borderBox, prescript, function, eqArr:
		return true
	}
	return false
}

// baseOf writes a script base: braced unless it is one atom.
func baseOf(l list) string {
	if singleAtom(l) {
		return writeList(l, ctxMath)
	}
	return arg(l)
}

// writeScripts returns base with subscript and superscript, writing prime-only superscripts as primes.
func writeScripts(base string, sub, sup list, hasSub, hasSup bool) string {
	s := base
	if hasSup && !hasSub && isPrimes(sup) {
		return s + strings.Repeat("'", len([]rune(sup[0].(run).text)))
	}
	if hasSub {
		s += "_" + arg(sub)
	}
	if hasSup {
		s += "^" + arg(sup)
	}
	return s
}

// isPrimes reports whether l is only prime characters, written x'.
func isPrimes(l list) bool {
	if len(l) != 1 {
		return false
	}
	r, ok := l[0].(run)
	return ok && !r.plain && r.scr == "" && r.sty == "p" && strings.Trim(r.text, "\u2032") == ""
}

// writeDelim writes delim d as a binom, matrix or cases environment, or a left-right delimited expression.
func writeDelim(d delim) string {
	if len(d.entries) == 1 && len(d.entries[0]) == 1 {
		switch inner := d.entries[0][0].(type) {
		case frac:
			if inner.kind == fracNoBar && d.beg == "(" && d.end == ")" {
				return `\binom` + arg(inner.num) + arg(inner.den)
			}
		case matrix:
			env := map[[2]string]string{
				{"(", ")"}: "pmatrix", {"[", "]"}: "bmatrix", {"{", "}"}: "Bmatrix",
				{"|", "|"}: "vmatrix", {"\u2016", "\u2016"}: "Vmatrix",
			}[[2]string{d.beg, d.end}]
			if env != "" {
				return writeMatrix(env, inner)
			}
		case eqArr:
			if d.beg == "{" && d.end == "" {
				return writeEqArr("cases", inner)
			}
		}
	}
	parts := []string{`\left`, delimToken(d.beg)}
	for i, e := range d.entries {
		if i > 0 {
			parts = append(parts, `\middle`, delimToken(d.sep))
		}
		parts = append(parts, writeList(e, ctxMath))
	}
	parts = append(parts, `\right`, delimToken(d.end))
	return join(parts)
}

// delimToken returns the LaTeX token for delimiter character c.
func delimToken(c string) string {
	if name, ok := delimiterNames[c]; ok {
		return name
	}
	if len([]rune(c)) == 1 {
		r := firstRune(c)
		return writeChar(r, ctxMath)
	}
	return `\unicode{` + c + "}"
}

// writeMatrix writes matrix m as a begin-end environment named env.
func writeMatrix(env string, m matrix) string {
	rows := make([]string, len(m.rows))
	for i, r := range m.rows {
		cells := make([]string, len(r))
		for j, c := range r {
			cells[j] = writeList(c, ctxMath)
		}
		rows[i] = strings.Join(cells, " & ")
	}
	return `\begin{` + env + "}" + strings.Join(rows, ` \\ `) + `\end{` + env + "}"
}

// writeEqArr writes eqArr a as an aligned or cases environment.
func writeEqArr(env string, a eqArr) string {
	rows := make([]string, len(a.rows))
	for i, r := range a.rows {
		rows[i] = writeList(r, ctxEqArr)
	}
	return `\begin{` + env + "}" + strings.Join(rows, ` \\ `) + `\end{` + env + "}"
}

// writeLimit writes limit n as a named limit script or \underset/\overset.
func writeLimit(n limit) string {
	if len(n.base) == 1 {
		switch b := n.base[0].(type) {
		case group:
			for name, g := range groupNames {
				if g.chr == b.chr && g.top == b.top && g.top == n.upper {
					if n.upper {
						return `\` + name + arg(b.base) + "^" + arg(n.lim)
					}
					return `\` + name + arg(b.base) + "_" + arg(n.lim)
				}
			}
		case run:
			if !b.plain && b.sty == "p" && b.scr == "" && limitNames[b.text] {
				if n.upper {
					return `\` + b.text + "^" + arg(n.lim)
				}
				return `\` + b.text + "_" + arg(n.lim)
			}
		}
	}
	if n.upper {
		return `\overset` + arg(n.lim) + arg(n.base)
	}
	return `\underset` + arg(n.lim) + arg(n.base)
}

// operatorName returns the operator name a list is, if it is one upright
// run of letters.
func operatorName(l list) (string, bool) {
	if len(l) != 1 {
		return "", false
	}
	r, ok := l[0].(run)
	if !ok || r.plain || r.sty != "p" || r.scr != "" || r.text == "" || !allRunes(r.text, ascii.IsAlpha) {
		return "", false
	}
	return r.text, true
}

// nameCommand returns the LaTeX command for name, or \operatorname{name} for other names.
func nameCommand(name string) string {
	if operatorNames[name] || limitNames[name] {
		return `\` + name
	}
	return `\operatorname{` + name + "}"
}

// writeFunctionName writes function-name list l as an operator, limit or \docxfunc.
func writeFunctionName(l list) string {
	if name, ok := operatorName(l); ok {
		return nameCommand(name)
	}
	if len(l) == 1 {
		switch n := l[0].(type) {
		case limit:
			if name, ok := operatorName(n.base); ok && limitNames[name] {
				return writeLimit(n)
			}
		case script:
			if name, ok := operatorName(n.base); ok && !limitNames[name] {
				return writeScripts(nameCommand(name), n.sub, n.sup, n.hasSub, n.hasSup)
			}
		}
	}
	return `\docxfunc` + arg(l)
}

// writePhantom writes phantom n as \phantom, \vphantom, \hphantom, \smash or \docxphantom.
func writePhantom(n phantom) string {
	switch {
	case n.transp:
	case !n.show && !n.zeroWid && !n.zeroAsc && !n.zeroDesc:
		return `\phantom` + arg(n.base)
	case !n.show && n.zeroWid && !n.zeroAsc && !n.zeroDesc:
		return `\vphantom` + arg(n.base)
	case !n.show && !n.zeroWid && n.zeroAsc && n.zeroDesc:
		return `\hphantom` + arg(n.base)
	case n.show && !n.zeroWid && n.zeroAsc && n.zeroDesc:
		return `\smash` + arg(n.base)
	}
	var fl []string
	for _, f := range []struct {
		on   bool
		name string
	}{{n.show, "show"}, {n.zeroWid, "zeroWid"}, {n.zeroAsc, "zeroAsc"}, {n.zeroDesc, "zeroDesc"}, {n.transp, "transp"}} {
		if f.on {
			fl = append(fl, f.name)
		}
	}
	return `\docxphantom[` + strings.Join(fl, ",") + "]" + arg(n.base)
}

var borderFlags = []string{"hideTop", "hideBot", "hideLeft", "hideRight", "strikeH", "strikeV", "strikeBLTR", "strikeTLBR"}

// writeBorderBox writes borderBox n as \boxed, a cancel command or \docxboxed.
func writeBorderBox(n borderBox) string {
	all := n.hide == [4]bool{true, true, true, true}
	switch {
	case n.hide == [4]bool{} && n.strike == [4]bool{}:
		return `\boxed` + arg(n.base)
	case all && n.strike == [4]bool{false, false, true, false}:
		return `\cancel` + arg(n.base)
	case all && n.strike == [4]bool{false, false, false, true}:
		return `\bcancel` + arg(n.base)
	case all && n.strike == [4]bool{false, false, true, true}:
		return `\xcancel` + arg(n.base)
	}
	var fl []string
	for i, f := range append(n.hide[:], n.strike[:]...) {
		if f {
			fl = append(fl, borderFlags[i])
		}
	}
	return `\docxboxed[` + strings.Join(fl, ",") + "]" + arg(n.base)
}

// segment is part of a run written with one wrapper.
type segment struct {
	text, sty, scr string
}

// segments splits a math run where the commands it needs change: a
// character needs a style command when its style differs from its
// default.
func segments(r run) []segment {
	var out []segment
	for _, c := range r.text {
		sty := ""
		if r.sty != defaultSty(c) {
			sty = r.sty
		}
		if n := len(out); n > 0 && out[n-1].sty == sty && out[n-1].scr == r.scr {
			out[n-1].text += string(c)
			continue
		}
		out = append(out, segment{string(c), sty, r.scr})
	}
	return out
}

// writeRun writes run r to LaTeX with style and script wrappers, \docxlit or \text.
func writeRun(r run, ctx writeCtx, next string) string {
	if r.lit {
		r.lit = false
		return `\docxlit{` + writeRun(r, ctx, "") + "}"
	}
	if r.plain {
		return writeText(r.text, ctx)
	}
	segs := segments(r)
	parts := make([]string, len(segs))
	for i, s := range segs {
		var body string
		if s.sty == "p" && s.scr == "" && (operatorNames[s.text] || limitNames[s.text]) &&
			(i < len(segs)-1 || !strings.HasPrefix(next, "{")) {
			parts[i] = `\` + s.text
			continue
		}
		body = writeRunText(s.text, ctx)
		if s.scr != "" {
			body = `\` + scriptNames[s.scr] + "{" + body + "}"
		}
		if s.sty != "" {
			body = `\` + styleNames[s.sty] + "{" + body + "}"
		}
		parts[i] = body
	}
	return join(parts)
}

// writeRunText writes math characters.
func writeRunText(text string, ctx writeCtx) string {
	var parts []string
	rs := []rune(text)
	for i := 0; i < len(rs); i++ {
		if rs[i] == '\u2003' && i+1 < len(rs) && rs[i+1] == '\u2003' {
			parts = append(parts, `\qquad`)
			i++
			continue
		}
		parts = append(parts, writeChar(rs[i], ctx))
	}
	return join(parts)
}

// writeChar writes the single math character r to LaTeX, escaping or naming it as needed.
func writeChar(r rune, ctx writeCtx) string {
	switch {
	case r == '&':
		if ctx == ctxEqArr {
			return "&"
		}
		return `\&`
	case ascii.IsAlpha(r) || ascii.IsDigit(r):
		return string(r)
	}
	if s, ok := spaceCommands[r]; ok {
		return s
	}
	if name, ok := symbolNames[r]; ok {
		return `\` + name
	}
	switch r {
	case '+', '-', '=', '<', '>', '(', ')', '[', ']', '|', '/', '*', ',', '.', ';', ':', '!', '?', '@', '"', '`':
		return string(r)
	}
	if r > 0x7f && unicode.IsPrint(r) && !unicode.Is(unicode.Mn, r) && !unicode.Is(unicode.Me, r) && !unicode.IsSpace(r) {
		return string(r)
	}
	return `\unicode{x` + strings.ToUpper(strconv.FormatInt(int64(r), 16)) + "}"
}

// writeText writes normal text as \text{...}; in an equation-array row
// an & stays an alignment mark outside it.
func writeText(text string, ctx writeCtx) string {
	if ctx == ctxEqArr && strings.Contains(text, "&") {
		pieces := strings.Split(text, "&")
		out := make([]string, len(pieces))
		for i, p := range pieces {
			if p != "" {
				out[i] = writeText(p, ctxMath)
			}
		}
		return strings.Join(out, "&")
	}
	var b strings.Builder
	b.WriteString(`\text{`)
	prevSpace := false
	for _, r := range text {
		switch r {
		case ' ':
			if prevSpace {
				b.WriteString(`\ `)
			} else {
				b.WriteByte(' ')
			}
			prevSpace = true
			continue
		case '\\':
			b.WriteString(`\textbackslash{}`)
		case '{', '}', '$', '%', '#', '&', '_':
			b.WriteString(`\` + string(r))
		case '^':
			b.WriteString(`\textasciicircum{}`)
		case '~':
			b.WriteString(`\textasciitilde{}`)
		default:
			if unicode.IsPrint(r) && !unicode.Is(unicode.Mn, r) && (r < 0x80 || !unicode.IsSpace(r)) {
				b.WriteRune(r)
			} else {
				b.WriteString(`\unicode{x` + strings.ToUpper(strconv.FormatInt(int64(r), 16)) + "}")
			}
		}
		prevSpace = false
	}
	b.WriteString("}")
	return b.String()
}
