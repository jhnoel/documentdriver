package ops_test

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/jhnoel/documentdriver/internal/cli"
	"github.com/jhnoel/documentdriver/internal/laws"
	"github.com/jhnoel/documentdriver/internal/opc"
	"github.com/jhnoel/documentdriver/internal/repo"
	"github.com/jhnoel/documentdriver/internal/vsdx/doc"
	"github.com/jhnoel/documentdriver/internal/vsdx/edit"
	"github.com/jhnoel/documentdriver/internal/vsdx/ops"
	"github.com/jhnoel/documentdriver/internal/vsdx/read"
	"github.com/jhnoel/documentdriver/internal/vsdx/vsd"
	"github.com/jhnoel/documentdriver/internal/vsdx/vsdxtest"
)

// blankPath writes the blank drawing from the golden members.
func blankPath(t *testing.T) string {
	t.Helper()
	root, err := repo.Root()
	if err != nil {
		t.Fatalf("repo root: %v", err)
	}
	dir := filepath.Join(root, "testdata", "vsdx", "create", "blank")
	bodies := map[string]string{}
	err = filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || d.Name() == "PROVENANCE.md" {
			return nil
		}
		rel, err := filepath.Rel(dir, path)
		if err != nil {
			return err
		}
		body, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		bodies[filepath.ToSlash(rel)] = string(body)
		return nil
	})
	if err != nil {
		t.Fatalf("walk blank golden: %v", err)
	}
	names := make([]string, 0, len(bodies))
	for n := range bodies {
		if n != "[Content_Types].xml" {
			names = append(names, n)
		}
	}
	slices.Sort(names)
	members := []opc.ZipMember{{Name: "[Content_Types].xml", Body: bodies["[Content_Types].xml"]}}
	for _, n := range names {
		members = append(members, opc.ZipMember{Name: n, Body: bodies[n]})
	}
	data, err := opc.BuildZip(members...)
	if err != nil {
		t.Fatalf("build blank: %v", err)
	}
	path := filepath.Join(t.TempDir(), "blank.vsdx")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatalf("write blank: %v", err)
	}
	return path
}

// mustBegin begins a workspace, failing on error.
func mustBegin(t *testing.T, path string) *edit.Workspace {
	t.Helper()
	w, err := edit.Begin(path)
	if err != nil {
		t.Fatalf("Begin: %v", err)
	}
	t.Cleanup(func() { w.Tx().Rollback() })
	return w
}

// summaryPage is one read summary page for assertions.
type summaryPage struct {
	ID               string
	Name             string
	Size             []float64
	Orientation      string
	PrintOrientation string
}

// readSummary returns the summary pages of the snapshot.
func readSummary(t *testing.T, w *edit.Workspace) []summaryPage {
	t.Helper()
	path := filepath.Join(t.TempDir(), "snap.vsdx")
	if err := w.Tx().Snapshot(path); err != nil {
		t.Fatalf("Snapshot: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read snapshot: %v", err)
	}
	sum, err := read.Document(data, "sha256:"+strings.Repeat("0", 64))
	if err != nil {
		t.Fatalf("read.Document: %v", err)
	}
	out := make([]summaryPage, 0, len(sum.Pages))
	for _, p := range sum.Pages {
		sp := summaryPage{ID: p.ID, Name: p.Name, Orientation: p.Orientation, PrintOrientation: p.PrintOrientation}
		for _, n := range p.Size {
			f, _ := n.Float64()
			sp.Size = append(sp.Size, f)
		}
		out = append(out, sp)
	}
	return out
}

// pageCellV returns the stored V of the page sheet cell name on addr.
func pageCellV(t *testing.T, w *edit.Workspace, addr, name string) string {
	t.Helper()
	d, err := doc.Open(w.Package())
	if err != nil {
		t.Fatalf("doc.Open: %v", err)
	}
	rels, err := w.Package().Rels(d.Part)
	if err != nil {
		t.Fatalf("Rels: %v", err)
	}
	var pagesPart string
	for _, rel := range rels.All() {
		if rel.External() || !strings.Contains(rel.Type, "pages") {
			continue
		}
		part, err := rels.TargetPart(rel)
		if err == nil {
			pagesPart = part
			break
		}
	}
	if pagesPart == "" {
		t.Fatalf("no pages part")
	}
	pages, err := w.Core().Tree(pagesPart)
	if err != nil {
		t.Fatalf("Tree(pages): %v", err)
	}
	a, err := vsd.Parse(addr)
	if err != nil {
		t.Fatalf("Parse(%s): %v", addr, err)
	}
	for c := range pages.Root().ElementsSeq() {
		if c.Local() != vsd.ElemPage || !vsd.IsMain(c.Space()) {
			continue
		}
		idStr, _ := c.Attr("ID")
		var id uint32
		for _, ch := range idStr {
			id = id*10 + uint32(ch-'0')
		}
		if id != a.Container {
			continue
		}
		for sheet := range c.ElementsSeq() {
			if sheet.Local() != vsd.ElemPageSheet {
				continue
			}
			for cell := range sheet.ElementsSeq() {
				if cell.Local() != vsd.ElemCell {
					continue
				}
				if nn, _ := cell.Attr("N"); nn == name {
					v, _ := cell.Attr("V")
					return v
				}
			}
		}
	}
	t.Fatalf("no cell %s on %s", name, addr)
	return ""
}

// TestPageC3OnBlank checks the OP1 row: page.set landscape, page.add
// Wide 1224x792 as p2, page.rename Overview on the blank, then read
// gives page0 Overview [792 612] landscape/landscape and page1 Wide
// [1224 792] landscape/landscape, local print 2 on both.
func TestPageC3OnBlank(t *testing.T) {
	w := mustBegin(t, blankPath(t))
	aliases := map[string]string{}
	as, changed, _, _, err := (&ops.PageSetSpec{At: "page0", Orientation: "landscape"}).Run(w, aliases, "pt", nil)
	if err != nil {
		t.Fatalf("page.set: %v", err)
	}
	if as != "" || len(changed) != 1 || changed[0] != "page0" {
		t.Errorf("page.set = (%q, %v), want page0 only", as, changed)
	}
	as, changed, _, _, err = (&ops.PageAddSpec{Name: "Wide", As: "p2", Size: []float64{1224, 792}}).Run(w, aliases, "pt", nil)
	if err != nil {
		t.Fatalf("page.add: %v", err)
	}
	if as != "page1" || len(changed) != 1 || changed[0] != "page1" {
		t.Errorf("page.add = (%q, %v), want page1", as, changed)
	}
	aliases["p2"] = as
	_, changed, _, _, err = (&ops.PageRenameSpec{At: "page0", Name: "Overview"}).Run(w, aliases, "pt", nil)
	if err != nil {
		t.Fatalf("page.rename: %v", err)
	}
	if len(changed) != 1 || changed[0] != "page0" {
		t.Errorf("page.rename changed = %v, want [page0]", changed)
	}
	pages := readSummary(t, w)
	if len(pages) != 2 {
		t.Fatalf("pages = %+v, want 2", pages)
	}
	if pages[0].Name != "Overview" || len(pages[0].Size) != 2 || pages[0].Size[0] != 792 || pages[0].Size[1] != 612 || pages[0].Orientation != "landscape" || pages[0].PrintOrientation != "landscape" {
		t.Errorf("page0 = %+v, want Overview [792 612] landscape/landscape", pages[0])
	}
	if pages[1].ID != "page1" || pages[1].Name != "Wide" || len(pages[1].Size) != 2 || pages[1].Size[0] != 1224 || pages[1].Size[1] != 792 || pages[1].Orientation != "landscape" || pages[1].PrintOrientation != "landscape" {
		t.Errorf("page1 = %+v, want Wide [1224 792] landscape/landscape", pages[1])
	}
	if v := pageCellV(t, w, "page0", vsd.CellOrientation); v != "2" {
		t.Errorf("page0 local PrintPageOrientation = %q, want 2", v)
	}
	if v := pageCellV(t, w, "page1", vsd.CellOrientation); v != "2" {
		t.Errorf("page1 local PrintPageOrientation = %q, want 2 (StyleSheet 0 has 1)", v)
	}
}

// TestPageSetV17 checks the V17 rows.
func TestPageSetV17(t *testing.T) {
	t.Run("page0 landscape", func(t *testing.T) {
		w := mustBegin(t, vsdxtest.Fixture(t, "v17-orientation"))
		_, changed, _, stale, err := (&ops.PageSetSpec{At: "page0", Orientation: "landscape"}).Run(w, map[string]string{}, "pt", nil)
		if err != nil {
			t.Fatalf("page.set: %v", err)
		}
		if !strings.Contains(stale, "page0/s1") || !strings.Contains(stale, "PinY") {
			t.Errorf("stale = %q, want the ThePage! dependent page0/s1 PinY", stale)
		}
		if len(changed) != 2 || changed[0] != "page0" || changed[1] != "page0/s1" {
			t.Errorf("changed = %v, want [page0 page0/s1]", changed)
		}
		pages := readSummary(t, w)
		if len(pages[0].Size) != 2 || pages[0].Size[0] != 792 || pages[0].Size[1] != 612 {
			t.Errorf("page0 size = %v, want [792 612]", pages[0].Size)
		}
		if v := pageCellV(t, w, "page0", vsd.CellOrientation); v != "2" {
			t.Errorf("page0 local Print = %q, want 2", v)
		}
	})
	t.Run("page2 landscape", func(t *testing.T) {
		w := mustBegin(t, vsdxtest.Fixture(t, "v17-orientation"))
		_, changed, _, stale, err := (&ops.PageSetSpec{At: "page2", Orientation: "landscape"}).Run(w, map[string]string{}, "pt", nil)
		if err != nil {
			t.Fatalf("page.set: %v", err)
		}
		if stale != "" {
			t.Errorf("stale = %q, want none on page2", stale)
		}
		if len(changed) != 1 || changed[0] != "page2" {
			t.Errorf("changed = %v, want [page2]", changed)
		}
		pages := readSummary(t, w)
		for _, p := range pages {
			if p.ID == "page2" && (len(p.Size) != 2 || p.Size[0] != 792 || p.Size[1] != 612) {
				t.Errorf("page2 size = %v, want unchanged [792 612]", p.Size)
			}
		}
		if v := pageCellV(t, w, "page2", vsd.CellOrientation); v != "2" {
			t.Errorf("page2 local Print = %q, want 2", v)
		}
	})
	t.Run("page3 landscape", func(t *testing.T) {
		w := mustBegin(t, vsdxtest.Fixture(t, "v17-orientation"))
		_, changed, _, _, err := (&ops.PageSetSpec{At: "page3", Orientation: "landscape"}).Run(w, map[string]string{}, "pt", nil)
		if err != nil {
			t.Fatalf("page.set: %v", err)
		}
		if len(changed) != 1 || changed[0] != "page3" {
			t.Errorf("changed = %v, want [page3]", changed)
		}
		pages := readSummary(t, w)
		for _, p := range pages {
			if p.ID == "page3" && (len(p.Size) != 2 || p.Size[0] != 576 || p.Size[1] != 576) {
				t.Errorf("page3 size = %v, want unchanged [576 576]", p.Size)
			}
		}
		if v := pageCellV(t, w, "page3", vsd.CellOrientation); v != "2" {
			t.Errorf("page3 local Print = %q, want 2 (square never swaps)", v)
		}
	})
	t.Run("page1 noop", func(t *testing.T) {
		w := mustBegin(t, vsdxtest.Fixture(t, "v17-orientation"))
		_, changed, _, stale, err := (&ops.PageSetSpec{At: "page1", Orientation: "landscape"}).Run(w, map[string]string{}, "pt", nil)
		if err != nil {
			t.Fatalf("page.set: %v", err)
		}
		if len(changed) != 0 || stale != "" {
			t.Errorf("changed = %v, stale = %q, want no-op", changed, stale)
		}
	})
}

// TestPageRenameDuplicate checks renaming to an existing name.
func TestPageRenameDuplicate(t *testing.T) {
	w := mustBegin(t, vsdxtest.Fixture(t, "v17-orientation"))
	_, _, _, _, err := (&ops.PageRenameSpec{At: "page1", Name: "tall"}).Run(w, map[string]string{}, "pt", nil)
	var ce *cli.Error
	if !errors.As(err, &ce) || ce.Code != cli.CodeOperationFailed || ce.Path != "name" {
		t.Fatalf("rename to Tall: %v, want operation_failed at name", err)
	}
	if err := w.Verify(); err != nil {
		t.Fatalf("Verify: %v", err)
	}
}

// TestPageSetOffPage checks a shape whose box falls off the page.
func TestPageSetOffPage(t *testing.T) {
	w := mustBegin(t, vsdxtest.Fixture(t, "v1-minimal"))
	_, changed, _, _, err := (&ops.PageSetSpec{At: "page0", Orientation: "landscape"}).Run(w, map[string]string{}, "pt", nil)
	if err != nil {
		t.Fatalf("page.set: %v", err)
	}
	if len(changed) != 2 || changed[0] != "page0" || changed[1] != "page0/s1" {
		t.Fatalf("changed = %v, want [page0 page0/s1]", changed)
	}
	path := filepath.Join(t.TempDir(), "off.vsdx")
	if err := w.Tx().Snapshot(path); err != nil {
		t.Fatalf("Snapshot: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read snapshot: %v", err)
	}
	raw, err := read.Page(data, "sha256:"+strings.Repeat("0", 64), "page0", read.Options{})
	if err != nil {
		t.Fatalf("read.Page: %v", err)
	}
	var page struct {
		Size    []json.Number `json:"size"`
		Objects []struct {
			ID  string        `json:"id"`
			Box []json.Number `json:"box"`
		} `json:"objects"`
	}
	if err := json.Unmarshal(raw, &page); err != nil {
		t.Fatalf("unmarshal page: %v", err)
	}
	if len(page.Objects) != 1 || len(page.Objects[0].Box) != 4 {
		t.Fatalf("page objects = %+v, want one with a box", page.Objects)
	}
	box := make([]float64, 4)
	for i, n := range page.Objects[0].Box {
		box[i], _ = n.Float64()
	}
	wPt, _ := page.Size[0].Float64()
	hPt, _ := page.Size[1].Float64()
	outside := box[0]+box[2] <= 0 || box[0] >= wPt || box[1]+box[3] <= 0 || box[1] >= hPt
	if !outside {
		t.Errorf("box = %v on [%v %v], want wholly outside (off-page)", box, wPt, hPt)
	}
}

// TestPageAddAfterBefore checks page.add placement.
func TestPageAddAfterBefore(t *testing.T) {
	w := mustBegin(t, blankPath(t))
	if _, _, _, _, err := (&ops.PageAddSpec{Name: "A", Size: []float64{612, 792}}).Run(w, map[string]string{}, "pt", nil); err != nil {
		t.Fatalf("add A: %v", err)
	}
	if _, _, _, _, err := (&ops.PageAddSpec{Name: "B", Size: []float64{612, 792}}).Run(w, map[string]string{}, "pt", nil); err != nil {
		t.Fatalf("add B: %v", err)
	}
	addr, _, _, _, err := (&ops.PageAddSpec{Name: "C", At: "page0", Position: "after", Size: []float64{612, 792}}).Run(w, map[string]string{}, "pt", nil)
	if err != nil {
		t.Fatalf("add C after page0: %v", err)
	}
	if addr != "page3" {
		t.Errorf("C address = %q, want page3 (max+1)", addr)
	}
	addr, _, _, _, err = (&ops.PageAddSpec{Name: "D", At: "page2", Position: "before", Size: []float64{612, 792}}).Run(w, map[string]string{}, "pt", nil)
	if err != nil {
		t.Fatalf("add D before page2: %v", err)
	}
	if addr != "page4" {
		t.Errorf("D address = %q, want page4 (max+1)", addr)
	}
	addr, _, _, _, err = (&ops.PageAddSpec{Name: "E", At: "page0", Position: "before", Size: []float64{612, 792}}).Run(w, map[string]string{}, "pt", nil)
	if err != nil {
		t.Fatalf("add E before page0: %v", err)
	}
	if addr != "page5" {
		t.Errorf("E address = %q, want page5 (max+1)", addr)
	}
	if pages := readSummary(t, w); len(pages) == 0 || pages[0].ID != "page5" || pages[0].Name != "E" {
		t.Errorf("first page after E = %+v, want page5 E (before the first page, CC13)", pages)
	}
}

// TestPageSetSizeOrientation checks size-only and contradictions.
func TestPageSetSizeOrientation(t *testing.T) {
	w := mustBegin(t, blankPath(t))
	_, changed, _, _, err := (&ops.PageSetSpec{At: "page0", Size: []float64{11, 8.5}}).Run(w, map[string]string{}, "in", nil)
	if err != nil {
		t.Fatalf("page.set size: %v", err)
	}
	if len(changed) != 1 || changed[0] != "page0" {
		t.Errorf("changed = %v, want [page0] (no shapes)", changed)
	}
	if v := pageCellV(t, w, "page0", vsd.CellOrientation); v != "2" {
		t.Errorf("local Print = %q, want 2 for a landscape size", v)
	}
	_, _, _, _, err = (&ops.PageSetSpec{At: "page0", Size: []float64{1224, 792}, Orientation: "portrait"}).Run(w, map[string]string{}, "pt", nil)
	var ce *cli.Error
	if !errors.As(err, &ce) || ce.Code != cli.CodeInvalidOperation || ce.Path != "orientation" {
		t.Errorf("wide+portrait: %v, want invalid_operation at orientation", err)
	}
}

// TestScalingPageSet checks the scaling row: N=1000, 4N=4000 shapes.
func TestScalingPageSet(t *testing.T) {
	setup := func(n int) (func() error, error) {
		path := vsdxtest.Generated(t, n)
		return func() error {
			w, err := edit.Begin(path)
			if err != nil {
				return err
			}
			defer w.Tx().Rollback()
			_, _, _, _, err = (&ops.PageSetSpec{At: "page0", Size: []float64{1224, 792}}).Run(w, map[string]string{}, "pt", nil)
			if err != nil {
				return err
			}
			return w.Verify()
		}, nil
	}
	laws.Scales(t, "vsdx PageSet", setup)
	laws.TimeScales(t, "vsdx PageSet", setup)
}
