package ops_test

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jhnoel/documentdriver/internal/cli"
	"github.com/jhnoel/documentdriver/internal/laws"
	"github.com/jhnoel/documentdriver/internal/opc"
	"github.com/jhnoel/documentdriver/internal/vsdx/apply"
	"github.com/jhnoel/documentdriver/internal/vsdx/doc"
	"github.com/jhnoel/documentdriver/internal/vsdx/edit"
	"github.com/jhnoel/documentdriver/internal/vsdx/ops"
	"github.com/jhnoel/documentdriver/internal/vsdx/vsdxtest"
)

// TestObjectMoveV1 checks the V1 s1 row: dx 72 dy 36 moves the box
// to [180,162,72,36] and stores PinX 3, PinY 8.5.
func TestObjectMoveV1(t *testing.T) {
	path := vsdxtest.Fixture(t, "v1-minimal")
	out := filepath.Join(t.TempDir(), "out.vsdx")
	res, err := apply.Run(path, parsePlan(t, `{"ops":[{"op":"object.move","at":"page0/s1","dx":72,"dy":36}]}`), apply.Options{Out: out})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(res.Changed) != 1 {
		t.Errorf("changed = %v, want [page0/s1]", res.Changed)
	}
	page := readPage(t, out)
	i := findObject(t, page, "page0/s1")
	checkBox(t, "s1", page.Objects[i].Box, [4]float64{180, 162, 72, 36})
	xml := shapeXML(t, out, "page0/s1")
	if !strings.Contains(xml, "<Cell N='PinX' V='3'/>") {
		t.Errorf("stored PinX is not 3: %s", xml)
	}
	if !strings.Contains(xml, "<Cell N='PinY' V='8.5'/>") {
		t.Errorf("stored PinY is not 8.5: %s", xml)
	}
	checkNoNewFindings(t, path, out)
}

// TestObjectMoveV5Stale checks the V5 s1 row: dx 72 leaves s3's
// begin (and s5's begin) glued to the moved shape, reports
// stale-connector for s3's begin, and sets RecalcDocument in
// custom.xml.
func TestObjectMoveV5Stale(t *testing.T) {
	path := vsdxtest.Fixture(t, "v5-glue")
	out := filepath.Join(t.TempDir(), "out.vsdx")
	res, err := apply.Run(path, parsePlan(t, `{"ops":[{"op":"object.move","at":"page0/s1","dx":72}]}`), apply.Options{Out: out})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	found := false
	for _, h := range res.Hazards {
		if h.Kind == "stale-values" && strings.Contains(h.Detail, "page0/s3") && strings.Contains(h.Detail, "begin") && strings.Contains(h.Detail, "stale-connector") {
			found = true
		}
	}
	if !found {
		t.Errorf("hazards %+v, want a stale-values detail naming page0/s3 begin and stale-connector", res.Hazards)
	}
	pkg, err := opc.Open(out)
	if err != nil {
		t.Fatalf("open output: %v", err)
	}
	defer pkg.Close()
	if !pkg.Has("/docProps/custom.xml") {
		t.Error("the output has no docProps/custom.xml")
	}
	d, err := doc.Open(pkg)
	if err != nil {
		t.Fatalf("doc.Open output: %v", err)
	}
	if !d.Recalc {
		t.Error("custom.xml has no RecalcDocument true")
	}
	page := readPage(t, out)
	i := findObject(t, page, "page0/s1")
	checkBox(t, "s1", page.Objects[i].Box, [4]float64{180, 126, 72, 36})
}

// TestObjectMoveV5OneD checks the CC2 rows: V5 s3 is glued at both
// ends (from_glue s1, to_glue s2:1), so dx 72 is operation_failed
// at ops[0].at (both ends glued); V5 s5 (from_glue s1:1, to free)
// dx 72 keeps from at [216,216] glued s1:1, moves to to [288,288],
// and recomputes PinX, Width and Angle by the line family.
func TestObjectMoveV5OneD(t *testing.T) {
	path := vsdxtest.Fixture(t, "v5-glue")
	_, err := apply.Run(path, parsePlan(t, `{"ops":[{"op":"object.move","at":"page0/s3","dx":72}]}`), apply.Options{Out: filepath.Join(t.TempDir(), "out.vsdx")})
	var ce *cli.Error
	if !errors.As(err, &ce) {
		t.Fatalf("error %v, want *cli.Error", err)
	}
	if ce.Code != cli.CodeOperationFailed || ce.Path != "ops[0].at" {
		t.Errorf("got %s at %q, want operation_failed at ops[0].at", ce.Code, ce.Path)
	}
	if !strings.Contains(ce.Message, "both ends glued") {
		t.Errorf("message %q does not name both ends glued", ce.Message)
	}
	out := filepath.Join(t.TempDir(), "out.vsdx")
	if _, err := apply.Run(path, parsePlan(t, `{"ops":[{"op":"object.move","at":"page0/s5","dx":72}]}`), apply.Options{Out: out}); err != nil {
		t.Fatalf("Run: %v", err)
	}
	page := readPage(t, out)
	i := findObject(t, page, "page0/s5")
	checkPt(t, "s5 from", page.Objects[i].From, [2]float64{216, 216})
	checkPt(t, "s5 to", page.Objects[i].To, [2]float64{288, 288})
	if page.Objects[i].FromGlue != "s1:1" {
		t.Errorf("s5 from_glue = %q, want s1:1 (kept)", page.Objects[i].FromGlue)
	}
	if page.Objects[i].ToGlue != "" {
		t.Errorf("s5 to_glue = %q, want free", page.Objects[i].ToGlue)
	}
	xml := shapeXML(t, out, "page0/s5")
	if !strings.Contains(xml, "N='PinX' V='3.5'") {
		t.Errorf("stored PinX is not 3.5: %s", xml)
	}
	if !strings.Contains(xml, "N='Width'") {
		t.Errorf("stored Width missing: %s", xml)
	}
	if !strings.Contains(xml, "N='BeginX' V='3'") {
		t.Errorf("stored BeginX did not stay 3: %s", xml)
	}
	if !strings.Contains(xml, "N='EndX' V='4'") {
		t.Errorf("stored EndX is not 4: %s", xml)
	}
	checkNoNewFindings(t, path, out)
}

// TestObjectMoveV4Group checks the V4 s10 row: dx 72 moves the group
// as one, s11's box becomes [288,432,72,72], and s11's stored cells
// are unchanged.
func TestObjectMoveV4Group(t *testing.T) {
	path := vsdxtest.Fixture(t, "v4-groups")
	before := shapeXML(t, path, "page0/s11")
	out := filepath.Join(t.TempDir(), "out.vsdx")
	if _, err := apply.Run(path, parsePlan(t, `{"ops":[{"op":"object.move","at":"page0/s10","dx":72}]}`), apply.Options{Out: out}); err != nil {
		t.Fatalf("Run: %v", err)
	}
	page := readPage(t, out)
	i := findObject(t, page, "page0/s11")
	checkBox(t, "s11", page.Objects[i].Box, [4]float64{288, 432, 72, 72})
	if after := shapeXML(t, out, "page0/s11"); after != before {
		t.Errorf("s11 cells changed:\nbefore %s\nafter  %s", before, after)
	}
	checkNoNewFindings(t, path, out)
}

// TestObjectMoveV2Refused checks the V2 s4 row: the group child's
// PinX formula Sheet.5!Width*0.25 is outside the allow list, so the
// move is operation_failed at ops[0].at naming PinX, with the
// formula in detail.
func TestObjectMoveV2Refused(t *testing.T) {
	path := vsdxtest.Fixture(t, "v2-master")
	_, err := apply.Run(path, parsePlan(t, `{"ops":[{"op":"object.move","at":"page0/s4","dx":72}]}`), apply.Options{Out: filepath.Join(t.TempDir(), "out.vsdx")})
	var ce *cli.Error
	if !errors.As(err, &ce) {
		t.Fatalf("error %v, want *cli.Error", err)
	}
	if ce.Code != cli.CodeOperationFailed || ce.Path != "ops[0].at" {
		t.Errorf("got %s at %q, want operation_failed at ops[0].at", ce.Code, ce.Path)
	}
	if !strings.Contains(ce.Message, "PinX") || !strings.Contains(ce.Message, "is bound to a formula vsdx does not evaluate") {
		t.Errorf("message %q does not name PinX in the formula refusal form", ce.Message)
	}
	if ce.Detail != "PinX F=Sheet.5!Width*0.25" {
		t.Errorf("detail = %q, want PinX F=Sheet.5!Width*0.25", ce.Detail)
	}
}

// TestObjectMoveIdentity checks the identity row: dx 0 dy 0 on V1 s1
// writes nothing.
func TestObjectMoveIdentity(t *testing.T) {
	path := vsdxtest.Fixture(t, "v1-minimal")
	out := filepath.Join(t.TempDir(), "out.vsdx")
	res, err := apply.Run(path, parsePlan(t, `{"ops":[{"op":"object.move","at":"page0/s1","dx":0,"dy":0}]}`), apply.Options{Out: out})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if _, err := os.Stat(out); !errors.Is(err, os.ErrNotExist) {
		t.Error("identity move wrote its out file")
	}
	if len(res.Parts) != 0 || len(res.Changed) != 0 {
		t.Errorf("identity result parts %v changed %v, want empty", res.Parts, res.Changed)
	}
	if res.NewBase != res.Base {
		t.Errorf("identity new_base %q != base %q", res.NewBase, res.Base)
	}
	if len(res.Hazards) != 0 {
		t.Errorf("identity hazards %+v, want none", res.Hazards)
	}
}

// TestObjectMoveNotFound checks that moving a missing shape is
// not_found at ops[0].at.
func TestObjectMoveNotFound(t *testing.T) {
	path := vsdxtest.Fixture(t, "v1-minimal")
	_, err := apply.Run(path, parsePlan(t, `{"ops":[{"op":"object.move","at":"page0/s99","dx":72}]}`), apply.Options{Out: filepath.Join(t.TempDir(), "out.vsdx")})
	var ce *cli.Error
	if !errors.As(err, &ce) {
		t.Fatalf("error %v, want *cli.Error", err)
	}
	if ce.Code != cli.CodeNotFound || ce.Path != "ops[0].at" {
		t.Errorf("got %s at %q, want not_found at ops[0].at", ce.Code, ce.Path)
	}
}

// TestScalingObjectMove checks the scaling row: moving one shape on a
// page of N shapes allocates and takes time in proportion to N.
func TestScalingObjectMove(t *testing.T) {
	setup := func(n int) (func() error, error) {
		src := vsdxtest.Generated(t, n)
		return func() error {
			w, err := edit.Begin(src)
			if err != nil {
				return err
			}
			defer w.Tx().Rollback()
			_, _, _, _, err = (&ops.ObjectMoveSpec{At: "page0/s1", DX: 72}).Run(w, map[string]string{}, "pt", nil)
			if err != nil {
				return err
			}
			return w.Verify()
		}, nil
	}
	laws.Scales(t, "vsdx object.move", setup)
	laws.TimeScales(t, "vsdx object.move", setup)
}
