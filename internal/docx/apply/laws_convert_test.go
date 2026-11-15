package apply

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/jhnoel/documentdriver/internal/docx/model"
	"github.com/jhnoel/documentdriver/internal/laws"
)

// convertSuite is revision.convert_to_formatting's change-mode law: it
// writes no revision, so both modes give one result.
func convertSuite(name, opsJSON string) *laws.ModeSuite {
	s := settleSuite("all", "accept")
	s.Name = "revision.convert_to_formatting " + name
	s.Class = laws.Same
	s.Project = projectNoSource
	s.Apply = func(src, dst, mode string) error { return runPlan(src, dst, mode, opsJSON) }
	return s
}

// firstAuthor is the author of the first revision the body lists, or "".
func firstAuthor(path string) string {
	b, err := os.ReadFile(path) //nolint:gosec // G304: a law input
	if err != nil {
		return ""
	}
	d, err := model.OpenDocument(b)
	if err != nil || d.XML.Body() == nil {
		return ""
	}
	for _, n := range d.XML.RevisionElements() {
		if a := model.AttrOr(n, "author"); a != "" && !bytes.ContainsAny([]byte(a), `"\`) {
			return a
		}
	}
	return ""
}

// TestConvertLaws runs revision.convert_to_formatting's laws on every law
// input: the change-mode law for every revision, one author's, the first
// revision and the first paragraph's; converting everything leaves no
// revision, so a second conversion, and settling either way, write
// nothing more.
func TestConvertLaws(t *testing.T) {
	eachInput(t, sectionLawInputs(t), func(t *testing.T, in string, _ int) {
		plans := map[string]string{"all": `[{"op":"revision.convert_to_formatting","all":true}]`}
		if id := firstID(in); id != "" {
			plans["one"] = `[{"op":"revision.convert_to_formatting","at":"revision:` + id + `"}]`
		}
		if a := firstAuthor(in); a != "" {
			plans["author"] = `[{"op":"revision.convert_to_formatting","all":true,"author":"` + a + `"}]`
		}
		if ids := bodyParagraphIDs(t, in); len(ids) > 0 {
			plans["paragraph"] = `[{"op":"revision.convert_to_formatting","at":["` + ids[0] + `"]}]`
		}
		for name, p := range plans {
			convertSuite(name, p).Check(t, in)
		}
		dir := t.TempDir()
		once, twice := filepath.Join(dir, "once.docx"), filepath.Join(dir, "twice.docx")
		err := runPlan(laws.Private(t, in), once, laws.Direct, plans["all"])
		var r refusal
		if errors.As(err, &r) {
			return
		}
		if err != nil {
			t.Errorf("%s: %v", filepath.Base(in), err)
			return
		}
		if err := runPlan(once, twice, laws.Direct, plans["all"]); err != nil {
			t.Errorf("%s: again: %v", filepath.Base(in), err)
			return
		}
		if a, b := mustRead(t, once), mustRead(t, twice); !bytes.Equal(a, b) {
			t.Errorf("%s: converting everything again wrote changes", filepath.Base(in))
		}
		p, err := projectNoSource(once)
		if err != nil {
			t.Fatal(err)
		}
		for _, accept := range []bool{true, false} {
			settled := filepath.Join(dir, "settled.docx")
			if err := settleFile(once, settled, accept); err != nil {
				t.Fatal(err)
			}
			if q, err := projectNoSource(settled); err != nil || q != p {
				t.Errorf("%s: settling (accept %v) a converted document changed it", filepath.Base(in), accept)
			}
		}
	})
}
