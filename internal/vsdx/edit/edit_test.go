package edit

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jhnoel/documentdriver/internal/laws"
	"github.com/jhnoel/documentdriver/internal/ooxml/tree"
	"github.com/jhnoel/documentdriver/internal/plan"
	"github.com/jhnoel/documentdriver/internal/repo"
	"github.com/jhnoel/documentdriver/internal/vsdx/create"
	"github.com/jhnoel/documentdriver/internal/vsdx/vsd"
	"github.com/jhnoel/documentdriver/internal/vsdx/vsdxtest"
)

// blankPath writes the blank drawing (create.Template) as a file in
// t.TempDir and returns its path: the blank of the E1 rows, with one
// empty page0.
func blankPath(t *testing.T) string {
	t.Helper()
	data, err := create.Template(time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("create.Template: %v", err)
	}
	path := filepath.Join(t.TempDir(), "blank.vsdx")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatalf("write blank: %v", err)
	}
	return path
}

// goldenEmit returns the bytes of the designer-authored emit golden
// testdata/vsdx/create/emit/<name>.
func goldenEmit(t *testing.T, name string) []byte {
	t.Helper()
	root, err := repo.Root()
	if err != nil {
		t.Fatalf("repo root: %v", err)
	}
	body, err := os.ReadFile(filepath.Join(root, "testdata", "vsdx", "create", "emit", name))
	if err != nil {
		t.Fatalf("read emit golden %s: %v", name, err)
	}
	return body
}

// connectFrags splits the golden connects.txt into its Connect
// fragments, one per line.
func connectFrags(t *testing.T) [][]byte {
	t.Helper()
	var frags [][]byte
	for _, line := range strings.Split(string(goldenEmit(t, "connects.txt")), "\n") {
		if strings.TrimSpace(line) != "" {
			frags = append(frags, []byte(strings.TrimSpace(line)))
		}
	}
	if len(frags) == 0 {
		t.Fatal("golden connects.txt holds no Connect")
	}
	return frags
}

// pageRoot returns the root of the working page part.
func pageRoot(t *testing.T, w *Workspace, part string) *tree.Node {
	t.Helper()
	d, err := w.Core().Tree(part)
	if err != nil {
		t.Fatalf("Tree(%s): %v", part, err)
	}
	return d.Root()
}

// mustBegin begins a workspace on path, failing the test on error,
// and rolls it back at test end.
func mustBegin(t *testing.T, path string) *Workspace {
	t.Helper()
	w, err := Begin(path)
	if err != nil {
		t.Fatalf("Begin(%s): %v", path, err)
	}
	t.Cleanup(func() { w.Tx().Rollback() })
	return w
}

// TestNextShapeID checks the E1 rows: next free shape ID on V2 page0
// is 6 (nested shapes included), and on the blank it is 1.
func TestNextShapeID(t *testing.T) {
	w := mustBegin(t, vsdxtest.Fixture(t, "v2-master"))
	id, err := w.NextShapeID("page0")
	if err != nil {
		t.Fatalf("NextShapeID(page0): %v", err)
	}
	if id != 6 {
		t.Errorf("NextShapeID on V2 page0 = %d, want 6", id)
	}
	w = mustBegin(t, blankPath(t))
	id, err = w.NextShapeID("page0")
	if err != nil {
		t.Fatalf("NextShapeID(page0): %v", err)
	}
	if id != 1 {
		t.Errorf("NextShapeID on the blank page0 = %d, want 1", id)
	}
}

// TestInsertShapeOnBlank checks the E1 row: InsertShape(page0, "",
// rect.xml) on the blank returns page0/s1, Verify passes, the page
// part changed and every other part is byte-equal.
func TestInsertShapeOnBlank(t *testing.T) {
	src := blankPath(t)
	rect := goldenEmit(t, "rect.xml")
	w := mustBegin(t, src)
	addr, err := w.InsertShape("page0", "", rect)
	if err != nil {
		t.Fatalf("InsertShape: %v", err)
	}
	if addr != "page0/s1" {
		t.Errorf("InsertShape returned %q, want page0/s1", addr)
	}
	if err := w.Verify(); err != nil {
		t.Fatalf("Verify: %v", err)
	}
	snap := filepath.Join(t.TempDir(), "snap.vsdx")
	if err := w.Tx().Snapshot(snap); err != nil {
		t.Fatalf("Snapshot: %v", err)
	}
	if err := laws.SameParts(src, snap, "/visio/pages/page1.xml"); err != nil {
		t.Errorf("parts other than the page part changed: %v", err)
	}
	before, err := laws.Parts(src)
	if err != nil {
		t.Fatalf("Parts(src): %v", err)
	}
	after, err := laws.Parts(snap)
	if err != nil {
		t.Fatalf("Parts(snap): %v", err)
	}
	if bytes.Equal(before["/visio/pages/page1.xml"], after["/visio/pages/page1.xml"]) {
		t.Error("the page part did not change")
	}
	s1 := findShape(pageRoot(t, w, "/visio/pages/page1.xml"), 1, 1)
	if s1 == nil {
		t.Fatal("no s1 in the working page")
	}
	if !bytes.Equal(s1.Bytes(), rect) {
		t.Errorf("inserted shape bytes = %.80q, want rect.xml %.80q", s1.Bytes(), rect)
	}
}

// TestInsertShapeIntoGroup checks the E1 row: InsertShape into V2's
// group s3 returns page0/s6 (the next ID is 6) with parent s3.
func TestInsertShapeIntoGroup(t *testing.T) {
	w := mustBegin(t, vsdxtest.Fixture(t, "v2-master"))
	next, err := w.NextShapeID("page0")
	if err != nil {
		t.Fatalf("NextShapeID: %v", err)
	}
	if next != 6 {
		t.Fatalf("NextShapeID = %d, want 6", next)
	}
	addr, err := w.InsertShape("page0", "page0/s3", goldenEmit(t, "rect.xml"))
	if err != nil {
		t.Fatalf("InsertShape: %v", err)
	}
	if addr != "page0/s6" {
		t.Errorf("InsertShape returned %q, want page0/s6", addr)
	}
	if err := w.Verify(); err != nil {
		t.Fatalf("Verify: %v", err)
	}
	root := pageRoot(t, w, "/visio/pages/page1.xml")
	s6 := findShape(root, 6, 1)
	if s6 == nil {
		t.Fatal("no s6 after insert")
	}
	// The parent is s3: s6's Shapes parent is the group shape with ID 3.
	if shapes := s6.Parent(); shapes == nil || shapes.Local() != "Shapes" {
		t.Fatalf("s6 parent = %v, want Shapes", shapes)
	} else if group := shapes.Parent(); group == nil || group.Local() != "Shape" {
		t.Fatalf("s6 grandparent = %v, want the group Shape", group)
	} else if id, _ := group.Attr("ID"); id != "3" {
		t.Errorf("s6 parent shape ID = %q, want 3 (s3)", id)
	}
	want := bytes.Replace(goldenEmit(t, "rect.xml"), []byte("ID='1'"), []byte("ID='6'"), 1)
	if !bytes.Equal(s6.Bytes(), want) {
		t.Errorf("inserted group child bytes = %.80q, want rect.xml with ID='6' %.80q", s6.Bytes(), want)
	}
}

// TestInsertConnectsCreatesConnects checks the E1 row: InsertConnects
// on the blank creates <Connects> after <Shapes>.
func TestInsertConnectsCreatesConnects(t *testing.T) {
	w := mustBegin(t, blankPath(t))
	if err := w.InsertConnects("page0", connectFrags(t)); err != nil {
		t.Fatalf("InsertConnects: %v", err)
	}
	if err := w.Verify(); err != nil {
		t.Fatalf("Verify: %v", err)
	}
	var order []string
	for _, e := range pageRoot(t, w, "/visio/pages/page1.xml").Elements() {
		order = append(order, e.Local())
	}
	want := []string{"Shapes", "Connects"}
	if len(order) != len(want) || order[0] != want[0] || order[1] != want[1] {
		t.Errorf("page root children = %v, want %v (Connects after Shapes)", order, want)
	}
	got := string(child(pageRoot(t, w, "/visio/pages/page1.xml"), "Connects").Bytes())
	for _, frag := range connectFrags(t) {
		if !strings.Contains(got, string(frag)) {
			t.Errorf("Connects %q does not hold %q", got, frag)
		}
	}
}

// TestSetTextReplacesExisting checks the E1 row: SetText on a shape
// with an existing Text replaces it and its Character section (and
// keeps its Geometry section).
func TestSetTextReplacesExisting(t *testing.T) {
	sections := []byte("<Section N='Character'><Row IX='0'><Cell N='Style' V='3'/></Row></Section>")
	text := []byte("<Text>Bye\n</Text>")
	w := mustBegin(t, vsdxtest.Fixture(t, "v1-minimal"))
	if err := w.SetText("page0/s1", sections, text); err != nil {
		t.Fatalf("SetText: %v", err)
	}
	if err := w.Verify(); err != nil {
		t.Fatalf("Verify: %v", err)
	}
	s1 := findShape(pageRoot(t, w, "/visio/pages/page1.xml"), 1, 1)
	if s1 == nil {
		t.Fatal("no s1")
	}
	var names []string
	for _, e := range s1.Elements() {
		if e.Local() == "Section" {
			n, _ := e.Attr("N")
			names = append(names, n)
		}
	}
	if len(names) != 2 || names[0] != "Character" || names[1] != "Geometry" {
		t.Errorf("sections = %v, want [Character Geometry] (Geometry kept)", names)
	}
	for _, e := range s1.Elements() {
		if e.Local() != "Section" {
			continue
		}
		n, _ := e.Attr("N")
		if n == "Character" && !bytes.Equal(e.Bytes(), sections) {
			t.Errorf("Character section = %q, want %q", e.Bytes(), sections)
		}
	}
	if txt := child(s1, "Text"); txt == nil {
		t.Error("no Text after SetText")
	} else if !bytes.Equal(txt.Bytes(), text) {
		t.Errorf("Text = %q, want %q", txt.Bytes(), text)
	}
}

// TestSetCellsPageSheet checks the E1 row: SetCells(page0,
// [{PrintPageOrientation, 2}]) on the blank adds the cell after
// DrawingScaleType on the page sheet.
func TestSetCellsPageSheet(t *testing.T) {
	w := mustBegin(t, blankPath(t))
	if err := w.SetCells("page0", []Cell{{N: "PrintPageOrientation", V: "2"}}); err != nil {
		t.Fatalf("SetCells: %v", err)
	}
	if err := w.Verify(); err != nil {
		t.Fatalf("Verify: %v", err)
	}
	d, err := w.Core().Tree("/visio/pages/pages.xml")
	if err != nil {
		t.Fatalf("Tree(pages.xml): %v", err)
	}
	sheet := findPageSheet(d.Root(), 0, 1)
	if sheet == nil {
		t.Fatal("page0 has no PageSheet")
	}
	var cells []string
	for _, c := range sheet.Elements() {
		if c.Local() != "Cell" {
			continue
		}
		n, _ := c.Attr("N")
		v, _ := c.Attr("V")
		cells = append(cells, n+"="+v)
	}
	want := []string{"PageWidth=8.5", "PageHeight=11", "PageScale=1", "DrawingScale=1", "DrawingSizeType=1", "DrawingScaleType=0", "PrintPageOrientation=2"}
	if len(cells) != len(want) {
		t.Fatalf("page sheet cells = %v, want %v", cells, want)
	}
	for i := range want {
		if cells[i] != want[i] {
			t.Errorf("cells[%d] = %q, want %q (all %v)", i, cells[i], want[i], cells)
		}
	}
}

// TestSetCellsShape checks shape cells: an existing cell is updated,
// an empty F removes the formula, and a new cell goes at the end of
// the Cell slot (after the last cell, before the sections).
func TestSetCellsShape(t *testing.T) {
	w := mustBegin(t, vsdxtest.Fixture(t, "v1-minimal"))
	cells := []Cell{
		{N: "PinX", V: "3"},
		{N: "LocPinX", V: "0.5"}, // drops F="Width*0.5"
		{N: "NewCell", V: "9", U: "IN", F: "2+3"},
	}
	if err := w.SetCells("page0/s1", cells); err != nil {
		t.Fatalf("SetCells: %v", err)
	}
	if err := w.Verify(); err != nil {
		t.Fatalf("Verify: %v", err)
	}
	s1 := findShape(pageRoot(t, w, "/visio/pages/page1.xml"), 1, 1)
	if pin := findCell(s1, "PinX"); pin == nil {
		t.Error("no PinX")
	} else if v, _ := pin.Attr("V"); v != "3" {
		t.Errorf("PinX = %q, want 3", v)
	}
	if loc := findCell(s1, "LocPinX"); loc == nil {
		t.Error("no LocPinX")
	} else if _, hasF := loc.Attr("F"); hasF {
		t.Errorf("LocPinX still has F=%q, want it removed", loc.Bytes())
	}
	var order []string
	for _, e := range s1.Elements() {
		n, _ := e.Attr("N")
		order = append(order, e.Local()+"/"+n)
	}
	if len(order) == 0 || order[len(order)-4] != "Cell/NewCell" {
		t.Errorf("children = %v, want NewCell after FillForegnd and before the sections", order)
	}
	// The new cell carries N, V, U and F as written.
	newCell := findCell(s1, "NewCell")
	if newCell == nil {
		t.Fatal("no NewCell")
	}
	for _, want := range [4][2]string{{"N", "NewCell"}, {"V", "9"}, {"U", "IN"}, {"F", "2+3"}} {
		if got, _ := newCell.Attr(want[0]); got != want[1] {
			t.Errorf("NewCell %s = %q, want %q", want[0], got, want[1])
		}
	}
}

// TestVerifyUndeclaredWriteFails checks the E1 row: a write outside
// the declared set fails Verify. The declared write is SetCells on
// s1; the undeclared one changes s2 of the same page part through a
// read-only (Peek) handle.
func TestVerifyUndeclaredWriteFails(t *testing.T) {
	w := mustBegin(t, vsdxtest.Fixture(t, "v2-master"))
	if err := w.SetCells("page0/s1", []Cell{{N: "PinX", V: "9"}}); err != nil {
		t.Fatalf("SetCells: %v", err)
	}
	root := pageRoot(t, w, "/visio/pages/page1.xml")
	s2 := findShape(root, 2, 1)
	if s2 == nil {
		t.Fatal("no s2")
	}
	h := w.Core().Peek("/visio/pages/page1.xml", "page0/s2", []*tree.Node{s2})
	if err := h.SetNodeAttr(findCell(s2, "PinX"), "V", "99"); err != nil {
		t.Fatalf("undeclared write: %v", err)
	}
	if err := w.Verify(); !errors.Is(err, plan.ErrUndeclared) {
		t.Errorf("Verify = %v, want plan.ErrUndeclared", err)
	}
}

// TestStepsErrors checks the refusals the rows imply: unknown pages
// and shapes, non-page and non-shape addresses, a non-group parent
// and a bad fragment.
func TestStepsErrors(t *testing.T) {
	w := mustBegin(t, vsdxtest.Fixture(t, "v1-minimal"))
	rect := goldenEmit(t, "rect.xml")
	type step struct {
		name string
		run  func() error
	}
	steps := []step{
		{name: "next id of missing page", run: func() error { _, err := w.NextShapeID("page9"); return err }},
		{name: "next id of a shape address", run: func() error { _, err := w.NextShapeID("page0/s1"); return err }},
		{name: "insert into missing page", run: func() error { _, err := w.InsertShape("page9", "", rect); return err }},
		{name: "insert into non-group", run: func() error { _, err := w.InsertShape("page0", "page0/s1", rect); return err }},
		{name: "insert unknown parent", run: func() error { _, err := w.InsertShape("page0", "page0/s99", rect); return err }},
		{name: "insert bad fragment", run: func() error { _, err := w.InsertShape("page0", "", []byte("<Connect/>")); return err }},
		{name: "connects of missing page", run: func() error { return w.InsertConnects("page9", connectFrags(t)) }},
		{name: "connects bad fragment", run: func() error { return w.InsertConnects("page0", [][]byte{[]byte("<Shape/>")}) }},
		{name: "set text of missing shape", run: func() error { return w.SetText("page0/s99", nil, []byte("<Text>x</Text>")) }},
		{name: "set text of a page", run: func() error { return w.SetText("page0", nil, nil) }},
		{name: "set cells of missing shape", run: func() error { return w.SetCells("page0/s99", []Cell{{N: "PinX", V: "1"}}) }},
		{name: "set cells of a layer", run: func() error { return w.SetCells("page0/layer:Foo", []Cell{{N: "PinX", V: "1"}}) }},
		{name: "set cells without a name", run: func() error { return w.SetCells("page0/s1", []Cell{{V: "1"}}) }},
	}
	for _, st := range steps {
		if err := st.run(); err == nil {
			t.Errorf("%s: want an error, got nil", st.name)
		}
	}
}

// TestScalingNextShapeID checks the scaling row: NextShapeID (and
// Verify) on Generated(1000) and Generated(4000) (N = 1000, 4N = 4000
// shapes) allocates at most 5.5 times as much (laws.Scales) and the
// laws.TimeScales time ratio is at most 8.
func TestScalingNextShapeID(t *testing.T) {
	setup := func(n int) (func() error, error) {
		path := vsdxtest.Generated(t, n)
		return func() error {
			w, err := Begin(path)
			if err != nil {
				return err
			}
			defer w.Tx().Rollback()
			id, err := w.NextShapeID("page0")
			if err != nil {
				return err
			}
			if id == 0 {
				return errors.New("NextShapeID returned 0")
			}
			return w.Verify()
		}, nil
	}
	laws.Scales(t, "vsdx NextShapeID", setup)
	laws.TimeScales(t, "vsdx NextShapeID", setup)
}

// TestV11Namespace checks that a part in the 2011 namespace spelling
// is edited in that spelling: an insert into V11 keeps NSCore2011.
func TestV11Namespace(t *testing.T) {
	w := mustBegin(t, vsdxtest.Fixture(t, "v11-ns2011"))
	addr, err := w.InsertShape("page0", "", goldenEmit(t, "rect.xml"))
	if err != nil {
		t.Fatalf("InsertShape: %v", err)
	}
	if addr != "page0/s2" {
		t.Errorf("InsertShape returned %q, want page0/s2", addr)
	}
	if err := w.Verify(); err != nil {
		t.Fatalf("Verify: %v", err)
	}
	s2 := findShape(pageRoot(t, w, "/visio/pages/page1.xml"), 2, 1)
	if s2 == nil {
		t.Fatal("no s2")
	}
	if s2.Space() != vsd.NSCore2011 {
		t.Errorf("inserted shape namespace = %q, want the 2011 core spelling", s2.Space())
	}
}
