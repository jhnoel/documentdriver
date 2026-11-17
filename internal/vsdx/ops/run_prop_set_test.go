package ops

import (
	"errors"
	"path/filepath"
	"testing"

	"github.com/jhnoel/documentdriver/internal/cli"
	"github.com/jhnoel/documentdriver/internal/laws"
	"github.com/jhnoel/documentdriver/internal/vsdx/edit"
	"github.com/jhnoel/documentdriver/internal/vsdx/vsdxtest"
)

// TestPropSetV2 checks the OD1 row: a prop.set of V2 s2 Cost to 20
// gives value 20 currency.
func TestPropSetV2(t *testing.T) {
	src := vsdxtest.Fixture(t, "v2-master")
	w, err := edit.Begin(src)
	if err != nil {
		t.Fatalf("Begin: %v", err)
	}
	spec := &PropSetSpec{At: "page0/s2/prop:Cost", Value: float64(20)}
	_, changed, _, _, err := spec.Run(w, nil, "", nil)
	if err != nil {
		t.Fatalf("prop.set: %v", err)
	}
	if len(changed) != 1 || changed[0] != "page0/s2/prop:Cost" {
		t.Errorf("changed = %v, want [page0/s2/prop:Cost]", changed)
	}
	if err := w.Verify(); err != nil {
		t.Fatalf("Verify: %v", err)
	}
	out := filepath.Join(t.TempDir(), "out.vsdx")
	if err := w.Tx().Snapshot(out); err != nil {
		t.Fatalf("Snapshot: %v", err)
	}
	w.Tx().Rollback()
	props := pageProps(t, out, "page0", "page0/s2")
	if len(props) != 1 || props[0].Type != "currency" || string(props[0].Value) != "20" {
		t.Errorf("props = %v, want Cost currency 20", props)
	}
}

// TestPropSetMasterOnly checks that a prop.set of a master-only row
// creates a local override: V2 s1 Cost (origin master) set to 99
// reads back 99.
func TestPropSetMasterOnly(t *testing.T) {
	src := vsdxtest.Fixture(t, "v2-master")
	w, err := edit.Begin(src)
	if err != nil {
		t.Fatalf("Begin: %v", err)
	}
	spec := &PropSetSpec{At: "page0/s1/prop:Cost", Value: float64(99)}
	if _, _, _, _, err := spec.Run(w, nil, "", nil); err != nil {
		t.Fatalf("prop.set: %v", err)
	}
	if err := w.Verify(); err != nil {
		t.Fatalf("Verify: %v", err)
	}
	out := filepath.Join(t.TempDir(), "out.vsdx")
	if err := w.Tx().Snapshot(out); err != nil {
		t.Fatalf("Snapshot: %v", err)
	}
	w.Tx().Rollback()
	props := pageProps(t, out, "page0", "page0/s1")
	if len(props) != 1 || string(props[0].Value) != "99" {
		t.Errorf("props = %v, want Cost 99", props)
	}
}

// TestPropSetDataLinked checks the OD1 row: a prop.set of a row
// with DataLinked 1 refuses with operation_failed.
func TestPropSetDataLinked(t *testing.T) {
	src := vsdxtest.Fixture(t, "v6-data")
	w, err := edit.Begin(src)
	if err != nil {
		t.Fatalf("Begin: %v", err)
	}
	defer w.Tx().Rollback()
	spec := &PropSetSpec{At: "page0/s1/prop:Linked", Value: "x"}
	_, _, _, _, err = spec.Run(w, nil, "", nil)
	var ce *cli.Error
	if !errors.As(err, &ce) {
		t.Fatalf("prop.set of data-linked row: error %v, want *cli.Error", err)
	}
	if ce.Code != cli.CodeOperationFailed {
		t.Errorf("got %v at %q, want operation_failed", ce.Code, ce.Path)
	}
}

// TestPropSetTypes checks that a prop.set of each type encodes by
// the row's type and round trips, keeping a number's unit.
func TestPropSetTypes(t *testing.T) {
	for _, tc := range []struct {
		fixture string
		at      string
		value   any
		want    string
		unit    string
	}{
		{"v6-data", "page0/s1/prop:Owner", "Grace", `"Grace"`, ""},
		{"v6-data", "page0/s1/prop:Cost", float64(3.5), "3.5", ""},
		{"v6-data", "page0/s1/prop:Done", false, "false", ""},
		{"v6-data", "page0/s1/prop:Due", map[string]string{"date": "2025-01-01T00:00:00"}, `{"date":"2025-01-01T00:00:00"}`, ""},
		{"v6-data", "page0/s1/prop:Size", float64(2), "2", "MM"},
	} {
		t.Run(tc.at, func(t *testing.T) {
			src := vsdxtest.Fixture(t, tc.fixture)
			w, err := edit.Begin(src)
			if err != nil {
				t.Fatalf("Begin: %v", err)
			}
			spec := &PropSetSpec{At: tc.at, Value: tc.value}
			if _, _, _, _, err := spec.Run(w, nil, "", nil); err != nil {
				t.Fatalf("prop.set %s: %v", tc.at, err)
			}
			if err := w.Verify(); err != nil {
				t.Fatalf("Verify: %v", err)
			}
			out := filepath.Join(t.TempDir(), "out.vsdx")
			if err := w.Tx().Snapshot(out); err != nil {
				t.Fatalf("Snapshot: %v", err)
			}
			w.Tx().Rollback()
			shape := "page0/s1"
			var got propValue
			for _, p := range pageProps(t, out, "page0", shape) {
				if p.ID == tc.at {
					got = p
				}
			}
			if got.ID == "" || string(got.Value) != tc.want || got.Unit != tc.unit {
				t.Errorf("prop %s = %+v %s, want value %s unit %q", tc.at, got, got.Value, tc.want, tc.unit)
			}
		})
	}
}

// TestPropSetErrors checks that a prop.set of a missing row is
// not_found at at, and of a value of the wrong form is
// operation_failed at value.
func TestPropSetErrors(t *testing.T) {
	src := vsdxtest.Fixture(t, "v2-master")
	w, err := edit.Begin(src)
	if err != nil {
		t.Fatalf("Begin: %v", err)
	}
	defer w.Tx().Rollback()
	spec := &PropSetSpec{At: "page0/s1/prop:Missing", Value: "x"}
	_, _, _, _, err = spec.Run(w, nil, "", nil)
	var ce *cli.Error
	if !errors.As(err, &ce) || ce.Code != cli.CodeNotFound || ce.Path != "at" {
		t.Errorf("missing row: got %v, want not_found at at", err)
	}
	spec = &PropSetSpec{At: "page0/s2/prop:Cost", Value: "x"}
	_, _, _, _, err = spec.Run(w, nil, "", nil)
	if !errors.As(err, &ce) || ce.Code != cli.CodeOperationFailed || ce.Path != "value" {
		t.Errorf("wrong value form: got %v, want operation_failed at value", err)
	}
}

// TestPropValueFormula checks the formula allow list: a Value with
// no formula, Inh, No Formula, THEMEVAL()/THEMEGUARD() or a
// constant is allowed; anything else is refused.
func TestPropValueFormula(t *testing.T) {
	for _, tc := range []struct {
		f    string
		hasF bool
		ok   bool
	}{
		{"", false, true},
		{"Inh", true, true},
		{"No Formula", true, true},
		{"THEMEVAL()", true, true},
		{"THEMEGUARD(1)", true, true},
		{"20", true, true},
		{"1 MM", true, true},
		{"Width*0.5", true, false},
		{"GUARD(1)", true, false},
		{"PAR(PNT(1,2))", true, false},
	} {
		err := propValueRefusal("Cost", tc.f, tc.hasF)
		if tc.ok && err != nil {
			t.Errorf("formula %q (hasF %v): %v, want allowed", tc.f, tc.hasF, err)
		}
		if !tc.ok && err == nil {
			t.Errorf("formula %q: allowed, want refused", tc.f)
		}
	}
}

// TestScalingPropSet checks the scaling row: prop.set on
// Generated(1000) and Generated(4000) (N = 1000, 4N = 4000 shapes)
// allocates at most 5.5 times as much (laws.Scales) and the
// interleaved time ratio is at most 8 (laws.TimeScales).
func TestScalingPropSet(t *testing.T) {
	setup := func(n int) (func() error, error) {
		src := vsdxtest.Generated(t, n)
		return func() error {
			w, err := edit.Begin(src)
			if err != nil {
				return err
			}
			defer w.Tx().Rollback()
			spec := &PropSetSpec{At: "page0/s1/prop:Cost", Value: float64(1)}
			if _, _, _, _, err := spec.Run(w, nil, "", nil); err != nil {
				return err
			}
			return w.Verify()
		}, nil
	}
	laws.Scales(t, "vsdx prop.set shapes", setup)
	laws.TimeScales(t, "vsdx prop.set shapes", setup)
}
