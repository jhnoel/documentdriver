package ops

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/jhnoel/documentdriver/internal/cli"
	"github.com/jhnoel/documentdriver/internal/laws"
	"github.com/jhnoel/documentdriver/internal/opc"
	"github.com/jhnoel/documentdriver/internal/vsdx/edit"
	"github.com/jhnoel/documentdriver/internal/vsdx/integrity"
	"github.com/jhnoel/documentdriver/internal/vsdx/read"
	"github.com/jhnoel/documentdriver/internal/vsdx/vsd"
	"github.com/jhnoel/documentdriver/internal/vsdx/vsdxtest"
)

// glueObject is one object of a read Page, as set_glue tests read it.
type glueObject struct {
	ID       string        `json:"id"`
	From     []json.Number `json:"from"`
	To       []json.Number `json:"to"`
	FromGlue string        `json:"from_glue"`
	ToGlue   string        `json:"to_glue"`
	Kind     string        `json:"kind"`
}

// gluePage is the decoded read Page of set_glue tests.
type gluePage struct {
	Objects []glueObject      `json:"objects"`
	Hazards []json.RawMessage `json:"hazards"`
}

// glueRead reads page0 of the drawing at path through read.Page.
func glueRead(t *testing.T, path string) gluePage {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	raw, err := read.Page(b, "base", "page0", read.Options{Units: "pt"})
	if err != nil {
		t.Fatalf("read.Page: %v", err)
	}
	var page gluePage
	if err := json.Unmarshal(raw, &page); err != nil {
		t.Fatalf("decode page: %v", err)
	}
	return page
}

// glueFind returns the object with address id or fails the test.
func glueFind(t *testing.T, page gluePage, id string) glueObject {
	t.Helper()
	for _, o := range page.Objects {
		if o.ID == id {
			return o
		}
	}
	t.Fatalf("no object %s in %d objects", id, len(page.Objects))
	return glueObject{}
}

// gluePt checks a from/to point to 1e-6.
func gluePt(t *testing.T, name string, got []json.Number, want [2]float64) {
	t.Helper()
	if len(got) != 2 {
		t.Fatalf("%s: point %v, want %v", name, got, want)
	}
	for i := range want {
		f, err := got[i].Float64()
		if err != nil || f < want[i]-1e-6 || f > want[i]+1e-6 {
			t.Errorf("%s: point %v, want %v", name, got, want)
			return
		}
	}
}

// glueShapeXML returns the XML of the shape at addr in the drawing
// at path.
func glueShapeXML(t *testing.T, path, addr string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	raw, err := read.XML(b, addr)
	if err != nil {
		t.Fatalf("XML(%s): %v", addr, err)
	}
	return string(raw)
}

// gluePageXML returns the bytes of the output's page1.xml part.
func gluePageXML(t *testing.T, path string) string {
	t.Helper()
	pkg, err := opc.Open(path)
	if err != nil {
		t.Fatalf("open %s: %v", path, err)
	}
	defer pkg.Close()
	pt, err := pkg.Part("/visio/pages/page1.xml")
	if err != nil {
		t.Fatalf("page part: %v", err)
	}
	b, err := pt.Bytes()
	if err != nil {
		t.Fatalf("page part bytes: %v", err)
	}
	return string(b)
}

// glueRun runs setEnds of spec on a fresh workspace of the V5
// fixture and snapshots the result to out.
func glueRun(t *testing.T, spec *ObjectSetSpec, aliases map[string]string) (out string, changed []string, stale string, err error) {
	t.Helper()
	src := vsdxtest.Fixture(t, "v5-glue")
	w, err := edit.Begin(src)
	if err != nil {
		t.Fatalf("Begin: %v", err)
	}
	changed, stale, err = setEnds(w, spec, aliases, "pt")
	if err != nil {
		w.Tx().Rollback()
		return "", nil, "", err
	}
	if err := w.Verify(); err != nil {
		t.Fatalf("Verify: %v", err)
	}
	out = filepath.Join(t.TempDir(), "out.vsdx")
	if err := w.Tx().Snapshot(out); err != nil {
		t.Fatalf("Snapshot: %v", err)
	}
	w.Tx().Rollback()
	return out, changed, stale, nil
}

// TestSetGlueRows checks every OSG row, verbatim: V5 s4 to_glue
// "s2:2" gives Connect FromSheet 4 EndX ToSheet 2 Connections.X2
// ToPart 101, the end at s2's X2 (5.5, 9) reading to [396,144]
// with no dangling-glue; V5 s3 from_glue "none" removes its BeginX
// Connect and BeginX formula keeping V 2.5 with from_glue absent;
// whole-shape glue "s2" on a master-less connector is
// operation_failed.
func TestSetGlueRows(t *testing.T) {
	t.Run("s4-to-glue-s2-2", func(t *testing.T) {
		out, changed, _, err := glueRun(t, &ObjectSetSpec{At: "page0/s4", ToGlue: new("s2:2")}, nil)
		if err != nil {
			t.Fatalf("setEnds: %v", err)
		}
		if !slices.Equal(changed, []string{"page0/s4"}) {
			t.Errorf("changed = %v, want [page0/s4]", changed)
		}
		page := glueRead(t, out)
		s4 := glueFind(t, page, "page0/s4")
		if s4.ToGlue != "s2:2" {
			t.Errorf("s4 to_glue = %q, want s2:2", s4.ToGlue)
		}
		gluePt(t, "s4 to", s4.To, [2]float64{396, 144})
		for _, raw := range page.Hazards {
			var h struct {
				Kind string `json:"kind"`
				ID   string `json:"id"`
			}
			if err := json.Unmarshal(raw, &h); err != nil {
				t.Fatalf("decode hazard: %v", err)
			}
			if h.Kind == "dangling-glue" && h.ID == "page0/s4" {
				t.Errorf("dangling-glue hazard for s4 after gluing to s2:2: %s", raw)
			}
		}
		xml := glueShapeXML(t, out, "page0/s4")
		if !strings.Contains(xml, "PAR(PNT(Sheet.2!Connections.X2") {
			t.Errorf("s4 end formula is not the PAR(PNT point glue: %s", xml)
		}
		wantConnect := "<Connect FromSheet='4' FromCell='EndX' FromPart='12' ToSheet='2' ToCell='Connections.X2' ToPart='101'/>"
		if pageXML := gluePageXML(t, out); !strings.Contains(pageXML, wantConnect) {
			t.Errorf("page Connects do not hold %s", wantConnect)
		}
	})
	t.Run("s3-from-glue-none", func(t *testing.T) {
		out, changed, _, err := glueRun(t, &ObjectSetSpec{At: "page0/s3", FromGlue: new("none")}, nil)
		if err != nil {
			t.Fatalf("setEnds: %v", err)
		}
		if !slices.Equal(changed, []string{"page0/s3"}) {
			t.Errorf("changed = %v, want [page0/s3]", changed)
		}
		page := glueRead(t, out)
		s3 := glueFind(t, page, "page0/s3")
		if s3.FromGlue != "" {
			t.Errorf("s3 from_glue = %q, want free (absent)", s3.FromGlue)
		}
		if s3.ToGlue != "s2:1" {
			t.Errorf("s3 to_glue = %q, want s2:1 (untouched)", s3.ToGlue)
		}
		gluePt(t, "s3 from", s3.From, [2]float64{180, 144})
		xml := glueShapeXML(t, out, "page0/s3")
		if !strings.Contains(xml, "<Cell N='BeginX' V='2.5'/>") {
			t.Errorf("s3 BeginX did not keep V 2.5 without F: %s", xml)
		}
		if strings.Contains(xml, "BeginX' V='2.5' F=") {
			t.Errorf("s3 BeginX still has a formula: %s", xml)
		}
		if pageXML := gluePageXML(t, out); strings.Contains(pageXML, "FromSheet='3' FromCell='BeginX'") {
			t.Errorf("s3 BeginX Connect row still present: %s", pageXML)
		} else if !strings.Contains(pageXML, "FromSheet='3' FromCell='EndX'") {
			t.Errorf("s3 EndX Connect row was removed with the BeginX one")
		}
	})
	t.Run("whole-shape-on-master-less", func(t *testing.T) {
		_, _, _, err := glueRun(t, &ObjectSetSpec{At: "page0/s4", ToGlue: new("s2")}, nil)
		var ce *cli.Error
		if !errors.As(err, &ce) {
			t.Fatalf("whole-shape glue: error %v, want *cli.Error", err)
		}
		if ce.Code != cli.CodeOperationFailed || ce.Path != "to_glue" {
			t.Errorf("got %s at %q, want operation_failed at to_glue", ce.Code, ce.Path)
		}
		if !strings.Contains(ce.Message, "whole-shape") {
			t.Errorf("message %q does not name whole-shape glue", ce.Message)
		}
	})
}

// TestSetGlueIdentity checks that writing the same glue twice is
// a no-op the second time: the first write puts V5 s4's end on
// s2's X2 site with the PAR(PNT) formula, the second finds the
// value, formula and Connect row already there. (The fixture
// itself stores no end formula, so writing its existing glue
// once is a real change, not an identity.)
func TestSetGlueIdentity(t *testing.T) {
	spec := &ObjectSetSpec{At: "page0/s4", ToGlue: new("s2:2")}
	src := vsdxtest.Fixture(t, "v5-glue")
	w, err := edit.Begin(src)
	if err != nil {
		t.Fatalf("Begin: %v", err)
	}
	defer w.Tx().Rollback()
	if _, _, err := setEnds(w, spec, nil, "pt"); err != nil {
		t.Fatalf("first setEnds: %v", err)
	}
	changed, stale, err := setEnds(w, spec, nil, "pt")
	if err != nil {
		t.Fatalf("second setEnds: %v", err)
	}
	if len(changed) != 0 {
		t.Errorf("second setEnds: changed = %v, want nothing (identity)", changed)
	}
	if stale != "" {
		t.Errorf("second setEnds: stale = %q, want empty", stale)
	}
	if err := w.Verify(); err != nil {
		t.Fatalf("Verify: %v", err)
	}
}

// TestSetGlueFreeEnd checks the from/to form: moving V5 s4's end
// to a free point frees it (its dangling Connect to s9 and its end
// formula go) and the transform follows by OneD.
func TestSetGlueFreeEnd(t *testing.T) {
	out, changed, _, err := glueRun(t, &ObjectSetSpec{At: "page0/s4", To: []float64{504, 144}}, nil)
	if err != nil {
		t.Fatalf("setEnds: %v", err)
	}
	if !slices.Equal(changed, []string{"page0/s4"}) {
		t.Errorf("changed = %v, want [page0/s4]", changed)
	}
	page := glueRead(t, out)
	s4 := glueFind(t, page, "page0/s4")
	if s4.ToGlue != "" {
		t.Errorf("s4 to_glue = %q, want free after a to move", s4.ToGlue)
	}
	gluePt(t, "s4 to", s4.To, [2]float64{504, 144})
	gluePt(t, "s4 from", s4.From, [2]float64{396, 144})
	xml := glueShapeXML(t, out, "page0/s4")
	// Ends are now 5.5 to 7 inches: PinX 6.25, Width 1.5 unchanged
	// here, so check the end cells carry no formula.
	if strings.Contains(xml, "EndX' V='7' F=") {
		t.Errorf("s4 EndX still has a formula after freeing: %s", xml)
	}
	for _, raw := range page.Hazards {
		var h struct {
			Kind string `json:"kind"`
			ID   string `json:"id"`
		}
		if err := json.Unmarshal(raw, &h); err != nil {
			t.Fatalf("decode hazard: %v", err)
		}
		if h.Kind == "dangling-glue" && h.ID == "page0/s4" {
			t.Errorf("dangling-glue hazard for s4 after freeing its end: %s", raw)
		}
	}
}

// TestSetGlueUnitsIn checks from/to in inches: with units "in",
// to [7,2] is the view point (504,144) points, stored (7,9).
func TestSetGlueUnitsIn(t *testing.T) {
	src := vsdxtest.Fixture(t, "v5-glue")
	w, err := edit.Begin(src)
	if err != nil {
		t.Fatalf("Begin: %v", err)
	}
	if _, _, err := setEnds(w, &ObjectSetSpec{At: "page0/s4", To: []float64{7, 2}}, nil, "in"); err != nil {
		t.Fatalf("setEnds: %v", err)
	}
	if err := w.Verify(); err != nil {
		t.Fatalf("Verify: %v", err)
	}
	out := filepath.Join(t.TempDir(), "out.vsdx")
	if err := w.Tx().Snapshot(out); err != nil {
		t.Fatalf("Snapshot: %v", err)
	}
	w.Tx().Rollback()
	s4 := glueFind(t, glueRead(t, out), "page0/s4")
	gluePt(t, "s4 to", s4.To, [2]float64{504, 144})
	if s4.ToGlue != "" {
		t.Errorf("s4 to_glue = %q, want free", s4.ToGlue)
	}
}

// TestSetGlueAlias checks a $name glue target: an earlier alias
// names s2, and to_glue "$a:2" glues as s2:2 does.
func TestSetGlueAlias(t *testing.T) {
	out, _, _, err := glueRun(t, &ObjectSetSpec{At: "page0/s4", ToGlue: new("$a:2")}, map[string]string{"a": "page0/s2"})
	if err != nil {
		t.Fatalf("setEnds: %v", err)
	}
	s4 := glueFind(t, glueRead(t, out), "page0/s4")
	if s4.ToGlue != "s2:2" {
		t.Errorf("s4 to_glue = %q, want s2:2", s4.ToGlue)
	}
	gluePt(t, "s4 to", s4.To, [2]float64{396, 144})
}

// TestSetGlueRefusals checks the refusals: a missing glue target
// and a missing connection point are not_found at the glue field, a
// non-1-D shape is operation_failed, and an end cell with a GUARD
// formula is operation_failed naming the cell and formula.
func TestSetGlueRefusals(t *testing.T) {
	for _, tc := range []struct {
		name string
		spec *ObjectSetSpec
		code string
		path string
	}{
		{"missing-target", &ObjectSetSpec{At: "page0/s4", ToGlue: new("s99:1")}, cli.CodeNotFound, "to_glue"},
		{"missing-point", &ObjectSetSpec{At: "page0/s4", ToGlue: new("s2:9")}, cli.CodeNotFound, "to_glue"},
		{"not-1d", &ObjectSetSpec{At: "page0/s1", From: []float64{0, 0}}, cli.CodeOperationFailed, "at"},
		{"missing-shape", &ObjectSetSpec{At: "page0/s99", To: []float64{0, 0}}, cli.CodeNotFound, "at"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			src := vsdxtest.Fixture(t, "v5-glue")
			w, err := edit.Begin(src)
			if err != nil {
				t.Fatalf("Begin: %v", err)
			}
			defer w.Tx().Rollback()
			_, _, err = setEnds(w, tc.spec, nil, "pt")
			var ce *cli.Error
			if !errors.As(err, &ce) {
				t.Fatalf("setEnds: error %v, want *cli.Error", err)
			}
			if ce.Code != tc.code || ce.Path != tc.path {
				t.Errorf("got %s at %q, want %s at %s", ce.Code, ce.Path, tc.code, tc.path)
			}
		})
	}
	t.Run("guard-formula", func(t *testing.T) {
		src := vsdxtest.Fixture(t, "v5-glue")
		w, err := edit.Begin(src)
		if err != nil {
			t.Fatalf("Begin: %v", err)
		}
		defer w.Tx().Rollback()
		if err := w.SetCells("page0/s4", []edit.Cell{{N: vsd.CellEndX, V: "7", F: "GUARD(7)"}}); err != nil {
			t.Fatalf("SetCells: %v", err)
		}
		_, _, err = setEnds(w, &ObjectSetSpec{At: "page0/s4", ToGlue: new("none")}, nil, "pt")
		var ce *cli.Error
		if !errors.As(err, &ce) {
			t.Fatalf("setEnds: error %v, want *cli.Error", err)
		}
		if ce.Code != cli.CodeOperationFailed || ce.Path != "to_glue" {
			t.Errorf("got %s at %q, want operation_failed at to_glue", ce.Code, ce.Path)
		}
		if !strings.Contains(ce.Message, "EndX") || !strings.Contains(ce.Message, "is bound to a formula vsdx does not evaluate") {
			t.Errorf("message %q does not name cell EndX in the formula refusal form", ce.Message)
		}
		if ce.Detail != "EndX F=GUARD(7)" {
			t.Errorf("detail = %q, want EndX F=GUARD(7)", ce.Detail)
		}
	})
	t.Run("from-and-glue", func(t *testing.T) {
		out, changed, stale, err := glueRun(t, &ObjectSetSpec{At: "page0/s4", From: []float64{0, 0}, FromGlue: new("s2:1")}, nil)
		if err != nil {
			t.Fatalf("setEnds: %v", err)
		}
		if !slices.Equal(changed, []string{"page0/s4"}) {
			t.Errorf("changed = %v, want [page0/s4]", changed)
		}
		if !strings.Contains(stale, "page0/s4") || !strings.Contains(stale, "begin") || !strings.Contains(stale, "stale-connector") {
			t.Errorf("stale = %q, want it to name page0/s4 begin and stale-connector (stale-values)", stale)
		}
		page := glueRead(t, out)
		s4 := glueFind(t, page, "page0/s4")
		if s4.FromGlue != "s2:1" {
			t.Errorf("s4 from_glue = %q, want s2:1", s4.FromGlue)
		}
		gluePt(t, "s4 from", s4.From, [2]float64{0, 0})
		found := false
		for _, raw := range page.Hazards {
			var h struct {
				Kind string `json:"kind"`
				ID   string `json:"id"`
			}
			if err := json.Unmarshal(raw, &h); err != nil {
				t.Fatalf("decode hazard: %v", err)
			}
			if h.Kind == "stale-connector" && h.ID == "page0/s4" {
				found = true
			}
		}
		if !found {
			t.Errorf("hazards %s, want a stale-connector for page0/s4", page.Hazards)
		}
	})
}

// TestEditGlueWholeDirect checks the edit layer's whole-shape
// Connect form directly: SetConnectorEnds with a whole end writes
// ToCell PinX ToPart 3, which read shows as to_glue "s2". The op
// layer refuses whole-shape glue (the OSG row), so this is the
// only whole-shape writer.
func TestEditGlueWholeDirect(t *testing.T) {
	src := vsdxtest.Fixture(t, "v5-glue")
	w, err := edit.Begin(src)
	if err != nil {
		t.Fatalf("Begin: %v", err)
	}
	changed, err := w.SetConnectorEnds("page0/s4", edit.ConnectorEnds{
		Begin: edit.GlueEnd{X: 5.5, Y: 9, KeepFormula: true, KeepConnect: true},
		End:   edit.GlueEnd{X: 5.5, Y: 9, Glue: true, Whole: true, Target: 2, KeepFormula: true},
		PinX:  5.5, PinY: 9, Width: 0, Height: 0, LocPinX: 0, LocPinY: 0, Angle: 0,
	})
	if err != nil {
		t.Fatalf("SetConnectorEnds: %v", err)
	}
	if !changed {
		t.Error("SetConnectorEnds changed = false, want true")
	}
	if err := w.Verify(); err != nil {
		t.Fatalf("Verify: %v", err)
	}
	out := filepath.Join(t.TempDir(), "out.vsdx")
	if err := w.Tx().Snapshot(out); err != nil {
		t.Fatalf("Snapshot: %v", err)
	}
	w.Tx().Rollback()
	s4 := glueFind(t, glueRead(t, out), "page0/s4")
	if s4.ToGlue != "s2" {
		t.Errorf("s4 to_glue = %q, want s2 (whole-shape)", s4.ToGlue)
	}
}

// TestSetGlueUntargeted checks that gluing s4's end leaves every
// other shape and Connect alone, and that the output introduces no
// new integrity finding.
func TestSetGlueUntargeted(t *testing.T) {
	src := vsdxtest.Fixture(t, "v5-glue")
	inPkg, err := opc.Open(src)
	if err != nil {
		t.Fatalf("open input: %v", err)
	}
	inFindings, err := integrity.Check(inPkg)
	if err != nil {
		t.Fatalf("integrity input: %v", err)
	}
	inPkg.Close()
	out, _, _, err := glueRun(t, &ObjectSetSpec{At: "page0/s4", ToGlue: new("s2:2")}, nil)
	if err != nil {
		t.Fatalf("setEnds: %v", err)
	}
	page := glueRead(t, out)
	s3 := glueFind(t, page, "page0/s3")
	if s3.FromGlue != "s1" || s3.ToGlue != "s2:1" {
		t.Errorf("s3 glue changed: %q %q, want s1 s2:1", s3.FromGlue, s3.ToGlue)
	}
	gluePt(t, "s3 from", s3.From, [2]float64{180, 144})
	gluePt(t, "s3 to", s3.To, [2]float64{324, 144})
	s5 := glueFind(t, page, "page0/s5")
	if s5.FromGlue != "s1:1" {
		t.Errorf("s5 from_glue = %q, want s1:1", s5.FromGlue)
	}
	outPkg, err := opc.Open(out)
	if err != nil {
		t.Fatalf("open output: %v", err)
	}
	outFindings, err := integrity.Check(outPkg)
	if err != nil {
		t.Fatalf("integrity output: %v", err)
	}
	outPkg.Close()
	for _, f := range outFindings {
		if !slices.Contains(inFindings, f) {
			t.Errorf("integrity output has new finding %+v", f)
		}
	}
}

// TestScalingSetGlue checks the scaling row: gluing one connector
// on a page of N shapes (laws.Small and laws.Large) allocates and
// takes time in proportion to N.
func TestScalingSetGlue(t *testing.T) {
	setup := func(n int) (func() error, error) {
		src := vsdxtest.Generated(t, n)
		connector := "page0/s" + strconv.Itoa(n+1)
		return func() error {
			w, err := edit.Begin(src)
			if err != nil {
				return err
			}
			defer w.Tx().Rollback()
			spec := &ObjectSetSpec{At: connector, To: []float64{504, 144}}
			if _, _, err := setEnds(w, spec, nil, "pt"); err != nil {
				return err
			}
			return w.Verify()
		}, nil
	}
	laws.Scales(t, "vsdx object.set glue shapes", setup)
	laws.TimeScales(t, "vsdx object.set glue shapes", setup)
}
