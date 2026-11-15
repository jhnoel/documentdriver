package math

import (
	"strconv"
	"strings"
)

// writeOMML writes the model as an m:oMath (inline) or m:oMathPara
// (display) fragment declaring xmlns:m.
func writeOMML(eq equation) string {
	var b strings.Builder
	if eq.display {
		b.WriteString(`<m:oMathPara xmlns:m="` + OMMLNS + `">`)
		for _, l := range eq.lines {
			b.WriteString("<m:oMath>")
			writeOMMLList(&b, l)
			b.WriteString("</m:oMath>")
		}
		b.WriteString("</m:oMathPara>")
		return b.String()
	}
	b.WriteString(`<m:oMath xmlns:m="` + OMMLNS + `">`)
	for _, l := range eq.lines {
		writeOMMLList(&b, l)
	}
	b.WriteString("</m:oMath>")
	return b.String()
}

// runProps are what an m:rPr says besides the style.
type runProps struct {
	brk   bool
	alnAt int
	aln   bool
}

// writeOMMLList writes list l as OMML to b, carrying line-break and alignment marks on runs.
func writeOMMLList(b *strings.Builder, l list) {
	var pending runProps
	for i := 0; i < len(l); i++ {
		switch n := l[i].(type) {
		case lineBreak:
			if pending.brk || pending.aln {
				writeOMMLRun(b, run{}, pending)
				pending = runProps{}
			}
			pending.brk, pending.alnAt = true, n.alnAt
			continue
		case alignPoint:
			if pending.aln {
				writeOMMLRun(b, run{}, pending)
				pending = runProps{}
			}
			pending.aln = true
			continue
		case run:
			// Runs whose characters all have their default style go into
			// one m:r without m:sty.
			if allDefault(n) {
				text := n.text
				for i+1 < len(l) {
					next, ok := l[i+1].(run)
					if !ok || !allDefault(next) || next.scr != n.scr || next.plain != n.plain || next.lit != n.lit {
						break
					}
					text += next.text
					i++
				}
				n.text, n.sty = text, ""
			}
			writeOMMLRun(b, n, pending)
			pending = runProps{}
			continue
		}
		if pending != (runProps{}) {
			writeOMMLRun(b, run{}, pending)
			pending = runProps{}
		}
		writeOMMLNode(b, l[i])
	}
	if pending != (runProps{}) {
		writeOMMLRun(b, run{}, pending)
	}
}

// allDefault reports whether every character of a math run has its
// default style, so the run needs no m:sty.
func allDefault(r run) bool {
	if r.plain {
		return true
	}
	for _, c := range r.text {
		if defaultSty(c) != r.sty {
			return false
		}
	}
	return true
}

// writeOMMLRun writes an m:r; r.sty "" writes no m:sty.
func writeOMMLRun(b *strings.Builder, r run, pr runProps) {
	text := r.text
	b.WriteString("<m:r>")
	var rp strings.Builder
	if r.lit {
		rp.WriteString("<m:lit/>")
	}
	switch {
	case r.plain:
		rp.WriteString("<m:nor/>")
	default:
		if r.scr != "" {
			rp.WriteString(`<m:scr m:val="` + r.scr + `"/>`)
		}
		if r.sty != "" {
			rp.WriteString(`<m:sty m:val="` + r.sty + `"/>`)
		}
	}
	if pr.brk {
		if pr.alnAt > 0 {
			rp.WriteString(`<m:brk m:alnAt="` + strconv.Itoa(pr.alnAt) + `"/>`)
		} else {
			rp.WriteString("<m:brk/>")
		}
	}
	if pr.aln {
		rp.WriteString("<m:aln/>")
	}
	if rp.Len() > 0 {
		b.WriteString("<m:rPr>" + rp.String() + "</m:rPr>")
	}
	if needsPreserve(text) {
		b.WriteString(`<m:t xml:space="preserve">`)
	} else {
		b.WriteString("<m:t>")
	}
	b.WriteString(escapeMT(text) + "</m:t></m:r>")
}

// needsPreserve reports whether text t needs xml:space preserve for its whitespace.
func needsPreserve(t string) bool {
	return strings.ContainsAny(t, "\t\n\r") || strings.HasPrefix(t, " ") || strings.HasSuffix(t, " ") ||
		strings.Contains(t, "  ")
}

// escapeMT escapes m:t text; a carriage return becomes a reference, as an
// XML parser would turn a literal one into a line feed.
func escapeMT(t string) string {
	return strings.ReplaceAll(escapeText(t), "\r", "&#13;")
}

// writeOMMLArg writes list l as an m:name argument element to b.
func writeOMMLArg(b *strings.Builder, name string, l list) {
	b.WriteString("<m:" + name + ">")
	writeOMMLList(b, l)
	b.WriteString("</m:" + name + ">")
}

// val returns the m:name property element with value v, attribute-escaped.
func val(name, v string) string { return `<m:` + name + ` m:val="` + escapeAttr(v) + `"/>` }

// onIf returns an m:name element when on is true, otherwise the empty string.
func onIf(name string, on bool) string {
	if on {
		return "<m:" + name + "/>"
	}
	return ""
}

// writeOMMLNode writes node n as its OMML element to b.
func writeOMMLNode(b *strings.Builder, n node) {
	switch n := n.(type) {
	case frac:
		b.WriteString("<m:f>")
		if n.kind != fracBar {
			b.WriteString("<m:fPr>" + val("type", n.kind) + "</m:fPr>")
		}
		writeOMMLArg(b, "num", n.num)
		writeOMMLArg(b, "den", n.den)
		b.WriteString("</m:f>")
	case script:
		name := "sSubSup"
		switch {
		case !n.hasSup:
			name = "sSub"
		case !n.hasSub:
			name = "sSup"
		}
		b.WriteString("<m:" + name + ">")
		writeOMMLArg(b, "e", n.base)
		if n.hasSub {
			writeOMMLArg(b, "sub", n.sub)
		}
		if n.hasSup {
			writeOMMLArg(b, "sup", n.sup)
		}
		b.WriteString("</m:" + name + ">")
	case prescript:
		b.WriteString("<m:sPre>")
		writeOMMLArg(b, "sub", n.sub)
		writeOMMLArg(b, "sup", n.sup)
		writeOMMLArg(b, "e", n.base)
		b.WriteString("</m:sPre>")
	case nary:
		b.WriteString("<m:nary><m:naryPr>" + val("chr", n.chr))
		if n.limLoc != "" {
			b.WriteString(val("limLoc", n.limLoc))
		}
		if !n.hasSub {
			b.WriteString(val("subHide", "1"))
		}
		if !n.hasSup {
			b.WriteString(val("supHide", "1"))
		}
		b.WriteString("</m:naryPr>")
		writeOMMLArg(b, "sub", n.sub)
		writeOMMLArg(b, "sup", n.sup)
		writeOMMLArg(b, "e", n.body)
		b.WriteString("</m:nary>")
	case radical:
		b.WriteString("<m:rad>")
		if !n.hasDeg {
			b.WriteString("<m:radPr>" + val("degHide", "1") + "</m:radPr>")
		}
		writeOMMLArg(b, "deg", n.deg)
		writeOMMLArg(b, "e", n.body)
		b.WriteString("</m:rad>")
	case delim:
		b.WriteString("<m:d>")
		var pr strings.Builder
		if n.beg != "(" {
			pr.WriteString(val("begChr", n.beg))
		}
		if n.sep != "|" {
			pr.WriteString(val("sepChr", n.sep))
		}
		if n.end != ")" {
			pr.WriteString(val("endChr", n.end))
		}
		if pr.Len() > 0 {
			b.WriteString("<m:dPr>" + pr.String() + "</m:dPr>")
		}
		for _, e := range n.entries {
			writeOMMLArg(b, "e", e)
		}
		b.WriteString("</m:d>")
	case matrix:
		b.WriteString("<m:m>")
		for _, r := range n.rows {
			b.WriteString("<m:mr>")
			for _, c := range r {
				writeOMMLArg(b, "e", c)
			}
			b.WriteString("</m:mr>")
		}
		b.WriteString("</m:m>")
	case eqArr:
		b.WriteString("<m:eqArr>")
		for _, r := range n.rows {
			writeOMMLArg(b, "e", r)
		}
		b.WriteString("</m:eqArr>")
	case accent:
		b.WriteString("<m:acc><m:accPr>" + val("chr", n.chr) + "</m:accPr>")
		writeOMMLArg(b, "e", n.base)
		b.WriteString("</m:acc>")
	case bar:
		pos := "bot"
		if n.top {
			pos = "top"
		}
		b.WriteString("<m:bar><m:barPr>" + val("pos", pos) + "</m:barPr>")
		writeOMMLArg(b, "e", n.base)
		b.WriteString("</m:bar>")
	case group:
		pos, jc := "bot", "top"
		if n.top {
			pos, jc = "top", "bot"
		}
		b.WriteString("<m:groupChr><m:groupChrPr>" + val("chr", n.chr) + val("pos", pos) + val("vertJc", jc) + "</m:groupChrPr>")
		writeOMMLArg(b, "e", n.base)
		b.WriteString("</m:groupChr>")
	case limit:
		name := "limLow"
		if n.upper {
			name = "limUpp"
		}
		b.WriteString("<m:" + name + ">")
		writeOMMLArg(b, "e", n.base)
		writeOMMLArg(b, "lim", n.lim)
		b.WriteString("</m:" + name + ">")
	case function:
		b.WriteString("<m:func>")
		writeOMMLArg(b, "fName", n.name)
		writeOMMLArg(b, "e", n.arg)
		b.WriteString("</m:func>")
	case phantom:
		b.WriteString("<m:phant>")
		pr := ""
		if !n.show {
			pr += val("show", "0")
		}
		pr += onIf("zeroWid", n.zeroWid) + onIf("zeroAsc", n.zeroAsc) + onIf("zeroDesc", n.zeroDesc) + onIf("transp", n.transp)
		if pr != "" {
			b.WriteString("<m:phantPr>" + pr + "</m:phantPr>")
		}
		writeOMMLArg(b, "e", n.base)
		b.WriteString("</m:phant>")
	case borderBox:
		b.WriteString("<m:borderBox>")
		pr := ""
		for i, f := range append(n.hide[:], n.strike[:]...) {
			pr += onIf(borderFlags[i], f)
		}
		if pr != "" {
			b.WriteString("<m:borderBoxPr>" + pr + "</m:borderBoxPr>")
		}
		writeOMMLArg(b, "e", n.base)
		b.WriteString("</m:borderBox>")
	case box:
		b.WriteString("<m:box>")
		if pr := onIf("opEmu", n.opEmu) + onIf("noBreak", n.noBreak) + onIf("diff", n.diff); pr != "" {
			b.WriteString("<m:boxPr>" + pr + "</m:boxPr>")
		}
		writeOMMLArg(b, "e", n.base)
		b.WriteString("</m:box>")
	}
}
