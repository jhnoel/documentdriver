package apply

import (
	"os"
	"strings"
	"testing"

	"github.com/jhnoel/documentdriver/internal/docx/integrity"
	"github.com/jhnoel/documentdriver/internal/docx/ops"
)

// TestRebuiltPropertiesKeepTheirFaults: the revision core rebuilds
// properties from their bytes (a property change's snapshot, a short
// row's w:trPr); the rebuilt elements are recorded as copies, so a fault
// the original already had never blocks the save. TestGateRefusesAtTheOp
// is the other side: the same fault written fresh is refused.
func TestRebuiltPropertiesKeepTheirFaults(t *testing.T) {
	t.Run("track paragraph.format snapshots a faulty pPr", func(t *testing.T) {
		path := writeDocx(t, `<w:p w14:paraId="0000000A"><w:pPr><w:ind w:left="abc"/></w:pPr><w:r><w:t>a</w:t></w:r></w:p>`)
		mustFault(t, path, "ind")
		c := commit(t, path, planOf(t, path, "track", &ops.FormatParagraph{At: "0000000A", Alignment: ops.Ptr("right")}))
		if c.Outcome != Committed {
			t.Fatalf("%s %+v", c.Outcome, c.Diagnostic)
		}
		mustFault(t, path, "ind")
		if !strings.Contains(documentXML(t, path), "pPrChange") {
			t.Error("no property change was recorded")
		}
	})
	t.Run("track paragraph.delete adopts a faulty pPr", func(t *testing.T) {
		path := writeDocx(t, `<w:p w14:paraId="0000000A"><w:pPr><w:ind w:left="abc"/></w:pPr><w:r><w:t>a</w:t></w:r></w:p>`+
			`<w:p w14:paraId="0000000B"><w:pPr><w:jc w:val="center"/></w:pPr><w:r><w:t>b</w:t></w:r></w:p>`)
		c := commit(t, path, planOf(t, path, "track", &ops.DeleteParagraphs{At: []ops.ParagraphAddress{"0000000B"}}))
		if c.Outcome != Committed {
			t.Fatalf("%s %+v", c.Outcome, c.Diagnostic)
		}
		if strings.Count(documentXML(t, path), `w:left="abc"`) != 2 {
			t.Error("the last paragraph did not adopt the previous one's properties")
		}
	})
	t.Run("settle rebuilds the trPr of a row with a fault", func(t *testing.T) {
		path := writeDocx(t, `<w:tbl><w:tblPr/><w:tblGrid><w:gridCol w:w="100"/><w:gridCol w:w="100"/></w:tblGrid>`+
			`<w:tr><w:trPr><w:trHeight w:val="abc"/></w:trPr>`+
			`<w:tc><w:p w14:paraId="0000000A"/></w:tc>`+
			`<w:tc><w:tcPr><w:cellDel w:id="1" w:author="Bob" w:date="2020-01-01T00:00:00Z"/></w:tcPr><w:p w14:paraId="0000000B"/></w:tc>`+
			`</w:tr></w:tbl><w:p w14:paraId="0000000C"/>`)
		mustFault(t, path, "trHeight")
		c := commit(t, path, planText(t, path, "track", "all", "accept"))
		if c.Outcome != Committed {
			t.Fatalf("%s %+v", c.Outcome, c.Diagnostic)
		}
		mustFault(t, path, "trHeight")
		if !strings.Contains(documentXML(t, path), "gridAfter") {
			t.Error("the short row was not padded")
		}
	})
}

// mustFault returns the integrity findings of the file at path and fails
// unless one names element local.
func mustFault(t *testing.T, path, local string) string {
	t.Helper()
	b, err := os.ReadFile(path) //nolint:gosec // G304: the test's own copy
	if err != nil {
		t.Fatal(err)
	}
	var all []string
	for _, k := range integrity.Check(b).Kinds() {
		all = append(all, k.Message)
	}
	s := strings.Join(all, "\n")
	if !strings.Contains(s, local) {
		t.Fatalf("no finding on w:%s: %s", local, s)
	}
	return s
}

// documentXML returns the main document part of the file at path.
func documentXML(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path) //nolint:gosec // G304: the test's own copy
	if err != nil {
		t.Fatal(err)
	}
	return string(zipEntries(t, b)["word/document.xml"])
}
