package apply

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jhnoel/documentdriver/internal/docx/ops"
	"github.com/jhnoel/documentdriver/internal/laws"
)

// siblingRuns is one paragraph of n runs, each a single "a" under a
// distinct font size, so no two runs ever merge into one.
func siblingRuns(n int) string {
	var b strings.Builder
	b.WriteString(`<w:p w14:paraId="00000001">`)
	for i := range n {
		fmt.Fprintf(&b, `<w:r><w:rPr><w:sz w:val="%d"/></w:rPr><w:t>a</w:t></w:r>`, 20+i)
	}
	b.WriteString("</w:p>")
	return b.String()
}

// siblingRevisions is one paragraph of n revision wrappers, alternating
// insertions and deletions of a single "a" (replacementGaps' shape:
// every converted wrapper meets the opposite kind beside it).
func siblingRevisions(n int) string {
	var b strings.Builder
	b.WriteString(`<w:p w14:paraId="00000001">`)
	for i := range n {
		if i%2 == 0 {
			fmt.Fprintf(&b, `<w:ins w:id="%d" w:author="Bob" w:date="2020-01-01T00:00:00Z"><w:r><w:t>a</w:t></w:r></w:ins>`, i+1)
		} else {
			fmt.Fprintf(&b, `<w:del w:id="%d" w:author="Bob" w:date="2020-01-01T00:00:00Z"><w:r><w:delText>a</w:delText></w:r></w:del>`, i+1)
		}
	}
	b.WriteString("</w:p>")
	return b.String()
}

// siblingCase runs one laws.Scales case over one paragraph of n units:
// body builds the paragraph, spec is the operation over it in track mode.
func siblingCase(t *testing.T, name string, body func(n int) string, spec func(n int) ops.Spec) {
	t.Helper()
	laws.Scales(t, name, func(n int) (func() error, error) {
		path := writeDocx(t, body(n))
		p, d := ops.ParsePlanJSON(planOf(t, path, "track", spec(n)))
		if d != nil {
			return nil, fmt.Errorf("%s", d.Message)
		}
		opt := Options{Commit: p.PreviewKey(), Out: filepath.Join(t.TempDir(), "out.docx"), Now: "2026-01-01T00:00:00Z", View: viewOpt}
		return func() error {
			r, err := Run(path, p, opt)
			if err == nil && r.Outcome != Committed {
				err = fmt.Errorf("%s: %+v", r.Outcome, r.Diagnostic)
			}
			return err
		}, nil
	})
}

// TestScalingSiblings is the scaling gate for the sibling walks (W1):
// text.replace in track mode over a paragraph of n runs (DeleteUnits
// asks adjacent per unit) and revision.convert_to_formatting over n
// alternating insertions and deletions (replacementGaps) each cost at
// most linearly more on four times the runs.
func TestScalingSiblings(t *testing.T) {
	siblingCase(t, "docx apply text.replace over n runs", siblingRuns, func(n int) ops.Spec {
		return &ops.ReplaceText{At: "00000001", Select: strings.Repeat("a", n), With: strings.Repeat("b", n)}
	})
	siblingCase(t, "docx apply revision.convert_to_formatting over n revisions", siblingRevisions, func(_ int) ops.Spec {
		return &ops.ConvertRevisionsToFormatting{All: true}
	})
}
