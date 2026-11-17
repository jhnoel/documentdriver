package main

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jhnoel/documentdriver/internal/cli"
	"github.com/jhnoel/documentdriver/internal/ids"
	"github.com/jhnoel/documentdriver/internal/repo"
	"github.com/jhnoel/documentdriver/internal/vsdx/apply"
	"github.com/jhnoel/documentdriver/internal/vsdx/create"
	"github.com/jhnoel/documentdriver/internal/vsdx/ops"
)

// planFile writes text as the plan in the temp dir and returns its path.
func planFile(t *testing.T, text string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "plan.json")
	if err := os.WriteFile(p, []byte(text), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

// blankFiles writes the created blank (create.Template at the fixed
// clock) as two identical files: cliIn for the CLI apply and libIn for
// the library apply.Run.
func blankFiles(t *testing.T) (cliIn, libIn string) {
	t.Helper()
	clock := time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC)
	data, err := create.Template(clock)
	if err != nil {
		t.Fatalf("Template: %v", err)
	}
	dir := t.TempDir()
	cliIn = filepath.Join(dir, "cli.vsdx")
	libIn = filepath.Join(dir, "lib.vsdx")
	for _, path := range []string{cliIn, libIn} {
		if err := os.WriteFile(path, data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return cliIn, libIn
}

// createPlan reads a create plan of testdata/vsdx/create/plans.
func createPlan(t *testing.T, name string) []byte {
	t.Helper()
	root, err := repo.Root()
	if err != nil {
		t.Fatalf("repo root: %v", err)
	}
	b, err := os.ReadFile(filepath.Join(root, "testdata", "vsdx", "create", "plans", name))
	if err != nil {
		t.Fatalf("read %s: %v", name, err)
	}
	return b
}

// TestApplyEqualsLibrary checks the A2 row in the form this chunk can
// reach: `vsdx apply` of c2 on the created blank (create.Template)
// produces the library apply.Run's result — what CR1's create delegates
// to — with the same deterministic seed and clock. The c2 ops are implemented, so this covers the
// successful run; the refused-run path it also carries covers a
// future op that refuses.
func TestApplyEqualsLibrary(t *testing.T) {
	cliIn, libIn := blankFiles(t)
	planText := createPlan(t, "c2-shapes.json")
	dir := t.TempDir()
	planPath := filepath.Join(dir, "c2.json")
	if err := os.WriteFile(planPath, planText, 0o600); err != nil {
		t.Fatal(err)
	}
	// Both runs share one out path, so their saved fields match; the
	// CLI-written file is snapshotted before the library run overwrites
	// it.
	out := filepath.Join(dir, "out.vsdx")
	clock := defaultClock
	code, cliOutBytes, cliErrs := runCLI(t, "apply", cliIn, planPath, "-o", out, "--seed", "0", "--clock", clock.Format(time.RFC3339))
	cliFile, cliErr := os.ReadFile(out)
	parsed, errs := ops.ParsePlan(planText)
	if len(errs) != 0 {
		t.Fatalf("ParsePlan(c2): %v", errs)
	}
	libRes, libErr := apply.Run(libIn, parsed, apply.Options{Out: out, IDs: ids.Fixed(0, clock)})
	if libErr != nil {
		var le *cli.Error
		if !errors.As(libErr, &le) {
			t.Fatalf("apply.Run error %v, want *cli.Error", libErr)
		}
		if code != cli.ExitFailed || len(cliErrs) != 1 || cliErrs[0].Code != le.Code || cliErrs[0].Path != le.Path {
			t.Errorf("apply file exit %d errors %+v, want failure like library %s at %q", code, cliErrs, le.Code, le.Path)
		}
		if !errors.Is(cliErr, os.ErrNotExist) {
			t.Error("a refused apply wrote its out file")
		}
		if _, err := os.Stat(out); !errors.Is(err, os.ErrNotExist) {
			t.Error("a refused library apply wrote its out file")
		}
		return
	}
	if code != cli.ExitOK || libRes == nil {
		t.Fatalf("apply file exit %d errors %+v, library %v %v", code, cliErrs, libRes, libErr)
	}
	libJSON, err := libRes.JSON()
	if err != nil {
		t.Fatalf("library result JSON: %v", err)
	}
	// CLI prints the result JSON through cli.WriteJSON: trimmed plus a
	// newline.
	if want := strings.TrimRight(string(libJSON), "\n") + "\n"; cliOutBytes != want {
		t.Errorf("apply stdout differs from library result JSON\n got %s\nwant %s", cliOutBytes, want)
	}
	if cliErr != nil {
		t.Fatal(cliErr)
	}
	libFile, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(cliFile, libFile) {
		t.Error("apply file bytes differ from library file bytes")
	}
}

// TestApplyErrors checks the apply plumbing: document-independent plan
// faults come back together, a stale base is refused at base, and an op
// the document refuses is operation_failed at ops[0] with the file
// unchanged. The refusal is page.delete of a drawing's only page (a
// rule of the op, which stays true as other ops are implemented; an op
// with no Run is covered by apply's TestUnimplementedOps, which reads
// the table).
func TestApplyErrors(t *testing.T) {
	file := drawingFile(t)
	data, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	badJSON := planFile(t, `{"ops":`)
	unknownOp := planFile(t, `{"ops":[{"op":"object.zap","at":"page0"}]}`)
	badOp := planFile(t, `{"ops":[{"op":"page.set","at":"page0"}]}`)
	stale := planFile(t, `{"base":"sha256:`+strings.Repeat("0", 64)+`","ops":[{"op":"page.rename","at":"page0","name":"X"}]}`)
	refused := planFile(t, `{"ops":[{"op":"page.delete","at":"page0"}]}`)
	for _, c := range []struct {
		plan string
		exit int
		code string
		path string
	}{
		{badJSON, cli.ExitFailed, "invalid_json", ""},
		{unknownOp, cli.ExitFailed, cli.CodeInvalidPlan, "ops[0].op"},
		{badOp, cli.ExitFailed, cli.CodeInvalidOperation, "ops[0]"},
		{stale, cli.ExitFailed, cli.CodeStaleBase, "base"},
		{refused, cli.ExitFailed, cli.CodeOperationFailed, "ops[0].at"},
	} {
		code, _, errs := runCLI(t, "apply", file, c.plan)
		if code != c.exit || len(errs) != 1 || errs[0].Code != c.code || errs[0].Path != c.path {
			t.Errorf("apply %s: exit %d errors %+v; want exit %d %s at %q", c.plan, code, errs, c.exit, c.code, c.path)
		}
	}
	if code, _, errs := runCLI(t, "apply", file, refused, "--seed", "bogus"); code != cli.ExitUsage || len(errs) != 1 || errs[0].Code != cli.CodeUsage || errs[0].Path != "--seed" {
		t.Errorf("apply --seed bogus: exit %d errors %+v; want usage at --seed", code, errs)
	}
	if code, _, errs := runCLI(t, "apply", file, refused, "--clock", "bogus"); code != cli.ExitUsage || len(errs) != 1 || errs[0].Code != cli.CodeUsage || errs[0].Path != "--clock" {
		t.Errorf("apply --clock bogus: exit %d errors %+v; want usage at --clock", code, errs)
	}
	after, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(data, after) {
		t.Error("a refused apply changed the drawing")
	}
	if got := cli.Hash(after); got != cli.Hash(data) {
		t.Error("a refused apply changed the drawing hash")
	}
}

// TestApplyDryRunWritesNothing checks --dry-run on a refused op (the
// only page deleted): the refusal is the same as without it and no out
// file appears. It names no op that might be implemented later.
func TestApplyDryRunWritesNothing(t *testing.T) {
	file := drawingFile(t)
	plan := planFile(t, `{"ops":[{"op":"page.delete","at":"page0"}]}`)
	out := filepath.Join(t.TempDir(), "out.vsdx")
	code, _, errs := runCLI(t, "apply", file, plan, "-o", out, "--dry-run")
	if code != cli.ExitFailed || len(errs) != 1 || errs[0].Code != cli.CodeOperationFailed || errs[0].Path != "ops[0].at" {
		t.Errorf("apply --dry-run: exit %d errors %+v; want operation_failed at ops[0].at", code, errs)
	}
	if _, err := os.Stat(out); !errors.Is(err, os.ErrNotExist) {
		t.Error("--dry-run wrote its out file")
	}
}
