package view

import (
	"archive/zip"
	"bytes"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/jhnoel/documentdriver/internal/docx/math"
	"github.com/jhnoel/documentdriver/internal/docx/model"
	"github.com/jhnoel/documentdriver/internal/ooxml/tree"
)

const docHead = `<w:document xmlns:w="` + model.NSW + `" xmlns:w14="` + model.NSW14 +
	`" xmlns:m="http://schemas.openxmlformats.org/officeDocument/2006/math"><w:body>`

func build(t testing.TB, parts map[string]string) *model.Document {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for _, name := range []string{"word/document.xml", "word/styles.xml", "word/numbering.xml", "word/_rels/document.xml.rels"} {
		body, ok := parts[name]
		if !ok {
			continue
		}
		w, err := zw.CreateHeader(&zip.FileHeader{Name: name, Method: zip.Store, Modified: time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC)})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write([]byte(body)); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	d, err := model.OpenDocument(buf.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	return d
}

func render(t testing.TB, body string, v View, opt Options) *Rendered {
	t.Helper()
	r, err := Render(build(t, map[string]string{"word/document.xml": docHead + body + `</w:body></w:document>`}), v, opt)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func latex(*tree.Node) (string, bool, []math.Loss) { return "a<b", false, nil }

func TestEquations(t *testing.T) {
	body := `<w:p w14:paraId="00000001"><m:oMath><m:r><m:t>x</m:t></m:r></m:oMath>` +
		`<w:del w:id="1" w:author="a"><m:oMath/></w:del><m:oMathPara><m:oMath/></m:oMathPara></w:p>`
	r := render(t, body, Markup, Options{Equation: latex})
	for _, w := range []string{
		`<equation display="inline" data-ref="00000001/equation:1">a&lt;b</equation>`,
		`<del data-revision="1" data-author="a"><equation display="inline">a&lt;b</equation></del>`,
		`<equation display="block" data-ref="00000001/equation:2">a&lt;b</equation>`,
	} {
		if !strings.Contains(r.HTML, w) {
			t.Errorf("missing %s in %s", w, r.HTML)
		}
	}
	if f := render(t, body, Final, Options{Equation: latex}); strings.Count(f.HTML, "<equation") != 2 {
		t.Errorf("final view shows the deleted equation: %s", f.HTML)
	}
	_, err := Render(build(t, map[string]string{"word/document.xml": docHead + body + `</w:body></w:document>`}), Markup, Options{})
	if !errors.Is(err, ErrNoEquation) {
		t.Errorf("err = %v, want ErrNoEquation", err)
	}
}

// TestEquationLosses: content the LaTeX leaves out is listed in
// data-loss (math.OMMLToLaTeX, the CLI's converter); formatting the
// LaTeX does not carry is not.
func TestEquationLosses(t *testing.T) {
	body := `<w:p w14:paraId="00000001"><m:oMath><m:r><w:rPr><w:b/></w:rPr><m:t>x</m:t></m:r></m:oMath>` +
		`<m:oMath><m:r><m:t>a</m:t></m:r><w:bookmarkStart w:id="1" w:name="b"/><m:r><m:delText>c</m:delText></m:r>` +
		`<w:bookmarkEnd w:id="1"/><w:bookmarkStart w:id="2" w:name="d"/></m:oMath></w:p>`
	r := render(t, body, Markup, Options{Equation: math.OMMLToLaTeX})
	for _, w := range []string{
		`<equation display="inline" data-ref="00000001/equation:1">x</equation>`,
		`<equation display="inline" data-ref="00000001/equation:2" data-loss="m:delText; w:bookmarkEnd; w:bookmarkStart">ac</equation>`,
	} {
		if !strings.Contains(r.HTML, w) {
			t.Errorf("missing %s in %s", w, r.HTML)
		}
	}
}

func TestContentLosses(t *testing.T) {
	got := ContentLosses([]math.Loss{
		{Kind: math.LossFormatting, What: "w:rPr", Count: 3},
		{Kind: math.LossContent, What: "w:ins", Count: 1},
		{Kind: math.LossContent, What: "m:delText", Count: 2},
		{Kind: math.LossContent, What: "m:delText", Count: 1},
	})
	if strings.Join(got, "|") != "m:delText|w:ins" {
		t.Errorf("ContentLosses = %q", got)
	}
	if ContentLosses(nil) != nil {
		t.Error("no losses gives a list")
	}
}

// TestDisplayEquationLines: an m:oMathPara holding several m:oMath is one
// block equation (the converter gets the whole m:oMathPara).
func TestDisplayEquationLines(t *testing.T) {
	var got []string
	conv := func(n *tree.Node) (string, bool, []math.Loss) {
		got = append(got, n.Local())
		return "a\\\\b", true, nil
	}
	r := render(t, `<w:p w14:paraId="00000001"><m:oMathPara><m:oMath/><m:oMath/></m:oMathPara></w:p>`, Markup, Options{Equation: conv})
	if !strings.Contains(r.HTML, `<equation display="block" data-ref="00000001/equation:1">a\\b</equation></p>`) || len(got) != 1 || got[0] != "oMathPara" {
		t.Errorf("got %v %s", got, r.HTML)
	}
}

func TestFormattingTransitions(t *testing.T) {
	body := `<w:p w14:paraId="00000001"><w:r><w:rPr><w:b/></w:rPr><w:t>b</w:t></w:r><w:r><w:rPr><w:b/><w:i/></w:rPr><w:t>bi</w:t></w:r>` +
		`<w:ins w:id="2" w:author="x"><w:r><w:rPr><w:color w:val="ff0000"/></w:rPr><w:t>c</w:t></w:r></w:ins>` +
		`<w:r><w:rPr><w:rPrChange w:id="3" w:author="y"><w:rPr/></w:rPrChange></w:rPr><w:t>p</w:t></w:r><w:r><w:br w:type="page"/><w:tab/></w:r></w:p>`
	r := render(t, body, Markup, Options{})
	// A change in the reset bits (i: off to on) reopens the formatting; a
	// break keeps the pending wrapper open.
	want := `<p id="00000001" data-ord="1"><b>b</b><b><i>bi</i></b><ins data-revision="2" data-author="x"><span style="color:#FF0000;">c</span></ins>` +
		`<span data-format-change="true" data-revision="3" data-author="y">p<br data-break="page"></span><span data-tab="true">` + "\t" + `</span></p>`
	if !strings.Contains(r.HTML, want) {
		t.Errorf("got %s\nwant %s", r.HTML, want)
	}
}

// TestDeepLists: a run of ever-deeper list levels renders without
// unbounded recursion.
func TestDeepLists(t *testing.T) {
	var lvls, body strings.Builder
	const n = 2000
	for i := range n {
		lvls.WriteString(`<w:lvl w:ilvl="` + itoa(i) + `"><w:numFmt w:val="bullet"/></w:lvl>`)
		body.WriteString(`<w:p><w:pPr><w:numPr><w:ilvl w:val="` + itoa(i) + `"/><w:numId w:val="1"/></w:numPr></w:pPr></w:p>`)
	}
	d := build(t, map[string]string{
		"word/document.xml":            docHead + body.String() + `</w:body></w:document>`,
		"word/numbering.xml":           `<w:numbering xmlns:w="` + model.NSW + `"><w:abstractNum w:abstractNumId="0">` + lvls.String() + `</w:abstractNum><w:num w:numId="1"><w:abstractNumId w:val="0"/></w:num></w:numbering>`,
		"word/_rels/document.xml.rels": `<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"/>`,
	})
	r, err := Render(d, Markup, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Count(r.HTML, "<p"); got != n {
		t.Errorf("%d paragraphs rendered, want %d", got, n)
	}
	if got := strings.Count(r.HTML, "<ul"); got != maxListDepth+1 {
		t.Errorf("%d lists, want %d", got, maxListDepth+1)
	}
}

// TestDeepNesting renders content controls nested to the tokenizer's
// depth limit.
func TestDeepNesting(t *testing.T) {
	depth := tree.MaxDepth - 8
	body := strings.Repeat(`<w:sdt><w:sdtContent>`, depth/2-4) + `<w:p><w:r><w:t>deep</w:t></w:r></w:p>` + strings.Repeat(`</w:sdtContent></w:sdt>`, depth/2-4)
	for _, v := range []View{Markup, Final, Original} {
		if r := render(t, body, v, Options{}); !strings.Contains(r.HTML, ">deep</p>") {
			t.Errorf("view %d: %s", v, r.HTML)
		}
	}
}

func TestStyleCascade(t *testing.T) {
	styles := `<w:styles xmlns:w="` + model.NSW + `"><w:docDefaults><w:rPrDefault><w:rPr><w:sz w:val="22"/></w:rPr></w:rPrDefault></w:docDefaults>` +
		`<w:style w:type="paragraph" w:styleId="N" w:default="1"><w:pPr><w:jc w:val="both"/></w:pPr></w:style>` +
		`<w:style w:type="paragraph" w:styleId="Heading2"><w:basedOn w:val="N"/><w:rPr><w:b/></w:rPr></w:style></w:styles>`
	d := build(t, map[string]string{
		"word/document.xml": docHead + `<w:p w14:paraId="00000001"><w:pPr><w:pStyle w:val="Heading2"/></w:pPr><w:r><w:t>h</w:t></w:r></w:p>` +
			`<w:p w14:paraId="00000002"><w:r><w:rPr><w:sz w:val="0"/></w:rPr><w:t>z</w:t></w:r></w:p></w:body></w:document>`,
		"word/styles.xml": styles,
	})
	r, err := Render(d, Markup, Options{})
	if err != nil {
		t.Fatal(err)
	}
	for _, w := range []string{
		`<h2 id="00000001" data-ord="1" data-style="Heading2" style="font-size:11pt;text-align:justify;"><b>h</b></h2>`,
		// sz 0 is no CSS size, so the run loses all its formatting.
		`<p id="00000002" data-ord="2" data-style="N" style="text-align:justify;">z</p>`,
	} {
		if !strings.Contains(r.HTML, w) {
			t.Errorf("missing %s in %s", w, r.HTML)
		}
	}
	got := AgentHTML(r.HTML, LoadStyles(d.Pkg).Catalog(), "N", true)
	if !strings.Contains(got, `<h2 id="00000001" data-style="Heading2" style="font-weight:unset;"><b>h</b></h2>`) || !strings.Contains(got, `<p id="00000002" style="font-size:unset;">z</p>`) {
		t.Errorf("agent markup %s", got)
	}
}

// Ported from compact.rs tests.
func TestModelHTML(t *testing.T) {
	in := "<p id=\"ABC\" data-ord=\"1\">😀 data-ord=\"9\" <span data-field=\"REF &quot;data-ord=&quot; &gt; value\" contenteditable=\"false\">Field</span></p>"
	want := "<p id=\"ABC\">😀 data-ord=\"9\" <span data-field=\"REF &quot;data-ord=&quot; &gt; value\">Field</span></p>"
	if got := modelHTML(in); got != want {
		t.Errorf("got %s", got)
	}
	in = "<p id=\"A\"> A\t B\n<br data-break=\"page\"></p>"
	if got := modelHTML(in); got != in {
		t.Errorf("got %s", got)
	}
}

func TestAssetAliases(t *testing.T) {
	assets := []any{map[string]any{"url": "assets/hash.png", "parts": []any{"word/media/a.png"}}}
	got := AssetAliases(`<p>assets/hash.png<img src="assets/hash.png" alt="assets/hash.png"></p>`, assets)
	if got != `<p>assets/hash.png<img src="assets/i1.png" alt="assets/hash.png"></p>` {
		t.Errorf("got %s", got)
	}
	if a := assets[0].(map[string]any); a["asset_url"] != "assets/hash.png" || a["url"] != "assets/i1.png" {
		t.Errorf("asset %v", a)
	}
}

func TestAgentHTML(t *testing.T) {
	styles := map[string]string{"Normal": "color:#000000;font-size:12pt;line-height:1;", "Quote": `font-family:"A;B";font-style:italic;`}
	got := AgentHTML(`<p id="A" data-style="Normal" style="color:#000000;font-size:12pt;line-height:1;">same</p>`+
		`<p id="B" data-style="Normal" style="color:#000000;font-size:12pt;line-height:2;">double</p>`+
		`<p id="C" style="font-size:12pt;line-height:1;">no colour</p>`+
		`<p id="D" data-style="Quote" style="font-family:&quot;A;B&quot;;font-style:italic;"><span style="color:#000000;">run</span></p>`, styles, "Normal", true)
	want := `<p id="A">same</p><p id="B" style="line-height:2;">double</p><p id="C" style="color:unset;">no colour</p>` +
		`<p id="D" data-style="Quote"><span style="color:#000000;">run</span></p>`
	if got != want {
		t.Errorf("got  %s\nwant %s", got, want)
	}
	got = AgentHTML(`<ol data-list="1" data-level="0" start="1" type="1"><li data-marker="1."><p id="A" data-number="1.">x`+
		`<sup data-note="footnote" data-note-id="7"><a href="#docx-0a-footnote-7">1</a></sup></p>`+
		`<p id="B" data-number="9.">kept</p><a href="#docx-literal">link</a></li></ol>`+
		`<aside data-notes="footnote"><div id="docx-0a-footnote-7" data-note-body="footnote" data-note-id="7"><p>n</p></div></aside>`, nil, "", false)
	want = `<ol data-list="1" data-level="0" start="1" type="1"><li data-marker="1."><p id="A">x` +
		`<sup data-note="footnote" data-note-id="7"><a>1</a></sup></p>` +
		`<p id="B" data-number="9.">kept</p><a href="#docx-literal">link</a></li></ol>` +
		`<aside data-notes="footnote"><div data-note-body="footnote" data-note-id="7"><p>n</p></div></aside>`
	if got != want {
		t.Errorf("got  %s\nwant %s", got, want)
	}
}

func TestAffectedMarkup(t *testing.T) {
	d := build(t, map[string]string{"word/document.xml": docHead +
		`<w:p w14:paraId="00000001"><w:r><w:t>a</w:t></w:r></w:p><w:tbl><w:tr><w:tc><w:p w14:paraId="00000002"><w:r><w:t>b</w:t></w:r></w:p></w:tc></w:tr></w:tbl></w:body></w:document>`})
	got, err := AffectedMarkup(d, []string{"00000002", "00000001", "7FFFFFFF"}, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if got["00000001"] != `<p id="00000001">a</p>` || !strings.HasPrefix(got["00000002"], `<table data-table="1"`) || len(got) != 2 {
		t.Errorf("got %v", got)
	}
}

func TestSafeURL(t *testing.T) {
	for v, want := range map[string]bool{"https://x": true, " HTTP:x": true, "#a": true, "rel/x": true, "javascript:x": false, "http://\x01": false} {
		if safeURL(v) != want {
			t.Errorf("safeURL(%q) != %v", v, want)
		}
	}
}
