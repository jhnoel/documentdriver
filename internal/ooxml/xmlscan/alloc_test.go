package xmlscan_test

import (
	"testing"

	"github.com/jhnoel/documentdriver/internal/ooxml/xmlscan"
)

// TestAllocs holds the reading side to its promise: lexing, attribute
// iteration and lookup, element ends, spans and unchanged decode or
// escape allocate nothing.
func TestAllocs(t *testing.T) {
	src := `<sheetData><row r="1" spans="1:2"><c r="A1" s="3" t="s"><v>0</v></c>` +
		`<c r="B1"><f t="shared" ref="B1:B9" si="0">A1+1</f><v>1</v></c></row><!--x--><![CDATA[y]]></sheetData>`
	root, err := xmlscan.Next(src, 0)
	if err != nil {
		t.Fatal(err)
	}
	n := testing.AllocsPerRun(100, func() {
		for i := 0; i < len(src); {
			tok, err := xmlscan.Next(src, i)
			if err != nil {
				t.Fatal(err)
			}
			if tok.IsElement() {
				_ = tok.Name(src)
				_ = tok.Local(src)
				for a := range xmlscan.Attrs(src, tok) {
					_ = a.Raw(src)
					_ = a.Value(src)
				}
				if a, ok := xmlscan.FindAttr(src, tok, "r"); ok {
					_ = xmlscan.DecodeAttr(a.Raw(src))
				}
			}
			i = tok.End
		}
		if _, _, err := xmlscan.ElementEnd(src, root); err != nil {
			t.Fatal(err)
		}
		_ = xmlscan.DecodeText("plain")
		_ = xmlscan.EscapeText("plain")
		_ = xmlscan.EscapeAttr("plain", '"')
		_ = xmlscan.EscapeAttrMinimal("a>b", '"')
		_, _ = xmlscan.EditTag(src, root)
		_, _ = xmlscan.Apply(src, nil)
	})
	if n != 0 {
		t.Errorf("%v allocations per run, want 0", n)
	}
}
