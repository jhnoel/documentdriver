package create

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/jhnoel/documentdriver/internal/cli"
	"github.com/jhnoel/documentdriver/internal/ids"
	"github.com/jhnoel/documentdriver/internal/repo"
	"github.com/jhnoel/documentdriver/internal/vsdx/ops"
)

// fixedClock is the deterministic clock of the CR1 rows.
var fixedClock = time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC)

// createPlanText reads the designer-authored create plan name from
// testdata/vsdx/create/plans.
func createPlanText(t *testing.T, name string) string {
	t.Helper()
	root, err := repo.Root()
	if err != nil {
		t.Fatalf("repo root: %v", err)
	}
	b, err := os.ReadFile(filepath.Join(root, "testdata", "vsdx", "create", "plans", name))
	if err != nil {
		t.Fatalf("read %s: %v", name, err)
	}
	return string(b)
}

// TestRunBlank checks the CR1 blank row: no plan and an empty plan both
// write Template's bytes unchanged, with Saved and NewBase set and Base
// empty.
func TestRunBlank(t *testing.T) {
	template, err := Template(fixedClock)
	if err != nil {
		t.Fatalf("Template: %v", err)
	}
	for _, p := range []*ops.Plan{nil, {}} {
		out := filepath.Join(t.TempDir(), "out.vsdx")
		res, err := Run(p, Options{Out: out, IDs: ids.Fixed(0, fixedClock)})
		if err != nil {
			t.Fatalf("Run: %v", err)
		}
		got, err := os.ReadFile(out)
		if err != nil {
			t.Fatalf("read out: %v", err)
		}
		if !bytes.Equal(got, template) {
			t.Errorf("out differs from Template bytes (%d vs %d)", len(got), len(template))
		}
		if res.Base != "" || res.Saved != out || res.NewBase != cli.Hash(got) {
			t.Errorf("result %+v, hash %s", res, cli.Hash(got))
		}
		if res.DryRun {
			t.Errorf("blank create reported dry run: %+v", res)
		}
	}
}

// TestRunDryRun checks that --dry-run writes nothing, with or without
// a plan, and reports DryRun with no Saved or NewBase.
func TestRunDryRun(t *testing.T) {
	parsed, errs := ops.ParsePlan([]byte(createPlanText(t, "c2-shapes.json")))
	if len(errs) != 0 {
		t.Fatalf("ParsePlan(c2): %v", errs)
	}
	for _, p := range []*ops.Plan{nil, parsed} {
		out := filepath.Join(t.TempDir(), "dry.vsdx")
		res, err := Run(p, Options{Out: out, DryRun: true, IDs: ids.Fixed(0, fixedClock)})
		if err != nil {
			t.Fatalf("Run dry: %v", err)
		}
		if !res.DryRun || res.Saved != "" || res.NewBase != "" || res.Base != "" {
			t.Errorf("dry result %+v", res)
		}
		if _, err := os.Stat(out); !errors.Is(err, os.ErrNotExist) {
			t.Errorf("dry run wrote %s", out)
		}
	}
	out := filepath.Join(t.TempDir(), "dry.vsdx")
	res, err := Run(parsed, Options{Out: out, DryRun: true, IDs: ids.Fixed(0, fixedClock)})
	if err != nil {
		t.Fatalf("Run dry c2: %v", err)
	}
	if res.Aliases["a"] != "page0/s1" || len(res.Changed) == 0 {
		t.Errorf("dry c2 aliases %v changed %d", res.Aliases, len(res.Changed))
	}
}

// TestRunWithPlan checks create c2 and c3: the plan runs on the blank
// and the result carries the OA1 and OP1 aliases and changed units.
func TestRunWithPlan(t *testing.T) {
	for _, c := range []struct {
		plan        string
		alias, want string
	}{
		{"c2-shapes.json", "a", "page0/s1"},
		{"c3-pages.json", "p2", "page1"},
	} {
		parsed, errs := ops.ParsePlan([]byte(createPlanText(t, c.plan)))
		if len(errs) != 0 {
			t.Fatalf("ParsePlan(%s): %v", c.plan, errs)
		}
		out := filepath.Join(t.TempDir(), "out.vsdx")
		res, err := Run(parsed, Options{Out: out, IDs: ids.Fixed(0, fixedClock)})
		if err != nil {
			t.Fatalf("Run(%s): %v", c.plan, err)
		}
		if res.Base != "" || res.Saved != out {
			t.Errorf("Run(%s): result %+v", c.plan, res)
		}
		if res.Aliases[c.alias] != c.want {
			t.Errorf("Run(%s): aliases %v, want %s=%s", c.plan, res.Aliases, c.alias, c.want)
		}
		data, err := os.ReadFile(out)
		if err != nil {
			t.Fatalf("read out: %v", err)
		}
		if res.NewBase != cli.Hash(data) {
			t.Errorf("Run(%s): new_base %s, want hash of out %s", c.plan, res.NewBase, cli.Hash(data))
		}
		if len(res.Changed) == 0 || len(res.Hazards) != 0 {
			t.Errorf("Run(%s): changed %d hazards %v", c.plan, len(res.Changed), res.Hazards)
		}
	}
}

// TestRunRefusals checks c4, c5 and c6 and the missing-output refusal:
// each fails with the CR1 row's code, names nothing written, and never
// writes the output.
func TestRunRefusals(t *testing.T) {
	parse := func(name string) *ops.Plan {
		t.Helper()
		parsed, errs := ops.ParsePlan([]byte(createPlanText(t, name)))
		if len(errs) != 0 {
			t.Fatalf("ParsePlan(%s): %v", name, errs)
		}
		return parsed
	}
	for _, c := range []struct {
		name       string
		code, path string
	}{
		{"c4-refused-master.json", cli.CodeNotFound, "ops[0].master"},
		{"c5-refused-glue.json", cli.CodeOperationFailed, "ops[1].to_glue"},
	} {
		out := filepath.Join(t.TempDir(), "out.vsdx")
		_, err := Run(parse(c.name), Options{Out: out, IDs: ids.Fixed(0, fixedClock)})
		var ce *cli.Error
		if !errors.As(err, &ce) || ce.Code != c.code || ce.Path != c.path {
			t.Errorf("Run(%s): error %v, want %s at %q", c.name, err, c.code, c.path)
		}
		if _, serr := os.Stat(out); !errors.Is(serr, os.ErrNotExist) {
			t.Errorf("Run(%s): refused create wrote output", c.name)
		}
	}
	out := filepath.Join(t.TempDir(), "out.vsdx")
	_, err := Run(parse("c6-refused-base.json"), Options{Out: out, IDs: ids.Fixed(0, fixedClock)})
	var ce *cli.Error
	if !errors.As(err, &ce) || ce.Code != cli.CodeInvalidPlan || ce.Path != "base" {
		t.Errorf("Run(c6): error %v, want invalid_plan at base", err)
	}
	if _, serr := os.Stat(out); !errors.Is(serr, os.ErrNotExist) {
		t.Error("Run(c6): refused create wrote output")
	}
	if _, err := Run(nil, Options{}); err == nil {
		t.Error("missing output accepted")
	} else if !errors.As(err, &ce) || ce.Code != cli.CodeUsage {
		t.Errorf("missing output: unexpected error %v", err)
	}
}

// TestRunOutputMode checks the CRF row: vsdx create -o x.vsdx writes
// x.vsdx with mode 0644, not CreateTemp's 0600, with no plan, an empty
// plan, and a plan that changes the drawing.
func TestRunOutputMode(t *testing.T) {
	parsed, errs := ops.ParsePlan([]byte(createPlanText(t, "c2-shapes.json")))
	if len(errs) != 0 {
		t.Fatalf("ParsePlan(c2): %v", errs)
	}
	for _, c := range []struct {
		name string
		plan *ops.Plan
	}{
		{"blank", nil},
		{"empty", &ops.Plan{}},
		{"plan", parsed},
	} {
		out := filepath.Join(t.TempDir(), "x.vsdx")
		if _, err := Run(c.plan, Options{Out: out, IDs: ids.Fixed(0, fixedClock)}); err != nil {
			t.Fatalf("%s create: %v", c.name, err)
		}
		info, err := os.Stat(out)
		if err != nil {
			t.Fatalf("%s stat out: %v", c.name, err)
		}
		if got := info.Mode().Perm(); got != 0o644 {
			t.Errorf("%s create output mode %o, want 644", c.name, got)
		}
	}
}

// TestRunDeterministic checks the CR1 determinism row: the same plan
// and clock twice give equal bytes.
func TestRunDeterministic(t *testing.T) {
	parsed, errs := ops.ParsePlan([]byte(createPlanText(t, "c2-shapes.json")))
	if len(errs) != 0 {
		t.Fatalf("ParsePlan(c2): %v", errs)
	}
	dir := t.TempDir()
	var first []byte
	for i, name := range []string{"a.vsdx", "b.vsdx"} {
		out := filepath.Join(dir, name)
		if _, err := Run(parsed, Options{Out: out, IDs: ids.Fixed(0, fixedClock)}); err != nil {
			t.Fatalf("run %d: %v", i, err)
		}
		data, err := os.ReadFile(out)
		if err != nil {
			t.Fatalf("read %d: %v", i, err)
		}
		if i == 0 {
			first = data
		} else if !bytes.Equal(first, data) {
			t.Errorf("second run differs (%d vs %d bytes)", len(first), len(data))
		}
	}
}
