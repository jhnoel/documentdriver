package text

import (
	"errors"
	"strings"
	"testing"
)

// diagramBody is the API box of the diagram fixture.
const diagramBody = `<p:txBody><a:bodyPr wrap="none"/><a:lstStyle/>` +
	`<a:p><a:pPr algn="ctr"/><a:r><a:rPr lang="en-US" b="1"/><a:t>API</a:t></a:r>` +
	`<a:r><a:rPr lang="en-US" sz="1200"><a:solidFill><a:schemeClr val="accent2"><a:lumMod val="75000"/></a:schemeClr></a:solidFill></a:rPr><a:t> Gateway &amp; co</a:t></a:r>` +
	`<a:br><a:rPr lang="en-US"/></a:br>` +
	`<a:r><a:rPr lang="en-US" i="1" u="sng"><a:latin typeface="+mj-lt"/></a:rPr><a:t>v2</a:t></a:r></a:p>` +
	`<a:p><a:pPr marL="285750" indent="-285750"><a:buFont typeface="Arial"/><a:buChar char="&#8226;"/></a:pPr><a:r><a:rPr lang="en-US" dirty="0"/><a:t>bullet</a:t></a:r><a:endParaRPr lang="en-US" dirty="0"/></a:p>` +
	`</p:txBody>`

func mustSet(t *testing.T, f *fake, h string) {
	t.Helper()
	if err := Set(f, h); err != nil {
		t.Fatalf("Set(%q): %v", h, err)
	}
	b, err := Parse(h)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := key(ReadBody(f.body(0))), key(b); got != want {
		t.Fatalf("after Set(%q):\n got %s\nwant %s", h, got, want)
	}
}

func paraXML(t *testing.T, f *fake, i int) string {
	t.Helper()
	var ps []string
	for _, c := range f.body(0).Elements() {
		if c.Local() == "p" {
			ps = append(ps, string(c.Bytes()))
		}
	}
	if i >= len(ps) {
		t.Fatalf("no paragraph %d in %s", i, f.xml(0))
	}
	return ps[i]
}

func TestSetIdentityIsNoOp(t *testing.T) {
	f := newFake(t, sp(diagramBody))
	before := f.xml(0)
	h := f.html(0)
	want := `<p style="text-align:center" lang="en-US"><b>API</b><span style="font-size:12pt" data-color="accent2 lumMod=75000 #?"> Gateway &amp; co</span><br/><i><u><span style="font-family:var(--mj-lt)">v2</span></u></i></p><ul><li style="margin-left:22.5pt;text-indent:-22.5pt" data-char="•" data-bu-font="'Arial'" lang="en-US">bullet</li></ul>`
	if h != want {
		t.Fatalf("html\n got %s\nwant %s", h, want)
	}
	mustSet(t, f, h)
	if f.replaces != 0 || f.xml(0) != before {
		t.Fatalf("identity set wrote: %d replaces", f.replaces)
	}
	// With the displayed colour the agent sees, still a no-op.
	mustSet(t, f, strings.Replace(h, "#?", "#C55A11", 1))
	if f.replaces != 0 {
		t.Fatal("displayed colour counted as a change")
	}
}

func TestSetChangedParagraphCarries(t *testing.T) {
	f := newFake(t, sp(diagramBody))
	p0 := paraXML(t, f, 0)
	// Change the bullet text: paragraph 0 keeps its bytes, the bullet
	// paragraph keeps its pPr and endParaRPr, the run its rPr.
	mustSet(t, f, `<p style="text-align:center" lang="en-US"><b>API</b><span style="font-size:12pt" data-color="accent2 lumMod=75000 #?"> Gateway &amp; co</span><br/><i><u><span style="font-family:var(--mj-lt)">v2</span></u></i></p><ul><li style="margin-left:22.5pt;text-indent:-22.5pt" data-char="•" data-bu-font="'Arial'" lang="en-US">bullets</li></ul>`)
	if got := paraXML(t, f, 0); got != p0 {
		t.Errorf("unchanged paragraph rewritten:\n%s\n%s", p0, got)
	}
	want := `<a:p><a:pPr marL="285750" indent="-285750"><a:buFont typeface="Arial"/><a:buChar char="&#8226;"/></a:pPr><a:r><a:rPr lang="en-US" dirty="0"/><a:t>bullets</a:t></a:r><a:endParaRPr lang="en-US" dirty="0"/></a:p>`
	if got := paraXML(t, f, 1); got != want {
		t.Errorf("bullet paragraph\n got %s\nwant %s", got, want)
	}
}

func TestSetFormattingOverlay(t *testing.T) {
	f := newFake(t, sp(diagramBody))
	// Bold part of the bullet's run and add a second bullet: the new
	// run takes the covering run's rPr with b set, the new paragraph
	// takes the previous paragraph's pPr (Enter) with what the HTML shows
	// set as it says, and its run says only what the HTML says.
	mustSet(t, f, `<p style="text-align:center" lang="en-US"><b>API</b><span style="font-size:12pt" data-color="accent2 lumMod=75000 #?"> Gateway &amp; co</span><br/><i><u><span style="font-family:var(--mj-lt)">v2</span></u></i></p><ul><li style="margin-left:22.5pt;text-indent:-22.5pt" data-char="•" data-bu-font="'Arial'" lang="en-US"><b>bul</b>let</li><li style="margin-left:22.5pt;text-indent:-22.5pt" data-char="•" data-bu-font="'Arial'"><span style="color:var(--accent1)">next</span></li></ul>`)
	want1 := `<a:p><a:pPr marL="285750" indent="-285750"><a:buFont typeface="Arial"/><a:buChar char="&#8226;"/></a:pPr><a:r><a:rPr lang="en-US" b="1" dirty="0"/><a:t>bul</a:t></a:r><a:r><a:rPr lang="en-US" dirty="0"/><a:t>let</a:t></a:r><a:endParaRPr lang="en-US" dirty="0"/></a:p>`
	if got := paraXML(t, f, 1); got != want1 {
		t.Errorf("overlay\n got %s\nwant %s", got, want1)
	}
	want2 := `<a:p><a:pPr marL="285750" indent="-285750"><a:buFont typeface="Arial"/><a:buChar char="&#8226;"/></a:pPr><a:r><a:rPr><a:solidFill><a:schemeClr val="accent1"/></a:solidFill></a:rPr><a:t>next</a:t></a:r></a:p>`
	if got := paraXML(t, f, 2); got != want2 {
		t.Errorf("new paragraph\n got %s\nwant %s", got, want2)
	}
}

func TestSetPropertyChanges(t *testing.T) {
	f := newFake(t, sp(diagramBody))
	// Change the first paragraph: alignment off, level 2, size and colour
	// changed, font removed, underline changed.
	mustSet(t, f, `<p data-level="2" data-bullet="none" lang="en-US"><b>API</b><span style="font-size:14pt;color:#FF0000"> Gateway &amp; co</span><br/><i><u data-u="dbl">v2</u></i></p><ul><li style="margin-left:22.5pt;text-indent:-22.5pt" data-char="•" data-bu-font="'Arial'" lang="en-US">bullet</li></ul>`)
	want := `<a:p><a:pPr lvl="1"><a:buNone/></a:pPr><a:r><a:rPr lang="en-US" b="1"/><a:t>API</a:t></a:r>` +
		`<a:r><a:rPr lang="en-US" sz="1400"><a:solidFill><a:srgbClr val="FF0000"/></a:solidFill></a:rPr><a:t> Gateway &amp; co</a:t></a:r>` +
		`<a:br><a:rPr lang="en-US"/></a:br>` +
		`<a:r><a:rPr lang="en-US" i="1" u="dbl"/><a:t>v2</a:t></a:r></a:p>`
	if got := paraXML(t, f, 0); got != want {
		t.Errorf("\n got %s\nwant %s", got, want)
	}
}

func TestSetNewBodyAndShrink(t *testing.T) {
	f := newFake(t, sp(""))
	mustSet(t, f, "Cache")
	if !strings.Contains(f.xml(0), `<p:txBody><a:bodyPr/><a:p><a:r><a:t>Cache</a:t></a:r></a:p></p:txBody>`) {
		t.Errorf("new body: %s", f.xml(0))
	}
	g := newFake(t, sp(diagramBody))
	mustSet(t, g, "")
	// The one empty paragraph is paired with the first; it shows no
	// alignment, so algn goes and with it the whole pPr, and its runs.
	if got := paraXML(t, g, 0); got != `<a:p/>` {
		t.Errorf("emptied body: %s", got)
	}
}

func TestSetMultiBranch(t *testing.T) {
	choice := sp(diagramBody)
	fallback := strings.Replace(sp(diagramBody), `lang="en-US" b="1"`, `lang="en-US" altLang="en-GB" b="1"`, 1)
	f := newFake(t, choice, fallback)
	h := `<p lang="en-US"><b>API!</b></p><ul><li style="margin-left:22.5pt;text-indent:-22.5pt" data-char="•" data-bu-font="'Arial'" lang="en-US">bullet</li></ul>`
	mustSet(t, f, h)
	for i := range 2 {
		if got := Render(ReadBody(f.body(i))); got != h {
			t.Errorf("branch %d: %s", i, got)
		}
	}
	if !strings.Contains(f.xml(1), `lang="en-US" altLang="en-GB" b="1"/><a:t>API!`) {
		t.Errorf("branch 1 did not carry its own rPr: %s", f.xml(1))
	}
}

func TestSetErrors(t *testing.T) {
	f := newFake(t, sp(diagramBody))
	if err := Set(f, `<p data-bullet="picture">x</p>`); !errors.Is(err, ErrInexpressible) {
		t.Errorf("picture bullet: %v", err)
	}
	if err := Set(f, `<p><span data-fill="gradFill">x</span></p>`); !errors.Is(err, ErrInexpressible) {
		t.Errorf("gradient fill: %v", err)
	}
	if err := Set(f, `<p>x</b>`); !errors.Is(err, ErrGrammar) {
		t.Errorf("bad html: %v", err)
	}
	math := sp(`<p:txBody><a:bodyPr/><a:p><mc:AlternateContent><mc:Choice Requires="a14"><a14:m/></mc:Choice><mc:Fallback><a:r><a:t>x²</a:t></a:r></mc:Fallback></mc:AlternateContent></a:p></p:txBody>`)
	g := newFake(t, math)
	if err := Set(g, "<p>y</p>"); !errors.Is(err, ErrUnsupported) {
		t.Errorf("equation paragraph: %v", err)
	}
	if err := Set(g, "<p></p><p>y</p>"); err != nil {
		t.Errorf("keeping the equation paragraph's html: %v", err)
	}
	if f.replaces != 0 {
		t.Error("a failed set wrote")
	}
}

// keepsGradient: a data-fill run may stay where it is.
func TestSetKeepsUncreatableProperties(t *testing.T) {
	f := newFake(t, sp(`<p:txBody><a:bodyPr/><a:p><a:r><a:rPr><a:gradFill/></a:rPr><a:t>g</a:t></a:r><a:r><a:t>x</a:t></a:r></a:p></p:txBody>`))
	mustSet(t, f, `<p><span data-fill="gradFill">g</span>y</p>`)
	if !strings.Contains(f.xml(0), `<a:r><a:rPr><a:gradFill/></a:rPr><a:t>g</a:t></a:r><a:r><a:t>y</a:t></a:r>`) {
		t.Errorf("%s", f.xml(0))
	}
}

func TestReplace(t *testing.T) {
	f := newFake(t, sp(diagramBody))
	before := f.xml(0)
	n, err := ReplaceText(f, "Gateway", "Proxy", false, 0)
	if err != nil || n != 1 {
		t.Fatalf("replace: %d, %v", n, err)
	}
	if want := strings.Replace(before, "> Gateway &amp; co<", "> Proxy &amp; co<", 1); f.xml(0) != want {
		t.Errorf("replace changed more than the run text:\n%s\n%s", want, f.xml(0))
	}
	// Spanning two runs: merged into the first, keeping its formatting.
	n, err = ReplaceText(f, "PI Pro", "pp", false, 0)
	if err != nil || n != 1 {
		t.Fatalf("spanning replace: %d, %v", n, err)
	}
	if !strings.Contains(f.xml(0), `<a:r><a:rPr lang="en-US" b="1"/><a:t>Appxy &amp; co</a:t></a:r><a:br>`) {
		t.Errorf("spanning: %s", f.xml(0))
	}
	if _, err := ReplaceText(f, "zzz", "y", false, 0); !errors.Is(err, ErrNoMatch) {
		t.Errorf("no match: %v", err)
	}
	if _, err := ReplaceText(f, "l", "L", false, 0); !errors.Is(err, ErrManyMatches) {
		t.Errorf("many: %v", err)
	}
	if _, err := ReplaceText(f, "co\vv2", "x", false, 0); !errors.Is(err, ErrCrossing) {
		t.Errorf("line break: %v", err)
	}
	if _, err := ReplaceText(f, "v2\nbul", "x", false, 0); !errors.Is(err, ErrCrossing) {
		t.Errorf("paragraph break: %v", err)
	}
	if _, err := ReplaceText(f, "", "x", false, 0); err == nil {
		t.Error("empty find accepted")
	}
	writes := f.replaces
	if n, err := ReplaceText(f, "bullet", "bullet", false, 0); err != nil || n != 1 || f.replaces != writes {
		t.Errorf("self replace: %d, %v, wrote %v", n, err, f.replaces != writes)
	}
	n, err = ReplaceText(f, "l", "L", true, 0)
	if err != nil || n != 2 {
		t.Fatalf("all: %d, %v", n, err)
	}
	if got := f.html(0); !strings.Contains(got, "buLLet") {
		t.Errorf("all: %s", got)
	}
}

func TestReplaceField(t *testing.T) {
	f := newFake(t, sp(`<p:txBody><a:bodyPr/><a:p><a:r><a:t>Slide </a:t></a:r><a:fld id="{1}" type="slidenum"><a:t>3</a:t></a:fld><a:r><a:t> of 9</a:t></a:r></a:p></p:txBody>`))
	if _, err := ReplaceText(f, "e 3", "x", false, 0); !errors.Is(err, ErrCrossing) {
		t.Errorf("field: %v", err)
	}
	if _, err := ReplaceText(f, "9", "10", false, 0); err != nil {
		t.Errorf("after field: %v", err)
	}
	if !strings.Contains(f.xml(0), `<a:fld id="{1}" type="slidenum"><a:t>3</a:t></a:fld><a:r><a:t> of 10</a:t></a:r>`) {
		t.Errorf("%s", f.xml(0))
	}
}

func TestReplaceMultiBranchCounts(t *testing.T) {
	f := newFake(t, sp(diagramBody), strings.Replace(sp(diagramBody), "bullet", "bullet bullet", 1))
	if _, err := ReplaceText(f, "bullet", "b", true, 0); err == nil {
		t.Error("branches with different match counts accepted")
	}
}
