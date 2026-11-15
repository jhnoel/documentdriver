package apply

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/jhnoel/documentdriver/internal/docx/model"
)

// TestN830205RejectAll: rejecting every revision of n830205.docx keeps
// the first paragraph's mark (its deletion is rejected: no join), leaves
// no revision, and restores the mark's formatting from the rPrChange
// snapshot (w:sz 24) without its w:del.
func TestN830205RejectAll(t *testing.T) {
	dir := os.Getenv("DOCX_CORPUS")
	if dir == "" {
		t.Skip("DOCX_CORPUS not set")
	}
	src, err := os.ReadFile(filepath.Join(dir, "ooxmlexport", "n830205.docx"))
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "in.docx")
	if err := os.WriteFile(path, src, 0o600); err != nil {
		t.Fatal(err)
	}
	in, err := model.OpenDocument(src)
	if err != nil {
		t.Fatal(err)
	}
	nIn := len(model.ParagraphsIn(in.XML.Body()))
	c := commit(t, path, planText(t, path, "track", "all", "reject"))
	if c.Outcome != Committed {
		t.Fatalf("%+v", c.Diagnostic)
	}
	out, _ := os.ReadFile(path) //nolint:errcheck,gosec // checked below
	d, err := model.OpenDocument(out)
	if err != nil {
		t.Fatal(err)
	}
	if n := len(d.XML.RevisionElements()); n != 0 {
		t.Errorf("%d revisions left", n)
	}
	ps := model.ParagraphsIn(d.XML.Body())
	t.Logf("paragraphs: input %d, output %d", nIn, len(ps))
	first := ps[0].Node
	if got := model.ParagraphText(first, model.All); got != "XXX" {
		t.Errorf("first paragraph text %q, want XXX (no join)", got)
	}
	rpr := model.ChildW(model.ChildW(first, "pPr"), "rPr")
	if rpr == nil || model.ChildW(rpr, "del") != nil || model.AttrOr(model.ChildW(rpr, "sz"), "val") != "24" {
		t.Errorf("mark rPr %s", rpr.Bytes())
	}
}
