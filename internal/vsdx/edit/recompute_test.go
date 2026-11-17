package edit

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/jhnoel/documentdriver/internal/laws"
	"github.com/jhnoel/documentdriver/internal/opc"
	"github.com/jhnoel/documentdriver/internal/vsdx/sheet"
	"github.com/jhnoel/documentdriver/internal/vsdx/vsd"
	"github.com/jhnoel/documentdriver/internal/vsdx/vsdxtest"
)

// localCell returns the stored cell at k of the shape with stored ID
// id in the working page part, and whether the shape stores it.
func localCell(t *testing.T, w *Workspace, id uint32, k sheet.Key) (sheet.Cell, bool) {
	t.Helper()
	s := findShape(pageRoot(t, w, "/visio/pages/page1.xml"), id, 1)
	if s == nil {
		t.Fatalf("no shape %d in the working page", id)
	}
	sh, _ := sheet.Index(s)
	return sh.Cell(k)
}

// TestRecomputeShapeV2Width checks the RC1 row: V2 s2 with Width set
// to 2 (master LocPinX F Width*0.5, Geometry rows Width*k) recomputes
// to LocPinX V 1 materialised with F="Inh", Geometry rows rewritten
// (MoveTo X 0, LineTo X 2 ...), Connection X 1 materialised and an
// empty stale list. Cells the change does not reach (LocPinY,
// Geometry Y) are not materialised, and the master part is unchanged.
func TestRecomputeShapeV2Width(t *testing.T) {
	src := vsdxtest.Fixture(t, "v2-master")
	w := mustBegin(t, src)
	if err := w.SetCells("page0/s2", []Cell{{N: "Width", V: "2"}}); err != nil {
		t.Fatalf("SetCells(Width): %v", err)
	}
	stale, err := w.RecomputeShape("page0/s2", []string{"Width"})
	if err != nil {
		t.Fatalf("RecomputeShape: %v", err)
	}
	if len(stale) != 0 {
		t.Errorf("stale = %v, want empty", stale)
	}
	if err := w.Verify(); err != nil {
		t.Fatalf("Verify: %v", err)
	}
	loc, ok := localCell(t, w, 2, sheet.ShapeCell("LocPinX"))
	if !ok {
		t.Fatal("LocPinX was not materialised locally")
	}
	if loc.V != "1" || loc.F != "Inh" {
		t.Errorf("LocPinX = (V %q, F %q), want (1, Inh)", loc.V, loc.F)
	}
	if got := string(loc.Node.Bytes()); got != "<Cell N='LocPinX' V='1' F='Inh'/>" {
		t.Errorf("LocPinX bytes = %q, want <Cell N='LocPinX' V='1' F='Inh'/>", got)
	}
	for row, want := range map[int]string{1: "0", 2: "2", 3: "2", 4: "0", 5: "0"} {
		k := sheet.IndexedRowCell(vsd.SectionGeometry, 0, row, "X")
		if c, ok := localCell(t, w, 2, k); !ok || c.V != want {
			t.Errorf("Geometry1.X%d = (%q, %v), want V %q materialised", row, c.V, ok, want)
		}
	}
	if c, ok := localCell(t, w, 2, sheet.IndexedRowCell(vsd.SectionGeometry, 0, 2, "Y")); ok {
		t.Errorf("Geometry1.Y2 was materialised as %q, want it left to the master (Height)", c.V)
	}
	conn, ok := localCell(t, w, 2, sheet.IndexedRowCell(vsd.SectionConnection, -1, 0, "X"))
	if !ok || conn.V != "1" || conn.F != "Inh" {
		t.Errorf("Connections.X1 = (%q, %q, %v), want V 1 F Inh materialised", conn.V, conn.F, ok)
	}
	if _, ok := localCell(t, w, 2, sheet.ShapeCell("LocPinY")); ok {
		t.Error("LocPinY was materialised, want it left to the master (Height)")
	}
	snap := filepath.Join(t.TempDir(), "snap.vsdx")
	if err := w.Tx().Snapshot(snap); err != nil {
		t.Fatalf("Snapshot: %v", err)
	}
	if err := laws.SameParts(src, snap, "/visio/pages/page1.xml"); err != nil {
		t.Errorf("parts other than the page part changed: %v", err)
	}
}

// TestRecomputeShapeGuard checks the RC1 row: a shape whose dependent
// is GUARD(Width*0.5) rewrites nothing and reports every dependent
// stale, including one in the closed grammar.
func TestRecomputeShapeGuard(t *testing.T) {
	w := mustBegin(t, vsdxtest.Fixture(t, "v1-minimal"))
	if err := w.SetCells("page0/s1", []Cell{
		{N: "LocPinX", V: "0.5", F: "GUARD(Width*0.5)"},
		{N: "Angle", V: "0.5", F: "Width*0.5"},
		{N: "Width", V: "2"},
	}); err != nil {
		t.Fatalf("SetCells: %v", err)
	}
	stale, err := w.RecomputeShape("page0/s1", []string{"Width"})
	if err != nil {
		t.Fatalf("RecomputeShape: %v", err)
	}
	want := []string{"page0/s1 LocPinX", "page0/s1 Angle"}
	if !slices.Equal(stale, want) {
		t.Errorf("stale = %v, want %v (every dependent)", stale, want)
	}
	if err := w.Verify(); err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if c, _ := localCell(t, w, 1, sheet.ShapeCell("LocPinX")); c.V != "0.5" {
		t.Errorf("LocPinX V = %q, want 0.5 (nothing rewritten)", c.V)
	}
	if c, _ := localCell(t, w, 1, sheet.ShapeCell("Angle")); c.V != "0.5" {
		t.Errorf("Angle V = %q, want 0.5 (nothing rewritten)", c.V)
	}
}

// TestRecomputeShapeOtherShape checks the RC1 row: a dependent
// referencing Sheet.5!Width (another shape) is outside the grammar,
// so nothing is rewritten and stale lists every dependent.
func TestRecomputeShapeOtherShape(t *testing.T) {
	w := mustBegin(t, vsdxtest.Fixture(t, "v1-minimal"))
	if err := w.SetCells("page0/s1", []Cell{
		{N: "PinX", V: "2", F: "Sheet.5!Width"},
		{N: "Width", V: "2"},
	}); err != nil {
		t.Fatalf("SetCells: %v", err)
	}
	stale, err := w.RecomputeShape("page0/s1", []string{"Width"})
	if err != nil {
		t.Fatalf("RecomputeShape: %v", err)
	}
	want := []string{"page0/s1 PinX", "page0/s1 LocPinX"}
	if !slices.Equal(stale, want) {
		t.Errorf("stale = %v, want %v (every dependent)", stale, want)
	}
	if err := w.Verify(); err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if c, _ := localCell(t, w, 1, sheet.ShapeCell("LocPinX")); c.V != "0.5" {
		t.Errorf("LocPinX V = %q, want 0.5 (nothing rewritten)", c.V)
	}
}

// TestRecomputeShapeGroupChild checks that a group child inherits
// its master's formulas through its group's Master: V2 s4
// (MasterShape 6 of master 3, Master carried by its group s3) with
// Width set to 2 collects its master LocPinX (Width*0.5) and its
// PinX (Sheet.5!Width*0.25, another shape), and so rewrites nothing
// and reports both stale.
func TestRecomputeShapeGroupChild(t *testing.T) {
	w := mustBegin(t, vsdxtest.Fixture(t, "v2-master"))
	if err := w.SetCells("page0/s4", []Cell{{N: "Width", V: "2"}}); err != nil {
		t.Fatalf("SetCells: %v", err)
	}
	stale, err := w.RecomputeShape("page0/s4", []string{"Width"})
	if err != nil {
		t.Fatalf("RecomputeShape: %v", err)
	}
	want := []string{"page0/s4 PinX", "page0/s4 LocPinX"}
	if !slices.Equal(stale, want) {
		t.Errorf("stale = %v, want %v", stale, want)
	}
	if _, ok := localCell(t, w, 4, sheet.ShapeCell("LocPinX")); ok {
		t.Error("LocPinX was materialised, want nothing rewritten")
	}
}

// TestRecomputeShapeTransitive checks that a dependent of a dependent
// is rewritten in dependency order and keeps its local formula: with
// Width 2, LocPinX (Width*0.5) becomes 1 and Angle (LocPinX*2)
// becomes 2.
func TestRecomputeShapeTransitive(t *testing.T) {
	w := mustBegin(t, vsdxtest.Fixture(t, "v1-minimal"))
	if err := w.SetCells("page0/s1", []Cell{
		{N: "Angle", V: "0", F: "LocPinX*2"},
		{N: "Width", V: "2"},
	}); err != nil {
		t.Fatalf("SetCells: %v", err)
	}
	stale, err := w.RecomputeShape("page0/s1", []string{"Width"})
	if err != nil {
		t.Fatalf("RecomputeShape: %v", err)
	}
	if len(stale) != 0 {
		t.Errorf("stale = %v, want empty", stale)
	}
	if c, _ := localCell(t, w, 1, sheet.ShapeCell("LocPinX")); c.V != "1" || c.F != "Width*0.5" {
		t.Errorf("LocPinX = (V %q, F %q), want (1, Width*0.5)", c.V, c.F)
	}
	if c, _ := localCell(t, w, 1, sheet.ShapeCell("Angle")); c.V != "2" || c.F != "LocPinX*2" {
		t.Errorf("Angle = (V %q, F %q), want (2, LocPinX*2)", c.V, c.F)
	}
	if err := w.Verify(); err != nil {
		t.Fatalf("Verify: %v", err)
	}
}

// TestRecomputeShapeUnknownValue checks that a dependent naming a
// cell with no numeric value (FillForegnd "#4472c4") cannot be
// evaluated: it is stale and nothing is rewritten.
func TestRecomputeShapeUnknownValue(t *testing.T) {
	w := mustBegin(t, vsdxtest.Fixture(t, "v1-minimal"))
	if err := w.SetCells("page0/s1", []Cell{
		{N: "Angle", V: "0", F: "FillForegnd*2"},
	}); err != nil {
		t.Fatalf("SetCells: %v", err)
	}
	stale, err := w.RecomputeShape("page0/s1", []string{"FillForegnd"})
	if err != nil {
		t.Fatalf("RecomputeShape: %v", err)
	}
	if want := []string{"page0/s1 Angle"}; !slices.Equal(stale, want) {
		t.Errorf("stale = %v, want %v", stale, want)
	}
	if c, _ := localCell(t, w, 1, sheet.ShapeCell("Angle")); c.V != "0" {
		t.Errorf("Angle V = %q, want 0 (nothing rewritten)", c.V)
	}
}

// TestRecomputeShapeCycle checks that two dependents referencing each
// other (each also naming the changed Width) are stale and nothing is
// rewritten.
func TestRecomputeShapeCycle(t *testing.T) {
	w := mustBegin(t, vsdxtest.Fixture(t, "v1-minimal"))
	if err := w.SetCells("page0/s1", []Cell{
		{N: "LocPinX", V: "0.5", F: "LocPinY+Width"},
		{N: "LocPinY", V: "0.25", F: "LocPinX+Width"},
		{N: "Width", V: "2"},
	}); err != nil {
		t.Fatalf("SetCells: %v", err)
	}
	stale, err := w.RecomputeShape("page0/s1", []string{"Width"})
	if err != nil {
		t.Fatalf("RecomputeShape: %v", err)
	}
	want := []string{"page0/s1 LocPinX", "page0/s1 LocPinY"}
	if !slices.Equal(stale, want) {
		t.Errorf("stale = %v, want %v", stale, want)
	}
	if c, _ := localCell(t, w, 1, sheet.ShapeCell("LocPinX")); c.V != "0.5" {
		t.Errorf("LocPinX V = %q, want 0.5 (nothing rewritten)", c.V)
	}
}

// TestRecomputeShapeNoDependents checks that a changed cell without
// dependents and an empty changed list change nothing.
func TestRecomputeShapeNoDependents(t *testing.T) {
	src := vsdxtest.Fixture(t, "v1-minimal")
	w := mustBegin(t, src)
	if err := w.SetCells("page0/s1", []Cell{{N: "PinX", V: "3"}}); err != nil {
		t.Fatalf("SetCells: %v", err)
	}
	before := filepath.Join(t.TempDir(), "before.vsdx")
	if err := w.Tx().Snapshot(before); err != nil {
		t.Fatalf("Snapshot: %v", err)
	}
	stale, err := w.RecomputeShape("page0/s1", []string{"PinX"})
	if err != nil {
		t.Fatalf("RecomputeShape: %v", err)
	}
	if len(stale) != 0 {
		t.Errorf("stale = %v, want empty", stale)
	}
	stale, err = w.RecomputeShape("page0/s1", nil)
	if err != nil || len(stale) != 0 {
		t.Errorf("RecomputeShape with no changed cells = %v, %v; want empty, nil", stale, err)
	}
	after := filepath.Join(t.TempDir(), "after.vsdx")
	if err := w.Tx().Snapshot(after); err != nil {
		t.Fatalf("Snapshot: %v", err)
	}
	if err := laws.SameParts(before, after, ""); err != nil {
		t.Errorf("a recompute without dependents changed the package: %v", err)
	}
	if err := w.Verify(); err != nil {
		t.Fatalf("Verify: %v", err)
	}
}

// TestRecomputeParseRef checks that recomputeParseRef inverts
// recomputeRefName on the spellings formulas use, and refuses a
// qualified reference.
func TestRecomputeParseRef(t *testing.T) {
	keys := []sheet.Key{
		sheet.ShapeCell("Width"),
		sheet.IndexedRowCell(vsd.SectionGeometry, 0, 2, "X"),
		sheet.Key{Section: vsd.SectionGeometry, SectionIX: 0, RowIX: -1, Cell: "NoFill"},
		sheet.IndexedRowCell(vsd.SectionConnection, -1, 0, "X"),
		sheet.NamedRowCell(vsd.SectionProperty, "Cost", "Value"),
		sheet.IndexedRowCell(vsd.SectionCharacter, -1, 0, "Size"),
		sheet.IndexedRowCell(vsd.SectionLayer, -1, 0, "Name"),
	}
	for _, k := range keys {
		got, ok := recomputeParseRef(recomputeRefName(k))
		if !ok || got != k {
			t.Errorf("recomputeParseRef(%q) = (%+v, %v), want (%+v, true)", recomputeRefName(k), got, ok, k)
		}
	}
	if _, ok := recomputeParseRef("Sheet.5!Width"); ok {
		t.Error("recomputeParseRef(Sheet.5!Width) = true, want false")
	}
}

// TestRecomputeShapeErrors checks the refusals: a missing shape is
// ErrNotFound, and a master shape, a page address, a bad address and
// an empty changed name are errors.
func TestRecomputeShapeErrors(t *testing.T) {
	w := mustBegin(t, vsdxtest.Fixture(t, "v1-minimal"))
	if _, err := w.RecomputeShape("page0/s99", []string{"Width"}); !errors.Is(err, ErrNotFound) {
		t.Errorf("RecomputeShape(page0/s99) error = %v, want ErrNotFound", err)
	}
	for _, shape := range []string{"master2/s5", "page0", "bogus", "page0/s1"} {
		changed := []string{"Width"}
		if shape == "page0/s1" {
			changed = []string{""}
		}
		if _, err := w.RecomputeShape(shape, changed); err == nil {
			t.Errorf("RecomputeShape(%q, %v): want an error, got nil", shape, changed)
		}
	}
}

// pageWithDependents builds V1 with its page part replaced by one
// shape holding Width 1 and n shape-level dependents C<i> (F
// Width*1, V 0), and returns the drawing path.
func pageWithDependents(t *testing.T, n int) string {
	t.Helper()
	raw, err := laws.Parts(vsdxtest.Fixture(t, "v1-minimal"))
	if err != nil {
		t.Fatal(err)
	}
	parts := map[string][]byte{}
	for name, b := range raw {
		parts[strings.TrimPrefix(name, "/")] = b
	}
	var b strings.Builder
	b.WriteString("<?xml version='1.0' encoding='utf-8' ?>\n<PageContents xmlns='" + vsd.NSMain2012 +
		"' xmlns:r='" + vsd.NSRel + "' xml:space='preserve'><Shapes><Shape ID='1' Type='Shape'>")
	b.WriteString("<Cell N='Width' V='1'/>")
	for i := 1; i <= n; i++ {
		fmt.Fprintf(&b, "<Cell N='C%d' V='0' F='Width*1'/>", i)
	}
	b.WriteString("</Shape></Shapes></PageContents>")
	parts["visio/pages/page1.xml"] = []byte(b.String())
	names := make([]string, 0, len(parts))
	for name := range parts {
		if name != "[Content_Types].xml" {
			names = append(names, name)
		}
	}
	slices.Sort(names)
	members := []opc.ZipMember{{Name: "[Content_Types].xml", Body: string(parts["[Content_Types].xml"])}}
	for _, name := range names {
		members = append(members, opc.ZipMember{Name: name, Body: string(parts[name])})
	}
	z, err := opc.BuildZip(members...)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), fmt.Sprintf("dependents-%d.vsdx", n))
	if err := os.WriteFile(path, z, 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

// TestRecomputeShapeScaling checks the RC1 scaling row: RecomputeShape
// on a shape with n and 4n dependents (n = laws.Small and laws.Large)
// allocates at most 5.5 times as much (laws.Scales) and the
// laws.TimeScales time ratio is at most 8. Each dependent is a
// shape-level cell F Width*1 on one shape; Width is set to 2 first.
func TestRecomputeShapeScaling(t *testing.T) {
	setup := func(n int) (func() error, error) {
		path := pageWithDependents(t, n)
		return func() error {
			w, err := Begin(path)
			if err != nil {
				return err
			}
			defer w.Tx().Rollback()
			if err := w.SetCells("page0/s1", []Cell{{N: "Width", V: "2"}}); err != nil {
				return err
			}
			stale, err := w.RecomputeShape("page0/s1", []string{"Width"})
			if err != nil {
				return err
			}
			if len(stale) != 0 {
				return fmt.Errorf("stale = %v, want empty", stale)
			}
			if err := w.Verify(); err != nil {
				return err
			}
			d, err := w.Core().Tree("/visio/pages/page1.xml")
			if err != nil {
				return err
			}
			s1 := findShape(d.Root(), 1, 1)
			if s1 == nil {
				return errors.New("no s1 in the working page")
			}
			count := 0
			for _, e := range s1.Elements() {
				if e.Local() != "Cell" {
					continue
				}
				name, _ := e.Attr("N")
				v, _ := e.Attr("V")
				if strings.HasPrefix(name, "C") && v == "2" {
					count++
				}
			}
			if count != n {
				return fmt.Errorf("%d of %d dependents rewritten to 2", count, n)
			}
			return nil
		}, nil
	}
	laws.Scales(t, "vsdx RecomputeShape dependents", setup)
	laws.TimeScales(t, "vsdx RecomputeShape dependents", setup)
}
