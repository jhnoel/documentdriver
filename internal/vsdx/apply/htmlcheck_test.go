package apply

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jhnoel/documentdriver/internal/cli"
	"github.com/jhnoel/documentdriver/internal/vsdx/ops"
	"github.com/jhnoel/documentdriver/internal/vsdx/vsdxtest"
)

// htmlPreOpenPlan returns two text.set ops whose html is bad: an
// <a> element and a bogus attribute.
func htmlPreOpenPlan() *ops.Plan {
	return &ops.Plan{Ops: []ops.Spec{
		&ops.TextSetSpec{At: "page0/s1", HTML: `<p><a href="x">a</a></p>`},
		&ops.TextSetSpec{At: "page0/s1", HTML: `<p bogus="1">x</p>`},
	}}
}

// checkPreOpenErrs checks err is exactly the two CC5 faults, at the
// fancy paths, with no other code.
func checkPreOpenErrs(t *testing.T, err error) {
	t.Helper()
	es := cli.AsErrors(err)
	if len(es) != 2 {
		t.Fatalf("errors %v, want exactly two", es)
	}
	want := [][2]string{
		{cli.CodeInvalidOperation, "ops[0].html<a>"},
		{cli.CodeInvalidOperation, "ops[1].html@bogus"},
	}
	for i, e := range es {
		if e.Code != want[i][0] || e.Path != want[i][1] {
			t.Errorf("error %d: got %s@%s, want %s@%s", i, e.Code, e.Path, want[i][0], want[i][1])
		}
		if e.Code == cli.CodeInvalidDocument {
			t.Errorf("error %d is invalid_document: html must be checked before the file is read", i)
		}
	}
}

// TestHTMLPreOpenNonZip checks the CC5 row: the two-op plan on a
// non-zip file gives exactly the two invalid_operation faults, with no
// invalid_document, and writes nothing.
func TestHTMLPreOpenNonZip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "notzip.vsdx")
	plain := []byte("not a zip, just words")
	if err := os.WriteFile(path, plain, 0o600); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(dir, "out.vsdx")
	_, err := Run(path, htmlPreOpenPlan(), Options{Out: out})
	checkPreOpenErrs(t, err)
	if _, statErr := os.Stat(out); !os.IsNotExist(statErr) {
		t.Error("a plan refused before open wrote its out file")
	}
	if got, readErr := os.ReadFile(path); readErr != nil || !bytes.Equal(got, plain) {
		t.Error("a plan refused before open changed the input")
	}
}

// TestHTMLPreOpenV2 checks the CC5 row on the V2 fixture: the same
// two faults, and the file is unchanged.
func TestHTMLPreOpenV2(t *testing.T) {
	path := vsdxtest.Fixture(t, "v2-master")
	before := readFile(t, path)
	out := filepath.Join(t.TempDir(), "out.vsdx")
	_, err := Run(path, htmlPreOpenPlan(), Options{Out: out})
	checkPreOpenErrs(t, err)
	if _, statErr := os.Stat(out); !os.IsNotExist(statErr) {
		t.Error("a refused plan wrote its out file")
	}
	if got := readFile(t, path); !bytes.Equal(got, before) {
		t.Error("a refused plan changed the input")
	}
}

// TestHTMLPreOpenStyleAndColor checks the CC5 rows for a style
// property and for a data-color that disagrees with color.
func TestHTMLPreOpenStyleAndColor(t *testing.T) {
	for _, tc := range []struct {
		name    string
		html    string
		path    string
		wantMsg string
	}{
		{name: "style property", html: `<p><span style="letter-spacing:1pt">x</span></p>`, path: "ops[0].html@style", wantMsg: "letter-spacing"},
		{name: "data-color disagree", html: `<p><span style="color:#FF0000" data-color="#00FF00">x</span></p>`, path: "ops[0].html@data-color", wantMsg: "data-color"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := vsdxtest.Fixture(t, "v1-minimal")
			plan := &ops.Plan{Ops: []ops.Spec{&ops.TextSetSpec{At: "page0/s1", HTML: tc.html}}}
			_, err := Run(path, plan, Options{Out: filepath.Join(t.TempDir(), "out.vsdx")})
			es := cli.AsErrors(err)
			if len(es) != 1 || es[0].Code != cli.CodeInvalidOperation || es[0].Path != tc.path {
				t.Fatalf("errors %v, want one invalid_operation at %s", es, tc.path)
			}
			if !strings.Contains(es[0].Message, tc.wantMsg) {
				t.Errorf("message %q does not name %q", es[0].Message, tc.wantMsg)
			}
		})
	}
}

// TestHTMLPreOpenValidShape checks that a plan whose html is in the
// grammar still runs: on a non-zip file it reaches the document open
// and fails there, not in the html pass.
func TestHTMLPreOpenValidShape(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "notzip.vsdx")
	if err := os.WriteFile(path, []byte("not a zip"), 0o600); err != nil {
		t.Fatal(err)
	}
	plan := &ops.Plan{Ops: []ops.Spec{&ops.TextSetSpec{At: "page0/s1", HTML: `<p><b>Hello</b></p>`}}}
	_, err := Run(path, plan, Options{Out: filepath.Join(dir, "out.vsdx")})
	es := cli.AsErrors(err)
	if len(es) != 1 || es[0].Code != cli.CodeInvalidDocument {
		t.Fatalf("errors %v, want one invalid_document (valid html, unreadable file)", es)
	}
}
