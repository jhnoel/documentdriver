package math

import (
	"errors"
	"fmt"
	"maps"
	"slices"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/jhnoel/documentdriver/internal/ascii"
)

// ErrInvalidLaTeX is wrapped by every LaTeX parse error.
var ErrInvalidLaTeX = errors.New("invalid LaTeX")

// maxLaTeXDepth bounds nesting of groups, arguments and environments.
const maxLaTeXDepth = 256

// style is the style a style command puts on the runs inside it.
type style struct {
	sty, scr string
	lit      bool
}

type parser struct {
	s     []rune
	pos   int
	depth int
}

// errorf returns a formatted parsing error wrapping ErrInvalidLaTeX at the current position.
func (p *parser) errorf(format string, args ...any) error {
	return fmt.Errorf("%w at character %d: %s", ErrInvalidLaTeX, p.pos, fmt.Sprintf(format, args...))
}

// parseEquation parses LaTeX in the grammar into the model. display
// allows several lines separated by \\.
func parseEquation(src string, display bool) (equation, error) {
	if !utf8.ValidString(src) {
		return equation{}, fmt.Errorf("%w: not UTF-8", ErrInvalidLaTeX)
	}
	for i, r := range src {
		if !xmlChar(r) {
			return equation{}, fmt.Errorf("%w: character %U at byte %d cannot appear in an equation", ErrInvalidLaTeX, r, i)
		}
	}
	p := &parser{s: []rune(src)}
	eq := equation{display: display}
	for {
		l, err := p.parseList(style{}, termLine)
		if err != nil {
			return equation{}, err
		}
		eq.lines = append(eq.lines, l)
		switch {
		case p.eof():
			return eq, nil
		case p.lookCommand("\\"):
			if !display {
				return equation{}, p.errorf(`\\ starts a new line, which only display math ($$...$$) has`)
			}
			p.pos += 2
		default:
			return equation{}, p.unexpectedIn("the end")
		}
	}
}

// xmlChar reports whether r may appear in XML 1.0 character data.
func xmlChar(r rune) bool {
	switch {
	case r == '\t' || r == '\n' || r == '\r':
		return true
	case r < 0x20, r >= 0xd800 && r <= 0xdfff, r == 0xfffe, r == 0xffff:
		return false
	}
	return r <= 0x10ffff
}

// eof reports whether the parser is at the end of the input.
func (p *parser) eof() bool { return p.pos >= len(p.s) }

// peek returns the current rune, or 0 at the end of the input.
func (p *parser) peek() rune {
	if p.eof() {
		return 0
	}
	return p.s[p.pos]
}

// skipSpace advances the parser past spaces, line ends and comments.
func (p *parser) skipSpace() {
	for !p.eof() {
		switch p.s[p.pos] {
		case ' ', '\t', '\n', '\r':
			p.pos++
		case '%':
			for !p.eof() && p.s[p.pos] != '\n' {
				p.pos++
			}
		default:
			return
		}
	}
}

// lookCommand reports whether a command with the given name starts at
// the current position (after spaces).
func (p *parser) lookCommand(name string) bool {
	p.skipSpace()
	if p.peek() != '\\' {
		return false
	}
	n, end := p.commandAt(p.pos)
	return n == name && end > p.pos
}

// commandAt reads the command name at i (a backslash): letters, or one
// other character.
func (p *parser) commandAt(i int) (string, int) {
	if i >= len(p.s) || p.s[i] != '\\' {
		return "", i
	}
	j := i + 1
	if j >= len(p.s) {
		return "", j
	}
	if !ascii.IsAlpha(p.s[j]) {
		return string(p.s[j]), j + 1
	}
	for j < len(p.s) && ascii.IsAlpha(p.s[j]) {
		j++
	}
	return string(p.s[i+1 : j]), j
}

// readCommand consumes a command and returns its name.
func (p *parser) readCommand() string {
	name, end := p.commandAt(p.pos)
	p.pos = end
	return name
}

// unexpected returns an error describing the unexpected token or end of input.
func (p *parser) unexpected() error {
	if p.eof() {
		return p.errorf("unexpected end")
	}
	if p.peek() == '\\' {
		name, _ := p.commandAt(p.pos)
		return p.errorf(`unexpected \%s here`, name)
	}
	return p.errorf("unexpected %q", p.peek())
}

// Where a list ends.
type term int

const (
	termLine  term = iota // a top-level line: ends at \\ or the end
	termGroup             // a braced group: ends at }
	termCell              // an environment cell: ends at &, \\ or \end
	termDelim             // between \left and \right: ends at \middle or \right
	termOpt               // an optional argument: ends at ]
)

// atStop reports whether the list being read ends here.
func (p *parser) atStop(t term) bool {
	p.skipSpace()
	if p.eof() {
		return true
	}
	c := p.peek()
	switch {
	case c == '}' || c == '&':
		return true
	case c == ']' && t == termOpt:
		return true
	case c == '\\':
		name, _ := p.commandAt(p.pos)
		switch name {
		case "\\", "end", "right", "middle":
			return true
		}
	}
	return false
}

// parseList reads items until the list ends; the caller checks how.
func (p *parser) parseList(st style, t term) (list, error) {
	p.depth++
	defer func() { p.depth-- }()
	if p.depth > maxLaTeXDepth {
		return nil, p.errorf("nested more than %d deep", maxLaTeXDepth)
	}
	var l list
	for !p.atStop(t) {
		var err error
		if l, err = p.parseItem(l, st, t == termLine); err != nil {
			return nil, err
		}
	}
	return l, nil
}

// group reads a braced group.
func (p *parser) group(st style) (list, error) {
	p.skipSpace()
	if p.peek() != '{' {
		return nil, p.errorf("expected {")
	}
	p.pos++
	l, err := p.parseList(st, termGroup)
	if err != nil {
		return nil, err
	}
	if p.peek() != '}' {
		return nil, p.unexpectedIn("}")
	}
	p.pos++
	return l, nil
}

// unexpectedIn returns an error for the unexpected token where want was expected.
func (p *parser) unexpectedIn(want string) error {
	if p.eof() {
		return p.errorf("missing %s", want)
	}
	if p.peek() == '\\' {
		if name, _ := p.commandAt(p.pos); name == "right" || name == "middle" || name == "end" {
			if want == "the end" || want == "}" {
				return p.errorf(`\%s without its \%s`, name, map[string]string{"right": "left", "middle": "left", "end": "begin"}[name])
			}
		}
	}
	if p.peek() == '&' {
		return p.errorf("& is only allowed in matrix, aligned, cases and split")
	}
	if p.lookCommand("\\") {
		return p.errorf(`\\ is only allowed between the lines of display math and in matrix, aligned, cases and split`)
	}
	return p.unexpected()
}

// argument reads a command argument: a braced group or a single atom.
func (p *parser) argument(st style) (list, error) {
	p.skipSpace()
	if p.peek() == '{' {
		return p.group(st)
	}
	if p.atStop(termGroup) {
		return nil, p.errorf("missing argument")
	}
	a, err := p.parseAtom(st)
	if err != nil {
		return nil, err
	}
	return a.nodes, nil
}

// optional reads [..] if present.
func (p *parser) optional(st style) (list, bool, error) {
	p.skipSpace()
	if p.peek() != '[' {
		return nil, false, nil
	}
	p.pos++
	l, err := p.parseList(st, termOpt)
	if err != nil {
		return nil, false, err
	}
	if p.peek() != ']' {
		return nil, false, p.unexpectedIn("]")
	}
	p.pos++
	if len(l) == 1 {
		if b, ok := l[0].(box); ok && !b.opEmu && !b.noBreak && !b.diff {
			l = b.base
		}
	}
	return l, true, nil
}

// flagList reads an optional [a,b,...] of flag names from allowed.
func (p *parser) flagList(allowed ...string) (map[string]bool, error) {
	out := map[string]bool{}
	p.skipSpace()
	if p.peek() != '[' {
		return out, nil
	}
	end := p.pos + 1
	for end < len(p.s) && p.s[end] != ']' {
		end++
	}
	if end >= len(p.s) {
		return nil, p.errorf("missing ]")
	}
	body := string(p.s[p.pos+1 : end])
	p.pos = end + 1
	for _, f := range strings.Split(body, ",") {
		f = strings.TrimSpace(f)
		if f == "" {
			continue
		}
		ok := false
		for _, a := range allowed {
			ok = ok || a == f
		}
		if !ok {
			return nil, p.errorf("unknown flag %q (allowed: %s)", f, strings.Join(allowed, ", "))
		}
		out[f] = true
	}
	return out, nil
}

// charArg reads an argument that must be characters (or none): an
// accent, grouping or operator character.
func (p *parser) charArg() (string, error) {
	l, err := p.argument(style{})
	if err != nil {
		return "", err
	}
	var b strings.Builder
	for _, n := range l {
		r, ok := n.(run)
		if !ok || r.plain {
			return "", p.errorf("expected a character")
		}
		b.WriteString(r.text)
	}
	return b.String(), nil
}

// atom is what parseAtom read, before scripts.
type atom struct {
	nodes  list
	kind   atomKind
	braced bool   // a braced group: a box unless it takes scripts
	chr    string // the operator of a large operator
	limLoc string
}

type atomKind int

const (
	atomPlain atomKind = iota
	atomBigOp
	atomName      // an operator name: \sin, \operatorname{f}
	atomLimitName // a name taking limits: \lim, \operatorname*{f}
	atomGroupChr  // \underbrace and the like: a script becomes a limit
)

// parseItem reads one item with its scripts and appends it to l.
func (p *parser) parseItem(l list, st style, top bool) (list, error) {
	p.skipSpace()
	if p.peek() == '\\' {
		if name, _ := p.commandAt(p.pos); name == "begin" {
			return p.environment(l, st, top)
		}
	}
	var a atom
	switch p.peek() {
	case '_', '^', '\'':
		a = atom{} // a script on an empty base
	default:
		var err error
		if a, err = p.parseAtom(st); err != nil {
			return nil, err
		}
	}
	return p.postfix(l, a, st)
}

// postfix appends atom a with its limits, primes and scripts to l, erroring on double scripts.
func (p *parser) postfix(l list, a atom, st style) (list, error) {
	if a.kind == atomBigOp {
		for {
			switch {
			case p.lookCommand("limits"):
				p.readCommand()
				a.limLoc = "undOvr"
				continue
			case p.lookCommand("nolimits"):
				p.readCommand()
				a.limLoc = "subSup"
				continue
			}
			break
		}
	}
	primes := 0
	var sub, sup list
	hasSub, hasSup := false, false
	for {
		p.skipSpace()
		c := p.peek()
		switch c {
		case '\'':
			if hasSup {
				return nil, p.errorf("double superscript")
			}
			p.pos++
			primes++
			continue
		case '_', '^':
			if (c == '_' && hasSub) || (c == '^' && hasSup) {
				return nil, p.errorf("double %s", map[rune]string{'_': "subscript", '^': "superscript"}[c])
			}
			p.pos++
			s, err := p.argument(st)
			if err != nil {
				return nil, err
			}
			if c == '_' {
				sub, hasSub = s, true
			} else {
				sup, hasSup = s, true
			}
			continue
		}
		break
	}
	if primes > 0 {
		pr := list{run{text: strings.Repeat("\u2032", primes), sty: "p"}}
		sup, hasSup = appendAll(pr, sup), true
	}
	scripted := hasSub || hasSup
	switch a.kind {
	case atomBigOp:
		n := nary{chr: a.chr, limLoc: a.limLoc, sub: sub, sup: sup, hasSub: hasSub && len(sub) > 0, hasSup: hasSup && len(sup) > 0}
		body, err := p.naryBody(st)
		if err != nil {
			return nil, err
		}
		n.body = body
		return append(l, n), nil
	case atomName, atomLimitName:
		var name list
		switch {
		case a.kind == atomLimitName && hasSub && hasSup:
			name = list{limit{upper: true, base: list{limit{base: a.nodes, lim: sub}}, lim: sup}}
		case a.kind == atomLimitName && hasSub:
			name = list{limit{base: a.nodes, lim: sub}}
		case a.kind == atomLimitName && hasSup:
			name = list{limit{upper: true, base: a.nodes, lim: sup}}
		case scripted:
			name = list{script{base: a.nodes, sub: sub, sup: sup, hasSub: hasSub, hasSup: hasSup}}
		default:
			name = a.nodes
		}
		p.skipSpace()
		if p.peek() == '{' {
			arg, err := p.group(st)
			if err != nil {
				return nil, err
			}
			return append(l, function{name: name, arg: arg}), nil
		}
		return appendAll(l, name), nil
	case atomGroupChr:
		g := a.nodes[0].(group)
		switch {
		case !g.top && hasSub && !hasSup:
			return append(l, limit{base: a.nodes, lim: sub}), nil
		case g.top && hasSup && !hasSub && primes == 0:
			return append(l, limit{upper: true, base: a.nodes, lim: sup}), nil
		}
	}
	if !scripted {
		if a.braced {
			return append(l, box{base: a.nodes}), nil
		}
		return appendAll(l, a.nodes), nil
	}
	return append(l, script{base: a.nodes, sub: sub, sup: sup, hasSub: hasSub, hasSup: hasSup}), nil
}

// appendAll appends nodes, merging runs.
func appendAll(l list, nodes list) list {
	for _, n := range nodes {
		if r, ok := n.(run); ok {
			l = appendRun(l, r)
		} else {
			l = append(l, n)
		}
	}
	return l
}

// naryBody reads the operand of a large operator: a braced group, or the
// items up to an additive or relational operator at this level.
func (p *parser) naryBody(st style) (list, error) {
	p.skipSpace()
	if p.peek() == '{' {
		return p.group(st)
	}
	var body list
	for !p.atStop(termGroup) {
		if len(body) > 0 && p.atBoundary() {
			break
		}
		var err error
		if body, err = p.parseItem(body, st, false); err != nil {
			return nil, err
		}
	}
	return body, nil
}

var boundaryCommands = map[string]bool{
	"pm": true, "mp": true, "leq": true, "geq": true, "le": true, "ge": true, "neq": true, "ne": true,
	"approx": true, "equiv": true, "lt": true, "gt": true, "sim": true, "to": true,
}

// atBoundary reports whether the next token ends a large operator's body.
func (p *parser) atBoundary() bool {
	p.skipSpace()
	switch c := p.peek(); c {
	case '+', '-', '=', '<', '>', ',', ';', '\u2212', '\u00b1', '\u2213', '\u2264', '\u2265', '\u2260', '\u2248', '\u2261':
		return true
	case '\\':
		name, _ := p.commandAt(p.pos)
		return boundaryCommands[name]
	}
	return false
}

// charRun makes the run for one character in style st.
func charRun(c string, st style) run {
	sty := st.sty
	if sty == "" {
		sty = defaultSty(firstRune(c))
	}
	return run{text: c, sty: sty, scr: st.scr, lit: st.lit}
}

// runsOf returns the runs of text in style st, merged by effective style.
func runsOf(text string, st style) list {
	var l list
	for _, r := range text {
		l = appendRun(l, charRun(string(r), st))
	}
	return l
}

// parseAtom reads one token or command with its arguments.
func (p *parser) parseAtom(st style) (atom, error) {
	p.depth++
	defer func() { p.depth-- }()
	if p.depth > maxLaTeXDepth {
		return atom{}, p.errorf("nested more than %d deep", maxLaTeXDepth)
	}
	p.skipSpace()
	if p.eof() {
		return atom{}, p.errorf("missing argument")
	}
	c := p.peek()
	switch c {
	case '{':
		l, err := p.group(st)
		return atom{nodes: l, braced: true}, err
	case '}':
		return atom{}, p.errorf("unexpected }")
	case '&':
		return atom{}, p.errorf("& is only allowed in matrix, aligned, cases and split")
	case '_', '^':
		return atom{}, p.errorf("%c needs a base here", c)
	case '$', '#':
		return atom{}, p.errorf("%q is not allowed in an equation; write \\%c", c, c)
	case '~':
		p.pos++
		return atom{nodes: list{charRun("\u00a0", st)}}, nil
	case '\\':
		return p.command(st)
	}
	p.pos++
	return atom{nodes: list{charRun(string(c), st)}}, nil
}

// command reads a command and its arguments.
func (p *parser) command(st style) (atom, error) {
	start := p.pos
	name := p.readCommand()
	if name == "" {
		return atom{}, p.errorf(`a \ at the end`)
	}
	plain := func(n node) (atom, error) { return atom{nodes: list{n}}, nil }
	two := func() (list, list, error) {
		a, err := p.argument(st)
		if err != nil {
			return nil, nil, err
		}
		b, err := p.argument(st)
		return a, b, err
	}
	one := func(build func(list) node) (atom, error) {
		a, err := p.argument(st)
		if err != nil {
			return atom{}, err
		}
		return plain(build(a))
	}
	if r, ok := symbolCommand(name); ok {
		return atom{nodes: list{charRun(string(r), st)}}, nil
	}
	if sp, ok := spaceAliases[name]; ok {
		return atom{nodes: runsOf(sp, st)}, nil
	}
	if s, ok := styleCommands[name]; ok {
		inner := st
		inner.sty = s
		l, err := p.argument(inner)
		return atom{nodes: l}, err
	}
	if s, ok := scriptCommands[name]; ok {
		inner := st
		inner.scr = s
		l, err := p.argument(inner)
		return atom{nodes: l}, err
	}
	if chr, ok := naryCommand(name); ok {
		return atom{kind: atomBigOp, chr: chr}, nil
	}
	if operatorNames[name] || limitNames[name] {
		k := atomName
		if limitNames[name] {
			k = atomLimitName
		}
		return atom{kind: k, nodes: list{run{text: name, sty: "p"}}}, nil
	}
	if chr, ok := accentCommand(name); ok {
		return one(func(a list) node { return accent{chr: chr, base: a} })
	}
	if g, ok := groupNames[name]; ok {
		a, err := p.argument(st)
		if err != nil {
			return atom{}, err
		}
		return atom{kind: atomGroupChr, nodes: list{group{chr: g.chr, top: g.top, base: a}}}, nil
	}
	switch name {
	case "unicode":
		r, err := p.unicodeArg()
		if err != nil {
			return atom{}, err
		}
		return atom{nodes: list{charRun(string(r), st)}}, nil
	case "text", "textrm", "textnormal", "mbox":
		t, err := p.textArg()
		if err != nil {
			return atom{}, err
		}
		if t == "" {
			return atom{}, nil
		}
		return plain(run{text: t, sty: "p", plain: true, lit: st.lit})
	case "operatorname":
		k := atomName
		if p.peek() == '*' {
			p.pos++
			k = atomLimitName
		}
		t, err := p.nameArg()
		if err != nil {
			return atom{}, err
		}
		return atom{kind: k, nodes: list{run{text: t, sty: "p"}}}, nil
	case "mathop":
		c, err := p.charArg()
		if err != nil {
			return atom{}, err
		}
		return atom{kind: atomBigOp, chr: c}, nil
	case "frac", "dfrac", "tfrac", "cfrac":
		a, b, err := two()
		return atom{nodes: list{frac{kind: fracBar, num: a, den: b}}}, err
	case "sfrac":
		a, b, err := two()
		return atom{nodes: list{frac{kind: fracSkw, num: a, den: b}}}, err
	case "docxlinfrac":
		a, b, err := two()
		return atom{nodes: list{frac{kind: fracLin, num: a, den: b}}}, err
	case "binom", "dbinom", "tbinom":
		a, b, err := two()
		return atom{nodes: list{delim{beg: "(", end: ")", sep: "|", entries: []list{{frac{kind: fracNoBar, num: a, den: b}}}}}}, err
	case "genfrac":
		return p.genfrac(st)
	case "sqrt":
		deg, has, err := p.optional(st)
		if err != nil {
			return atom{}, err
		}
		body, err := p.argument(st)
		if err != nil {
			return atom{}, err
		}
		return plain(radical{deg: deg, hasDeg: has && len(deg) > 0, body: body})
	case "left":
		return p.leftRight(st)
	case "docxaccent":
		c, err := p.charArg()
		if err != nil {
			return atom{}, err
		}
		return one(func(a list) node { return accent{chr: c, base: a} })
	case "overline", "underline":
		top := name == "overline"
		return one(func(a list) node { return bar{top: top, base: a} })
	case "docxovergroup", "docxundergroup":
		c, err := p.charArg()
		if err != nil {
			return atom{}, err
		}
		a, err := p.argument(st)
		if err != nil {
			return atom{}, err
		}
		return atom{kind: atomGroupChr, nodes: list{group{chr: c, top: name == "docxovergroup", base: a}}}, nil
	case "underset", "overset", "stackrel":
		lim, base, err := two()
		return atom{nodes: list{limit{upper: name != "underset", base: base, lim: lim}}}, err
	case "prescript":
		sup, sub, err := two()
		if err != nil {
			return atom{}, err
		}
		base, err := p.argument(st)
		return atom{nodes: list{prescript{sub: sub, sup: sup, base: base}}}, err
	case "phantom", "vphantom", "hphantom", "smash":
		ph := phantom{zeroWid: name == "vphantom", zeroAsc: name == "hphantom" || name == "smash", show: name == "smash"}
		ph.zeroDesc = ph.zeroAsc
		return one(func(a list) node { ph.base = a; return ph })
	case "docxphantom":
		f, err := p.flagList("show", "zeroWid", "zeroAsc", "zeroDesc", "transp")
		if err != nil {
			return atom{}, err
		}
		return one(func(a list) node {
			return phantom{show: f["show"], zeroWid: f["zeroWid"], zeroAsc: f["zeroAsc"], zeroDesc: f["zeroDesc"], transp: f["transp"], base: a}
		})
	case "boxed":
		return one(func(a list) node { return borderBox{base: a} })
	case "cancel", "bcancel", "xcancel":
		bb := borderBox{hide: [4]bool{true, true, true, true}}
		bb.strike[2] = name != "bcancel"
		bb.strike[3] = name != "cancel"
		return one(func(a list) node { bb.base = a; return bb })
	case "docxboxed":
		f, err := p.flagList(borderFlags...)
		if err != nil {
			return atom{}, err
		}
		var bb borderBox
		for i, k := range borderFlags {
			if i < 4 {
				bb.hide[i] = f[k]
			} else {
				bb.strike[i-4] = f[k]
			}
		}
		return one(func(a list) node { bb.base = a; return bb })
	case "docxbox":
		f, err := p.flagList("opEmu", "noBreak", "diff")
		if err != nil {
			return atom{}, err
		}
		return one(func(a list) node { return box{opEmu: f["opEmu"], noBreak: f["noBreak"], diff: f["diff"], base: a} })
	case "docxfunc":
		fname, farg, err := two()
		return atom{nodes: list{function{name: fname, arg: farg}}}, err
	case "docxlit":
		inner := st
		inner.lit = true
		l, err := p.argument(inner)
		return atom{nodes: l}, err
	case "displaystyle", "textstyle", "scriptstyle", "scriptscriptstyle":
		return atom{}, nil
	case "limits", "nolimits":
		return atom{}, p.errorf(`\%s must follow a large operator`, name)
	case "end":
		return atom{}, p.errorf(`\end without \begin`)
	case "right", "middle":
		return atom{}, p.errorf(`\%s without \left`, name)
	case "\\":
		return atom{}, p.errorf(`\\ is only allowed between the lines of display math and in matrix, aligned, cases and split`)
	}
	p.pos = start
	return atom{}, p.errorf(`unknown command \%s; supported commands: %s`, name, strings.Join(SupportedCommands(), " "))
}

// symbolCommand returns the symbol rune for command name and whether it is known.
func symbolCommand(name string) (rune, bool) {
	if r, ok := symbolAliases[name]; ok {
		return r, true
	}
	for r, n := range symbolNames {
		if n == name {
			return r, true
		}
	}
	return 0, false
}

// naryCommand returns the operator character for command name and whether it is known.
func naryCommand(name string) (string, bool) {
	for c, n := range naryNames {
		if n == name {
			return c, true
		}
	}
	return "", false
}

// accentCommand returns the combining character for command name and whether it is known.
func accentCommand(name string) (string, bool) {
	if c, ok := accentAliases[name]; ok {
		return c, true
	}
	for c, n := range accentNames {
		if n == name {
			return c, true
		}
	}
	return "", false
}

// unicodeArg reads a \unicode code argument and returns its rune, erroring if invalid.
func (p *parser) unicodeArg() (rune, error) {
	p.skipSpace()
	if p.peek() != '{' {
		return 0, p.errorf(`\unicode needs {code}`)
	}
	end := p.pos + 1
	for end < len(p.s) && p.s[end] != '}' {
		end++
	}
	if end >= len(p.s) {
		return 0, p.errorf("missing }")
	}
	code := strings.TrimSpace(string(p.s[p.pos+1 : end]))
	p.pos = end + 1
	var v int64
	var err error
	if h, ok := strings.CutPrefix(strings.ToLower(code), "x"); ok {
		v, err = strconv.ParseInt(h, 16, 32)
	} else {
		v, err = strconv.ParseInt(code, 10, 32)
	}
	if err != nil || v < 0 || !xmlChar(rune(v)) {
		return 0, p.errorf(`\unicode{%s} is not a character an equation can hold`, code)
	}
	return rune(v), nil
}

// textArg reads the argument of \text in text mode.
func (p *parser) textArg() (string, error) {
	p.skipSpace()
	if p.peek() != '{' {
		return "", p.errorf(`\text needs {text}`)
	}
	p.pos++
	var b strings.Builder
	depth := 0
	space := false
	for {
		if p.eof() {
			return "", p.errorf("missing }")
		}
		c := p.s[p.pos]
		switch c {
		case ' ', '\t', '\n', '\r':
			p.pos++
			if !space {
				b.WriteByte(' ')
			}
			space = true
			continue
		case '{':
			depth++
			p.pos++
		case '}':
			p.pos++
			if depth == 0 {
				return b.String(), nil
			}
			depth--
		case '~':
			p.pos++
			b.WriteRune('\u00a0')
		case '\\':
			name := p.readCommand()
			switch name {
			case "{", "}", "$", "%", "#", "&", "_", " ":
				b.WriteString(name)
			case "textbackslash":
				b.WriteByte('\\')
			case "textasciicircum":
				b.WriteByte('^')
			case "textasciitilde":
				b.WriteByte('~')
			case "unicode":
				r, err := p.unicodeArg()
				if err != nil {
					return "", err
				}
				b.WriteRune(r)
			default:
				return "", p.errorf(`\%s is not allowed in \text`, name)
			}
		default:
			p.pos++
			b.WriteRune(c)
		}
		space = false
	}
}

// nameArg reads the argument of \operatorname: letters.
func (p *parser) nameArg() (string, error) {
	t, err := p.textArg()
	if err != nil {
		return "", err
	}
	if t == "" || !allRunes(t, ascii.IsAlpha) {
		return "", p.errorf(`\operatorname takes letters; use \docxfunc for other names`)
	}
	return t, nil
}

// genfrac reads genfrac delimiters, thickness and arguments and returns its fraction atom.
func (p *parser) genfrac(st style) (atom, error) {
	var parts []string
	for i := 0; i < 4; i++ {
		if i < 2 {
			d, err := p.bracedDelimiter()
			if err != nil {
				return atom{}, err
			}
			parts = append(parts, d)
			continue
		}
		t, err := p.textArg()
		if err != nil {
			return atom{}, err
		}
		parts = append(parts, strings.TrimSpace(t))
	}
	num, err := p.argument(st)
	if err != nil {
		return atom{}, err
	}
	den, err := p.argument(st)
	if err != nil {
		return atom{}, err
	}
	kind := fracBar
	if em, ok := zeroLength(parts[2]); ok && em {
		kind = fracNoBar
	}
	f := frac{kind: kind, num: num, den: den}
	if parts[0] == "" && parts[1] == "" {
		return atom{nodes: list{f}}, nil
	}
	return atom{nodes: list{delim{beg: parts[0], end: parts[1], sep: "|", entries: []list{{f}}}}}, nil
}

// bracedDelimiter reads {} or {delimiter}, as \genfrac takes them.
func (p *parser) bracedDelimiter() (string, error) {
	p.skipSpace()
	if p.peek() != '{' {
		return "", p.errorf(`\genfrac needs {delimiter}`)
	}
	p.pos++
	p.skipSpace()
	d := ""
	if p.peek() != '}' {
		var err error
		if d, err = p.delimiter(); err != nil {
			return "", err
		}
		p.skipSpace()
	}
	if p.peek() != '}' {
		return "", p.unexpectedIn("}")
	}
	p.pos++
	return d, nil
}

// zeroLength reports whether a \genfrac thickness is zero.
func zeroLength(s string) (bool, bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return false, true
	}
	i := 0
	for i < len(s) && (ascii.IsDigit(rune(s[i])) || s[i] == '.') {
		i++
	}
	v, err := strconv.ParseFloat(s[:i], 64)
	return err == nil && v == 0, err == nil
}

// delimiter reads the delimiter after \left, \middle or \right.
func (p *parser) delimiter() (string, error) {
	p.skipSpace()
	if p.eof() {
		return "", p.errorf("missing delimiter")
	}
	c := p.peek()
	if c != '\\' {
		p.pos++
		if c == '.' {
			return "", nil
		}
		return string(c), nil
	}
	name := p.readCommand()
	for ch, n := range delimiterNames {
		if n == `\`+name {
			return ch, nil
		}
	}
	if r, ok := symbolCommand(name); ok {
		return string(r), nil
	}
	if name == "unicode" {
		r, err := p.unicodeArg()
		return string(r), err
	}
	return "", p.errorf(`\%s is not a delimiter`, name)
}

// leftRight reads a left-delimited expression and returns its delimiter atom.
func (p *parser) leftRight(st style) (atom, error) {
	beg, err := p.delimiter()
	if err != nil {
		return atom{}, err
	}
	d := delim{beg: beg, sep: "|"}
	sepSet := false
	for {
		e, err := p.parseList(st, termDelim)
		if err != nil {
			return atom{}, err
		}
		d.entries = append(d.entries, e)
		switch {
		case p.lookCommand("middle"):
			p.readCommand()
			sep, err := p.delimiter()
			if err != nil {
				return atom{}, err
			}
			if sepSet && sep != d.sep {
				return atom{}, p.errorf(`every \middle of one \left...\right must be the same`)
			}
			d.sep, sepSet = sep, true
		case p.lookCommand("right"):
			p.readCommand()
			end, err := p.delimiter()
			if err != nil {
				return atom{}, err
			}
			d.end = end
			return atom{nodes: list{d}}, nil
		default:
			return atom{}, p.unexpectedIn(`\right`)
		}
	}
}

// environment reads \begin{name}...\end{name}. A split environment adds
// its items to the line it is in, with its line breaks and alignment
// points.
func (p *parser) environment(l list, st style, top bool) (list, error) {
	p.readCommand()
	envName, err := p.textArg()
	if err != nil {
		return nil, err
	}
	envName = strings.TrimSpace(envName)
	var rows [][]list
	split := envName == "split"
	if split && !top {
		return nil, p.errorf(`split is only allowed as a line of an equation`)
	}
	eqArray := envName == "aligned" || envName == "gathered" || envName == "cases"
	matrixEnv := map[string][2]string{
		"matrix": {"", ""}, "pmatrix": {"(", ")"}, "bmatrix": {"[", "]"}, "Bmatrix": {"{", "}"},
		"vmatrix": {"|", "|"}, "Vmatrix": {"\u2016", "\u2016"},
	}
	fences, isMatrix := matrixEnv[envName]
	if !split && !eqArray && !isMatrix {
		return nil, p.errorf("unknown environment %s; supported: Bmatrix Vmatrix aligned bmatrix cases gathered matrix pmatrix split vmatrix", envName)
	}
	row := []list{nil}
	var line list
	for {
		cell, err := p.parseList(st, termCell)
		if err != nil {
			return nil, err
		}
		switch {
		case split:
			line = appendAll(line, cell)
		case eqArray:
			row[len(row)-1] = appendAll(row[len(row)-1], cell)
		default:
			row[len(row)-1] = cell
		}
		p.skipSpace()
		switch {
		case p.peek() == '&':
			p.pos++
			switch {
			case split:
				line = append(line, alignPoint{})
			case eqArray:
				row[len(row)-1] = appendRun(row[len(row)-1], run{text: "&", sty: "p"})
			default:
				row = append(row, nil)
			}
		case p.lookCommand("\\"):
			p.readCommand()
			if split {
				br := lineBreak{}
				if p.lookCommand("docxalnat") {
					p.readCommand()
					t, err := p.textArg()
					if err != nil {
						return nil, err
					}
					k, err := strconv.Atoi(strings.TrimSpace(t))
					if err != nil || k < 1 || k > 255 {
						return nil, p.errorf(`\docxalnat takes 1 to 255`)
					}
					br.alnAt = k
				}
				line = append(line, br)
				continue
			}
			rows = append(rows, row)
			row = []list{nil}
		case p.lookCommand("end"):
			p.readCommand()
			endName, err := p.textArg()
			if err != nil {
				return nil, err
			}
			if strings.TrimSpace(endName) != envName {
				return nil, p.errorf(`\begin{%s} ended by \end{%s}`, envName, endName)
			}
			// Every \\ starts a row, a trailing one included, so that
			// empty rows survive.
			rows = append(rows, row)
			return p.finishEnv(l, envName, fences, rows, line)
		default:
			return nil, p.unexpectedIn(`\end{` + envName + "}")
		}
	}
}

// finishEnv returns l extended with the finished environment built from fences, rows and line.
func (p *parser) finishEnv(l list, envName string, fences [2]string, rows [][]list, line list) (list, error) {
	switch envName {
	case "split":
		return appendAll(l, line), nil
	case "aligned", "gathered", "cases":
		var a eqArr
		for _, r := range rows {
			row, _ := alignRow(r[0])
			a.rows = append(a.rows, row)
		}
		if envName == "cases" {
			return append(l, delim{beg: "{", end: "", sep: "|", entries: []list{{a}}}), nil
		}
		return append(l, a), nil
	}
	m := matrix{rows: rows}
	if envName == "matrix" {
		return append(l, m), nil
	}
	return append(l, delim{beg: fences[0], end: fences[1], sep: "|", entries: []list{{m}}}), nil
}

// SupportedCommands lists the LaTeX commands the grammar reads, sorted.
func SupportedCommands() []string {
	set := map[string]bool{}
	add := func(names ...string) {
		for _, n := range names {
			set[`\`+n] = true
		}
	}
	for _, n := range symbolNames {
		add(n)
	}
	for n := range symbolAliases {
		add(n)
	}
	for n := range spaceAliases {
		add(n)
	}
	for _, m := range []map[string]string{styleCommands, scriptCommands, accentAliases} {
		for n := range m {
			add(n)
		}
	}
	for _, m := range []map[string]string{naryNames, accentNames} {
		for _, n := range m {
			add(n)
		}
	}
	for n := range groupNames {
		add(n)
	}
	for n := range operatorNames {
		add(n)
	}
	for n := range limitNames {
		add(n)
	}
	add("unicode", "text", "textrm", "textnormal", "mbox", "operatorname", "mathop", "frac", "dfrac",
		"tfrac", "cfrac", "sfrac", "docxlinfrac", "binom", "dbinom", "tbinom", "genfrac", "sqrt", "left",
		"middle", "right", "docxaccent", "overline", "underline", "docxovergroup", "docxundergroup",
		"underset", "overset", "stackrel", "prescript", "phantom", "vphantom", "hphantom", "smash",
		"docxphantom", "boxed", "cancel", "bcancel", "xcancel", "docxboxed", "docxbox", "docxfunc",
		"displaystyle", "textstyle", "scriptstyle", "scriptscriptstyle", "limits", "nolimits", "begin",
		"end", "\\", "docxalnat", "docxlit")
	return slices.Sorted(maps.Keys(set))
}
