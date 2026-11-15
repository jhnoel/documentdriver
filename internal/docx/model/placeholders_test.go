package model

import (
	"strings"
	"testing"

	"github.com/jhnoel/documentdriver/internal/ooxml/tree"
)

func firstParagraph(t *testing.T, content string) *tree.Node {
	t.Helper()
	d, err := tree.Parse([]byte(`<w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main" ` +
		`xmlns:mc="http://schemas.openxmlformats.org/markup-compatibility/2006" xmlns:x="urn:x"><w:body><w:p>` +
		content + `</w:p></w:body></w:document>`))
	if err != nil {
		t.Fatal(err)
	}
	return d.Root().Elements()[0].Elements()[0]
}

// TestPlaceholders: what counts as a placeholder, in document order, and
// what blocks.
func TestPlaceholders(t *testing.T) {
	for _, c := range []struct {
		name, xml, kinds, blocked string
	}{
		{"objects in runs", `<w:r><w:t>a</w:t><w:drawing/><w:sym/></w:r><w:r><w:footnoteReference w:id="1"/></w:r>`, "drawing symbol footnote", ""},
		{"wrappers", `<w:sdt><w:sdtContent><w:r><w:t>x</w:t></w:r></w:sdtContent></w:sdt><w:fldSimple w:instr="PAGE"/><w:ruby/><mc:AlternateContent/><x:y/>`, "content-control field ruby alternate-content unknown", ""},
		{"complex field", `<w:r><w:fldChar w:fldCharType="begin"/></w:r><w:r><w:instrText>REF a</w:instrText></w:r><w:r><w:fldChar w:fldCharType="separate"/></w:r><w:r><w:t>res</w:t><w:drawing/></w:r><w:r><w:fldChar w:fldCharType="end"/></w:r><w:r><w:sym/></w:r>`, "field symbol", ""},
		{"inside a hyperlink and an insertion", `<w:hyperlink><w:r><w:drawing/></w:r></w:hyperlink><w:ins><w:r><w:object/></w:r></w:ins>`, "drawing object", ""},
		{"deleted content is not current", `<w:del><w:r><w:drawing/></w:r></w:del><w:r><w:t>a</w:t></w:r>`, "", ""},
		{"field not closed", `<w:r><w:fldChar w:fldCharType="begin"/></w:r><w:r><w:t>x</w:t></w:r>`, "", "field"},
		{"field ends here", `<w:r><w:t>x</w:t></w:r><w:r><w:fldChar w:fldCharType="end"/></w:r>`, "", "field"},
		{"text before the begin", `<w:r><w:t>x</w:t><w:fldChar w:fldCharType="begin"/></w:r><w:r><w:fldChar w:fldCharType="end"/></w:r>`, "", "field"},
	} {
		p := firstParagraph(t, c.xml)
		list, blocked := Placeholders(p)
		var kinds []string
		for _, ph := range list {
			kinds = append(kinds, ph.Kind)
		}
		if got := strings.Join(kinds, " "); got != c.kinds || blocked != c.blocked {
			t.Errorf("%s: kinds %q blocked %q, want %q %q", c.name, got, blocked, c.kinds, c.blocked)
		}
	}
	p := firstParagraph(t, `<w:r><w:fldChar w:fldCharType="begin"/></w:r><w:r><w:t>re</w:t><w:tab/></w:r><w:r><w:t>s</w:t><w:fldChar w:fldCharType="end"/></w:r>`)
	list, _ := Placeholders(p)
	if len(list) != 1 || len(list[0].Nodes) != 3 || PlaceholderText(list[0]) != "re\ts" {
		t.Errorf("field unit %+v %q", list, PlaceholderText(list[0]))
	}
}
