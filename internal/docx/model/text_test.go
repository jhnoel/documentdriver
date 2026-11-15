package model

import (
	"testing"
)

// TestLeafText: an equation counts as one U+FFFC and a break as "\n", in
// text and in segment offsets alike; nothing inside an equation counts.
func TestLeafText(t *testing.T) {
	const m = `xmlns:m="http://schemas.openxmlformats.org/officeDocument/2006/math"`
	for _, c := range []struct{ body, want string }{
		{`<w:r><w:t>before</w:t></w:r><m:oMathPara ` + m + `><m:oMath><m:r><m:t>x</m:t></m:r></m:oMath></m:oMathPara><w:r><w:t>after</w:t></w:r>`, "before￼after"},
		{`<w:r><w:t>one</w:t></w:r><w:r><w:br/></w:r><w:r><w:t>two</w:t><w:cr/></w:r><m:oMath ` + m + `><m:r><m:t>y</m:t></m:r></m:oMath>`, "one\ntwo\n￼"},
		{`<m:oMathPara ` + m + `><m:oMath><m:r><m:t>a</m:t><w:br/><w:t>z</w:t></m:r></m:oMath></m:oMathPara>`, "￼"},
		{`<w:del w:id="1"><w:r><w:br/><w:delText>d</w:delText></w:r></w:del><w:r><w:t>k</w:t></w:r>`, "k"},
		// Tabs, hyphens and symbols are characters too; objects one U+FFFC;
		// markers, field codes and properties nothing.
		{`<w:r><w:t>a</w:t><w:tab/><w:ptab w:alignment="right"/><w:noBreakHyphen/><w:softHyphen/><w:sym w:font="Symbol" w:char="F0B7"/></w:r>`, "a\t\t\u2011\u00AD￼"},
		{`<w:r><w:drawing><w:txbxContent><w:p><w:r><w:t>in</w:t></w:r></w:p></w:txbxContent></w:drawing><w:pict/><w:object/><w:footnoteReference w:id="1"/><w:endnoteReference w:id="2"/><w:pgNum/></w:r>`, "￼￼￼￼￼￼"},
		{`<w:proofErr/><w:bookmarkStart w:id="0" w:name="b"/><w:r><w:rPr><w:b/></w:rPr><w:lastRenderedPageBreak/><w:fldChar w:fldCharType="begin"/><w:instrText>PAGE</w:instrText><w:fldChar w:fldCharType="separate"/><w:t>7</w:t><w:fldChar w:fldCharType="end"/></w:r><w:bookmarkEnd w:id="0"/>`, "7"},
		{`<w:r><w:t><![CDATA[c` + "\r\n" + `d]]></w:t></w:r>`, "c\nd"},
	} {
		x, err := ParseXML([]byte(`<w:document xmlns:w="` + NSW + `"><w:body><w:p>` + c.body + `</w:p></w:body></w:document>`))
		if err != nil {
			t.Fatal(err)
		}
		p := x.Paragraphs()[0].Node
		if got := ParagraphText(p, Current); got != c.want {
			t.Errorf("text %q, want %q", got, c.want)
		}
		segs := BuildSegments(p)
		var cur string
		for _, s := range segs {
			if s.Wrap != WrapDel {
				cur += s.Text
			}
		}
		if cur != c.want {
			t.Errorf("segments %q, want %q", cur, c.want)
		}
		if all := AllText(segs); all != ParagraphText(p, All) {
			t.Errorf("all-view segments %q, text %q", all, ParagraphText(p, All))
		}
	}
}
