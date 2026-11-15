package span

import (
	"errors"
	"fmt"
	"math/rand/v2"
	"slices"
	"strings"
	"testing"

	"github.com/jhnoel/documentdriver/internal/docx/model"
	core "github.com/jhnoel/documentdriver/internal/ooxml/edit"
	"github.com/jhnoel/documentdriver/internal/ooxml/tree"
)

// TestSelectViews: offsets in each view map to all-view spans that hold
// the hidden content between the first and last selected characters.
func TestSelectViews(t *testing.T) {
	_, p := one(t, `<w:p><w:r><w:t>ab</w:t></w:r><w:del w:id="1"><w:r><w:delText>XY</w:delText></w:r></w:del>`+
		`<w:ins w:id="2"><w:r><w:t>cd</w:t></w:r></w:ins><w:r><w:tab/><w:t>e</w:t></w:r></w:p>`)
	r := Read(p)
	for v, want := range map[model.View]string{model.Current: "abcd\te", model.Baseline: "abXY\te", model.All: "abXYcd\te"} {
		if got := r.Text(v); got != want || got != model.ParagraphText(p, v) {
			t.Errorf("view %d text %q, want %q (model %q)", v, got, want, model.ParagraphText(p, v))
		}
	}
	for _, c := range []struct {
		v                  model.View
		start, end, ws, we int
	}{
		{model.Current, 1, 3, 1, 5},  // "bc": the deleted XY lies between
		{model.Current, 2, 2, 4, 4},  // point before c
		{model.Current, 6, 6, 8, 8},  // point at the end
		{model.Baseline, 1, 5, 1, 7}, // "bXY\t": the inserted cd between
		{model.All, 4, 7, 4, 7},
	} {
		s, err := r.Select(c.v, c.start, c.end)
		if err != nil || s.Start != c.ws || s.End != c.we {
			t.Errorf("Select(%d, %d, %d) = %+v, %v; want [%d, %d)", c.v, c.start, c.end, s, err, c.ws, c.we)
		}
	}
	if _, err := r.Select(model.Current, 0, 7); !errors.Is(err, ErrRange) {
		t.Errorf("out of range: %v", err)
	}
	ms := r.Find(model.Current, "d\te")
	if len(ms) != 1 || ms[0].Start != 3 || ms[0].Span.Start != 5 || ms[0].Span.End != 8 {
		t.Errorf("Find = %+v", ms)
	}
}

// TestFindNonOverlapping counts matches as docxdriver's select does.
func TestFindNonOverlapping(t *testing.T) {
	_, p := one(t, `<w:p><w:r><w:t>aaaa é aa</w:t></w:r></w:p>`)
	ms := Read(p).Find(model.Current, "aa")
	var got [][2]int
	for _, m := range ms {
		got = append(got, [2]int{m.Start, m.End})
	}
	if want := [][2]int{{0, 2}, {2, 4}, {7, 9}}; !slices.Equal(got, want) {
		t.Errorf("Find = %v, want %v", got, want)
	}
}

// TestIsolate covers the run shapes: an end inside text, between leaves
// of one run, around objects, inside wrappers, and markers at the edges.
func TestIsolate(t *testing.T) {
	for _, c := range []struct {
		name, p     string
		start, end  int // all-view
		units, text string
	}{
		{"inside text", `<w:p><w:r><w:rPr><w:b/></w:rPr><w:t>hello world</w:t></w:r></w:p>`, 2, 7,
			`<w:r><w:rPr><w:b/></w:rPr><w:t>llo w</w:t></w:r>`, "llo w"},
		{"space at a split", `<w:p><w:r><w:t>hello world</w:t></w:r></w:p>`, 5, 11,
			`<w:r><w:t xml:space="preserve"> world</w:t></w:r>`, " world"},
		{"same leaf", `<w:p><w:r><w:t>abcdef</w:t></w:r></w:p>`, 1, 5, `<w:r><w:t>bcde</w:t></w:r>`, "bcde"},
		{"across a tab", `<w:p><w:r><w:t>one</w:t><w:tab/><w:t>two</w:t></w:r></w:p>`, 2, 5,
			`<w:r><w:t>e</w:t><w:tab/><w:t>t</w:t></w:r>`, "e\tt"},
		{"leaf boundaries", `<w:p><w:r><w:t>a</w:t><w:br/><w:t>b</w:t><w:sym w:char="F0B7"/></w:r></w:p>`, 1, 3,
			`<w:r><w:br/><w:t>b</w:t></w:r>`, "\nb"},
		{"equation", `<w:p><w:r><w:t>x=</w:t></w:r><m:oMath><m:r><m:t>y</m:t></m:r></m:oMath><w:r><w:t>!</w:t></w:r></w:p>`, 1, 3,
			`<w:r><w:t>=</w:t></w:r>|<m:oMath><m:r><m:t>y</m:t></m:r></m:oMath>`, "=￼"},
		{"markers inside and at the edges", `<w:p><w:r><w:t>ab</w:t></w:r><w:bookmarkStart w:id="1" w:name="x"/><w:r><w:t>cd</w:t></w:r>` +
			`<w:proofErr w:type="spellStart"/><w:r><w:t>ef</w:t></w:r><w:bookmarkEnd w:id="1"/><w:r><w:t>gh</w:t></w:r></w:p>`, 2, 6,
			`<w:r><w:t>cd</w:t></w:r>|<w:proofErr w:type="spellStart"/>|<w:r><w:t>ef</w:t></w:r>`, "cdef"},
		{"field characters go outside", `<w:p><w:r><w:t>ab</w:t><w:fldChar w:fldCharType="begin"/><w:t>cd</w:t></w:r></w:p>`, 2, 4,
			`<w:r><w:t>cd</w:t></w:r>`, "cd"},
		{"part of a revision", `<w:p><w:ins w:id="1"><w:r><w:t>abc</w:t></w:r><w:r><w:t>de</w:t></w:r></w:ins><w:r><w:t>f</w:t></w:r></w:p>`, 2, 6,
			`<w:r><w:t>c</w:t></w:r>|<w:r><w:t>de</w:t></w:r>|<w:r><w:t>f</w:t></w:r>`, "cdef"},
		{"whole revision", `<w:p><w:r><w:t>a</w:t></w:r><w:del w:id="1"><w:r><w:delText>bc</w:delText></w:r></w:del><w:r><w:t>d</w:t></w:r></w:p>`, 1, 3,
			`<w:del w:id="1"><w:r><w:delText>bc</w:delText></w:r></w:del>`, "bc"},
		{"deleted text split", `<w:p><w:del w:id="1"><w:r><w:delText>bc</w:delText></w:r></w:del></w:p>`, 1, 2,
			`<w:r><w:delText>c</w:delText></w:r>`, "c"},
		{"hyperlink", `<w:p><w:hyperlink r:id="r1"><w:r><w:t>link</w:t></w:r></w:hyperlink><w:r><w:t>s</w:t></w:r></w:p>`, 0, 5,
			`<w:hyperlink r:id="r1"><w:r><w:t>link</w:t></w:r></w:hyperlink>|<w:r><w:t>s</w:t></w:r>`, "links"},
		{"astral characters", `<w:p><w:r><w:t>a😀b</w:t></w:r></w:p>`, 1, 2, `<w:r><w:t>😀</w:t></w:r>`, "😀"},
	} {
		t.Run(c.name, func(t *testing.T) {
			w, p := one(t, c.p)
			before := projection(p)
			h := w.Target(w.DocumentPart(), "paragraph", p)
			iso, err := Isolate(h, Span{p, c.start, c.end})
			if err != nil {
				t.Fatal(err)
			}
			var units []string
			for _, u := range iso.Units {
				units = append(units, string(u.Bytes()))
			}
			if got := strings.Join(units, "|"); got != c.units {
				t.Errorf("units\n got %s\nwant %s", got, c.units)
			}
			if got := unitText(t, Read(p), iso.Units); got != c.text {
				t.Errorf("unit text %q, want %q", got, c.text)
			}
			if after := projection(p); !slices.Equal(before, after) {
				t.Errorf("projection changed:\n%q\n%q", before, after)
			}
			if err := w.Verify(); err != nil {
				t.Errorf("verify: %v", err)
			}
		})
	}
}

// TestIsolateRefuses: ends with no faithful split point.
func TestIsolateRefuses(t *testing.T) {
	for _, c := range []struct {
		name, p    string
		start, end int
		want       error
	}{
		{"bare text", `<w:p><w:t>bare</w:t></w:p>`, 1, 2, ErrBareText},
		{"alternate content", `<w:p><mc:AlternateContent><mc:Choice Requires="x"><w:r><w:t>abc</w:t></w:r></mc:Choice>` +
			`<mc:Fallback><w:r><w:t>abc</w:t></w:r></mc:Fallback></mc:AlternateContent></w:p>`, 1, 3, ErrAlternateContent},
		{"range", `<w:p><w:r><w:t>ab</w:t></w:r></w:p>`, 1, 3, ErrRange},
	} {
		t.Run(c.name, func(t *testing.T) {
			w, p := one(t, c.p)
			orig := string(p.Bytes())
			_, err := Isolate(w.Target(w.DocumentPart(), "paragraph", p), Span{p, c.start, c.end})
			if !errors.Is(err, c.want) {
				t.Fatalf("err %v, want %v", err, c.want)
			}
			if string(p.Bytes()) != orig {
				t.Errorf("refusal changed the paragraph: %s", p.Bytes())
			}
		})
	}
	// Bare text is selectable whole.
	w, p := one(t, `<w:p><w:r><w:t>a</w:t></w:r><w:t>bare</w:t></w:p>`)
	iso, err := Isolate(w.Target(w.DocumentPart(), "paragraph", p), Span{p, 1, 5})
	if err != nil || len(iso.Units) != 1 || !model.IsW(iso.Units[0], "t") {
		t.Errorf("whole bare text: %+v, %v", iso, err)
	}
}

// TestBlocker labels as docxdriver does; unknown content is not a blocker
// (TestUnknownPreserved).
func TestBlocker(t *testing.T) {
	for _, c := range []struct {
		p, want string
	}{
		{`<w:p><w:r><w:t>a</w:t></w:r><w:commentRangeStart w:id="0"/><w:r><w:t>b</w:t></w:r></w:p>`, "comment"},
		{`<w:p><w:r><w:t>a</w:t><w:drawing/><w:t>b</w:t></w:r></w:p>`, "drawing"},
		{`<w:p><w:r><w:t>a</w:t><w:footnoteReference w:id="1"/><w:t>b</w:t></w:r></w:p>`, "note"},
		{`<w:p><w:r><w:t>a</w:t></w:r><w:fldSimple w:instr="PAGE"><w:r><w:t>1</w:t></w:r></w:fldSimple><w:r><w:t>b</w:t></w:r></w:p>`, "field"},
		{`<w:p><w:r><w:t>a</w:t></w:r><w:sdt><w:sdtContent><w:r><w:t>x</w:t></w:r></w:sdtContent></w:sdt><w:r><w:t>b</w:t></w:r></w:p>`, "structured document tag"},
		{`<w:p><w:r><w:t>a</w:t></w:r><w14:foo xmlns:w14="` + model.NSW14 + `"/><w:r><w:t>b</w:t></w:r></w:p>`, ""},
		{`<w:p><w:r><w:t>a</w:t><w:tab/><w:sym w:char="F0B7"/><w:t>b</w:t></w:r><m:oMath/><w:r><w:t>c</w:t></w:r></w:p>`, ""},
	} {
		w, p := one(t, c.p)
		r := Read(p)
		iso, err := Isolate(w.Target(w.DocumentPart(), "paragraph", p), Span{p, 0, r.Len()})
		if err != nil {
			t.Fatal(err)
		}
		got, _, _ := iso.Blocker()
		if got != c.want {
			t.Errorf("%s: blocker %q, want %q", c.p, got, c.want)
		}
	}
}

// genParagraph builds a random paragraph from every kind of content the
// taxonomy names.
func genParagraph(r *rand.Rand) string {
	texts := []string{"a", "bc", " d ", "éf", "g😀h", "  ", "ij k"}
	rprs := []string{"", `<w:rPr><w:b/></w:rPr>`, `<w:rPr><w:i/><w:sz w:val="20"/></w:rPr>`}
	leaf := func(del bool) string {
		t := texts[r.IntN(len(texts))]
		switch r.IntN(12) {
		case 0:
			return `<w:tab/>`
		case 1:
			return `<w:br/>`
		case 2:
			return `<w:sym w:font="Symbol" w:char="F0B7"/>`
		case 3:
			return `<w:fldChar w:fldCharType="begin"/>`
		case 4:
			return `<w:lastRenderedPageBreak/>`
		case 5:
			return `<w:noBreakHyphen/>`
		case 6:
			return `<w:drawing><w:txbxContent><w:p><w:r><w:t>box</w:t></w:r></w:p></w:txbxContent></w:drawing>`
		}
		sp := ""
		if strings.TrimSpace(t) != t {
			sp = ` xml:space="preserve"`
		}
		if del {
			return `<w:delText` + sp + `>` + t + `</w:delText>`
		}
		return `<w:t` + sp + `>` + t + `</w:t>`
	}
	run := func(del bool) string {
		var b strings.Builder
		b.WriteString(`<w:r>` + rprs[r.IntN(len(rprs))])
		for range 1 + r.IntN(4) {
			b.WriteString(leaf(del))
		}
		return b.String() + `</w:r>`
	}
	var b strings.Builder
	b.WriteString(`<w:p><w:pPr><w:jc w:val="left"/></w:pPr>`)
	id := 0
	for range 1 + r.IntN(7) {
		id++
		switch r.IntN(10) {
		case 0:
			fmt.Fprintf(&b, `<w:ins w:id="%d" w:author="a">%s%s</w:ins>`, id, run(false), run(false))
		case 1:
			fmt.Fprintf(&b, `<w:del w:id="%d" w:author="a">%s</w:del>`, id, run(true))
		case 2:
			fmt.Fprintf(&b, `<w:hyperlink r:id="r%d">%s</w:hyperlink>`, id, run(false))
		case 3:
			fmt.Fprintf(&b, `<w:bookmarkStart w:id="%d" w:name="b%d"/>`, id, id)
		case 4:
			b.WriteString(`<w:proofErr w:type="spellStart"/>`)
		case 5:
			b.WriteString(`<m:oMath><m:r><m:t>x</m:t></m:r></m:oMath>`)
		case 6:
			fmt.Fprintf(&b, `<w:smartTag w:element="x">%s</w:smartTag>`, run(false))
		case 7:
			switch r.IntN(3) {
			case 0:
				b.WriteString(`<x:mark xmlns:x="urn:example" x:v="1"/>`)
			case 1:
				fmt.Fprintf(&b, `<x:wrap xmlns:x="urn:example">%s</x:wrap>`, run(false))
			default:
				b.WriteString(`<w:r><w:t>u</w:t><w:rPr><w:i/></w:rPr><w:t>v</w:t><w:futureLeaf/></w:r>`)
			}
		default:
			b.WriteString(run(false))
		}
	}
	return b.String() + `</w:p>`
}

// TestIsolateProperties: on random paragraphs and random selections in
// every view, Select then Isolate keeps the text of every view and the
// formatting projection exactly, the units hold exactly the span, and the
// change passes the write-set verifier.
func TestIsolateProperties(t *testing.T) {
	r := rand.New(rand.NewPCG(1, 2))
	refused := 0
	defer func() { t.Logf("%d of 2000 refused around unknown or misordered markup", refused) }()
	for i := range 2000 {
		src := genParagraph(r)
		w, p := one(t, src)
		read := Read(p)
		v := []model.View{model.Current, model.Baseline, model.All}[r.IntN(3)]
		n := runeLen(read.Text(v))
		a, b := r.IntN(n+1), r.IntN(n+1)
		if a > b {
			a, b = b, a
		}
		s, err := read.Select(v, a, b)
		if err != nil {
			t.Fatal(err)
		}
		if err := checkIsolate(t, fmt.Sprintf("case %d %s view %d [%d,%d)", i, src, v, a, b), w.Target(w.DocumentPart(), "paragraph", p), s); err != nil {
			if !errors.Is(err, ErrUnknown) && !errors.Is(err, ErrRunOrder) {
				t.Fatalf("case %d: refused: %v", i, err)
			}
			refused++
		}
		if err := w.Verify(); err != nil {
			t.Fatalf("case %d: verify: %v", i, err)
		}
	}
}

// checkIsolate isolates s through h and checks the laws. A refusal (an
// end with no faithful split point) must change nothing; it is returned.
func checkIsolate(t *testing.T, name string, h *core.Handle, s Span) error {
	t.Helper()
	p := s.Para
	views := []model.View{model.Current, model.Baseline, model.All}
	var texts []string
	for _, v := range views {
		texts = append(texts, model.ParagraphText(p, v))
	}
	before := projection(p)
	all := texts[2]
	orig := string(p.Bytes())
	unknown := map[*tree.Node]string{}
	p.Walk(func(n *tree.Node) bool {
		if model.IsElement(n) && model.Classify(n) == model.KindUnknown {
			unknown[n] = string(n.Bytes())
		}
		return true
	})
	iso, err := Isolate(h, s)
	if err != nil {
		if errors.Is(err, ErrAlternateContent) || errors.Is(err, ErrBareText) || errors.Is(err, ErrNestedLeaf) ||
			errors.Is(err, ErrUnknown) || errors.Is(err, ErrRunOrder) {
			if string(p.Bytes()) != orig {
				t.Fatalf("%s: refusal %v changed the paragraph", name, err)
			}
			return err
		}
		t.Fatalf("%s: %v", name, err)
	}
	for i, v := range views {
		if got := model.ParagraphText(p, v); got != texts[i] {
			t.Fatalf("%s: view %d text %q, was %q", name, v, got, texts[i])
		}
	}
	if after := projection(p); !slices.Equal(before, after) {
		t.Fatalf("%s: projection changed\n%q\n%q", name, before, after)
	}
	for n, b := range unknown {
		if string(n.Bytes()) != b || !model.Within(n, p) {
			t.Fatalf("%s: unknown element changed: %s -> %s", name, b, n.Bytes())
		}
	}
	read := Read(p)
	if got, want := unitText(t, read, iso.Units), model.CharSlice(all, s.Start, s.End); got != want {
		t.Fatalf("%s: units hold %q, want %q", name, got, want)
	}
	for i, u := range iso.Units {
		for _, o := range iso.Units[i+1:] {
			if model.Within(o, u) || model.Within(u, o) {
				t.Fatalf("%s: nested units", name)
			}
		}
	}
	// Every inside segment is under a unit; no outside one is.
	for _, g := range read.Segs {
		if g.Start == g.End {
			continue // an empty w:t is zero-width content
		}
		in := g.Start >= s.Start && g.End <= s.End && !s.Empty()
		under := slices.ContainsFunc(iso.Units, func(u *tree.Node) bool { return model.Within(g.Leaf, u) })
		if in != under {
			t.Fatalf("%s: segment %q [%d,%d) inside %v, under a unit %v", name, g.Text, g.Start, g.End, in, under)
		}
		if g.Start < s.Start && g.End > s.Start || g.Start < s.End && g.End > s.End {
			t.Fatalf("%s: segment %q straddles a span end", name, g.Text)
		}
	}
	// Isolating again changes nothing.
	again := string(p.Bytes())
	if _, err := Isolate(h, s); err != nil || string(p.Bytes()) != again {
		t.Fatalf("%s: second isolate: %v", name, err)
	}
	return nil
}

// TestUnknownPreserved: markup the taxonomy does not name, and runs whose
// properties follow their content, are never split or rewritten: a span
// inside or around them isolates with them byte-identical, or is refused
// with the document unchanged.
func TestUnknownPreserved(t *testing.T) {
	const x = ` xmlns:x="urn:example"`
	for _, c := range []struct {
		name, p    string
		start, end int
		want       error
		unknown    int
	}{
		{"zero-width unknown inside", `<w:p><w:r><w:t>ab</w:t></w:r><x:mark` + x + `/><w:r><w:t>cd</w:t></w:r></w:p>`, 1, 3, nil, 1},
		{"unknown wrapper whole", `<w:p><w:r><w:t>a</w:t></w:r><x:wrap` + x + `><w:r><w:t>bc</w:t></w:r></x:wrap><w:r><w:t>d</w:t></w:r></w:p>`, 1, 3, nil, 1},
		{"end inside an unknown wrapper", `<w:p><x:wrap` + x + `><w:r><w:t>bc</w:t></w:r></x:wrap></w:p>`, 0, 1, ErrUnknown, 0},
		{"unknown w element whole", `<w:p><w:r><w:t>a</w:t><w:futureLeaf/><w:t>b</w:t></w:r></w:p>`, 0, 2, nil, 1},
		{"properties after content", `<w:p><w:r><w:t>ab</w:t><w:rPr><w:b/></w:rPr><w:t>c</w:t></w:r></w:p>`, 0, 1, ErrRunOrder, 0},
		{"properties after content, split after them", `<w:p><w:r><w:t>ab</w:t><w:rPr><w:b/></w:rPr><w:t>c</w:t></w:r></w:p>`, 2, 3, nil, 0},
	} {
		t.Run(c.name, func(t *testing.T) {
			w, p := one(t, c.p)
			before := map[*tree.Node]string{}
			p.Walk(func(n *tree.Node) bool {
				if model.IsElement(n) && model.Classify(n) == model.KindUnknown {
					before[n] = string(n.Bytes())
				}
				return true
			})
			orig := string(p.Bytes())
			iso, err := Isolate(w.Target(w.DocumentPart(), "paragraph", p), Span{p, c.start, c.end})
			if !errors.Is(err, c.want) {
				t.Fatalf("err %v, want %v", err, c.want)
			}
			if err != nil {
				if string(p.Bytes()) != orig {
					t.Errorf("refusal changed the paragraph: %s", p.Bytes())
				}
				return
			}
			for n, b := range before {
				if string(n.Bytes()) != b || !model.Within(n, p) {
					t.Errorf("unknown element changed: %s -> %s", b, n.Bytes())
				}
			}
			if got := len(iso.Unknown()); got != c.unknown {
				t.Errorf("%d unknown in the span, want %d", got, c.unknown)
			}
			if err := w.Verify(); err != nil {
				t.Error(err)
			}
		})
	}
}

// TestDamagedPartsDoNotBlockEditing: a media member whose stored bytes
// fail their checksum and malformed numbering and comments parts are never
// read by a span edit of the body: the workspace opens, the span
// isolates, the change verifies, and the damaged members are untouched.
func TestDamagedPartsDoNotBlockEditing(t *testing.T) {
	payload := "\x89PNG-payload-0123456789"
	b := packageOf(t, docOf(`<w:p><w:r><w:t>hello world</w:t></w:r></w:p>`),
		[2]string{"word/media/a.png", payload},
		[2]string{"word/numbering.xml", `<w:numbering xmlns:w="` + nsW + `"><w:num>`},
		[2]string{"word/comments.xml", `<w:comments`})
	i := strings.Index(string(b), payload)
	b[i+len(payload)-1] ^= 0xFF
	w, ps := workspace(t, b)
	p := ps[0]
	if _, err := Isolate(w.Target(w.DocumentPart(), "paragraph", p), Span{p, 2, 7}); err != nil {
		t.Fatal(err)
	}
	if err := w.Verify(); err != nil {
		t.Fatal(err)
	}
	changed, err := w.Package().ChangedParts()
	if err != nil || len(changed) != 1 || changed[0] != "/word/document.xml" {
		t.Errorf("changed parts %v, %v", changed, err)
	}
}
