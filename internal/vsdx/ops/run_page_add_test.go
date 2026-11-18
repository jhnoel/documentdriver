package ops_test

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/jhnoel/documentdriver/internal/cli"
	"github.com/jhnoel/documentdriver/internal/laws"
	"github.com/jhnoel/documentdriver/internal/vsdx/apply"
	"github.com/jhnoel/documentdriver/internal/vsdx/edit"
	"github.com/jhnoel/documentdriver/internal/vsdx/ops"
	"github.com/jhnoel/documentdriver/internal/vsdx/read"
	"github.com/jhnoel/documentdriver/internal/vsdx/vsdxtest"
)

// TestPageAddTogetherParsePlan checks the CC13 rows at plan time: a
// page.add with at alone, or with position alone, is
// invalid_operation at ops[0] with the together message, while
// neither (append) and both (before or after) plans decode.
func TestPageAddTogetherParsePlan(t *testing.T) {
	const msg = "at and position go together: the page to add next to, and before or after"
	for _, tc := range []struct {
		text string
		want string // "" for a plan that decodes
	}{
		{`{"ops":[{"op":"page.add","name":"X","at":"page0"}]}`, msg},
		{`{"ops":[{"op":"page.add","name":"X","position":"before"}]}`, msg},
		{`{"ops":[{"op":"page.add","name":"X"}]}`, ""},
		{`{"ops":[{"op":"page.add","name":"X","at":"page0","position":"before"}]}`, ""},
		{`{"ops":[{"op":"page.add","name":"X","at":"page1","position":"after"}]}`, ""},
	} {
		p, errs := ops.ParsePlan([]byte(tc.text))
		if tc.want == "" {
			if len(errs) != 0 || p == nil || len(p.Ops) != 1 {
				t.Errorf("%s: plan %v errors %v, want one decoded op", tc.text, p, errs)
			}
			continue
		}
		if len(errs) != 1 {
			t.Fatalf("%s: errors %v, want one", tc.text, errs)
		}
		if errs[0].Code != cli.CodeInvalidOperation || errs[0].Path != "ops[0]" || errs[0].Message != tc.want {
			t.Errorf("%s: error = %s@%s %q, want invalid_operation@ops[0] %q", tc.text, errs[0].Code, errs[0].Path, errs[0].Message, tc.want)
		}
	}
}

// TestPageAddTogetherRun checks the CC13 rows through Run: a lone at
// or a lone position is invalid_operation, changes nothing, and a
// plan through ParsePlan is refused before any drawing is opened.
func TestPageAddTogetherRun(t *testing.T) {
	for _, tc := range []struct {
		name string
		spec *ops.PageAddSpec
	}{
		{name: "at alone", spec: &ops.PageAddSpec{Name: "X", At: "page0"}},
		{name: "position alone", spec: &ops.PageAddSpec{Name: "X", Position: "before"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := mustBegin(t, vsdxtest.Fixture(t, "v8-pages"))
			_, _, _, _, err := tc.spec.Run(w, map[string]string{}, "pt", nil)
			var ce *cli.Error
			if !errors.As(err, &ce) || ce.Code != cli.CodeInvalidOperation || ce.Path != "" {
				t.Fatalf("Run error = %v, want invalid_operation at the operation", err)
			}
			if ce.Message != "at and position go together: the page to add next to, and before or after" {
				t.Errorf("message = %q, want the together message", ce.Message)
			}
			if err := w.Verify(); err != nil {
				t.Fatalf("Verify after refusal: %v", err)
			}
			if got := summaryIDs(readSummary(t, w)); !equalIDs(got, []string{"page0", "page1", "page3"}) {
				t.Errorf("summary order after refusal = %v, want unchanged [page0 page1 page3]", got)
			}
		})
	}
}

// TestPageAddV8BeforeFirst checks the CC13 row: V8 page.add X at
// page0 position before puts X first in the summary.
func TestPageAddV8BeforeFirst(t *testing.T) {
	w := mustBegin(t, vsdxtest.Fixture(t, "v8-pages"))
	addr, changed, _, _, err := (&ops.PageAddSpec{Name: "X", At: "page0", Position: "before"}).Run(w, map[string]string{}, "pt", nil)
	if err != nil {
		t.Fatalf("page.add X before page0: %v", err)
	}
	if addr != "page4" || len(changed) != 1 || changed[0] != "page4" {
		t.Errorf("page.add = (%q, %v), want (page4, [page4])", addr, changed)
	}
	if err := w.Verify(); err != nil {
		t.Fatalf("Verify: %v", err)
	}
	pages := readSummary(t, w)
	if got := summaryIDs(pages); !equalIDs(got, []string{"page4", "page0", "page1", "page3"}) {
		t.Errorf("summary order = %v, want [page4 page0 page1 page3]", got)
	}
	if len(pages) == 0 || pages[0].Name != "X" {
		t.Errorf("first summary page = %+v, want X first", pages)
	}
}

// TestPageAddV8AfterPage1 checks the CC13 row: V8 page.add X at
// page1 position after puts X after page1.
func TestPageAddV8AfterPage1(t *testing.T) {
	w := mustBegin(t, vsdxtest.Fixture(t, "v8-pages"))
	addr, changed, _, _, err := (&ops.PageAddSpec{Name: "X", At: "page1", Position: "after"}).Run(w, map[string]string{}, "pt", nil)
	if err != nil {
		t.Fatalf("page.add X after page1: %v", err)
	}
	if addr != "page4" || len(changed) != 1 || changed[0] != "page4" {
		t.Errorf("page.add = (%q, %v), want (page4, [page4])", addr, changed)
	}
	if err := w.Verify(); err != nil {
		t.Fatalf("Verify: %v", err)
	}
	pages := readSummary(t, w)
	if got := summaryIDs(pages); !equalIDs(got, []string{"page0", "page1", "page4", "page3"}) {
		t.Errorf("summary order = %v, want [page0 page1 page4 page3]", got)
	}
	if len(pages) != 4 || pages[2].Name != "X" {
		t.Errorf("third summary page = %+v, want X after page1", pages)
	}
}

// TestPageAddApplyBeforeFirst checks the CC13 row through apply:
// V8 page.add X at page0 position before reads back with X first.
func TestPageAddApplyBeforeFirst(t *testing.T) {
	src := vsdxtest.Fixture(t, "v8-pages")
	p, errs := ops.ParsePlan([]byte(`{"ops":[{"op":"page.add","name":"X","at":"page0","position":"before"}]}`))
	if len(errs) != 0 || p == nil {
		t.Fatalf("ParsePlan: %v errors %v", p, errs)
	}
	out := filepath.Join(t.TempDir(), "out.vsdx")
	if _, err := apply.Run(src, p, apply.Options{Out: out}); err != nil {
		t.Fatalf("apply.Run: %v", err)
	}
	data, err := os.ReadFile(out)
	if err != nil {
		t.Fatalf("read out: %v", err)
	}
	sum, err := read.Document(data, "sha256:"+strings.Repeat("0", 64))
	if err != nil {
		t.Fatalf("read.Document: %v", err)
	}
	var got []string
	for _, pg := range sum.Pages {
		got = append(got, pg.ID)
	}
	if !equalIDs(got, []string{"page4", "page0", "page1", "page3"}) {
		t.Errorf("applied summary order = %v, want [page4 page0 page1 page3]", got)
	}
	if len(sum.Pages) == 0 || sum.Pages[0].Name != "X" {
		t.Errorf("first applied summary page = %+v, want X first", sum.Pages)
	}
}

// TestScalingPageAdd checks the scaling row: page.add before the
// first page on a one-page drawing whose page holds N and 4N
// shapes (N = 1000, 4N = 4000 shapes) allocates at most 5.5 times
// as much (laws.Scales) and the laws.TimeScales time ratio is at
// most 8. Page.add touches only pages.xml, never a page's
// contents, so its cost must not grow with the shapes on the
// page. Every measured run adds before the current first page,
// so every run takes the append plus reorder path. Scaling in
// the number of pages is covered where it is measured without
// reopening the drawing: TestScalingPages (edit AddPage) and
// TestScalingPageReorder (page.reorder).
func TestScalingPageAdd(t *testing.T) {
	setup := func(n int) (func() error, error) {
		path := vsdxtest.Generated(t, n)
		w, err := edit.Begin(path)
		if err != nil {
			return nil, err
		}
		t.Cleanup(func() { w.Tx().Rollback() })
		count := 0
		first := "page0"
		return func() error {
			count++
			name := "Extra" + strconv.Itoa(count)
			addr, changed, _, _, err := (&ops.PageAddSpec{Name: name, At: first, Position: "before"}).Run(w, map[string]string{}, "pt", nil)
			if err != nil {
				return err
			}
			if len(changed) != 1 || changed[0] != addr {
				return fmt.Errorf("changed = %v, want [%s]", changed, addr)
			}
			first = addr
			return w.Verify()
		}, nil
	}
	laws.Scales(t, "vsdx page.add shapes", setup)
	laws.TimeScales(t, "vsdx page.add shapes", setup)
}
