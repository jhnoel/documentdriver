package ops

import (
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/jhnoel/documentdriver/internal/cli"
	"github.com/jhnoel/documentdriver/internal/laws"
	"github.com/jhnoel/documentdriver/internal/vsdx/edit"
	"github.com/jhnoel/documentdriver/internal/vsdx/read"
	"github.com/jhnoel/documentdriver/internal/vsdx/vsdxtest"
)

// styleObject is one object of a read page, for set_style tests.
type styleObject struct {
	ID      string            `json:"id"`
	Name    string            `json:"name"`
	Fill    json.RawMessage   `json:"fill"`
	Line    json.RawMessage   `json:"line"`
	Origins map[string]string `json:"origins"`
}

// styleRead returns the object with address id from the read Page of
// the drawing at path, at full detail so origins show.
func styleRead(t *testing.T, path, id string) styleObject {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	got, err := read.Page(data, "B", "page0", read.Options{Detail: "full"})
	if err != nil {
		t.Fatalf("read.Page: %v", err)
	}
	var v struct {
		Objects []styleObject `json:"objects"`
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
	return styleObject{}
}

// styleRun runs setStyle on a fresh workspace on src and snapshots it
// to a file, returning the changed addresses and the snapshot path.
func styleRun(t *testing.T, src string, spec *ObjectSetSpec, aliases map[string]string, units string) ([]string, string) {
	t.Helper()
	w, err := edit.Begin(src)
	if err != nil {
		t.Fatalf("Begin: %v", err)
	}
	changed, stale, err := setStyle(w, spec, aliases, units)
	if err != nil {
		t.Fatalf("setStyle: %v", err)
	}
	if stale != "" {
		t.Errorf("stale = %q, want empty", stale)
	}
	if err := w.Verify(); err != nil {
		t.Fatalf("Verify: %v", err)
	}
	return changed, propDeleteSnapshot(t, w)
}

// TestSetStyleFillHex checks the OSS row: V1 s1 fill #00FF00 reads
// back #00FF00 with origin local, changed [page0/s1].
func TestSetStyleFillHex(t *testing.T) {
	src := vsdxtest.Fixture(t, "v1-minimal")
	spec := &ObjectSetSpec{At: "page0/s1", Fill: &PaintSpec{Hex: "#00FF00"}}
	changed, out := styleRun(t, src, spec, nil, "pt")
	if len(changed) != 1 || changed[0] != "page0/s1" {
		t.Fatalf("changed = %v, want [page0/s1]", changed)
	}
	got := styleRead(t, out, "page0/s1")
	if string(got.Fill) != `{"hex":"#00FF00"}` {
		t.Errorf("fill = %s, want {\"hex\":\"#00FF00\"}", got.Fill)
	}
	if got.Origins["fill"] != "local" {
		t.Errorf("fill origin = %q, want local", got.Origins["fill"])
	}
}

// TestSetStyleFillNone checks the OSS row: V1 s1 fill none reads back
// fill none.
func TestSetStyleFillNone(t *testing.T) {
	src := vsdxtest.Fixture(t, "v1-minimal")
	spec := &ObjectSetSpec{At: "page0/s1", Fill: &PaintSpec{None: true}}
	changed, out := styleRun(t, src, spec, nil, "pt")
	if len(changed) != 1 || changed[0] != "page0/s1" {
		t.Fatalf("changed = %v, want [page0/s1]", changed)
	}
	got := styleRead(t, out, "page0/s1")
	if string(got.Fill) != `"none"` {
		t.Errorf("fill = %s, want \"none\"", got.Fill)
	}
}

// TestSetStyleLineWeight checks the OSS row: V1 s1 line w 2 stores
// LineWeight 0.027777777777777776 inches and reads back w 2.
func TestSetStyleLineWeight(t *testing.T) {
	src := vsdxtest.Fixture(t, "v1-minimal")
	w2 := float64(2)
	spec := &ObjectSetSpec{At: "page0/s1", Line: &LineSpec{W: &w2}}
	changed, out := styleRun(t, src, spec, nil, "pt")
	if len(changed) != 1 || changed[0] != "page0/s1" {
		t.Fatalf("changed = %v, want [page0/s1]", changed)
	}
	got := styleRead(t, out, "page0/s1")
	var line struct {
		W json.Number `json:"w"`
	}
	if err := json.Unmarshal(got.Line, &line); err != nil {
		t.Fatalf("Unmarshal line: %v", err)
	}
	if line.W.String() != "2" {
		t.Errorf("line w = %s, want 2 (line %s)", line.W, got.Line)
	}
	if xml := propDeletePageXML(t, out); !strings.Contains(xml, "LineWeight") || !strings.Contains(xml, "0.027777777777777776") {
		t.Errorf("sealed page has no LineWeight 0.027777777777777776: %s", xml)
	}
}

// TestSetStyleLineColorDash checks that a line colour and dash read
// back, and a line none reads back as none.
func TestSetStyleLineColorDash(t *testing.T) {
	src := vsdxtest.Fixture(t, "v1-minimal")
	spec := &ObjectSetSpec{At: "page0/s1", Line: &LineSpec{Color: "#00FF00", Dash: "2"}}
	_, out := styleRun(t, src, spec, nil, "pt")
	got := styleRead(t, out, "page0/s1")
	var line struct {
		Color struct {
			Hex string `json:"hex"`
		} `json:"color"`
		Dash string `json:"dash"`
	}
	if err := json.Unmarshal(got.Line, &line); err != nil {
		t.Fatalf("Unmarshal line: %v", err)
	}
	if line.Color.Hex != "#00FF00" || line.Dash != "2" {
		t.Errorf("line = %s, want color #00FF00 dash 2", got.Line)
	}
	src2 := vsdxtest.Fixture(t, "v1-minimal")
	spec2 := &ObjectSetSpec{At: "page0/s1", Line: &LineSpec{None: true}}
	_, out2 := styleRun(t, src2, spec2, nil, "pt")
	if got := styleRead(t, out2, "page0/s1"); string(got.Line) != `"none"` {
		t.Errorf("line = %s, want \"none\"", got.Line)
	}
}

// TestSetStyleLineOnNone checks that a weight on a none line (V3 s4,
// line none by style) reads back: the pattern is made solid.
func TestSetStyleLineOnNone(t *testing.T) {
	src := vsdxtest.Fixture(t, "v3-styles")
	w2 := float64(2)
	spec := &ObjectSetSpec{At: "page0/s4", Line: &LineSpec{W: &w2}}
	_, out := styleRun(t, src, spec, nil, "pt")
	got := styleRead(t, out, "page0/s4")
	var line struct {
		W json.Number `json:"w"`
	}
	if err := json.Unmarshal(got.Line, &line); err != nil || line.W.String() != "2" {
		t.Errorf("line = %s, want w 2", got.Line)
	}
}

// TestSetStyleLineUnitsIn checks that a line weight in inches (units
// in, w 1) stores LineWeight 1 inch and reads back w 72 points.
func TestSetStyleLineUnitsIn(t *testing.T) {
	src := vsdxtest.Fixture(t, "v1-minimal")
	w1 := float64(1)
	spec := &ObjectSetSpec{At: "page0/s1", Line: &LineSpec{W: &w1}}
	_, out := styleRun(t, src, spec, map[string]string{}, "in")
	got := styleRead(t, out, "page0/s1")
	var line struct {
		W json.Number `json:"w"`
	}
	if err := json.Unmarshal(got.Line, &line); err != nil || line.W.String() != "72" {
		t.Errorf("line = %s, want w 72", got.Line)
	}
	if xml := propDeletePageXML(t, out); !strings.Contains(xml, "LineWeight") || !strings.Contains(xml, `V="1"`) && !strings.Contains(xml, "V='1'") {
		t.Errorf("sealed page has no LineWeight 1: %s", xml)
	}
}

// TestSetStyleName checks the OSS row: V1 s1 name Start reads back
// name Start, stored as Name and NameU.
func TestSetStyleName(t *testing.T) {
	src := vsdxtest.Fixture(t, "v1-minimal")
	name := "Start"
	spec := &ObjectSetSpec{At: "page0/s1", Name: &name}
	changed, out := styleRun(t, src, spec, nil, "pt")
	if len(changed) != 1 || changed[0] != "page0/s1" {
		t.Fatalf("changed = %v, want [page0/s1]", changed)
	}
	if got := styleRead(t, out, "page0/s1"); got.Name != "Start" {
		t.Errorf("name = %q, want Start", got.Name)
	}
	if xml := propDeletePageXML(t, out); !strings.Contains(xml, "NameU='Start' Name='Start'") {
		t.Errorf("sealed page has no Start name: %s", xml)
	}
}

// TestSetStyleThemed checks the OSS row: a shape with FillForegnd
// F THEMEVAL() (V10 s9) is overwritten with the formula removed.
func TestSetStyleThemed(t *testing.T) {
	src := vsdxtest.Fixture(t, "v10-defects")
	spec := &ObjectSetSpec{At: "page0/s9", Fill: &PaintSpec{Hex: "#00FF00"}}
	changed, out := styleRun(t, src, spec, nil, "pt")
	if len(changed) != 1 || changed[0] != "page0/s9" {
		t.Fatalf("changed = %v, want [page0/s9]", changed)
	}
	if got := styleRead(t, out, "page0/s9"); string(got.Fill) != `{"hex":"#00FF00"}` {
		t.Errorf("fill = %s, want {\"hex\":\"#00FF00\"}", got.Fill)
	}
	xml := propDeletePageXML(t, out)
	if strings.Contains(xml, "THEMEVAL") {
		t.Errorf("sealed page still has THEMEVAL: %s", xml)
	}
	if !strings.Contains(xml, "<Cell N='FillForegnd' V='#00FF00'/>") {
		t.Errorf("sealed page has no constant FillForegnd: %s", xml)
	}
}

// TestSetStyleIdentity checks that writing the effective values
// (fill, line and name) changes nothing: changed is empty and the
// sealed page is the original page.
func TestSetStyleIdentity(t *testing.T) {
	src := vsdxtest.Fixture(t, "v1-minimal")
	w := float64(0.75)
	name := "Rectangle"
	spec := &ObjectSetSpec{At: "page0/s1", Fill: &PaintSpec{Hex: "#4472C4"}, Line: &LineSpec{W: &w, Color: "#7F7F7F", Dash: "1"}, Name: &name}
	changed, out := styleRun(t, src, spec, nil, "pt")
	if len(changed) != 0 {
		t.Errorf("changed = %v, want empty for identity", changed)
	}
	if got, want := propDeletePageXML(t, out), propDeletePageXML(t, src); got != want {
		t.Errorf("identity changed the page:\ngot  %s\nwant %s", got, want)
	}
}

// TestSetStyleUntargeted checks that setting the fill of V3 s1
// leaves V3 s2's fill and line as read from the original.
func TestSetStyleUntargeted(t *testing.T) {
	src := vsdxtest.Fixture(t, "v3-styles")
	before := styleRead(t, src, "page0/s2")
	spec := &ObjectSetSpec{At: "page0/s1", Fill: &PaintSpec{Hex: "#00FF00"}}
	_, out := styleRun(t, src, spec, nil, "pt")
	after := styleRead(t, out, "page0/s2")
	if string(after.Fill) != string(before.Fill) || string(after.Line) != string(before.Line) || after.Name != before.Name {
		t.Errorf("untargeted s2 changed: before fill %s line %s, after fill %s line %s", before.Fill, before.Line, after.Fill, after.Line)
	}
}

// TestSetStyleErrors checks the refusals: a missing shape and a
// missing alias are not_found at at, a master, Foreign and invalid
// values are operation_failed, and a cell outside the allow list is
// in the formula refusal form at fill.
func TestSetStyleErrors(t *testing.T) {
	for _, tc := range []struct {
		fixture string
		spec    *ObjectSetSpec
		aliases map[string]string
		code    string
		path    string
	}{
		{"v1-minimal", &ObjectSetSpec{At: "page0/s99", Fill: &PaintSpec{Hex: "#00FF00"}}, nil, cli.CodeNotFound, "at"},
		{"v1-minimal", &ObjectSetSpec{At: "$missing", Fill: &PaintSpec{Hex: "#00FF00"}}, nil, cli.CodeNotFound, "at"},
		{"v2-master", &ObjectSetSpec{At: "master2/s5", Fill: &PaintSpec{Hex: "#00FF00"}}, nil, cli.CodeOperationFailed, "at"},
		{"v15-foreign", &ObjectSetSpec{At: "page0/s1", Fill: &PaintSpec{Hex: "#00FF00"}}, nil, cli.CodeOperationFailed, "at"},
		{"v1-minimal", &ObjectSetSpec{At: "page0/s1", Fill: &PaintSpec{Hex: "red"}}, nil, cli.CodeOperationFailed, "fill"},
		{"v1-minimal", &ObjectSetSpec{At: "page0/s1", Line: &LineSpec{Color: "red"}}, nil, cli.CodeOperationFailed, "line"},
	} {
		t.Run(tc.spec.At+"/"+tc.path, func(t *testing.T) {
			src := vsdxtest.Fixture(t, tc.fixture)
			w, err := edit.Begin(src)
			if err != nil {
				t.Fatalf("Begin: %v", err)
			}
			defer w.Tx().Rollback()
			_, _, err = setStyle(w, tc.spec, tc.aliases, "pt")
			var ce *cli.Error
			if !errors.As(err, &ce) || ce.Code != tc.code || ce.Path != tc.path {
				t.Errorf("setStyle: got %v, want %s at %s", err, tc.code, tc.path)
			}
		})
	}
	t.Run("formula", func(t *testing.T) {
		src := vsdxtest.Fixture(t, "v1-minimal")
		w, err := edit.Begin(src)
		if err != nil {
			t.Fatalf("Begin: %v", err)
		}
		defer w.Tx().Rollback()
		if err := w.SetCells("page0/s1", []edit.Cell{{N: "FillForegnd", V: "#4472c4", F: "Width*0.5"}}); err != nil {
			t.Fatalf("SetCells: %v", err)
		}
		spec := &ObjectSetSpec{At: "page0/s1", Fill: &PaintSpec{Hex: "#00FF00"}}
		_, _, err = setStyle(w, spec, nil, "pt")
		var ce *cli.Error
		if !errors.As(err, &ce) || ce.Code != cli.CodeOperationFailed || ce.Path != "fill" {
			t.Fatalf("formula refusal: got %v, want operation_failed at fill", err)
		}
		if !strings.Contains(ce.Message, "FillForegnd") || !strings.Contains(ce.Message, "is bound to a formula vsdx does not evaluate") {
			t.Errorf("refusal = %q, want it to name FillForegnd in the formula refusal form", ce.Message)
		}
		if ce.Detail != "FillForegnd F=Width*0.5" {
			t.Errorf("detail = %q, want FillForegnd F=Width*0.5", ce.Detail)
		}
	})
}

// TestSetStyleAtomicity checks that a refused setStyle writes
// nothing: an invalid fill with a name in the same spec leaves the
// page and name as they were.
func TestSetStyleAtomicity(t *testing.T) {
	src := vsdxtest.Fixture(t, "v1-minimal")
	w, err := edit.Begin(src)
	if err != nil {
		t.Fatalf("Begin: %v", err)
	}
	name := "Start"
	spec := &ObjectSetSpec{At: "page0/s1", Fill: &PaintSpec{Hex: "red"}, Name: &name}
	if _, _, err := setStyle(w, spec, nil, "pt"); err == nil {
		t.Fatal("setStyle with an invalid fill succeeded, want a refusal")
	}
	if err := w.Verify(); err != nil {
		t.Fatalf("Verify: %v", err)
	}
	out := propDeleteSnapshot(t, w)
	if got, want := propDeletePageXML(t, out), propDeletePageXML(t, src); got != want {
		t.Errorf("refused setStyle changed the page:\ngot  %s\nwant %s", got, want)
	}
	if got := styleRead(t, out, "page0/s1"); got.Name != "Rectangle" {
		t.Errorf("name = %q, want Rectangle after the refusal", got.Name)
	}
}

// TestSetStyleAlias checks that a $name alias resolves to its shape.
func TestSetStyleAlias(t *testing.T) {
	src := vsdxtest.Fixture(t, "v1-minimal")
	name := "Start"
	spec := &ObjectSetSpec{At: "$a", Name: &name}
	changed, out := styleRun(t, src, spec, map[string]string{"a": "page0/s1"}, "pt")
	if len(changed) != 1 || changed[0] != "page0/s1" {
		t.Fatalf("changed = %v, want [page0/s1]", changed)
	}
	if got := styleRead(t, out, "page0/s1"); got.Name != "Start" {
		t.Errorf("name = %q, want Start", got.Name)
	}
}

// TestScalingSetStyle checks the scaling row: setStyle on
// Generated(1000) and Generated(4000) (N = 1000, 4N = 4000 shapes)
// allocates at most 5.5 times as much (laws.Scales) and the
// interleaved time ratio is at most 8 (laws.TimeScales).
func TestScalingSetStyle(t *testing.T) {
	setup := func(n int) (func() error, error) {
		src := vsdxtest.Generated(t, n)
		return func() error {
			w, err := edit.Begin(src)
			if err != nil {
				return err
			}
			defer w.Tx().Rollback()
			spec := &ObjectSetSpec{At: "page0/s1", Fill: &PaintSpec{Hex: "#00FF00"}}
			if _, _, err := setStyle(w, spec, nil, "pt"); err != nil {
				return err
			}
			return w.Verify()
		}, nil
	}
	laws.Scales(t, "vsdx setStyle shapes", setup)
	laws.TimeScales(t, "vsdx setStyle shapes", setup)
}
