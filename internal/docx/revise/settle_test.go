package revise

import (
	"strings"
	"testing"

	docxedit "github.com/jhnoel/documentdriver/internal/docx/edit"
	"github.com/jhnoel/documentdriver/internal/docx/model"
	"github.com/jhnoel/documentdriver/internal/ooxml/edit"
)

const head = `<w:document xmlns:w="` + model.NSW + `"><w:body>`

func settled(t *testing.T, body string, accept bool) string {
	t.Helper()
	x, err := model.ParseXML([]byte(head + body + `</w:body></w:document>`))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Settle(private(x), Scope(x), Options{Accept: accept}); err != nil {
		t.Fatal(err)
	}
	out := string(x.Doc.Bytes())
	out = strings.TrimPrefix(out, head)
	return strings.TrimSuffix(out, `</w:body></w:document>`)
}

func TestSettle(t *testing.T) {
	for _, c := range []struct {
		name, body string
		accept     bool
		want       string
	}{
		{"accept insertion", `<w:p><w:ins w:id="1"><w:r><w:t>a</w:t></w:r></w:ins></w:p>`, true, `<w:p><w:r><w:t>a</w:t></w:r></w:p>`},
		{"reject deletion revives text and field code",
			`<w:p><w:del w:id="1"><w:r><w:delText>a</w:delText><w:delInstrText>REF</w:delInstrText></w:r></w:del></w:p>`, false,
			`<w:p><w:r><w:t>a</w:t><w:instrText>REF</w:instrText></w:r></w:p>`},
		{"accept deletion keeps markers and comment references",
			`<w:p><w:del w:id="1"><w:bookmarkStart w:id="0" w:name="b"/><w:r><w:commentReference w:id="4"/><w:delText>a</w:delText></w:r></w:del></w:p>`, true,
			`<w:p><w:bookmarkStart w:id="0" w:name="b"/><w:r><w:commentReference w:id="4"/></w:r></w:p>`},
		{"accept deletion cutting a field drops its other parts",
			`<w:p><w:r><w:fldChar w:fldCharType="begin"/></w:r><w:r><w:instrText>PAGE</w:instrText></w:r><w:del w:id="1"><w:r><w:fldChar w:fldCharType="separate"/></w:r></w:del><w:r><w:t>1</w:t></w:r><w:r><w:fldChar w:fldCharType="end"/></w:r></w:p>`, true,
			`<w:p><w:r><w:t>1</w:t></w:r></w:p>`},
		{"deleted mark joins into the next paragraph",
			`<w:p><w:pPr><w:rPr><w:del w:id="1"/></w:rPr></w:pPr><w:r><w:t>a</w:t></w:r></w:p><w:p><w:pPr><w:jc w:val="center"/></w:pPr><w:r><w:t>b</w:t></w:r></w:p>`, true,
			`<w:p><w:pPr><w:jc w:val="center"/></w:pPr><w:r><w:t>a</w:t></w:r><w:r><w:t>b</w:t></w:r></w:p>`},
		{"rejected inserted row goes with its table",
			`<w:tbl><w:tr><w:trPr><w:ins w:id="1"/></w:trPr><w:tc><w:p/></w:tc></w:tr></w:tbl><w:p/>`, false, `<w:p/>`},
		{"accepted deleted cell empties the table into a paragraph",
			`<w:tbl><w:tr><w:tc><w:tcPr><w:cellDel w:id="1"/></w:tcPr><w:p/></w:tc></w:tr></w:tbl><w:p/>`, true, `<w:p/><w:p/>`},
		{"rejected cell change restores the snapshot",
			`<w:tbl><w:tr><w:tc><w:tcPr><w:tcW w:w="1"/><w:tcPrChange w:id="1"><w:tcPr><w:gridSpan w:val="2"/></w:tcPr></w:tcPrChange></w:tcPr><w:p/></w:tc></w:tr></w:tbl>`, false,
			`<w:tbl><w:tr><w:tc><w:tcPr><w:gridSpan w:val="2"/></w:tcPr><w:p/></w:tc></w:tr></w:tbl>`},
		{"rejected paragraph change keeps the mark properties",
			`<w:p><w:pPr><w:jc w:val="left"/><w:rPr><w:b/></w:rPr><w:pPrChange w:id="1"><w:pPr><w:jc w:val="right"/></w:pPr></w:pPrChange></w:pPr></w:p>`, false,
			`<w:p><w:pPr><w:jc w:val="right"/><w:rPr><w:b/></w:rPr></w:pPr></w:p>`},
	} {
		if got := settled(t, c.body, c.accept); got != c.want {
			t.Errorf("%s:\ngot  %s\nwant %s", c.name, got, c.want)
		}
	}
}

// private is a region over a private copy of the parsed part x.
func private(x *model.XML) *edit.Handle { return edit.Private(docxedit.Format, x.Root()) }
