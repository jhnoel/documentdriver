package read

import (
	"bytes"
	"strings"
	"testing"

	"github.com/jhnoel/documentdriver/internal/cli"
)

// TestDamagedPartsStayInTheirFacet: a malformed styles, numbering or
// comments part, or a media member whose stored bytes fail their
// checksum, disables only its own facet. The document read and find
// complete, and a note names the part; only a read of that part itself
// (the styles listing) fails.
func TestDamagedPartsStayInTheirFacet(t *testing.T) {
	body := `<w:p><w:pPr><w:numPr><w:ilvl w:val="0"/><w:numId w:val="1"/></w:numPr></w:pPr><w:r><w:t>item</w:t></w:r></w:p>`
	payload := "\x89PNG-payload-0123456789"
	b := buildDocx(t,
		part{"word/document.xml", docHead + body + docTail},
		part{"word/styles.xml", `<w:styles xmlns:w="` + nsW + `"><w:style>`},
		part{"word/numbering.xml", `<w:numbering xmlns:w="` + nsW + `"><w:num w:numId="1" w:numId="2"/></w:numbering>`},
		part{"word/media/a.png", payload},
	)
	// Corrupt the stored media bytes: the member's checksum now fails.
	i := bytes.Index(b, []byte(payload))
	if i < 0 {
		t.Fatal("payload not stored")
	}
	b[i+len(payload)-1] ^= 0xFF
	for _, c := range []struct {
		kind  string
		notes []string
	}{
		{"document", []string{`"at":"word/styles.xml"`, `"at":"word/numbering.xml"`}},
		{"assets", []string{`"at":"word/media/a.png","reason":"the part could not be read`}},
		{"revisions", nil},
	} {
		r, err := Read(b, c.kind, "", Options{})
		if err != nil {
			t.Fatal(err)
		}
		got := encodeResult(t, r)
		for _, n := range c.notes {
			if !strings.Contains(got, n) {
				t.Errorf("%s: no note %s in\n%s", c.kind, n, got)
			}
		}
		if c.notes == nil && !strings.Contains(got, `"notes":[]`) {
			t.Errorf("%s: notes for parts it never read:\n%s", c.kind, got)
		}
	}
	f, err := Find(b, "item", false, "markup", Options{})
	if err != nil || !strings.Contains(encodeResult(t, f), `"text":"item"`) {
		t.Errorf("find: %v", err)
	}
	// The styles listing targets the damaged part: it alone is rejected.
	_, err = Read(b, "styles", "", Options{})
	if e := refused(t, err); e.Code != cli.CodeUnreadableTarget || e.Path != "--kind" || !strings.HasPrefix(e.Detail, "styles.xml parse failed: ") {
		t.Errorf("styles: %+v", e)
	}
}
