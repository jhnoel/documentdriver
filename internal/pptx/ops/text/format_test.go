package text

import (
	"errors"
	"strings"
	"testing"

	"github.com/jhnoel/documentdriver/internal/ooxml/tree"
)

// TestNormaliseFormatting covers the paragraph and run formatting beyond
// alignment, level, bullet choice, bold, italic, underline, size, colour
// and font.
func TestNormaliseFormatting(t *testing.T) {
	cases := []struct{ in, want string }{
		{`<p style="line-height:150%;margin-top:6pt;margin-bottom:12.5pt">x</p>`, `<p style="line-height:150%;margin-top:6pt;margin-bottom:12.5pt">x</p>`},
		{`<p style="text-indent:-18pt;margin-left:18.0pt;tab-size:36pt;margin-right:1pt">x</p>`, `<p style="margin-left:18pt;margin-right:1pt;text-indent:-18pt;tab-size:36pt">x</p>`},
		{`<p style="line-height:090.50%">x</p>`, `<p style="line-height:90.5%">x</p>`},
		{`<p data-tabs="36pt 72pt:ctr" dir="rtl">x</p>`, `<p data-tabs="36pt 72pt:ctr" dir="rtl">x</p>`},
		{`<ol><li data-type="arabicPeriod" data-start="3">a</li></ol>`, `<ol><li data-type="arabicPeriod" data-start="3">a</li></ol>`},
		{`<ul><li data-char="•" data-bu-font="'Wingdings'" data-bu-size="75%" data-bu-color="accent2 #ED7D31">a</li></ul>`,
			`<ul><li data-char="•" data-bu-font="'Wingdings'" data-bu-size="75%" data-bu-color="accent2 #ED7D31">a</li></ul>`},
		{`<p data-bu-font="var(--mn-lt)" data-bu-size="text" data-bu-color="text">a</p>`, `<p data-bu-font="var(--mn-lt)" data-bu-size="text" data-bu-color="text">a</p>`},
		{`<p data-bu-size="12pt" data-bu-color="#FF0000">a</p>`, `<p data-bu-size="12pt" data-bu-color="#FF0000">a</p>`},
		{`<s>x</s><s data-strike="dblStrike">y</s><span data-strike="noStrike">z</span>`, `<p><s>x</s><s data-strike="dblStrike">y</s><span data-strike="noStrike">z</span></p>`},
		{`H<span style="vertical-align:-25%">2</span>O`, `<p>H<span style="vertical-align:-25%">2</span>O</p>`},
		{`<span style="text-transform:uppercase">a</span><span style="font-variant:small-caps">b</span><span style="text-transform:none">c</span>`,
			`<p><span style="text-transform:uppercase">a</span><span style="font-variant:small-caps">b</span><span style="text-transform:none">c</span></p>`},
		{`<span style="letter-spacing:-0.5pt">x</span>`, `<p><span style="letter-spacing:-0.5pt">x</span></p>`},
		{`<span style="background-color:#ffff00">x</span>`, `<p><span style="background-color:#FFFF00" data-highlight="#FFFF00">x</span></p>`},
		{`<span style="background-color:var(--accent1)">x</span>`, `<p><span style="background-color:var(--accent1)">x</span></p>`},
		{`<span data-highlight="prst:yellow #FFFF00">x</span>`, `<p><span style="background-color:#FFFF00" data-highlight="prst:yellow #FFFF00">x</span></p>`},
		// Language: on the paragraph when every run has the same one.
		{`<p lang="en-US">a<b>b</b></p>`, `<p lang="en-US">a<b>b</b></p>`},
		{`<span lang="en-US">a</span><span lang="en-US">b</span>`, `<p lang="en-US">ab</p>`},
		{`<p lang="en-US">a<span lang="de-DE">b</span></p>`, `<p><span lang="en-US">a</span><span lang="de-DE">b</span></p>`},
		{`<p lang="en-US">a<span lang="">b</span></p>`, `<p><span lang="en-US">a</span>b</p>`},
		{`<p lang="en-US">a<br/>b</p>`, `<p lang="en-US">a<br/>b</p>`},
		// Links: the outermost wrapper.
		{`<b><a href="https://x.test/a?b=1&amp;c=2">x</a></b>`, `<p><a href="https://x.test/a?b=1&amp;c=2"><b>x</b></a></p>`},
		{`<a href="mailto:a@b.test" title="Mail">x</a>`, `<p><a href="mailto:a@b.test" title="Mail">x</a></p>`},
		{`<a data-link="ppaction://hlinkshowjump?jump=nextslide">x</a>`, `<p><a data-link="ppaction://hlinkshowjump?jump=nextslide">x</a></p>`},
	}
	for _, c := range cases {
		got, err := Normalise(c.in)
		if err != nil {
			t.Errorf("Normalise(%q): %v", c.in, err)
			continue
		}
		if got != c.want {
			t.Errorf("Normalise(%q)\n got %s\nwant %s", c.in, got, c.want)
		}
		again, err := Normalise(got)
		if err != nil || again != got {
			t.Errorf("Normalise not idempotent on %q: %q, %v", got, again, err)
		}
	}
}

func TestParseFormattingErrors(t *testing.T) {
	cases := []struct{ in, want string }{
		{`<p style="line-height:1.5">x</p>`, "line-height"},
		{`<p style="line-height:150.0001%">x</p>`, "three decimals"},
		{`<p style="margin-top:20000pt">x</p>`, "a:ST_TextSpacingPoint"},
		{`<p style="margin-left:-1pt">x</p>`, "margin-left"},
		{`<p style="text-indent:5000pt">x</p>`, "a:ST_TextIndent"},
		{`<p style="color:red">x</p>`, "paragraphs take"},
		{`<p data-start="2">x</p>`, "data-start"},
		{`<ol><li data-type="arabicPeriod" data-start="0">x</li></ol>`, "a:ST_TextBulletStartAtNum"},
		{`<p data-bu-size="10%">x</p>`, "25%"},
		{`<p data-bu-size="75.5%">x</p>`, "25%"},
		{`<p data-bu-font="Arial">x</p>`, "data-bu-font"},
		{`<p data-bu-color="accent9">x</p>`, "a:ST_SchemeColorVal"},
		{`<p data-tabs="36pt:left">x</p>`, "a:ST_TextTabAlignType"},
		{`<p data-tabs="">x</p>`, "empty data-tabs"},
		{`<p dir="auto">x</p>`, "dir"},
		{`<s data-strike="noStrike">x</s>`, "span data-strike"},
		{`<s data-strike="tripleStrike">x</s>`, "a:ST_TextStrikeType"},
		{`<s><s data-strike="dblStrike">x</s></s>`, "contradicts"},
		{`<span data-strike="sngStrike">x</span>`, "only noStrike"},
		{`<span style="vertical-align:super">x</span>`, "vertical-align"},
		{`<span style="text-transform:lowercase">x</span>`, "text-transform"},
		{`<span style="font-variant:small-caps;text-transform:uppercase">x</span>`, "contradict"},
		{`<span style="letter-spacing:5000pt">x</span>`, "letter-spacing"},
		{`<span style="background-color:transparent">x</span>`, "background-color"},
		{`<span lang="en US">x</span>`, "no spaces"},
		{`<a>x</a>`, "needs href"},
		{`<a href="">x</a>`, "empty href"},
		{`<a href="https://a.test" data-link="x">x</a>`, "both"},
		{`<a href="https://a.test"><a href="https://b.test">x</a></a>`, "<a> inside an open <a>"},
		{`<a href="https://a.test" target="_blank">x</a>`, "attribute target"},
	}
	for _, c := range cases {
		_, err := Parse(c.in)
		if err == nil {
			t.Errorf("Parse(%q): no error, want %q", c.in, c.want)
			continue
		}
		if !errors.Is(err, ErrGrammar) {
			t.Errorf("Parse(%q): %v does not wrap ErrGrammar", c.in, err)
		}
		if !strings.Contains(err.Error(), c.want) {
			t.Errorf("Parse(%q): %v, want it to mention %q", c.in, err, c.want)
		}
	}
}

// fakeRels is an in-memory relationships part for the link tests.
type fakeRels struct {
	rels   map[string][2]string // id -> target, mode
	next   int
	pruned []string
}

func (r *fakeRels) Color(string, *tree.Node) (string, bool) { return "", true }
func (r *fakeRels) Link(rid string) (string, bool, bool) {
	v, ok := r.rels[rid]
	return v[0], v[1] == "External", ok
}
func (r *fakeRels) Hyperlink(target string) (string, error) {
	for id, v := range r.rels {
		if v[0] == target && v[1] == "External" {
			return id, nil
		}
	}
	r.next++
	id := "rId" + string(rune('0'+r.next))
	r.rels[id] = [2]string{target, "External"}
	return id, nil
}
func (r *fakeRels) Prune(rids []string) error {
	r.pruned = append(r.pruned, rids...)
	return nil
}

func TestSetLinks(t *testing.T) {
	const ns = ` xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships"`
	body := `<p:txBody><a:bodyPr/><a:lstStyle/><a:p><a:r><a:rPr lang="en-US" dirty="0"><a:hlinkClick r:id="rId1"/></a:rPr><a:t>old</a:t></a:r><a:r><a:rPr lang="en-US"/><a:t> plain</a:t></a:r></a:p></p:txBody>`
	f := newFake(t, strings.Replace(sp(body), "<p:sp ", "<p:sp"+ns+" ", 1))
	rels := &fakeRels{rels: map[string][2]string{"rId1": {"https://old.test/", "External"}}, next: 1}
	if got := Render(ReadBodyWith(f.body(0), rels)); got != `<p lang="en-US"><a href="https://old.test/">old</a> plain</p>` {
		t.Fatalf("read: %s", got)
	}
	// A new link on the second run: a new relationship, the old one kept.
	h := `<p lang="en-US"><a href="https://old.test/">old</a><a href="https://new.test/" title="New"> plain</a></p>`
	if err := SetWith(f, h, rels); err != nil {
		t.Fatal(err)
	}
	if got := Render(ReadBodyWith(f.body(0), rels)); got != h {
		t.Fatalf("after link:\n got %s\nwant %s", got, h)
	}
	if !strings.Contains(f.xml(0), `<a:rPr lang="en-US"><a:hlinkClick r:id="rId2" tooltip="New"/></a:rPr><a:t> plain</a:t>`) {
		t.Errorf("new link: %s", f.xml(0))
	}
	// Linking the first run to the same target as the second reuses its
	// relationship; rId1 is offered for pruning.
	h = `<p lang="en-US"><a href="https://new.test/" title="New">old plain</a></p>`
	if err := SetWith(f, h, rels); err != nil {
		t.Fatal(err)
	}
	if got := Render(ReadBodyWith(f.body(0), rels)); got != h {
		t.Fatalf("after relink:\n got %s\nwant %s", got, h)
	}
	if strings.Contains(f.xml(0), "rId1") || len(rels.rels) != 2 {
		t.Errorf("relink: %s, rels %v", f.xml(0), rels.rels)
	}
	if !strings.Contains(strings.Join(rels.pruned, " "), "rId1") {
		t.Errorf("rId1 not offered for pruning: %v", rels.pruned)
	}
	// Removing the link.
	if err := SetWith(f, `<p lang="en-US">old plain</p>`, rels); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(f.xml(0), "hlinkClick") {
		t.Errorf("unlink: %s", f.xml(0))
	}
	// A link HTML cannot create is refused.
	if err := SetWith(f, `<p><a data-link="ppaction://hlinkshowjump?jump=nextslide">x</a></p>`, rels); !errors.Is(err, ErrInexpressible) {
		t.Errorf("data-link: %v", err)
	}
	// Without the part's relationships, no link can be created.
	if err := Set(f, `<p><a href="https://x.test/">x</a></p>`); !errors.Is(err, ErrInexpressible) {
		t.Errorf("no rels: %v", err)
	}
}

// TestSetParagraphFormatting writes the paragraph properties into an
// existing pPr: shown properties set as the HTML says, the rest kept, in
// schema order.
func TestSetParagraphFormatting(t *testing.T) {
	body := `<p:txBody><a:bodyPr/><a:lstStyle/><a:p><a:pPr marL="285750" indent="-285750" fontAlgn="base"><a:buFont typeface="Arial" charset="0"/><a:buChar char="•"/><a:defRPr sz="1200"/></a:pPr><a:r><a:rPr lang="en-US"/><a:t>x</a:t></a:r></a:p></p:txBody>`
	f := newFake(t, sp(body))
	h := `<ul><li style="line-height:90%;margin-bottom:6pt;margin-left:22.5pt;text-indent:-22.5pt" data-char="•" data-bu-font="'Arial'" data-bu-size="80%" data-bu-color="text" data-tabs="72pt:r" lang="en-US">x</li></ul>`
	mustSet(t, f, h)
	want := `<a:p><a:pPr marL="285750" indent="-285750" fontAlgn="base"><a:lnSpc><a:spcPct val="90000"/></a:lnSpc><a:spcAft><a:spcPts val="600"/></a:spcAft><a:buClrTx/><a:buSzPct val="80000"/><a:buFont typeface="Arial" charset="0"/><a:buChar char="•"/><a:tabLst><a:tab pos="914400" algn="r"/></a:tabLst><a:defRPr sz="1200"/></a:pPr><a:r><a:rPr lang="en-US"/><a:t>x</a:t></a:r></a:p>`
	if got := paraXML(t, f, 0); got != want {
		t.Errorf("\n got %s\nwant %s", got, want)
	}
	// Writing it again is a no-op.
	n := f.replaces
	mustSet(t, f, h)
	if f.replaces != n {
		t.Error("identity set wrote")
	}
}
