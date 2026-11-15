package main

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jhnoel/documentdriver/internal/cli"
	"github.com/jhnoel/documentdriver/internal/xlsx/xlsxtest"
)

// G19: the apply and create commands end to end (the binary's
// contract: one JSON result on stdout, errors on stderr, exit codes).
// G19 also updates main_test.go's TestRefusals, which expects
// apply and create to be refused today.

func TestApplyCommand(t *testing.T) {
	if !applyWired() {
		t.Skip("unimplemented: chunk G19")
	}
	dir := t.TempDir()
	in, err := xlsxtest.Book{Sheets: []xlsxtest.Sheet{{Name: "Budget", Rows: `<row r="1"><c r="A1"><v>1</v></c></row>`}}}.Write(dir, "in.xlsx")
	if err != nil {
		t.Fatal(err)
	}
	plan := filepath.Join(dir, "plan.json")
	if err := os.WriteFile(plan, []byte(`{"ops":[{"op":"cell.set","at":"Budget!B1","value":"=A1*2"}]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(dir, "out.xlsx")
	var stdout, stderr bytes.Buffer
	code := run([]string{"apply", in, plan, "-o", out}, &stdout, &stderr)
	if code != 0 || stderr.Len() != 0 {
		t.Fatalf("exit %d: %s", code, stderr.String())
	}
	for _, key := range []string{`"new_base"`, `"saved"`, `"changed"`, `"stale-values"`, `"parts"`} {
		if !strings.Contains(stdout.String(), key) {
			t.Errorf("result lacks %s: %s", key, stdout.String())
		}
	}
	stdout.Reset()
	// The --recalc step needs chunk G18; until it lands its stub panics.
	func() {
		defer func() {
			if r := recover(); r != nil {
				if msg, ok := r.(string); ok && strings.HasPrefix(msg, "unimplemented: ") {
					t.Logf("--recalc skipped: %s", msg)
					return
				}
				panic(r)
			}
		}()
		code = run([]string{"apply", in, plan, "--recalc", "--dry-run"}, &stdout, &stderr)
		// LibreOffice is found as apply finds it: DD_SOFFICE, else soffice on PATH.
		_, lookErr := exec.LookPath("soffice")
		if os.Getenv("DD_SOFFICE") == "" && lookErr != nil && (code != cli.ExitFailed || !strings.Contains(stderr.String(), "--recalc")) {
			t.Errorf("--recalc without LibreOffice: exit %d %s", code, stderr.String())
		}
	}()
}

func TestCreateCommand(t *testing.T) {
	if !createWired() {
		t.Skip("unimplemented: chunk G19b")
	}
	out := filepath.Join(t.TempDir(), "new.xlsx")
	var stdout, stderr bytes.Buffer
	if code := run([]string{"create", "-o", out}, &stdout, &stderr); code != 0 {
		t.Fatalf("exit %d: %s", code, stderr.String())
	}
	if _, err := os.Stat(out); err != nil {
		t.Error(err)
	}
}

// applyWired reports whether apply is wired (G19 makes it true by
// replacing the notImplemented handler).
// createWired reports whether runCreate is implemented (chunk G19b).
func createWired() bool {
	var stdout, stderr bytes.Buffer
	run([]string{"create", "-o", filepath.Join(os.TempDir(), "dd-create-probe-missing-dir", "x.xlsx")}, &stdout, &stderr)
	return !strings.Contains(stderr.String(), "not implemented")
}

func applyWired() bool {
	var stdout, stderr bytes.Buffer
	run([]string{"apply", "missing.xlsx", "missing.json"}, &stdout, &stderr)
	return !strings.Contains(stderr.String(), "not implemented")
}

// TestHelpOps (G19c): help ops lists every op from ops.Ops; help <op>
// gives its fields, rules and, for structural ops, the two lists.
func TestHelpOps(t *testing.T) {
	if opsHelp() == "The operation reference is not generated yet." {
		t.Skip("unimplemented: chunk G19c")
	}
	var stdout, stderr bytes.Buffer
	if code := run([]string{"help", "ops"}, &stdout, &stderr); code != 0 {
		t.Fatalf("help ops: exit %d %s", code, stderr.String())
	}
	for _, name := range []string{"cell.set", "range.set", "row.insert", "sheet.rename", "name.delete"} {
		if !strings.Contains(stdout.String(), name) {
			t.Errorf("help ops lacks %s", name)
		}
	}
	stdout.Reset()
	if code := run([]string{"help", "row.delete"}, &stdout, &stderr); code != 0 {
		t.Fatalf("help row.delete: exit %d", code)
	}
	for _, want := range []string{"at", "defined names", "pivot table"} {
		if !strings.Contains(stdout.String(), want) {
			t.Errorf("help row.delete lacks %q:\n%s", want, stdout.String())
		}
	}
}
