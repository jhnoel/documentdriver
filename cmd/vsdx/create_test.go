package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jhnoel/documentdriver/internal/cli"
	"github.com/jhnoel/documentdriver/internal/vsdx/create"
)

// TestCreateChunkMissingOut checks the CR1 usage row: create without
// -o is usage at -o, with or without a plan or --dry-run.
func TestCreateChunkMissingOut(t *testing.T) {
	plan := createPlan(t, "c2-shapes.json")
	dir := t.TempDir()
	planPath := filepath.Join(dir, "c2.json")
	if err := os.WriteFile(planPath, plan, 0o600); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{"create"}, {"create", "--dry-run"}, {"create", planPath}, {"create", planPath, "--dry-run"}} {
		code, _, errs := runCLI(t, args...)
		if code != cli.ExitUsage || len(errs) != 1 || errs[0].Code != cli.CodeUsage || errs[0].Path != "-o" {
			t.Errorf("vsdx %v: exit %d, errors %+v; want exit 2, usage at -o", args, code, errs)
		}
	}
}

// TestCreateChunkBlank checks the CR1 blank row: create at the fixed
// clock writes Template's bytes unchanged, prints exactly dry_run,
// saved and new_base, and reads back as the one empty Page-1.
func TestCreateChunkBlank(t *testing.T) {
	clock := defaultClock
	out := filepath.Join(t.TempDir(), "out.vsdx")
	code, stdout, errs := runCLI(t, "create", "-o", out, "--clock", clock.Format(time.RFC3339))
	if code != cli.ExitOK {
		t.Fatalf("exit %d: %+v", code, errs)
	}
	data, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	template, err := create.Template(clock)
	if err != nil {
		t.Fatalf("Template: %v", err)
	}
	if !bytes.Equal(data, template) {
		t.Errorf("blank create differs from Template bytes (%d vs %d)", len(data), len(template))
	}
	var keys map[string]json.RawMessage
	if err := json.Unmarshal([]byte(stdout), &keys); err != nil {
		t.Fatal(err)
	}
	if len(keys) != 3 {
		t.Fatalf("blank create keys %v, want 3", keys)
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
		t.Errorf("blank create result: %+v", res)
	}
	code, readOut, readErrs := runCLI(t, "read", out)
	if code != cli.ExitOK {
		t.Fatalf("read exit %d: %+v", code, readErrs)
	}
	want := `{"base":"` + cli.Hash(data) + `","pages":[{"id":"page0","name":"Page-1","size":[612,792],"orientation":"portrait","print_orientation":"portrait","objects":0}],"masters":[],"notes":[]}` + "\n"
	if readOut != want {
		t.Errorf("read stdout = %s, want %s", readOut, want)
	}
}

// TestCreateChunkBlankDryRun checks --dry-run on the blank: nothing is
// written and saved and new_base are omitted.
func TestCreateChunkBlankDryRun(t *testing.T) {
	out := filepath.Join(t.TempDir(), "out.vsdx")
	code, stdout, errs := runCLI(t, "create", "-o", out, "--dry-run")
	if code != cli.ExitOK {
		t.Fatalf("exit %d: %+v", code, errs)
	}
	if _, err := os.Stat(out); !os.IsNotExist(err) {
		t.Fatalf("dry run wrote %s", out)
	}
	var res struct {
		DryRun bool `json:"dry_run"`
	}
	if err := json.Unmarshal([]byte(stdout), &res); err != nil || !res.DryRun {
		t.Errorf("dry result %s err %v", stdout, err)
	}
	if strings.Contains(stdout, `"saved"`) || strings.Contains(stdout, `"new_base"`) {
		t.Errorf("dry result must omit saved and new_base: %s", stdout)
	}
}

// TestCreateChunkWithPlan checks create c2 and c3 against the OA1 and
// OP1 rows: aliases, changed units, hazards and the read summary.
func TestCreateChunkWithPlan(t *testing.T) {
	clockStr := defaultClock.Format(time.RFC3339)
	for _, c := range []struct {
		plan    string
		alias   string
		changed string
		single  string
	}{
		{"c2-shapes.json", "a", "page0/s1", "page0/s3"},
		{"c3-pages.json", "p2", "page1", "page1/s1"},
	} {
		dir := t.TempDir()
		planPath := filepath.Join(dir, "plan.json")
		if err := os.WriteFile(planPath, createPlan(t, c.plan), 0o600); err != nil {
			t.Fatal(err)
		}
		out := filepath.Join(dir, "out.vsdx")
		code, stdout, errs := runCLI(t, "create", planPath, "-o", out, "--seed", "0", "--clock", clockStr)
		if code != cli.ExitOK {
			t.Fatalf("%s exit %d: %+v", c.plan, code, errs)
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
				t.Errorf("%s lacks %s: %s", c.plan, want, stdout)
			}
		}
		if !strings.Contains(string(keys["changed"]), c.changed) || !strings.Contains(string(keys["changed"]), c.single) {
			t.Errorf("%s changed lacks %s or %s: %s", c.plan, c.changed, c.single, stdout)
		}
		var res struct {
			Aliases map[string]string `json:"aliases"`
			DryRun  bool              `json:"dry_run"`
			Saved   string            `json:"saved"`
			NewBase string            `json:"new_base"`
		}
		if err := json.Unmarshal([]byte(stdout), &res); err != nil {
			t.Fatal(err)
		}
		if res.DryRun || res.Saved != out || res.NewBase != cli.Hash(data) {
			t.Errorf("%s result: %+v", c.plan, res)
		}
		if got := res.Aliases[c.alias]; got == "" {
			t.Errorf("%s aliases %v lack %s", c.plan, res.Aliases, c.alias)
		}
	}
	// c3 reads back as the two OP1 pages.
	dir := t.TempDir()
	planPath := filepath.Join(dir, "c3.json")
	if err := os.WriteFile(planPath, createPlan(t, "c3-pages.json"), 0o600); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(dir, "out.vsdx")
	if code, _, errs := runCLI(t, "create", planPath, "-o", out, "--seed", "0", "--clock", clockStr); code != cli.ExitOK {
		t.Fatalf("c3 create exit %d: %+v", code, errs)
	}
	code, readOut, readErrs := runCLI(t, "read", out)
	if code != cli.ExitOK {
		t.Fatalf("read exit %d: %+v", code, readErrs)
	}
	data, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	want := `{"base":"` + cli.Hash(data) + `","pages":[{"id":"page0","name":"Overview","size":[792,612],"orientation":"landscape","print_orientation":"landscape","objects":0},{"id":"page1","name":"Wide","size":[1224,792],"orientation":"landscape","print_orientation":"landscape","objects":1}],"masters":[],"notes":[]}` + "\n"
	if readOut != want {
		t.Errorf("c3 read stdout = %s, want %s", readOut, want)
	}
}

// TestCreateChunkRefusals checks c4, c5 and c6: not_found at
// ops[0].master, operation_failed at ops[1].to_glue, and invalid_plan
// at base, with nothing written.
func TestCreateChunkRefusals(t *testing.T) {
	for _, c := range []struct {
		plan, code, path string
	}{
		{"c4-refused-master.json", cli.CodeNotFound, "ops[0].master"},
		{"c5-refused-glue.json", cli.CodeOperationFailed, "ops[1].to_glue"},
		{"c6-refused-base.json", cli.CodeInvalidPlan, "base"},
	} {
		dir := t.TempDir()
		planPath := filepath.Join(dir, "plan.json")
		if err := os.WriteFile(planPath, createPlan(t, c.plan), 0o600); err != nil {
			t.Fatal(err)
		}
		out := filepath.Join(dir, "out.vsdx")
		code, _, errs := runCLI(t, "create", planPath, "-o", out, "--seed", "0", "--clock", defaultClock.Format(time.RFC3339))
		if code != cli.ExitFailed || len(errs) != 1 || errs[0].Code != c.code || errs[0].Path != c.path {
			t.Errorf("%s: exit %d errors %+v; want exit 1 %s at %q", c.plan, code, errs, c.code, c.path)
		}
		if _, err := os.Stat(out); !os.IsNotExist(err) {
			t.Errorf("%s: refused create wrote output", c.plan)
		}
	}
}

// TestCreateChunkWithPlanDryRun checks --dry-run with a plan: checked
// and applied in memory, nothing written, saved and new_base omitted.
func TestCreateChunkWithPlanDryRun(t *testing.T) {
	dir := t.TempDir()
	planPath := filepath.Join(dir, "c2.json")
	if err := os.WriteFile(planPath, createPlan(t, "c2-shapes.json"), 0o600); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(dir, "out.vsdx")
	code, stdout, errs := runCLI(t, "create", planPath, "-o", out, "--dry-run", "--seed", "0", "--clock", defaultClock.Format(time.RFC3339))
	if code != cli.ExitOK {
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
		t.Errorf("dry plan create must omit saved and new_base: %s", stdout)
	}
}

// TestCreateChunkDeterministic checks the CR1 determinism row: the
// same plan and clock twice give equal bytes.
func TestCreateChunkDeterministic(t *testing.T) {
	dir := t.TempDir()
	planPath := filepath.Join(dir, "c2.json")
	if err := os.WriteFile(planPath, createPlan(t, "c2-shapes.json"), 0o600); err != nil {
		t.Fatal(err)
	}
	clockStr := defaultClock.Format(time.RFC3339)
	first := filepath.Join(dir, "a.vsdx")
	second := filepath.Join(dir, "b.vsdx")
	for _, out := range []string{first, second} {
		code, _, errs := runCLI(t, "create", planPath, "-o", out, "--seed", "0", "--clock", clockStr)
		if code != cli.ExitOK {
			t.Fatalf("exit %d: %+v", code, errs)
		}
	}
	a, err := os.ReadFile(first)
	if err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(second)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(a, b) {
		t.Errorf("two creates with the same plan and clock differ (%d vs %d bytes)", len(a), len(b))
	}
}

// TestCreateChunkBadPlan checks the plumbing: a missing plan file is
// io, malformed JSON is invalid_json, a bad --seed or --clock is usage.
func TestCreateChunkBadPlan(t *testing.T) {
	dir := t.TempDir()
	out := filepath.Join(dir, "out.vsdx")
	code, _, errs := runCLI(t, "create", filepath.Join(dir, "missing.json"), "-o", out)
	if code != cli.ExitFailed || len(errs) != 1 || errs[0].Code != cli.CodeIO {
		t.Fatalf("missing plan: exit %d errors %+v; want exit 1 io", code, errs)
	}
	if _, err := os.Stat(out); !os.IsNotExist(err) {
		t.Fatal("failed create wrote output")
	}
	bad := planFile(t, `{"ops":`)
	code, _, errs = runCLI(t, "create", bad, "-o", out)
	if code != cli.ExitFailed || len(errs) != 1 || errs[0].Code != cli.CodeInvalidJSON {
		t.Fatalf("bad json: exit %d errors %+v; want exit 1 invalid_json", code, errs)
	}
	if code, _, errs := runCLI(t, "create", "-o", out, "--seed", "bogus"); code != cli.ExitUsage || len(errs) != 1 || errs[0].Code != cli.CodeUsage || errs[0].Path != "--seed" {
		t.Errorf("--seed bogus: exit %d errors %+v; want usage at --seed", code, errs)
	}
	if code, _, errs := runCLI(t, "create", "-o", out, "--clock", "bogus"); code != cli.ExitUsage || len(errs) != 1 || errs[0].Code != cli.CodeUsage || errs[0].Path != "--clock" {
		t.Errorf("--clock bogus: exit %d errors %+v; want usage at --clock", code, errs)
	}
}
