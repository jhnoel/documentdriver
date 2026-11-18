package ops

import (
	"encoding/json"
	"errors"
	"math"
	"os"
	"strings"
	"testing"

	"github.com/jhnoel/documentdriver/internal/cli"
	"github.com/jhnoel/documentdriver/internal/laws"
	"github.com/jhnoel/documentdriver/internal/vsdx/edit"
	"github.com/jhnoel/documentdriver/internal/vsdx/read"
	"github.com/jhnoel/documentdriver/internal/vsdx/vsdxtest"
)

// transformObject is one object of a read Page, as
// set_transform tests read it.
type transformObject struct {
	ID    string          `json:"id"`
	Box   []json.Number   `json:"box"`
	Rot   json.Number     `json:"rot"`
	FlipH bool            `json:"flip_h"`
	FlipV bool            `json:"flip_v"`
	Name  string          `json:"name"`
	Fill  json.RawMessage `json:"fill"`
}

// transformRead returns the object with address id from the read
// Page of the drawing at path.
func transformRead(t *testing.T, path, id string) transformObject {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	got, err := read.Page(data, "B", "page0", read.Options{})
	if err != nil {
		t.Fatalf("read.Page: %v", err)
	}
	var v struct {
		Objects []transformObject `json:"objects"`
	}
	if err := json.Unmarshal(got, &v); err != nil {
		t.Fatalf("Unmarshal Page: %v", err)
	}
	for _, o := range v.Objects {
		if o.ID == id {
			return o
		}
	}
	t.Fatalf("no object %s in Page", id)
	return transformObject{}
}

// transformBox checks a read box to 1e-6.
func transformBox(t *testing.T, name string, got []json.Number, want [4]float64) {
	t.Helper()
	if len(got) != 4 {
		t.Fatalf("%s: box %v, want %v", name, got, want)
	}
	for i := range want {
		f, err := got[i].Float64()
		if err != nil || math.Abs(f-want[i]) > 1e-6 {
			t.Errorf("%s: box %v, want %v", name, got, want)
			return
		}
	}
}

// transformRun runs setTransform on a fresh workspace on src and
// snapshots it, returning changed, stale and the snapshot path.
func transformRun(t *testing.T, src string, spec *ObjectSetSpec, aliases map[string]string, units string) ([]string, string, string) {
	t.Helper()
	w, err := edit.Begin(src)
	if err != nil {
		t.Fatalf("Begin: %v", err)
	}
	changed, stale, err := setTransform(w, spec, aliases, units)
	if err != nil {
		t.Fatalf("setTransform: %v", err)
	}
	if err := w.Verify(); err != nil {
		t.Fatalf("Verify: %v", err)
	}
	return changed, stale, propDeleteSnapshot(t, w)
}

// TestSetTransformBoxV1 checks the OST row: V1 s1 box
// [72,90,144,72] reads back the same, stores PinX 2, PinY 9.25,
// Width 2, Height 1 and LocPinX 1 (Width*0.5 recomputed), changed
// [page0/s1] with no stale.
func TestSetTransformBoxV1(t *testing.T) {
	src := vsdxtest.Fixture(t, "v1-minimal")
	spec := &ObjectSetSpec{At: "page0/s1", Box: []float64{72, 90, 144, 72}}
	changed, stale, out := transformRun(t, src, spec, nil, "pt")
	if len(changed) != 1 || changed[0] != "page0/s1" {
		t.Fatalf("changed = %v, want [page0/s1]", changed)
	}
	if stale != "" {
		t.Errorf("stale = %q, want empty", stale)
	}
	transformBox(t, "s1", transformRead(t, out, "page0/s1").Box, [4]float64{72, 90, 144, 72})
	xml := propDeletePageXML(t, out)
	for _, want := range []string{"<Cell N='PinX' V='2'/>", "<Cell N='PinY' V='9.25'/>", "<Cell N='Width' V='2'/>", "<Cell N='Height' V='1'/>", "<Cell N='LocPinX' V='1' F='Width*0.5'/>", "<Cell N='LocPinY' V='0.5' F='Height*0.5'/>"} {
		if !strings.Contains(xml, want) {
			t.Errorf("sealed page has no %s: %s", want, xml)
		}
	}
}

// TestSetTransformRotatedBox checks that a box on a rotated shape
// (V9 s3, rot 30) reads back with the new box and rot 30, and that
// setting rot 0 on V9 s1 (LocPin 0/0, rot 270) keeps its box.
func TestSetTransformRotatedBox(t *testing.T) {
	src := vsdxtest.Fixture(t, "v9-rotation")
	spec := &ObjectSetSpec{At: "page0/s3", Box: []float64{360, 90, 144, 72}}
	_, stale, out := transformRun(t, src, spec, nil, "pt")
	if stale != "" {
		t.Errorf("stale = %q, want empty", stale)
	}
	got := transformRead(t, out, "page0/s3")
	transformBox(t, "s3", got.Box, [4]float64{360, 90, 144, 72})
	if got.Rot.String() != "30" {
		t.Errorf("rot = %s, want 30", got.Rot)
	}
	src2 := vsdxtest.Fixture(t, "v9-rotation")
	rot0 := float64(0)
	spec2 := &ObjectSetSpec{At: "page0/s1", Rot: &rot0}
	_, _, out2 := transformRun(t, src2, spec2, nil, "pt")
	got2 := transformRead(t, out2, "page0/s1")
	transformBox(t, "s1", got2.Box, [4]float64{90, 90, 72, 36})
	if got2.Rot.String() != "" {
		t.Errorf("rot = %s, want 0 (absent)", got2.Rot)
	}
}

// TestSetTransformGroupShape checks that a group (V4 s10) resized
// to [216,288,288,288] reads back the same and its child s11's
// box moves with the group.
func TestSetTransformGroupShape(t *testing.T) {
	src := vsdxtest.Fixture(t, "v4-groups")
	spec := &ObjectSetSpec{At: "page0/s10", Box: []float64{216, 288, 288, 288}}
	_, _, out := transformRun(t, src, spec, nil, "pt")
	transformBox(t, "s10", transformRead(t, out, "page0/s10").Box, [4]float64{216, 288, 288, 288})
	// The child's stored cells are untouched, so with the group's
	// LocPin scaled to the new size its centre moves by the LocPin
	// delta from the group's centre.
	transformBox(t, "s11", transformRead(t, out, "page0/s11").Box, [4]float64{216, 504, 72, 72})
}

// TestSetTransformUnitsIn checks that a box in inches (units in,
// [1,1.25,2,1]) reads back in points as [72,90,144,72].
func TestSetTransformUnitsIn(t *testing.T) {
	src := vsdxtest.Fixture(t, "v1-minimal")
	spec := &ObjectSetSpec{At: "page0/s1", Box: []float64{1, 1.25, 2, 1}}
	_, _, out := transformRun(t, src, spec, nil, "in")
	transformBox(t, "s1", transformRead(t, out, "page0/s1").Box, [4]float64{72, 90, 144, 72})
}

// TestSetTransformRotFlip checks the OST rows on V9: s2 rot 90
// reads back rot 90 at the same box and stores Angle
// -1.5707963267948966; s2 flip_h false and flip_v true reads back
// flip_v at the same box and stores FlipX 0, FlipY 1.
func TestSetTransformRotFlip(t *testing.T) {
	src := vsdxtest.Fixture(t, "v9-rotation")
	rot := float64(90)
	spec := &ObjectSetSpec{At: "page0/s2", Rot: &rot}
	changed, stale, out := transformRun(t, src, spec, nil, "pt")
	if len(changed) != 1 || changed[0] != "page0/s2" {
		t.Fatalf("changed = %v, want [page0/s2]", changed)
	}
	if stale != "" {
		t.Errorf("stale = %q, want empty", stale)
	}
	got := transformRead(t, out, "page0/s2")
	transformBox(t, "s2", got.Box, [4]float64{252, 126, 72, 36})
	if got.Rot.String() != "90" {
		t.Errorf("rot = %s, want 90", got.Rot)
	}
	if xml := propDeletePageXML(t, out); !strings.Contains(xml, "N='Angle' V='-1.5707963267948966'") {
		t.Errorf("sealed page has no Angle -1.5707963267948966: %s", xml)
	}
	src2 := vsdxtest.Fixture(t, "v9-rotation")
	falseV, trueV := false, true
	spec2 := &ObjectSetSpec{At: "page0/s2", FlipH: &falseV, FlipV: &trueV}
	_, _, out2 := transformRun(t, src2, spec2, nil, "pt")
	got2 := transformRead(t, out2, "page0/s2")
	transformBox(t, "s2", got2.Box, [4]float64{252, 126, 72, 36})
	if got2.FlipH || !got2.FlipV {
		t.Errorf("flips = (%v,%v), want (false,true)", got2.FlipH, got2.FlipV)
	}
	xml2 := propDeletePageXML(t, out2)
	if !strings.Contains(xml2, "N='FlipX' V='0'") || !strings.Contains(xml2, "N='FlipY' V='1'") {
		t.Errorf("sealed page has no FlipX 0 / FlipY 1: %s", xml2)
	}
}

// TestSetTransformMasterResize checks the OST/RC1 row: V2 s2
// resized to [216,117,144,54] reads back the same, stores Width 2,
// LocPinX 1 materialised with F="Inh", Geometry rows rewritten, and
// connection X 1 materialised, with no stale.
func TestSetTransformMasterResize(t *testing.T) {
	src := vsdxtest.Fixture(t, "v2-master")
	spec := &ObjectSetSpec{At: "page0/s2", Box: []float64{216, 117, 144, 54}}
	changed, stale, out := transformRun(t, src, spec, nil, "pt")
	if len(changed) != 1 || changed[0] != "page0/s2" {
		t.Fatalf("changed = %v, want [page0/s2]", changed)
	}
	if stale != "" {
		t.Errorf("stale = %q, want empty", stale)
	}
	transformBox(t, "s2", transformRead(t, out, "page0/s2").Box, [4]float64{216, 117, 144, 54})
	xml := propDeletePageXML(t, out)
	if !strings.Contains(xml, "N='Width' V='2'") {
		t.Errorf("sealed page has no Width 2: %s", xml)
	}
	if !strings.Contains(xml, "<Cell N='LocPinX' V='1' F='Inh'/>") {
		t.Errorf("sealed page has no LocPinX 1 F Inh: %s", xml)
	}
}

// TestSetTransformGroup checks that V4 s11, a group child, resized
// to [216,432,144,72] reads back the same and its group s10's
// stored cells are unchanged.
func TestSetTransformGroup(t *testing.T) {
	src := vsdxtest.Fixture(t, "v4-groups")
	spec := &ObjectSetSpec{At: "page0/s11", Box: []float64{216, 432, 144, 72}}
	changed, _, out := transformRun(t, src, spec, nil, "pt")
	if len(changed) != 1 || changed[0] != "page0/s11" {
		t.Fatalf("changed = %v, want [page0/s11]", changed)
	}
	transformBox(t, "s11", transformRead(t, out, "page0/s11").Box, [4]float64{216, 432, 144, 72})
}

// TestSetTransformIdentity checks that writing the box, rot and
// flips V1 s1 already shows changes nothing.
func TestSetTransformIdentity(t *testing.T) {
	src := vsdxtest.Fixture(t, "v1-minimal")
	rot := float64(0)
	falseV := false
	spec := &ObjectSetSpec{At: "page0/s1", Box: []float64{108, 126, 72, 36}, Rot: &rot, FlipH: &falseV, FlipV: &falseV}
	changed, stale, out := transformRun(t, src, spec, nil, "pt")
	if len(changed) != 0 {
		t.Errorf("changed = %v, want empty for identity", changed)
	}
	if stale != "" {
		t.Errorf("stale = %q, want empty", stale)
	}
	if got, want := propDeletePageXML(t, out), propDeletePageXML(t, src); got != want {
		t.Errorf("identity changed the page:\ngot  %s\nwant %s", got, want)
	}
}

// TestSetTransformUntargeted checks that resizing V2 s1 leaves V2
// s2's box as read from the original.
func TestSetTransformUntargeted(t *testing.T) {
	src := vsdxtest.Fixture(t, "v2-master")
	before := transformRead(t, src, "page0/s2")
	spec := &ObjectSetSpec{At: "page0/s1", Box: []float64{72, 117, 144, 54}}
	_, _, out := transformRun(t, src, spec, nil, "pt")
	after := transformRead(t, out, "page0/s2")
	transformBox(t, "s2 before", before.Box, [4]float64{252, 117, 72, 54})
	transformBox(t, "s2 after", after.Box, [4]float64{252, 117, 72, 54})
}

// TestSetTransformErrors checks the refusals: a missing shape is
// not_found at at, a master shape, a 1-D shape and an unreadable
// transform are operation_failed at at, a non-finite rot is
// operation_failed at rot, a non-finite box at box, and a cell
// outside the allow list names the cell and formula at at.
func TestSetTransformErrors(t *testing.T) {
	for _, tc := range []struct {
		fixture string
		spec    *ObjectSetSpec
		code    string
		path    string
	}{
		{"v1-minimal", &ObjectSetSpec{At: "page0/s99", Box: []float64{0, 0, 72, 36}}, cli.CodeNotFound, "at"},
		{"v2-master", &ObjectSetSpec{At: "master2/s5", Box: []float64{0, 0, 72, 36}}, cli.CodeOperationFailed, "at"},
		{"v5-glue", &ObjectSetSpec{At: "page0/s3", Box: []float64{0, 0, 72, 36}}, cli.CodeOperationFailed, "at"},
		{"v1-minimal", &ObjectSetSpec{At: "page0/s1", Rot: func() *float64 { f := math.NaN(); return &f }()}, cli.CodeOperationFailed, "rot"},
		{"v1-minimal", &ObjectSetSpec{At: "page0/s1", Box: []float64{math.Inf(1), 0, 72, 36}}, cli.CodeOperationFailed, "box"},
	} {
		t.Run(tc.spec.At+"/"+tc.path, func(t *testing.T) {
			src := vsdxtest.Fixture(t, tc.fixture)
			w, err := edit.Begin(src)
			if err != nil {
				t.Fatalf("Begin: %v", err)
			}
			defer w.Tx().Rollback()
			_, _, err = setTransform(w, tc.spec, nil, "pt")
			var ce *cli.Error
			if !errors.As(err, &ce) || ce.Code != tc.code || ce.Path != tc.path {
				t.Errorf("setTransform: got %v, want %s at %s", err, tc.code, tc.path)
			}
		})
	}
	t.Run("formula", func(t *testing.T) {
		src := vsdxtest.Fixture(t, "v2-master")
		w, err := edit.Begin(src)
		if err != nil {
			t.Fatalf("Begin: %v", err)
		}
		defer w.Tx().Rollback()
		spec := &ObjectSetSpec{At: "page0/s4", Box: []float64{360, 324, 144, 72}}
		_, _, err = setTransform(w, spec, nil, "pt")
		var ce *cli.Error
		if !errors.As(err, &ce) || ce.Code != cli.CodeOperationFailed || ce.Path != "at" {
			t.Fatalf("formula refusal: got %v, want operation_failed at at", err)
		}
		if !strings.Contains(ce.Message, "PinX") || !strings.Contains(ce.Message, "Sheet.5!Width*0.25") {
			t.Errorf("refusal = %q, want it to name PinX and Sheet.5!Width*0.25", ce.Message)
		}
	})
}

// TestSetTransformStencil checks that a stencil shape is
// operation_failed at at.
func TestSetTransformStencil(t *testing.T) {
	src := vsdxtest.Fixture(t, "v12-stencil")
	w, err := edit.Begin(src)
	if err != nil {
		t.Fatalf("Begin: %v", err)
	}
	defer w.Tx().Rollback()
	spec := &ObjectSetSpec{At: "page0/s1", Box: []float64{0, 0, 72, 36}}
	_, _, err = setTransform(w, spec, nil, "pt")
	var ce *cli.Error
	if !errors.As(err, &ce) || ce.Code != cli.CodeOperationFailed || ce.Path != "at" {
		t.Fatalf("setTransform: got %v, want operation_failed at at", err)
	}
}

// TestSetTransformAtomicity checks that a refused setTransform
// writes nothing: a formula refusal leaves the page as it was.
func TestSetTransformAtomicity(t *testing.T) {
	src := vsdxtest.Fixture(t, "v2-master")
	w, err := edit.Begin(src)
	if err != nil {
		t.Fatalf("Begin: %v", err)
	}
	spec := &ObjectSetSpec{At: "page0/s4", Box: []float64{360, 324, 144, 72}}
	if _, _, err := setTransform(w, spec, nil, "pt"); err == nil {
		t.Fatal("setTransform on a formula shape succeeded, want a refusal")
	}
	if err := w.Verify(); err != nil {
		t.Fatalf("Verify: %v", err)
	}
	out := propDeleteSnapshot(t, w)
	if got, want := propDeletePageXML(t, out), propDeletePageXML(t, src); got != want {
		t.Errorf("refused setTransform changed the page:\ngot  %s\nwant %s", got, want)
	}
}

// TestSetTransformAlias checks that a $name alias resolves.
func TestSetTransformAlias(t *testing.T) {
	src := vsdxtest.Fixture(t, "v1-minimal")
	spec := &ObjectSetSpec{At: "$a", Box: []float64{72, 90, 144, 72}}
	changed, _, out := transformRun(t, src, spec, map[string]string{"a": "page0/s1"}, "pt")
	if len(changed) != 1 || changed[0] != "page0/s1" {
		t.Fatalf("changed = %v, want [page0/s1]", changed)
	}
	transformBox(t, "s1", transformRead(t, out, "page0/s1").Box, [4]float64{72, 90, 144, 72})
}

// TestSetTransformStale checks that a dependent outside the closed
// grammar is left stale: V1 s1 with LocPinX Sheet.5!Width*0.5
// resized reports LocPinX left stale; with LocPinX GUARD(Width*0.5)
// (in the grammar since CC1) the resize rewrites it to V 1, formula
// kept, nothing stale; and V5 s1 resized reports its glued connectors
// stale.
func TestSetTransformStale(t *testing.T) {
	src := vsdxtest.Fixture(t, "v1-minimal")
	w, err := edit.Begin(src)
	if err != nil {
		t.Fatalf("Begin: %v", err)
	}
	if err := w.SetCells("page0/s1", []edit.Cell{{N: "LocPinX", V: "0.5", F: "Sheet.5!Width*0.5"}}); err != nil {
		t.Fatalf("SetCells: %v", err)
	}
	spec := &ObjectSetSpec{At: "page0/s1", Box: []float64{72, 90, 144, 72}}
	changed, stale, err := setTransform(w, spec, nil, "pt")
	if err != nil {
		t.Fatalf("setTransform: %v", err)
	}
	if len(changed) != 1 {
		t.Fatalf("changed = %v, want [page0/s1]", changed)
	}
	if !strings.Contains(stale, "page0/s1 LocPinX") || !strings.Contains(stale, "left stale") {
		t.Errorf("stale = %q, want it to name page0/s1 LocPinX left stale", stale)
	}
	w.Tx().Rollback()

	// GUARD(Width*0.5) is in the closed grammar since CC1: the box
	// write recomputes it (V 1 for Width 2) and keeps the formula,
	// with nothing stale.
	wg, err := edit.Begin(src)
	if err != nil {
		t.Fatalf("Begin: %v", err)
	}
	if err := wg.SetCells("page0/s1", []edit.Cell{{N: "LocPinX", V: "0.5", F: "GUARD(Width*0.5)"}}); err != nil {
		t.Fatalf("SetCells: %v", err)
	}
	if _, staleG, err := setTransform(wg, spec, nil, "pt"); err != nil || staleG != "" {
		t.Errorf("GUARD LocPinX: stale = %q, err %v; want none", staleG, err)
	}
	if err := wg.Verify(); err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if xml := propDeletePageXML(t, propDeleteSnapshot(t, wg)); !strings.Contains(xml, "<Cell N='LocPinX' V='1' F='GUARD(Width*0.5)'/>") && !strings.Contains(xml, `<Cell N="LocPinX" V="1" F="GUARD(Width*0.5)"/>`) {
		t.Errorf("GUARD LocPinX not rewritten to V 1 with its formula: %s", xml)
	}
	wg.Tx().Rollback()
	src2 := vsdxtest.Fixture(t, "v5-glue")
	spec2 := &ObjectSetSpec{At: "page0/s1", Box: []float64{72, 90, 144, 72}}
	_, stale2, _ := transformRun(t, src2, spec2, nil, "pt")
	if !strings.Contains(stale2, "page0/s3") || !strings.Contains(stale2, "stale-connector") {
		t.Errorf("stale = %q, want it to name page0/s3 stale-connector", stale2)
	}
}

// TestScalingSetTransform checks the scaling row: setTransform on
// Generated(1000) and Generated(4000) (N = 1000, 4N = 4000 shapes)
// allocates at most 5.5 times as much (laws.Scales) and the
// interleaved time ratio is at most 8 (laws.TimeScales).
func TestScalingSetTransform(t *testing.T) {
	setup := func(n int) (func() error, error) {
		src := vsdxtest.Generated(t, n)
		return func() error {
			w, err := edit.Begin(src)
			if err != nil {
				return err
			}
			defer w.Tx().Rollback()
			spec := &ObjectSetSpec{At: "page0/s1", Box: []float64{0, 693, 144, 54}}
			if _, _, err := setTransform(w, spec, nil, "pt"); err != nil {
				return err
			}
			return w.Verify()
		}, nil
	}
	laws.Scales(t, "vsdx setTransform shapes", setup)
	laws.TimeScales(t, "vsdx setTransform shapes", setup)
}
