package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"regexp"
	"strings"
	"testing"

	"github.com/jhnoel/documentdriver/internal/cli"
	"github.com/jhnoel/documentdriver/internal/vsdx/read"
	"github.com/jhnoel/documentdriver/internal/vsdx/vsdxtest"
)

// drawingFile writes the V1 minimal drawing as a file and returns its
// path. It is the v1.vsdx of the C1 rows. It takes a testing.TB so the
// fuzzer can write its fixture once, outside the fuzz target.
func drawingFile(t testing.TB) string {
	t.Helper()
	return vsdxtest.Fixture(t, "v1-minimal")
}

// runCLI runs the command line. A failure must leave stdout empty and put
// exactly one errors object on stderr; a success must leave stderr empty.
func runCLI(t *testing.T, args ...string) (code int, stdout string, errs []cli.Error) {
	t.Helper()
	var out, errb bytes.Buffer
	code = run(args, &out, &errb)
	if code == cli.ExitOK {
		if errb.Len() != 0 {
			t.Errorf("vsdx %v: stderr on success: %s", args, errb.String())
		}
		return cli.ExitOK, out.String(), nil
	}
	if out.Len() != 0 {
		t.Errorf("vsdx %v: stdout on failure: %s", args, out.String())
	}
	var v struct {
		Errors []cli.Error `json:"errors"`
	}
	dec := json.NewDecoder(&errb)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&v); err != nil || len(v.Errors) == 0 || dec.More() {
		t.Fatalf("vsdx %v: stderr is not one errors object: %v", args, err)
	}
	return code, "", v.Errors
}

// TestReadSummaryV1 checks the C1 R1 row: `vsdx read v1.vsdx` exit 0,
// stdout R1's V1 bytes with the real hash.
func TestReadSummaryV1(t *testing.T) {
	file := drawingFile(t)
	data, err := os.ReadFile(file)
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	code, out, _ := runCLI(t, "read", file)
	if code != cli.ExitOK {
		t.Fatalf("read exit %d", code)
	}
	want := `{"base":"` + cli.Hash(data) + `","pages":[{"id":"page0","name":"Page-1","size":[612,792],"orientation":"portrait","print_orientation":"portrait","objects":1}],"masters":[],"notes":[]}` + "\n"
	if out != want {
		t.Errorf("read stdout = %s, want %s", out, want)
	}
	after, err := os.ReadFile(file)
	if err != nil {
		t.Fatalf("re-read fixture: %v", err)
	}
	if !bytes.Equal(data, after) {
		t.Error("read changed the drawing")
	}
}

// TestReadErrors checks the C1 read rows: --page with --master is usage,
// --detail without --page is usage, an unknown page is not_found at
// --page, and a non-zip is invalid_document.
func TestReadErrors(t *testing.T) {
	file := drawingFile(t)
	notAZip := t.TempDir() + "/not.vsdx"
	if err := os.WriteFile(notAZip, []byte("not a zip"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		args []string
		exit int
		code string
		path string
	}{
		{[]string{"read", file, "--page", "page0", "--master", "master2"}, cli.ExitUsage, cli.CodeUsage, "--master"},
		{[]string{"read", file, "--detail", "full"}, cli.ExitUsage, cli.CodeUsage, "--detail"},
		{[]string{"read", file, "--ids", "page0/s1"}, cli.ExitUsage, cli.CodeUsage, "--ids"},
		{[]string{"read", file, "--region", "0,0,10,10"}, cli.ExitUsage, cli.CodeUsage, "--region"},
		{[]string{"read", file, "--units", "in"}, cli.ExitUsage, cli.CodeUsage, "--units"},
		{[]string{"read", file, "--page", "page7"}, cli.ExitFailed, cli.CodeNotFound, "--page"},
		{[]string{"read", notAZip}, cli.ExitFailed, cli.CodeInvalidDocument, ""},
		{[]string{"read", file, "--page", "page0", "--region", "1,2,3"}, cli.ExitUsage, cli.CodeUsage, "--region"},
		{[]string{"read", file, "--page", "page0", "--region", "1,2,x,4"}, cli.ExitUsage, cli.CodeUsage, "--region"},
	} {
		code, _, errs := runCLI(t, c.args...)
		if code != c.exit || len(errs) != 1 || errs[0].Code != c.code || errs[0].Path != c.path {
			t.Errorf("vsdx %v: exit %d, errors %+v; want exit %d, %s at %q", c.args, code, errs, c.exit, c.code, c.path)
		}
	}
}

// TestJunkInvalidDocumentNW checks the NW row: read, find and xml
// on a file that is not a zip give invalid_document with pptx's
// form opc: <path>: zip: not a valid zip file (the path the caller
// named).
func TestJunkInvalidDocumentNW(t *testing.T) {
	junk := t.TempDir() + "/junk.vsdx"
	if err := os.WriteFile(junk, []byte("not a zip"), 0o600); err != nil {
		t.Fatal(err)
	}
	want := "opc: " + junk + ": zip: not a valid zip file"
	for _, args := range [][]string{
		{"read", junk},
		{"read", junk, "--page", "page0"},
		{"find", junk, "Hello"},
		{"xml", junk, "page0/s1"},
	} {
		code, _, errs := runCLI(t, args...)
		if code != cli.ExitFailed || len(errs) != 1 || errs[0].Code != cli.CodeInvalidDocument || errs[0].Message != want {
			t.Errorf("vsdx %v: exit %d, errors %+v; want invalid_document %q", args, code, errs, want)
		}
	}
}

// TestReadPage checks read --page and --master wire their options to
// read.Page: the page, its detail, ids, region and units.
func TestReadPage(t *testing.T) {
	file := drawingFile(t)
	data, err := os.ReadFile(file)
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	want, err := read.Page(data, cli.Hash(data), "page0", read.Options{})
	if err != nil {
		t.Fatalf("read.Page(page0): %v", err)
	}
	code, out, _ := runCLI(t, "read", file, "--page", "page0")
	if code != cli.ExitOK || out != string(want) {
		t.Errorf("read --page exit %d, stdout %s; want %s", code, out, want)
	}
	for _, args := range [][]string{
		{"read", file, "--page", "page0", "--detail", "full"},
		{"read", file, "--page", "page0", "--units", "in"},
		{"read", file, "--page", "page0", "--ids", "page0/s1"},
		{"read", file, "--page", "page0", "--region", "0,0,150,150"},
		{"read", file, "--page", "page0", "--pretty"},
	} {
		if code, out, _ := runCLI(t, args...); code != cli.ExitOK || !strings.Contains(out, `"page":"page0"`) && !strings.Contains(out, `"page": "page0"`) {
			t.Errorf("vsdx %v: exit %d, stdout %.200s", args, code, out)
		}
	}
	code, out, _ = runCLI(t, "read", vsdxtest.Fixture(t, "v2-master"), "--master", "master2")
	if code != cli.ExitOK || !strings.Contains(out, `"page":"master2"`) {
		t.Errorf("read --master exit %d, stdout %.200s", code, out)
	}
}

// TestFind checks the find handler: a match, --ignore-case, --page,
// --limit cutting, and the limit and page errors.
func TestFind(t *testing.T) {
	file := drawingFile(t)
	code, out, _ := runCLI(t, "find", file, "Hello")
	if code != cli.ExitOK || !strings.Contains(out, `"matches":[{"at":"page0/s1","text":"Hello"}]`) {
		t.Errorf("find Hello exit %d, stdout %s", code, out)
	}
	if code, out, _ := runCLI(t, "find", file, "hello", "--ignore-case"); code != cli.ExitOK || !strings.Contains(out, "page0/s1") {
		t.Errorf("find ignore-case exit %d, stdout %s", code, out)
	}
	if code, out, _ := runCLI(t, "find", file, "Hello", "--page", "page0"); code != cli.ExitOK || !strings.Contains(out, "page0/s1") {
		t.Errorf("find --page exit %d, stdout %s", code, out)
	}
	v2 := vsdxtest.Fixture(t, "v2-master")
	code, out, _ = runCLI(t, "find", v2, "Process")
	if code != cli.ExitOK || !strings.Contains(out, `"matches":[{"at":"page0/s1","text":"Process"}]`) {
		t.Errorf("find Process exit %d, stdout %s", code, out)
	}
	for _, c := range []struct {
		args []string
		exit int
		code string
		path string
	}{
		{[]string{"find", file, "Hello", "--limit", "ten"}, cli.ExitUsage, cli.CodeUsage, "--limit"},
		{[]string{"find", file, "Hello", "--limit", "-1"}, cli.ExitUsage, cli.CodeUsage, "--limit"},
		{[]string{"find", file, "Hello", "--page", "page7"}, cli.ExitFailed, cli.CodeNotFound, "--page"},
	} {
		code, _, errs := runCLI(t, c.args...)
		if code != c.exit || len(errs) != 1 || errs[0].Code != c.code || errs[0].Path != c.path {
			t.Errorf("vsdx %v: exit %d, errors %+v; want exit %d, %s at %q", c.args, code, errs, c.exit, c.code, c.path)
		}
	}
}

// TestParseLimit checks parseLimit as xlsx parses --limit: the default,
// 0, the maximum, and usage for a sign, a space, text and overflow.
func TestParseLimit(t *testing.T) {
	for _, c := range []struct {
		value string
		has   bool
		want  int
		ok    bool
	}{
		{"", false, read.DefaultLimit, true},
		{"0", true, 0, true},
		{"5", true, 5, true},
		{"2147483647", true, 2147483647, true},
		{"2147483648", true, 0, false},
		{"-1", true, 0, false},
		{"+1", true, 0, false},
		{" 1", true, 0, false},
		{"ten", true, 0, false},
		{"", true, 0, false},
	} {
		// Build the Parsed through find's grammar, not its fields.
		args := []string{"f.vsdx", "q"}
		if c.has {
			args = append(args, "--limit", c.value)
		}
		p, perr := findCommand.Parse("vsdx", args)
		if perr != nil {
			t.Fatalf("parse find %v: %v", args, perr)
		}
		got, err := parseLimit(p)
		if c.ok {
			if err != nil || got != c.want {
				t.Errorf("parseLimit(%q) = %d, %v; want %d", c.value, got, err, c.want)
			}
		} else if err == nil {
			t.Errorf("parseLimit(%q) = %d; want a usage error", c.value, got)
		} else {
			var e *cli.Error
			if !errors.As(err, &e) || e.Code != cli.CodeUsage || e.Path != "--limit" {
				t.Errorf("parseLimit(%q) error = %v; want usage at --limit", c.value, err)
			}
		}
	}
}

// TestXML checks the xml handler: a shape's bytes plus a newline, a
// bare page address is usage, and an unknown shape is not_found.
func TestXML(t *testing.T) {
	file := drawingFile(t)
	data, err := os.ReadFile(file)
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	want, err := read.XML(data, "page0/s1")
	if err != nil {
		t.Fatalf("read.XML(page0/s1): %v", err)
	}
	code, out, _ := runCLI(t, "xml", file, "page0/s1")
	if code != cli.ExitOK || out != string(want)+"\n" {
		t.Errorf("xml exit %d, stdout %q; want %q", code, out, string(want)+"\n")
	}
	for _, c := range []struct {
		args []string
		exit int
		code string
		path string
	}{
		{[]string{"xml", file, "page0"}, cli.ExitUsage, cli.CodeUsage, ""},
		{[]string{"xml", file, "page0/"}, cli.ExitUsage, cli.CodeUsage, ""},
		{[]string{"xml", file, "page0/s99"}, cli.ExitFailed, cli.CodeNotFound, ""},
	} {
		code, _, errs := runCLI(t, c.args...)
		if code != c.exit || len(errs) != 1 || errs[0].Code != c.code || errs[0].Path != c.path {
			t.Errorf("vsdx %v: exit %d, errors %+v; want exit %d, %s at %q", c.args, code, errs, c.exit, c.code, c.path)
		}
	}
}

// TestHelpHTML checks the C1 T2 row: `vsdx help html` lists exactly
// T2's tags, attributes and CSS properties.
func TestHelpHTML(t *testing.T) {
	code, out, _ := runCLI(t, "help", "html")
	if code != cli.ExitOK {
		t.Fatalf("help html exit %d", code)
	}
	tagRe := regexp.MustCompile(`<([a-z]+)`)
	wantTags := map[string]bool{"p": true, "ul": true, "li": true, "b": true, "i": true, "u": true, "s": true, "span": true, "br": true, "opaque": true}
	gotTags := map[string]bool{}
	for _, m := range tagRe.FindAllStringSubmatch(out, -1) {
		gotTags[m[1]] = true
	}
	if len(gotTags) != len(wantTags) {
		t.Errorf("help html tags = %v, want exactly %v", gotTags, wantTags)
	}
	for tag := range wantTags {
		if !gotTags[tag] {
			t.Errorf("help html lacks tag <%s>", tag)
		}
	}
	for _, attr := range []string{"style", "data-char", "data-ref", "data-kind", "data-text"} {
		if !strings.Contains(out, attr) {
			t.Errorf("help html lacks attribute %s", attr)
		}
	}
	for _, attr := range []string{"href", "data-level", "lang", "dir"} {
		if strings.Contains(out, attr) {
			t.Errorf("help html lists attribute %s, which is not in the T2 grammar", attr)
		}
	}
	if strings.Contains(out, "data-*") {
		t.Error("help html lists data-*, which is not in the T2 grammar")
	}
	for _, prop := range []string{"font-weight", "font-style", "text-decoration", "font-size", "color", "font-family", "text-align", "line-height", "margin-top", "margin-bottom", "margin-left", "margin-right", "text-indent"} {
		if !strings.Contains(out, prop) {
			t.Errorf("help html lacks CSS property %s", prop)
		}
	}
	for _, prop := range []string{"font-variant", "letter-spacing", "text-transform", "background-color"} {
		if strings.Contains(out, prop) {
			t.Errorf("help html lists CSS property %s, which is not in the T2 grammar", prop)
		}
	}
}

// TestRunExitCodes checks the CLI exit codes for help, version and a
// bogus command.
func TestRunExitCodes(t *testing.T) {
	for _, c := range []struct {
		args []string
		want int
	}{
		{nil, cli.ExitUsage},
		{[]string{"help"}, cli.ExitOK},
		{[]string{"read"}, cli.ExitUsage},
		{[]string{"help", "html"}, cli.ExitOK},
		{[]string{"--version"}, cli.ExitOK},
		{[]string{"bogus"}, cli.ExitUsage},
	} {
		var out, errb bytes.Buffer
		if got := run(c.args, &out, &errb); got != c.want {
			t.Errorf("run(%v) = %d, want %d", c.args, got, c.want)
		}
	}
}
