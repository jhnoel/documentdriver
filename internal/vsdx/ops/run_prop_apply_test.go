package ops_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jhnoel/documentdriver/internal/cli"
	"github.com/jhnoel/documentdriver/internal/vsdx/apply"
	"github.com/jhnoel/documentdriver/internal/vsdx/ops"
	"github.com/jhnoel/documentdriver/internal/vsdx/read"
	"github.com/jhnoel/documentdriver/internal/vsdx/vsdxtest"
)

// runPlan parses text and applies it to path, returning the result
// and the output path.
func runPlan(t *testing.T, path, text string) (out string, changed []json.RawMessage) {
	t.Helper()
	p, errs := ops.ParsePlan([]byte(text))
	if len(errs) != 0 || p == nil {
		t.Fatalf("ParsePlan(%s): %v errors %v", text, p, errs)
	}
	out = filepath.Join(t.TempDir(), "out.vsdx")
	res, err := apply.Run(path, p, apply.Options{Out: out})
	if err != nil {
		t.Fatalf("apply.Run(%s): %v", text, err)
	}
	return out, res.Changed
}

// TestApplyPropAddChecks checks the OD1 rows through apply: a
// prop.add of Owner gives a changed prop and read props with
// label Owner, type string and value Ada.
func TestApplyPropAddChecks(t *testing.T) {
	src := vsdxtest.Fixture(t, "v1-minimal")
	out, changed := runPlan(t, src, `{"ops":[{"op":"prop.add","at":"page0/s1","name":"Owner","type":"string","value":"Ada"}]}`)
	if len(changed) != 1 {
		t.Fatalf("changed = %v, want one prop", changed)
	}
	if string(changed[0]) != `{"id":"page0/s1/prop:Owner","label":"Owner","type":"string","value":"Ada"}` {
		t.Errorf("changed prop = %s", changed[0])
	}
	data, err := os.ReadFile(out)
	if err != nil {
		t.Fatalf("read output: %v", err)
	}
	got, err := read.Page(data, "B", "page0", read.Options{})
	if err != nil {
		t.Fatalf("read.Page: %v", err)
	}
	if !strings.Contains(string(got), `"page0/s1/prop:Owner"`) || !strings.Contains(string(got), `"value":"Ada"`) {
		t.Errorf("read page = %s, want Owner Ada", got)
	}
}

// TestApplyPropExistingPath checks the OD1 row: a prop.add of an
// existing name refuses with operation_failed at ops[0].name and
// leaves the file unchanged.
func TestApplyPropExistingPath(t *testing.T) {
	src := vsdxtest.Fixture(t, "v2-master")
	before, err := os.ReadFile(src)
	if err != nil {
		t.Fatalf("read before: %v", err)
	}
	p, errs := ops.ParsePlan([]byte(`{"ops":[{"op":"prop.add","at":"page0/s2","name":"Cost","type":"currency","value":1}]}`))
	if len(errs) != 0 || p == nil {
		t.Fatalf("ParsePlan: %v %v", p, errs)
	}
	out := filepath.Join(t.TempDir(), "out.vsdx")
	_, err = apply.Run(src, p, apply.Options{Out: out})
	var ce *cli.Error
	if !errors.As(err, &ce) || ce.Code != cli.CodeOperationFailed || ce.Path != "ops[0].name" {
		t.Errorf("got %v, want operation_failed at ops[0].name", err)
	}
	if _, err := os.Stat(out); !errors.Is(err, os.ErrNotExist) {
		t.Error("a refused plan wrote its out file")
	}
	after, err := os.ReadFile(src)
	if err != nil {
		t.Fatalf("read after: %v", err)
	}
	if !bytes.Equal(before, after) {
		t.Error("a refused plan changed the input")
	}
}
