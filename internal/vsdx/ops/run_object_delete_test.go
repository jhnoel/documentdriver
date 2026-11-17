package ops_test

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/jhnoel/documentdriver/internal/cli"
	"github.com/jhnoel/documentdriver/internal/laws"
	"github.com/jhnoel/documentdriver/internal/opc"
	"github.com/jhnoel/documentdriver/internal/vsdx/apply"
	"github.com/jhnoel/documentdriver/internal/vsdx/edit"
	"github.com/jhnoel/documentdriver/internal/vsdx/integrity"
	"github.com/jhnoel/documentdriver/internal/vsdx/ops"
	"github.com/jhnoel/documentdriver/internal/vsdx/read"
	"github.com/jhnoel/documentdriver/internal/vsdx/vsdxtest"
)

// hazardJSON is one page hazard as read shows it.
type hazardJSON struct {
	Kind   string `json:"kind"`
	ID     string `json:"id"`
	Detail string `json:"detail"`
}

// checkNoNewFindings fails when the drawing at out carries an
// integrity finding the drawing at in lacks.
func checkNoNewFindings(t *testing.T, in, out string) {
	t.Helper()
	inFindings, err := integrityOf(t, in)
	if err != nil {
		t.Fatalf("integrity input: %v", err)
	}
	outFindings, err := integrityOf(t, out)
	if err != nil {
		t.Fatalf("integrity output: %v", err)
	}
	for _, f := range outFindings {
		if !slices.Contains(inFindings, f) {
			t.Errorf("integrity output has new finding %+v", f)
		}
	}
}

// integrityOf returns the integrity findings of the drawing at path.
func integrityOf(t *testing.T, path string) ([]integrity.Finding, error) {
	t.Helper()
	pkg, err := opc.Open(path)
	if err != nil {
		return nil, err
	}
	t.Cleanup(func() {
		if err := pkg.Close(); err != nil {
			t.Errorf("close %s: %v", path, err)
		}
	})
	return integrity.Check(pkg)
}

// pageHazards returns the hazards of page0 of the drawing at path.
func pageHazards(t *testing.T, page pageJSON) []hazardJSON {
	t.Helper()
	var out []hazardJSON
	for _, raw := range page.Hazards {
		var h hazardJSON
		if err := json.Unmarshal(raw, &h); err != nil {
			t.Fatalf("decode hazard: %v", err)
		}
		out = append(out, h)
	}
	return out
}

// shapeXML returns the XML of the shape at addr in the drawing at path.
func shapeXML(t *testing.T, path, addr string) string {
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

// TestObjectDeleteV5 checks the V5 s1 row: deleted is page0/s1, the
// Connects of s3 begin and s5 begin are gone (s3 and s5 read free),
// s3 BeginX keeps V 2.5 without F, and no dangling-glue names s1.
func TestObjectDeleteV5(t *testing.T) {
	path := vsdxtest.Fixture(t, "v5-glue")
	out := filepath.Join(t.TempDir(), "out.vsdx")
	res, err := apply.Run(path, parsePlan(t, `{"ops":[{"op":"object.delete","at":"page0/s1"}]}`), apply.Options{Out: out})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !slices.Equal(res.Deleted, []string{"page0/s1"}) {
		t.Errorf("deleted = %v, want [page0/s1]", res.Deleted)
	}
	page := readPage(t, out)
	for _, id := range []string{"page0/s1"} {
		for _, obj := range page.Objects {
			if obj.ID == id {
				t.Errorf("object %s still present after delete", id)
			}
		}
	}
	i3 := findObject(t, page, "page0/s3")
	if page.Objects[i3].FromGlue != "" {
		t.Errorf("s3 from_glue = %q, want free", page.Objects[i3].FromGlue)
	}
	if page.Objects[i3].ToGlue != "s2:1" {
		t.Errorf("s3 to_glue = %q, want s2:1", page.Objects[i3].ToGlue)
	}
	checkPt(t, "s3 from", page.Objects[i3].From, [2]float64{180, 144})
	i5 := findObject(t, page, "page0/s5")
	if page.Objects[i5].FromGlue != "" {
		t.Errorf("s5 from_glue = %q, want free", page.Objects[i5].FromGlue)
	}
	for _, h := range pageHazards(t, page) {
		if h.Kind == "dangling-glue" && (h.ID == "page0/s3" || h.ID == "page0/s5") {
			t.Errorf("dangling-glue hazard after delete for s1: %+v", h)
		}
		if strings.Contains(h.Detail, "shape 1") {
			t.Errorf("dangling-glue hazard names shape 1 after delete: %+v", h)
		}
	}
	xml := shapeXML(t, out, "page0/s3")
	if !strings.Contains(xml, "<Cell N='BeginX' V='2.5'/>") {
		t.Errorf("s3 BeginX cell missing or changed: %s", xml)
	}
	if strings.Contains(xml, "BeginX' V='2.5' F=") {
		t.Errorf("s3 BeginX still has a formula: %s", xml)
	}
	checkNoNewFindings(t, path, out)
}

// TestObjectDeleteV4Group checks the V4 s10 row: deleting the group
// deletes s10, s11, s12 and s13 with it, in document order.
func TestObjectDeleteV4Group(t *testing.T) {
	path := vsdxtest.Fixture(t, "v4-groups")
	out := filepath.Join(t.TempDir(), "out.vsdx")
	res, err := apply.Run(path, parsePlan(t, `{"ops":[{"op":"object.delete","at":"page0/s10"}]}`), apply.Options{Out: out})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	want := []string{"page0/s10", "page0/s11", "page0/s12", "page0/s13"}
	if !slices.Equal(res.Deleted, want) {
		t.Errorf("deleted = %v, want %v", res.Deleted, want)
	}
	page := readPage(t, out)
	for _, id := range want {
		for _, obj := range page.Objects {
			if obj.ID == id {
				t.Errorf("object %s still present after group delete", id)
			}
		}
	}
	findObject(t, page, "page0/s20")
	findObject(t, page, "page0/s21")
	checkNoNewFindings(t, path, out)
}

// TestObjectDeleteV2Group checks the V2 s3 row: deleting the group
// instance deletes s3, s4 and s5 with it, in document order.
func TestObjectDeleteV2Group(t *testing.T) {
	path := vsdxtest.Fixture(t, "v2-master")
	out := filepath.Join(t.TempDir(), "out.vsdx")
	res, err := apply.Run(path, parsePlan(t, `{"ops":[{"op":"object.delete","at":"page0/s3"}]}`), apply.Options{Out: out})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	want := []string{"page0/s3", "page0/s4", "page0/s5"}
	if !slices.Equal(res.Deleted, want) {
		t.Errorf("deleted = %v, want %v", res.Deleted, want)
	}
	page := readPage(t, out)
	for _, id := range want {
		for _, obj := range page.Objects {
			if obj.ID == id {
				t.Errorf("object %s still present after group delete", id)
			}
		}
	}
	findObject(t, page, "page0/s1")
	findObject(t, page, "page0/s2")
	checkNoNewFindings(t, path, out)
}

// TestObjectDeleteGlueFormula checks that deleting a glue target
// removes the PAR(PNT(Sheet.N!...)) end formula and the _WALKGLUE
// and _XFTRIGGER formulas naming it, keeping each cell's V.
func TestObjectDeleteGlueFormula(t *testing.T) {
	src := vsdxtest.Fixture(t, "v5-glue")
	w, err := edit.Begin(src)
	if err != nil {
		t.Fatalf("Begin: %v", err)
	}
	par := "PAR(PNT(Sheet.1!Connections.X1,Sheet.1!Connections.Y1))"
	walk := "_WALKGLUE(BegTrigger,EndTrigger,WalkPreference)"
	if err := w.SetCells("page0/s3", []edit.Cell{
		{N: "BeginX", V: "2.5", F: par},
		{N: "BeginY", V: "9", F: par},
		{N: "BegTrigger", V: "2", F: "_XFTRIGGER(Sheet.1!EventXFMod)"},
	}); err != nil {
		t.Fatalf("SetCells s3: %v", err)
	}
	if err := w.SetCells("page0/s5", []edit.Cell{
		{N: "BeginX", V: "3", F: walk},
		{N: "BeginY", V: "8", F: walk},
	}); err != nil {
		t.Fatalf("SetCells s5: %v", err)
	}
	deleted, _, err := w.DeleteShape("page0/s1")
	if err != nil {
		t.Fatalf("DeleteShape: %v", err)
	}
	if len(deleted) != 1 || deleted[0] != "page0/s1" {
		t.Fatalf("deleted = %v, want [page0/s1]", deleted)
	}
	if err := w.Verify(); err != nil {
		t.Fatalf("Verify: %v", err)
	}
	out := filepath.Join(t.TempDir(), "out.vsdx")
	if err := w.Tx().Snapshot(out); err != nil {
		t.Fatalf("Snapshot: %v", err)
	}
	w.Tx().Rollback()
	xml3 := shapeXML(t, out, "page0/s3")
	if strings.Contains(xml3, "Sheet.1!") {
		t.Errorf("s3 XML still names deleted shape 1: %s", xml3)
	}
	if !strings.Contains(xml3, "<Cell N='BeginX' V='2.5'/>") {
		t.Errorf("s3 BeginX did not keep V 2.5 without F: %s", xml3)
	}
	if strings.Contains(xml3, "_XFTRIGGER") {
		t.Errorf("s3 BegTrigger still has a trigger formula: %s", xml3)
	}
	if !strings.Contains(xml3, "BegTrigger") || !strings.Contains(xml3, `V="2"`) {
		t.Errorf("s3 BegTrigger did not keep V 2 without F: %s", xml3)
	}
	xml5 := shapeXML(t, out, "page0/s5")
	if strings.Contains(xml5, "_WALKGLUE") {
		t.Errorf("s5 XML still has _WALKGLUE: %s", xml5)
	}
	if !strings.Contains(xml5, "<Cell N='BeginX' V='3'/>") {
		t.Errorf("s5 BeginX did not keep V 3 without F: %s", xml5)
	}
}

// TestObjectDeleteNotFound checks that deleting a missing shape is
// not_found at ops[0].at and leaves the file as it was.
func TestObjectDeleteNotFound(t *testing.T) {
	path := vsdxtest.Fixture(t, "v5-glue")
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	_, err = apply.Run(path, parsePlan(t, `{"ops":[{"op":"object.delete","at":"page0/s99"}]}`), apply.Options{Out: filepath.Join(t.TempDir(), "out.vsdx")})
	var ce *cli.Error
	if !errors.As(err, &ce) {
		t.Fatalf("error %v, want *cli.Error", err)
	}
	if ce.Code != cli.CodeNotFound || ce.Path != "ops[0].at" {
		t.Errorf("got %s at %q, want not_found at ops[0].at", ce.Code, ce.Path)
	}
	after, _ := os.ReadFile(path)
	if string(after) != string(before) {
		t.Error("a refused plan changed the file")
	}
}

// TestScalingObjectDelete checks the scaling row: deleting one shape
// from a page of N shapes allocates and takes time in proportion to N.
func TestScalingObjectDelete(t *testing.T) {
	setup := func(n int) (func() error, error) {
		src := vsdxtest.Generated(t, n)
		return func() error {
			w, err := edit.Begin(src)
			if err != nil {
				return err
			}
			defer w.Tx().Rollback()
			_, _, _, _, err = (&ops.ObjectDeleteSpec{At: "page0/s1"}).Run(w, map[string]string{}, "pt", nil)
			if err != nil {
				return err
			}
			return w.Verify()
		}, nil
	}
	laws.Scales(t, "vsdx object.delete", setup)
	laws.TimeScales(t, "vsdx object.delete", setup)
}
