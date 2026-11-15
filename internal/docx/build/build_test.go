package build

import (
	"errors"
	"strings"
	"testing"

	docxedit "github.com/jhnoel/documentdriver/internal/docx/edit"
	"github.com/jhnoel/documentdriver/internal/docx/html"
	"github.com/jhnoel/documentdriver/internal/docx/math"
	core "github.com/jhnoel/documentdriver/internal/ooxml/edit"
	"github.com/jhnoel/documentdriver/internal/ooxml/tree"
)

const doc = `<w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main" ` +
	`xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships" ` +
	`xmlns:m="http://schemas.openxmlformats.org/officeDocument/2006/math"><w:body>` +
	`<w:p><w:r><w:t>old</w:t></w:r><m:oMath><m:r><w:rPr><w:sz w:val="30"/></w:rPr><m:t>y</m:t></m:r></m:oMath></w:p>` +
	`</w:body></w:document>`

// fixture returns a region over a private copy of doc's body, a new empty
// paragraph placed at its end, and the existing paragraph.
func fixture(t *testing.T) (*core.Handle, *tree.Node, *tree.Node) {
	t.Helper()
	d, err := tree.Parse([]byte(doc))
	if err != nil {
		t.Fatal(err)
	}
	body := d.Root().Elements()[0]
	h := core.Private(docxedit.Format, body)
	nodes, err := h.Workspace().Parse(body, []byte(`<w:p/>`), nsW)
	if err != nil {
		t.Fatal(err)
	}
	if err := h.PlaceAt(body, nodes[0], nil); err != nil {
		t.Fatal(err)
	}
	return h, nodes[0], body.Elements()[0]
}

func atoms(t *testing.T, s string) []html.Atom {
	t.Helper()
	items, err := html.ParseFragment("<p>"+s+"</p>", math.LaTeXToOMML)
	if err != nil {
		t.Fatal(err)
	}
	a, err := InlineAtoms(items)
	if err != nil {
		t.Fatal(err)
	}
	return a
}

// TestRunPropertyOrder: run properties go in the schema's declaration
// order whatever order the HTML nests them in.
func TestRunPropertyOrder(t *testing.T) {
	h, p, _ := fixture(t)
	in := `<span style="font-family:Arial;font-size:12pt;color:#00ff00"><sup><u><i><b><mark data-highlight="cyan">x</mark></b></i></u></sup></span>`
	if _, err := Inline(h, p, nil, atoms(t, in), Options{}); err != nil {
		t.Fatal(err)
	}
	want := `<w:p><w:r><w:rPr><w:rFonts w:ascii="Arial" w:hAnsi="Arial"/><w:b/><w:i/><w:strike w:val="0"/><w:color w:val="00FF00"/><w:sz w:val="24"/><w:highlight w:val="cyan"/><w:u w:val="single"/><w:vertAlign w:val="superscript"/></w:rPr><w:t>x</w:t></w:r></w:p>`
	if got := string(p.Bytes()); got != want {
		t.Errorf("got  %s\nwant %s", got, want)
	}
}

// TestRunsAndGroups: characters with one formatting share a run, breaks
// and tabs ride in the run before them, an equation ends a run, and a
// link's content is its own group.
func TestRunsAndGroups(t *testing.T) {
	h, p, _ := fixture(t)
	opt := Options{Link: func(string) (string, error) { return "rId7", nil }}
	c, err := Inline(h, p, nil, atoms(t, `a  b<br/>c<b>d</b>&#9;<equation display="inline">x</equation>e`), opt)
	if err != nil {
		t.Fatal(err)
	}
	got := string(p.Bytes())
	for _, want := range []string{
		`<w:r><w:t xml:space="preserve">a  b</w:t><w:br/><w:t>c</w:t></w:r><w:r><w:rPr><w:b/></w:rPr><w:t>d</w:t><w:tab/></w:r><m:oMath`,
		`</m:oMath><w:r><w:t>e</w:t></w:r>`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("no %s in %s", want, got)
		}
	}
	if len(c.Groups) != 1 || len(c.Groups[0]) != 4 || len(c.Nodes) != 4 {
		t.Errorf("groups %d nodes %d", len(c.Groups), len(c.Nodes))
	}
	h, p, _ = fixture(t)
	c, err = Inline(h, p, nil, atoms(t, `<a href="https://e.com">l<b>k</b></a>`), opt)
	if err != nil {
		t.Fatal(err)
	}
	if got := string(p.Bytes()); !strings.HasPrefix(got, `<w:p><w:hyperlink r:id="rId7"><w:r><w:t>l</w:t></w:r>`) || len(c.Groups) != 1 || len(c.Nodes) != 1 || c.Groups[0][0].Parent() != c.Nodes[0] {
		t.Errorf("link: %s groups %v", got, c.Groups)
	}
}

// TestEquationIdentity: an equation sent back with its data-ref keeps its
// original bytes (its run formatting included); one whose LaTeX changed
// is refused; an unknown ref is a RefError; a new equation takes the
// formatting template.
func TestEquationIdentity(t *testing.T) {
	h, p, old := fixture(t)
	orig := old.Elements()[1]
	id := Equations{"0000000A/equation:1": {Node: orig, LaTeX: "y"}}
	if _, err := Inline(h, p, nil, atoms(t, `<equation display="inline" data-ref="0000000A/equation:1">y</equation>`), Options{Identity: id}); err != nil {
		t.Fatal(err)
	}
	if got := string(p.Elements()[0].Bytes()); got != string(orig.Bytes()) {
		t.Errorf("kept equation %s, want %s", got, orig.Bytes())
	}
	h, p, _ = fixture(t)
	_, err := Inline(h, p, nil, atoms(t, `<equation display="inline" data-ref="0000000A/equation:1">z</equation>`), Options{Identity: id})
	if !errors.Is(err, ErrContent) {
		t.Errorf("changed LaTeX under a ref: %v", err)
	}
	var ref *RefError
	if _, err := Inline(h, p, nil, atoms(t, `<equation display="inline" data-ref="0000000A/equation:2">y</equation>`), Options{Identity: id}); !errors.As(err, &ref) {
		t.Errorf("unknown ref: %v", err)
	}
	h, p, _ = fixture(t)
	if _, err := Inline(h, p, nil, atoms(t, `<equation display="inline">z</equation>`), Options{EquationRunProperties: math.RunProperties(orig)}); err != nil {
		t.Fatal(err)
	}
	if got := string(p.Bytes()); !strings.Contains(got, `<m:r><w:rPr><w:sz w:val="30"/></w:rPr><m:t>z</m:t></m:r>`) {
		t.Errorf("new equation without the template: %s", got)
	}
}

// TestInlineRefusals: the safe inline set.
func TestInlineRefusals(t *testing.T) {
	for _, s := range []string{
		`<ins>a</ins>`, `a</p><p>b`, `<a href="https://e.com">a</a>b`,
		`<a href="https://e.com">a</a><a href="https://f.com">b</a>`,
		`<footnote>n</footnote>`,
	} {
		items, err := html.ParseFragment("<p>"+s+"</p>", math.LaTeXToOMML)
		if err != nil {
			continue // refused by the parser already
		}
		if _, err := InlineAtoms(items); !errors.Is(err, ErrContent) {
			t.Errorf("%s: %v", s, err)
		}
	}
}

// TestProperties: style from the tag or class, CSS in schema order.
func TestProperties(t *testing.T) {
	h, p, _ := fixture(t)
	a := html.ParaAttrs{Tag: "p", Class: html.Some("Quote"), CSS: html.Some("text-align:center;margin-top:6pt;margin-left:12pt")}
	if err := Properties(h, p, a); err != nil {
		t.Fatal(err)
	}
	want := `<w:p><w:pPr><w:pStyle w:val="Quote"/><w:spacing w:before="120"/><w:ind w:left="240"/><w:jc w:val="center"/></w:pPr></w:p>`
	if got := string(p.Bytes()); got != want {
		t.Errorf("got  %s\nwant %s", got, want)
	}
	if StyleOf(html.ParaAttrs{Tag: "h2"}) != "Heading2" {
		t.Error("h2")
	}
}

// TestFieldsNotDirty: new page fields carry no w:dirty (Word would ask
// to update fields on open) and cache a result, as w:fldSimple and as
// complex field runs.
func TestFieldsNotDirty(t *testing.T) {
	for _, complexFields := range []bool{false, true} {
		h, p, _ := fixture(t)
		items, err := html.ParseFragment(`<p>Page <page-number/> of <page-count/></p>`, math.LaTeXToOMML)
		if err != nil {
			t.Fatal(err)
		}
		paras, err := Paragraphs(items)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := Inline(h, p, nil, paras[0].Atoms, Options{ComplexFields: complexFields}); err != nil {
			t.Fatal(err)
		}
		got := string(p.Bytes())
		if strings.Contains(got, "dirty") || strings.Count(got, `<w:t>1</w:t>`) != 2 {
			t.Errorf("complex %v: %s", complexFields, got)
		}
	}
}
