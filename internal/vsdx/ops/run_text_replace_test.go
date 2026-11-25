package ops_test

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jhnoel/documentdriver/internal/cli"
	"github.com/jhnoel/documentdriver/internal/laws"
	"github.com/jhnoel/documentdriver/internal/ooxml/tree"
	"github.com/jhnoel/documentdriver/internal/opc"
	"github.com/jhnoel/documentdriver/internal/plan"
	"github.com/jhnoel/documentdriver/internal/repo"
	"github.com/jhnoel/documentdriver/internal/vsdx/apply"
	"github.com/jhnoel/documentdriver/internal/vsdx/doc"
	"github.com/jhnoel/documentdriver/internal/vsdx/edit"
	"github.com/jhnoel/documentdriver/internal/vsdx/inherit"
	"github.com/jhnoel/documentdriver/internal/vsdx/ops"
	"github.com/jhnoel/documentdriver/internal/vsdx/sheet"
	"github.com/jhnoel/documentdriver/internal/vsdx/vsd"
	"github.com/jhnoel/documentdriver/internal/vsdx/vsdxtest"
)

// replacePlan returns a one-op text.replace plan.
func replacePlan(at, sel, with string, occurrence int, all bool) *ops.Plan {
	return &ops.Plan{Ops: []ops.Spec{&ops.TextReplaceSpec{At: at, Select: sel, With: with, Occurrence: occurrence, All: all}}}
}

// replaceRun applies plan to the fixture name and returns the result
// and the output bytes (empty when nothing was written).
func replaceRun(t *testing.T, fixture string, p *ops.Plan) (*plan.Result, []byte) {
	t.Helper()
	path := vsdxtest.Fixture(t, fixture)
	out := filepath.Join(t.TempDir(), "out.vsdx")
	res, err := apply.Run(path, p, apply.Options{Out: out})
	if err != nil {
		t.Fatalf("Run(%s): %v", fixture, err)
	}
	data, err := os.ReadFile(out)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return res, nil
		}
		t.Fatalf("read out: %v", err)
	}
	return res, data
}

// TestTextReplaceV1 checks the TRP row: V1 s1 "Hello" to "Bye" reads
// back as html `<p><b>Bye</b></p>` and text "Bye".
func TestTextReplaceV1(t *testing.T) {
	res, data := replaceRun(t, "v1-minimal", replacePlan("page0/s1", "Hello", "Bye", 0, false))
	if len(res.Changed) != 1 || !strings.Contains(string(res.Changed[0]), `"id":"page0/s1"`) {
		t.Fatalf("changed %v, want [page0/s1]", res.Changed)
	}
	obj := textSetObject(t, data, "page0", "page0/s1")
	if got, _ := obj["html"].(string); got != `<p><b>Bye</b></p>` {
		t.Errorf("html = %q, want <p><b>Bye</b></p>", got)
	}
	if got, _ := obj["text"].(string); got != "Bye" {
		t.Errorf("text = %q, want Bye", got)
	}
}

// TestTextReplaceV2Master checks the TRP row: V2 at page0 "Process"
// to "Step" with all gives s1 the local Text `<cp IX='0'/>Step\n`
// with no local Character or Paragraph row, html `<p>Step</p>` with
// text origin local, and the resolver's Character row 0 Style still
// "2" with origin master.
func TestTextReplaceV2Master(t *testing.T) {
	res, data := replaceRun(t, "v2-master", replacePlan("page0", "Process", "Step", 0, true))
	if len(res.Changed) != 1 || !strings.Contains(string(res.Changed[0]), `"id":"page0/s1"`) {
		t.Fatalf("changed %v, want [page0/s1]", res.Changed)
	}
	obj := textSetObject(t, data, "page0", "page0/s1")
	if got, _ := obj["html"].(string); got != `<p>Step</p>` {
		t.Errorf("html = %q, want <p>Step</p>", got)
	}
	origins, _ := obj["origins"].(map[string]any)
	if origins["html"] != "local" {
		t.Errorf("html origin = %v, want local", origins["html"])
	}
	if got := textSetText(t, data, 1); got != "<Text><cp IX='0'/>Step\n</Text>" {
		t.Errorf("local Text = %q, want <Text><cp IX='0'/>Step\\n</Text>", got)
	}
	// No local Character or Paragraph row, and the master row 0
	// Style is still the effective one, from the master.
	pkg, err := opc.OpenBytes(data, opc.DefaultLimits())
	if err != nil {
		t.Fatalf("open output: %v", err)
	}
	defer func() { _ = pkg.Close() }()
	d, err := doc.Open(pkg)
	if err != nil {
		t.Fatalf("doc.Open(output): %v", err)
	}
	var entry *doc.PageEntry
	for i := range d.Pages {
		if d.Pages[i].ID == 0 {
			entry = &d.Pages[i]
			break
		}
	}
	if entry == nil {
		t.Fatal("no page0")
	}
	contents, err := d.PageContents(entry)
	if err != nil {
		t.Fatalf("PageContents: %v", err)
	}
	var shape *tree.Node
	contents.Root().Walk(func(n *tree.Node) bool {
		if shape != nil {
			return false
		}
		if n.Kind() == tree.ElementNode && n.Local() == vsd.ElemShape {
			if v, ok := n.Attr("ID"); ok && v == "1" {
				shape = n
				return false
			}
		}
		return true
	})
	if shape == nil {
		t.Fatal("no shape 1")
	}
	for c := range shape.ElementsSeq() {
		if c.Local() == vsd.ElemSection {
			if n, _ := c.Attr("N"); n == vsd.SectionCharacter || n == vsd.SectionParagraph {
				t.Errorf("s1 has a local %s section after text.replace", n)
			}
		}
	}
	sh := &inherit.Shape{Master: 2, HasMaster: true}
	sh.Sheet, _ = sheet.Index(shape)
	r := inherit.New(d)
	cell, origin, ok := r.Cell(sh, sheet.IndexedRowCell(vsd.SectionCharacter, -1, 0, "").WithCell("Style"))
	if !ok || cell.V != "2" || origin != inherit.Master {
		t.Errorf("Character row 0 Style = %q origin %q ok %v, want 2 master true", cell.V, origin, ok)
	}
}

// TestTextReplaceV2Identity checks the TRP row: text.set of the V2
// result's html `<p>Step</p>` on the result writes nothing.
func TestTextReplaceV2Identity(t *testing.T) {
	_, data := replaceRun(t, "v2-master", replacePlan("page0", "Process", "Step", 0, true))
	path := filepath.Join(t.TempDir(), "result.vsdx")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatalf("write result: %v", err)
	}
	out := filepath.Join(t.TempDir(), "out.vsdx")
	res, err := apply.Run(path, textSetPlan("page0/s1", `<p>Step</p>`), apply.Options{Out: out})
	if err != nil {
		t.Fatalf("identity text.set: %v", err)
	}
	if _, statErr := os.Stat(out); !errors.Is(statErr, os.ErrNotExist) {
		t.Error("identity text.set wrote its out file")
	}
	if len(res.Parts) != 0 || len(res.Changed) != 0 {
		t.Errorf("identity result parts %v changed %v, want empty", res.Parts, res.Changed)
	}
}

// TestTextReplaceV7 checks the TRP row: V7 s1 "red" to "blue" gives
// golden/v7-page0.html with " red" replaced by " blue" in the bold
// red run.
func TestTextReplaceV7(t *testing.T) {
	root, err := repo.Root()
	if err != nil {
		t.Fatalf("repo root: %v", err)
	}
	golden, err := os.ReadFile(filepath.Join(root, "testdata", "vsdx", "golden", "v7-page0.html"))
	if err != nil {
		t.Fatalf("read golden: %v", err)
	}
	_, data := replaceRun(t, "v7-text", replacePlan("page0/s1", "red", "blue", 0, false))
	obj := textSetObject(t, data, "page0", "page0/s1")
	want := strings.Replace(string(golden), " red", " blue", 1)
	if got, _ := obj["html"].(string); got != want {
		t.Errorf("html = %q, want %q", got, want)
	}
}

// TestTextReplaceCrossRun checks that a match across runs keeps the
// first run's formatting: V7 "Title red" spans the plain run and the
// bold red run, and becomes "Intro" in the first run.
func TestTextReplaceCrossRun(t *testing.T) {
	_, data := replaceRun(t, "v7-text", replacePlan("page0/s1", "Title red", "Intro", 0, false))
	obj := textSetObject(t, data, "page0", "page0/s1")
	html, _ := obj["html"].(string)
	if !strings.Contains(html, "<p style=\"text-align:left\">Intro</p>") {
		t.Errorf("cross-run html = %q, want the first paragraph to be Intro with no bold red run", html)
	}
	if strings.Contains(html, "red") {
		t.Errorf("cross-run html = %q, want no red", html)
	}
}

// TestTextReplaceNotFound checks the TRP row: select "zzz" is
// not_found at ops[0].select and nothing is written.
func TestTextReplaceNotFound(t *testing.T) {
	path := vsdxtest.Fixture(t, "v1-minimal")
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	out := filepath.Join(t.TempDir(), "out.vsdx")
	_, err = apply.Run(path, replacePlan("page0/s1", "zzz", "x", 0, false), apply.Options{Out: out})
	var ce *cli.Error
	if !errors.As(err, &ce) {
		t.Fatalf("error %v, want *cli.Error", err)
	}
	if ce.Code != cli.CodeNotFound || ce.Path != "ops[0].select" {
		t.Errorf("got %s at %q, want not_found at ops[0].select", ce.Code, ce.Path)
	}
	if _, statErr := os.Stat(out); !errors.Is(statErr, os.ErrNotExist) {
		t.Error("a refused text.replace wrote its out file")
	}
	if got, readErr := os.ReadFile(path); readErr != nil || !bytes.Equal(got, before) {
		t.Error("a refused text.replace changed the input")
	}
}

// TestTextReplacePageSingle checks the TRP row's first half: V2 at
// page0 "Process" without occurrence or all is allowed when only
// s1 matches, and changes only s1.
func TestTextReplacePageSingle(t *testing.T) {
	res, data := replaceRun(t, "v2-master", replacePlan("page0", "Process", "Step", 0, false))
	if len(res.Changed) != 1 || !strings.Contains(string(res.Changed[0]), `"id":"page0/s1"`) {
		t.Fatalf("changed %v, want [page0/s1]", res.Changed)
	}
	obj := textSetObject(t, data, "page0", "page0/s2")
	if got, _ := obj["html"].(string); !strings.Contains(got, "Review") {
		t.Errorf("s2 html = %q, want Review unchanged", got)
	}
}

// TestTextReplaceAmbiguous checks the TRP row: V2 at page0
// "Process" without occurrence or all, with a second "Process" in a
// test-built shape, is ambiguous with matches 2 and writes nothing.
func TestTextReplaceAmbiguous(t *testing.T) {
	path := vsdxtest.Fixture(t, "v2-master")
	built := filepath.Join(t.TempDir(), "built.vsdx")
	add := &ops.Plan{Ops: []ops.Spec{&ops.ObjectAddSpec{At: "page0", Master: "master2", Pin: []float64{144, 126}, HTML: new("<p>Process</p>")}}}
	if _, err := apply.Run(path, add, apply.Options{Out: built}); err != nil {
		t.Fatalf("object.add second Process: %v", err)
	}
	before, err := os.ReadFile(built)
	if err != nil {
		t.Fatalf("read built: %v", err)
	}
	out := filepath.Join(t.TempDir(), "out.vsdx")
	_, err = apply.Run(built, replacePlan("page0", "Process", "Step", 0, false), apply.Options{Out: out})
	var ce *cli.Error
	if !errors.As(err, &ce) {
		t.Fatalf("error %v, want *cli.Error", err)
	}
	if ce.Code != cli.CodeAmbiguous || ce.Path != "ops[0].select" || ce.Matches != 2 {
		t.Errorf("got %s at %q matches %d, want ambiguous at ops[0].select matches 2", ce.Code, ce.Path, ce.Matches)
	}
	if _, statErr := os.Stat(out); !errors.Is(statErr, os.ErrNotExist) {
		t.Error("an ambiguous text.replace wrote its out file")
	}
	if got, readErr := os.ReadFile(built); readErr != nil || !bytes.Equal(got, before) {
		t.Error("an ambiguous text.replace changed the input")
	}
}

// TestTextReplaceOccurrence checks occurrence: the second "Ha" of a
// shape whose text was set to "Ha Ha" in the same plan is replaced,
// and the first is left.
func TestTextReplaceOccurrence(t *testing.T) {
	path := vsdxtest.Fixture(t, "v1-minimal")
	out := filepath.Join(t.TempDir(), "out.vsdx")
	p := &ops.Plan{Ops: []ops.Spec{
		&ops.TextSetSpec{At: "page0/s1", HTML: `<p>Ha Ha</p>`},
		&ops.TextReplaceSpec{At: "page0/s1", Select: "Ha", With: "Ho", Occurrence: 2},
	}}
	if _, err := apply.Run(path, p, apply.Options{Out: out}); err != nil {
		t.Fatalf("occurrence plan: %v", err)
	}
	data, err := os.ReadFile(out)
	if err != nil {
		t.Fatalf("read out: %v", err)
	}
	obj := textSetObject(t, data, "page0", "page0/s1")
	if got, _ := obj["text"].(string); got != "Ha Ho" {
		t.Errorf("text = %q, want Ha Ho", got)
	}
}

// TestTextReplaceIdentity checks that replacing text with itself
// writes nothing.
func TestTextReplaceIdentity(t *testing.T) {
	res, data := replaceRun(t, "v1-minimal", replacePlan("page0/s1", "Hello", "Hello", 0, false))
	if len(data) != 0 || len(res.Changed) != 0 {
		t.Errorf("identity replace changed %v and wrote %d bytes, want nothing", res.Changed, len(data))
	}
}

// TestTextReplaceRefuses checks master shapes, missing shapes and
// pages, and an unknown alias, each at its field, with nothing
// written.
func TestTextReplaceRefuses(t *testing.T) {
	for _, tc := range []struct {
		name string
		at   string
		code string
		path string
	}{
		{name: "master shape", at: "master2/s5", code: cli.CodeOperationFailed, path: "ops[0].at"},
		{name: "missing shape", at: "page0/s99", code: cli.CodeNotFound, path: "ops[0].at"},
		{name: "missing page", at: "page99", code: cli.CodeNotFound, path: "ops[0].at"},
		{name: "unknown alias", at: "$nope", code: cli.CodeNotFound, path: "ops[0].at"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := vsdxtest.Fixture(t, "v2-master")
			out := filepath.Join(t.TempDir(), "out.vsdx")
			_, err := apply.Run(path, replacePlan(tc.at, "Process", "Step", 0, true), apply.Options{Out: out})
			var ce *cli.Error
			if !errors.As(err, &ce) {
				t.Fatalf("error %v, want *cli.Error", err)
			}
			if ce.Code != tc.code || ce.Path != tc.path {
				t.Errorf("got %s at %q, want %s at %s", ce.Code, ce.Path, tc.code, tc.path)
			}
			if _, statErr := os.Stat(out); !errors.Is(statErr, os.ErrNotExist) {
				t.Error("a refused text.replace wrote its out file")
			}
		})
	}
}

// TestScalingTextReplace checks the scaling row: text.replace at a
// page of N shapes (N = 1000, 4N = 4000, each with text "Box <i>"),
// replacing "Box" with "Bin" in every shape with all, allocates at
// most 5.5 times as much per shape (laws.Scales) and the time ratio
// is at most 8 (laws.TimeScales).
func TestScalingTextReplace(t *testing.T) {
	setup := func(n int) (func() error, error) {
		path := vsdxtest.Generated(t, n)
		return func() error {
			w, err := edit.Begin(path)
			if err != nil {
				return err
			}
			defer w.Tx().Rollback()
			spec := &ops.TextReplaceSpec{At: "page0", Select: "Box", With: "Bin", All: true}
			_, changed, _, _, err := spec.Run(w, nil, "", nil)
			if err != nil {
				return err
			}
			if len(changed) != n {
				return errors.New("scaling text.replace changed " + itoa(len(changed)) + " shapes")
			}
			return w.Verify()
		}, nil
	}
	laws.Scales(t, "vsdx text.replace page shapes", setup)
	laws.TimeScales(t, "vsdx text.replace page shapes", setup)
}
