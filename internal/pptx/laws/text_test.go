package laws

import (
	"archive/zip"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/jhnoel/documentdriver/internal/corpustest"
	"github.com/jhnoel/documentdriver/internal/ooxml/tree"
	"github.com/jhnoel/documentdriver/internal/opc"
	"github.com/jhnoel/documentdriver/internal/pptx/derive"
	"github.com/jhnoel/documentdriver/internal/pptx/model"
	"github.com/jhnoel/documentdriver/internal/pptx/ops/text"
	"github.com/jhnoel/documentdriver/internal/testdeck"
)

// The text laws through the read model:
//
//   - agreement: the read model's HTML parses in the text grammar, is a
//     fixed point of text.Normalise, and shows the same body as
//     text.ReadBody of the same p:txBody;
//   - set round trip: html(read(set(h))) == Normalise(h), for h an edit
//     of every text body (text appended, bold toggled, a scheme colour,
//     alignment, a new paragraph), all bodies of a deck written at once
//     and the deck read again by the read model.
//
// The package's own tests check the identity, rewrite and replace laws
// over the same decks without the read model.

type textTally map[string]int

func (t textTally) String() string {
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

var reNum = regexp.MustCompile(`[0-9]+`)

// textObject is one shown text body: the object's id, its HTML and its
// p:txBody in the slide part parsed separately.
type textObject struct {
	id   string
	html string
	body *tree.Node
}

// textObjects reads a slide through the read model and finds each text
// object's element in the slide part, parsed from src.
func textObjects(s *model.Slide, src []byte) ([]textObject, error) {
	doc, err := tree.Parse(src)
	if err != nil {
		return nil, err
	}
	byBytes := map[string][]*tree.Node{}
	doc.Root().Walk(func(n *tree.Node) bool {
		if derive.Child(n, derive.PML, "txBody") != nil {
			k := string(n.Bytes())
			byBytes[k] = append(byBytes[k], n)
		}
		return true
	})
	var out []textObject
	for _, o := range s.Objects {
		if o.Text == nil {
			continue
		}
		k := string(o.ElementXML())
		ns := byBytes[k]
		if len(ns) == 0 {
			return nil, fmt.Errorf("%s: element not found in the part", o.ID)
		}
		byBytes[k] = ns[1:]
		out = append(out, textObject{o.ID, o.Text.HTML, derive.Child(ns[0], derive.PML, "txBody")})
	}
	return out, nil
}

// agentEdit changes a body the way an agent might.
func agentEdit(b text.Body) text.Body {
	var out text.Body
	for i, p := range b.Paras {
		q := p
		q.Runs = append([]text.Run(nil), p.Runs...)
		var runs []int
		for k, r := range q.Runs {
			if !r.Br {
				runs = append(runs, k)
			}
		}
		if len(runs) == 0 {
			q.Runs = append(q.Runs, text.Run{Text: "x"})
		} else {
			q.Runs[runs[len(runs)-1]].Text += " x"
			f := &q.Runs[runs[0]].Fmt
			if f.B == "on" {
				f.B = ""
			} else {
				f.B = "on"
			}
			if len(runs) > 1 {
				q.Runs[runs[1]].Fmt.Fill = text.Fill{Kind: "solid", Color: text.Color{Color: derive.Color{Kind: "scheme", Val: "accent3"}}}
			}
		}
		if len(runs) > 0 {
			// Paragraph and run formatting beyond the first grammar:
			// letter spacing on the first run, a strike on the last.
			q.Runs[runs[0]].Fmt.Spc = "1.5pt"
			if l := &q.Runs[runs[len(runs)-1]].Fmt; l.Strike == "sngStrike" {
				l.Strike = ""
			} else {
				l.Strike = "sngStrike"
			}
		}
		if i == 0 {
			q.Algn = "r"
			q.PF.LnSpc, q.PF.SpcBef = "120%", "6pt"
		}
		out.Paras = append(out.Paras, q)
		if i == 0 {
			n := text.Para{Lvl: p.Lvl, Bullet: p.Bullet, PF: text.PFmt{MarL: "18pt", Indent: "-9pt"}, Runs: []text.Run{{Text: "new", Fmt: text.Fmt{
				Size: "20", Lang: "en-GB", Cap: "small", Baseline: "30%",
				Highlight: text.Fill{Kind: "solid", Color: text.Color{Color: derive.Color{Kind: "srgb", Val: "FFFF00"}, Hex: "FFFF00"}},
			}}}}
			if n.Bullet == "picture" {
				n.Bullet = ""
			}
			out.Paras = append(out.Paras, n)
		}
	}
	return out
}

// span is a byte range of a slide part to replace.
type span struct {
	s, e int
	b    []byte
}

// paragraphSpan is the range text.Object.ReplaceParagraphs replaces.
func paragraphSpan(body *tree.Node) (int, int) {
	s, e := -1, -1
	for _, c := range body.Elements() {
		if derive.Is(c, derive.DML, "p") {
			cs, ce, _ := c.Span()
			if s < 0 {
				s = cs
			}
			e = ce
		}
	}
	if s < 0 {
		a := derive.Child(body, derive.DML, "lstStyle")
		if a == nil {
			a = derive.Child(body, derive.DML, "bodyPr")
		}
		_, e, _ = a.Span()
		s = e
	}
	return s, e
}

// rewriteZip copies the deck at src to dst with the given parts replaced.
func rewriteZip(src, dst string, parts map[string][]byte) error {
	zr, err := zip.OpenReader(src)
	if err != nil {
		return err
	}
	defer zr.Close()
	f, err := os.Create(dst)
	if err != nil {
		return err
	}
	zw := zip.NewWriter(f)
	for _, m := range zr.File {
		b, ok := parts["/"+m.Name]
		if !ok {
			if err := zw.Copy(m); err != nil {
				return err
			}
			continue
		}
		w, err := zw.Create(m.Name)
		if err != nil {
			return err
		}
		if _, err := w.Write(b); err != nil {
			return err
		}
	}
	if err := zw.Close(); err != nil {
		return err
	}
	return f.Close()
}

// lawRels reads a slide's hyperlink relationships for the builder and
// ReadBodyWith, as edit.PartRels does; the edits here add no links.
type lawRels struct{ rels *opc.Rels }

func (r lawRels) Color(string, *tree.Node) (string, bool) { return "", true }
func (r lawRels) Link(rid string) (string, bool, bool) {
	rel, ok := r.rels.Get(rid)
	return rel.Target, rel.External(), ok
}
func (r lawRels) Hyperlink(string) (string, error) { return "", errors.New("no new links in this law") }
func (r lawRels) Prune([]string) error             { return nil }

func checkTextDeck(t *testing.T, path string, c textTally) {
	p, err := opc.Open(path)
	if err != nil {
		c["deck unreadable"]++
		return
	}
	defer p.Close()
	d, err := model.OpenDeck(p)
	if err != nil {
		c["deck unreadable"]++
		return
	}
	parts := map[string][]byte{}
	want := map[string]map[string]string{} // slide -> id -> html
	for _, part := range d.Slides {
		s, err := readComplete(d, part)
		if err != nil {
			continue
		}
		pt, err := p.Part(part)
		if err != nil {
			t.Fatal(err)
		}
		src, err := pt.Bytes()
		if err != nil {
			t.Fatal(err)
		}
		sr, err := p.Rels(part)
		if err != nil {
			t.Fatal(err)
		}
		objs, err := textObjects(s, src)
		if err != nil {
			t.Errorf("%s %s: %v", path, part, err)
			continue
		}
		var spans []span
		want[part] = map[string]string{}
		for _, o := range objs {
			c["bodies"]++
			b, err := text.Parse(o.html)
			if err != nil {
				c["model html does not parse: "+reNum.ReplaceAllString(err.Error(), "N")]++
				t.Errorf("%s %s %s: %v\n%s", path, part, o.id, err, o.html)
				continue
			}
			if got := text.Render(b); got != o.html {
				c["model html not a fixed point of Normalise"]++
				t.Errorf("%s %s %s: Normalise\n%s\n%s", path, part, o.id, o.html, got)
			}
			rels := lawRels{rels: sr}
			if !text.Equal(b, text.ReadBodyWith(o.body, rels)) {
				c["model and ReadBody disagree"]++
				t.Errorf("%s %s %s: model %s, ReadBody %s", path, part, o.id, o.html, text.Render(text.ReadBodyWith(o.body, rels)))
				continue
			}
			c["model and ReadBody agree"]++
			e := agentEdit(b)
			frag, _, err := text.BuildWith(o.body, e, rels)
			if err != nil {
				c["edit refused: "+reNum.ReplaceAllString(err.Error(), "N")]++
				want[part][o.id] = o.html
				continue
			}
			s, e2 := paragraphSpan(o.body)
			spans = append(spans, span{s, e2, frag})
			want[part][o.id] = text.Render(e)
		}
		if len(spans) == 0 {
			continue
		}
		sort.Slice(spans, func(i, j int) bool { return spans[i].s > spans[j].s })
		out := append([]byte(nil), src...)
		for k, sp := range spans {
			if k > 0 && sp.e > spans[k-1].s {
				t.Fatalf("%s %s: overlapping text bodies", path, part)
			}
			out = append(append(append([]byte(nil), out[:sp.s]...), sp.b...), out[sp.e:]...)
		}
		parts[part] = out
	}
	if len(parts) == 0 {
		return
	}
	dst := filepath.Join(t.TempDir(), "edited.pptx")
	if err := rewriteZip(path, dst, parts); err != nil {
		t.Fatal(err)
	}
	q, err := opc.Open(dst)
	if err != nil {
		t.Fatal(err)
	}
	defer q.Close()
	d2, err := model.OpenDeck(q)
	if err != nil {
		t.Fatalf("%s: edited deck unreadable: %v", path, err)
	}
	for part := range parts {
		s, err := d2.ReadSlide(part)
		if err != nil {
			c["edited slide unreadable"]++
			t.Errorf("%s %s: edited slide unreadable: %v", path, part, err)
			continue
		}
		for _, o := range s.Objects {
			w, ok := want[part][o.ID]
			if !ok {
				continue
			}
			if o.Text == nil {
				c["edited body lost"]++
				t.Errorf("%s %s %s: text gone", path, part, o.ID)
				continue
			}
			if o.Text.HTML != w {
				c["html(read(set(h))) != Normalise(h)"]++
				t.Errorf("%s %s %s: read back\n%s\nwant\n%s", path, part, o.ID, o.Text.HTML, w)
				continue
			}
			c["html(read(set(h))) == Normalise(h)"]++
		}
	}
}

func TestTextLawsFixtures(t *testing.T) {
	c := textTally{}
	for name, path := range testdeck.All(t) {
		t.Run(name, func(t *testing.T) { checkTextDeck(t, path, c) })
	}
	t.Log(c)
	if c["html(read(set(h))) == Normalise(h)"] == 0 {
		t.Fatalf("text laws exercised nothing:%v", c)
	}
}

func TestTextLawsCorpus(t *testing.T) {
	t.Parallel() // beside the other corpus suites (corpus_test.go)
	files, missing := nativeFiles(t)
	c := textTally{}
	for _, r := range corpustest.Run(t, files, func(t *testing.T, f corpustest.File, d *textTally) {
		*d = textTally{}
		checkTextDeck(t, f.Path, *d)
	}) {
		addInts(c, r.Value)
	}
	t.Logf("%d manifest decks missing on disk%v", missing, c)
}
