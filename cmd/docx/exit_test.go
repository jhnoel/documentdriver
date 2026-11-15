package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"testing"
	"time"

	"github.com/jhnoel/documentdriver/internal/cli"
	"github.com/jhnoel/documentdriver/internal/docx/create"
	"github.com/jhnoel/documentdriver/internal/docx/integrity"
	"github.com/jhnoel/documentdriver/internal/docx/math"
	"github.com/jhnoel/documentdriver/internal/docx/read"
	"github.com/jhnoel/documentdriver/internal/repo"
)

// The P4 exit criteria that are properties of the CLI (docs/plan.md):
// a plan whose operations change nothing writes nothing, and create's
// output passes the created-file gate, the Open XML SDK validator and
// reads back. Schema drift is TestCommittedOutputs in tools/schemagen.

// TestNoOpPlanWritesNothing: plans whose operations all apply but change
// nothing (accept or reject every revision of a document that has none,
// format a paragraph to what it has) leave the file as it was, bytes and
// mtime, in place and in both modes, and report the input's hash as
// new_base.
func TestNoOpPlanWritesNothing(t *testing.T) {
	doc := filepath.Join(t.TempDir(), "plain.docx")
	b, err := create.Build(`<p style="text-align:center">Hello</p><p>World</p>`)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(doc, b, 0o644); err != nil {
		t.Fatal(err)
	}
	_, found, _ := runCLI(t, "find", doc, "Hello")
	id := regexp.MustCompile(`"id":"([0-9A-F]{8})"`).FindStringSubmatch(found)
	if id == nil {
		t.Fatalf("no paragraph id in %s", found)
	}
	old := time.Now().Add(-48 * time.Hour).Truncate(time.Second)
	if err := os.Chtimes(doc, old, old); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(doc)
	if err != nil {
		t.Fatal(err)
	}
	for _, mode := range []string{"track", "direct"} {
		for _, ops := range []string{
			`[{"op":"revision.accept","all":true}]`,
			`[{"op":"paragraph.format","at":"` + id[1] + `","alignment":"center"}]`,
			`[{"op":"revision.reject","all":true},{"op":"paragraph.format","at":"` + id[1] + `","alignment":"center"}]`,
		} {
			plan := `{"author":"Ann","change_mode":"` + mode + `","ops":` + ops + `}`
			code, res, errs := applyPlan(t, doc, plan)
			if code != 0 || res["new_base"] != cli.Hash(before) || res["saved"] != nil || len(res["parts"].([]any)) != 0 {
				t.Errorf("%s %s: %d %v %+v", mode, ops, code, res, errs)
			}
			after, err := os.ReadFile(doc)
			if err != nil {
				t.Fatal(err)
			}
			st, err := os.Stat(doc)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(after, before) || !st.ModTime().Equal(old) {
				t.Fatalf("%s %s: the file changed (mtime %v)", mode, ops, st.ModTime())
			}
		}
	}
	ents, err := os.ReadDir(filepath.Dir(doc))
	if err != nil {
		t.Fatal(err)
	}
	// The document and the plans applyPlan wrote; no temporary file left.
	for _, e := range ents {
		if filepath.Ext(e.Name()) != ".docx" && filepath.Ext(e.Name()) != ".json" {
			t.Errorf("left behind: %s", e.Name())
		}
	}
}

// TestNoOpPlanWithOutWritesNothing: a no-op plan given -o to a new path
// reports new_base equal to base, omits saved, and does not create the
// output file.
func TestNoOpPlanWithOutWritesNothing(t *testing.T) {
	doc := filepath.Join(t.TempDir(), "plain.docx")
	b, err := create.Build(`<p style="text-align:center">Hello</p><p>World</p>`)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(doc, b, 0o644); err != nil {
		t.Fatal(err)
	}
	_, found, _ := runCLI(t, "find", doc, "Hello")
	id := regexp.MustCompile(`"id":"([0-9A-F]{8})"`).FindStringSubmatch(found)
	if id == nil {
		t.Fatalf("no paragraph id in %s", found)
	}
	before, err := os.ReadFile(doc)
	if err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(t.TempDir(), "out.docx")
	plan := `{"author":"Ann","change_mode":"track","ops":[{"op":"paragraph.format","at":"` + id[1] + `","alignment":"center"}]}`
	code, res, errs := applyPlan(t, doc, plan, "-o", out)
	if code != 0 || res["saved"] != nil || res["new_base"] != cli.Hash(before) {
		t.Fatalf("no-op -o: exit %d saved %v new_base %v: %+v", code, res["saved"], res["new_base"], errs)
	}
	if _, err := os.Stat(out); err == nil {
		t.Fatalf("no-op -o wrote %s", out)
	}
	after, err := os.ReadFile(doc)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(after, before) {
		t.Fatal("no-op -o changed the input")
	}
}

// createSample is create's input with every element P4 builds.
const createSample = `<h1>Title <i>here</i></h1>
<h2>A section</h2>
<p>Plain <b>bold</b>, <i>italic</i>, <u>under</u>, <s>struck</s>, H<sub>2</sub>O, x<sup>2</sup>, <mark>marked</mark> and <a href="https://example.com/x">a link</a>.</p>
<p class="Quote" style="text-align:center">Quoted<br/>line&#9;tab</p>
<p>Inline <equation display="inline">\frac{a}{b}</equation> and a block:</p>
<p><equation display="block">\sum_{i=1}^{n} i^2</equation></p>
<h3>Page <page-number></page-number> of <page-count></page-count></h3>`

// TestCreateExitCriteria: create's output has no integrity finding at
// all (GateCreated), the Open XML SDK validator finds no error in it
// when OPENXML_VALIDATOR names its build (bin/openxml-validator/
// openxml-validator.dll, which make conformance builds; skipped
// otherwise), and docx read reads it back with what was written.
func TestCreateExitCriteria(t *testing.T) {
	out, err := create.Build(createSample)
	if err != nil {
		t.Fatal(err)
	}
	if err := integrity.GateCreated(out); err != nil {
		t.Fatalf("GateCreated: %v", err)
	}
	res, err := read.Read(out, "document", "", read.Options{Equation: math.OMMLToLaTeX})
	if err != nil {
		t.Fatal(err)
	}
	s, err := read.Encode(res.Object(), false)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`<h1 `, `<h2 `, `<b>bold</b>`, `<i>italic</i>`, `<sub>2</sub>`, `<sup>2</sup>`, `https://example.com/x`, `\\frac{a}{b}`, `i^{2}`, `Quote`, `PAGE`, `NUMPAGES`} {
		if !bytes.Contains([]byte(s), []byte(want)) {
			t.Errorf("read back has no %s", want)
		}
	}
	path := filepath.Join(t.TempDir(), "created.docx")
	if err := os.WriteFile(path, out, 0o600); err != nil {
		t.Fatal(err)
	}
	t.Run("sdk-validator", func(t *testing.T) { sdkValidate(t, path) })
}

// sdkValidate runs the Open XML SDK validator on path and fails on any
// error, or skips when OPENXML_VALIDATOR is not set (a relative value is
// taken from the repository root).
func sdkValidate(t *testing.T, path string) {
	t.Helper()
	dll := os.Getenv("OPENXML_VALIDATOR")
	if dll == "" {
		t.Skip("OPENXML_VALIDATOR not set: build tools/openxml-validator (make conformance does) and name its dll")
	}
	if !filepath.IsAbs(dll) {
		root, err := repo.Root()
		if err != nil {
			t.Fatal(err)
		}
		dll = filepath.Join(root, dll)
	}
	cmd := exec.Command("dotnet", dll) //nolint:gosec // G204: the validator the developer named
	cmd.Stdin = bytes.NewBufferString(path + "\n")
	b, err := cmd.Output()
	if err != nil {
		t.Fatalf("validator: %v", err)
	}
	sc := bufio.NewScanner(bytes.NewReader(b))
	sc.Buffer(nil, 1<<24)
	for sc.Scan() {
		var r struct {
			Errors []map[string]string `json:"errors"`
			Fatal  string              `json:"fatal"`
		}
		if err := json.Unmarshal(sc.Bytes(), &r); err != nil {
			t.Fatalf("validator output: %v", err)
		}
		if r.Fatal != "" || len(r.Errors) > 0 {
			t.Errorf("validator: fatal %q, errors %v", r.Fatal, r.Errors)
		}
	}
}
