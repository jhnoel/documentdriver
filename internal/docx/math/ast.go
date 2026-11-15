package math

import (
	"strconv"
	"strings"
)

// The equation model both directions convert through. It holds exactly
// what the LaTeX grammar can say, so OMML read into it and written back,
// or LaTeX parsed into it and written back, keeps everything it holds.

// node is one element of a math list.
type node interface {
	// key writes a canonical description of the node, used to compare
	// models.
	key(b *strings.Builder)
}

// list is a math list: an OMML argument (m:e, m:num, ...) or a LaTeX
// group.
type list []node

// equation is one equation: inline, or display with one or more lines
// (the m:oMath elements of an m:oMathPara).
type equation struct {
	display bool
	lines   []list
}

// run is text of one style: a merged sequence of characters whose
// effective style is the same.
type run struct {
	text  string
	sty   string // effective m:sty: p, i, b or bi
	scr   string // m:scr other than roman, or ""
	plain bool   // m:nor: normal (non-math) text, \text{}
	lit   bool   // m:lit: literal text Word does not reformat, \docxlit{}
}

// fraction kinds (ST_FType).
const (
	fracBar   = "bar"
	fracNoBar = "noBar"
	fracLin   = "lin"
	fracSkw   = "skw"
)

type frac struct {
	kind     string
	num, den list
}

// script is m:sSub, m:sSup or m:sSubSup.
type script struct {
	base, sub, sup list
	hasSub, hasSup bool
}

// prescript is m:sPre.
type prescript struct{ sub, sup, base list }

// nary is m:nary. A hidden or empty limit is absent.
type nary struct {
	chr            string
	limLoc         string // "undOvr", "subSup" or "" for the document default
	sub, sup, body list
	hasSub, hasSup bool
}

// radical is m:rad. A hidden or empty degree is absent.
type radical struct {
	deg, body list
	hasDeg    bool
}

// delim is m:d with its characters resolved; "" is no character.
type delim struct {
	beg, end, sep string
	entries       []list
}

type matrix struct{ rows [][]list }

type eqArr struct{ rows []list }

type accent struct {
	chr  string
	base list
}

type bar struct {
	top  bool
	base list
}

// group is m:groupChr.
type group struct {
	chr  string
	top  bool
	base list
}

// limit is m:limLow (upper false) or m:limUpp.
type limit struct {
	upper     bool
	base, lim list
}

// function is m:func.
type function struct{ name, arg list }

// phantom is m:phant with its on/off properties resolved.
type phantom struct {
	show, zeroWid, zeroAsc, zeroDesc, transp bool
	base                                     list
}

// borderBox is m:borderBox: hidden sides top, bottom, left, right and
// strikes horizontal, vertical, bottom-left to top-right, top-left to
// bottom-right.
type borderBox struct {
	hide   [4]bool
	strike [4]bool
	base   list
}

// box is m:box with its on/off properties.
type box struct {
	opEmu, noBreak, diff bool
	base                 list
}

// key writes the canonical description of list l, bracketing each node's key.
func (l list) key(b *strings.Builder) {
	b.WriteByte('[')
	for _, n := range l {
		n.key(b)
	}
	b.WriteByte(']')
}

// key writes the canonical description of equation e, marking display and each line.
func (e equation) key(b *strings.Builder) {
	if e.display {
		b.WriteString("display")
	}
	for _, l := range e.lines {
		l.key(b)
	}
}

// keyOf returns the canonical key string of the value k.
func keyOf(k interface{ key(*strings.Builder) }) string {
	var b strings.Builder
	k.key(&b)
	return b.String()
}

// flags returns a string of '1' or '0' for each bool in fs.
func flags(fs ...bool) string {
	var b strings.Builder
	for _, f := range fs {
		if f {
			b.WriteByte('1')
		} else {
			b.WriteByte('0')
		}
	}
	return b.String()
}

// key writes the canonical description of run n, quoting its text and style flags.
func (n run) key(b *strings.Builder) {
	b.WriteString("r(" + strconv.Quote(n.text) + " " + n.sty + " " + n.scr + " " + flags(n.plain, n.lit) + ")")
}

// key writes the canonical description of frac n with its kind, numerator and denominator.
func (n frac) key(b *strings.Builder) {
	b.WriteString("f" + n.kind)
	n.num.key(b)
	n.den.key(b)
}

// key writes the canonical description of script n with script-presence flags and base, sub and sup.
func (n script) key(b *strings.Builder) {
	b.WriteString("s" + flags(n.hasSub, n.hasSup))
	n.base.key(b)
	n.sub.key(b)
	n.sup.key(b)
}

// key writes the canonical description of prescript n with its sub, sup and base.
func (n prescript) key(b *strings.Builder) {
	b.WriteString("pre")
	n.sub.key(b)
	n.sup.key(b)
	n.base.key(b)
}

// key writes the canonical description of nary n with character, limit location and parts.
func (n nary) key(b *strings.Builder) {
	b.WriteString("n(" + strconv.Quote(n.chr) + " " + n.limLoc + " " + flags(n.hasSub, n.hasSup) + ")")
	n.sub.key(b)
	n.sup.key(b)
	n.body.key(b)
}

// key writes the canonical description of radical n with degree-presence flag, degree and body.
func (n radical) key(b *strings.Builder) {
	b.WriteString("rad" + flags(n.hasDeg))
	n.deg.key(b)
	n.body.key(b)
}

// key writes the canonical description of delim n with quoted characters and entries.
func (n delim) key(b *strings.Builder) {
	b.WriteString("d(" + strconv.Quote(n.beg) + strconv.Quote(n.sep) + strconv.Quote(n.end) + ")")
	for _, e := range n.entries {
		e.key(b)
	}
}

// key writes the canonical description of matrix n as angle-bracketed rows.
func (n matrix) key(b *strings.Builder) {
	b.WriteString("m")
	for _, r := range n.rows {
		b.WriteByte('<')
		for _, c := range r {
			c.key(b)
		}
		b.WriteByte('>')
	}
}

// key writes the canonical description of eqArr n with each row.
func (n eqArr) key(b *strings.Builder) {
	b.WriteString("eq")
	for _, r := range n.rows {
		r.key(b)
	}
}

// key writes the canonical description of accent n with quoted character and base.
func (n accent) key(b *strings.Builder) {
	b.WriteString("acc" + strconv.Quote(n.chr))
	n.base.key(b)
}

// key writes the canonical description of bar n with top flag and base.
func (n bar) key(b *strings.Builder) {
	b.WriteString("bar" + flags(n.top))
	n.base.key(b)
}

// key writes the canonical description of group n with quoted character, top flag and base.
func (n group) key(b *strings.Builder) {
	b.WriteString("grp" + strconv.Quote(n.chr) + flags(n.top))
	n.base.key(b)
}

// key writes the canonical description of limit n with upper flag, base and limit.
func (n limit) key(b *strings.Builder) {
	b.WriteString("lim" + flags(n.upper))
	n.base.key(b)
	n.lim.key(b)
}

// key writes the canonical description of function n with its name and argument.
func (n function) key(b *strings.Builder) {
	b.WriteString("fn")
	n.name.key(b)
	n.arg.key(b)
}

// key writes the canonical description of phantom n with property flags and base.
func (n phantom) key(b *strings.Builder) {
	b.WriteString("ph" + flags(n.show, n.zeroWid, n.zeroAsc, n.zeroDesc, n.transp))
	n.base.key(b)
}

// key writes the canonical description of borderBox n with hide and strike flags and base.
func (n borderBox) key(b *strings.Builder) {
	b.WriteString("bb" + flags(n.hide[:]...) + flags(n.strike[:]...))
	n.base.key(b)
}

// key writes the canonical description of box n with property flags and base.
func (n box) key(b *strings.Builder) {
	b.WriteString("box" + flags(n.opEmu, n.noBreak, n.diff))
	n.base.key(b)
}

// defaultSty is the style OMML gives a character without m:sty: italic
// for letters, upright for everything else.
func defaultSty(r rune) string {
	if isAlphabetic(r) {
		return "i"
	}
	return "p"
}

// appendRun adds text of one effective style to l, merging it into a
// preceding run of the same style.
func appendRun(l list, r run) list {
	if r.text == "" {
		return l
	}
	if n := len(l); n > 0 {
		if prev, ok := l[n-1].(run); ok && prev.sty == r.sty && prev.scr == r.scr && prev.plain == r.plain && prev.lit == r.lit {
			prev.text += r.text
			l[n-1] = prev
			return l
		}
	}
	return append(l, r)
}

// alignRow normalizes an equation-array row: an & among its runs is an
// alignment mark (Microsoft's omml2mathml.xsl reads it so), kept as an
// upright & of its own whatever the run's style. It reports whether a
// style was dropped.
func alignRow(l list) (list, bool) {
	var out list
	dropped := false
	for _, n := range l {
		r, ok := n.(run)
		if !ok || !strings.Contains(r.text, "&") {
			out = append(out, n)
			continue
		}
		for i, piece := range strings.Split(r.text, "&") {
			if i > 0 {
				out = append(out, run{text: "&", sty: "p"})
				dropped = dropped || r.plain || r.lit || r.scr != "" || r.sty != "p"
			}
			if piece != "" {
				p := r
				p.text = piece
				out = append(out, p)
			}
		}
	}
	// Merge neighbours the split left apart.
	var merged list
	for _, n := range out {
		if r, ok := n.(run); ok {
			merged = appendRun(merged, r)
		} else {
			merged = append(merged, n)
		}
	}
	return merged, dropped
}

// appendText adds text whose characters take their style from sty (""
// for each character's default), splitting it where the effective style
// changes.
func appendStyled(l list, text, sty, scr string, plain, lit bool) list {
	if plain {
		return appendRun(l, run{text: text, sty: "p", plain: true, lit: lit})
	}
	for _, r := range text {
		s := sty
		if s == "" {
			s = defaultSty(r)
		}
		l = appendRun(l, run{text: string(r), sty: s, scr: scr, lit: lit})
	}
	return l
}

// lineBreak is a manual line break inside one m:oMath (m:brk on the next
// run, or w:br inside a run); alnAt is m:brk's m:alnAt, 0 for none.
type lineBreak struct{ alnAt int }

// alignPoint is m:aln: the next run is the alignment point of its line.
type alignPoint struct{}

// key writes the canonical description of lineBreak n with its alignment position.
func (n lineBreak) key(b *strings.Builder) { b.WriteString("br" + strconv.Itoa(n.alnAt)) }

// key writes the canonical description of alignPoint n as an alignment point.
func (alignPoint) key(b *strings.Builder) { b.WriteString("aln") }
