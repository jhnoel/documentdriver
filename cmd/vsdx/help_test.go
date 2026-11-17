package main

import (
	"strings"
	"testing"

	"github.com/jhnoel/documentdriver/internal/cli"
	"github.com/jhnoel/documentdriver/internal/vsdx/ops"
)

// TestHelpOps checks the A2 row: `vsdx help ops` lists the table's
// implemented ops, generated from ops.Ops (never hand-written).
func TestHelpOps(t *testing.T) {
	code, out, _ := runCLI(t, "help", "ops")
	if code != cli.ExitOK {
		t.Fatalf("help ops exit %d", code)
	}
	if out != opsHelp() {
		t.Errorf("help ops stdout is not opsHelp()")
	}
	for i := range ops.Ops {
		d := &ops.Ops[i]
		if !d.Implemented() {
			if strings.Contains(out, d.Name) {
				t.Errorf("help ops lists unimplemented op %s", d.Name)
			}
			continue
		}
		if !strings.Contains(out, d.Name) {
			t.Errorf("help ops lacks implemented op %s", d.Name)
		}
		text, ok := opHelp(d.Name)
		if !ok {
			t.Errorf("opHelp(%s) reports no help", d.Name)
			continue
		}
		if !strings.Contains(text, d.Summary) {
			t.Errorf("help %s lacks its summary", d.Name)
		}
		if code, got, _ := runCLI(t, "help", d.Name); code != cli.ExitOK || got != text {
			t.Errorf("help %s exit %d, stdout differs from opHelp", d.Name, code)
		}
	}
	if text, ok := opHelp("not.an.op"); ok || text != "" {
		t.Errorf("opHelp(not.an.op) = %q, %v; want \"\", false", text, ok)
	}
	for _, want := range []string{"help <op>", "vsdx"} {
		if !strings.Contains(out, want) {
			t.Errorf("help ops footer lacks %q", want)
		}
	}
}

// TestHelpUnimplementedOp checks an unimplemented op has no help: it is not listed
// in opsHelp, so `vsdx help <op>` is usage.
func TestHelpUnimplementedOp(t *testing.T) {
	for i := range ops.Ops {
		if ops.Ops[i].Implemented() {
			continue
		}
		code, _, errs := runCLI(t, "help", ops.Ops[i].Name)
		if code != cli.ExitUsage || len(errs) != 1 || errs[0].Code != cli.CodeUsage {
			t.Errorf("help %s: exit %d errors %+v; want usage", ops.Ops[i].Name, code, errs)
		}
	}
}
