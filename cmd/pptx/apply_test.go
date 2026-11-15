package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jhnoel/documentdriver/internal/cli"
	"github.com/jhnoel/documentdriver/internal/testdeck"
)

// runErr runs a command that may fail and returns its exit code and the
// entries of the errors object on stderr. A failure must leave stdout
// empty and put exactly one errors object on stderr.
func runErr(t *testing.T, args ...string) (int, []cli.Error) {
	t.Helper()
	var out, errb bytes.Buffer
	code := run(args, &out, &errb)
	if code == 0 {
		if errb.Len() != 0 {
			t.Errorf("pptx %v: stderr on success: %s", args, errb.String())
		}
		return 0, nil
	}
	if out.Len() != 0 {
		t.Errorf("pptx %v: stdout on failure: %s", args, out.String())
	}
	var v struct {
		Errors []cli.Error `json:"errors"`
	}
	dec := json.NewDecoder(&errb)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&v); err != nil || len(v.Errors) == 0 || dec.More() {
		t.Fatalf("pptx %v: stderr is not one errors object: %v", args, err)
	}
	return code, v.Errors
}

// TestApplyReportsEveryPlanFault: faults found without the deck come back
// together, each with its code and path, before the deck is opened.
func TestApplyReportsEveryPlanFault(t *testing.T) {
	plan := filepath.Join(t.TempDir(), "plan.json")
	if err := os.WriteFile(plan, []byte(`{"ops": [
	  {"op": "object.set", "at": "slide1/s10", "bx": [1, 2, 3, 4], "rot": null},
	  {"op": "object.zap", "at": "slide1/s10"},
	  {"op": "object.add", "at": "slide1", "kind": "shape", "geom": "rect", "box": [0, 0, -5, 10]},
	  {"op": "text.replace", "at": "$nope", "select": "a", "with": "b", "all": true, "occurrence": 2},
	  {"op": "text.set", "at": "slide1/s3", "html": "<p><blink>x</blink></p>"}]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	// The deck does not exist: none of this needs it.
	code, errs := runErr(t, "apply", filepath.Join(t.TempDir(), "missing.pptx"), plan)
	var got []string
	for _, e := range errs {
		got = append(got, e.Code+" "+e.Path)
	}
	want := []string{"invalid_plan ops[0].bx", "invalid_plan ops[0].rot", "invalid_plan ops[1].op"}
	if code != 1 || strings.Join(got, "; ") != strings.Join(want, "; ") {
		t.Fatalf("decode faults: exit %d\n got %v\nwant %v", code, got, want)
	}
	if err := os.WriteFile(plan, []byte(`{"ops": [
	  {"op": "object.add", "at": "slide1", "kind": "shape", "geom": "rect", "box": [0, 0, -5, 10]},
	  {"op": "text.replace", "at": "$nope", "select": "a", "with": "b", "all": true, "occurrence": 2},
	  {"op": "text.set", "at": "slide1/s3", "html": "<p><blink>x</blink></p>"},
	  {"op": "object.reorder", "at": "slide1/s3", "to": "slide2/s4", "position": "before"},
	  {"op": "text.set", "at": "slide1/s3", "html": "<p class=\"x\">y</p>"}]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	code, errs = runErr(t, "apply", filepath.Join(t.TempDir(), "missing.pptx"), plan)
	got = nil
	for _, e := range errs {
		got = append(got, e.Code+" "+e.Path)
	}
	want = []string{"invalid_operation ops[0].name", "invalid_operation ops[0].box", "invalid_operation ops[1].at",
		"invalid_operation ops[1].occurrence", "invalid_operation ops[2].html<blink>", "invalid_operation ops[3].to", "invalid_operation ops[4].html@class"}
	if code != 1 || strings.Join(got, "; ") != strings.Join(want, "; ") {
		t.Fatalf("rule faults: exit %d\n got %v\nwant %v", code, got, want)
	}
}

// TestApplyTextSelection: one match needs nothing more, several are
// ambiguous with the count, occurrence picks one and all takes each.
func TestApplyTextSelection(t *testing.T) {
	deck := testdeck.Diagram(t)
	dir := t.TempDir()
	apply := func(op string) (int, []cli.Error) {
		plan := filepath.Join(dir, "plan.json")
		if err := os.WriteFile(plan, []byte(`{"ops": [`+op+`]}`), 0o644); err != nil {
			t.Fatal(err)
		}
		return runErr(t, "apply", deck, plan, "--dry-run")
	}
	code, errs := apply(`{"op": "text.replace", "at": "slide1/s10", "select": "a", "with": "A"}`)
	if code != 1 || errs[0].Code != cli.CodeAmbiguous || errs[0].Matches < 2 || errs[0].Path != "ops[0].select" {
		t.Fatalf("several matches: exit %d %+v", code, errs)
	}
	if code, errs := apply(`{"op": "text.replace", "at": "slide1/s10", "select": "a", "with": "A", "occurrence": 2}`); code != 0 {
		t.Errorf("occurrence 2: %+v", errs)
	}
	if code, errs := apply(`{"op": "text.replace", "at": "slide1/s10", "select": "a", "with": "A", "all": true}`); code != 0 {
		t.Errorf("all: %+v", errs)
	}
	code, errs = apply(`{"op": "text.replace", "at": "slide1/s10", "select": "a", "with": "A", "occurrence": 99}`)
	if code != 1 || errs[0].Code != cli.CodeNotFound {
		t.Errorf("occurrence past the last match: exit %d %+v", code, errs)
	}
	code, errs = apply(`{"op": "text.replace", "at": "slide1/s10", "select": "zzz", "with": "A"}`)
	if code != 1 || errs[0].Code != cli.CodeNotFound || errs[0].Path != "ops[0].select" {
		t.Errorf("no match: exit %d %+v", code, errs)
	}
}

func TestApplyDryRunAndOut(t *testing.T) {
	deck := testdeck.Diagram(t)
	before, _ := os.ReadFile(deck)
	plan := filepath.Join(t.TempDir(), "plan.json")
	if err := os.WriteFile(plan, []byte(`{"ops": [
	  {"op": "object.add", "at": "slide1", "as": "db", "kind": "shape", "name": "DB2", "geom": "can", "box": [400, 300, 60, 80], "fill": {"scheme": "accent2"}},
	  {"op": "object.add", "at": "slide1", "kind": "connector", "name": "to db", "geom": "straightConnector1",
	   "from": [200, 125], "to": [430, 300], "from_glue": "s10:2", "to_glue": "$db:0", "line": {"w": 1, "color": {"hex": "#000000"}, "tail": "triangle"}},
	  {"op": "object.delete", "at": "slide1/s11"}]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	var out, errb bytes.Buffer
	if code := run([]string{"apply", deck, plan, "--dry-run"}, &out, &errb); code != 0 {
		t.Fatalf("exit %d: %s", code, errb.String())
	}
	if after, _ := os.ReadFile(deck); !bytes.Equal(before, after) {
		t.Fatal("dry run wrote the deck")
	}
	var res struct {
		Base    string            `json:"base"`
		NewBase string            `json:"new_base"`
		Saved   string            `json:"saved"`
		DryRun  bool              `json:"dry_run"`
		Aliases map[string]string `json:"aliases"`
		Changed []map[string]any  `json:"changed"`
		Deleted []string          `json:"deleted"`
		Hazards []struct {
			Kind, ID string
			New      bool
		} `json:"hazards"`
		Parts []string `json:"parts"`
	}
	if err := json.Unmarshal(out.Bytes(), &res); err != nil {
		t.Fatalf("%v\n%s", err, out.String())
	}
	if !res.DryRun || res.Aliases["db"] != "slide1/s1000" || len(res.Changed) != 2 || len(res.Deleted) != 1 || res.Deleted[0] != "slide1/s11" {
		t.Fatalf("result:\n%s", out.String())
	}
	// A dry run names the input's hash and no new one.
	if res.Base != cli.Hash(before) || res.NewBase != "" || res.Saved != "" {
		t.Fatalf("dry run base %q new_base %q saved %q", res.Base, res.NewBase, res.Saved)
	}
	// Deleting s11 leaves s12's end glue dangling: reported as new.
	found := false
	for _, h := range res.Hazards {
		if h.Kind == "dangling-glue" && h.ID == "slide1/s12" && h.New {
			found = true
		}
	}
	if !found || len(res.Parts) != 1 {
		t.Fatalf("hazards/parts:\n%s", out.String())
	}
	dst := filepath.Join(t.TempDir(), "out.pptx")
	out.Reset()
	if code := run([]string{"apply", deck, plan, "-o", dst}, &out, &errb); code != 0 {
		t.Fatalf("exit %d: %s", code, errb.String())
	}
	written, err := os.ReadFile(dst)
	if err != nil {
		t.Fatalf("no output written: %v\n%s", err, out.String())
	}
	res.NewBase, res.Saved = "", ""
	if err := json.Unmarshal(out.Bytes(), &res); err != nil || res.Saved != dst || res.NewBase != cli.Hash(written) || res.Base != cli.Hash(before) {
		t.Fatalf("saved %q new_base %q: %v\n%s", res.Saved, res.NewBase, err, out.String())
	}
	// --changed ids shows addresses only.
	out.Reset()
	if code := run([]string{"apply", deck, plan, "--dry-run", "--changed", "ids"}, &out, &errb); code != 0 || !strings.Contains(out.String(), `"slide1/s1000"`) || strings.Contains(out.String(), `"geom"`) {
		t.Fatalf("--changed ids: exit %d\n%s", code, out.String())
	}
	// A stale base refuses the plan; the right one passes.
	for base, want := range map[string]int{"sha256:" + strings.Repeat("0", 64): 1, cli.Hash(before): 0} {
		stale := filepath.Join(t.TempDir(), "stale.json")
		if err := os.WriteFile(stale, []byte(`{"base": "`+base+`", "ops": [{"op": "object.move", "at": "slide1/s10", "dx": 1}]}`), 0o644); err != nil {
			t.Fatal(err)
		}
		code, errs := runErr(t, "apply", deck, stale, "--dry-run")
		if code != want || want == 1 && (len(errs) != 1 || errs[0].Code != cli.CodeStaleBase || errs[0].Path != "base") {
			t.Errorf("base %s: exit %d, errors %+v", base, code, errs)
		}
	}
	// A failing plan writes nothing and says so.
	if err := os.WriteFile(plan, []byte(`{"ops": [{"op": "object.delete", "at": "slide1/s404"}]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	code, errs := runErr(t, "apply", deck, plan)
	if code != 1 || len(errs) != 1 || errs[0].Code != cli.CodeNotFound || errs[0].Path != "ops[0]" {
		t.Fatalf("exit %d: %+v", code, errs)
	}
	if after, _ := os.ReadFile(deck); !bytes.Equal(before, after) {
		t.Fatal("failed plan wrote the deck")
	}
}

// TestApplyTextReportsChanged: text operations report the changed object
// as pptx read --json shows it, html included.
func TestApplyTextReportsChanged(t *testing.T) {
	deck := testdeck.Diagram(t)
	plan := filepath.Join(t.TempDir(), "plan.json")
	if err := os.WriteFile(plan, []byte(`{"ops": [
	  {"op": "text.replace", "at": "slide1/s10", "select": "Gateway", "with": "Proxy"},
	  {"op": "text.set", "at": "slide1/s3", "html": "<p>one</p><p data-level=\"2\">2</p>"},
	  {"op": "object.add", "at": "slide1", "as": "n", "kind": "shape", "name": "Note", "geom": "rect", "box": [10, 10, 50, 20], "html": "<b>Note</b>"}]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	var out, errb bytes.Buffer
	if code := run([]string{"apply", deck, plan, "--dry-run"}, &out, &errb); code != 0 {
		t.Fatalf("exit %d: %s", code, errb.String())
	}
	var res struct {
		Changed []struct {
			ID   string `json:"id"`
			HTML string `json:"html"`
		} `json:"changed"`
	}
	if err := json.Unmarshal(out.Bytes(), &res); err != nil {
		t.Fatalf("%v\n%s", err, out.String())
	}
	got := map[string]string{}
	for _, o := range res.Changed {
		got[o.ID] = o.HTML
	}
	if len(got) != 3 || !strings.Contains(got["slide1/s10"], "Proxy") || got["slide1/s3"] != `<p>one</p><p data-level="2">2</p>` || got["slide1/s1000"] != `<p><b>Note</b></p>` {
		t.Fatalf("changed:\n%s", out.String())
	}
}

// TestApplySeedClock covers the hidden deterministic flags: the same seed
// gives byte-identical decks and bad values exit 2.
func TestApplySeedClock(t *testing.T) {
	deck := testdeck.Diagram(t)
	dir := t.TempDir()
	plan := filepath.Join(dir, "plan.json")
	if err := os.WriteFile(plan, []byte(`{"ops": [{"op": "object.duplicate", "at": "slide1/s10", "dx": 10}]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	apply := func(args ...string) []byte {
		t.Helper()
		dst := filepath.Join(dir, "out.pptx")
		var out, errb bytes.Buffer
		if code := run(append([]string{"apply", deck, plan, "-o", dst}, args...), &out, &errb); code != 0 {
			t.Fatalf("exit %d: %s", code, errb.String())
		}
		b, err := os.ReadFile(dst)
		if err != nil {
			t.Fatal(err)
		}
		return b
	}
	a := apply("--seed", "5", "--clock", "2026-01-02T03:04:05Z")
	if b := apply("--seed=5", "--clock=2026-01-02T03:04:05Z"); !bytes.Equal(a, b) {
		t.Error("same seed and clock: decks differ")
	}
	// The fixture has no a16:creationId, so another seed changes nothing
	// here; TestDeterministicBenchPlans covers seeds on the corpus.
	if c := apply("--seed", "6"); !bytes.Equal(a, c) {
		t.Error("seed changed a deck that needs no GUID")
	}
	var out, errb bytes.Buffer
	if code := run([]string{"apply", deck, plan, "--seed", "x"}, &out, &errb); code != 2 {
		t.Errorf("bad seed: exit %d", code)
	}
	if code := run([]string{"apply", deck, plan, "--clock", "yesterday"}, &out, &errb); code != 2 {
		t.Errorf("bad clock: exit %d", code)
	}
}

// TestApplyNoOpWithOutWritesNothing: a no-op plan given -o to a new path
// reports new_base equal to base, omits saved, and does not create the
// output file.
func TestApplyNoOpWithOutWritesNothing(t *testing.T) {
	deck := testdeck.Diagram(t)
	before, err := os.ReadFile(deck)
	if err != nil {
		t.Fatal(err)
	}
	plan := filepath.Join(t.TempDir(), "plan.json")
	if err := os.WriteFile(plan, []byte(`{"ops": [{"op": "object.move", "at": "slide1/s10", "dx": 0, "dy": 0}]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	dst := filepath.Join(t.TempDir(), "out.pptx")
	var out, errb bytes.Buffer
	if code := run([]string{"apply", deck, plan, "-o", dst}, &out, &errb); code != 0 {
		t.Fatalf("exit %d: %s", code, errb.String())
	}
	var res struct {
		Base    string `json:"base"`
		NewBase string `json:"new_base"`
		Saved   string `json:"saved"`
	}
	if err := json.Unmarshal(out.Bytes(), &res); err != nil {
		t.Fatalf("%v\n%s", err, out.String())
	}
	if res.Saved != "" || res.NewBase != cli.Hash(before) || res.Base != cli.Hash(before) {
		t.Fatalf("no-op -o: base %q new_base %q saved %q\n%s", res.Base, res.NewBase, res.Saved, out.String())
	}
	if _, err := os.Stat(dst); err == nil {
		t.Fatalf("no-op -o wrote %s", dst)
	}
	after, err := os.ReadFile(deck)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(after, before) {
		t.Fatal("no-op -o changed the input")
	}
}
