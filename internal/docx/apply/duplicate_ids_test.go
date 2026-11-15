package apply

import (
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/jhnoel/documentdriver/internal/docx/math"
	"github.com/jhnoel/documentdriver/internal/docx/model"
	"github.com/jhnoel/documentdriver/internal/docx/read"
	"github.com/jhnoel/documentdriver/internal/laws"
)

// TestDuplicateParagraphIDs reads a document whose paragraphs share
// stored ids, takes every paragraph address the read shows (0000000A,
// 0000000A~2, ...), and formats each one: the address resolves to the
// paragraph read numbered so, and only that paragraph changes.
func TestDuplicateParagraphIDs(t *testing.T) {
	in := writeDocx(t, `<w:p w14:paraId="0000000A"><w:r><w:t>one</w:t></w:r></w:p>`+
		`<w:p w14:paraId="0000000A"><w:r><w:t>two</w:t></w:r></w:p>`+
		`<w:p w14:paraId="0000000B"><w:r><w:t>three</w:t></w:r></w:p>`+
		`<w:p w14:paraId="0000000A"><w:r><w:t>four</w:t></w:r></w:p>`)
	res, err := read.Read(mustRead(t, in), "document", "", read.Options{Equation: math.OMMLToLaTeX})
	if err != nil {
		t.Fatal(err)
	}
	s, err := read.Encode(res.Object(), false)
	if err != nil {
		t.Fatal(err)
	}
	var ids []string
	for _, m := range regexp.MustCompile(`<p id=\\"([^\\"]+)\\"`).FindAllStringSubmatch(s, -1) {
		ids = append(ids, m[1])
	}
	if got := strings.Join(ids, " "); got != "0000000A 0000000A~2 0000000B 0000000A~3" {
		t.Fatalf("read shows %s", got)
	}
	for k, id := range ids {
		out := filepath.Join(t.TempDir(), "out.docx")
		if err := runPlan(laws.Private(t, in), out, laws.Direct, `[{"op":"paragraph.format","at":"`+id+`","alignment":"center"}]`); err != nil {
			t.Errorf("%s: %v", id, err)
			continue
		}
		d, err := model.OpenDocument(mustRead(t, out))
		if err != nil {
			t.Fatal(err)
		}
		for i, p := range model.ParagraphsIn(d.XML.Body()) {
			centred := strings.Contains(string(p.Node.Bytes()), `<w:jc w:val="center"/>`)
			if centred != (i == k) {
				t.Errorf("%s: paragraph %d centred %v", id, i+1, centred)
			}
		}
	}
}
