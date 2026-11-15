package math

import (
	"sort"
	"strconv"
	"strings"

	"github.com/jhnoel/documentdriver/internal/ooxml/tree"
)

// Loss reports OMML that the LaTeX grammar does not carry: content (it
// changes what the equation shows or means) or formatting (character
// formatting and layout hints, which LaTeX does not hold).
type Loss struct {
	// Kind is LossContent or LossFormatting.
	Kind string
	// What names the element or property, such as "m:rPr/m:lit".
	What string
	// Count is how many times it occurs in the equation.
	Count int
}

// The kinds of Loss.
const (
	// LossContent is OMML that changes what the equation shows or means.
	LossContent = "content"
	// LossFormatting is character formatting or a layout hint.
	LossFormatting = "formatting"
)

// reader reads OMML into the model, counting what it drops.
type reader struct {
	losses map[[2]string]int
}

// lose records one occurrence of a loss of the given kind and what.
func (rd *reader) lose(kind, what string) {
	if rd.losses == nil {
		rd.losses = map[[2]string]int{}
	}
	rd.losses[[2]string{kind, what}]++
}

// report returns the recorded losses sorted by kind and what.
func (rd *reader) report() []Loss {
	var out []Loss
	for k, n := range rd.losses {
		out = append(out, Loss{Kind: k[0], What: k[1], Count: n})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Kind != out[j].Kind {
			return out[i].Kind < out[j].Kind
		}
		return out[i].What < out[j].What
	})
	return out
}

// qname names an element for a loss report: m: or w: and its local name.
func qname(n *tree.Node) string {
	switch {
	case inMath(n):
		return "m:" + elemName(n)
	case inWord(n):
		return "w:" + elemName(n)
	}
	return n.Name()
}

// readEquation reads an m:oMath or m:oMathPara.
func (rd *reader) readEquation(n *tree.Node) equation {
	if !isElem(n, "oMathPara") {
		return equation{lines: []list{rd.readLine(n)}}
	}
	eq := equation{display: true}
	for _, c := range elements(n) {
		switch {
		case isElem(c, "oMath"):
			eq.lines = append(eq.lines, rd.readLine(c))
		case isElem(c, "oMathParaPr"):
			rd.props(c, nil)
		default:
			rd.lose(LossContent, qname(c))
		}
	}
	if len(eq.lines) == 0 {
		eq.lines = []list{nil}
	}
	return eq
}

// readLine reads one m:oMath: the only list that may hold line breaks
// and alignment points.
func (rd *reader) readLine(n *tree.Node) list {
	return rd.readList(n, 0, true)
}

// props reads a property bag, reporting every property not in known as
// formatting (m:ctrlPr and its w:rPr included).
func (rd *reader) props(bag *tree.Node, known map[string]bool) {
	if bag == nil {
		return
	}
	for _, c := range elements(bag) {
		name := elemName(c)
		if known[name] && inMath(c) {
			continue
		}
		rd.lose(LossFormatting, qname(bag)+"/"+qname(c))
	}
}

// readList reads the element children of n as a math list.
func (rd *reader) readList(n *tree.Node, depth int, top bool) list {
	var l list
	if n == nil || depth > tree.MaxDepth {
		return l
	}
	for _, c := range elements(n) {
		l = rd.readElement(l, c, depth+1, top)
	}
	return l
}

// arg reads the first child named local of n as a list.
func (rd *reader) arg(n *tree.Node, local string, depth int) list {
	return rd.readList(childElem(n, local), depth, false)
}

// readElement reads OMML element n into list l and returns the extended list, reporting losses.
func (rd *reader) readElement(l list, n *tree.Node, depth int, top bool) list {
	name := elemName(n)
	if !inMath(n) {
		switch {
		case inWord(n) && (name == "ins" || name == "moveTo"):
			// Tracked insertions inside an equation: the content counts,
			// the revision mark does not survive.
			rd.lose(LossContent, qname(n))
			for _, c := range elements(n) {
				l = rd.readElement(l, c, depth+1, top)
			}
		default:
			rd.lose(LossContent, qname(n))
		}
		return l
	}
	if strings.HasSuffix(name, "Pr") {
		return l
	}
	switch name {
	case "r":
		return rd.readRun(l, n, top)
	case "f":
		bag := childElem(n, "fPr")
		rd.props(bag, map[string]bool{"type": true})
		kind := fracBar
		if v, ok := attr(childElem(bag, "type"), "val"); ok {
			switch v {
			case fracNoBar, fracLin, fracSkw:
				kind = v
			}
		}
		return append(l, frac{kind: kind, num: rd.arg(n, "num", depth), den: rd.arg(n, "den", depth)})
	case "sSub", "sSup", "sSubSup":
		rd.props(childElem(n, name+"Pr"), nil)
		s := script{base: rd.arg(n, "e", depth), hasSub: name != "sSup", hasSup: name != "sSub"}
		if s.hasSub {
			s.sub = rd.arg(n, "sub", depth)
		}
		if s.hasSup {
			s.sup = rd.arg(n, "sup", depth)
		}
		return append(l, s)
	case "sPre":
		rd.props(childElem(n, "sPrePr"), nil)
		return append(l, prescript{sub: rd.arg(n, "sub", depth), sup: rd.arg(n, "sup", depth), base: rd.arg(n, "e", depth)})
	case "nary":
		return append(l, rd.readNary(n, depth))
	case "rad":
		bag := childElem(n, "radPr")
		rd.props(bag, map[string]bool{"degHide": true})
		r := radical{body: rd.arg(n, "e", depth)}
		deg := rd.arg(n, "deg", depth)
		if !onOff(childElem(bag, "degHide")) && len(deg) > 0 {
			r.deg, r.hasDeg = deg, true
		} else if len(deg) > 0 {
			rd.lose(LossContent, "m:rad/m:deg hidden by m:degHide")
		}
		return append(l, r)
	case "d":
		bag := childElem(n, "dPr")
		rd.props(bag, map[string]bool{"begChr": true, "endChr": true, "sepChr": true})
		d := delim{beg: charProp(bag, "begChr", "("), end: charProp(bag, "endChr", ")"), sep: charProp(bag, "sepChr", "|")}
		for e := range n.ChildrenSeq() {
			if isElem(e, "e") {
				d.entries = append(d.entries, rd.readList(e, depth, false))
			}
		}
		if len(d.entries) == 0 {
			d.entries = []list{nil}
		}
		return append(l, d)
	case "m":
		rd.props(childElem(n, "mPr"), nil)
		var m matrix
		for r := range n.ChildrenSeq() {
			if !isElem(r, "mr") {
				continue
			}
			var row []list
			for e := range r.ChildrenSeq() {
				if isElem(e, "e") {
					row = append(row, rd.readList(e, depth, false))
				}
			}
			if len(row) == 0 {
				row = []list{nil}
			}
			m.rows = append(m.rows, row)
		}
		if len(m.rows) == 0 {
			m.rows = [][]list{{nil}}
		}
		return append(l, m)
	case "eqArr":
		rd.props(childElem(n, "eqArrPr"), nil)
		var a eqArr
		for e := range n.ChildrenSeq() {
			if isElem(e, "e") {
				row, dropped := alignRow(rd.readList(e, depth, false))
				if dropped {
					rd.lose(LossFormatting, "style of an alignment & in m:eqArr")
				}
				a.rows = append(a.rows, row)
			}
		}
		if len(a.rows) == 0 {
			a.rows = []list{nil}
		}
		return append(l, a)
	case "acc":
		bag := childElem(n, "accPr")
		rd.props(bag, map[string]bool{"chr": true})
		return append(l, accent{chr: charProp(bag, "chr", "\u0302"), base: rd.arg(n, "e", depth)})
	case "bar":
		bag := childElem(n, "barPr")
		rd.props(bag, map[string]bool{"pos": true})
		pos, _ := attr(childElem(bag, "pos"), "val")
		return append(l, bar{top: pos == "top", base: rd.arg(n, "e", depth)})
	case "groupChr":
		bag := childElem(n, "groupChrPr")
		rd.props(bag, map[string]bool{"chr": true, "pos": true, "vertJc": true})
		pos, _ := attr(childElem(bag, "pos"), "val")
		top := pos == "top"
		if jc, ok := attr(childElem(bag, "vertJc"), "val"); ok && (jc == "top") == top {
			// The standard justification is the opposite of the
			// position; this one cannot be written.
			rd.lose(LossContent, "m:groupChrPr/m:vertJc")
		}
		return append(l, group{chr: charProp(bag, "chr", "\u23df"), top: top, base: rd.arg(n, "e", depth)})
	case "limLow", "limUpp":
		rd.props(childElem(n, name+"Pr"), nil)
		return append(l, limit{upper: name == "limUpp", base: rd.arg(n, "e", depth), lim: rd.arg(n, "lim", depth)})
	case "func":
		rd.props(childElem(n, "funcPr"), nil)
		return append(l, function{name: rd.arg(n, "fName", depth), arg: rd.arg(n, "e", depth)})
	case "phant":
		bag := childElem(n, "phantPr")
		rd.props(bag, map[string]bool{"show": true, "zeroWid": true, "zeroAsc": true, "zeroDesc": true, "transp": true})
		show := childElem(bag, "show")
		return append(l, phantom{
			show:    show == nil || onOff(show),
			zeroWid: onOff(childElem(bag, "zeroWid")), zeroAsc: onOff(childElem(bag, "zeroAsc")),
			zeroDesc: onOff(childElem(bag, "zeroDesc")), transp: onOff(childElem(bag, "transp")),
			base: rd.arg(n, "e", depth),
		})
	case "borderBox":
		bag := childElem(n, "borderBoxPr")
		names := []string{"hideTop", "hideBot", "hideLeft", "hideRight", "strikeH", "strikeV", "strikeBLTR", "strikeTLBR"}
		known := map[string]bool{}
		for _, k := range names {
			known[k] = true
		}
		rd.props(bag, known)
		bb := borderBox{base: rd.arg(n, "e", depth)}
		for i, k := range names {
			if i < 4 {
				bb.hide[i] = onOff(childElem(bag, k))
			} else {
				bb.strike[i-4] = onOff(childElem(bag, k))
			}
		}
		return append(l, bb)
	case "box":
		bag := childElem(n, "boxPr")
		rd.props(bag, map[string]bool{"opEmu": true, "noBreak": true, "diff": true})
		return append(l, box{
			opEmu: onOff(childElem(bag, "opEmu")), noBreak: onOff(childElem(bag, "noBreak")),
			diff: onOff(childElem(bag, "diff")), base: rd.arg(n, "e", depth),
		})
	case "oMath":
		// An equation inside an equation: its content, in place.
		rd.lose(LossContent, "m:oMath inside an equation")
		return appendAll(l, rd.readList(n, depth, false))
	}
	rd.lose(LossContent, qname(n))
	return l
}

// charProp returns the m:val of the character property local in bag,
// def when the property or its value is absent.
func charProp(bag *tree.Node, local, def string) string {
	if v, ok := attr(childElem(bag, local), "val"); ok {
		return v
	}
	return def
}

// readNary reads an m:nary element and returns its nary, reporting hidden limits as losses.
func (rd *reader) readNary(n *tree.Node, depth int) nary {
	bag := childElem(n, "naryPr")
	rd.props(bag, map[string]bool{"chr": true, "limLoc": true, "subHide": true, "supHide": true})
	x := nary{chr: charProp(bag, "chr", "\u222b"), body: rd.arg(n, "e", depth)}
	if v, ok := attr(childElem(bag, "limLoc"), "val"); ok && (v == "undOvr" || v == "subSup") {
		x.limLoc = v
	}
	sub, sup := rd.arg(n, "sub", depth), rd.arg(n, "sup", depth)
	if onOff(childElem(bag, "subHide")) {
		if len(sub) > 0 {
			rd.lose(LossContent, "m:nary/m:sub hidden by m:subHide")
		}
	} else if len(sub) > 0 {
		x.sub, x.hasSub = sub, true
	}
	if onOff(childElem(bag, "supHide")) {
		if len(sup) > 0 {
			rd.lose(LossContent, "m:nary/m:sup hidden by m:supHide")
		}
	} else if len(sup) > 0 {
		x.sup, x.hasSup = sup, true
	}
	return x
}

// readRun reads an m:r: its style, breaks and alignment, and its text
// (m:t, w:t, and w:br, w:tab, w:cr and w:noBreakHyphen inside it).
func (rd *reader) readRun(l list, n *tree.Node, top bool) list {
	var rPr *tree.Node
	for _, c := range elements(n) {
		if isElem(c, "rPr") && inMath(c) {
			rPr = c
			break
		}
	}
	sty, scr, plain, lit := "", "", false, false
	if rPr != nil {
		for _, c := range elements(rPr) {
			switch elemName(c) {
			case "sty":
				if v, ok := attr(c, "val"); ok && (v == "p" || v == "b" || v == "i" || v == "bi") {
					sty = v
				}
			case "scr":
				if v, ok := attr(c, "val"); ok && v != "roman" {
					scr = v
				}
			case "nor":
				plain = onOff(c)
			case "brk":
				alnAt := 0
				if v, ok := attr(c, "alnAt"); ok {
					if k, err := strconv.Atoi(v); err == nil && k > 0 && k < 256 {
						alnAt = k
					}
				}
				l = rd.mark(l, lineBreak{alnAt}, top)
			case "aln":
				if onOff(c) {
					l = rd.mark(l, alignPoint{}, top)
				}
			case "lit":
				lit = onOff(c)
			default:
				rd.lose(LossFormatting, "m:rPr/"+qname(c))
			}
		}
	}
	for _, c := range elements(n) {
		name := elemName(c)
		switch {
		case name == "rPr":
			if !inMath(c) {
				rd.lose(LossFormatting, qname(c))
			}
		case name == "t" || (name == "delText" && inMath(c)):
			if name == "delText" {
				rd.lose(LossContent, "m:delText")
			}
			l = appendStyled(l, textContent(c), sty, scr, plain, lit)
		case inWord(c) && (name == "br" || name == "cr"):
			l = rd.mark(l, lineBreak{}, top)
		case inWord(c) && name == "tab":
			l = appendStyled(l, "\t", sty, scr, plain, lit)
		case inWord(c) && name == "noBreakHyphen":
			l = appendStyled(l, "\u2011", sty, scr, plain, lit)
		default:
			rd.lose(LossContent, "m:r/"+qname(c))
		}
	}
	return l
}

// mark adds a line break or alignment point, which only an m:oMath's own
// list can hold.
func (rd *reader) mark(l list, n node, top bool) list {
	if !top {
		rd.lose(LossContent, "line break or alignment inside a structure")
		return l
	}
	return append(l, n)
}

// Office Math and WordprocessingML namespaces, transitional and strict.
const (
	strictOMMLNS = "http://purl.oclc.org/ooxml/officeDocument/math"
	strictWordNS = "http://purl.oclc.org/ooxml/wordprocessingml/main"
)

// inMath reports whether n is in the Office Math namespace, transitional or Strict.
func inMath(n *tree.Node) bool {
	s := n.Space()
	return s == OMMLNS || s == strictOMMLNS
}

// inWord reports whether n is in the WordprocessingML namespace, transitional or Strict.
func inWord(n *tree.Node) bool {
	s := n.Space()
	return s == wordNS || s == strictWordNS
}
