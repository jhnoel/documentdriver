package revise

import (
	"strings"
	"testing"

	"github.com/jhnoel/documentdriver/internal/docx/model"
	"github.com/jhnoel/documentdriver/internal/ooxml/tree"
)

// TestSettleSelection settles a selection by paragraph and by author:
// only the selected paragraphs' revisions go, never a row or cell marker
// in them, and the author narrows the paragraphs.
func TestSettleSelection(t *testing.T) {
	body := `<w:p><w:ins w:id="1" w:author="Ann"><w:r><w:t>a</w:t></w:r></w:ins><w:del w:id="2" w:author="Bob"><w:r><w:delText>b</w:delText></w:r></w:del></w:p>` +
		`<w:tbl><w:tr><w:trPr><w:ins w:id="3" w:author="Ann"/></w:trPr><w:tc><w:tcPr><w:cellIns w:id="4" w:author="Ann"/></w:tcPr>` +
		`<w:p><w:ins w:id="5" w:author="Ann"><w:r><w:t>c</w:t></w:r></w:ins></w:p></w:tc></w:tr></w:tbl>` +
		`<w:p><w:ins w:id="6" w:author="Ann"><w:r><w:t>d</w:t></w:r></w:ins></w:p>`
	for _, c := range []struct {
		name   string
		paras  []int // indexes into the body's paragraphs, in document order
		author string
		want   []string // ids left
	}{
		{"first paragraph", []int{0}, "", []string{"3", "4", "5", "6"}},
		{"cell paragraph keeps its row and cell", []int{1}, "", []string{"1", "2", "3", "4", "6"}},
		{"author in the first paragraph", []int{0}, "Bob", []string{"1", "3", "4", "5", "6"}},
		{"author everywhere", nil, "Ann", []string{"2"}},
	} {
		x, err := model.ParseXML([]byte(head + body + `</w:body></w:document>`))
		if err != nil {
			t.Fatal(err)
		}
		var paras map[*tree.Node]bool
		if c.paras != nil {
			all := model.ParagraphsIn(Scope(x))
			paras = map[*tree.Node]bool{}
			for _, i := range c.paras {
				paras[all[i].Node] = true
			}
		}
		var author *string
		if c.author != "" {
			author = &c.author
		}
		if _, err := Settle(private(x), Scope(x), Options{Accept: true, Select: Selection(paras, author)}); err != nil {
			t.Fatal(err)
		}
		var left []string
		for _, n := range x.RevisionElements() {
			left = append(left, model.AttrOr(n, "id"))
		}
		if strings.Join(left, ",") != strings.Join(c.want, ",") {
			t.Errorf("%s: left %v, want %v", c.name, left, c.want)
		}
	}
	if Selection(nil, nil) != nil {
		t.Error("an unnarrowed selection is not nil")
	}
}

// TestRejectMarkFormattingKeepsPendingMark: rejecting a paragraph mark's
// rPrChange alone restores the snapshot's properties without its mark
// revision, and keeps the mark's current revision, which is not settled
// (tdf123460.docx: rejecting one author's formatting change dropped
// another author's pending move).
func TestRejectMarkFormattingKeepsPendingMark(t *testing.T) {
	body := `<w:p><w:pPr><w:rPr><w:moveTo w:id="5" w:author="A"/><w:noProof/>` +
		`<w:rPrChange w:id="6" w:author="B"><w:rPr><w:moveTo w:id="7" w:author="A"/><w:b/></w:rPr></w:rPrChange></w:rPr></w:pPr></w:p>`
	x, err := model.ParseXML([]byte(head + body + `</w:body></w:document>`))
	if err != nil {
		t.Fatal(err)
	}
	b := "B"
	if _, err := Settle(private(x), Scope(x), Options{Select: Selection(nil, &b)}); err != nil {
		t.Fatal(err)
	}
	got := strings.TrimSuffix(strings.TrimPrefix(string(x.Doc.Bytes()), head), `</w:body></w:document>`)
	if want := `<w:p><w:pPr><w:rPr><w:moveTo w:id="5" w:author="A"/><w:b/></w:rPr></w:pPr></w:p>`; got != want {
		t.Errorf("got  %s\nwant %s", got, want)
	}
}

// TestAuthorTakesTheGrid: a w:tblGridChange has no author; settling an
// author's cell spans settles the grid change of their table with them,
// and not that of a table without their changes.
func TestAuthorTakesTheGrid(t *testing.T) {
	grid := func(n string) string {
		return `<w:tblGrid><w:gridCol w:w="1"/><w:tblGridChange w:id="` + n + `"><w:tblGrid><w:gridCol w:w="1"/><w:gridCol w:w="1"/></w:tblGrid></w:tblGridChange></w:tblGrid>`
	}
	body := `<w:tbl>` + grid("1") + `<w:tr><w:tc><w:tcPr><w:tcPrChange w:id="2" w:author="Ann"><w:tcPr><w:gridSpan w:val="2"/></w:tcPr></w:tcPrChange></w:tcPr><w:p/></w:tc></w:tr></w:tbl>` +
		`<w:tbl>` + grid("3") + `<w:tr><w:tc><w:tcPr><w:tcPrChange w:id="4" w:author="Bob"><w:tcPr><w:gridSpan w:val="2"/></w:tcPr></w:tcPrChange></w:tcPr><w:p/></w:tc></w:tr></w:tbl><w:p/>`
	x, err := model.ParseXML([]byte(head + body + `</w:body></w:document>`))
	if err != nil {
		t.Fatal(err)
	}
	ann := "Ann"
	if _, err := Settle(private(x), Scope(x), Options{Select: Selection(nil, &ann)}); err != nil {
		t.Fatal(err)
	}
	var left []string
	for _, n := range x.RevisionElements() {
		left = append(left, model.AttrOr(n, "id"))
	}
	if got := strings.Join(left, ","); got != "3,4" {
		t.Errorf("left %s, want 3,4", got)
	}
}
