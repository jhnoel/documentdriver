package apply

import (
	"archive/zip"
	"bytes"
	"cmp"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/jhnoel/documentdriver/internal/corpustest"
	docxedit "github.com/jhnoel/documentdriver/internal/docx/edit"
	"github.com/jhnoel/documentdriver/internal/docx/integrity"
	"github.com/jhnoel/documentdriver/internal/docx/model"
	"github.com/jhnoel/documentdriver/internal/docx/ops"
	"github.com/jhnoel/documentdriver/internal/docx/revise"
	"github.com/jhnoel/documentdriver/internal/ooxml/ctypes"
	core "github.com/jhnoel/documentdriver/internal/ooxml/edit"
	"github.com/jhnoel/documentdriver/internal/ooxml/tree"
	"github.com/jhnoel/documentdriver/internal/opc"
	"github.com/jhnoel/documentdriver/internal/plan"
	"github.com/jhnoel/documentdriver/internal/repo"
)

const nsDecl = ` xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"` +
	` xmlns:w14="http://schemas.microsoft.com/office/word/2010/wordml"` +
	` xmlns:mc="http://schemas.openxmlformats.org/markup-compatibility/2006" mc:Ignorable="w14"`

// writeDocx writes a one-part package whose body is body.
func writeDocx(t testing.TB, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "doc.docx")
	var b bytes.Buffer
	z := zip.NewWriter(&b)
	parts := []struct{ name, body string }{
		{"[Content_Types].xml", `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>` +
			`<Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types">` +
			`<Default Extension="rels" ContentType="` + ctypes.CTPackageRelationships + `"/>` +
			`<Default Extension="xml" ContentType="application/xml"/>` +
			`<Override PartName="/word/document.xml" ContentType="` + ctypes.CTWordprocessingmlDocumentMain + `"/></Types>`},
		{"_rels/.rels", `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>` +
			`<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">` +
			`<Relationship Id="rId1" Type="` + ctypes.RelOfficeDocument + `" Target="word/document.xml"/></Relationships>`},
		{"word/document.xml", `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>` +
			`<w:document` + nsDecl + `><w:body>` + body + `<w:sectPr/></w:body></w:document>`},
	}
	for _, p := range parts {
		w, err := z.Create(p.name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write([]byte(p.body)); err != nil {
			t.Fatal(err)
		}
	}
	if err := z.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, b.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-48 * time.Hour)
	if err := os.Chtimes(path, old, old); err != nil {
		t.Fatal(err)
	}
	return path
}

// settle is the operation that accepts or rejects the revision with the
// file's id target, or every revision for "all", built as a Go value.
func settle(target, action string) ops.Spec {
	var at *ops.RevisionScope
	if target != "all" {
		at = &ops.RevisionScope{ID: target}
	}
	if action == "accept" {
		return &ops.AcceptRevisions{At: at, All: at == nil}
	}
	return &ops.RejectRevisions{At: at, All: at == nil}
}

// planOf is the text of a plan of the given operations in mode against
// the file at path, built from Go values (ops.NewPlan).
func planOf(t testing.TB, path, mode string, specs ...ops.Spec) []byte {
	t.Helper()
	b, err := os.ReadFile(path) //nolint:gosec // G304: the test's own copy
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(b)
	p, d := ops.NewPlan(ops.SourceHash("sha256:"+hex.EncodeToString(sum[:])), "Ann", ops.ChangeMode(mode), specs...)
	if d != nil {
		t.Fatalf("plan: %s %s: %s", d.Code, d.Path, d.Message)
	}
	return []byte(p.CanonicalJSON())
}

func planText(t *testing.T, path, mode, target, action string) []byte {
	t.Helper()
	return planOf(t, path, mode, settle(target, action))
}

// commit reviews and commits a plan, returning the commit's result.
func commit(t *testing.T, path string, text []byte) *Result {
	t.Helper()
	r, err := runText(path, text, Options{View: viewOpt})
	if err != nil {
		t.Fatal(err)
	}
	if r.Outcome != Previewed {
		t.Fatalf("review: %s %+v", r.Outcome, r.Diagnostic)
	}
	c, err := runText(path, text, Options{Commit: r.PreviewKey, Now: "2026-01-01T00:00:00Z", View: viewOpt})
	if err != nil {
		t.Fatal(err)
	}
	if err := sameHazards(c); err != nil {
		t.Error(err)
	}
	return c
}

// sameHazards checks that a result's hazards, read from the gate's scoped
// findings, are what a full check of the output gives on the changed
// parts, and that its markup is the full render's (sameAffected).
func sameHazards(r *Result) error {
	if err := sameAffected(r); err != nil {
		return err
	}
	if r.Snapshot == nil {
		return nil
	}
	in := map[string]bool{}
	for _, c := range r.Parts {
		in[plan.PartKey(c)] = true
	}
	var want []Hazard
	for _, k := range integrity.Check(r.Snapshot).Kinds() {
		if in[plan.PartKey(k.Part)] {
			kind, _, _ := strings.Cut(k.Message, ": ")
			want = append(want, Hazard{Kind: kind, ID: k.Part, Detail: k.Message})
		}
	}
	if !slices.Equal(r.Hazards, want) {
		return fmt.Errorf("hazards %v, a full check gives %v", r.Hazards, want)
	}
	return nil
}

func unchangedFile(t *testing.T, path string, want []byte, mtime time.Time) {
	t.Helper()
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	st, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) || !st.ModTime().Equal(mtime) {
		t.Error("the file changed")
	}
	ents, err := os.ReadDir(filepath.Dir(path))
	if err != nil {
		t.Fatal(err)
	}
	if len(ents) != 1 {
		t.Errorf("the directory holds %d entries", len(ents))
	}
}

// TestNoOpWritesNothing: a plan that settles nothing leaves the file as
// it was, bytes and mtime, also when paragraphs lack ids (the repair the
// review reports is not written: reading gives the same ids).
func TestNoOpWritesNothing(t *testing.T) {
	for _, body := range []string{
		`<w:p w14:paraId="0000000A"><w:r><w:t>a</w:t></w:r></w:p>`,
		`<w:p><w:r><w:t>a</w:t></w:r></w:p>`,
	} {
		path := writeDocx(t, body)
		before, _ := os.ReadFile(path) //nolint:errcheck // compared below
		st, _ := os.Stat(path)         //nolint:errcheck // compared below
		c := commit(t, path, planText(t, path, "track", "all", "accept"))
		if c.Outcome != Committed || len(c.Parts) != 0 {
			t.Errorf("%s: %s changed %v", body, c.Outcome, c.Parts)
		}
		unchangedFile(t, path, before, st.ModTime())
	}
}

// TestEmptyPlanPassesGate: the pipeline with no operations (a plan the
// contract refuses, run below it) changes nothing and passes the gate.
func TestEmptyPlanPassesGate(t *testing.T) {
	path := writeDocx(t, `<w:p><w:r><w:t>a</w:t></w:r></w:p>`)
	input, _ := os.ReadFile(path) //nolint:errcheck // checked by fold
	st, _ := os.Stat(path)        //nolint:errcheck // compared below
	p := &ops.Plan{Author: "Ann", ChangeMode: "track"}
	f, err := fold(path, input, sha256.Sum256(input), p, time.Now(), Options{Commit: "x"})
	if err != nil || f.diag != nil {
		t.Fatalf("fold: %v %+v", err, f.diag)
	}
	if err := integrity.Gate(input, input); err != nil {
		t.Error(err)
	}
	unchangedFile(t, path, input, st.ModTime())
}

// TestSettleCommits: accepting a tracked change writes the main document
// only, passes the gate, and leaves no revision.
func TestSettleCommits(t *testing.T) {
	for _, mode := range []string{"track", "direct"} {
		path := writeDocx(t, `<w:p w14:paraId="0000000A"><w:ins w:id="1" w:author="Bob"><w:r><w:t>a</w:t></w:r></w:ins>`+
			`<w:del w:id="2" w:author="Bob"><w:r><w:delText>b</w:delText></w:r></w:del></w:p>`)
		input, _ := os.ReadFile(path) //nolint:errcheck // checked below
		c := commit(t, path, planText(t, path, mode, "all", "accept"))
		if c.Outcome != Committed || !c.Report.Ops[0].Applied {
			t.Fatalf("%s: %+v %+v", mode, c, c.Report)
		}
		if len(c.Parts) != 1 || !strings.HasSuffix(c.Parts[0], "word/document.xml") {
			t.Errorf("%s: changed %v", mode, c.Parts)
		}
		if len(c.Changed) != 1 || c.Changed[0] != "0000000A" || len(c.Deleted) != 0 {
			t.Errorf("%s: changed paragraphs %v, deleted %v", mode, c.Changed, c.Deleted)
		}
		out, _ := os.ReadFile(path) //nolint:errcheck // checked below
		if err := integrity.Gate(input, out); err != nil {
			t.Error(err)
		}
		d, err := model.OpenDocument(out)
		if err != nil {
			t.Fatal(err)
		}
		if n := len(d.XML.RevisionElements()); n != 0 {
			t.Errorf("%s: %d revisions left", mode, n)
		}
		if got := model.ParagraphText(model.ParagraphsIn(d.XML.Body())[0].Node, model.All); got != "a" {
			t.Errorf("%s: text %q", mode, got)
		}
	}
}

// TestSettleRefusals: an unknown id is refused (not_found at the
// operation), a stale base and a wrong key are refused; nothing is written.
func TestSettleRefusals(t *testing.T) {
	path := writeDocx(t, `<w:p w14:paraId="0000000A"><w:ins w:id="1" w:author="Bob"><w:r><w:t>a</w:t></w:r></w:ins></w:p>`)
	before, _ := os.ReadFile(path) //nolint:errcheck // compared below
	st, _ := os.Stat(path)         //nolint:errcheck // compared below
	r, err := runText(path, planText(t, path, "track", "9", "accept"), Options{})
	if err != nil {
		t.Fatal(err)
	}
	if r.Outcome != Rejected || r.Diagnostic.Code != "not_found" || r.Diagnostic.Path != "ops[0].at" ||
		r.Diagnostic.Message != "revision not found: 9" || r.Report.StoppedAt != 0 {
		t.Errorf("unknown id: %+v %+v", r.Diagnostic, r.Report)
	}
	text := planText(t, path, "track", "1", "reject")
	if r, _ := runText(path, text, Options{Commit: "p1:sha256:00"}); r == nil || r.Diagnostic == nil || r.Diagnostic.Code != "preview_key_mismatch" {
		t.Errorf("wrong key: %+v", r)
	}
	// Another base: the first hex digit of the hash changed.
	stale := bytes.Clone(text)
	digit := bytes.Index(stale, []byte(`"base":"sha256:`)) + len(`"base":"sha256:`)
	if stale[digit] == '0' {
		stale[digit] = '1'
	} else {
		stale[digit] = '0'
	}
	if r, _ := runText(path, stale, Options{}); r == nil || r.Diagnostic == nil || r.Diagnostic.Code != "source_mismatch" {
		t.Errorf("stale base: %+v", r.Diagnostic)
	}
	unchangedFile(t, path, before, st.ModTime())
}

// TestAuthoredFixtureSettles runs every settle plan on the authored rich
// fixture and checks the gate and the reader on each output.
func TestAuthoredFixtureSettles(t *testing.T) {
	root, err := repo.Root()
	if err != nil {
		t.Fatal(err)
	}
	src, err := os.ReadFile(filepath.Join(root, "testdata", "docx", "rich.docx"))
	if err != nil {
		t.Fatal(err)
	}
	for _, mode := range []string{"track", "direct"} {
		for _, action := range []string{"accept", "reject"} {
			path := filepath.Join(t.TempDir(), "rich.docx")
			if err := os.WriteFile(path, src, 0o600); err != nil {
				t.Fatal(err)
			}
			c := commit(t, path, planText(t, path, mode, "all", action))
			if c.Outcome != Committed {
				t.Fatalf("%s %s: %+v", mode, action, c.Diagnostic)
			}
			out, _ := os.ReadFile(path) //nolint:errcheck // checked below
			if err := integrity.Gate(src, out); err != nil {
				t.Errorf("%s %s: %v", mode, action, err)
			}
			d, err := model.OpenDocument(out)
			if err != nil {
				t.Fatal(err)
			}
			if n := len(d.XML.RevisionElements()); n != 0 {
				t.Errorf("%s %s: %d body revisions left", mode, action, n)
			}
		}
	}
}

// runText decodes plan text and runs it, as the CLI will.
func runText(path string, text []byte, opt Options) (*Result, error) {
	p, d := ops.ParsePlanJSON(text)
	if d != nil {
		return &Result{Outcome: Rejected, Diagnostic: &Diagnostic{d.Code, d.Message, d.Path, d.Span}}, nil
	}
	opt.gate = lazyIsEager
	return Run(path, p, opt)
}

// lazyIsEager is the commit gate that also runs the reference one, eager
// and serialising the working trees again, and fails the run when they
// disagree: the law, on every run through runText, that checking the
// input only when the output has findings, and trusting the sealed bytes
// to be the trees', give the same findings and the same refusal.
func lazyIsEager(before, after []byte, ws integrity.Workspace, log *integrity.OpLog, o integrity.GateOptions) (*integrity.Findings, error) {
	lf, lerr := integrity.GateEditWith(before, after, ws, log, o)
	o.Eager, o.Sealed, o.Mark = true, false, nil
	ef, eerr := integrity.GateEditWith(before, after, ws, log, o)
	if got, want := sortedInstances(lf), sortedInstances(ef); !reflect.DeepEqual(got, want) {
		return nil, fmt.Errorf("lazy gate findings %v, eager %v", got, want)
	}
	if lg, eg := gateFailureOf(lerr), gateFailureOf(eerr); !reflect.DeepEqual(lg, eg) || (lg == nil) != (lerr == nil) || (eg == nil) != (eerr == nil) {
		return nil, fmt.Errorf("lazy gate %v, eager %v", lerr, eerr)
	}
	return lf, lerr
}

// sortedInstances are the findings' instances in a stable order (some
// checks find them in map order).
// gateFailureOf is the gate's refusal in err, or nil.
func gateFailureOf(err error) *integrity.GateFailure {
	var g *integrity.GateFailure
	if errors.As(err, &g) {
		return g
	}
	return nil
}

func sortedInstances(f *integrity.Findings) []integrity.Instance {
	if f == nil {
		return nil
	}
	out := f.Instances()
	slices.SortFunc(out, func(a, b integrity.Instance) int {
		return cmp.Or(strings.Compare(a.Part, b.Part), strings.Compare(a.Message, b.Message), slices.Compare(a.Nodes, b.Nodes))
	})
	return out
}

// TestRegionScoping shows the write set is the region's, declared before
// anything changes: an edit inside a paragraph-scoped region verifies; one
// that widens to another paragraph, or a settle whose scope outgrows its
// region, is refused by the region (ErrOutside) and, had it been made,
// would fail verification.
func TestRegionScoping(t *testing.T) {
	body := `<w:p w14:paraId="0000000A"><w:ins w:id="1" w:author="Bob"><w:r><w:t>a</w:t></w:r></w:ins></w:p>` +
		`<w:p w14:paraId="0000000B"><w:ins w:id="2" w:author="Bob"><w:r><w:t>b</w:t></w:r></w:ins></w:p>`
	for _, c := range []struct {
		name  string
		edit  func(ws *docxedit.Workspace, part string, x *model.XML) error
		valid bool
	}{
		{"settle inside the region", func(ws *docxedit.Workspace, part string, x *model.XML) error {
			p := model.ParagraphsIn(x.Body())[0].Node
			_, err := revise.Settle(mustRange(t, ws, part, p), p, revise.Options{Accept: true})
			return err
		}, true},
		{"edit widened to the next paragraph", func(ws *docxedit.Workspace, part string, x *model.XML) error {
			ps := model.ParagraphsIn(x.Body())
			return mustRange(t, ws, part, ps[0].Node).RemoveNode(model.ChildW(ps[1].Node, "ins"))
		}, false},
		{"settle scope wider than its region", func(ws *docxedit.Workspace, part string, x *model.XML) error {
			p := model.ParagraphsIn(x.Body())[0].Node
			_, err := revise.Settle(mustRange(t, ws, part, p), x.Body(), revise.Options{Accept: true})
			return err
		}, false},
	} {
		path := writeDocx(t, body)
		var ws *docxedit.Workspace
		tx, err := plan.Begin(path, func(pkg *opc.Package) plan.Workspace {
			ws, _ = docxedit.New(pkg, docxedit.Options{}) //nolint:errcheck // the fixture opens
			return ws
		})
		if err != nil {
			t.Fatal(err)
		}
		part := ws.DocumentPart()
		doc, err := ws.Tree(part)
		if err != nil {
			t.Fatal(err)
		}
		err = tx.Run(c.name, func() error { return c.edit(ws, part, &model.XML{Doc: doc}) })
		if c.valid {
			if err != nil {
				t.Fatalf("%s: %v", c.name, err)
			}
			if err := ws.Verify(); err != nil {
				t.Errorf("%s: %v", c.name, err)
			}
		} else if !errors.Is(err, core.ErrOutside) {
			if verr := ws.Verify(); !errors.Is(verr, plan.ErrUndeclared) {
				t.Errorf("%s: edit %v, Verify %v: want ErrOutside or ErrUndeclared", c.name, err, verr)
			}
		}
		tx.Rollback()
	}
}

// TestGateCountsInstances: a new instance of a fault kind the input
// already has is refused; the input's own instances never are.
func TestGateCountsInstances(t *testing.T) {
	one := `<w:p><w:bookmarkStart w:id="1" w:name="a"/><w:r><w:t>a</w:t></w:r></w:p>`
	two := one + `<w:p><w:bookmarkStart w:id="2" w:name="b"/><w:r><w:t>b</w:t></w:r></w:p>`
	read := func(body string) []byte {
		b, err := os.ReadFile(writeDocx(t, body))
		if err != nil {
			t.Fatal(err)
		}
		return b
	}
	in, same, more := read(one), read(one+`<w:p><w:r><w:t>c</w:t></w:r></w:p>`), read(two)
	if integrity.Check(in).Empty() {
		t.Fatal("the input has no fault to start from")
	}
	if err := integrity.Gate(in, same); err != nil {
		t.Errorf("an edit beside an existing fault was refused: %v", err)
	}
	if err := integrity.Gate(in, more); err == nil {
		t.Error("a second instance of an existing fault kind was let through")
	}
}

// TestExistingFaultsDoNotBlock settles every DOCX_CORPUS document that
// has both integrity findings and revisions: the save succeeds, and every
// part the plan did not change comes out byte for byte.
func TestExistingFaultsDoNotBlock(t *testing.T) {
	dir := os.Getenv("DOCX_CORPUS")
	if dir == "" {
		t.Skip("DOCX_CORPUS not set")
	}
	var paths []string
	err := filepath.WalkDir(dir, func(p string, d os.DirEntry, err error) error {
		if err == nil && !d.IsDir() && strings.HasSuffix(p, ".docx") {
			paths = append(paths, p)
		}
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Parallel() // beside the other law suites (corpus_test.go)
	// Document by document in parallel; each reports whether it was an
	// input.
	n := 0
	for _, r := range corpustest.Run(t, inputFiles(t, paths), func(t *testing.T, f corpustest.File, settled *bool) {
		p := f.Path
		src, err := os.ReadFile(p) //nolint:gosec // G304: the corpus the caller named
		if err != nil {
			t.Fatal(err)
		}
		doc, err := model.OpenDocument(src)
		if err != nil || len(doc.XML.RevisionElements()) == 0 || integrity.Check(src).Empty() {
			return // not an input for this test
		}
		*settled = true
		for _, action := range []string{"accept", "reject"} {
			path := filepath.Join(t.TempDir(), "in.docx")
			if err := os.WriteFile(path, src, 0o600); err != nil {
				t.Fatal(err)
			}
			c := commit(t, path, planText(t, path, "track", "all", action))
			if c.Outcome != Committed {
				t.Errorf("%s %s: %+v", filepath.Base(p), action, c.Diagnostic)
				continue
			}
			out, err := os.ReadFile(path) //nolint:gosec // G304: the test's own copy
			if err != nil {
				t.Fatal(err)
			}
			changed := map[string]bool{}
			for _, name := range c.Parts {
				changed[strings.TrimPrefix(name, "/")] = true
			}
			a, b := zipEntries(t, src), zipEntries(t, out)
			for name, data := range a {
				if !changed[name] && !bytes.Equal(b[name], data) {
					t.Errorf("%s %s: untouched part %s changed", filepath.Base(p), action, name)
				}
			}
		}
	}) {
		if r.Value {
			n++
		}
	}
	if n == 0 && !corpustest.Sharded() {
		t.Error("no corpus document with findings and revisions")
	}
	t.Logf("%d corpus documents with existing findings settled", n)
}

func zipEntries(t *testing.T, b []byte) map[string][]byte {
	t.Helper()
	zr, err := zip.NewReader(bytes.NewReader(b), int64(len(b)))
	if err != nil {
		t.Fatal(err)
	}
	out := map[string][]byte{}
	for _, f := range zr.File {
		rc, err := f.Open()
		if err != nil {
			t.Fatal(err)
		}
		var buf bytes.Buffer
		_, err = buf.ReadFrom(rc)
		if cerr := rc.Close(); err != nil || cerr != nil {
			t.Fatal(err, cerr)
		}
		out[f.Name] = buf.Bytes()
	}
	return out
}

// mustRange opens a region over one unit.
func mustRange(t *testing.T, ws *docxedit.Workspace, part string, u *tree.Node) *core.Handle {
	t.Helper()
	h, err := ws.Core().Range(part, "test", u, u)
	if err != nil {
		t.Fatal(err)
	}
	return h
}

// TestDefectElsewhereDoesNotBlock: an operation on one paragraph saves
// although the same part has a fault of the same kind in another
// paragraph, which comes out as it was.
func TestDefectElsewhereDoesNotBlock(t *testing.T) {
	path := writeDocx(t, `<w:p w14:paraId="0000000A"><w:ins w:id="1" w:author="Bob"><w:r><w:t>a</w:t></w:r></w:ins></w:p>`+
		`<w:p w14:paraId="0000000B"><w:bookmarkStart w:id="5" w:name="broken"/><w:r><w:t>b</w:t></w:r></w:p>`)
	input, _ := os.ReadFile(path) //nolint:errcheck // checked below
	if integrity.Check(input).Empty() {
		t.Fatal("the input has no fault")
	}
	c := commit(t, path, planText(t, path, "track", "1", "accept"))
	if c.Outcome != Committed {
		t.Fatalf("%+v", c.Diagnostic)
	}
	out, _ := os.ReadFile(path) //nolint:errcheck // checked below
	if !bytes.Contains(zipEntries(t, out)["word/document.xml"], []byte(`<w:p w14:paraId="0000000B"><w:bookmarkStart w:id="5" w:name="broken"/><w:r><w:t>b</w:t></w:r></w:p>`)) {
		t.Error("the untouched paragraph with the fault changed")
	}
	if len(c.Hazards) == 0 {
		t.Error("the pre-existing fault is not reported as a hazard")
	}
}

// TestBrokenContentTypesStillSave: a package whose content types stream
// cannot register parts still takes a plan that adds no part.
func TestBrokenContentTypesStillSave(t *testing.T) {
	path := writeDocx(t, `<w:p w14:paraId="0000000A"><w:ins w:id="1" w:author="Bob"><w:r><w:t>a</w:t></w:r></w:ins></w:p>`)
	src, _ := os.ReadFile(path) //nolint:errcheck // checked below
	var b bytes.Buffer
	z := zip.NewWriter(&b)
	for name, data := range zipEntries(t, src) {
		if name == "[Content_Types].xml" {
			data = bytes.Replace(data, []byte("<Types "), []byte("<Typez "), 1)
			data = bytes.Replace(data, []byte("</Types>"), []byte("</Typez>"), 1)
		}
		w, err := z.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write(data); err != nil {
			t.Fatal(err)
		}
	}
	if err := z.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, b.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}
	c := commit(t, path, planText(t, path, "track", "all", "accept"))
	if c.Outcome != Committed || len(c.Parts) != 1 {
		t.Fatalf("%s %+v %v", c.Outcome, c.Diagnostic, c.Parts)
	}
}

// addresses are the main document's paragraph addresses as the read
// model resolves them.
func addresses(t *testing.T, path string) map[string]bool {
	t.Helper()
	b, err := os.ReadFile(path) //nolint:gosec // G304: the test's own copy
	if err != nil {
		t.Fatal(err)
	}
	d, err := model.OpenDocument(b)
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]bool{}
	for _, id := range d.ParaIDs().IDs {
		out[id.ID] = true
	}
	return out
}

// TestAddressesStableAcrossSaves: paragraph addresses survive two
// consecutive saves on documents with out-of-range, duplicate and missing
// stored ids (settle one revision, then all): every address the second
// and the third version show, the version before showed too.
func TestAddressesStableAcrossSaves(t *testing.T) {
	var files []string
	if dir := os.Getenv("DOCX_CORPUS"); dir != "" {
		files = append(files, filepath.Join(dir, "ooxmlexport", "tdf154478.docx"), filepath.Join(dir, "ooxmlexport", "tdf154481.docx"))
	}
	if repo := os.Getenv("DOCXDRIVER_REPO"); repo != "" {
		files = append(files, filepath.Join(repo, "crates", "docxdriver-core", "tests", "fixtures", "projection-parity", "parity.docx"))
	}
	if len(files) == 0 {
		t.Skip("DOCX_CORPUS and DOCXDRIVER_REPO not set")
	}
	for _, f := range files {
		src, err := os.ReadFile(f) //nolint:gosec // G304: the corpus the caller named
		if err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(t.TempDir(), "in.docx")
		if err := os.WriteFile(path, src, 0o600); err != nil {
			t.Fatal(err)
		}
		before := addresses(t, path)
		for i, target := range []string{firstID(path), "all"} {
			c := commit(t, path, planText(t, path, "track", target, "accept"))
			if c.Outcome != Committed || len(c.Parts) == 0 {
				t.Fatalf("%s save %d: %s %+v", filepath.Base(f), i+1, c.Outcome, c.Diagnostic)
			}
			after := addresses(t, path)
			for id := range after {
				if !before[id] {
					t.Errorf("%s save %d: address %s is new", filepath.Base(f), i+1, id)
				}
			}
			before = after
		}
	}
}
