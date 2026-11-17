package read

import (
	"encoding/json"
	"fmt"
	"os"
	"reflect"
	"runtime"
	"slices"
	"strings"
	"testing"

	"github.com/jhnoel/documentdriver/internal/casefold"
	"github.com/jhnoel/documentdriver/internal/laws"
	"github.com/jhnoel/documentdriver/internal/vsdx/model"
	"github.com/jhnoel/documentdriver/internal/vsdx/vsdxtest"
)

// The RM2 memory gate (make scaling): read.Page default, --detail
// full and Find on Generated(5000) and Generated(20000) (7,500 and
// 30,000 objects) are measured at the checkpoints inside the page
// build (Page: "walk" after the walk and "link" after the glue pass,
// with the nodes released on this path; Find: "shape" as the
// streamed walk goes and "notes" after its notes pass). read.Page
// default and --detail full hold peak live heap at most 2 KB per
// object plus 3 times the output (memoryCeiling), Find at most
// min(2 KB per object, 45 MB), and the 20k/5k peak ratio is at most
// 5.5. The budgets are the designer's (RM1, restored after RM2; a
// chunk does not loosen the checks that judge it). Allocation: page
// default 96 MB at 5k and 388 MB at 20k, full 164 MB and 662 MB (RM1's
// numbers); find 46 MB and 183 MB (lowered by RM2's streaming, kept).
// Measured at 7d09f90: default 87.7 and 357.5 MB, full 150.2 and
// 608.5 MB, find 38.1 and 152.4 MB; RM3 removes marshalPage's second
// marshal and lowers the page budgets.

// memoryCeiling is the peak-live ceiling for a page read with objects
// objects and outBytes bytes of output: 2 KB per object plus 3 times
// the output.
func memoryCeiling(objects, outBytes int) uint64 {
	return uint64(objects)*2048 + 3*uint64(outBytes)
}

// generatedBytes returns the bytes of Generated(n).
func generatedBytes(t *testing.T, n int) []byte {
	t.Helper()
	data, err := os.ReadFile(vsdxtest.Generated(t, n))
	if err != nil {
		t.Fatalf("read Generated(%d): %v", n, err)
	}
	return data
}

// peakRatio checks the 20k/5k peak-live ratio row over the runs of
// one operation: each run marks at its internal checkpoints with its
// result kept alive, and the ratio of the two peaks is at most 5.5.
func peakRatio(t *testing.T, name string, run func(n int, mark func(string)) error) {
	t.Helper()
	var peaks [2]uint64
	for i, n := range []int{5000, 20000} {
		m, err := laws.MeasureMemory(func(mark func(string)) error {
			return run(n, mark)
		})
		if err != nil {
			t.Fatalf("%s Generated(%d): %v", name, n, err)
		}
		peaks[i] = m.PeakLive
	}
	ratio := float64(peaks[1]) / float64(max(peaks[0], 1))
	t.Logf("%s: peak live Generated(5000) %d bytes, Generated(20000) %d bytes, ratio %.2f", name, peaks[0], peaks[1], ratio)
	if ratio > 5.5 {
		t.Errorf("%s: peak live ratio 20k/5k is %.2f, want at most 5.5", name, ratio)
	}
}

// TestMemoryBudgetPageDefault is the RM2 gate for read.Page default:
// Generated(5000) and Generated(20000) stay under memoryCeiling of
// peak live heap at the "walk" and "link" checkpoints, and the peak ratio is
// at most 5.5.
func TestMemoryBudgetPageDefault(t *testing.T) {
	alloc := map[int]uint64{5000: laws.MB(96), 20000: laws.MB(388)}
	for _, n := range []int{5000, 20000} {
		data := generatedBytes(t, n)
		out, err := Page(data, "B", "page0", Options{})
		if err != nil {
			t.Fatalf("Page(Generated(%d)): %v", n, err)
		}
		runtime.KeepAlive(out)
		budget := laws.Budget{PeakLive: memoryCeiling(n+n/2, len(out)), Alloc: alloc[n]}
		laws.WithinMemory(t, fmt.Sprintf("vsdx Page default Generated(%d)", n), budget, func(mark func(string)) error {
			out, err := Page(data, "B", "page0", Options{Checkpoint: mark})
			if err != nil {
				return err
			}
			runtime.KeepAlive(out)
			runtime.KeepAlive(data)
			if len(out) == 0 {
				return fmt.Errorf("Page(Generated(%d)): empty output", n)
			}
			return nil
		})
	}
	peakRatio(t, "vsdx Page default", func(n int, mark func(string)) error {
		data := generatedBytes(t, n)
		out, err := Page(data, "B", "page0", Options{Checkpoint: mark})
		if err != nil {
			return err
		}
		runtime.KeepAlive(out)
		return nil
	})
}

// TestMemoryBudgetPageFull is the RM2 gate for --detail full, as
// TestMemoryBudgetPageDefault for the full view: peak live heap at
// the "walk" and "link" checkpoints is at most memoryCeiling.
func TestMemoryBudgetPageFull(t *testing.T) {
	alloc := map[int]uint64{5000: laws.MB(164), 20000: laws.MB(662)}
	for _, n := range []int{5000, 20000} {
		data := generatedBytes(t, n)
		out, err := Page(data, "B", "page0", Options{Detail: "full"})
		if err != nil {
			t.Fatalf("Page(Generated(%d), full): %v", n, err)
		}
		budget := laws.Budget{PeakLive: memoryCeiling(n+n/2, len(out)), Alloc: alloc[n]}
		laws.WithinMemory(t, fmt.Sprintf("vsdx Page full Generated(%d)", n), budget, func(mark func(string)) error {
			out, err := Page(data, "B", "page0", Options{Detail: "full", Checkpoint: mark})
			if err != nil {
				return err
			}
			runtime.KeepAlive(out)
			runtime.KeepAlive(data)
			if len(out) == 0 {
				return fmt.Errorf("Page(Generated(%d), full): empty output", n)
			}
			return nil
		})
	}
	peakRatio(t, "vsdx Page full", func(n int, mark func(string)) error {
		data := generatedBytes(t, n)
		out, err := Page(data, "B", "page0", Options{Detail: "full", Checkpoint: mark})
		if err != nil {
			return err
		}
		runtime.KeepAlive(out)
		return nil
	})
}

// TestMemoryBudgetFind is the RM2 gate for Find: Generated(5000)
// and Generated(20000) stay under min(2 KB per object, 45 MB) of
// peak live heap at the
// "shape" and "notes" checkpoints (the page bytes plus one shape
// plus the stored-ID set), and the peak ratio is at most 5.5.
func TestMemoryBudgetFind(t *testing.T) {
	alloc := map[int]uint64{5000: laws.MB(46), 20000: laws.MB(183)}
	for _, n := range []int{5000, 20000} {
		data := generatedBytes(t, n)
		res, err := Find(data, "B", FindRequest{Query: "Box"})
		if err != nil {
			t.Fatalf("Find(Generated(%d)): %v", n, err)
		}
		if len(res.Matches) != n {
			t.Fatalf("Find(Generated(%d)): len(Matches) = %d, want %d", n, len(res.Matches), n)
		}
		budget := laws.Budget{PeakLive: min(uint64(n+n/2)*2048, laws.MB(45)), Alloc: alloc[n]}
		laws.WithinMemory(t, fmt.Sprintf("vsdx Find Generated(%d)", n), budget, func(mark func(string)) error {
			res, err := Find(data, "B", FindRequest{Query: "Box", Checkpoint: mark})
			if err != nil {
				return err
			}
			runtime.KeepAlive(res)
			runtime.KeepAlive(data)
			if len(res.Matches) != n {
				return fmt.Errorf("Find(Generated(%d)): len(Matches) = %d, want %d", n, len(res.Matches), n)
			}
			return nil
		})
	}
	peakRatio(t, "vsdx Find", func(n int, mark func(string)) error {
		data := generatedBytes(t, n)
		res, err := Find(data, "B", FindRequest{Query: "Box", Checkpoint: mark})
		if err != nil {
			return err
		}
		runtime.KeepAlive(res)
		return nil
	})
}

// TestCheckpoints checks the checkpoint rows: Page calls "walk" then
// "link", and Find calls "shape" at most every 1024 shapes and then
// "notes".
func TestCheckpoints(t *testing.T) {
	data := fixtureBytes(t, "v1-minimal")
	var got []string
	if _, err := Page(data, "B", "page0", Options{Checkpoint: func(s string) { got = append(got, s) }}); err != nil {
		t.Fatalf("Page: %v", err)
	}
	if !slices.Equal(got, []string{"walk", "link"}) {
		t.Errorf("Page checkpoints = %v, want [walk link]", got)
	}
	got = nil
	if _, err := Find(data, "B", FindRequest{Query: "Hello", Checkpoint: func(s string) { got = append(got, s) }}); err != nil {
		t.Fatalf("Find: %v", err)
	}
	if len(got) == 0 || got[len(got)-1] != "notes" {
		t.Errorf("Find checkpoints = %v, want shape checkpoints then notes", got)
	}
	for _, s := range got[:len(got)-1] {
		if s != "shape" {
			t.Errorf("Find checkpoints = %v, want only shape before notes", got)
		}
	}
}

// oracleFind is the pre-RM2 Find kept as the test oracle: it scans
// the BuildPage model of every page in pages.xml order and matches
// as the old scanPage did. Only the limitless request is exercised.
func oracleFind(input []byte, base string, req FindRequest) (*FindResult, error) {
	pkg, d, r, err := openDoc(input)
	if err != nil {
		return nil, err
	}
	defer pkg.Close()
	res := &FindResult{Base: base, Matches: []Match{}, Notes: []Note{}}
	for _, n := range d.Notes {
		res.Notes = append(res.Notes, Note{At: n.At, Reason: n.Reason})
	}
	folded := req.Query
	if req.IgnoreCase {
		folded = casefold.Simple(req.Query)
	}
	for i := range d.Pages {
		e := &d.Pages[i]
		p, err := model.BuildPage(d, r, e, model.Options{})
		if err != nil {
			res.Notes = append(res.Notes, Note{At: model.PageSheet(d, r, e).ID, Reason: "unreadable: " + err.Error()})
			continue
		}
		for _, n := range p.Notes {
			res.Notes = append(res.Notes, Note{At: n.At, Reason: n.Reason})
		}
		for _, o := range p.Objects {
			text := o.Text
			if req.IgnoreCase {
				text = casefold.Simple(text)
			}
			if o.HasText && strings.Contains(text, folded) {
				res.Matches = append(res.Matches, Match{At: o.ID, Text: o.Text})
			}
			for _, pr := range o.Props {
				pv := propText(pr)
				pt := pv
				if req.IgnoreCase {
					pt = casefold.Simple(pt)
				}
				if pv != "" && strings.Contains(pt, folded) {
					res.Matches = append(res.Matches, Match{At: pr.ID, Text: pv})
				}
			}
		}
	}
	for _, n := range r.Notes() {
		res.Notes = append(res.Notes, Note{At: n.At, Reason: n.Reason})
	}
	return res, nil
}

// TestFindMatchesBuildPage checks the streamed-Find row on the
// fixtures and Generated(20000): the same matches, in the same
// order, and the same notes as the BuildPage oracle. V10 gives the
// connect note.
func TestFindMatchesBuildPage(t *testing.T) {
	fixtures := []string{
		"v1-minimal", "v2-master", "v3-styles", "v4-groups", "v5-glue",
		"v6-data", "v7-text", "v8-pages", "v9-rotation", "v10-defects",
		"v11-ns2011", "v12-stencil", "v12-vsdm", "v13-scale",
		"v15-foreign", "v17-orientation",
	}
	for _, name := range fixtures {
		input := fixtureBytes(t, name)
		got, err := Find(input, "B", FindRequest{Query: ""})
		if err != nil {
			t.Fatalf("Find(%s): %v", name, err)
		}
		want, err := oracleFind(input, "B", FindRequest{Query: ""})
		if err != nil {
			t.Fatalf("oracleFind(%s): %v", name, err)
		}
		if !reflect.DeepEqual(got.Matches, want.Matches) {
			t.Errorf("%s: Matches = %#v, want oracle %#v", name, got.Matches, want.Matches)
		}
		if !reflect.DeepEqual(got.Notes, want.Notes) {
			t.Errorf("%s: Notes = %#v, want oracle %#v", name, got.Notes, want.Notes)
		}
		if name == "v10-defects" {
			found := false
			for _, n := range got.Notes {
				if n.At == "page0" && n.Reason == "connect from sheet names no shape" {
					found = true
				}
			}
			if !found {
				t.Errorf("v10-defects: Notes = %v, want the connect note", got.Notes)
			}
		}
	}
	data := generatedBytes(t, 20000)
	got, err := Find(data, "B", FindRequest{Query: "Box"})
	if err != nil {
		t.Fatalf("Find(Generated(20000)): %v", err)
	}
	want, err := oracleFind(data, "B", FindRequest{Query: "Box"})
	if err != nil {
		t.Fatalf("oracleFind(Generated(20000)): %v", err)
	}
	if !reflect.DeepEqual(got.Matches, want.Matches) {
		t.Errorf("Generated(20000): %d matches, want oracle %d in the same order", len(got.Matches), len(want.Matches))
	}
	if !reflect.DeepEqual(got.Notes, want.Notes) {
		t.Errorf("Generated(20000): Notes = %#v, want oracle %#v", got.Notes, want.Notes)
	}
}

// TestFindCorpusMatchesBuildPage checks the streamed-Find row on the
// corpus: the same matches and notes as the BuildPage oracle for
// every VSDX_CORPUS drawing. It skips only when VSDX_CORPUS is
// unset.
func TestFindCorpusMatchesBuildPage(t *testing.T) {
	if os.Getenv("VSDX_CORPUS") == "" {
		t.Skip("no vsdx corpus (set VSDX_CORPUS)")
	}
	dir := vsdxCorpus()
	if dir == "" {
		t.Fatalf("VSDX_CORPUS=%q: no corpus with repos/ under it", os.Getenv("VSDX_CORPUS"))
	}
	for _, path := range corpusDrawings(dir) {
		input, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		got, gerr := Find(input, "B", FindRequest{Query: ""})
		want, werr := oracleFind(input, "B", FindRequest{Query: ""})
		if (gerr == nil) != (werr == nil) {
			t.Errorf("%s: Find error = %v, oracle error = %v", path, gerr, werr)
			continue
		}
		if gerr != nil {
			continue
		}
		if !reflect.DeepEqual(got.Matches, want.Matches) {
			t.Errorf("%s: %d matches, want oracle %d in the same order", path, len(got.Matches), len(want.Matches))
		}
		if !reflect.DeepEqual(got.Notes, want.Notes) {
			t.Errorf("%s: Notes = %#v, want oracle %#v", path, got.Notes, want.Notes)
		}
	}
}

// TestPageStoredMaster checks that Page (the path that releases the
// nodes) still shows each shape's stored master and not its
// effective one: v2's s1 stores master2, while s4, inside group s3,
// stores none.
func TestPageStoredMaster(t *testing.T) {
	input := fixtureBytes(t, "v2-master")
	out, err := Page(input, "B", "page0", Options{})
	if err != nil {
		t.Fatalf("Page: %v", err)
	}
	var v struct {
		Objects []struct {
			ID     string `json:"id"`
			Master string `json:"master"`
		} `json:"objects"`
	}
	if err := json.Unmarshal(out, &v); err != nil {
		t.Fatalf("unmarshal Page output: %v", err)
	}
	byID := map[string]string{}
	for _, o := range v.Objects {
		byID[o.ID] = o.Master
	}
	if byID["page0/s1"] != "master2" {
		t.Errorf("s1 master = %q, want master2", byID["page0/s1"])
	}
	if byID["page0/s4"] != "" {
		t.Errorf("s4 master = %q, want \"\" (it stores none; its group does)", byID["page0/s4"])
	}
}

// TestScalingReadPageFull checks the scaling row: Page --detail full
// on Generated(1000) and Generated(4000) (N = 1000, 4N = 4000 shapes)
// allocates at most 5.5 times as much (laws.Scales) and the
// laws.TimeScales time ratio is at most 8.
func TestScalingReadPageFull(t *testing.T) {
	setup := func(n int) (func() error, error) {
		data, err := os.ReadFile(vsdxtest.Generated(t, n))
		if err != nil {
			return nil, err
		}
		return func() error {
			got, err := Page(data, "B", "page0", Options{Detail: "full"})
			if err != nil {
				return err
			}
			if len(got) == 0 {
				return fmt.Errorf("Generated(%d): empty output", n)
			}
			return nil
		}, nil
	}
	laws.Scales(t, "vsdx Page full shapes", setup)
	laws.TimeScales(t, "vsdx Page full shapes", setup)
}
