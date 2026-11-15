package ops

import (
	"fmt"
	"strings"
	"testing"

	"github.com/jhnoel/documentdriver/internal/docx/model"
	"github.com/jhnoel/documentdriver/internal/docx/span"
	"github.com/jhnoel/documentdriver/internal/ooxml/tree"
)

// hiddenPara parses a one-paragraph document fragment and returns its
// w:p root.
func hiddenPara(t *testing.T, inner string) *tree.Node {
	t.Helper()
	doc, err := tree.ParseString(`<w:p xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"` +
		` xmlns:w14="http://schemas.microsoft.com/office/word/2010/wordml" w14:paraId="0000000A">` + inner + `</w:p>`)
	if err != nil {
		t.Fatal(err)
	}
	return doc.Root()
}

const (
	hiddenPlain   = `<w:r><w:t>alpha gamma</w:t></w:r>`
	hiddenWithDel = `<w:r><w:t>alpha </w:t></w:r>` +
		`<w:del w:id="1" w:author="Bob"><w:r><w:delText>beta</w:delText></w:r></w:del>` +
		`<w:r><w:t> gamma</w:t></w:r>`
)

// hiddenCases are the three misses: no deletions in the paragraph, a
// tracked deletion the selector names, and a tracked deletion the
// selector does not name.
var hiddenCases = []struct {
	name  string
	inner string
	sel   string
	want  bool // the message names hidden deleted text
}{
	{"plain paragraph, absent text", hiddenPlain, "zzz", false},
	{"deleted text, selector in it", hiddenWithDel, "beta", true},
	{"deleted text, selector absent from it too", hiddenWithDel, "zzz", false},
}

func checkHidden(t *testing.T, err error, want bool) {
	t.Helper()
	if err == nil {
		t.Fatal("no error, want a not_found refusal")
	}
	r, ok := err.(*Rejection)
	if !ok {
		t.Fatalf("error is %T, want *Rejection", err)
	}
	if r.Code != "not_found" {
		t.Errorf("code %q, want not_found", r.Code)
	}
	if got := strings.Contains(r.Msg, "hidden deleted text"); got != want {
		t.Errorf("message %q: names hidden deleted text = %v, want %v", r.Msg, got, want)
	}
}

func TestSelectOneHiddenDeletedClause(t *testing.T) {
	for _, c := range hiddenCases {
		t.Run(c.name, func(t *testing.T) {
			p := hiddenPara(t, c.inner)
			_, _, _, err := selectOne(p, "0000000A", c.sel, nil)
			checkHidden(t, err, c.want)
		})
	}
}

func TestCommentAddHiddenDeletedClause(t *testing.T) {
	for _, c := range hiddenCases {
		t.Run(c.name, func(t *testing.T) {
			p := hiddenPara(t, c.inner)
			o := &CommentAdd{Select: Ptr(c.sel)}
			_, err := o.anchor(p, "0000000A")
			checkHidden(t, err, c.want)
		})
	}
}

func TestTextReviseWithinHiddenDeletedClause(t *testing.T) {
	for _, c := range hiddenCases {
		t.Run(c.name, func(t *testing.T) {
			p := hiddenPara(t, c.inner)
			o := &ReviseText{Within: Ptr(c.sel)}
			cands := []reviseRegion{{part: "/word/document.xml", p: p, id: "0000000A",
				text: []rune(span.Read(p).Text(model.Current))}}
			fail := func(code, field, format string, args ...any) error {
				return &Rejection{Code: code, Field: field,
					Msg: "blocked in paragraph 0000000A: " + fmt.Sprintf(format, args...)}
			}
			_, err := o.regions(cands, "0000000A", fail)
			checkHidden(t, err, c.want)
		})
	}
}
