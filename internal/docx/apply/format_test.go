package apply

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"strings"
	"testing"

	"github.com/jhnoel/documentdriver/internal/docx/model"
	"github.com/jhnoel/documentdriver/internal/laws"
)

// opsText is a plan of the given ops (a JSON array) in mode against the
// file at path.
func opsText(t *testing.T, path, mode, opsJSON string) []byte {
	t.Helper()
	b, err := os.ReadFile(path) //nolint:gosec // G304: the test's own copy
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(b)
	return []byte(`{"base":"sha256:` + hex.EncodeToString(sum[:]) + `","author":"Ann","change_mode":"` + mode + `","ops":` + opsJSON + `}`)
}

func mainXML(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path) //nolint:gosec // G304: the test's own copy
	if err != nil {
		t.Fatal(err)
	}
	return string(zipEntries(t, b)["word/document.xml"])
}

// TestFormatParagraph: track records a w:pPrChange with the prior base
// properties; direct changes the properties with no record; formatting
// to what is there writes nothing; a pending change is refused.
func TestFormatParagraph(t *testing.T) {
	body := `<w:p w14:paraId="0000000A"><w:pPr><w:jc w:val="left"/><w:rPr><w:b/></w:rPr></w:pPr><w:r><w:t>a</w:t></w:r></w:p>`
	op := `[{"op":"paragraph.format","at":"0000000A","alignment":"center","indent_left":36}]`
	path := writeDocx(t, body)
	c := commit(t, path, opsText(t, path, "track", op))
	if c.Outcome != Committed {
		t.Fatalf("%+v", c.Diagnostic)
	}
	got := mainXML(t, path)
	for _, want := range []string{`<w:ind w:left="720"/>`, `<w:jc w:val="center"/>`, `<w:pPrChange w:id="1" w:author="Ann" w:date="2026-01-01T00:00:00Z"><w:pPr><w:jc w:val="left"/></w:pPr></w:pPrChange>`} {
		if !strings.Contains(got, want) {
			t.Errorf("track: no %s in %s", want, got)
		}
	}
	if len(c.Changed) != 1 || c.Changed[0] != "0000000A" {
		t.Errorf("changed %v", c.Changed)
	}
	path = writeDocx(t, body)
	if c := commit(t, path, opsText(t, path, "direct", op)); c.Outcome != Committed {
		t.Fatalf("%+v", c.Diagnostic)
	}
	if got := mainXML(t, path); strings.Contains(got, "pPrChange") || !strings.Contains(got, `<w:jc w:val="center"/>`) {
		t.Errorf("direct: %s", got)
	}
	path = writeDocx(t, body)
	before, _ := os.ReadFile(path) //nolint:errcheck // compared below
	if c := commit(t, path, opsText(t, path, "track", `[{"op":"paragraph.format","at":"0000000A","alignment":"left"}]`)); c.Outcome != Committed || len(c.Parts) != 0 {
		t.Errorf("same value: %s %v", c.Outcome, c.Parts)
	}
	if after, _ := os.ReadFile(path); string(after) != string(before) { //nolint:errcheck // compared
		t.Error("formatting to the current value wrote the file")
	}
	path = writeDocx(t, `<w:p w14:paraId="0000000A"><w:pPr><w:jc w:val="left"/><w:pPrChange w:id="3" w:author="Bob"><w:pPr/></w:pPrChange></w:pPr></w:p>`)
	r, _ := runText(path, opsText(t, path, "direct", op), Options{})
	if r.Outcome != Rejected || r.Diagnostic.Path != "ops[0].at" {
		t.Errorf("pending change: %+v", r.Diagnostic)
	}
}

// formatSuite is paragraph.format's change-mode law on the first body
// paragraph with an address (AcceptAuthored).
func formatSuite(t *testing.T, in string) *laws.ModeSuite {
	b, err := os.ReadFile(in) //nolint:gosec // G304: a law input
	if err != nil {
		t.Fatal(err)
	}
	d, err := model.OpenDocument(b)
	if err != nil || d.XML.Body() == nil {
		return nil
	}
	ps := model.ParagraphsIn(d.XML.Body())
	if len(ps) == 0 {
		return nil
	}
	id := d.ParaIDs().IDs[ps[0].Node].ID
	op := `[{"op":"paragraph.format","at":"` + id + `","alignment":"right","space_after":6,"first_line_indent":-18}]`
	s := settleSuite("all", "accept")
	s.Name = "paragraph.format"
	s.Class = laws.AcceptAuthored
	s.Apply = func(src, dst, mode string) error { return runPlan(src, dst, mode, op) }
	return s
}

// TestFormatParagraphLaws runs the change-mode law on every law input.
func TestFormatParagraphLaws(t *testing.T) {
	eachInput(t, lawInputs(t), func(t *testing.T, in string, _ int) {
		if s := formatSuite(t, in); s != nil {
			s.Check(t, in)
		}
	})
}
