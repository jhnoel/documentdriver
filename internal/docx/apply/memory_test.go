package apply

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/jhnoel/documentdriver/internal/docx/create"
	"github.com/jhnoel/documentdriver/internal/docx/math"
	"github.com/jhnoel/documentdriver/internal/docx/model"
	"github.com/jhnoel/documentdriver/internal/docx/ops"
	"github.com/jhnoel/documentdriver/internal/docx/read"
	"github.com/jhnoel/documentdriver/internal/laws"
)

// memoryBudgets are the docx commands' memory budgets at
// laws.MemoryUnits paragraphs of scalingHTML: peak live heap and bytes
// allocated. They are the measured numbers with about twenty percent of
// margin; lower one when a change makes its command smaller, never raise
// one without a design note (docs/design.md, Scaling and memory budget).
var memoryBudgets = map[string]laws.Budget{
	"create":           {PeakLive: laws.MB(20), Alloc: laws.MB(474)},
	"read":             {PeakLive: laws.MB(14), Alloc: laws.MB(153)},
	"find":             {PeakLive: laws.MB(16), Alloc: laws.MB(31)},
	"paragraph.format": {PeakLive: laws.MB(29), Alloc: laws.MB(396)},
	"paragraph.insert": {PeakLive: laws.MB(29), Alloc: laws.MB(396)},
	"paragraph.delete": {PeakLive: laws.MB(29), Alloc: laws.MB(385)},
}

// TestMemoryBudget is the docx memory gate (make scaling): create, read,
// find and a one-paragraph apply of each ported paragraph operation on a
// created document of laws.MemoryUnits paragraphs stay within their
// budgets of peak live heap and allocation.
func TestMemoryBudget(t *testing.T) {
	in := scalingHTML(laws.MemoryUnits)
	laws.WithinMemory(t, "docx create", memoryBudgets["create"], func(mark func(string)) error {
		_, err := create.BuildAt(in, mark)
		return err
	})
	doc, err := create.Build(in)
	if err != nil {
		t.Fatal(err)
	}
	laws.WithinMemory(t, "docx read", memoryBudgets["read"], func(mark func(string)) error {
		_, err := read.Read(doc, "document", "", read.Options{Equation: math.OMMLToLaTeX, Checkpoint: mark})
		return err
	})
	laws.WithinMemory(t, "docx find", memoryBudgets["find"], func(mark func(string)) error {
		_, err := read.Find(doc, "ordinary text", false, "", read.Options{Equation: math.OMMLToLaTeX, Checkpoint: mark})
		return err
	})
	path := filepath.Join(t.TempDir(), "doc.docx")
	if err := os.WriteFile(path, doc, 0o600); err != nil {
		t.Fatal(err)
	}
	mid := middleParagraph(t, doc)
	for _, c := range []struct {
		name string
		spec ops.Spec
	}{
		{"paragraph.format", &ops.FormatParagraph{At: ops.ParagraphAddress(mid), Alignment: ops.Ptr("center")}},
		{"paragraph.insert", &ops.InsertParagraph{At: ops.InsertAnchor(mid), Position: "after", HTML: ops.ParagraphContent{Items: []string{"<b>new</b> text"}}}},
		{"paragraph.delete", &ops.DeleteParagraphs{At: []ops.ParagraphAddress{ops.ParagraphAddress(mid)}}},
	} {
		p, d := ops.ParsePlanJSON(planOf(t, path, "direct", c.spec))
		if d != nil {
			t.Fatalf("%s: %s", c.name, d.Message)
		}
		opt := Options{Commit: p.PreviewKey(), Out: filepath.Join(t.TempDir(), "out.docx"), Now: "2026-01-01T00:00:00Z", View: viewOpt}
		laws.WithinMemory(t, "docx apply "+c.name, memoryBudgets[c.name], func(mark func(string)) error {
			opt.Checkpoint = mark
			r, err := Run(path, p, opt)
			if err == nil && r.Outcome != Committed {
				err = fmt.Errorf("%s: %+v", r.Outcome, r.Diagnostic)
			}
			return err
		})
	}
}

// middleParagraph is the address of the body's middle paragraph.
func middleParagraph(t *testing.T, doc []byte) string {
	t.Helper()
	d, err := model.OpenDocument(doc)
	if err != nil {
		t.Fatal(err)
	}
	all := d.XML.AllParagraphs()
	return d.ParaIDs().IDs[all[len(all)/2]].ID
}
