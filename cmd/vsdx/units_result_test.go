package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jhnoel/documentdriver/internal/cli"
)

// TestApplyUnitsEcho checks the CC9 rows: apply of a plan with
// "units":"in" and object.move prints "units":"in" in second position
// (after base); without units the key is absent.
func TestApplyUnitsEcho(t *testing.T) {
	file := drawingFile(t)
	dir := t.TempDir()
	out := filepath.Join(dir, "out.vsdx")
	planPath := filepath.Join(dir, "plan.json")
	if err := os.WriteFile(planPath, []byte(`{"units":"in","ops":[{"op":"object.move","at":"page0/s1","dx":1,"dy":0}]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	code, stdout, errs := runCLI(t, "apply", file, planPath, "-o", out, "--seed", "0", "--clock", defaultClock.Format(time.RFC3339))
	if code != cli.ExitOK {
		t.Fatalf("apply with units exit %d: %+v", code, errs)
	}
	keys := jsonKeys(t, stdout)
	if len(keys) < 2 || keys[0] != "base" || keys[1] != "units" {
		t.Errorf("apply keys %v, want units second after base: %s", keys, stdout)
	}
	if !strings.Contains(stdout, `"units": "in"`) {
		t.Errorf("apply stdout lacks units in: %s", stdout)
	}
	file2 := drawingFile(t)
	planPath2 := filepath.Join(dir, "plan2.json")
	if err := os.WriteFile(planPath2, []byte(`{"ops":[{"op":"object.move","at":"page0/s1","dx":1,"dy":0}]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	out2 := filepath.Join(dir, "out2.vsdx")
	code, stdout2, errs := runCLI(t, "apply", file2, planPath2, "-o", out2, "--seed", "0", "--clock", defaultClock.Format(time.RFC3339))
	if code != cli.ExitOK {
		t.Fatalf("apply without units exit %d: %+v", code, errs)
	}
	if strings.Contains(stdout2, `"units"`) {
		t.Errorf("apply without units prints a units key: %s", stdout2)
	}
}

// TestCreateUnitsEcho checks the CC9 create row: create with units
// "in" echoes it after new_base; without units the key is absent.
func TestCreateUnitsEcho(t *testing.T) {
	dir := t.TempDir()
	planPath := filepath.Join(dir, "plan.json")
	if err := os.WriteFile(planPath, []byte(`{"units":"in","ops":[{"op":"object.add","at":"page0","geom":"rect","as":"a","box":[1,1,2,1],"html":"<p>Start</p>"}]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(dir, "out.vsdx")
	code, stdout, errs := runCLI(t, "create", planPath, "-o", out, "--seed", "0", "--clock", defaultClock.Format(time.RFC3339))
	if code != cli.ExitOK {
		t.Fatalf("create with units exit %d: %+v", code, errs)
	}
	keys := jsonKeys(t, stdout)
	if len(keys) < 2 || keys[0] != "new_base" || keys[1] != "units" {
		t.Errorf("create keys %v, want units after new_base: %s", keys, stdout)
	}
	if !strings.Contains(stdout, `"units": "in"`) {
		t.Errorf("create stdout lacks units in: %s", stdout)
	}
	planPath2 := filepath.Join(dir, "plan2.json")
	if err := os.WriteFile(planPath2, []byte(`{"ops":[{"op":"object.add","at":"page0","geom":"rect","as":"a","box":[72,72,144,72],"html":"<p>Start</p>"}]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	out2 := filepath.Join(dir, "out2.vsdx")
	code, stdout2, errs := runCLI(t, "create", planPath2, "-o", out2, "--seed", "0", "--clock", defaultClock.Format(time.RFC3339))
	if code != cli.ExitOK {
		t.Fatalf("create without units exit %d: %+v", code, errs)
	}
	if strings.Contains(stdout2, `"units"`) {
		t.Errorf("create without units prints a units key: %s", stdout2)
	}
}
