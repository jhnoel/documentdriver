package main

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jhnoel/documentdriver/internal/cli"
	"github.com/jhnoel/documentdriver/internal/docx/ops"
	"github.com/jhnoel/documentdriver/internal/repo"
)

// runCLI runs the command line. A failure must leave stdout empty and put
// exactly one errors object on stderr; a success must leave stderr empty.
func runCLI(t *testing.T, args ...string) (code int, stdout string, errs []cli.Error) {
	t.Helper()
	var out, errb bytes.Buffer
	code = run(args, &out, &errb, options())
	if code == 0 {
		if errb.Len() != 0 {
			t.Errorf("docx %v: stderr on success: %s", args, errb.String())
		}
		return 0, out.String(), nil
	}
	if out.Len() != 0 {
		t.Errorf("docx %v: stdout on failure: %s", args, out.String())
	}
	var v struct {
		Errors []cli.Error `json:"errors"`
	}
	dec := json.NewDecoder(&errb)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&v); err != nil || len(v.Errors) == 0 || dec.More() {
		t.Fatalf("docx %v: stderr is not one errors object: %v", args, err)
	}
	return code, "", v.Errors
}

func writeDocx(t *testing.T, path, document string) {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	w, err := zw.Create("word/document.xml")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.Write([]byte(document)); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, buf.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
}

const helloDoc = `<w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main" xmlns:w14="http://schemas.microsoft.com/office/word/2010/wordml"><w:body><w:p w14:paraId="00000001"><w:r><w:t>Hello world</w:t></w:r></w:p></w:body></w:document>`

// rich copies the authored fixture (tracked changes, comments, a table,
// an equation) to a private file.
func rich(t *testing.T) string {
	t.Helper()
	root, err := repo.Root()
	if err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(filepath.Join(root, "testdata", "docx", "rich.docx"))
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "rich.docx")
	if err := os.WriteFile(path, b, 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestUsageAndFileErrors(t *testing.T) {
	dir := t.TempDir()
	doc := filepath.Join(dir, "d.docx")
	writeDocx(t, doc, helloDoc)
	notZip := filepath.Join(dir, "x.docx")
	if err := os.WriteFile(notZip, []byte("plain text"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		args       []string
		exit       int
		code, path string
	}{
		{nil, 2, cli.CodeUsage, ""},
		{[]string{"read"}, 2, cli.CodeUsage, ""},
		{[]string{"find", doc}, 2, cli.CodeUsage, ""},
		{[]string{"read", doc, "extra"}, 2, cli.CodeUsage, ""},
		{[]string{"read", doc, "--view", "Final"}, 2, cli.CodeUsage, "--view"},
		{[]string{"read", doc, "--kind", "document_ui"}, 2, cli.CodeUsage, "--kind"},
		{[]string{"read", doc, "--kind", "styles", "--view", "final"}, 2, cli.CodeUsage, "--view"},
		{[]string{"read", doc, "--json"}, 2, cli.CodeUsage, "--json"},
		{[]string{"find", doc, "q", "--ignore-case=true"}, 2, cli.CodeUsage, "--ignore-case"},
		{[]string{"reed", doc}, 2, cli.CodeUsage, ""},
		{[]string{"read", filepath.Join(dir, "nonexistent.docx")}, 1, cli.CodeIO, ""},
		{[]string{"read", dir}, 1, cli.CodeIO, ""},
		{[]string{"read", notZip}, 1, cli.CodeInvalidDocument, ""},
		{[]string{"find", notZip, "q"}, 1, cli.CodeInvalidDocument, ""},
	} {
		code, _, errs := runCLI(t, c.args...)
		if code != c.exit || errs[0].Code != c.code || errs[0].Path != c.path {
			t.Errorf("%q: exit %d %+v, want %d %s at %q", c.args, code, errs, c.exit, c.code, c.path)
		}
	}
}

func TestCreate(t *testing.T) {
	dir := t.TempDir()
	src, out := filepath.Join(dir, "doc.html"), filepath.Join(dir, "new.docx")
	if err := os.WriteFile(src, []byte(`<h1>Title</h1><p>One <b>bold</b> word and <equation display="inline">x^2</equation>.</p>`), 0o644); err != nil {
		t.Fatal(err)
	}
	// A dry run checks and writes nothing.
	code, stdout, errs := runCLI(t, "create", src, "--dry-run")
	if code != 0 || stdout != `{"dry_run":true}`+"\n" {
		t.Fatalf("dry run: %d %s %+v", code, stdout, errs)
	}
	if _, err := os.Stat(out); err == nil {
		t.Fatal("a dry run wrote a file")
	}
	code, stdout, errs = runCLI(t, "create", src, "-o", out)
	written, err := os.ReadFile(out)
	if code != 0 || err != nil || stdout != `{"dry_run":false,"saved":"`+out+`","new_base":"`+cli.Hash(written)+`"}`+"\n" {
		t.Fatalf("create: %d %s %+v %v", code, stdout, errs, err)
	}
	// The new document reads back, and its hash is the base to use.
	code, stdout, _ = runCLI(t, "find", out, "bold")
	if code != 0 || !strings.HasPrefix(stdout, `{"base":"`+cli.Hash(written)+`"`) || !strings.Contains(stdout, `"text":"One bold word and`) {
		t.Errorf("find in the created document: %d %s", code, stdout)
	}
	// What create does not build yet is refused naming the element, and
	// nothing is written.
	refused := filepath.Join(dir, "note.html")
	if err := os.WriteFile(refused, []byte(`<p>a<footnote>n</footnote></p>`), 0o644); err != nil {
		t.Fatal(err)
	}
	none := filepath.Join(dir, "none.docx")
	code, _, errs = runCLI(t, "create", refused, "-o", none)
	if code != 1 || errs[0].Code != cli.CodeInvalidOperation || errs[0].Path != "html<footnote>" {
		t.Errorf("a footnote: %d %+v", code, errs)
	}
	if _, err := os.Stat(none); err == nil {
		t.Error("a refused create wrote a file")
	}
	for _, c := range []struct {
		args       []string
		exit       int
		code, path string
	}{
		{[]string{"create", src}, 2, cli.CodeUsage, "--out"},
		{[]string{"create"}, 2, cli.CodeUsage, ""},
		{[]string{"create", filepath.Join(dir, "missing.html"), "-o", none}, 1, cli.CodeIO, ""},
	} {
		code, _, errs := runCLI(t, c.args...)
		if code != c.exit || errs[0].Code != c.code || errs[0].Path != c.path {
			t.Errorf("%q: exit %d %+v", c.args, code, errs)
		}
	}
}

func TestHelpAndVersion(t *testing.T) {
	for args, want := range map[string]string{
		"--help":            "usage: docx <command>",
		"help":              "usage: docx <command>",
		"help read":         "usage: docx read <file.docx> [--kind document|comments|revisions|bookmarks|styles|assets]",
		"find --help":       "usage: docx find <file.docx> <query> [--ignore-case]",
		"help apply":        "usage: docx apply <file.docx> <plan.json|-> [--dry-run] [-o <file>] [--changed ids|full]",
		"help create":       "usage: docx create <doc.html|-> [-o <file>] [--dry-run]",
		"help ops":          "text.replace ",
		"help row.insert":   "position           before|after, required",
		"help text.replace": "all                boolean, default false",
		"help html":         "<equation display=",
		"--version":         `{"tool":"docx","version":"` + version + `","contract":1}`,
	} {
		code, out, _ := runCLI(t, strings.Fields(args)...)
		if code != 0 || !strings.Contains(out, want) {
			t.Errorf("docx %s: exit %d, want %q in\n%s", args, code, want, out)
		}
	}
	// The operation list is the table: every operation, nothing else.
	_, out, _ := runCLI(t, "help", "ops")
	if n := strings.Count(out, "\n") - strings.Count(opsIntro, "\n") - 4; n != 42 {
		t.Errorf("help ops lists %d operations, want 42:\n%s", n, out)
	}
}

func TestReadAndFindOutput(t *testing.T) {
	// Any path works: the document is outside the working directory.
	doc := filepath.Join(t.TempDir(), "sub", "d.docx")
	if err := os.MkdirAll(filepath.Dir(doc), 0o755); err != nil {
		t.Fatal(err)
	}
	writeDocx(t, doc, helloDoc)
	b, err := os.ReadFile(doc)
	if err != nil {
		t.Fatal(err)
	}
	base := cli.Hash(b)
	code, out, _ := runCLI(t, "find", doc, "WORLD", "--ignore-case")
	want := `{"base":"` + base + `","matches":[{"id":"00000001","index":1,"selectable_text":"Hello world","text":"Hello world"}],"notes":[]}` + "\n"
	if code != 0 || out != want {
		t.Errorf("find: %d\n%s\nwant\n%s", code, out, want)
	}
	code, out, _ = runCLI(t, "read", doc, "--kind", "styles", "--pretty")
	want = "{\n  \"base\": \"" + base + "\",\n  \"kind\": \"styles\",\n  \"notes\": [],\n  \"styles\": []\n}\n"
	if code != 0 || out != want {
		t.Errorf("read styles: %d\n%s\nwant\n%s", code, out, want)
	}
	code, out, _ = runCLI(t, "read", doc)
	if code != 0 || !strings.HasPrefix(out, `{"base":"`+base+`",`) || !strings.Contains(out, `<p id=\"00000001\">Hello world</p>`) || strings.Count(out, "\n") != 1 {
		t.Errorf("read: %d\n%s", code, out)
	}
	// The same comment thread, in the same shape, in both kinds.
	doc = rich(t)
	var document, listing struct {
		Comments []json.RawMessage `json:"comments"`
	}
	for kind, v := range map[string]any{"document": &document, "comments": &listing} {
		_, out, _ := runCLI(t, "read", doc, "--kind", kind)
		if err := json.Unmarshal([]byte(out), v); err != nil {
			t.Fatal(err)
		}
	}
	if len(document.Comments) != 1 || len(listing.Comments) != 1 || string(document.Comments[0]) != string(listing.Comments[0]) {
		t.Errorf("comment threads differ between kinds:\n%s\n%s", document.Comments, listing.Comments)
	}
	if got := string(listing.Comments[0]); !strings.Contains(got, `"id":"comment:0"`) || !strings.Contains(got, `"select":`) || strings.Contains(got, "locator") {
		t.Errorf("thread: %s", got)
	}
}

// applyPlan writes the plan and runs apply with the extra arguments.
func applyPlan(t *testing.T, doc, plan string, args ...string) (int, map[string]any, []cli.Error) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "plan.json")
	if err := os.WriteFile(path, []byte(plan), 0o644); err != nil {
		t.Fatal(err)
	}
	code, out, errs := runCLI(t, append([]string{"apply", doc, path}, args...)...)
	var res map[string]any
	if code == 0 {
		if err := json.Unmarshal([]byte(out), &res); err != nil {
			t.Fatalf("%v\n%s", err, out)
		}
	}
	return code, res, errs
}

func TestApply(t *testing.T) {
	doc := rich(t)
	before, _ := os.ReadFile(doc) //nolint:errcheck // compared below
	base := cli.Hash(before)
	accept := `{"change_mode":"direct","ops":[{"op":"revision.accept","all":true}]}`

	// A dry run writes nothing and names no new base.
	code, res, errs := applyPlan(t, doc, accept, "--dry-run")
	if code != 0 || res["dry_run"] != true || res["base"] != base || res["new_base"] != nil || res["saved"] != nil {
		t.Fatalf("dry run: %d %v %+v", code, res, errs)
	}
	for _, k := range []string{"aliases", "changed", "deleted", "hazards", "parts"} {
		if res[k] == nil {
			t.Errorf("dry run: %s is absent", k)
		}
	}
	if after, _ := os.ReadFile(doc); !bytes.Equal(before, after) {
		t.Fatal("a dry run wrote the document")
	}
	// changed is read back from the result: a paragraph as {id, html},
	// the table whose inserted row was accepted as read lists it, and
	// every entry under read's key for an address, id.
	units, _ := res["changed"].([]any)
	var paragraphs, tables int
	for _, u := range units {
		unit, _ := u.(map[string]any)
		id, _ := unit["id"].(string)
		switch {
		case id == "":
			t.Errorf("a changed unit without an id: %v", u)
		case strings.HasPrefix(id, "table:"):
			tables++
			if unit["rows"] == nil {
				t.Errorf("the table is not as read lists it: %v", unit)
			}
		case unit["html"] != nil:
			paragraphs++
			if html, _ := unit["html"].(string); !strings.Contains(html, `id="`+id+`"`) || strings.Contains(html, "<ins") {
				t.Errorf("paragraph %s is not the document's markup after the plan: %s", id, html)
			}
		}
	}
	if paragraphs == 0 || tables != 1 {
		t.Errorf("changed has %d paragraphs and %d tables: %v", paragraphs, tables, units)
	}

	// -o writes the result elsewhere and leaves the input alone.
	out := filepath.Join(t.TempDir(), "out.docx")
	code, res, errs = applyPlan(t, doc, accept, "-o", out, "--changed", "ids")
	written, err := os.ReadFile(out)
	if code != 0 || err != nil || res["saved"] != out || res["new_base"] != cli.Hash(written) || res["base"] != base {
		t.Fatalf("apply -o: %d %v %+v %v", code, res, errs, err)
	}
	if changed, _ := res["changed"].([]any); len(changed) == 0 {
		t.Errorf("no changed paragraphs: %v", res)
	} else if _, isAddress := changed[0].(string); !isAddress {
		t.Errorf("--changed ids gives %v", changed[0])
	}
	if after, _ := os.ReadFile(doc); !bytes.Equal(before, after) {
		t.Fatal("apply -o wrote the input")
	}
	_, revisions, _ := runCLI(t, "read", out, "--kind", "revisions")
	if !strings.Contains(revisions, `"revisions":[]`) {
		t.Errorf("revisions left after accepting all: %s", revisions)
	}

	// The new base guards the next plan; a stale one refuses it.
	stale := `{"base":"` + base + `","change_mode":"direct","ops":[{"op":"revision.accept","all":true}]}`
	if code, _, errs := applyPlan(t, out, stale, "--dry-run"); code != 1 || errs[0].Code != cli.CodeStaleBase || errs[0].Path != "base" {
		t.Errorf("stale base: %d %+v", code, errs)
	}
	fresh := strings.Replace(stale, base, cli.Hash(written), 1)
	// Nothing left to accept: the plan changes nothing and writes nothing.
	code, res, errs = applyPlan(t, out, fresh)
	if code != 0 || res["saved"] != nil || res["new_base"] != res["base"] {
		t.Errorf("no-op: %d %v %+v", code, res, errs)
	}

	// A refusal at an operation names it and changes no file.
	code, _, errs = applyPlan(t, doc, `{"change_mode":"direct","ops":[{"op":"revision.accept","all":true},{"op":"revision.reject","at":"revision:999"}]}`)
	if code != 1 || len(errs) != 1 || errs[0].Code != cli.CodeNotFound || errs[0].Path != "ops[1].at" {
		t.Errorf("unknown revision: %d %+v", code, errs)
	}
	if after, _ := os.ReadFile(doc); !bytes.Equal(before, after) {
		t.Fatal("a refused plan wrote the document")
	}

	// An operation not ported yet is refused at it with operation_failed,
	// never internal, and changes no file (while one is left).
	if op, ok := unportedOp(t); ok {
		t.Logf("unported op: %s", op)
		code, _, errs = applyPlan(t, doc, `{"change_mode":"direct","ops":[{"op":"revision.accept","all":true},`+op+`]}`)
		if code != 1 || len(errs) != 1 || errs[0].Code != cli.CodeOperationFailed || errs[0].Path != "ops[1]" {
			t.Errorf("unported op %s: %d %+v", op, code, errs)
		}
		if after, _ := os.ReadFile(doc); !bytes.Equal(before, after) {
			t.Fatal("a plan with an unported op wrote the document")
		}
	}

	// Faults of the plan itself come back together, before the document
	// is opened: the file does not exist.
	code, _, errs = applyPlan(t, filepath.Join(t.TempDir(), "missing.docx"), `{"change_mode":"track","ops":[
		{"op":"text.replace","at":"0000000A","select":"","with":"b"},
		{"op":"replace_text","at":"0000000A","select":"a","with":"b"},
		{"op":"paragraph.format","at":"0000000A","alignment":"center","colour":"red"}]}`)
	var got []string
	for _, e := range errs {
		got = append(got, e.Code+" "+e.Path)
	}
	want := "invalid_operation ops[0].select; invalid_plan ops[1].op; invalid_plan ops[2].colour; invalid_plan author"
	if code != 1 || strings.Join(got, "; ") != want {
		t.Errorf("plan faults: %d\n got %s\nwant %s", code, strings.Join(got, "; "), want)
	}
}

// unportedOp returns, as plan JSON, an operation of the op table that is
// not ported yet (its Spec still embeds pending) and whose zero value is
// a valid operation, and false when none is left (apply's
// TestUnportedOpsRefuse covers every unported operation).
func unportedOp(t *testing.T) (string, bool) {
	t.Helper()
	for _, d := range ops.Ops {
		spec := d.New()
		if _, pending := spec.(interface{ PendingOp() }); !pending {
			continue
		}
		p, diag := ops.NewPlan("", "Ann", ops.ModeDirect, spec)
		if diag != nil {
			continue
		}
		var plan struct{ Ops []json.RawMessage }
		if err := json.Unmarshal([]byte(p.CanonicalJSON()), &plan); err != nil || len(plan.Ops) != 1 {
			t.Fatalf("%s: canonical plan %s: %v", d.Name, p.CanonicalJSON(), err)
		}
		op := string(plan.Ops[0])
		if _, errs := ops.ParsePlan([]byte(`{"change_mode":"direct","ops":[` + op + `]}`)); errs != nil {
			continue // its zero value is not a valid operation
		}
		return op, true
	}
	return "", false
}
