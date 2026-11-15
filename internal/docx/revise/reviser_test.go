package revise

import (
	"strings"
	"testing"
	"time"

	"github.com/jhnoel/documentdriver/internal/docx/model"
	"github.com/jhnoel/documentdriver/internal/ooxml/edit"
	"github.com/jhnoel/documentdriver/internal/ooxml/tree"
)

var clock = time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)

// authoring is one use of the Reviser on a body, given the parsed part.
type authoring struct {
	name string
	body string
	run  func(t *testing.T, r *Reviser, h *edit.Handle, x *model.XML) error
	// track is the body after the tracked authoring.
	track string
	// rejected, when set, is the input of the rejection law (body with
	// what the operation placed before authoring taken out).
	rejected string
}

func paras(x *model.XML) []*tree.Node {
	var out []*tree.Node
	for _, p := range model.ParagraphsIn(x.Body()) {
		out = append(out, p.Node)
	}
	return out
}

func runs(p *tree.Node) []*tree.Node {
	return model.Descendants(p, func(n *tree.Node) bool { return model.IsW(n, "r") && n.Parent() == p })
}

var authorings = []authoring{
	{
		name: "insert placed runs",
		// The op placed run b before the Reviser marks it inserted; the
		// input of the rejection law is the body without it.
		body:     `<w:p><w:r><w:t>a</w:t></w:r><w:r><w:t>b</w:t></w:r></w:p>`,
		rejected: `<w:p><w:r><w:t>a</w:t></w:r></w:p>`,
		run: func(_ *testing.T, r *Reviser, h *edit.Handle, x *model.XML) error {
			_, err := r.Insert(h, runs(paras(x)[0])[1:])
			return err
		},
		track: `<w:p><w:r><w:t>a</w:t></w:r><w:ins w:id="1" w:author="Ann" w:date="2026-01-02T03:04:05Z"><w:r><w:t>b</w:t></w:r></w:ins></w:p>`,
	},
	{
		name: "delete beside a foreign revision keeps its id space",
		body: `<w:p><w:ins w:id="7" w:author="Bob"><w:r><w:t>x</w:t></w:r></w:ins><w:r><w:t>a</w:t><w:instrText>REF</w:instrText></w:r></w:p>`,
		run: func(_ *testing.T, r *Reviser, h *edit.Handle, x *model.XML) error {
			return r.Delete(h, runs(paras(x)[0]))
		},
		track: `<w:p><w:ins w:id="7" w:author="Bob"><w:r><w:t>x</w:t></w:r></w:ins><w:del w:id="8" w:author="Ann" w:date="2026-01-02T03:04:05Z"><w:r><w:delText>a</w:delText><w:delInstrText>REF</w:delInstrText></w:r></w:del></w:p>`,
	},
	{
		name: "deleting the author's own insertion removes it",
		body: `<w:p><w:r><w:t>a</w:t></w:r><w:ins w:id="3" w:author="Ann"><w:r><w:t>b</w:t></w:r></w:ins></w:p>`,
		run: func(_ *testing.T, r *Reviser, h *edit.Handle, x *model.XML) error {
			ins := model.ChildW(paras(x)[0], "ins")
			return r.Delete(h, []*tree.Node{model.ChildW(ins, "r")})
		},
		track: `<w:p><w:r><w:t>a</w:t></w:r></w:p>`,
	},
	{
		name: "deleted paragraph mark",
		body: `<w:p><w:r><w:t>a</w:t></w:r></w:p><w:p><w:pPr><w:jc w:val="center"/></w:pPr><w:r><w:t>b</w:t></w:r></w:p>`,
		run: func(_ *testing.T, r *Reviser, h *edit.Handle, x *model.XML) error {
			_, err := r.MarkParagraph(h, paras(x)[0], "del")
			return err
		},
		track: `<w:p><w:pPr><w:rPr><w:del w:id="1" w:author="Ann" w:date="2026-01-02T03:04:05Z"/></w:rPr></w:pPr><w:r><w:t>a</w:t></w:r></w:p><w:p><w:pPr><w:jc w:val="center"/></w:pPr><w:r><w:t>b</w:t></w:r></w:p>`,
	},
	{
		name: "deleting the author's own inserted mark joins at once",
		body: `<w:p><w:pPr><w:rPr><w:ins w:id="2" w:author="Ann"/></w:rPr></w:pPr><w:r><w:t>a</w:t></w:r></w:p><w:p><w:r><w:t>b</w:t></w:r></w:p>`,
		run: func(_ *testing.T, r *Reviser, h *edit.Handle, x *model.XML) error {
			_, err := r.MarkParagraph(h, paras(x)[0], "del")
			return err
		},
		track: `<w:p><w:r><w:t>a</w:t></w:r><w:r><w:t>b</w:t></w:r></w:p>`,
	},
	{
		name: "paragraph property record",
		body: `<w:p><w:pPr><w:jc w:val="left"/><w:rPr><w:b/></w:rPr></w:pPr><w:r><w:t>a</w:t></w:r></w:p>`,
		run: func(t *testing.T, r *Reviser, h *edit.Handle, x *model.XML) error {
			ppr := model.ChildW(paras(x)[0], "pPr")
			changed, err := r.Property(h, ppr, func() error {
				return h.SetNodeAttr(model.ChildW(ppr, "jc"), "w:val", "right")
			})
			if !changed {
				t.Error("Property reported no change")
			}
			return err
		},
		track: `<w:p><w:pPr><w:jc w:val="right"/><w:rPr><w:b/></w:rPr><w:pPrChange w:id="1" w:author="Ann" w:date="2026-01-02T03:04:05Z"><w:pPr><w:jc w:val="left"/></w:pPr></w:pPrChange></w:pPr><w:r><w:t>a</w:t></w:r></w:p>`,
	},
	{
		name: "unchanged properties record nothing",
		body: `<w:p><w:pPr><w:jc w:val="left"/></w:pPr></w:p>`,
		run: func(_ *testing.T, r *Reviser, h *edit.Handle, x *model.XML) error {
			ppr := model.ChildW(paras(x)[0], "pPr")
			_, err := r.Property(h, ppr, func() error { return h.SetNodeAttr(model.ChildW(ppr, "jc"), "w:val", "left") })
			return err
		},
		track: `<w:p><w:pPr><w:jc w:val="left"/></w:pPr></w:p>`,
	},
	{
		name: "deleted row and the author's own inserted row",
		body: `<w:tbl><w:tr><w:tc><w:p/></w:tc></w:tr><w:tr><w:trPr><w:ins w:id="4" w:author="Ann"/></w:trPr><w:tc><w:p/></w:tc></w:tr></w:tbl><w:p/>`,
		run: func(_ *testing.T, r *Reviser, h *edit.Handle, x *model.XML) error {
			rows := model.Descendants(x.Body(), func(n *tree.Node) bool { return model.IsW(n, "tr") })
			if _, err := r.MarkRow(h, rows[0], "del"); err != nil {
				return err
			}
			_, err := r.MarkRow(h, rows[1], "del")
			return err
		},
		track: `<w:tbl><w:tr><w:trPr><w:del w:id="1" w:author="Ann" w:date="2026-01-02T03:04:05Z"/></w:trPr><w:tc><w:p/></w:tc></w:tr></w:tbl><w:p/>`,
	},
}

func parse(t *testing.T, body string) *model.XML {
	t.Helper()
	x, err := model.ParseXML([]byte(head + body + `</w:body></w:document>`))
	if err != nil {
		t.Fatal(err)
	}
	return x
}

func bodyOf(x *model.XML) string {
	out := strings.TrimPrefix(string(x.Doc.Bytes()), head)
	return strings.TrimSuffix(out, `</w:body></w:document>`)
}

func settleAll(t *testing.T, body string, accept bool) string {
	t.Helper()
	x := parse(t, body)
	if _, err := Settle(private(x), Scope(x), Options{Accept: accept}); err != nil {
		t.Fatal(err)
	}
	return bodyOf(x)
}

func author(t *testing.T, a authoring, mode Mode) string {
	t.Helper()
	x := parse(t, a.body)
	r := New("Ann", clock, mode)
	h := private(x)
	if err := a.run(t, r, h, x); err != nil {
		t.Fatalf("%s: %v", a.name, err)
	}
	if _, err := r.Finish(); err != nil {
		t.Fatalf("%s: finish: %v", a.name, err)
	}
	return bodyOf(x)
}

// TestReviser checks each authoring's tracked form and the change-mode
// law on it: direct equals the tracked output with the authored
// revisions accepted, and rejecting everything in the tracked output
// equals rejecting everything in the input.
func TestReviser(t *testing.T) {
	for _, a := range authorings {
		track := author(t, a, Track)
		if track != a.track {
			t.Errorf("%s track:\ngot  %s\nwant %s", a.name, track, a.track)
		}
		direct := author(t, a, Direct)
		// Accepting only Ann's revisions with the reviser's own ledger is
		// what Direct does; here the tracked output's Ann revisions dated
		// by the clock are the authored ones.
		x := parse(t, track)
		if _, err := Settle(private(x), Scope(x), Options{Accept: true, Select: func(n *tree.Node) bool {
			return model.AttrOr(n, "date") == clock.Format(DateLayout)
		}}); err != nil {
			t.Fatal(err)
		}
		// Direct drops the empty containers it created for a mark;
		// settling the tracked output leaves them, and they say nothing.
		if got := bodyOf(x); bare(got) != bare(direct) {
			t.Errorf("%s: accept(track) != direct:\naccepted %s\ndirect   %s", a.name, got, direct)
		}
		input := a.body
		if a.rejected != "" {
			input = a.rejected
		}
		if got, want := bare(settleAll(t, track, false)), bare(settleAll(t, input, false)); got != want {
			t.Errorf("%s: reject(track) != reject(input):\ngot  %s\nwant %s", a.name, got, want)
		}
	}
}

// TestDirectLeavesForeignRevisions checks that direct mode accepts the
// ledger only, not other revisions in the part.
func TestDirectLeavesForeignRevisions(t *testing.T) {
	a := authorings[1]
	got := author(t, a, Direct)
	want := `<w:p><w:ins w:id="7" w:author="Bob"><w:r><w:t>x</w:t></w:r></w:ins></w:p>`
	if got != want {
		t.Errorf("got  %s\nwant %s", got, want)
	}
}

// bare drops the empty property containers settling a mark leaves, which
// say nothing (docxdriver's change-mode tests drop them the same way).
func bare(body string) string {
	for {
		next := body
		for _, empty := range []string{"<w:rPr></w:rPr>", "<w:pPr></w:pPr>", "<w:rPr/>", "<w:pPr/>", "<w:trPr></w:trPr>"} {
			next = strings.ReplaceAll(next, empty, "")
		}
		if next == body {
			return body
		}
		body = next
	}
}

// TestRecordAttributes checks that a property-change record carries
// only the attributes its schema type has: a w:tblGridChange (CT_Markup)
// an id alone, a w:sectPrChange the id, author and date.
func TestRecordAttributes(t *testing.T) {
	for _, c := range []struct {
		name, body, want string
		bag              func(x *model.XML) *tree.Node
		local            string
	}{
		{"table grid", `<w:tbl><w:tblPr/><w:tblGrid><w:gridCol w:w="100"/></w:tblGrid><w:tr><w:tc><w:p/></w:tc></w:tr></w:tbl><w:p/>`,
			`<w:tblGrid><w:gridCol w:w="200"/><w:tblGridChange w:id="1"><w:tblGrid><w:gridCol w:w="100"/></w:tblGrid></w:tblGridChange></w:tblGrid>`,
			func(x *model.XML) *tree.Node { return model.ChildW(model.ChildW(x.Body(), "tbl"), "tblGrid") }, "gridCol"},
		{"section", `<w:p/><w:sectPr><w:pgMar w:w="100"/></w:sectPr>`,
			`<w:sectPr><w:pgMar w:w="200"/><w:sectPrChange w:id="1" w:author="Ann" w:date="2026-01-02T03:04:05Z"><w:sectPr><w:pgMar w:w="100"/></w:sectPr></w:sectPrChange></w:sectPr>`,
			func(x *model.XML) *tree.Node { return model.ChildW(x.Body(), "sectPr") }, "pgMar"},
	} {
		x := parse(t, c.body)
		r := New("Ann", clock, Track)
		h := private(x)
		bag := c.bag(x)
		if _, err := r.Property(h, bag, func() error { return h.SetNodeAttr(model.ChildW(bag, c.local), "w:w", "200") }); err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		if _, err := r.Finish(); err != nil {
			t.Fatal(err)
		}
		if got := bodyOf(x); !strings.Contains(got, c.want) {
			t.Errorf("%s: want %s in %s", c.name, c.want, got)
		}
	}
}

// TestFinishWalksOnce: numbering an operation's revisions walks the part
// once (the highest existing id and the operation's own revisions in
// one pass), so a plan of many operations costs one walk per operation,
// not two or more: the time-only O(operations x document) cost the
// allocation gate cannot see.
func TestFinishWalksOnce(t *testing.T) {
	var body strings.Builder
	for range 200 {
		body.WriteString(`<w:p><w:r><w:t>text</w:t></w:r></w:p>`)
	}
	walked := 0
	for _, a := range authorings {
		x := parse(t, a.body+body.String())
		r := New("Ann", clock, Track)
		if err := a.run(t, r, private(x), x); err != nil {
			t.Fatalf("%s: %v", a.name, err)
		}
		if _, err := r.Finish(); err != nil {
			t.Fatalf("%s: finish: %v", a.name, err)
		}
		nodes := 0
		x.Root().Walk(func(*tree.Node) bool { nodes++; return true })
		if r.visited > nodes {
			t.Errorf("%s: finish visited %d nodes of a part of %d: more than one walk", a.name, r.visited, nodes)
		}
		if r.visited > 0 {
			walked++
		}
	}
	if walked < len(authorings)/2 {
		t.Errorf("only %d of %d authorings numbered revisions: the test checks little", walked, len(authorings))
	}
}
