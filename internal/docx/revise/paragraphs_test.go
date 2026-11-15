package revise

import (
	"testing"

	"github.com/jhnoel/documentdriver/internal/docx/model"
	"github.com/jhnoel/documentdriver/internal/ooxml/edit"
	"github.com/jhnoel/documentdriver/internal/ooxml/tree"
)

const ann = ` w:author="Ann" w:date="2026-01-02T03:04:05Z"`

// deleting returns an authoring that deletes the paragraphs at indices.
func deleting(name, body, track string, at ...int) authoring {
	return authoring{
		name: name, body: body, track: track,
		run: func(_ *testing.T, r *Reviser, h *edit.Handle, x *model.XML) error {
			all := paras(x)
			var ps []*tree.Node
			for _, i := range at {
				ps = append(ps, all[i])
			}
			_, err := r.DeleteParagraphs(h, ps)
			return err
		},
	}
}

var deletions = []authoring{
	deleting("paragraph joins the next",
		`<w:p><w:r><w:t>a</w:t></w:r></w:p><w:p><w:r><w:t>b</w:t></w:r></w:p>`,
		`<w:p><w:pPr><w:rPr><w:del w:id="1"`+ann+`/></w:rPr></w:pPr><w:del w:id="2"`+ann+`><w:r><w:delText>a</w:delText></w:r></w:del></w:p><w:p><w:r><w:t>b</w:t></w:r></w:p>`, 0),
	deleting("hyperlink, foreign insertion, own insertion, markers and an existing deletion",
		`<w:p><w:hyperlink><w:r><w:t>h</w:t></w:r></w:hyperlink><w:bookmarkStart w:id="0" w:name="b"/><w:ins w:id="5" w:author="Bob"><w:r><w:t>x</w:t></w:r></w:ins><w:ins w:id="6" w:author="Ann"><w:r><w:t>y</w:t></w:r></w:ins><w:del w:id="7" w:author="Bob"><w:r><w:delText>z</w:delText></w:r></w:del><w:bookmarkEnd w:id="0"/></w:p><w:p/>`,
		`<w:p><w:pPr><w:rPr><w:del w:id="8"`+ann+`/></w:rPr></w:pPr><w:hyperlink><w:del w:id="9"`+ann+`><w:r><w:delText>h</w:delText></w:r></w:del></w:hyperlink><w:bookmarkStart w:id="0" w:name="b"/><w:ins w:id="5" w:author="Bob"><w:del w:id="10"`+ann+`><w:r><w:delText>x</w:delText></w:r></w:del></w:ins><w:del w:id="7" w:author="Bob"><w:r><w:delText>z</w:delText></w:r></w:del><w:bookmarkEnd w:id="0"/></w:p><w:p/>`, 0),
	deleting("the last paragraph takes the previous mark and adopts its properties",
		`<w:p><w:pPr><w:jc w:val="center"/></w:pPr><w:r><w:t>a</w:t></w:r></w:p><w:p><w:pPr><w:jc w:val="right"/></w:pPr><w:r><w:t>b</w:t></w:r></w:p>`,
		`<w:p><w:pPr><w:jc w:val="center"/><w:rPr><w:del w:id="1"`+ann+`/></w:rPr></w:pPr><w:r><w:t>a</w:t></w:r></w:p><w:p><w:pPr><w:jc w:val="center"/><w:pPrChange w:id="2"`+ann+`><w:pPr><w:jc w:val="right"/></w:pPr></w:pPrChange></w:pPr><w:del w:id="3"`+ann+`><w:r><w:delText>b</w:delText></w:r></w:del></w:p>`, 1),
	deleting("a paragraph before a table with nothing before it stays empty",
		`<w:p><w:r><w:t>a</w:t></w:r></w:p><w:tbl><w:tr><w:tc><w:p/></w:tc></w:tr></w:tbl><w:p/>`,
		`<w:p><w:del w:id="1"`+ann+`><w:r><w:delText>a</w:delText></w:r></w:del></w:p><w:tbl><w:tr><w:tc><w:p/></w:tc></w:tr></w:tbl><w:p/>`, 0),
	deleting("the author's own pending paragraph goes outright",
		`<w:p><w:pPr><w:rPr><w:ins w:id="3" w:author="Ann"/></w:rPr></w:pPr><w:ins w:id="4" w:author="Ann"><w:r><w:t>a</w:t></w:r></w:ins></w:p><w:p><w:r><w:t>b</w:t></w:r></w:p>`,
		`<w:p><w:r><w:t>b</w:t></w:r></w:p>`, 0),
	deleting("two consecutive paragraphs",
		`<w:p><w:r><w:t>a</w:t></w:r></w:p><w:p><w:r><w:t>b</w:t></w:r></w:p><w:p><w:r><w:t>c</w:t></w:r></w:p>`,
		`<w:p><w:pPr><w:rPr><w:del w:id="1"`+ann+`/></w:rPr></w:pPr><w:del w:id="2"`+ann+`><w:r><w:delText>a</w:delText></w:r></w:del></w:p><w:p><w:pPr><w:rPr><w:del w:id="3"`+ann+`/></w:rPr></w:pPr><w:del w:id="4"`+ann+`><w:r><w:delText>b</w:delText></w:r></w:del></w:p><w:p><w:r><w:t>c</w:t></w:r></w:p>`, 0, 1),
}

// marks are the inserted-mark authorings (paragraph.insert's), whose
// direct mode accepts the mark and drops the containers made for it.
var marks = []authoring{
	{
		name: "inserted mark on a paragraph without properties",
		body: `<w:p><w:r><w:t>a</w:t></w:r></w:p><w:p/>`,
		// Before the op split it, the paragraph was one.
		rejected: `<w:p><w:r><w:t>a</w:t></w:r></w:p>`,
		track:    `<w:p><w:pPr><w:rPr><w:ins w:id="1"` + ann + `/></w:rPr></w:pPr><w:r><w:t>a</w:t></w:r></w:p><w:p/>`,
		run: func(_ *testing.T, r *Reviser, h *edit.Handle, x *model.XML) error {
			_, err := r.MarkParagraph(h, paras(x)[0], "ins")
			return err
		},
	},
}

// TestMarkLaws runs TestReviser's laws on the inserted marks.
func TestMarkLaws(t *testing.T) {
	saved := authorings
	defer func() { authorings = saved }()
	authorings = marks
	TestReviser(t)
}

// TestDeleteParagraphs checks paragraph deletion's tracked forms and the
// change-mode laws on them (TestReviser's).
func TestDeleteParagraphs(t *testing.T) {
	saved := authorings
	defer func() { authorings = saved }()
	authorings = deletions
	TestReviser(t)
}

func TestDeleteContentRefusesContentControls(t *testing.T) {
	x := parse(t, `<w:p><w:sdt><w:sdtContent><w:r><w:t>a</w:t></w:r></w:sdtContent></w:sdt></w:p><w:p/>`)
	r := New("Ann", clock, Track)
	before := bodyOf(x)
	if _, err := r.DeleteParagraphs(private(x), paras(x)[:1]); err != ErrStructuredTag {
		t.Fatalf("got %v, want ErrStructuredTag", err)
	}
	if bodyOf(x) != before {
		t.Error("a refused deletion changed the document")
	}
}

// TestDirectDropsCreatedShells checks that direct mode, accepting a mark
// it authored, removes the w:rPr and w:pPr it created for the mark when
// they end up empty, and keeps containers the source already had, empty
// or not.
func TestDirectDropsCreatedShells(t *testing.T) {
	for _, c := range []struct{ name, body, want string }{
		{"no properties", `<w:p><w:r><w:t>a</w:t></w:r></w:p><w:p/>`, `<w:p><w:r><w:t>a</w:t></w:r></w:p><w:p/>`},
		{"empty pPr of the source", `<w:p><w:pPr/><w:r><w:t>a</w:t></w:r></w:p><w:p/>`, `<w:p><w:pPr/><w:r><w:t>a</w:t></w:r></w:p><w:p/>`},
		{"empty rPr of the source", `<w:p><w:pPr><w:rPr></w:rPr></w:pPr><w:r><w:t>a</w:t></w:r></w:p><w:p/>`, `<w:p><w:pPr><w:rPr></w:rPr></w:pPr><w:r><w:t>a</w:t></w:r></w:p><w:p/>`},
		{"pPr with properties", `<w:p><w:pPr><w:jc w:val="center"/></w:pPr><w:r><w:t>a</w:t></w:r></w:p><w:p/>`, `<w:p><w:pPr><w:jc w:val="center"/></w:pPr><w:r><w:t>a</w:t></w:r></w:p><w:p/>`},
	} {
		x := parse(t, c.body)
		r := New("Ann", clock, Direct)
		if _, err := r.MarkParagraph(private(x), paras(x)[0], "ins"); err != nil {
			t.Fatal(err)
		}
		if _, err := r.Finish(); err != nil {
			t.Fatal(err)
		}
		if got := bodyOf(x); got != c.want {
			t.Errorf("%s:\ngot  %s\nwant %s", c.name, got, c.want)
		}
	}
}
