package text

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/jhnoel/documentdriver/internal/corpustest"
	"github.com/jhnoel/documentdriver/internal/ooxml/tree"
	"github.com/jhnoel/documentdriver/internal/opc"
	"github.com/jhnoel/documentdriver/internal/pptx/derive"
	"github.com/jhnoel/documentdriver/internal/testdeck"
)

// tally counts law outcomes, with reasons.
type tally map[string]int

func (t tally) String() string {
	var ks []string
	for k := range t {
		ks = append(ks, k)
	}
	sort.Strings(ks)
	var b strings.Builder
	for _, k := range ks {
		fmt.Fprintf(&b, "\n  %6d  %s", t[k], k)
	}
	return b.String()
}

// eachBody calls fn for every p:txBody in every slide of a deck, with the
// slide part's bytes and the body parsed from them.
func eachBody(_ *testing.T, path string, fn func(src []byte, body *tree.Node)) error {
	p, err := opc.Open(path)
	if err != nil {
		return err
	}
	defer p.Close()
	for _, name := range p.Names() {
		if !strings.HasPrefix(name, "/ppt/slides/") || !strings.HasSuffix(name, ".xml") || strings.Contains(name, "_rels") {
			continue
		}
		part, err := p.Part(name)
		if err != nil {
			return err
		}
		src, err := part.Bytes()
		if err != nil {
			return err
		}
		doc, err := tree.Parse(src)
		if err != nil {
			continue // not a document the read model reads either
		}
		doc.Root().Walk(func(n *tree.Node) bool {
			if derive.Is(n, derive.PML, "txBody") {
				fn(src, n)
				return false
			}
			return true
		})
	}
	return nil
}

// reparsed splices frag into src in place of body's paragraphs and
// returns the body as parsed from the result.
func reparsed(src []byte, body *tree.Node, frag []byte) (*tree.Node, error) {
	start, _, _ := body.Span()
	doc, err := tree.Parse(spliceParagraphs(src, body, frag))
	if err != nil {
		return nil, err
	}
	var out *tree.Node
	doc.Root().Walk(func(n *tree.Node) bool {
		if s, _, _ := n.Span(); s == start && derive.Is(n, derive.PML, "txBody") {
			out = n
		}
		return out == nil
	})
	if out == nil {
		return nil, errors.New("body not found after splice")
	}
	return out, nil
}

// perturb edits a body the way an agent might: text appended to each
// paragraph's last run, bold toggled on its first, a scheme colour on its
// second, the first paragraph right-aligned, and a new paragraph after
// the first.
func perturb(b Body) Body {
	var out Body
	for i, p := range b.Paras {
		q := p
		q.Runs = append([]Run(nil), p.Runs...)
		var text []int
		for k, r := range q.Runs {
			if !r.Br {
				text = append(text, k)
			}
		}
		if len(text) == 0 {
			q.Runs = append(q.Runs, Run{Text: "x"})
		} else {
			q.Runs[text[len(text)-1]].Text += " x"
			f := &q.Runs[text[0]].Fmt
			if f.B == "on" {
				f.B = ""
			} else {
				f.B = "on"
			}
			if len(text) > 1 {
				q.Runs[text[1]].Fmt.Fill = Fill{Kind: "solid", Color: Color{Color: derive.Color{Kind: "scheme", Val: "accent3"}}}
			}
		}
		q.Runs = canonical(q.Runs)
		if i == 0 {
			q.Algn = "r"
		}
		out.Paras = append(out.Paras, q)
		if i == 0 {
			n := Para{Lvl: p.Lvl, Bullet: p.Bullet, Runs: []Run{{Text: "new", Fmt: Fmt{Size: "20"}}}}
			if n.Bullet == "picture" {
				n.Bullet = ""
			}
			out.Paras = append(out.Paras, n)
		}
	}
	return out
}

// paraDiff names why a paragraph built from HTML alone differs from the
// original: the first property HTML does not carry that it holds.
func paraDiff(o *tree.Node) string {
	var reasons []string
	add := func(s string) {
		for _, r := range reasons {
			if r == s {
				return
			}
		}
		reasons = append(reasons, s)
	}
	hidden := func(e *tree.Node, kind string, shown map[string]bool) {
		if e == nil {
			return
		}
		for _, a := range e.Attrs() {
			if !shown[a.Name] {
				add(kind + "@" + a.Name)
			}
		}
		for _, c := range e.Elements() {
			if !shown[c.Local()] {
				add(kind + "/" + c.Local())
			}
		}
	}
	rShown := set("b", "i", "u", "sz", "solidFill", "noFill", "latin")
	pShown := set("algn", "lvl", "buNone", "buChar", "buAutoNum")
	for _, c := range o.Elements() {
		switch c.Local() {
		case "pPr":
			hidden(c, "pPr", pShown)
		case "r":
			hidden(derive.Child(c, derive.DML, "rPr"), "rPr", rShown)
			if derive.Child(c, derive.DML, "rPr") != nil && len(derive.Child(c, derive.DML, "rPr").Attrs()) == 0 && len(derive.Child(c, derive.DML, "rPr").Elements()) == 0 {
				add("empty rPr")
			}
		case "br":
			if derive.Child(c, derive.DML, "rPr") != nil {
				add("br/rPr")
			}
		case "fld":
			add("fld")
		case "endParaRPr":
			add("endParaRPr")
		default:
			add(c.Name())
		}
	}
	if len(reasons) == 0 {
		return "serialisation (attribute order, quoting, empty runs, merged plain runs)"
	}
	sort.Strings(reasons)
	return reasons[0]
}

// checkBodies runs the package's own laws over one deck's text bodies.
func checkBodies(t *testing.T, path string, c tally) {
	err := eachBody(t, path, func(src []byte, body *tree.Node) {
		c["bodies"]++
		h := Render(ReadBody(body))
		want, err := Parse(h)
		if err != nil {
			c["parse of shown html failed: "+err.Error()]++
			t.Errorf("%s: shown html does not parse: %v\n%s", path, err, h)
			return
		}
		if got := Render(want); got != h {
			c["normalise not a fixed point"]++
			t.Errorf("%s: Normalise(%s) = %s", path, h, got)
		}
		// Identity: building from the shown html writes nothing.
		if _, changed, err := Build(body, want); err != nil || changed {
			c["identity set not a no-op"]++
			t.Errorf("%s: identity set changed=%v err=%v: %s", path, changed, err, h)
		} else {
			c["identity no-op"]++
		}
		// Forced rewrite: every paragraph rewritten from the same HTML,
		// carrying from itself. Which come out byte-identical, and why
		// the others do not.
		if rw, _, err := build(body, want, rewrite, nil); err != nil {
			c["rewrite refused: "+reason(err)]++
		} else if nb, err := reparsed(src, body, rw); err != nil {
			t.Errorf("%s: rewrite does not parse: %v", path, err)
		} else {
			if got := key(ReadBody(nb)); got != key(want) {
				c["rewrite reads back differently"]++
				t.Errorf("%s: rewrite reads back\n%s\nwant\n%s", path, got, key(want))
			}
			olds, news := paragraphs(body), paragraphs(nb)
			for k := range olds {
				c["rewritten paragraphs"]++
				if k < len(news) && bytes.Equal(olds[k].Bytes(), news[k].Bytes()) {
					c["rewritten paragraph identical"]++
				} else if k < len(news) {
					c["rewritten paragraph differs: "+rewriteDiff(olds[k], news[k])]++
				}
			}
		}
		// Faithfulness: built from HTML alone, which paragraphs come out
		// byte-identical, and why the others do not.
		fresh, _, err := build(body, want, carryNone, nil)
		if err != nil {
			c["fresh build refused: "+reason(err)]++
		} else {
			nb, err := reparsed(src, body, fresh)
			if err != nil {
				t.Errorf("%s: fresh build does not parse: %v", path, err)
				return
			}
			if got := key(ReadBody(nb)); got != key(want) {
				c["fresh build reads back differently"]++
				t.Errorf("%s: fresh build reads back\n%s\nwant\n%s", path, got, key(want))
			}
			var olds, news []*tree.Node
			for _, p := range body.Elements() {
				if derive.Is(p, derive.DML, "p") {
					olds = append(olds, p)
				}
			}
			for _, p := range nb.Elements() {
				if derive.Is(p, derive.DML, "p") {
					news = append(news, p)
				}
			}
			for k := range olds {
				c["paragraphs"]++
				if k < len(news) && bytes.Equal(olds[k].Bytes(), news[k].Bytes()) {
					c["paragraph identical from HTML alone"]++
				} else {
					c["paragraph differs from HTML alone: "+paraDiff(olds[k])]++
				}
			}
		}
		// Edit round trip: a perturbed body reads back as written.
		pb := perturb(want)
		frag, _, err := Build(body, pb)
		if err != nil {
			c["perturbed set refused: "+reason(err)]++
		} else if nb, err := reparsed(src, body, frag); err != nil {
			c["perturbed set does not parse"]++
			t.Errorf("%s: perturbed build does not parse: %v\n%s", path, err, frag)
		} else if got := key(ReadBody(nb)); got != key(pb) {
			c["perturbed set reads back differently"]++
			t.Errorf("%s: perturbed set reads back\n%s\nwant\n%s", path, got, key(pb))
		} else {
			c["perturbed set reads back"]++
		}
		checkReplace(t, path, src, body, c)
	})
	if err != nil {
		c["deck unreadable"]++
	}
}

// checkReplace replaces the text of one run that occurs once in the body
// and, where possible, a text spanning two adjacent runs, and checks that
// nothing else changed.
func checkReplace(t *testing.T, path string, src []byte, body *tree.Node, c tally) {
	var all strings.Builder
	type cand struct {
		pi, k int
		it    []item
	}
	var cands []cand
	var paras []*tree.Node
	for _, p := range body.Elements() {
		if derive.Is(p, derive.DML, "p") {
			its := items(p, nil)
			for k := range its {
				cands = append(cands, cand{len(paras), k, its})
			}
			if len(paras) > 0 {
				all.WriteString("\n")
			}
			paras = append(paras, p)
			// The text Replace searches: runs joined, line breaks as \v.
			for _, it := range its {
				switch {
				case it.run.Br:
					all.WriteString("\v")
				case it.text != nil:
					all.WriteString(it.text.Text())
				}
			}
		}
	}
	text := all.String()
	plainRun := func(it item) bool {
		return it.mc == nil && it.text != nil && derive.Is(it.el, derive.DML, "r") && it.text.Text() != ""
	}
	single, span := false, false
	for _, cd := range cands {
		it := cd.it[cd.k]
		if !plainRun(it) {
			continue
		}
		run := it.text.Text()
		if !single && strings.Count(text, run) == 1 {
			single = true
			replaceCase(t, path, src, body, c, run, cd.pi, "single run")
		}
		if !span && cd.k+1 < len(cd.it) && plainRun(cd.it[cd.k+1]) {
			a, b := []rune(run), []rune(cd.it[cd.k+1].text.Text())
			find := string(a[len(a)-1:]) + string(b[:1])
			if countOverlapping(text, find) == 1 {
				span = true
				replaceCase(t, path, src, body, c, find, cd.pi, "spanning runs")
			}
		}
	}
}

func replaceCase(t *testing.T, path string, src []byte, body *tree.Node, c tally, find string, pi int, kind string) {
	with := "«" + find + "»"
	frag, n, err := Replace(body, find, with, false, 0)
	if err != nil || n != 1 {
		c["replace ("+kind+") failed"]++
		t.Errorf("%s: replace %q: %d, %v", path, find, n, err)
		return
	}
	nb, err := reparsed(src, body, frag)
	if err != nil {
		c["replace ("+kind+") does not parse"]++
		t.Errorf("%s: replace %q does not parse: %v", path, find, err)
		return
	}
	var olds, news []*tree.Node
	for _, p := range body.Elements() {
		if derive.Is(p, derive.DML, "p") {
			olds = append(olds, p)
		}
	}
	for _, p := range nb.Elements() {
		if derive.Is(p, derive.DML, "p") {
			news = append(news, p)
		}
	}
	if len(olds) != len(news) {
		t.Errorf("%s: replace changed the paragraph count", path)
		return
	}
	for k := range olds {
		if k == pi {
			continue
		}
		if !bytes.Equal(olds[k].Bytes(), news[k].Bytes()) {
			c["replace ("+kind+") changed another paragraph"]++
			t.Errorf("%s: replace %q changed paragraph %d", path, find, k)
			return
		}
	}
	// In the changed paragraph, every run but the one where the match
	// starts (and the one it merges, for a spanning match) keeps its
	// bytes, and the text reads as replaced.
	oi, ni := items(olds[pi], nil), items(news[pi], nil)
	drop := 0
	if kind == "spanning runs" {
		drop = 1
	}
	if len(ni) != len(oi)-drop {
		c["replace ("+kind+") wrong run count"]++
		t.Errorf("%s: replace %q: %d runs, want %d", path, find, len(ni), len(oi)-drop)
		return
	}
	changed := 0
	for k := range ni {
		ok := k
		if k > 0 && drop > 0 && changed > 0 {
			ok = k + drop
		}
		if !bytes.Equal(ni[k].el.Bytes(), oi[ok].el.Bytes()) {
			changed++
			if r1, r0 := derive.Child(ni[k].el, derive.DML, "rPr"), derive.Child(oi[ok].el, derive.DML, "rPr"); (r1 == nil) != (r0 == nil) || r1 != nil && !bytes.Equal(r1.Bytes(), r0.Bytes()) {
				c["replace ("+kind+") changed formatting"]++
				t.Errorf("%s: replace %q changed a run's rPr", path, find)
			}
		}
	}
	if changed != 1 {
		c["replace ("+kind+") changed other runs"]++
		t.Errorf("%s: replace %q changed %d runs", path, find, changed)
		return
	}
	before, after := plain(olds[pi]), plain(news[pi])
	if strings.Replace(before, find, with, 1) != after {
		c["replace ("+kind+") wrong text"]++
		t.Errorf("%s: replace %q: %q -> %q", path, find, before, after)
		return
	}
	c["replace ("+kind+") ok"]++
}

func paragraphs(body *tree.Node) []*tree.Node {
	var out []*tree.Node
	for _, p := range body.Elements() {
		if derive.Is(p, derive.DML, "p") {
			out = append(out, p)
		}
	}
	return out
}

var reDigits = regexp.MustCompile(`[0-9]+`)

// reason is an error message with numbers masked, for tallies.
func reason(err error) string { return reDigits.ReplaceAllString(err.Error(), "N") }

// rewriteDiff names why a rewritten paragraph differs from the original.
func rewriteDiff(o, n *tree.Node) string {
	oi, ni := items(o, nil), items(n, nil)
	for _, it := range oi {
		if it.mc != nil {
			return "mc:AlternateContent in the paragraph"
		}
		if !it.run.Br && it.run.Text == "" && it.run.Fmt.zero() {
			return "empty unformatted run dropped"
		}
	}
	if len(oi) != len(ni) {
		return "adjacent unformatted runs merged"
	}
	if !bytes.Equal(o.StartTag(), n.StartTag()) {
		return "a:p start tag re-serialised"
	}
	var ob, nb []byte
	for _, c := range o.Children() {
		if c.Kind() != tree.ElementNode {
			return "whitespace or comments between runs"
		}
		ob = append(ob, c.Bytes()...)
	}
	for _, c := range n.Children() {
		nb = append(nb, c.Bytes()...)
	}
	if bytes.Equal(ob, nb) {
		return "a:p start or end tag re-serialised"
	}
	return "other"
}

func plain(p *tree.Node) string {
	var b strings.Builder
	for _, it := range items(p, nil) {
		if it.text != nil {
			b.WriteString(it.text.Text())
		}
	}
	return b.String()
}

func TestLawsFixtures(t *testing.T) {
	c := tally{}
	for name, path := range testdeck.All(t) {
		t.Run(name, func(t *testing.T) { checkBodies(t, path, c) })
	}
	t.Log(c)
	if c["bodies"] == 0 || c["perturbed set reads back"] == 0 || c["replace (single run) ok"] == 0 {
		t.Fatalf("laws exercised nothing:%v", c)
	}
}

func TestLawsCorpus(t *testing.T) {
	var on []string
	missing := 0
	for _, d := range testdeck.Native(t) {
		if _, err := os.Stat(d); err != nil {
			missing++
			continue
		}
		on = append(on, d)
	}
	// Deck by deck in parallel, each into a tally of its own.
	c := tally{}
	for _, r := range corpustest.Run(t, corpustest.Files(on, os.Getenv("PPTX_CORPUS")), func(t *testing.T, f corpustest.File, d *tally) {
		*d = tally{}
		checkBodies(t, f.Path, *d)
	}) {
		for k, v := range r.Value {
			c[k] += v
		}
	}
	t.Logf("%d manifest decks missing on disk%v", missing, c)
}

// countOverlapping counts occurrences of sub in s, overlapping ones
// included, so that a once-only text is found where it was taken.
func countOverlapping(s, sub string) int {
	n := 0
	for i := 0; i < len(s); {
		k := strings.Index(s[i:], sub)
		if k < 0 {
			break
		}
		n++
		i += k + 1
	}
	return n
}
