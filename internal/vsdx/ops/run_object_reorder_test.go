package ops_test

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/jhnoel/documentdriver/internal/cli"
	"github.com/jhnoel/documentdriver/internal/laws"
	"github.com/jhnoel/documentdriver/internal/vsdx/apply"
	"github.com/jhnoel/documentdriver/internal/vsdx/edit"
	"github.com/jhnoel/documentdriver/internal/vsdx/ops"
	"github.com/jhnoel/documentdriver/internal/vsdx/vsdxtest"
)

// reorderIDs returns the object ids of the page read from path, in
// paint order.
func reorderIDs(t *testing.T, path string) []string {
	t.Helper()
	page := readPage(t, path)
	ids := make([]string, len(page.Objects))
	for i, o := range page.Objects {
		ids[i] = o.ID
	}
	return ids
}

// checkReorderOrder applies text to fixture and checks the paint
// order of page0 in the output.
func checkReorderOrder(t *testing.T, fixture, text string, want []string) {
	t.Helper()
	src := vsdxtest.Fixture(t, fixture)
	out, _ := runPlan(t, src, text)
	if got := reorderIDs(t, out); !reflect.DeepEqual(got, want) {
		t.Errorf("paint order = %v, want %v", got, want)
	}
}

// TestObjectReorderV5Front checks the ORO row: V5 s1 to front gives
// paint order s2, s3, s4, s5, s6, s1.
func TestObjectReorderV5Front(t *testing.T) {
	checkReorderOrder(t, "v5-glue",
		`{"ops":[{"op":"object.reorder","at":"page0/s1","to":"front"}]}`,
		[]string{"page0/s2", "page0/s3", "page0/s4", "page0/s5", "page0/s6", "page0/s1"})
}

// TestObjectReorderV5Back checks the ORO row: V5 s6 to back puts s6
// first.
func TestObjectReorderV5Back(t *testing.T) {
	checkReorderOrder(t, "v5-glue",
		`{"ops":[{"op":"object.reorder","at":"page0/s6","to":"back"}]}`,
		[]string{"page0/s6", "page0/s1", "page0/s2", "page0/s3", "page0/s4", "page0/s5"})
}

// TestObjectReorderV5Before checks the ORO row: V5 s3 before s1 gives
// s3, s1, s2, and the rest unchanged.
func TestObjectReorderV5Before(t *testing.T) {
	checkReorderOrder(t, "v5-glue",
		`{"ops":[{"op":"object.reorder","at":"page0/s3","to":"page0/s1","position":"before"}]}`,
		[]string{"page0/s3", "page0/s1", "page0/s2", "page0/s4", "page0/s5", "page0/s6"})
}

// TestObjectReorderV5After checks the after row by the same shape: V5
// s1 after s3 gives s2, s3, s1, s4, s5, s6.
func TestObjectReorderV5After(t *testing.T) {
	checkReorderOrder(t, "v5-glue",
		`{"ops":[{"op":"object.reorder","at":"page0/s1","to":"page0/s3","position":"after"}]}`,
		[]string{"page0/s2", "page0/s3", "page0/s1", "page0/s4", "page0/s5", "page0/s6"})
}

// TestObjectReorderDifferentParent checks the ORO row: V4 s11
// relative to s20 (a different parent) refuses with operation_failed
// at ops[0].to and leaves the file unchanged.
func TestObjectReorderDifferentParent(t *testing.T) {
	src := vsdxtest.Fixture(t, "v4-groups")
	before, err := os.ReadFile(src)
	if err != nil {
		t.Fatalf("read before: %v", err)
	}
	for _, text := range []string{
		`{"ops":[{"op":"object.reorder","at":"page0/s11","to":"page0/s20","position":"before"}]}`,
		`{"ops":[{"op":"object.reorder","at":"page0/s11","to":"page0/s20","position":"after"}]}`,
	} {
		p := parsePlan(t, text)
		out := filepath.Join(t.TempDir(), "out.vsdx")
		_, err = apply.Run(src, p, apply.Options{Out: out})
		var ce *cli.Error
		if !errors.As(err, &ce) || ce.Code != cli.CodeOperationFailed || ce.Path != "ops[0].to" {
			t.Errorf("apply.Run(%s) error = %v, want operation_failed at ops[0].to", text, err)
		}
		if _, err := os.Stat(out); !errors.Is(err, os.ErrNotExist) {
			t.Errorf("apply.Run(%s) wrote its out file", text)
		}
	}
	after, err := os.ReadFile(src)
	if err != nil {
		t.Fatalf("read after: %v", err)
	}
	if string(before) != string(after) {
		t.Error("a refused plan changed the input")
	}
}

// TestObjectReorderInGroup checks that a reorder inside a group stays
// in the group: V4 s11 to front (within s10) gives the flat paint
// order s10, s12, s13, s11, s20, s21, and s11 remains in s10.
func TestObjectReorderInGroup(t *testing.T) {
	checkReorderOrder(t, "v4-groups",
		`{"ops":[{"op":"object.reorder","at":"page0/s11","to":"front"}]}`,
		[]string{"page0/s10", "page0/s12", "page0/s13", "page0/s11", "page0/s20", "page0/s21"})
}

// TestObjectReorderSequences checks two reorders in one plan: V5 s1
// to front, then s2 to front, gives s3, s4, s5, s6, s1, s2.
func TestObjectReorderSequences(t *testing.T) {
	checkReorderOrder(t, "v5-glue",
		`{"ops":[{"op":"object.reorder","at":"page0/s1","to":"front"},{"op":"object.reorder","at":"page0/s2","to":"front"}]}`,
		[]string{"page0/s3", "page0/s4", "page0/s5", "page0/s6", "page0/s1", "page0/s2"})
}

// TestObjectReorderNoop checks that a move to where the shape already
// stands changes nothing: V5 s1 to back keeps the order and reports
// no changed object.
func TestObjectReorderNoop(t *testing.T) {
	w := mustBegin(t, vsdxtest.Fixture(t, "v5-glue"))
	spec := &ops.ObjectReorderSpec{At: "page0/s1", To: "back"}
	as, changed, _, _, err := spec.Run(w, nil, "", nil)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if as != "" || len(changed) != 0 {
		t.Errorf("Run = (%q, %v), want no changed object", as, changed)
	}
	if err := w.Verify(); err != nil {
		t.Fatalf("Verify: %v", err)
	}
}

// TestObjectReorderAlias checks that at and to resolve $aliases.
func TestObjectReorderAlias(t *testing.T) {
	w := mustBegin(t, vsdxtest.Fixture(t, "v5-glue"))
	aliases := map[string]string{"a": "page0/s1", "b": "page0/s6"}
	spec := &ops.ObjectReorderSpec{At: "$a", To: "$b", Position: "before"}
	_, changed, _, _, err := spec.Run(w, aliases, "", nil)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(changed) != 1 || changed[0] != "page0/s1" {
		t.Errorf("changed = %v, want [page0/s1]", changed)
	}
	if err := w.Verify(); err != nil {
		t.Fatalf("Verify: %v", err)
	}
}

// TestObjectReorderErrors checks the refusals: a missing at is
// not_found at at, a missing to is not_found at to, and a shape
// relative to itself is operation_failed at to.
func TestObjectReorderErrors(t *testing.T) {
	cases := []struct {
		spec  *ops.ObjectReorderSpec
		code  string
		field string
	}{
		{&ops.ObjectReorderSpec{At: "page0/s99", To: "front"}, cli.CodeNotFound, "at"},
		{&ops.ObjectReorderSpec{At: "page0/s1", To: "page0/s99", Position: "before"}, cli.CodeNotFound, "to"},
		{&ops.ObjectReorderSpec{At: "page0/s1", To: "page0/s1", Position: "before"}, cli.CodeOperationFailed, "to"},
		{&ops.ObjectReorderSpec{At: "page0/s11", To: "page0/s20", Position: "after"}, cli.CodeOperationFailed, "to"},
	}
	for _, tc := range cases {
		w := mustBegin(t, vsdxtest.Fixture(t, "v5-glue"))
		if tc.spec.At == "page0/s11" {
			w.Tx().Rollback()
			w = mustBegin(t, vsdxtest.Fixture(t, "v4-groups"))
		}
		_, _, _, _, err := tc.spec.Run(w, nil, "", nil)
		var ce *cli.Error
		if !errors.As(err, &ce) || ce.Code != tc.code || ce.Path != tc.field {
			t.Errorf("Run(%+v) error = %v, want %s at %s", *tc.spec, err, tc.code, tc.field)
		}
	}
}

// TestScalingObjectReorder checks the scaling row: object.reorder of
// s1 to front on Generated(1000) and Generated(4000) (N = 1000,
// 4N = 4000 shapes) allocates at most 5.5 times as much (laws.Scales)
// and the interleaved time ratio is at most 8 (laws.TimeScales).
func TestScalingObjectReorder(t *testing.T) {
	setup := func(n int) (func() error, error) {
		src := vsdxtest.Generated(t, n)
		return func() error {
			w, err := edit.Begin(src)
			if err != nil {
				return err
			}
			defer w.Tx().Rollback()
			spec := &ops.ObjectReorderSpec{At: "page0/s1", To: "front"}
			if _, _, _, _, err := spec.Run(w, nil, "", nil); err != nil {
				return err
			}
			return w.Verify()
		}, nil
	}
	laws.Scales(t, "vsdx object.reorder shapes", setup)
	laws.TimeScales(t, "vsdx object.reorder shapes", setup)
}
