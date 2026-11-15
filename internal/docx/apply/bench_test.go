package apply

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jhnoel/documentdriver/internal/docx/ops"
	"github.com/jhnoel/documentdriver/internal/laws"
)

// largeBody is the body of a large document: n paragraphs of mixed
// formatting, with a revision in every tenth and a table every hundredth,
// about 260 bytes of document.xml per paragraph.
func largeBody(n int) string {
	var b strings.Builder
	for i := 1; i <= n; i++ {
		fmt.Fprintf(&b, `<w:p w14:paraId="%08X"><w:pPr><w:spacing w:after="120"/><w:jc w:val="both"/></w:pPr>`, i)
		fmt.Fprintf(&b, `<w:r><w:rPr><w:b/></w:rPr><w:t xml:space="preserve">Paragraph %d opens </w:t></w:r>`, i)
		b.WriteString(`<w:r><w:t>with some ordinary text that runs on for a while.</w:t></w:r>`)
		if i%10 == 0 {
			fmt.Fprintf(&b, `<w:ins w:id="%d" w:author="Bob" w:date="2020-01-01T00:00:00Z"><w:r><w:t> Inserted.</w:t></w:r></w:ins>`, i)
		}
		b.WriteString(`</w:p>`)
		if i%100 == 0 {
			b.WriteString(`<w:tbl><w:tblPr><w:tblW w:w="0" w:type="auto"/></w:tblPr><w:tblGrid><w:gridCol w:w="4000"/><w:gridCol w:w="4000"/></w:tblGrid>`)
			for r := range 2 {
				b.WriteString(`<w:tr>`)
				for c := range 2 {
					fmt.Fprintf(&b, `<w:tc><w:tcPr><w:tcW w:w="4000" w:type="dxa"/></w:tcPr><w:p w14:paraId="%08X"><w:r><w:t>cell</w:t></w:r></w:p></w:tc>`, 0x10000000+i*4+r*2+c)
				}
				b.WriteString(`</w:tr>`)
			}
			b.WriteString(`</w:tbl>`)
		}
	}
	return b.String()
}

// benchSource returns the benchmark's input: DOCX_BENCH (a file) when
// set, else a generated document of 4,000 paragraphs (about 1.1 MB of
// document.xml), and a paragraph id in its middle.
func benchSource(b *testing.B) (path, mid string) {
	b.Helper()
	if f := os.Getenv("DOCX_BENCH"); f != "" {
		dst := filepath.Join(b.TempDir(), "in.docx")
		if err := laws.CopyFile(f, dst); err != nil {
			b.Fatal(err)
		}
		return dst, os.Getenv("DOCX_BENCH_PARA")
	}
	return writeDocx(b, largeBody(4000)), fmt.Sprintf("%08X", 2000)
}

// BenchmarkApplyLarge commits one operation to a large document, writing
// the output beside it: the whole pipeline of one `docx apply` (decode
// excluded): fold, gate, report, changed paragraphs and hazards.
func BenchmarkApplyLarge(b *testing.B) {
	path, mid := benchSource(b)
	out := filepath.Join(b.TempDir(), "out.docx")
	for _, c := range []struct {
		name string
		spec ops.Spec
	}{
		{"insert", &ops.InsertParagraph{At: ops.InsertAnchor(mid), Position: "after", HTML: ops.ParagraphContent{Items: []string{"<b>new</b> text"}}}},
		{"format", &ops.FormatParagraph{At: ops.ParagraphAddress(mid), Alignment: ops.Ptr("center")}},
		{"comment", &ops.CommentAdd{At: ops.ParagraphAddress(mid), Text: "a note"}},
		{"delete", &ops.DeleteParagraphs{At: []ops.ParagraphAddress{ops.ParagraphAddress(mid)}}},
		{"accept-all", &ops.AcceptRevisions{All: true}},
	} {
		for _, mode := range []string{"track", "direct"} {
			b.Run(c.name+"/"+mode, func(b *testing.B) {
				p, d := ops.ParsePlanJSON(planOf(b, path, mode, c.spec))
				if d != nil {
					b.Fatal(d.Message)
				}
				opt := Options{Commit: p.PreviewKey(), Out: out, Now: "2026-01-01T00:00:00Z", View: viewOpt}
				b.ReportAllocs()
				for b.Loop() {
					r, err := Run(path, p, opt)
					if err != nil || r.Outcome != Committed {
						b.Fatalf("%v %+v", err, r.Diagnostic)
					}
				}
			})
		}
	}
}
