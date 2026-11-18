package ops_test

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/jhnoel/documentdriver/internal/cli"
	"github.com/jhnoel/documentdriver/internal/plan"
	"github.com/jhnoel/documentdriver/internal/vsdx/apply"
	"github.com/jhnoel/documentdriver/internal/vsdx/edit"
	"github.com/jhnoel/documentdriver/internal/vsdx/ops"
	"github.com/jhnoel/documentdriver/internal/vsdx/vsd"
	"github.com/jhnoel/documentdriver/internal/vsdx/vsdxtest"
)

// TestPageSetOffPageApply checks the OPF apply-level row: page.set
// orientation landscape on V1 gives an off-page hazard for page0/s1
// with new true and detail "box [108,-54,72,36] lies outside the
// page [0,0,792,612]".
func TestPageSetOffPageApply(t *testing.T) {
	src := vsdxtest.Fixture(t, "v1-minimal")
	out := filepath.Join(t.TempDir(), "out.vsdx")
	res, err := apply.Run(src, parsePlan(t, `{"ops":[{"op":"page.set","at":"page0","orientation":"landscape"}]}`), apply.Options{Out: out})
	if err != nil {
		t.Fatalf("apply.Run: %v", err)
	}
	want := plan.Hazard{Kind: "off-page", ID: "page0/s1", Detail: "box [108,-54,72,36] lies outside the page [0,0,792,612]", New: true}
	if !slices.Contains(res.Hazards, want) {
		t.Errorf("hazards = %+v, want them to include %+v", res.Hazards, want)
	}
}

// TestPageSetFormulaDetail checks the CC10 detail form: a PageWidth
// outside the allow list is operation_failed at size with detail.
func TestPageSetFormulaDetail(t *testing.T) {
	src := vsdxtest.Fixture(t, "v1-minimal")
	w, err := edit.Begin(src)
	if err != nil {
		t.Fatalf("Begin: %v", err)
	}
	defer w.Tx().Rollback()
	if err := w.SetCells("page0", []edit.Cell{{N: vsd.CellPageWidth, V: "8.5", F: "User.DefaultWidth"}}); err != nil {
		t.Fatalf("SetCells: %v", err)
	}
	_, _, _, _, err = (&ops.PageSetSpec{At: "page0", Size: []float64{792, 612}}).Run(w, map[string]string{}, "pt", nil)
	var ce *cli.Error
	if !errors.As(err, &ce) || ce.Code != cli.CodeOperationFailed || ce.Path != "size" {
		t.Fatalf("page.set: got %v, want operation_failed at size", err)
	}
	if !strings.Contains(ce.Message, "PageWidth") || !strings.Contains(ce.Message, "is bound to a formula vsdx does not evaluate") {
		t.Errorf("message = %q, want it to name PageWidth in the formula refusal form", ce.Message)
	}
	if ce.Detail != "PageWidth F=User.DefaultWidth" {
		t.Errorf("detail = %q, want PageWidth F=User.DefaultWidth", ce.Detail)
	}
}

// corpusTest6Path returns the path of dave-howard-vsdx
// tests/test6_shape_properties.vsdx under VSDX_CORPUS (a vsdx
// directory holding repos/<source>/, a worktree root holding
// corpus/vsdx/repos, or a repos directory directly). It skips when
// VSDX_CORPUS is unset and fails naming the path when VSDX_CORPUS is
// set but the file is missing.
func corpusTest6Path(t *testing.T) string {
	t.Helper()
	dir := os.Getenv("VSDX_CORPUS")
	if dir == "" {
		t.Skip("no vsdx corpus (set VSDX_CORPUS)")
	}
	rel := filepath.Join("dave-howard-vsdx", "tests", "test6_shape_properties.vsdx")
	for _, cand := range []string{
		filepath.Join(dir, "repos", rel),
		filepath.Join(dir, "corpus", "vsdx", "repos", rel),
		filepath.Join(dir, rel),
	} {
		if _, err := os.Stat(cand); err == nil {
			return cand
		}
	}
	t.Fatalf("corpus file %s not found under VSDX_CORPUS %s", filepath.Join(dir, "repos", rel), dir)
	return ""
}

// TestPageSetOffPageCorpus checks the OPF corpus row:
// dave-howard-vsdx tests/test6_shape_properties.vsdx, page.set
// orientation landscape on page0, gives one new off-page hazard per
// shape the review saw at y -228: page0/s1, page0/s2 and page0/s5,
// each with detail naming its box at y -228.61 on the page
// [0,0,841.89,595.28]. It skips without VSDX_CORPUS and fails when
// VSDX_CORPUS is set but the file is missing.
func TestPageSetOffPageCorpus(t *testing.T) {
	src := corpusTest6Path(t)
	out := filepath.Join(t.TempDir(), "out.vsdx")
	res, err := apply.Run(src, parsePlan(t, `{"ops":[{"op":"page.set","at":"page0","orientation":"landscape"}]}`), apply.Options{Out: out})
	if err != nil {
		t.Fatalf("apply.Run: %v", err)
	}
	want := []plan.Hazard{
		{Kind: "off-page", ID: "page0/s1", Detail: "box [18,-228.61,155.91,113.39] lies outside the page [0,0,841.89,595.28]", New: true},
		{Kind: "off-page", ID: "page0/s2", Detail: "box [219.69,-228.61,155.91,113.39] lies outside the page [0,0,841.89,595.28]", New: true},
		{Kind: "off-page", ID: "page0/s5", Detail: "box [421.37,-228.61,155.91,113.39] lies outside the page [0,0,841.89,595.28]", New: true},
	}
	var got []plan.Hazard
	for _, h := range res.Hazards {
		if h.Kind == "off-page" {
			got = append(got, h)
		}
	}
	if !slices.Equal(got, want) {
		t.Errorf("off-page hazards = %+v, want %+v", got, want)
	}
	for _, h := range got {
		if !strings.Contains(h.Detail, "-228.61") {
			t.Errorf("off-page detail = %q, want y -228", h.Detail)
		}
	}
}
