package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jhnoel/documentdriver/internal/cli"
	"github.com/jhnoel/documentdriver/internal/xlsx/xlsxtest"
)

// G19a chunk tests: apply end to end without --recalc (which needs
// G17/G18 when soffice is present), --changed ids/full, dry-run,
// in-place, --pretty, refusal paths and the part names.

func chunkBook(t *testing.T) string {
	t.Helper()
	in, err := xlsxtest.Book{Sheets: []xlsxtest.Sheet{{Name: "Budget", Rows: `<row r="1"><c r="A1"><v>1</v></c></row>`}}}.Write(t.TempDir(), "in.xlsx")
	if err != nil {
		t.Fatal(err)
	}
	return in
}

func TestApplyChunkCellSet(t *testing.T) {
	xlsxtest.Skip(t, func() {
		dir := t.TempDir()
		in := chunkBook(t)
		plan := filepath.Join(dir, "plan.json")
		if err := os.WriteFile(plan, []byte(`{"ops":[{"op":"cell.set","at":"Budget!B1","value":2}]}`), 0o600); err != nil {
			t.Fatal(err)
		}
		out := filepath.Join(dir, "out.xlsx")
		var stdout, stderr bytes.Buffer
		if code := run([]string{"apply", in, plan, "-o", out}, &stdout, &stderr); code != cli.ExitOK {
			t.Fatalf("exit %d: %s", code, stderr.String())
		}
		var res map[string]json.RawMessage
		if err := json.Unmarshal(bytes.TrimSpace(stdout.Bytes()), &res); err != nil {
			t.Fatal(err)
		}
		for _, key := range []string{"base", "new_base", "dry_run", "saved", "aliases", "changed", "deleted", "hazards", "parts"} {
			if _, ok := res[key]; !ok {
				t.Errorf("result lacks %s: %s", key, stdout.String())
			}
		}
		if !strings.Contains(stdout.String(), `"id":"Budget!B1"`) {
			t.Errorf("changed lacks the cell: %s", stdout.String())
		}
		if !strings.Contains(stdout.String(), `"stale-values"`) {
			t.Errorf("hazards lack stale-values: %s", stdout.String())
		}
		// Part names as docx and pptx print them (V: was without the
		// leading slash, unlike the other tools).
		if !strings.Contains(stdout.String(), `"/xl/worksheets/sheet1.xml"`) {
			t.Errorf("parts lack the sheet: %s", stdout.String())
		}
		if _, err := os.Stat(out); err != nil {
			t.Error(err)
		}
	})
}

func TestApplyChunkChangedIDsAndPretty(t *testing.T) {
	xlsxtest.Skip(t, func() {
		dir := t.TempDir()
		in := chunkBook(t)
		plan := filepath.Join(dir, "plan.json")
		if err := os.WriteFile(plan, []byte(`{"ops":[{"op":"cell.set","at":"Budget!B1","value":"hi"}]}`), 0o600); err != nil {
			t.Fatal(err)
		}
		out := filepath.Join(dir, "out.xlsx")
		var stdout, stderr bytes.Buffer
		if code := run([]string{"apply", in, plan, "-o", out, "--changed", "ids", "--pretty"}, &stdout, &stderr); code != cli.ExitOK {
			t.Fatalf("exit %d: %s", code, stderr.String())
		}
		if !strings.Contains(stdout.String(), `"Budget!B1"`) || strings.Contains(stdout.String(), `"cells"`) {
			t.Errorf("--changed ids: %s", stdout.String())
		}
		if !strings.Contains(stdout.String(), "\n  \"base\"") {
			t.Errorf("--pretty not indented: %s", stdout.String())
		}
	})
}

func TestApplyChunkDryRunAndInPlace(t *testing.T) {
	xlsxtest.Skip(t, func() {
		dir := t.TempDir()
		in := chunkBook(t)
		before, _ := os.ReadFile(in)
		plan := filepath.Join(dir, "plan.json")
		if err := os.WriteFile(plan, []byte(`{"ops":[{"op":"cell.set","at":"Budget!B1","value":5}]}`), 0o600); err != nil {
			t.Fatal(err)
		}
		var stdout, stderr bytes.Buffer
		if code := run([]string{"apply", in, plan, "--dry-run"}, &stdout, &stderr); code != cli.ExitOK {
			t.Fatalf("dry-run exit %d: %s", code, stderr.String())
		}
		if !strings.Contains(stdout.String(), `"dry_run":true`) || strings.Contains(stdout.String(), `"saved"`) {
			t.Errorf("dry-run result: %s", stdout.String())
		}
		if got, _ := os.ReadFile(in); !bytes.Equal(got, before) {
			t.Error("dry-run wrote the input")
		}
		// In place: no -o replaces the file.
		stdout.Reset()
		stderr.Reset()
		if code := run([]string{"apply", in, plan}, &stdout, &stderr); code != cli.ExitOK {
			t.Fatalf("in-place exit %d: %s", code, stderr.String())
		}
		if got, _ := os.ReadFile(in); bytes.Equal(got, before) {
			t.Error("in-place apply wrote nothing")
		}
	})
}

func TestApplyChunkRecalcUnavailable(t *testing.T) {
	// Deterministic without depending on PATH: DD_SOFFICE names a
	// missing binary, so the early check refuses at --recalc before
	// any recalc chunk is needed.
	t.Setenv("DD_SOFFICE", filepath.Join(t.TempDir(), "no-soffice"))
	dir := t.TempDir()
	in := chunkBook(t)
	plan := filepath.Join(dir, "plan.json")
	if err := os.WriteFile(plan, []byte(`{"ops":[{"op":"cell.set","at":"Budget!B1","value":2}]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	code := run([]string{"apply", in, plan, "--recalc", "--dry-run"}, &stdout, &stderr)
	if code != cli.ExitFailed || stdout.Len() != 0 || !strings.Contains(stderr.String(), `"--recalc"`) {
		t.Errorf("recalc unavailable: exit %d stdout %q stderr %s", code, stdout.String(), stderr.String())
	}
}

func TestApplyChunkRefusals(t *testing.T) {
	xlsxtest.Skip(t, func() {
		dir := t.TempDir()
		in := chunkBook(t)
		// Bad plan shape: invalid_plan.
		bad := filepath.Join(dir, "bad.json")
		if err := os.WriteFile(bad, []byte(`{"ops":[{"op":"cell.set","at":"B1","value":1}]}`), 0o600); err != nil {
			t.Fatal(err)
		}
		var stdout, stderr bytes.Buffer
		if code := run([]string{"apply", in, bad}, &stdout, &stderr); code != cli.ExitFailed || !strings.Contains(stderr.String(), "invalid_operation") {
			t.Errorf("bad address: exit %d stderr %s", code, stderr.String())
		}
		// Unknown sheet: not_found at ops[0].at.
		missing := filepath.Join(dir, "missing.json")
		if err := os.WriteFile(missing, []byte(`{"ops":[{"op":"cell.set","at":"Nope!A1","value":1}]}`), 0o600); err != nil {
			t.Fatal(err)
		}
		stdout.Reset()
		stderr.Reset()
		if code := run([]string{"apply", in, missing}, &stdout, &stderr); code != cli.ExitFailed || !strings.Contains(stderr.String(), "not_found") {
			t.Errorf("missing sheet: exit %d stderr %s", code, stderr.String())
		}
		// Missing plan file: io.
		stdout.Reset()
		stderr.Reset()
		if code := run([]string{"apply", in, filepath.Join(dir, "no.json")}, &stdout, &stderr); code != cli.ExitFailed || !strings.Contains(stderr.String(), "io") {
			t.Errorf("missing plan: exit %d stderr %s", code, stderr.String())
		}
		// Malformed JSON: invalid_json or invalid_plan, never stdout.
		broken := filepath.Join(dir, "broken.json")
		if err := os.WriteFile(broken, []byte(`{`), 0o600); err != nil {
			t.Fatal(err)
		}
		stdout.Reset()
		stderr.Reset()
		if code := run([]string{"apply", in, broken}, &stdout, &stderr); code == cli.ExitOK || stdout.Len() != 0 {
			t.Errorf("broken plan: exit %d stdout %q", code, stdout.String())
		}
	})
}
