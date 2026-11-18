package apply

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/jhnoel/documentdriver/internal/cli"
	"github.com/jhnoel/documentdriver/internal/corpustest"
	"github.com/jhnoel/documentdriver/internal/repo"
	"github.com/jhnoel/documentdriver/internal/soffice"
	"github.com/jhnoel/documentdriver/internal/vsdx/model"
	"github.com/jhnoel/documentdriver/internal/vsdx/ops"
	"github.com/jhnoel/documentdriver/internal/vsdx/read"
)

// corpusDir returns the vsdx corpus directory: VSDX_CORPUS
// directly, below corpus/vsdx when VSDX_CORPUS is a worktree root,
// or the vsdx directory of the repository corpus when unset, or ""
// when there is none. It mirrors read's corpus helper.
func corpusDir() string {
	if d := os.Getenv("VSDX_CORPUS"); d != "" {
		for _, cand := range []string{d, filepath.Join(d, "corpus", "vsdx"), filepath.Join(d, "vsdx")} {
			if _, err := os.Stat(filepath.Join(cand, "repos")); err == nil {
				return cand
			}
		}
		if _, err := os.Stat(d); err == nil {
			return d
		}
		return ""
	}
	dir, err := repo.Corpus()
	if err != nil {
		return ""
	}
	dir = filepath.Join(dir, "vsdx")
	if _, err := os.Stat(dir); err != nil {
		return ""
	}
	return dir
}

// corpusDrawings lists the drawings under dir in traversal order,
// sorted: the Visio extensions the design names for the corpus
// sweep.
func corpusDrawings(dir string) []string {
	var out []string
	_ = filepath.WalkDir(dir, func(p string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		switch strings.ToLower(filepath.Ext(p)) {
		case ".vsdx", ".vsdm", ".vstx", ".vstm", ".vssx", ".vssm":
			out = append(out, p)
		}
		return nil
	})
	slices.Sort(out)
	return out
}

// corpusTrial is one op tried on a corpus drawing's first page:
// its plan and the page-count change it makes when it applies.
type corpusTrial struct {
	op        string
	plan      *ops.Plan
	pageDelta int
}

// corpusTrials builds one trial per op of the op table, in table
// order, on the first page of the drawing (sum and page are its
// summary and first page model). A builder with no suitable
// target on that page returns no trial and is counted skipped.
func corpusTrials(sum *read.Summary, page *model.Page) []corpusTrial {
	var out []corpusTrial
	add := func(op string, plan *ops.Plan, delta int) {
		if plan != nil {
			out = append(out, corpusTrial{op: op, plan: plan, pageDelta: delta})
		}
	}
	firstShape := func(want func(*model.Object) bool) *model.Object {
		for _, o := range page.Objects {
			if want(o) {
				return o
			}
		}
		return nil
	}
	writable := func(o *model.Object) bool { return lawWritableKind(o.Kind) }
	// The builders follow ops.Ops order.
	for _, def := range ops.Ops {
		switch def.Name {
		case "object.add":
			add(def.Name, &ops.Plan{Ops: []ops.Spec{&ops.ObjectAddSpec{At: page.ID, Geom: "rect", Box: []float64{72, 72, 72, 36}}}}, 0)
		case "object.set":
			if o := firstShape(func(o *model.Object) bool { return writable(o) && o.Fill != nil }); o != nil {
				add(def.Name, &ops.Plan{Ops: []ops.Spec{&ops.ObjectSetSpec{At: o.ID, Fill: &ops.PaintSpec{None: o.Fill.None, Hex: o.Fill.Hex}}}}, 0)
			} else if o := firstShape(func(o *model.Object) bool { return writable(o) && o.Box != nil }); o != nil {
				add(def.Name, &ops.Plan{Ops: []ops.Spec{&ops.ObjectSetSpec{At: o.ID, Box: []float64{o.Box.X, o.Box.Y, o.Box.W, o.Box.H}}}}, 0)
			}
		case "text.set":
			if o := firstShape(func(o *model.Object) bool { return writable(o) && o.HasText }); o != nil {
				add(def.Name, &ops.Plan{Ops: []ops.Spec{&ops.TextSetSpec{At: o.ID, HTML: o.HTML}}}, 0)
			}
		case "prop.add":
			if o := firstShape(writable); o != nil {
				add(def.Name, &ops.Plan{Ops: []ops.Spec{&ops.PropAddSpec{At: o.ID, Name: "LawRow", Type: "string", Value: "x"}}}, 0)
			}
		case "prop.set":
			trial := propSetTrial(page)
			add(def.Name, trial, 0)
		case "page.add":
			add(def.Name, &ops.Plan{Ops: []ops.Spec{&ops.PageAddSpec{Name: "LawPage", Size: []float64{612, 792}}}}, 1)
		case "page.rename":
			add(def.Name, &ops.Plan{Ops: []ops.Spec{&ops.PageRenameSpec{At: page.ID, Name: page.Name}}}, 0)
		case "page.set":
			if page.Orientation != "" {
				add(def.Name, &ops.Plan{Ops: []ops.Spec{&ops.PageSetSpec{At: page.ID, Orientation: page.Orientation}}}, 0)
			} else if page.HasSize {
				add(def.Name, &ops.Plan{Ops: []ops.Spec{&ops.PageSetSpec{At: page.ID, Size: []float64{page.W * 72, page.H * 72}}}}, 0)
			}
		case "object.move":
			if o := firstShape(func(o *model.Object) bool { return writable(o) && o.Box != nil }); o != nil {
				add(def.Name, &ops.Plan{Ops: []ops.Spec{&ops.ObjectMoveSpec{At: o.ID, DX: 72}}}, 0)
			}
		case "object.delete":
			if o := firstShape(writable); o != nil {
				add(def.Name, &ops.Plan{Ops: []ops.Spec{&ops.ObjectDeleteSpec{At: o.ID}}}, 0)
			}
		case "object.reorder":
			if o := firstShape(writable); o != nil {
				add(def.Name, &ops.Plan{Ops: []ops.Spec{&ops.ObjectReorderSpec{At: o.ID, To: "front"}}}, 0)
			}
		case "text.replace":
			if o := firstShape(func(o *model.Object) bool { return writable(o) && o.Text != "" }); o != nil {
				add(def.Name, &ops.Plan{Ops: []ops.Spec{&ops.TextReplaceSpec{At: o.ID, Select: o.Text, With: o.Text + " law", All: true}}}, 0)
			}
		case "prop.delete":
			trial := propDeleteTrial(page)
			add(def.Name, trial, 0)
		case "page.delete":
			if len(sum.Pages) > 1 {
				add(def.Name, &ops.Plan{Ops: []ops.Spec{&ops.PageDeleteSpec{At: sum.Pages[1].ID}}}, -1)
			} else {
				add(def.Name, &ops.Plan{Ops: []ops.Spec{&ops.PageDeleteSpec{At: page.ID}}}, -1)
			}
		case "page.reorder":
			if len(sum.Pages) > 1 {
				add(def.Name, &ops.Plan{Ops: []ops.Spec{&ops.PageReorderSpec{At: sum.Pages[0].ID, To: sum.Pages[1].ID, Position: "after"}}}, 0)
			}
		}
	}
	return out
}

// propSetTrial returns a prop.set identity trial on the first
// non-linked prop of the page, or nil when there is none.
func propSetTrial(page *model.Page) *ops.Plan {
	for _, o := range page.Objects {
		if o.Parent != "" {
			continue
		}
		for _, pr := range o.Props {
			if pr.Linked || pr.Value == nil {
				continue
			}
			return &ops.Plan{Ops: []ops.Spec{&ops.PropSetSpec{At: pr.ID, Value: pr.Value}}}
		}
	}
	return nil
}

// propDeleteTrial returns a prop.delete trial on the first prop
// of the page, or nil when there is none.
func propDeleteTrial(page *model.Page) *ops.Plan {
	for _, o := range page.Objects {
		if o.Parent != "" {
			continue
		}
		if len(o.Props) > 0 {
			return &ops.Plan{Ops: []ops.Spec{&ops.PropDeleteSpec{At: o.Props[0].ID}}}
		}
	}
	return nil
}

// corpusCounts are one corpus file's apply sweep tally.
type corpusCounts struct {
	outcome      string
	applied      int
	refused      int
	skippedTrial int
	errors       int
	loChecked    int
	loFailed     int
	refusedNames map[string]int
}

// pdfPageRe matches a PDF page dictionary marker.
var pdfPageRe = regexp.MustCompile(`/Type\s*/Page[^s]`)

// pdfCountRe matches a PDF page-tree count.
var pdfCountRe = regexp.MustCompile(`/Count\s+(\d+)`)

// pdfPageCount returns the page count of a PDF produced by
// LibreOffice: the number of page dictionaries, or the largest
// page-tree count when none are visible.
func pdfPageCount(data []byte) int {
	if n := len(pdfPageRe.FindAll(data, -1)); n > 0 {
		return n
	}
	best := 0
	for _, m := range pdfCountRe.FindAllSubmatch(data, -1) {
		if v, err := parseIntBytes(m[1]); err == nil && v > best {
			best = v
		}
	}
	return best
}

// parseIntBytes parses decimal bytes as an int.
func parseIntBytes(b []byte) (int, error) {
	n := 0
	for _, c := range b {
		if c < '0' || c > '9' {
			return 0, fmt.Errorf("not a number")
		}
		n = n*10 + int(c-'0')
	}
	return n, nil
}

// libreOfficePages opens every output in outputs with headless
// LibreOffice in one batch conversion to PDF (the L1c harness's
// soffice use) and checks each PDF's page count against want,
// returning the number checked and the failures. It skips
// (0, nil) without a soffice binary.
func libreOfficePages(t *testing.T, outputs []string, want map[string]int) (checked int, failures []string) {
	t.Helper()
	bin := soffice.Find(os.Getenv("DD_SOFFICE"), "soffice", soffice.DefaultPaths...)
	if bin == "" {
		t.Log("no soffice (LibreOffice) on PATH; skipping LibreOffice page-count check")
		return 0, nil
	}
	if len(outputs) == 0 {
		return 0, nil
	}
	// Copy outputs under unique plain names so soffice and the
	// output directory cannot collide.
	dir := t.TempDir()
	inputs := make([]string, len(outputs))
	for i, src := range outputs {
		dst := filepath.Join(dir, fmt.Sprintf("out%d.vsdx", i))
		data, err := os.ReadFile(src)
		if err != nil {
			failures = append(failures, fmt.Sprintf("%s: read output: %v", src, err))
			continue
		}
		if err := os.WriteFile(dst, data, 0o600); err != nil {
			failures = append(failures, fmt.Sprintf("%s: copy output: %v", src, err))
			continue
		}
		inputs[i] = dst
	}
	outdir := filepath.Join(dir, "pdf")
	if err := os.MkdirAll(outdir, 0o700); err != nil {
		return 0, []string{fmt.Sprintf("output dir: %v", err)}
	}
	profile := filepath.Join(dir, "profile")
	args := []string{
		"-env:UserInstallation=file://" + profile,
		"--headless", "--norestore", "--nolockcheck", "--nodefault", "--nologo",
		"--convert-to", "pdf", "--outdir", outdir,
	}
	args = append(args, inputs...)
	out, err := soffice.Run(120*time.Second, dir, bin, args...)
	if err != nil {
		return 0, []string{fmt.Sprintf("soffice convert: %v: %s", err, strings.TrimSpace(string(out)))}
	}
	for i, src := range outputs {
		if inputs[i] == "" {
			continue
		}
		pdf := filepath.Join(outdir, fmt.Sprintf("out%d.pdf", i))
		data, err := os.ReadFile(pdf)
		if err != nil {
			failures = append(failures, fmt.Sprintf("%s: LibreOffice produced no PDF: %v", src, err))
			continue
		}
		checked++
		if got, want := pdfPageCount(data), want[src]; got != want {
			failures = append(failures, fmt.Sprintf("%s: LibreOffice PDF has %d pages, want %d", src, got, want))
		}
	}
	return checked, failures
}

// TestApplyCorpus checks the CC14 corpus row: each op of the op
// table on the first page of each VSDX_CORPUS drawing, outcome
// counted (applied, refused by name, error). Any error that is
// not a documented refusal (operation_failed) fails. LibreOffice
// opens every output in one batch per file and reads back the
// same page count (plus the page.add/page.delete delta). It
// skips without VSDX_CORPUS and fails naming the path when
// VSDX_CORPUS is set but a file is missing.
func TestApplyCorpus(t *testing.T) {
	dir := corpusDir()
	if dir == "" {
		t.Skip("no vsdx corpus (set VSDX_CORPUS)")
	}
	paths := corpusDrawings(dir)
	if len(paths) == 0 {
		t.Skip("no vsdx corpus drawings under VSDX_CORPUS")
	}
	// The corpus-file rule: with VSDX_CORPUS set, every listed
	// drawing must exist; corpusDrawings only returns existing
	// files, so a missing corpus directory is a failure, not a
	// skip, and the count below names it.
	files := corpustest.Files(paths, dir)
	if len(corpustest.Shard(t, files)) == 0 {
		t.Skip("no corpus drawings in this CORPUS_SHARD")
	}
	res := corpustest.Run(t, files, func(t *testing.T, f corpustest.File, r *corpusCounts) {
		r.refusedNames = map[string]int{}
		r.outcome = "ok"
		input, err := os.ReadFile(f.Path)
		if err != nil {
			t.Fatal(err)
		}
		sum, err := read.Document(input, "BASE")
		if err != nil {
			// Invalid documents are read's classified outcome,
			// not an apply error: no op can run on them.
			r.outcome = "invalid_document"
			return
		}
		if len(sum.Pages) == 0 {
			r.outcome = "no-pages"
			return
		}
		page, err := read.PageModel(input, sum.Pages[0].ID, read.Options{Detail: "full"})
		if err != nil {
			r.outcome = "unreadable"
			return
		}
		trials := corpusTrials(sum, page)
		var outputs []string
		wantPages := map[string]int{}
		for _, trial := range trials {
			out := filepath.Join(t.TempDir(), "out.vsdx")
			_, err := Run(f.Path, trial.plan, Options{Out: out})
			if err != nil {
				var ce *cli.Error
				if errors.As(err, &ce) && ce.Code == cli.CodeOperationFailed {
					r.refused++
					r.refusedNames[ce.Message]++
					continue
				}
				r.errors++
				t.Errorf("%s: %s: error not a documented refusal: %v", f.Name, trial.op, err)
				continue
			}
			r.applied++
			if _, statErr := os.Stat(out); statErr == nil {
				// Unique output name per trial for the batch.
				unique := filepath.Join(t.TempDir(), trial.op+".vsdx")
				data, readErr := os.ReadFile(out)
				if readErr != nil {
					t.Errorf("%s: %s: read output: %v", f.Name, trial.op, readErr)
					continue
				}
				if err := os.WriteFile(unique, data, 0o600); err != nil {
					t.Errorf("%s: %s: copy output: %v", f.Name, trial.op, err)
					continue
				}
				outputs = append(outputs, unique)
				wantPages[unique] = len(sum.Pages) + trial.pageDelta
			}
		}
		checked, failures := libreOfficePages(t, outputs, wantPages)
		r.loChecked = checked
		r.loFailed = len(failures)
		for _, msg := range failures {
			r.errors++
			t.Errorf("%s: %s", f.Name, msg)
		}
	}, corpustest.MaxParallel(2))
	if corpustest.Sharded() {
		return
	}
	var applied, refused, skippedTrial, errs, loChecked, loFailed int
	var invalid, noPages, unreadable int
	refusedByName := map[string]int{}
	for _, r := range res {
		if !r.Ran {
			continue
		}
		applied += r.Value.applied
		refused += r.Value.refused
		skippedTrial += r.Value.skippedTrial
		errs += r.Value.errors
		loChecked += r.Value.loChecked
		loFailed += r.Value.loFailed
		switch r.Value.outcome {
		case "invalid_document":
			invalid++
		case "no-pages":
			noPages++
		case "unreadable":
			unreadable++
		}
		for name, n := range r.Value.refusedNames {
			refusedByName[name] += n
		}
	}
	t.Logf("vsdx apply corpus: %d files, applied %d, refused %d %v, skipped trials %d, errors %d, invalid %d, no-pages %d, unreadable %d, LibreOffice checked %d (failed %d)",
		len(res), applied, refused, refusedByName, skippedTrial, errs, invalid, noPages, unreadable, loChecked, loFailed)
	if len(files) != 65 {
		t.Errorf("corpus has %d drawings, want 65", len(files))
	}
	if errs != 0 {
		t.Errorf("corpus apply has %d errors, want 0", errs)
	}
}
