package apply

import (
	"testing"

	"github.com/jhnoel/documentdriver/internal/docx/ops"
)

// TestEditCreatedParagraph: an operation may edit a paragraph an earlier
// operation of the plan created (paragraph.insert as $p, then
// paragraph.format of $p), in both modes, and the change-mode law holds.
func TestEditCreatedParagraph(t *testing.T) {
	for _, mode := range []string{"track", "direct"} {
		path := writeDocx(t, `<w:p w14:paraId="0000000A"><w:r><w:t>a</w:t></w:r></w:p>`)
		text := planOf(t, path, mode,
			&ops.InsertParagraph{At: ops.InsertAnchor("0000000A"), Position: "after", HTML: ops.ParagraphContent{Items: []string{"new"}}, Alias: ops.Ptr("p")},
			&ops.FormatParagraph{At: ops.ParagraphAddress("$p"), Alignment: ops.Ptr("center")})
		r := commit(t, path, text)
		if r.Outcome != Committed {
			t.Fatalf("%s: %s %+v", mode, r.Outcome, r.Diagnostic)
		}
	}
}
