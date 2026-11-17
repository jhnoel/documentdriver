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
	"github.com/jhnoel/documentdriver/internal/vsdx/emit"
	"github.com/jhnoel/documentdriver/internal/vsdx/ops"
	"github.com/jhnoel/documentdriver/internal/vsdx/read"
	"github.com/jhnoel/documentdriver/internal/vsdx/vsd"
	"github.com/jhnoel/documentdriver/internal/vsdx/vsdxtest"
)

// summaryIDs returns the page IDs of pages in summary order.
func summaryIDs(pages []summaryPage) []string {
	ids := make([]string, 0, len(pages))
	for _, p := range pages {
		ids = append(ids, p.ID)
	}
	return ids
}

// checkOnlyPagesChanged snapshots w and fails unless every part
// but pages.xml is byte for byte the source's: IDs and parts are
// unchanged by a reorder.
func checkOnlyPagesChanged(t *testing.T, w *edit.Workspace, src string) {
	t.Helper()
	snap := filepath.Join(t.TempDir(), "snap.vsdx")
	if err := w.Tx().Snapshot(snap); err != nil {
		t.Fatalf("Snapshot: %v", err)
	}
	if err := laws.SameParts(src, snap, "/visio/pages/pages.xml"); err != nil {
		t.Errorf("parts other than pages.xml changed: %v", err)
	}
}

// equalIDs reports whether got holds want in order.
func equalIDs(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}

// TestPageReorderV8Page3BeforePage0 checks the PGR row: V8 page3
// before page0 gives summary order page3, page0, page1.
func TestPageReorderV8Page3BeforePage0(t *testing.T) {
	src := vsdxtest.Fixture(t, "v8-pages")
	w := mustBegin(t, src)
	as, changed, _, _, err := (&ops.PageReorderSpec{At: "page3", To: "page0", Position: "before"}).Run(w, map[string]string{}, "pt", nil)
	if err != nil {
		t.Fatalf("page.reorder page3 before page0: %v", err)
	}
	if as != "" || len(changed) != 1 || changed[0] != "page3" {
		t.Errorf("Run = (%q, %v), want changed [page3]", as, changed)
	}
	if err := w.Verify(); err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if got := summaryIDs(readSummary(t, w)); !equalIDs(got, []string{"page3", "page0", "page1"}) {
		t.Errorf("summary order = %v, want [page3 page0 page1]", got)
	}
	checkOnlyPagesChanged(t, w, src)
}

// TestPageReorderV8Page0AfterPage1 checks the PGR row: V8 page0
// after page1 gives summary order page1, page0, page3.
func TestPageReorderV8Page0AfterPage1(t *testing.T) {
	src := vsdxtest.Fixture(t, "v8-pages")
	w := mustBegin(t, src)
	_, changed, _, _, err := (&ops.PageReorderSpec{At: "page0", To: "page1", Position: "after"}).Run(w, map[string]string{}, "pt", nil)
	if err != nil {
		t.Fatalf("page.reorder page0 after page1: %v", err)
	}
	if len(changed) != 1 || changed[0] != "page0" {
		t.Errorf("changed = %v, want [page0]", changed)
	}
	if err := w.Verify(); err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if got := summaryIDs(readSummary(t, w)); !equalIDs(got, []string{"page1", "page0", "page3"}) {
		t.Errorf("summary order = %v, want [page1 page0 page3]", got)
	}
	checkOnlyPagesChanged(t, w, src)
}

// TestPageReorderSequences checks two reorders in one workspace:
// V8 page3 before page0, then page1 after page3, gives
// page3, page1, page0.
func TestPageReorderSequences(t *testing.T) {
	w := mustBegin(t, vsdxtest.Fixture(t, "v8-pages"))
	if _, _, _, _, err := (&ops.PageReorderSpec{At: "page3", To: "page0", Position: "before"}).Run(w, map[string]string{}, "pt", nil); err != nil {
		t.Fatalf("first reorder: %v", err)
	}
	if _, _, _, _, err := (&ops.PageReorderSpec{At: "page1", To: "page3", Position: "after"}).Run(w, map[string]string{}, "pt", nil); err != nil {
		t.Fatalf("second reorder: %v", err)
	}
	if err := w.Verify(); err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if got := summaryIDs(readSummary(t, w)); !equalIDs(got, []string{"page3", "page1", "page0"}) {
		t.Errorf("summary order = %v, want [page3 page1 page0]", got)
	}
}

// TestPageReorderApply checks page.reorder through apply: V8
// page3 before page0 reads back as page3, page0, page1.
func TestPageReorderApply(t *testing.T) {
	src := vsdxtest.Fixture(t, "v8-pages")
	p, errs := ops.ParsePlan([]byte(`{"ops":[{"op":"page.reorder","at":"page3","to":"page0","position":"before"}]}`))
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
	if !equalIDs(got, []string{"page3", "page0", "page1"}) {
		t.Errorf("applied summary order = %v, want [page3 page0 page1]", got)
	}
}

// TestPageReorderToItself checks the PGR row: a page to itself is
// no change and nothing is written.
func TestPageReorderToItself(t *testing.T) {
	for _, position := range []string{"before", "after"} {
		src := vsdxtest.Fixture(t, "v8-pages")
		w := mustBegin(t, src)
		_, changed, _, _, err := (&ops.PageReorderSpec{At: "page0", To: "page0", Position: position}).Run(w, map[string]string{}, "pt", nil)
		if err != nil {
			t.Fatalf("page.reorder page0 to itself %s: %v", position, err)
		}
		if len(changed) != 0 {
			t.Errorf("page0 to itself %s: changed = %v, want none", position, changed)
		}
		if err := w.Verify(); err != nil {
			t.Fatalf("Verify: %v", err)
		}
		if got := summaryIDs(readSummary(t, w)); !equalIDs(got, []string{"page0", "page1", "page3"}) {
			t.Errorf("page0 to itself %s: summary order = %v, want unchanged [page0 page1 page3]", position, got)
		}
		snap := filepath.Join(t.TempDir(), "snap.vsdx")
		if err := w.Tx().Snapshot(snap); err != nil {
			t.Fatalf("Snapshot: %v", err)
		}
		if err := laws.SameParts(src, snap, ""); err != nil {
			t.Errorf("page0 to itself %s: parts changed: %v", position, err)
		}
	}
}

// TestPageReorderAlreadyThere checks that a move to where the page
// already stands changes nothing: page0 before page1 on V8.
func TestPageReorderAlreadyThere(t *testing.T) {
	src := vsdxtest.Fixture(t, "v8-pages")
	w := mustBegin(t, src)
	_, changed, _, _, err := (&ops.PageReorderSpec{At: "page0", To: "page1", Position: "before"}).Run(w, map[string]string{}, "pt", nil)
	if err != nil {
		t.Fatalf("page.reorder page0 before page1: %v", err)
	}
	if len(changed) != 0 {
		t.Errorf("changed = %v, want none (page0 already before page1)", changed)
	}
	if err := w.Verify(); err != nil {
		t.Fatalf("Verify: %v", err)
	}
	snap := filepath.Join(t.TempDir(), "snap.vsdx")
	if err := w.Tx().Snapshot(snap); err != nil {
		t.Fatalf("Snapshot: %v", err)
	}
	if err := laws.SameParts(src, snap, ""); err != nil {
		t.Errorf("already-there move changed parts: %v", err)
	}
}

// TestPageReorderAlias checks that at and to resolve $aliases.
func TestPageReorderAlias(t *testing.T) {
	w := mustBegin(t, vsdxtest.Fixture(t, "v8-pages"))
	aliases := map[string]string{"bg": "page3", "first": "page0"}
	_, changed, _, _, err := (&ops.PageReorderSpec{At: "$bg", To: "$first", Position: "before"}).Run(w, aliases, "pt", nil)
	if err != nil {
		t.Fatalf("page.reorder with aliases: %v", err)
	}
	if len(changed) != 1 || changed[0] != "page3" {
		t.Errorf("changed = %v, want [page3]", changed)
	}
	if err := w.Verify(); err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if got := summaryIDs(readSummary(t, w)); !equalIDs(got, []string{"page3", "page0", "page1"}) {
		t.Errorf("summary order = %v, want [page3 page0 page1]", got)
	}
}

// TestPageReorderErrors checks the refusals: a missing at is
// not_found at at, and a missing to is not_found at to.
func TestPageReorderErrors(t *testing.T) {
	cases := []struct {
		spec  *ops.PageReorderSpec
		field string
	}{
		{&ops.PageReorderSpec{At: "page99", To: "page0", Position: "before"}, "at"},
		{&ops.PageReorderSpec{At: "page0", To: "page99", Position: "after"}, "to"},
		{&ops.PageReorderSpec{At: "page99", To: "page99", Position: "before"}, "at"},
	}
	for _, tc := range cases {
		w := mustBegin(t, vsdxtest.Fixture(t, "v8-pages"))
		_, _, _, _, err := tc.spec.Run(w, map[string]string{}, "pt", nil)
		var ce *cli.Error
		if !errors.As(err, &ce) || ce.Code != cli.CodeNotFound || ce.Path != tc.field {
			t.Errorf("Run(%+v) error = %v, want not_found at %s", *tc.spec, err, tc.field)
		}
		if err := w.Verify(); err != nil {
			t.Fatalf("Verify after refusal: %v", err)
		}
	}
}

// TestScalingPageReorder checks the scaling row: page.reorder on a
// drawing of N and 4N pages (N = 1000, 4N = 4000 pages) allocates
// at most 5.5 times as much (laws.Scales) and the laws.TimeScales
// time ratio is at most 8. The measured run holds one workspace
// per size (setup builds it) and moves the last page before and
// after page0 in turn, so every run moves: Begin's own cost is
// not measured, as Begin on many pages is doc.Open's, not this
// chunk's.
func TestScalingPageReorder(t *testing.T) {
	setup := func(n int) (func() error, error) {
		src := vsdxtest.Blank(t)
		w, err := edit.Begin(src)
		if err != nil {
			return nil, err
		}
		for i := 1; i < n; i++ {
			name := "P" + strconv.Itoa(i)
			if _, err := w.AddPage(func(relID string) ([]byte, error) {
				return emit.PageXML(emit.Page{Name: name, Width: 8.5, Height: 11, RelID: relID})
			}, ""); err != nil {
				w.Tx().Rollback()
				return nil, err
			}
		}
		snap := filepath.Join(t.TempDir(), fmt.Sprintf("pages-%d.vsdx", n))
		if err := w.Tx().Snapshot(snap); err != nil {
			w.Tx().Rollback()
			return nil, err
		}
		w.Tx().Rollback()
		w, err = edit.Begin(snap)
		if err != nil {
			return nil, err
		}
		t.Cleanup(func() { w.Tx().Rollback() })
		last := vsd.PageID(uint32(n-1), 1)
		count := 0
		return func() error {
			count++
			position := "before"
			if count%2 == 0 {
				position = "after"
			}
			_, changed, _, _, err := (&ops.PageReorderSpec{At: last, To: "page0", Position: position}).Run(w, map[string]string{}, "pt", nil)
			if err != nil {
				return err
			}
			if len(changed) != 1 || changed[0] != last {
				return fmt.Errorf("changed = %v, want [%s]", changed, last)
			}
			return w.Verify()
		}, nil
	}
	laws.Scales(t, "vsdx page.reorder pages", setup)
	laws.TimeScales(t, "vsdx page.reorder pages", setup)
}
