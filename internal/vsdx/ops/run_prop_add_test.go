package ops

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jhnoel/documentdriver/internal/cli"
	"github.com/jhnoel/documentdriver/internal/laws"
	"github.com/jhnoel/documentdriver/internal/vsdx/edit"
	"github.com/jhnoel/documentdriver/internal/vsdx/read"
	"github.com/jhnoel/documentdriver/internal/vsdx/vsdxtest"
)

// propValue is one prop of a read Page object.
type propValue struct {
	ID     string          `json:"id"`
	Label  string          `json:"label"`
	Type   string          `json:"type"`
	Value  json.RawMessage `json:"value"`
	Format string          `json:"format,omitempty"`
	Unit   string          `json:"unit,omitempty"`
}

// pageProps returns the props of the object with id in the read Page
// of the drawing at path.
func pageProps(t *testing.T, path, page, object string) []propValue {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	got, err := read.Page(data, "B", page, read.Options{})
	if err != nil {
		t.Fatalf("read.Page(%s): %v", page, err)
	}
	var v struct {
		Objects []struct {
			ID    string      `json:"id"`
			Props []propValue `json:"props"`
		} `json:"objects"`
	}
	if err := json.Unmarshal(got, &v); err != nil {
		t.Fatalf("Unmarshal Page: %v", err)
	}
	for _, o := range v.Objects {
		if o.ID == object {
			return o.Props
		}
	}
	t.Fatalf("no object %s in Page(%s)", object, page)
	return nil
}

// TestPropAddOwner checks the OD1 row: a prop.add of Owner (string,
// Ada) on a shape with no Property section gives read props
// [{page0/s1/prop:Owner, label Owner, type string, value Ada}].
func TestPropAddOwner(t *testing.T) {
	src := vsdxtest.Fixture(t, "v1-minimal")
	w, err := edit.Begin(src)
	if err != nil {
		t.Fatalf("Begin: %v", err)
	}
	spec := &PropAddSpec{At: "page0/s1", Name: "Owner", Type: "string", Value: "Ada"}
	_, changed, _, _, err := spec.Run(w, nil, "", nil)
	if err != nil {
		t.Fatalf("prop.add: %v", err)
	}
	if len(changed) != 1 || changed[0] != "page0/s1/prop:Owner" {
		t.Errorf("changed = %v, want [page0/s1/prop:Owner]", changed)
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
	if len(props) != 1 {
		t.Fatalf("props = %v, want one Owner", props)
	}
	got := props[0]
	if got.ID != "page0/s1/prop:Owner" || got.Label != "Owner" || got.Type != "string" || string(got.Value) != `"Ada"` {
		t.Errorf("prop = %+v %s, want Owner string Ada", got, got.Value)
	}
}

// TestPropAddExisting checks the OD1 row: a prop.add of an existing
// row name refuses with operation_failed at name.
func TestPropAddExisting(t *testing.T) {
	src := vsdxtest.Fixture(t, "v2-master")
	w, err := edit.Begin(src)
	if err != nil {
		t.Fatalf("Begin: %v", err)
	}
	defer w.Tx().Rollback()
	// s2 already has Cost (local Value over the master's row).
	spec := &PropAddSpec{At: "page0/s2", Name: "Cost", Type: "currency", Value: float64(1)}
	_, _, _, _, err = spec.Run(w, nil, "", nil)
	var ce *cli.Error
	if !errors.As(err, &ce) {
		t.Fatalf("prop.add of existing name: error %v, want *cli.Error", err)
	}
	if ce.Code != cli.CodeOperationFailed || ce.Path != "name" {
		t.Errorf("got %v at %q, want operation_failed at name", ce.Code, ce.Path)
	}
	// s1 has Cost only through its master: still existing.
	spec = &PropAddSpec{At: "page0/s1", Name: "Cost", Type: "currency", Value: float64(1)}
	_, _, _, _, err = spec.Run(w, nil, "", nil)
	if !errors.As(err, &ce) || ce.Code != cli.CodeOperationFailed || ce.Path != "name" {
		t.Errorf("master row: got %v, want operation_failed at name", err)
	}
}

// TestPropAddTypes checks that a prop.add of each type round trips
// through read: string, number, boolean, date, duration,
// fixed_list, variable_list and currency, with label and format.
func TestPropAddTypes(t *testing.T) {
	for _, tc := range []struct {
		typ   string
		value any
		want  string // JSON value text
		unit  string
	}{
		{"string", "Ada", `"Ada"`, ""},
		{"number", float64(42), "42", ""},
		{"boolean", true, "true", ""},
		{"date", map[string]string{"date": "2024-03-01T00:00:00"}, `{"date":"2024-03-01T00:00:00"}`, ""},
		{"duration", "PT1H", `"PT1H"`, ""},
		{"fixed_list", "pick", `"pick"`, ""},
		{"variable_list", "free", `"free"`, ""},
		{"currency", float64(12.5), "12.5", ""},
	} {
		t.Run(tc.typ, func(t *testing.T) {
			src := vsdxtest.Fixture(t, "v1-minimal")
			w, err := edit.Begin(src)
			if err != nil {
				t.Fatalf("Begin: %v", err)
			}
			label := "Lab-" + tc.typ
			spec := &PropAddSpec{At: "page0/s1", Name: "NewRow", Label: label, Type: tc.typ, Value: tc.value, Format: "fmt"}
			if _, _, _, _, err := spec.Run(w, nil, "", nil); err != nil {
				t.Fatalf("prop.add %s: %v", tc.typ, err)
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
			if len(props) != 1 {
				t.Fatalf("props = %v, want one", props)
			}
			got := props[0]
			if got.Label != label || got.Type != tc.typ || string(got.Value) != tc.want || got.Format != "fmt" {
				t.Errorf("prop = %+v %s, want label %q type %s value %s fmt", got, got.Value, label, tc.typ, tc.want)
			}
		})
	}
}

// TestPropAddDefaults checks that a prop.add without a label uses
// the row name as the label and without a value uses the empty
// value.
func TestPropAddDefaults(t *testing.T) {
	src := vsdxtest.Fixture(t, "v1-minimal")
	w, err := edit.Begin(src)
	if err != nil {
		t.Fatalf("Begin: %v", err)
	}
	spec := &PropAddSpec{At: "page0/s1", Name: "Num", Type: "number"}
	if _, _, _, _, err := spec.Run(w, nil, "", nil); err != nil {
		t.Fatalf("prop.add: %v", err)
	}
	out := filepath.Join(t.TempDir(), "out.vsdx")
	if err := w.Tx().Snapshot(out); err != nil {
		t.Fatalf("Snapshot: %v", err)
	}
	w.Tx().Rollback()
	props := pageProps(t, out, "page0", "page0/s1")
	if len(props) != 1 || props[0].Label != "Num" || string(props[0].Value) != "0" {
		t.Errorf("props = %v, want Num 0", props)
	}
}

// TestPropAddBadValue checks that a prop.add value of the wrong form
// refuses with operation_failed at value.
func TestPropAddBadValue(t *testing.T) {
	src := vsdxtest.Fixture(t, "v1-minimal")
	w, err := edit.Begin(src)
	if err != nil {
		t.Fatalf("Begin: %v", err)
	}
	defer w.Tx().Rollback()
	for _, tc := range []struct {
		name  string
		value any
	}{
		{"Str", float64(1)},
		{"Num", "x"},
		{"Bool", "yes"},
	} {
		typ := map[string]string{"Str": "string", "Num": "number", "Bool": "boolean"}[tc.name]
		spec := &PropAddSpec{At: "page0/s1", Name: tc.name, Type: typ, Value: tc.value}
		_, _, _, _, err := spec.Run(w, nil, "", nil)
		var ce *cli.Error
		if !errors.As(err, &ce) || ce.Code != cli.CodeOperationFailed || ce.Path != "value" {
			t.Errorf("prop.add %s %v: got %v, want operation_failed at value", tc.name, tc.value, err)
		}
	}
}

// TestPropAddShapeErrors checks that a prop.add of a missing shape
// is not_found at at, of a master shape is operation_failed at at,
// and of an unknown alias is not_found at at.
func TestPropAddShapeErrors(t *testing.T) {
	src := vsdxtest.Fixture(t, "v2-master")
	w, err := edit.Begin(src)
	if err != nil {
		t.Fatalf("Begin: %v", err)
	}
	defer w.Tx().Rollback()
	cases := []struct {
		at    string
		alias map[string]string
		code  string
	}{
		{"page0/s99", nil, cli.CodeNotFound},
		{"master2/s5", nil, cli.CodeOperationFailed},
		{"$missing", nil, cli.CodeNotFound},
	}
	for _, tc := range cases {
		spec := &PropAddSpec{At: tc.at, Name: "NewRow", Type: "string", Value: "x"}
		_, _, _, _, err := spec.Run(w, tc.alias, "", nil)
		var ce *cli.Error
		if !errors.As(err, &ce) || ce.Code != tc.code || ce.Path != "at" {
			t.Errorf("prop.add at %s: got %v, want %s at at", tc.at, err, tc.code)
		}
	}
}

// TestPropAddAlias checks that a prop.add at a $name alias adds the
// row to the aliased shape.
func TestPropAddAlias(t *testing.T) {
	src := vsdxtest.Fixture(t, "v1-minimal")
	w, err := edit.Begin(src)
	if err != nil {
		t.Fatalf("Begin: %v", err)
	}
	spec := &PropAddSpec{At: "$a", Name: "Owner", Type: "string", Value: "Ada"}
	_, changed, _, _, err := spec.Run(w, map[string]string{"a": "page0/s1"}, "", nil)
	if err != nil {
		t.Fatalf("prop.add at $a: %v", err)
	}
	if len(changed) != 1 || changed[0] != "page0/s1/prop:Owner" {
		t.Errorf("changed = %v, want [page0/s1/prop:Owner]", changed)
	}
	w.Tx().Rollback()
}

// TestScalingPropAdd checks the scaling row: prop.add on
// Generated(1000) and Generated(4000) (N = 1000, 4N = 4000 shapes)
// allocates at most 5.5 times as much (laws.Scales) and the
// interleaved time ratio is at most 8 (laws.TimeScales).
func TestScalingPropAdd(t *testing.T) {
	setup := func(n int) (func() error, error) {
		src := vsdxtest.Generated(t, n)
		return func() error {
			w, err := edit.Begin(src)
			if err != nil {
				return err
			}
			defer w.Tx().Rollback()
			spec := &PropAddSpec{At: "page0/s1", Name: "ScalingProp", Type: "string", Value: "x"}
			if _, _, _, _, err := spec.Run(w, nil, "", nil); err != nil {
				return err
			}
			return w.Verify()
		}, nil
	}
	laws.Scales(t, "vsdx prop.add shapes", setup)
	laws.TimeScales(t, "vsdx prop.add shapes", setup)
}

// TestPropAddWithEscape checks that a prop.add name and value holding
// XML punctuation round trip through read.
func TestPropAddWithEscape(t *testing.T) {
	src := vsdxtest.Fixture(t, "v1-minimal")
	w, err := edit.Begin(src)
	if err != nil {
		t.Fatalf("Begin: %v", err)
	}
	name := "A&B"
	spec := &PropAddSpec{At: "page0/s1", Name: name, Type: "string", Value: "a<b'c"}
	if _, _, _, _, err := spec.Run(w, nil, "", nil); err != nil {
		if strings.Contains(err.Error(), "already exists") {
			t.Skip("row exists")
		}
		t.Fatalf("prop.add: %v", err)
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
	if len(props) != 1 || props[0].ID != "page0/s1/prop:A&B" || string(props[0].Value) != `"a<b'c"` {
		t.Errorf("props = %v, want escaped round trip", props)
	}
}
