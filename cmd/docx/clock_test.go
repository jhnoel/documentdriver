package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jhnoel/documentdriver/internal/cli"
)

// clockDoc is a document docx create made, and clockPlan tracks an
// insert and a replace in it and adds a comment: every kind of stamp
// apply writes.
func clockDoc(t *testing.T) (doc, plan string) {
	t.Helper()
	dir := t.TempDir()
	html, doc := filepath.Join(dir, "doc.html"), filepath.Join(dir, "doc.docx")
	if err := os.WriteFile(html, []byte("<p>Hello world</p>"), 0o644); err != nil {
		t.Fatal(err)
	}
	if code, _, errs := runCLI(t, "create", html, "-o", doc); code != 0 {
		t.Fatalf("create: %+v", errs)
	}
	_, found, _ := runCLI(t, "find", doc, "Hello")
	_, rest, ok := strings.Cut(found, `"id":"`)
	if !ok {
		t.Fatalf("find: %s", found)
	}
	id := rest[:8]
	return doc, `{"author":"Ada","change_mode":"track","ops":[
	{"op":"paragraph.insert","at":"` + id + `","position":"after","html":"<p>New one</p>","as":"np"},
	{"op":"text.replace","at":"$np","select":"New","with":"Fresh"},
	{"op":"comment.add","at":"$np","select":"one","text":"why"}]}`
}

// TestClockDeterminism: with the same file, plan and clock (the hidden
// --clock, the parity oracle's), two runs of apply write byte-identical
// documents and print the same result; the clock is the time stamped,
// and a dry run with that clock describes the commit.
func TestClockDeterminism(t *testing.T) {
	const at = "2026-03-04T05:06:07Z"
	doc, clockPlan := clockDoc(t)
	dir := t.TempDir()
	var outs [2][]byte
	var results [2]string
	for i := range outs {
		out := filepath.Join(dir, "out"+string(rune('a'+i))+".docx")
		code, stdout := applyRaw(t, doc, clockPlan, "-o", out, "--clock", at)
		if code != 0 {
			t.Fatalf("run %d: exit %d", i, code)
		}
		b, err := os.ReadFile(out)
		if err != nil {
			t.Fatal(err)
		}
		outs[i], results[i] = b, strings.ReplaceAll(stdout, out, "OUT")
	}
	if !bytes.Equal(outs[0], outs[1]) {
		t.Fatal("two runs with the same file, plan and clock wrote different bytes")
	}
	if results[0] != results[1] {
		t.Errorf("results differ:\n%s\n%s", results[0], results[1])
	}
	// The clock is what was stamped: on the revisions and the comment.
	_, revisions, _ := runCLI(t, "read", filepath.Join(dir, "outa.docx"), "--kind", "revisions")
	_, comments, _ := runCLI(t, "read", filepath.Join(dir, "outa.docx"), "--kind", "comments")
	if !strings.Contains(revisions, `"author":"Ada","date":"`+at+`"`) || !strings.Contains(comments, `"author":"Ada","date":"`+at+`"`) {
		t.Errorf("the clock is not the stamp:\n%s\n%s", revisions, comments)
	}
	// A dry run with the clock describes the commit: the same result but
	// for dry_run, saved and new_base.
	_, dry := applyRaw(t, doc, clockPlan, "--dry-run", "--clock", at)
	commit := strings.NewReplacer(`"new_base":"`+cli.Hash(outs[0])+`",`, "", `,"saved":"OUT"`, "", `"dry_run":false`, `"dry_run":true`).Replace(results[0])
	if dry != commit {
		t.Errorf("dry run and commit differ:\n%s\n%s", dry, commit)
	}
}

// applyRaw writes the plan and runs apply, returning stdout as printed.
func applyRaw(t *testing.T, doc, plan string, args ...string) (int, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "plan.json")
	if err := os.WriteFile(path, []byte(plan), 0o644); err != nil {
		t.Fatal(err)
	}
	code, out, errs := runCLI(t, append([]string{"apply", doc, path}, args...)...)
	if code != 0 {
		t.Fatalf("apply %v: %d %+v", args, code, errs)
	}
	return code, out
}
