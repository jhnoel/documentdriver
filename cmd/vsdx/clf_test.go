package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jhnoel/documentdriver/internal/cli"
)

// TestHelpApplyComplete checks the CLF row: `vsdx help apply` says what
// apply prints, that it runs in place without -o, --dry-run, a plan
// example, the base and units fields, and the contract.
func TestHelpApplyComplete(t *testing.T) {
	code, out, _ := runCLI(t, "help", "apply")
	if code != cli.ExitOK {
		t.Fatalf("help apply exit %d", code)
	}
	for _, want := range []string{
		"base and new_base", "aliases", "deleted", "hazards", "parts",
		"in place", "-o", "atomically", "--dry-run",
		`{"ops":[{"op":"object.move","at":"page0/s5","dx":10,"dy":0}]}`,
		"base", "units",
		"the contract: docs/contract.md and docs/vsdx-contract-draft.md",
		"`vsdx help ops`",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("help apply lacks %q:\n%s", want, out)
		}
	}
}

// jsonKeys returns the top-level keys of a JSON object in order.
func jsonKeys(t *testing.T, s string) []string {
	t.Helper()
	dec := json.NewDecoder(strings.NewReader(s))
	tok, err := dec.Token()
	if err != nil || tok != json.Delim('{') {
		t.Fatalf("JSON %s: first token %v, %v", s, tok, err)
	}
	var keys []string
	for dec.More() {
		tok, err := dec.Token()
		if err != nil {
			t.Fatalf("JSON %s: key token: %v", s, err)
		}
		keys = append(keys, tok.(string))
		var raw json.RawMessage
		if err := dec.Decode(&raw); err != nil {
			t.Fatalf("JSON %s: value: %v", s, err)
		}
	}
	return keys
}

// TestCreateKeyOrderEqualsApply checks the CLF row: create prints its
// keys in apply's order, as a plan and blank.
func TestCreateKeyOrderEqualsApply(t *testing.T) {
	cliIn, _ := blankFiles(t)
	dir := t.TempDir()
	planPath := filepath.Join(dir, "c2.json")
	if err := os.WriteFile(planPath, createPlan(t, "c2-shapes.json"), 0o600); err != nil {
		t.Fatal(err)
	}
	clock := defaultClock.Format(time.RFC3339)
	applyOut := filepath.Join(dir, "apply.vsdx")
	code, applyOutText, errs := runCLI(t, "apply", cliIn, planPath, "-o", applyOut, "--seed", "0", "--clock", clock)
	if code != cli.ExitOK {
		t.Fatalf("apply exit %d: %+v", code, errs)
	}
	applyKeys := jsonKeys(t, applyOutText)
	createOut := filepath.Join(dir, "create.vsdx")
	code, createOutText, errs := runCLI(t, "create", planPath, "-o", createOut, "--seed", "0", "--clock", clock)
	if code != cli.ExitOK {
		t.Fatalf("create exit %d: %+v", code, errs)
	}
	var want []string
	for _, k := range applyKeys {
		if k != "base" {
			want = append(want, k)
		}
	}
	got := jsonKeys(t, createOutText)
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("create keys %v, want apply's %v", got, want)
	}
	blankOut := filepath.Join(dir, "blank.vsdx")
	code, blankText, errs := runCLI(t, "create", "-o", blankOut, "--seed", "0", "--clock", clock)
	if code != cli.ExitOK {
		t.Fatalf("blank create exit %d: %+v", code, errs)
	}
	if got := jsonKeys(t, blankText); strings.Join(got, ",") != "new_base,dry_run,saved" {
		t.Errorf("blank create keys %v, want [new_base dry_run saved] in apply's order", got)
	}
}
