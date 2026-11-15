package text

import (
	"bytes"
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"

	"github.com/jhnoel/documentdriver/internal/ooxml/tree"
	"github.com/jhnoel/documentdriver/internal/pptx/derive"
)

// ErrInexpressible is returned when HTML asks for something the builder
// cannot write from HTML alone (a picture bullet or a gradient text fill
// that the replaced paragraph or run does not already have).
var ErrInexpressible = errors.New("text: not expressible from HTML")

// ErrUnsupported is returned when a paragraph that must be rewritten
// holds content HTML does not show (an equation in mc:AlternateContent,
// or an element the paragraph schema does not list), which a rewrite
// would lose.
var ErrUnsupported = errors.New("text: paragraph holds content text.set cannot rewrite")

// Build returns the a:p elements that make body show want, as XML bytes
// for the range from body's first a:p to its last, and whether they differ
// from the paragraphs there now.
//
// Paragraphs whose HTML is unchanged keep their bytes. A changed
// paragraph is paired with the one it replaces (see pairGap: by order
// between unchanged ones when the counts agree, else by text similarity)
// and keeps that paragraph's start tag, a:endParaRPr and
// whatever its a:pPr says that HTML does not show; within it, runs whose
// HTML is unchanged keep their bytes, and a new or changed run takes the
// a:rPr of the old run covering where it starts (by the text before and
// after the change) with the shown properties set as the HTML says. A
// paragraph with no counterpart takes the a:pPr of the paragraph before
// it (the one after it, when first), as pressing Enter does, and only
// the HTML's run formatting. Nothing the HTML does not state is invented.
//
// New elements use the DrawingML prefix in scope at body, or declare
// prefix a on each new paragraph when none is.
func Build(body *tree.Node, want Body) ([]byte, bool, error) {
	return BuildWith(body, want, nil)
}

// BuildWith is Build with the relationships of the body's part, through
// which hyperlinks are read, reused or added (nil: links show as
// unresolved and none can be created).
func BuildWith(body *tree.Node, want Body, rels Rels) ([]byte, bool, error) {
	return build(body, want, carryAll, rels)
}

// buildMode selects how much build keeps, for the laws.
type buildMode int

const (
	carryAll  buildMode = iota // Build
	carryNone                  // nothing kept or carried: HTML alone
	rewrite                    // no paragraph kept whole: every one paired and rewritten
)

// build is Build in the given mode.
func build(body *tree.Node, want Body, mode buildMode, rels Rels) ([]byte, bool, error) {
	var olds []*tree.Node
	for _, c := range body.Elements() {
		if derive.Is(c, derive.DML, "p") {
			olds = append(olds, c)
		}
	}
	paras := make([]Para, len(want.Paras))
	for i, p := range want.Paras {
		p.Runs = canonical(p.Runs)
		paras[i] = p
	}
	if len(paras) == 0 {
		paras = []Para{{Lvl: 1}} // the schema needs one a:p
	}
	b := newBuilder(body, rels)
	oldKeys := make([]string, len(olds))
	for i, o := range olds {
		oldKeys[i] = paraKey(readPara(o, b.ctx))
	}
	newKeys := make([]string, len(paras))
	for i, p := range paras {
		newKeys[i] = paraKey(p)
	}
	match := make([]int, len(paras)) // old index matched (unchanged), -1
	pair := make([]int, len(paras))  // old index paired (rewritten), -1
	for i := range match {
		match[i], pair[i] = -1, -1
	}
	if mode != carryNone {
		if mode == carryAll {
			for _, m := range lcs(oldKeys, newKeys) {
				match[m[1]] = m[0]
			}
		}
		// Pair the rest within each gap between matches.
		oldText := make([]string, len(olds))
		for i, o := range olds {
			oldText[i] = plainText(readPara(o, b.ctx))
		}
		oi := 0
		for ni := 0; ni < len(paras); {
			if match[ni] >= 0 {
				oi = match[ni] + 1
				ni++
				continue
			}
			nj := ni
			for nj < len(paras) && match[nj] < 0 {
				nj++
			}
			oj := len(olds)
			if nj < len(paras) {
				oj = match[nj]
			}
			var nt []string
			for _, p := range paras[ni:nj] {
				nt = append(nt, plainText(p))
			}
			for _, pr := range pairGap(oldText[oi:oj], nt) {
				pair[ni+pr[1]] = oi + pr[0]
			}
			ni = nj
		}
	}
	// A paragraph that goes away must not take content HTML does not
	// show with it.
	kept := make([]bool, len(olds))
	for i := range paras {
		if match[i] >= 0 {
			kept[match[i]] = true
		}
	}
	for k, o := range olds {
		if el := hiddenContent(o); !kept[k] && el != nil {
			return nil, false, fmt.Errorf("%w: paragraph %d holds <%s>, which HTML does not show; keep its HTML unchanged where it is (use xml.set to remove it)", ErrUnsupported, k+1, el.Name())
		}
	}
	unchanged := len(paras) == len(olds) && mode == carryAll
	for i := range paras {
		if match[i] != i {
			unchanged = false
		}
	}
	if unchanged {
		var out bytes.Buffer
		for _, o := range olds {
			out.Write(o.Bytes())
		}
		return out.Bytes(), false, nil
	}
	var out bytes.Buffer
	for i, p := range paras {
		switch {
		case match[i] >= 0:
			out.Write(olds[match[i]].Bytes())
		case pair[i] >= 0:
			if err := b.para(&out, olds[pair[i]], p, i); err != nil {
				return nil, false, err
			}
		default:
			if err := b.newPara(&out, p, mode != carryNone, neighbour(olds, match, pair, i)); err != nil {
				return nil, false, err
			}
		}
	}
	return out.Bytes(), true, nil
}

// hiddenContent returns the first child of paragraph p that HTML does not
// show and a rewrite would lose (an equation in mc:AlternateContent, or
// an element the schema does not list), nil if none.
func hiddenContent(p *tree.Node) *tree.Node {
	for _, c := range p.Elements() {
		switch {
		case derive.Is(c, derive.DML, "pPr"), derive.Is(c, derive.DML, "endParaRPr"),
			derive.Is(c, derive.DML, "r"), derive.Is(c, derive.DML, "br"), derive.Is(c, derive.DML, "fld"):
		default:
			return c
		}
	}
	return nil
}

// plainText is a paragraph's text, line breaks as newlines.
func plainText(p Para) string {
	var b strings.Builder
	for _, r := range p.Runs {
		if r.Br {
			b.WriteByte('\n')
		} else {
			b.WriteString(r.Text)
		}
	}
	return b.String()
}

// similarity is the share of the longer text that two texts have in
// common at their start and end, in [0, 1]; two empty texts are alike.
func similarity(a, b string) float64 {
	ra, rb := []rune(a), []rune(b)
	n := max(len(ra), len(rb))
	if n == 0 {
		return 1
	}
	pre := 0
	for pre < len(ra) && pre < len(rb) && ra[pre] == rb[pre] {
		pre++
	}
	suf := 0
	for suf < len(ra)-pre && suf < len(rb)-pre && ra[len(ra)-1-suf] == rb[len(rb)-1-suf] {
		suf++
	}
	return float64(pre+suf) / float64(n)
}

// minSimilarity is the least similarity at which a changed paragraph in a
// gap whose old and new counts differ is taken to replace an old one.
const minSimilarity = 0.3

// pairGap pairs the changed paragraphs of one gap between unchanged
// ones (old texts olds, new texts news) with the old paragraphs they
// replace, in order. With as many new paragraphs as old, they pair by
// order: nothing says one was inserted. Otherwise the pairing is the
// order-keeping alignment of greatest total text similarity, using only
// pairs at least minSimilarity alike, and the stretches between its pairs
// with equal counts pair by order.
func pairGap(olds, news []string) [][2]int {
	m, n := len(olds), len(news)
	var out [][2]int
	if m == n {
		for k := range m {
			out = append(out, [2]int{k, k})
		}
		return out
	}
	if m == 0 || n == 0 || m*n > 1_000_000 {
		return nil
	}
	best := make([][]float64, m+1)
	for i := range best {
		best[i] = make([]float64, n+1)
	}
	sim := func(i, j int) float64 { return similarity(olds[i], news[j]) }
	for i := m - 1; i >= 0; i-- {
		for j := n - 1; j >= 0; j-- {
			v := max(best[i+1][j], best[i][j+1])
			if s := sim(i, j); s >= minSimilarity {
				v = max(v, s+best[i+1][j+1])
			}
			best[i][j] = v
		}
	}
	var pairs [][2]int
	for i, j := 0, 0; i < m && j < n; {
		s := sim(i, j)
		switch {
		case s >= minSimilarity && best[i][j] == s+best[i+1][j+1]:
			pairs = append(pairs, [2]int{i, j})
			i++
			j++
		case best[i][j] == best[i+1][j]:
			i++
		default:
			j++
		}
	}
	// Stretches between aligned pairs with equal counts pair by order.
	pi, pj := 0, 0
	for k := 0; k <= len(pairs); k++ {
		ei, ej := m, n
		if k < len(pairs) {
			ei, ej = pairs[k][0], pairs[k][1]
		}
		if ei-pi == ej-pj {
			for d := 0; d < ei-pi; d++ {
				out = append(out, [2]int{pi + d, pj + d})
			}
		}
		if k < len(pairs) {
			out = append(out, pairs[k])
			pi, pj = ei+1, ej+1
		}
	}
	return out
}

// neighbour returns the old paragraph whose a:pPr an unpaired new
// paragraph i takes: the one matched or paired with the nearest new
// paragraph before it, else after it; nil if none.
func neighbour(olds []*tree.Node, match, pair []int, i int) *tree.Node {
	at := func(k int) *tree.Node {
		if match[k] >= 0 {
			return olds[match[k]]
		}
		if pair[k] >= 0 {
			return olds[pair[k]]
		}
		return nil
	}
	for k := i - 1; k >= 0; k-- {
		if o := at(k); o != nil {
			return o
		}
	}
	for k := i + 1; k < len(match); k++ {
		if o := at(k); o != nil {
			return o
		}
	}
	return nil
}

// paraKey returns the HTML key for a paragraph without displayed colour values.
func paraKey(p Para) string {
	var w strings.Builder
	renderPara(&w, p, "p", false)
	return w.String()
}

// runKey returns the HTML key for a run without displayed colour values.
func runKey(r Run) string {
	var w strings.Builder
	renderRun(&w, r, false, "")
	return w.String()
}

// fmtKey returns the HTML key for a run carrying only formatting f.
func fmtKey(f Fmt) string { return runKey(Run{Fmt: f}) }

// lcs returns the index pairs (old, new) of a longest common subsequence
// of a and b, in order.
func lcs(a, b []string) [][2]int {
	n, m := len(a), len(b)
	// Trim the common prefix and suffix first: the usual case is a small
	// change in a long body.
	pre := 0
	for pre < n && pre < m && a[pre] == b[pre] {
		pre++
	}
	suf := 0
	for suf < n-pre && suf < m-pre && a[n-1-suf] == b[m-1-suf] {
		suf++
	}
	var out [][2]int
	for k := 0; k < pre; k++ {
		out = append(out, [2]int{k, k})
	}
	a2, b2 := a[pre:n-suf], b[pre:m-suf]
	if len(a2) > 0 && len(b2) > 0 && len(a2)*len(b2) <= 4_000_000 {
		la, lb := len(a2), len(b2)
		t := make([][]int32, la+1)
		for i := range t {
			t[i] = make([]int32, lb+1)
		}
		for i := la - 1; i >= 0; i-- {
			for j := lb - 1; j >= 0; j-- {
				if a2[i] == b2[j] {
					t[i][j] = t[i+1][j+1] + 1
				} else {
					t[i][j] = max(t[i+1][j], t[i][j+1])
				}
			}
		}
		for i, j := 0, 0; i < la && j < lb; {
			switch {
			case a2[i] == b2[j]:
				out = append(out, [2]int{pre + i, pre + j})
				i++
				j++
			case t[i+1][j] >= t[i][j+1]:
				i++
			default:
				j++
			}
		}
	}
	for k := 0; k < suf; k++ {
		out = append(out, [2]int{n - suf + k, m - suf + k})
	}
	return out
}

// builder writes elements with the DrawingML prefix in scope at a body.
type builder struct {
	prefix  string // "" for the default namespace
	uri     string
	declare bool // no prefix in scope: declare it on new paragraphs
	strict  bool
	// rels are the part's relationships (nil when not available) and ctx
	// reads through them.
	rels Rels
	ctx  Ctx
	body *tree.Node
}

// newBuilder returns a builder for writing elements under a body.
func newBuilder(body *tree.Node, rels Rels) *builder {
	b := &builder{rels: rels, body: body}
	if rels != nil {
		b.ctx = rels
	}
	for _, u := range derive.DML {
		if p, ok := body.LookupNamespace(u); ok {
			b.prefix, b.uri, b.strict = p, u, u == derive.DML[1]
			return b
		}
	}
	b.uri = derive.DML[0]
	if derive.In(body, derive.PML) && body.Space() == derive.PML[1] {
		b.uri, b.strict = derive.DML[1], true // a strict body gets strict DrawingML
	}
	b.prefix, b.declare = "a", true
	return b
}

// name returns local with the builder's prefix when one is set.
func (b *builder) name(local string) string {
	if b.prefix == "" {
		return local
	}
	return b.prefix + ":" + local
}

// para rewrites old paragraph o to show p (the i'th new paragraph).
func (b *builder) para(out *bytes.Buffer, o *tree.Node, p Para, i int) error {
	ppr := derive.Child(o, derive.DML, "pPr")
	epr := derive.Child(o, derive.DML, "endParaRPr")
	var body bytes.Buffer
	pb, err := b.pPr(ppr, p)
	if err != nil {
		return fmt.Errorf("paragraph %d: %w", i+1, err)
	}
	body.Write(pb)
	if err := b.runs(&body, o, p.Runs); err != nil {
		return fmt.Errorf("paragraph %d: %w", i+1, err)
	}
	if epr != nil {
		body.Write(epr.Bytes())
	}
	writeElem(out, o.Name(), attrsOf(o), body.Bytes())
	return nil
}

// newPara writes paragraph p with no counterpart, taking the a:pPr of
// from (if carry).
func (b *builder) newPara(out *bytes.Buffer, p Para, carry bool, from *tree.Node) error {
	var src *tree.Node
	if carry && from != nil {
		src = derive.Child(from, derive.DML, "pPr")
	}
	var body bytes.Buffer
	pb, err := b.pPr(src, p)
	if err != nil {
		return err
	}
	body.Write(pb)
	for _, r := range p.Runs {
		rb, err := b.run(nil, r)
		if err != nil {
			return err
		}
		body.Write(rb)
	}
	var attrs []tree.Attribute
	if b.declare {
		attrs = append(attrs, tree.Attribute{Name: "xmlns:" + b.prefix, Value: b.uri})
	}
	writeElem(out, b.name("p"), attrs, body.Bytes())
	return nil
}

// runs writes the runs of a rewritten paragraph: old runs whose HTML is
// unchanged keep their bytes; the others take the a:rPr of the old run
// covering where they start.
func (b *builder) runs(out *bytes.Buffer, o *tree.Node, want []Run) error {
	its := items(o, b.ctx)
	oldKeys := make([]string, len(its))
	for i, it := range its {
		oldKeys[i] = runKey(it.run)
	}
	newKeys := make([]string, len(want))
	for i, r := range want {
		newKeys[i] = runKey(r)
	}
	match := make([]int, len(want))
	for i := range match {
		match[i] = -1
	}
	for _, m := range lcs(oldKeys, newKeys) {
		match[m[1]] = m[0]
	}
	cover := coverer(its, want)
	for i, r := range want {
		if match[i] >= 0 {
			out.Write(its[match[i]].el.Bytes())
			continue
		}
		var src *tree.Node
		if !r.Br {
			if k := cover(i); k >= 0 {
				src = its[k].el
			}
		}
		rb, err := b.run(src, r)
		if err != nil {
			return err
		}
		out.Write(rb)
	}
	return nil
}

// coverer returns, for each new run, the index of the old text item
// covering the position where it starts, mapping positions through the
// common prefix and suffix of the old and new texts; -1 if the old
// paragraph has no text runs.
func coverer(its []item, want []Run) func(i int) int {
	flat := func(texts []string) []rune {
		var r []rune
		for _, t := range texts {
			r = append(r, []rune(t)...)
		}
		return r
	}
	const brMark = '\u0000' // never in shown text
	var oldTexts, newTexts []string
	oldStart := make([]int, len(its))
	pos := 0
	for i, it := range its {
		t := it.run.Text
		if it.run.Br {
			t = string(brMark)
		}
		oldStart[i] = pos
		pos += len([]rune(t))
		oldTexts = append(oldTexts, t)
	}
	newStart := make([]int, len(want))
	pos = 0
	for i, r := range want {
		t := r.Text
		if r.Br {
			t = string(brMark)
		}
		newStart[i] = pos
		pos += len([]rune(t))
		newTexts = append(newTexts, t)
	}
	ot, nt := flat(oldTexts), flat(newTexts)
	pre := 0
	for pre < len(ot) && pre < len(nt) && ot[pre] == nt[pre] {
		pre++
	}
	suf := 0
	for suf < len(ot)-pre && suf < len(nt)-pre && ot[len(ot)-1-suf] == nt[len(nt)-1-suf] {
		suf++
	}
	return func(i int) int {
		k := newStart[i]
		var o int
		switch {
		case k < pre:
			o = k
		case k >= len(nt)-suf:
			o = k - len(nt) + len(ot)
		default:
			o = pre
		}
		last, first := -1, -1
		for j, it := range its {
			if it.run.Br {
				continue
			}
			if first < 0 {
				first = j
			}
			end := oldStart[j] + len([]rune(it.run.Text))
			if oldStart[j] <= o && o < end {
				return j
			}
			if oldStart[j] <= o {
				last = j
			}
		}
		if last >= 0 {
			return last
		}
		return first
	}
}

// run writes one run or line break; src is the old a:r or a:fld whose
// a:rPr it takes, or nil.
func (b *builder) run(src *tree.Node, r Run) ([]byte, error) {
	var out bytes.Buffer
	if r.Br {
		writeElem(&out, b.name("br"), nil, nil)
		return out.Bytes(), nil
	}
	var rpr *tree.Node
	if src != nil {
		rpr = derive.Child(src, derive.DML, "rPr")
	}
	rb, err := b.rPr(rpr, r.Fmt)
	if err != nil {
		return nil, err
	}
	var body bytes.Buffer
	body.Write(rb)
	body.WriteString("<" + b.name("t") + ">")
	body.Write(tree.EscapeText(r.Text))
	body.WriteString("</" + b.name("t") + ">")
	writeElem(&out, b.name("r"), nil, body.Bytes())
	return out.Bytes(), nil
}

// pPr writes the paragraph properties for p, starting from src (nil for
// none): src's bytes when it already shows p, else src with the shown
// properties set as p says. It writes nothing when there is nothing to
// say.
func (b *builder) pPr(src *tree.Node, p Para) ([]byte, error) {
	oa, ol, ob := "", 1, ""
	var of PFmt
	if src != nil {
		oa, ol, ob = pprShown(src)
		of = pfmtShown(src, b.ctx)
		if oa == p.Algn && ol == p.Lvl && ob == p.Bullet && samePF(of, p.PF) {
			return src.Bytes(), nil
		}
	}
	f := p.PF
	e := b.edit(src, "pPr", pPrSlots, pPrAttrs)
	if oa != p.Algn {
		e.setAttr("algn", p.Algn)
	}
	if ol != p.Lvl {
		v := ""
		if p.Lvl > 1 {
			v = strconv.Itoa(p.Lvl - 1)
		}
		e.setAttr("lvl", v)
	}
	for _, a := range []struct{ attr, want, cur string }{
		{"marL", f.MarL, of.MarL}, {"marR", f.MarR, of.MarR}, {"indent", f.Indent, of.Indent}, {"defTabSz", f.DefTab, of.DefTab},
	} {
		if a.want != a.cur {
			v, err := ptEMU(a.want)
			if err != nil {
				return nil, fmt.Errorf("%s: %w", a.attr, err)
			}
			e.setAttr(a.attr, v)
		}
	}
	if f.Dir != of.Dir {
		e.setAttr("rtl", map[string]string{"": "", "rtl": "1", "ltr": "0"}[f.Dir])
	}
	for _, s := range []struct{ el, want, cur string }{{"lnSpc", f.LnSpc, of.LnSpc}, {"spcBef", f.SpcBef, of.SpcBef}, {"spcAft", f.SpcAft, of.SpcAft}} {
		if s.want == s.cur {
			continue
		}
		var el string
		if s.want != "" {
			inner, err := b.spacing(s.want)
			if err != nil {
				return nil, fmt.Errorf("%s: %w", s.el, err)
			}
			el = "<" + b.name(s.el) + ">" + inner + "</" + b.name(s.el) + ">"
		}
		if err := e.replaceSlot(slotIndex(pPrSlots, s.el), el); err != nil {
			return nil, err
		}
	}
	if !sameFill(f.BuClr, of.BuClr) {
		var el string
		switch f.BuClr.Kind {
		case "text":
			el = b.empty("buClrTx", nil)
		case "solid":
			el = "<" + b.name("buClr") + ">" + b.color(f.BuClr.Color) + "</" + b.name("buClr") + ">"
		}
		if err := e.replaceSlot(slotIndex(pPrSlots, "buClr"), el); err != nil {
			return nil, err
		}
	}
	if f.BuSize != of.BuSize {
		var el string
		switch {
		case f.BuSize == "text":
			el = b.empty("buSzTx", nil)
		case strings.HasSuffix(f.BuSize, "%"):
			x, ok := undecimal(strings.TrimSuffix(f.BuSize, "%"), 3)
			if !ok {
				return nil, fmt.Errorf("bullet size %q", f.BuSize)
			}
			el = b.empty("buSzPct", []tree.Attribute{{Name: "val", Value: strconv.FormatInt(x, 10)}})
		case strings.HasSuffix(f.BuSize, "pt"):
			x, ok := undecimal(strings.TrimSuffix(f.BuSize, "pt"), 2)
			if !ok {
				return nil, fmt.Errorf("bullet size %q", f.BuSize)
			}
			el = b.empty("buSzPts", []tree.Attribute{{Name: "val", Value: strconv.FormatInt(x, 10)}})
		}
		if err := e.replaceSlot(slotIndex(pPrSlots, "buSzPct"), el); err != nil {
			return nil, err
		}
	}
	if f.BuFont != of.BuFont {
		var el string
		switch f.BuFont {
		case "":
		case "text":
			el = b.empty("buFontTx", nil)
		default:
			tf, err := fontFamily(f.BuFont)
			if err != nil {
				return nil, err
			}
			el = b.empty("buFont", []tree.Attribute{{Name: "typeface", Value: tf}})
		}
		if err := e.replaceSlot(slotIndex(pPrSlots, "buFont"), el); err != nil {
			return nil, err
		}
	}
	if ob != p.Bullet || of.StartAt != f.StartAt {
		var el string
		switch {
		case p.Bullet == "":
		case p.Bullet == "none":
			el = b.empty("buNone", nil)
		case p.Bullet == "picture":
			return nil, fmt.Errorf("%w: a picture bullet cannot be created from HTML", ErrInexpressible)
		case strings.HasPrefix(p.Bullet, "char:"):
			el = b.empty("buChar", []tree.Attribute{{Name: "char", Value: strings.TrimPrefix(p.Bullet, "char:")}})
		case strings.HasPrefix(p.Bullet, "autonum:"):
			attrs := []tree.Attribute{{Name: "type", Value: strings.TrimPrefix(p.Bullet, "autonum:")}}
			if f.StartAt != "" {
				attrs = append(attrs, tree.Attribute{Name: "startAt", Value: f.StartAt})
			}
			el = b.empty("buAutoNum", attrs)
		}
		if f.StartAt != "" && !strings.HasPrefix(p.Bullet, "autonum:") {
			return nil, fmt.Errorf("%w: a start number needs an autonumbered bullet", ErrInexpressible)
		}
		if err := e.replaceSlot(slotIndex(pPrSlots, "buChar"), el); err != nil {
			return nil, err
		}
	}
	if f.Tabs != of.Tabs {
		var el string
		if f.Tabs != "" {
			var stops bytes.Buffer
			for _, s := range strings.Fields(f.Tabs) {
				pos, algn, _ := strings.Cut(s, ":")
				v, err := ptEMU(pos)
				if err != nil {
					return nil, fmt.Errorf("tab stop %q: %w", s, err)
				}
				attrs := []tree.Attribute{{Name: "pos", Value: v}}
				if algn != "" {
					attrs = append(attrs, tree.Attribute{Name: "algn", Value: algn})
				}
				stops.WriteString(b.empty("tab", attrs))
			}
			el = "<" + b.name("tabLst") + ">" + stops.String() + "</" + b.name("tabLst") + ">"
		}
		if err := e.replaceSlot(slotIndex(pPrSlots, "tabLst"), el); err != nil {
			return nil, err
		}
	}
	return e.bytes(), nil
}

// samePF reports whether two paragraph formats show the same, colours
// compared as written.
func samePF(a, b PFmt) bool {
	key := func(f PFmt) [12]string {
		return [12]string{f.LnSpc, f.SpcBef, f.SpcAft, f.MarL, f.MarR, f.Indent, f.DefTab, f.Tabs, f.Dir, f.StartAt, f.BuFont, f.BuSize}
	}
	return key(a) == key(b) && sameFill(a.BuClr, b.BuClr)
}

// ptEMU turns a length shown in points ("18pt", "" for none) into its
// EMU attribute value.
func ptEMU(s string) (string, error) {
	if s == "" {
		return "", nil
	}
	x, err := strconv.ParseFloat(strings.TrimSuffix(s, "pt"), 64)
	if err != nil || !strings.HasSuffix(s, "pt") {
		return "", fmt.Errorf("length %q: want points", s)
	}
	return strconv.FormatInt(int64(derive.RoundEMU(x*derive.EMUPerPoint)), 10), nil
}

// spacing writes the child of a CT_TextSpacing element for "150%"
// (a:spcPct in 1000ths of a percent) or "12pt" (a:spcPts in 100ths of a
// point).
func (b *builder) spacing(s string) (string, error) {
	if v, ok := strings.CutSuffix(s, "%"); ok {
		x, ok := undecimal(v, 3)
		if !ok {
			return "", fmt.Errorf("spacing %q", s)
		}
		return b.empty("spcPct", []tree.Attribute{{Name: "val", Value: strconv.FormatInt(x, 10)}}), nil
	}
	if v, ok := strings.CutSuffix(s, "pt"); ok {
		x, ok := undecimal(v, 2)
		if !ok {
			return "", fmt.Errorf("spacing %q", s)
		}
		return b.empty("spcPts", []tree.Attribute{{Name: "val", Value: strconv.FormatInt(x, 10)}}), nil
	}
	return "", fmt.Errorf("spacing %q: want N%% or Npt", s)
}

// rPr writes the run properties for f, starting from src as pPr does.
func (b *builder) rPr(src *tree.Node, f Fmt) ([]byte, error) {
	old := rprShown(src, b.ctx)
	if src != nil && fmtKey(old) == fmtKey(f) {
		return src.Bytes(), nil
	}
	e := b.edit(src, "rPr", rPrSlots, rPrAttrs)
	tri := map[string]string{"": "", "on": "1", "off": "0"}
	if f.Size != old.Size {
		v := ""
		if f.Size != "" {
			x, err := strconv.ParseFloat(f.Size, 64)
			if err != nil {
				return nil, fmt.Errorf("font size %q: %w", f.Size, err)
			}
			v = strconv.FormatInt(int64(math.Round(x*100)), 10)
		}
		e.setAttr("sz", v)
	}
	if f.B != old.B {
		e.setAttr("b", tri[f.B])
	}
	if f.I != old.I {
		e.setAttr("i", tri[f.I])
	}
	if f.U != old.U {
		e.setAttr("u", f.U)
	}
	if !sameFill(f.Fill, old.Fill) {
		k := slotIndex(rPrSlots, "solidFill")
		e.removeSlot(k)
		var el string
		switch f.Fill.Kind {
		case "":
		case "none":
			el = b.empty("noFill", nil)
		case "solid":
			el = "<" + b.name("solidFill") + ">" + b.color(f.Fill.Color) + "</" + b.name("solidFill") + ">"
		default:
			return nil, fmt.Errorf("%w: a %s text fill cannot be created from HTML; keep the run's data-fill where it was", ErrInexpressible, f.Fill.Kind)
		}
		if el != "" {
			if err := e.insert(k, el); err != nil {
				return nil, err
			}
		}
	}
	if f.HasFont != old.HasFont || f.Font != old.Font {
		k := slotIndex(rPrSlots, "latin")
		e.removeSlot(k)
		if f.HasFont {
			if err := e.insert(k, b.empty("latin", []tree.Attribute{{Name: "typeface", Value: f.Font}})); err != nil {
				return nil, err
			}
		}
	}
	if f.Strike != old.Strike {
		e.setAttr("strike", f.Strike)
	}
	if f.Cap != old.Cap {
		e.setAttr("cap", f.Cap)
	}
	if f.Lang != old.Lang {
		e.setAttr("lang", f.Lang)
	}
	for _, a := range []struct {
		attr, want, cur, unit string
		scale                 int
	}{{"baseline", f.Baseline, old.Baseline, "%", 3}, {"spc", f.Spc, old.Spc, "pt", 2}} {
		if a.want == a.cur {
			continue
		}
		v := ""
		if a.want != "" {
			x, ok := undecimal(strings.TrimSuffix(a.want, a.unit), a.scale)
			if !ok {
				return nil, fmt.Errorf("%s %q", a.attr, a.want)
			}
			v = strconv.FormatInt(x, 10)
		}
		e.setAttr(a.attr, v)
	}
	if !sameFill(f.Highlight, old.Highlight) {
		var el string
		if f.Highlight.Kind == "solid" {
			el = "<" + b.name("highlight") + ">" + b.color(f.Highlight.Color) + "</" + b.name("highlight") + ">"
		}
		if err := e.replaceSlot(slotIndex(rPrSlots, "highlight"), el); err != nil {
			return nil, err
		}
	}
	if f.Link != old.Link {
		if err := b.link(e, src, old.Link, f.Link); err != nil {
			return nil, err
		}
	}
	return e.bytes(), nil
}

// color writes a colour element with its modifiers. A system colour gets
// its displayed hex, when known, as lastClr: the value PowerPoint caches
// there and the views show.
func (b *builder) color(c Color) string {
	var name string
	var attrs []tree.Attribute
	switch c.Kind {
	case "srgb":
		name, attrs = "srgbClr", []tree.Attribute{{Name: "val", Value: c.Val}}
	case "scheme":
		name, attrs = "schemeClr", []tree.Attribute{{Name: "val", Value: c.Val}}
	case "sys":
		name, attrs = "sysClr", []tree.Attribute{{Name: "val", Value: c.Val}}
		if c.Hex != "" {
			attrs = append(attrs, tree.Attribute{Name: "lastClr", Value: c.Hex})
		}
	case "prst":
		name, attrs = "prstClr", []tree.Attribute{{Name: "val", Value: c.Val}}
	case "hsl", "scrgb":
		names := [3]string{"hue", "sat", "lum"}
		name = "hslClr"
		if c.Kind == "scrgb" {
			names, name = [3]string{"r", "g", "b"}, "scrgbClr"
		}
		for i, n := range names {
			attrs = append(attrs, tree.Attribute{Name: n, Value: strconv.FormatInt(c.Comp[i], 10)})
		}
	}
	var mods bytes.Buffer
	for _, m := range c.Mods {
		var ma []tree.Attribute
		if m.Val != "" {
			ma = []tree.Attribute{{Name: "val", Value: m.Val}}
		}
		mods.WriteString(b.empty(m.Name, ma))
	}
	var out bytes.Buffer
	writeElem(&out, b.name(name), attrs, mods.Bytes())
	return out.String()
}

// hlinkAttrs is the attribute order of CT_Hyperlink (ECMA-376
// dml-main.xsd), used to place a tooltip.
var hlinkAttrs = []string{"r:id", "invalidUrl", "action", "tgtFrame", "tooltip", "history", "highlightClick", "endSnd"}

// link makes the run properties e (read from rpr) hold the click
// hyperlink want instead of old: removed, its tooltip changed in place
// when the target stays, or a new a:hlinkClick to an external hyperlink
// relationship of the part (reused when one with the same target exists,
// else added with the next free id).
func (b *builder) link(e *elemEdit, rpr *tree.Node, old, want Link) error {
	k := slotIndex(rPrSlots, "hlinkClick")
	switch {
	case want.zero():
		return e.replaceSlot(k, "")
	case want.Other != "":
		return fmt.Errorf("%w: a link shown as data-link=%q cannot be created from HTML; keep it where it was", ErrInexpressible, want.Other)
	}
	if old.Href == want.Href && rpr != nil {
		if h := derive.Child(rpr, derive.DML, "hlinkClick"); h != nil {
			he := b.edit(h, "hlinkClick", nil, hlinkAttrs)
			he.setAttr("tooltip", want.Title)
			return e.replaceSlot(k, string(he.bytes()))
		}
	}
	if b.rels == nil {
		return fmt.Errorf("%w: a hyperlink needs the part's relationships", ErrInexpressible)
	}
	rid, err := b.rels.Hyperlink(want.Href)
	if err != nil {
		return err
	}
	var attrs []tree.Attribute
	prefix := ""
	for _, u := range derive.REL {
		if p, ok := b.body.LookupNamespace(u); ok && p != "" {
			prefix = p
			break
		}
	}
	if prefix == "" {
		uri := derive.REL[0]
		if b.strict {
			uri = derive.REL[1]
		}
		prefix = "r"
		attrs = append(attrs, tree.Attribute{Name: "xmlns:r", Value: uri})
	}
	attrs = append(attrs, tree.Attribute{Name: prefix + ":id", Value: rid})
	if want.Title != "" {
		attrs = append(attrs, tree.Attribute{Name: "tooltip", Value: want.Title})
	}
	return e.replaceSlot(k, b.empty("hlinkClick", attrs))
}

// empty returns an empty element named local with the given attributes.
func (b *builder) empty(local string, attrs []tree.Attribute) string {
	var out bytes.Buffer
	writeElem(&out, b.name(local), attrs, nil)
	return out.String()
}

// elemEdit is an element being rewritten: its name, attributes and
// children (as bytes, with their schema slots).
type elemEdit struct {
	name      string
	attrs     []tree.Attribute
	attrOrder []string
	kids      []kid
}

type kid struct {
	b    []byte
	slot int  // schema slot; -1 unknown element
	elem bool // false for text, comments and other non-elements
}

// edit starts rewriting src (or a new element named local when src is
// nil).
func (b *builder) edit(src *tree.Node, local string, slots [][]string, attrOrder []string) *elemEdit {
	e := &elemEdit{name: b.name(local), attrOrder: attrOrder}
	if src == nil {
		return e
	}
	e.name = src.Name()
	e.attrs = attrsOf(src)
	for _, c := range src.Children() {
		if c.Kind() != tree.ElementNode {
			e.kids = append(e.kids, kid{b: c.Raw(), slot: -1})
			continue
		}
		s := -1
		if derive.In(c, derive.DML) {
			s = slotIndex(slots, c.Local())
		}
		e.kids = append(e.kids, kid{b: c.Bytes(), slot: s, elem: true})
	}
	return e
}

// setAttr sets an attribute, or removes it when v is "". A new attribute
// goes before the first existing one the schema orders after it.
func (e *elemEdit) setAttr(name, v string) {
	for i, a := range e.attrs {
		if a.Name == name {
			if v == "" {
				e.attrs = append(e.attrs[:i], e.attrs[i+1:]...)
			} else {
				e.attrs[i].Value = v
			}
			return
		}
	}
	if v == "" {
		return
	}
	rank := func(n string) int {
		for i, x := range e.attrOrder {
			if x == n {
				return i
			}
		}
		return -1
	}
	r := rank(name)
	at := len(e.attrs)
	for i, a := range e.attrs {
		if ra := rank(a.Name); ra > r {
			at = i
			break
		}
	}
	e.attrs = append(e.attrs[:at], append([]tree.Attribute{{Name: name, Value: v}}, e.attrs[at:]...)...)
}

// replaceSlot removes every child element in slot k and, unless el is
// "", inserts el there.
func (e *elemEdit) replaceSlot(k int, el string) error {
	if k < 0 {
		return fmt.Errorf("text: slot not in the schema table")
	}
	e.removeSlot(k)
	if el == "" {
		return nil
	}
	return e.insert(k, el)
}

// removeSlot removes every child element in slot k.
func (e *elemEdit) removeSlot(k int) {
	out := e.kids[:0]
	for _, c := range e.kids {
		if c.elem && c.slot == k {
			continue
		}
		out = append(out, c)
	}
	e.kids = out
}

// insert places element bytes el of slot k as Handle.Ensure would: before
// the first child the schema orders after it, else after the last it
// orders before it; it fails when only unknown elements could fix the
// place.
func (e *elemEdit) insert(k int, el string) error {
	at, before, unknown := -1, -1, false
	for i, c := range e.kids {
		switch {
		case !c.elem:
		case c.slot < 0:
			unknown = true
		case c.slot > k && at < 0:
			at = i
		case c.slot < k:
			before = i
		}
	}
	switch {
	case at >= 0:
	case before >= 0:
		at = before + 1
	case unknown:
		return fmt.Errorf("%w: cannot place <%s> among elements the schema table does not know", ErrUnsupported, el)
	default:
		at = len(e.kids)
	}
	e.kids = append(e.kids[:at], append([]kid{{b: []byte(el), slot: k, elem: true}}, e.kids[at:]...)...)
	return nil
}

// bytes serialises the element, or nothing when it has no attributes and
// no children.
func (e *elemEdit) bytes() []byte {
	if len(e.attrs) == 0 && len(e.kids) == 0 {
		return nil
	}
	var body bytes.Buffer
	for _, c := range e.kids {
		body.Write(c.b)
	}
	var out bytes.Buffer
	writeElem(&out, e.name, e.attrs, body.Bytes())
	return out.Bytes()
}

// attrsOf returns the attributes written on a node.
func attrsOf(n *tree.Node) []tree.Attribute { return n.Attrs() }

// writeElem writes an element with double-quoted attributes, in the
// empty form when it has no content.
func writeElem(out *bytes.Buffer, name string, attrs []tree.Attribute, content []byte) {
	out.WriteString("<" + name)
	for _, a := range attrs {
		out.WriteString(" " + a.Name + `="`)
		out.Write(escAttr(a.Value))
		out.WriteString(`"`)
	}
	if len(content) == 0 {
		out.WriteString("/>")
		return
	}
	out.WriteString(">")
	out.Write(content)
	out.WriteString("</" + name + ">")
}

// escAttr escapes an attribute value for double quotes, writing
// whitespace other than space as references so it survives
// normalisation.
func escAttr(s string) []byte {
	var b []byte
	for i := 0; i < len(s); i++ {
		switch c := s[i]; c {
		case '&':
			b = append(b, "&amp;"...)
		case '<':
			b = append(b, "&lt;"...)
		case '>':
			b = append(b, "&gt;"...)
		case '"':
			b = append(b, "&quot;"...)
		case '\t':
			b = append(b, "&#9;"...)
		case '\n':
			b = append(b, "&#10;"...)
		case '\r':
			b = append(b, "&#13;"...)
		default:
			b = append(b, c)
		}
	}
	return b
}
