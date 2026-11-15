package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jhnoel/documentdriver/internal/cli"
	"github.com/jhnoel/documentdriver/internal/xlsx/read"
)

// TestCreateChunkMissingOut: -o is required (usage, path -o).
func TestCreateChunkMissingOut(t *testing.T) {
	for _, args := range [][]string{{"create"}, {"create", "--dry-run"}} {
		code, _, errs := runCLI(t, args...)
		if code != cli.ExitUsage || len(errs) != 1 || errs[0].Code != cli.CodeUsage || errs[0].Path != "-o" {
			t.Errorf("xlsx %v: exit %d, errors %+v; want exit 2, usage at -o", args, code, errs)
		}
	}
}

// TestCreateChunkBlank: no plan writes Sheet1 with only dry_run, saved, new_base.
func TestCreateChunkBlank(t *testing.T) {
	out := filepath.Join(t.TempDir(), "new.xlsx")
	code, stdout, errs := runCLI(t, "create", "-o", out)
	if code != 0 {
		t.Fatalf("exit %d: %+v", code, errs)
	}
	data, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	var keys map[string]json.RawMessage
	if err := json.Unmarshal([]byte(stdout), &keys); err != nil {
		t.Fatal(err)
	}
	if len(keys) != 3 {
		t.Fatalf("blank create keys %v, want 3", keys)
	}
	for _, want := range []string{"dry_run", "saved", "new_base"} {
		if _, ok := keys[want]; !ok {
			t.Errorf("blank create lacks %s: %s", want, stdout)
		}
	}
	for _, absent := range []string{"aliases", "changed", "deleted", "hazards"} {
		if _, ok := keys[absent]; ok {
			t.Errorf("blank create must not print %s: %s", absent, stdout)
		}
	}
	var res struct {
		DryRun  bool   `json:"dry_run"`
		Saved   string `json:"saved"`
		NewBase string `json:"new_base"`
	}
	if err := json.Unmarshal([]byte(stdout), &res); err != nil {
		t.Fatal(err)
	}
	if res.DryRun || res.Saved != out || res.NewBase != cli.Hash(data) {
		t.Errorf("blank create result: %+v, hash %s", res, cli.Hash(data))
	}
	wb, err := read.Workbook(data, read.Options{})
	if err != nil || len(wb.Sheets) != 1 || wb.Sheets[0].Name != "Sheet1" {
		t.Errorf("created workbook not Sheet1: %+v %v", wb, err)
	}
}

// TestCreateChunkDryRun: --dry-run checks and writes nothing.
func TestCreateChunkDryRun(t *testing.T) {
	out := filepath.Join(t.TempDir(), "new.xlsx")
	code, stdout, errs := runCLI(t, "create", "-o", out, "--dry-run")
	if code != 0 {
		t.Fatalf("exit %d: %+v", code, errs)
	}
	if _, err := os.Stat(out); !os.IsNotExist(err) {
		t.Fatalf("dry run wrote %s", out)
	}
	var keys map[string]json.RawMessage
	if err := json.Unmarshal([]byte(stdout), &keys); err != nil {
		t.Fatal(err)
	}
	if _, ok := keys["saved"]; ok {
		t.Errorf("dry run must omit saved: %s", stdout)
	}
	if _, ok := keys["new_base"]; ok {
		t.Errorf("dry run must omit new_base: %s", stdout)
	}
	var res struct {
		DryRun bool `json:"dry_run"`
	}
	if err := json.Unmarshal([]byte(stdout), &res); err != nil || !res.DryRun {
		t.Errorf("dry run result: %s err %v", stdout, err)
	}
}

// TestCreateChunkWithPlan: a plan is applied and its result published.
func TestCreateChunkWithPlan(t *testing.T) {
	dir := t.TempDir()
	planPath := filepath.Join(dir, "plan.json")
	if err := os.WriteFile(planPath, []byte(`{"ops":[{"op":"cell.set","at":"Sheet1!A1","value":"Item"}]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(dir, "new.xlsx")
	code, stdout, errs := runCLI(t, "create", planPath, "-o", out)
	if code != 0 {
		t.Fatalf("exit %d: %+v", code, errs)
	}
	data, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	var keys map[string]json.RawMessage
	if err := json.Unmarshal([]byte(stdout), &keys); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"dry_run", "saved", "new_base", "aliases", "changed", "deleted", "hazards"} {
		if _, ok := keys[want]; !ok {
			t.Errorf("plan create lacks %s: %s", want, stdout)
		}
	}
	if !strings.Contains(string(keys["changed"]), "Sheet1!A1") {
		t.Errorf("changed lacks Sheet1!A1: %s", stdout)
	}
	var res struct {
		DryRun  bool   `json:"dry_run"`
		Saved   string `json:"saved"`
		NewBase string `json:"new_base"`
	}
	if err := json.Unmarshal([]byte(stdout), &res); err != nil {
		t.Fatal(err)
	}
	if res.DryRun || res.Saved != out || res.NewBase != cli.Hash(data) {
		t.Errorf("plan create result: %+v", res)
	}
	cells, err := read.Cells(data, read.CellsRequest{Range: "Sheet1!A1"}, read.Options{})
	if err != nil || cells.Cells.Len() != 1 {
		t.Errorf("created cell not written: %v", err)
	}
}

// TestCreateChunkWithPlanDryRun: --dry-run with a plan checks without writing.
func TestCreateChunkWithPlanDryRun(t *testing.T) {
	dir := t.TempDir()
	planPath := filepath.Join(dir, "plan.json")
	if err := os.WriteFile(planPath, []byte(`{"ops":[{"op":"cell.set","at":"Sheet1!A1","value":"Item"}]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(dir, "new.xlsx")
	code, stdout, errs := runCLI(t, "create", planPath, "-o", out, "--dry-run")
	if code != 0 {
		t.Fatalf("exit %d: %+v", code, errs)
	}
	if _, err := os.Stat(out); !os.IsNotExist(err) {
		t.Fatalf("dry run wrote %s", out)
	}
	var keys map[string]json.RawMessage
	if err := json.Unmarshal([]byte(stdout), &keys); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"dry_run", "aliases", "changed", "deleted", "hazards"} {
		if _, ok := keys[want]; !ok {
			t.Errorf("dry plan create lacks %s: %s", want, stdout)
		}
	}
	if strings.Contains(stdout, `"saved"`) || strings.Contains(stdout, `"new_base"`) {
		t.Errorf("dry plan create must omit saved/new_base: %s", stdout)
	}
}

// TestCreateChunkPlanWithBase: a plan with base is invalid_plan at base.
func TestCreateChunkPlanWithBase(t *testing.T) {
	dir := t.TempDir()
	planPath := filepath.Join(dir, "plan.json")
	base := "sha256:" + strings.Repeat("0", 64)
	plan := `{"base":"` + base + `","ops":[{"op":"cell.set","at":"Sheet1!A1","value":"Item"}]}`
	if err := os.WriteFile(planPath, []byte(plan), 0o600); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(dir, "new.xlsx")
	code, _, errs := runCLI(t, "create", planPath, "-o", out)
	if code != cli.ExitFailed || len(errs) != 1 || errs[0].Code != cli.CodeInvalidPlan || errs[0].Path != "base" {
		t.Fatalf("exit %d, errors %+v; want exit 1, invalid_plan at base", code, errs)
	}
	if _, err := os.Stat(out); !os.IsNotExist(err) {
		t.Fatal("refused create wrote output")
	}
}

// TestCreateChunkBadPlan: a missing plan file is io.
func TestCreateChunkBadPlan(t *testing.T) {
	dir := t.TempDir()
	out := filepath.Join(dir, "new.xlsx")
	code, _, errs := runCLI(t, "create", filepath.Join(dir, "missing.json"), "-o", out)
	if code != cli.ExitFailed || len(errs) != 1 || errs[0].Code != cli.CodeIO {
		t.Fatalf("exit %d, errors %+v; want exit 1, io", code, errs)
	}
	if _, err := os.Stat(out); !os.IsNotExist(err) {
		t.Fatal("failed create wrote output")
	}
}

// TestCreateChunkPretty: --pretty is honoured.
func TestCreateChunkPretty(t *testing.T) {
	out := filepath.Join(t.TempDir(), "new.xlsx")
	code, stdout, errs := runCLI(t, "create", "-o", out, "--pretty")
	if code != 0 {
		t.Fatalf("exit %d: %+v", code, errs)
	}
	if !strings.Contains(stdout, "\n  ") {
		t.Errorf("pretty output not indented: %q", stdout)
	}
	var v any
	if err := json.Unmarshal([]byte(stdout), &v); err != nil {
		t.Fatal(err)
	}
}
