package ops

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jhnoel/documentdriver/internal/cli"
	"github.com/jhnoel/documentdriver/internal/laws"
	"github.com/jhnoel/documentdriver/internal/opc"
	"github.com/jhnoel/documentdriver/internal/vsdx/edit"
	"github.com/jhnoel/documentdriver/internal/vsdx/vsdxtest"
)

// propDeletePageXML returns the sealed page1.xml of the drawing at path.
func propDeletePageXML(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	pkg, err := opc.OpenBytes(data, opc.DefaultLimits())
	if err != nil {
		t.Fatalf("open %s: %v", path, err)
	}
	defer func() {
		if err := pkg.Close(); err != nil {
			t.Errorf("close %s: %v", path, err)
		}
	}()
	pt, err := pkg.Part("/visio/pages/page1.xml")
	if err != nil {
		t.Fatalf("Part(page1.xml): %v", err)
	}
	s, ok, err := pt.OriginalString()
	if err != nil {
		t.Fatalf("OriginalString(page1.xml): %v", err)
	}
	if !ok {
		t.Fatalf("page1.xml of %s holds no original bytes", path)
	}
	return s
}

// propDeleteSnapshot seals w to a file in a tempdir and rolls it back.
func propDeleteSnapshot(t *testing.T, w *edit.Workspace) string {
	t.Helper()
	out := filepath.Join(t.TempDir(), "out.vsdx")
	if err := w.Tx().Snapshot(out); err != nil {
		t.Fatalf("Snapshot: %v", err)
	}
	w.Tx().Rollback()
	return out
}

// propDeleteHas reports whether props of page0/s1 holds a prop id.
func propDeleteHas(props []propValue, id string) bool {
	for _, p := range props {
		if p.ID == id {
			return true
		}
	}
	return false
}

// TestPropDeleteLocalRow checks the PDL row: a prop.delete of V6 s1
// NewRow (stored only locally) is gone: it is the deleted address and
// read props without it.
func TestPropDeleteLocalRow(t *testing.T) {
	src := vsdxtest.Fixture(t, "v6-data")
	w, err := edit.Begin(src)
	if err != nil {
		t.Fatalf("Begin: %v", err)
	}
	spec := &PropDeleteSpec{At: "page0/s1/prop:NewRow"}
	as, changed, deleted, stale, err := spec.Run(w, nil, "", nil)
	if err != nil {
		t.Fatalf("prop.delete: %v", err)
	}
	if as != "" || stale != "" {
		t.Errorf("as = %q stale = %q, want empty", as, stale)
	}
	if len(changed) != 0 {
		t.Errorf("changed = %v, want empty", changed)
	}
	if len(deleted) != 1 || deleted[0] != "page0/s1/prop:NewRow" {
		t.Errorf("deleted = %v, want [page0/s1/prop:NewRow]", deleted)
	}
	if err := w.Verify(); err != nil {
		t.Fatalf("Verify: %v", err)
	}
	out := propDeleteSnapshot(t, w)
	props := pageProps(t, out, "page0", "page0/s1")
	if propDeleteHas(props, "page0/s1/prop:NewRow") {
		t.Errorf("props still has NewRow: %v", props)
	}
	if len(props) != 8 {
		t.Errorf("got %d props, want 8 with NewRow gone", len(props))
	}
	if got := propDeletePageXML(t, out); strings.Contains(got, "NewRow") {
		t.Errorf("sealed page still stores NewRow: %s", got)
	}
}

// TestPropDeleteMasterOnly checks the PDL row: a prop.delete of V6 s1
// MasterOnly (defined by the master only) writes a Del row and read
// props are without MasterOnly.
func TestPropDeleteMasterOnly(t *testing.T) {
	src := vsdxtest.Fixture(t, "v6-data")
	w, err := edit.Begin(src)
	if err != nil {
		t.Fatalf("Begin: %v", err)
	}
	spec := &PropDeleteSpec{At: "page0/s1/prop:MasterOnly"}
	_, changed, deleted, _, err := spec.Run(w, nil, "", nil)
	if err != nil {
		t.Fatalf("prop.delete: %v", err)
	}
	if len(changed) != 0 {
		t.Errorf("changed = %v, want empty", changed)
	}
	if len(deleted) != 1 || deleted[0] != "page0/s1/prop:MasterOnly" {
		t.Errorf("deleted = %v, want [page0/s1/prop:MasterOnly]", deleted)
	}
	if err := w.Verify(); err != nil {
		t.Fatalf("Verify: %v", err)
	}
	out := propDeleteSnapshot(t, w)
	props := pageProps(t, out, "page0", "page0/s1")
	if propDeleteHas(props, "page0/s1/prop:MasterOnly") {
		t.Errorf("props still has MasterOnly: %v", props)
	}
	if !propDeleteHas(props, "page0/s1/prop:Cost") {
		t.Errorf("props lost Cost: %v", props)
	}
	if got := propDeletePageXML(t, out); !strings.Contains(got, `<Row N='MasterOnly' Del='1'/>`) {
		t.Errorf("sealed page has no MasterOnly Del row: %s", got)
	}
}

// TestPropDeleteMasterOverride checks that a prop.delete of a row the
// master defines and the shape overrides (V6 s1 Cost) replaces the
// local row by a Del row with its cells removed and read props are
// without it.
func TestPropDeleteMasterOverride(t *testing.T) {
	src := vsdxtest.Fixture(t, "v6-data")
	w, err := edit.Begin(src)
	if err != nil {
		t.Fatalf("Begin: %v", err)
	}
	spec := &PropDeleteSpec{At: "page0/s1/prop:Cost"}
	if _, _, deleted, _, err := spec.Run(w, nil, "", nil); err != nil {
		t.Fatalf("prop.delete: %v", err)
	} else if len(deleted) != 1 || deleted[0] != "page0/s1/prop:Cost" {
		t.Errorf("deleted = %v, want [page0/s1/prop:Cost]", deleted)
	}
	if err := w.Verify(); err != nil {
		t.Fatalf("Verify: %v", err)
	}
	out := propDeleteSnapshot(t, w)
	props := pageProps(t, out, "page0", "page0/s1")
	if propDeleteHas(props, "page0/s1/prop:Cost") {
		t.Errorf("props still has Cost: %v", props)
	}
	if got := propDeletePageXML(t, out); !strings.Contains(got, `<Row N='Cost' Del='1'/>`) || strings.Contains(got, "<Row N='Cost'><Cell") {
		t.Errorf("sealed page has no clean Cost Del row: %s", got)
	}
}

// TestPropDeleteDataLinked checks the PDL row: a prop.delete of V6 s1
// Linked (DataLinked 1 on the master) refuses with operation_failed.
func TestPropDeleteDataLinked(t *testing.T) {
	src := vsdxtest.Fixture(t, "v6-data")
	w, err := edit.Begin(src)
	if err != nil {
		t.Fatalf("Begin: %v", err)
	}
	defer w.Tx().Rollback()
	spec := &PropDeleteSpec{At: "page0/s1/prop:Linked"}
	_, _, _, _, err = spec.Run(w, nil, "", nil)
	var ce *cli.Error
	if !errors.As(err, &ce) {
		t.Fatalf("prop.delete of data-linked row: error %v, want *cli.Error", err)
	}
	if ce.Code != cli.CodeOperationFailed {
		t.Errorf("got %v at %q, want operation_failed", ce.Code, ce.Path)
	}
}

// TestPropDeleteTwice checks the PDL row: deleting NewRow and deleting
// MasterOnly a second time each is not_found.
func TestPropDeleteTwice(t *testing.T) {
	for _, at := range []string{"page0/s1/prop:NewRow", "page0/s1/prop:MasterOnly"} {
		t.Run(at, func(t *testing.T) {
			src := vsdxtest.Fixture(t, "v6-data")
			w, err := edit.Begin(src)
			if err != nil {
				t.Fatalf("Begin: %v", err)
			}
			defer w.Tx().Rollback()
			spec := &PropDeleteSpec{At: at}
			if _, _, _, _, err := spec.Run(w, nil, "", nil); err != nil {
				t.Fatalf("first prop.delete: %v", err)
			}
			_, _, _, _, err = spec.Run(w, nil, "", nil)
			var ce *cli.Error
			if !errors.As(err, &ce) || ce.Code != cli.CodeNotFound || ce.Path != "at" {
				t.Errorf("second prop.delete: got %v, want not_found at at", err)
			}
		})
	}
}

// TestPropDeleteErrors checks that a prop.delete of a missing row or a
// missing shape is not_found at at, and of a master shape is
// operation_failed at at.
func TestPropDeleteErrors(t *testing.T) {
	src := vsdxtest.Fixture(t, "v6-data")
	w, err := edit.Begin(src)
	if err != nil {
		t.Fatalf("Begin: %v", err)
	}
	defer w.Tx().Rollback()
	for _, tc := range []struct {
		at   string
		code string
	}{
		{"page0/s1/prop:Missing", cli.CodeNotFound},
		{"page0/s99/prop:Cost", cli.CodeNotFound},
		{"master2/s5/prop:Cost", cli.CodeOperationFailed},
	} {
		spec := &PropDeleteSpec{At: tc.at}
		_, _, _, _, err := spec.Run(w, nil, "", nil)
		var ce *cli.Error
		if !errors.As(err, &ce) || ce.Code != tc.code || ce.Path != "at" {
			t.Errorf("prop.delete at %s: got %v, want %s at at", tc.at, err, tc.code)
		}
	}
	spec := &PropDeleteSpec{At: "$missing/prop:Cost"}
	_, _, _, _, err = spec.Run(w, nil, "", nil)
	var ce *cli.Error
	if !errors.As(err, &ce) || ce.Code != cli.CodeNotFound || ce.Path != "at" {
		t.Errorf("prop.delete at $missing: got %v, want not_found at at", err)
	}
}

// TestPropDeleteAlias checks that a prop.delete at a $name alias (with
// a /prop suffix) deletes the aliased shape's row.
func TestPropDeleteAlias(t *testing.T) {
	src := vsdxtest.Fixture(t, "v6-data")
	w, err := edit.Begin(src)
	if err != nil {
		t.Fatalf("Begin: %v", err)
	}
	spec := &PropDeleteSpec{At: "$a/prop:NewRow"}
	_, _, deleted, _, err := spec.Run(w, map[string]string{"a": "page0/s1"}, "", nil)
	if err != nil {
		t.Fatalf("prop.delete at $a: %v", err)
	}
	if len(deleted) != 1 || deleted[0] != "page0/s1/prop:NewRow" {
		t.Errorf("deleted = %v, want [page0/s1/prop:NewRow]", deleted)
	}
	w.Tx().Rollback()
}

// TestPropDeleteMasterWithoutSection checks that a prop.delete of a
// master-only row on a shape with no Property section (V2 s1 Cost)
// creates the section holding the Del row and read props are empty.
func TestPropDeleteMasterWithoutSection(t *testing.T) {
	src := vsdxtest.Fixture(t, "v2-master")
	w, err := edit.Begin(src)
	if err != nil {
		t.Fatalf("Begin: %v", err)
	}
	spec := &PropDeleteSpec{At: "page0/s1/prop:Cost"}
	if _, _, deleted, _, err := spec.Run(w, nil, "", nil); err != nil {
		t.Fatalf("prop.delete: %v", err)
	} else if len(deleted) != 1 || deleted[0] != "page0/s1/prop:Cost" {
		t.Errorf("deleted = %v, want [page0/s1/prop:Cost]", deleted)
	}
	if err := w.Verify(); err != nil {
		t.Fatalf("Verify: %v", err)
	}
	out := propDeleteSnapshot(t, w)
	if props := pageProps(t, out, "page0", "page0/s1"); len(props) != 0 {
		t.Errorf("props = %v, want empty", props)
	}
	if got := propDeletePageXML(t, out); !strings.Contains(got, `<Row N='Cost' Del='1'/>`) {
		t.Errorf("sealed page has no Cost Del row: %s", got)
	}
}

// TestScalingPropDelete checks the scaling row: prop.delete on
// Generated(1000) and Generated(4000) (N = 1000, 4N = 4000 shapes)
// allocates at most 5.5 times as much (laws.Scales) and the interleaved
// time ratio is at most 8 (laws.TimeScales).
func TestScalingPropDelete(t *testing.T) {
	setup := func(n int) (func() error, error) {
		src := vsdxtest.Generated(t, n)
		return func() error {
			w, err := edit.Begin(src)
			if err != nil {
				return err
			}
			defer w.Tx().Rollback()
			spec := &PropDeleteSpec{At: "page0/s1/prop:Cost"}
			if _, _, _, _, err := spec.Run(w, nil, "", nil); err != nil {
				return err
			}
			return w.Verify()
		}, nil
	}
	laws.Scales(t, "vsdx prop.delete shapes", setup)
	laws.TimeScales(t, "vsdx prop.delete shapes", setup)
}
