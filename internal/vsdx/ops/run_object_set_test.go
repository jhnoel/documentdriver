package ops_test

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jhnoel/documentdriver/internal/cli"
	"github.com/jhnoel/documentdriver/internal/laws"
	"github.com/jhnoel/documentdriver/internal/vsdx/apply"
	"github.com/jhnoel/documentdriver/internal/vsdx/edit"
	"github.com/jhnoel/documentdriver/internal/vsdx/ops"
	"github.com/jhnoel/documentdriver/internal/vsdx/vsdxtest"
)

// TestObjectSetBoxAndFill checks the OSR row: one op setting box and
// fill on V1 s1 gives both, reads back the box [72,90,144,72] and the
// fill {"hex":"#00FF00"}, and changes page0/s1 once.
func TestObjectSetBoxAndFill(t *testing.T) {
	path := vsdxtest.Fixture(t, "v1-minimal")
	out := filepath.Join(t.TempDir(), "out.vsdx")
	res, err := apply.Run(path, parsePlan(t, `{"ops":[{"op":"object.set","at":"page0/s1","box":[72,90,144,72],"fill":{"hex":"#00FF00"}}]}`), apply.Options{Out: out})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(res.Changed) != 1 {
		t.Fatalf("changed = %v, want one entry for page0/s1", res.Changed)
	}
	if string(res.Changed[0]) == "" || !strings.Contains(string(res.Changed[0]), "page0/s1") {
		t.Errorf("changed entry = %s, want page0/s1", res.Changed[0])
	}
	page := readPage(t, out)
	i := findObject(t, page, "page0/s1")
	checkBox(t, "s1", page.Objects[i].Box, [4]float64{72, 90, 144, 72})
	if string(page.Objects[i].Fill) != `{"hex":"#00FF00"}` {
		t.Errorf("fill = %s, want {\"hex\":\"#00FF00\"}", page.Objects[i].Fill)
	}
	checkNoNewFindings(t, path, out)
}

// TestObjectSetNameAndEnds checks that one op setting name (setStyle)
// and from/to (setEnds) on the V5 connector s3 gives both: the new
// name reads back and the ends move to the given points, freed.
func TestObjectSetNameAndEnds(t *testing.T) {
	path := vsdxtest.Fixture(t, "v5-glue")
	out := filepath.Join(t.TempDir(), "out.vsdx")
	res, err := apply.Run(path, parsePlan(t, `{"ops":[{"op":"object.set","at":"page0/s3","name":"Link","from":[216,144],"to":[360,144]}]}`), apply.Options{Out: out})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(res.Changed) != 1 {
		t.Fatalf("changed = %v, want one entry for page0/s3", res.Changed)
	}
	page := readPage(t, out)
	i := findObject(t, page, "page0/s3")
	if page.Objects[i].Name != "Link" {
		t.Errorf("name = %q, want Link", page.Objects[i].Name)
	}
	checkPt(t, "s3 from", page.Objects[i].From, [2]float64{216, 144})
	checkPt(t, "s3 to", page.Objects[i].To, [2]float64{360, 144})
	if page.Objects[i].FromGlue != "" || page.Objects[i].ToGlue != "" {
		t.Errorf("glue = %q %q, want both free", page.Objects[i].FromGlue, page.Objects[i].ToGlue)
	}
	checkNoNewFindings(t, path, out)
}

// TestObjectSetStale checks that a box and fill on V5 s1 merges the
// transform's stale report into apply's stale-values hazard naming
// page0/s3 (stale-connector), while both the box and the fill read
// back.
func TestObjectSetStale(t *testing.T) {
	path := vsdxtest.Fixture(t, "v5-glue")
	out := filepath.Join(t.TempDir(), "out.vsdx")
	res, err := apply.Run(path, parsePlan(t, `{"ops":[{"op":"object.set","at":"page0/s1","box":[72,90,144,72],"fill":{"hex":"#00FF00"}}]}`), apply.Options{Out: out})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	found := false
	for _, h := range res.Hazards {
		if h.Kind == "stale-values" && strings.Contains(h.Detail, "page0/s3") && strings.Contains(h.Detail, "stale-connector") {
			found = true
		}
	}
	if !found {
		t.Errorf("hazards %+v, want a stale-values detail naming page0/s3 stale-connector", res.Hazards)
	}
	page := readPage(t, out)
	i := findObject(t, page, "page0/s1")
	checkBox(t, "s1", page.Objects[i].Box, [4]float64{72, 90, 144, 72})
	if string(page.Objects[i].Fill) != `{"hex":"#00FF00"}` {
		t.Errorf("fill = %s, want {\"hex\":\"#00FF00\"}", page.Objects[i].Fill)
	}
}

// TestObjectSetIdentity checks that setting V1 s1 to the box and
// fill it already shows writes nothing.
func TestObjectSetIdentity(t *testing.T) {
	path := vsdxtest.Fixture(t, "v1-minimal")
	out := filepath.Join(t.TempDir(), "out.vsdx")
	res, err := apply.Run(path, parsePlan(t, `{"ops":[{"op":"object.set","at":"page0/s1","box":[108,126,72,36],"fill":{"hex":"#4472C4"}}]}`), apply.Options{Out: out})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if _, err := os.Stat(out); !errors.Is(err, os.ErrNotExist) {
		t.Error("identity object.set wrote its out file")
	}
	if len(res.Parts) != 0 || len(res.Changed) != 0 {
		t.Errorf("identity result parts %v changed %v, want empty", res.Parts, res.Changed)
	}
	if len(res.Hazards) != 0 {
		t.Errorf("identity hazards %+v, want none", res.Hazards)
	}
}

// TestObjectSetNoField checks the OSR row: an object.set with no
// field is refused by the rules (O0) as invalid_operation at ops[0].
func TestObjectSetNoField(t *testing.T) {
	_, errs := ops.ParsePlan([]byte(`{"ops":[{"op":"object.set","at":"page0/s1"}]}`))
	if len(errs) != 1 {
		t.Fatalf("ParsePlan errors %v, want one", errs)
	}
	if errs[0].Code != cli.CodeInvalidOperation || errs[0].Path != "ops[0]" {
		t.Errorf("error = %s at %q, want invalid_operation at ops[0]", errs[0].Code, errs[0].Path)
	}
}

// TestObjectSetAlias checks that an object.set through the $a alias
// an earlier object.add created sets the box and fill of the added
// shape.
func TestObjectSetAlias(t *testing.T) {
	path := vsdxtest.Fixture(t, "v1-minimal")
	out := filepath.Join(t.TempDir(), "out.vsdx")
	plan := `{"ops":[{"op":"object.add","at":"page0","geom":"rect","as":"a","box":[72,72,144,72]},` +
		`{"op":"object.set","at":"$a","box":[108,126,72,36],"fill":{"hex":"#00FF00"}}]}`
	res, err := apply.Run(path, parsePlan(t, plan), apply.Options{Out: out})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	addr := res.Aliases["a"]
	if addr == "" {
		t.Fatalf("aliases %v, want $a defined", res.Aliases)
	}
	page := readPage(t, out)
	i := findObject(t, page, addr)
	checkBox(t, "added", page.Objects[i].Box, [4]float64{108, 126, 72, 36})
	if string(page.Objects[i].Fill) != `{"hex":"#00FF00"}` {
		t.Errorf("fill = %s, want {\"hex\":\"#00FF00\"}", page.Objects[i].Fill)
	}
}

// TestObjectSetRefusals checks that the merged Run reports each
// field family's refusal: a box on a 1-D shape and a missing shape
// are refused at at, a from on a 2-D shape at at, and whole-shape
// glue at to_glue.
func TestObjectSetRefusals(t *testing.T) {
	for _, tc := range []struct {
		fixture string
		plan    string
		code    string
		path    string
	}{
		{"v5-glue", `{"ops":[{"op":"object.set","at":"page0/s3","box":[72,90,144,72],"fill":{"hex":"#00FF00"}}]}`, cli.CodeOperationFailed, "ops[0].at"},
		{"v1-minimal", `{"ops":[{"op":"object.set","at":"page0/s1","from":[0,0]}]}`, cli.CodeOperationFailed, "ops[0].at"},
		{"v1-minimal", `{"ops":[{"op":"object.set","at":"page0/s99","box":[72,90,144,72],"fill":{"hex":"#00FF00"}}]}`, cli.CodeNotFound, "ops[0].at"},
		{"v5-glue", `{"ops":[{"op":"object.set","at":"page0/s4","to_glue":"s2"}]}`, cli.CodeOperationFailed, "ops[0].to_glue"},
	} {
		path := vsdxtest.Fixture(t, tc.fixture)
		out := filepath.Join(t.TempDir(), "out.vsdx")
		_, err := apply.Run(path, parsePlan(t, tc.plan), apply.Options{Out: out})
		var ce *cli.Error
		if !errors.As(err, &ce) {
			t.Fatalf("%s: error %v, want *cli.Error", tc.plan, err)
		}
		if ce.Code != tc.code || ce.Path != tc.path {
			t.Errorf("%s: got %s at %q, want %s at %q", tc.plan, ce.Code, ce.Path, tc.code, tc.path)
		}
		if _, statErr := os.Stat(out); !errors.Is(statErr, os.ErrNotExist) {
			t.Errorf("%s: a refused object.set wrote its out file", tc.plan)
		}
	}
}

// TestScalingObjectSet checks the scaling row: object.set (box and
// fill) on Generated(1000) and Generated(4000) (N = 1000, 4N = 4000
// shapes) allocates at most 5.5 times as much (laws.Scales) and the
// interleaved time ratio is at most 8 (laws.TimeScales).
func TestScalingObjectSet(t *testing.T) {
	setup := func(n int) (func() error, error) {
		src := vsdxtest.Generated(t, n)
		return func() error {
			w, err := edit.Begin(src)
			if err != nil {
				return err
			}
			defer w.Tx().Rollback()
			rot := float64(0)
			spec := &ops.ObjectSetSpec{At: "page0/s1", Box: []float64{0, 693, 144, 54}, Rot: &rot, Fill: &ops.PaintSpec{Hex: "#00FF00"}}
			if _, _, _, _, err := spec.Run(w, map[string]string{}, "pt", nil); err != nil {
				return err
			}
			return w.Verify()
		}, nil
	}
	laws.Scales(t, "vsdx object.set shapes", setup)
	laws.TimeScales(t, "vsdx object.set shapes", setup)
}
