package ops_test

import (
	"errors"
	"fmt"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/jhnoel/documentdriver/internal/cli"
	"github.com/jhnoel/documentdriver/internal/laws"
	"github.com/jhnoel/documentdriver/internal/opc"
	"github.com/jhnoel/documentdriver/internal/vsdx/edit"
	"github.com/jhnoel/documentdriver/internal/vsdx/emit"
	"github.com/jhnoel/documentdriver/internal/vsdx/integrity"
	"github.com/jhnoel/documentdriver/internal/vsdx/ops"
	"github.com/jhnoel/documentdriver/internal/vsdx/vsd"
	"github.com/jhnoel/documentdriver/internal/vsdx/vsdxtest"
)

// TestPageDeleteV8Page1 checks the PGD row: V8 page1 deletes to
// summary pages page0, page3; part, override and rel gone, integrity
// finds nothing; deleted [page1].
func TestPageDeleteV8Page1(t *testing.T) {
	w := mustBegin(t, vsdxtest.Fixture(t, "v8-pages"))
	as, changed, deleted, _, err := (&ops.PageDeleteSpec{At: "page1"}).Run(w, map[string]string{}, "pt", nil)
	if err != nil {
		t.Fatalf("page.delete page1: %v", err)
	}
	if as != "" {
		t.Errorf("as = %q, want empty", as)
	}
	if len(changed) != 0 {
		t.Errorf("changed = %v, want none", changed)
	}
	if len(deleted) != 1 || deleted[0] != "page1" {
		t.Errorf("deleted = %v, want [page1]", deleted)
	}
	if err := w.Verify(); err != nil {
		t.Fatalf("Verify: %v", err)
	}
	pages := readSummary(t, w)
	if len(pages) != 2 || pages[0].ID != "page0" || pages[1].ID != "page3" {
		t.Errorf("summary pages = %+v, want page0, page3", pages)
	}
	if w.Package().Has("/visio/pages/page2.xml") {
		t.Error("/visio/pages/page2.xml still present after deleting page1")
	}
	pt, err := w.Package().Part("/[Content_Types].xml")
	if err != nil {
		t.Fatalf("Part(content types): %v", err)
	}
	ct, err := pt.Bytes()
	if err != nil {
		t.Fatalf("Bytes(content types): %v", err)
	}
	if got := strings.Count(string(ct), "/visio/pages/page2.xml"); got != 0 {
		t.Errorf("content types hold the page2.xml Override %d times, want 0", got)
	}
	rels, err := w.Package().Rels("/visio/pages/pages.xml")
	if err != nil {
		t.Fatalf("Rels(pages.xml): %v", err)
	}
	if _, ok := rels.Get("rId2"); ok {
		t.Error("rId2 still present in pages.xml.rels after deleting page1")
	}
	if len(rels.All()) != 2 {
		t.Errorf("pages.xml.rels holds %d relationships, want 2", len(rels.All()))
	}
	// Integrity runs on the saved output: doc.Open reads original
	// bytes, so a working copy would still show the deleted page.
	snap := filepath.Join(t.TempDir(), "snap.vsdx")
	if err := w.Tx().Snapshot(snap); err != nil {
		t.Fatalf("Snapshot: %v", err)
	}
	pkg, err := opc.Open(snap)
	if err != nil {
		t.Fatalf("Open(snapshot): %v", err)
	}
	defer func() {
		if err := pkg.Close(); err != nil {
			t.Errorf("close snapshot: %v", err)
		}
	}()
	findings, err := integrity.Check(pkg)
	if err != nil {
		t.Fatalf("integrity.Check: %v", err)
	}
	if len(findings) != 0 {
		t.Errorf("integrity findings = %v, want none after deleting page1", findings)
	}
}

// TestPageDeleteV8Background checks the PGD row: V8 page3 (background
// of page0) refuses with operation_failed naming page0.
func TestPageDeleteV8Background(t *testing.T) {
	w := mustBegin(t, vsdxtest.Fixture(t, "v8-pages"))
	_, _, _, _, err := (&ops.PageDeleteSpec{At: "page3"}).Run(w, map[string]string{}, "pt", nil)
	var ce *cli.Error
	if !errors.As(err, &ce) || ce.Code != cli.CodeOperationFailed {
		t.Fatalf("page.delete page3: %v, want operation_failed", err)
	}
	if ce.Path != "at" {
		t.Errorf("path = %q, want at", ce.Path)
	}
	if !strings.Contains(ce.Message, "page0") {
		t.Errorf("message = %q, want it to name page0", ce.Message)
	}
	if err := w.Verify(); err != nil {
		t.Fatalf("Verify after refusal: %v", err)
	}
}

// TestPageDeleteV1Last checks the PGD row: V1 page0 (the only page) refuses
// with operation_failed.
func TestPageDeleteV1Last(t *testing.T) {
	w := mustBegin(t, vsdxtest.Fixture(t, "v1-minimal"))
	_, _, _, _, err := (&ops.PageDeleteSpec{At: "page0"}).Run(w, map[string]string{}, "pt", nil)
	var ce *cli.Error
	if !errors.As(err, &ce) || ce.Code != cli.CodeOperationFailed {
		t.Fatalf("page.delete page0: %v, want operation_failed", err)
	}
	if ce.Path != "at" {
		t.Errorf("path = %q, want at", ce.Path)
	}
	if err := w.Verify(); err != nil {
		t.Fatalf("Verify after refusal: %v", err)
	}
}

// TestScalingPageDelete checks the PGD scaling row: page.delete of
// a page of N and 4N page drawings (N = 1000, 4N = 4000 pages)
// allocates at most 5.5 times as much (laws.Scales) and the
// laws.TimeScales time ratio is at most 8. The measured run holds
// one workspace per size (setup builds it) and deletes the next
// page from the end each run, so Begin's own cost is not measured:
// Begin on many pages is doc.Open's, not this chunk's.
func TestScalingPageDelete(t *testing.T) {
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
		count := 0
		return func() error {
			target := vsd.PageID(uint32(n-1-count), 1)
			count++
			_, _, deleted, _, err := (&ops.PageDeleteSpec{At: target}).Run(w, map[string]string{}, "pt", nil)
			if err != nil {
				return err
			}
			if len(deleted) != 1 || deleted[0] != target {
				return fmt.Errorf("deleted = %v, want [%s]", deleted, target)
			}
			return w.Verify()
		}, nil
	}
	laws.Scales(t, "vsdx page.delete pages", setup)
	laws.TimeScales(t, "vsdx page.delete pages", setup)
}
