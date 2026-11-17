package inherit

import (
	"fmt"
	"strconv"
	"testing"

	"github.com/jhnoel/documentdriver/internal/laws"
	"github.com/jhnoel/documentdriver/internal/ooxml/tree"
	"github.com/jhnoel/documentdriver/internal/vsdx/doc"
	"github.com/jhnoel/documentdriver/internal/vsdx/sheet"
	"github.com/jhnoel/documentdriver/internal/vsdx/vsdxtest"
)

// openFixture opens the vsdx fixture name and returns its Document.
func openFixture(t *testing.T, name string) *doc.Document {
	t.Helper()
	d, err := doc.Open(vsdxtest.OpenPackage(t, name))
	if err != nil {
		t.Fatalf("Open(%s): %v", name, err)
	}
	return d
}

// shapeFrame is one shape of a page waiting to be collected, with the
// Master its instance group carries when the shape stores none.
type shapeFrame struct {
	node      *tree.Node
	master    uint32
	hasMaster bool
}

// pageShapes returns the shapes of page 0 of d by stored ID, built as
// M1 will build them: each shape indexed once, MasterShape and style
// attributes as stored, and a sub-shape carrying its instance group's
// Master when it stores none. The first shape of a repeated ID wins.
func pageShapes(t *testing.T, d *doc.Document) map[uint32]*Shape {
	t.Helper()
	e, ok := d.Page(0)
	if !ok {
		t.Fatalf("Page(0) not found, Pages = %v", d.Pages)
	}
	contents, err := d.PageContents(e)
	if err != nil {
		t.Fatalf("PageContents(page0): %v", err)
	}
	shapes := shapesChild(contents.Root())
	if shapes == nil {
		t.Fatal("page0 has no Shapes")
	}
	out := map[uint32]*Shape{}
	stack := pushShapeFrames(nil, shapes, 0, false)
	for len(stack) > 0 {
		frame := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		sh, id, hasID := shapeOfNode(frame.node, frame.master, frame.hasMaster)
		if hasID {
			if _, dup := out[id]; !dup {
				out[id] = sh
			}
		}
		if nested := shapesChild(frame.node); nested != nil {
			master, hasMaster := frame.master, frame.hasMaster
			if sh.HasMaster {
				master, hasMaster = sh.Master, true
			}
			stack = pushShapeFrames(stack, nested, master, hasMaster)
		}
	}
	return out
}

// pushShapeFrames appends the Shape children of shapes to stack in
// reverse document order, so popping yields them in document order.
func pushShapeFrames(stack []shapeFrame, shapes *tree.Node, master uint32, hasMaster bool) []shapeFrame {
	for n := shapes.LastChild(); n != nil; n = n.PrevSibling() {
		if n.Kind() == tree.ElementNode && n.Local() == "Shape" {
			stack = append(stack, shapeFrame{node: n, master: master, hasMaster: hasMaster})
		}
	}
	return stack
}

// shapeOfNode builds the Shape of one page shape element, inheriting
// master and hasMaster when the element stores no Master of its own.
func shapeOfNode(node *tree.Node, master uint32, hasMaster bool) (*Shape, uint32, bool) {
	sh := &Shape{Master: master, HasMaster: hasMaster}
	sh.Sheet, _ = sheet.Index(node)
	var id uint32
	hasID := false
	for name, value := range node.AttrsSeq() {
		switch name {
		case "ID":
			if u, err := strconv.ParseUint(value, 10, 32); err == nil {
				id, hasID = uint32(u), true
			}
		case "Master":
			if u, err := strconv.ParseUint(value, 10, 32); err == nil {
				sh.Master, sh.HasMaster = uint32(u), true
			}
		case "MasterShape":
			if u, err := strconv.ParseUint(value, 10, 32); err == nil {
				sh.MasterShape, sh.HasMShape = uint32(u), true
			}
		case "LineStyle":
			if u, err := strconv.ParseUint(value, 10, 32); err == nil {
				sh.Styles[0], sh.HasStyle[0] = uint32(u), true
			}
		case "FillStyle":
			if u, err := strconv.ParseUint(value, 10, 32); err == nil {
				sh.Styles[1], sh.HasStyle[1] = uint32(u), true
			}
		case "TextStyle":
			if u, err := strconv.ParseUint(value, 10, 32); err == nil {
				sh.Styles[2], sh.HasStyle[2] = uint32(u), true
			}
		}
	}
	return sh, id, hasID
}

// shapeID returns the stored ID of a shape built from a tree node.
func shapeID(t *testing.T, sh *Shape) uint32 {
	t.Helper()
	if sh == nil || sh.Sheet == nil || sh.Sheet.Node == nil {
		t.Fatalf("shape %v has no node", sh)
	}
	v, ok := sh.Sheet.Node.Attr("ID")
	if !ok {
		t.Fatalf("shape node has no ID")
	}
	u, err := strconv.ParseUint(v, 10, 32)
	if err != nil {
		t.Fatalf("shape ID %q does not parse: %v", v, err)
	}
	return uint32(u)
}

// TestCellV2 checks the V2 rows: effective cells of the page shapes of
// v2-master, through their masters 2 and 3.
func TestCellV2(t *testing.T) {
	r := New(openFixture(t, "v2-master"))
	shapes := pageShapes(t, r.Doc())
	tests := []struct {
		name   string
		shape  uint32
		key    sheet.Key
		wantV  string
		origin Origin
	}{
		{"s1 Width", 1, sheet.ShapeCell("Width"), "1", Master},
		{"s1 PinX", 1, sheet.ShapeCell("PinX"), "2", Local},
		{"s2 Width", 2, sheet.ShapeCell("Width"), "1", Inherited},
		{"s2 FillForegnd", 2, sheet.ShapeCell("FillForegnd"), "2", Local},
		{"s1 FillForegnd", 1, sheet.ShapeCell("FillForegnd"), "#FFFFFF", Master},
		{"s4 PinX", 4, sheet.ShapeCell("PinX"), "0.5", Master},
		{"s4 Width", 4, sheet.ShapeCell("Width"), "1", Master},
		{"s5 FillForegnd", 5, sheet.ShapeCell("FillForegnd"), "4", Master},
	}
	for _, tc := range tests {
		s, ok := shapes[tc.shape]
		if !ok {
			t.Fatalf("%s: shape %d not found", tc.name, tc.shape)
		}
		cell, origin, ok := r.Cell(s, tc.key)
		if !ok {
			t.Errorf("%s: absent, want (%q, %s)", tc.name, tc.wantV, tc.origin)
			continue
		}
		if cell.V != tc.wantV || origin != tc.origin {
			t.Errorf("%s = (%q, %s), want (%q, %s)", tc.name, cell.V, origin, tc.wantV, tc.origin)
		}
	}
	if _, _, ok := r.Cell(shapes[1], sheet.ShapeCell("Bogus")); ok {
		t.Error("Bogus cell present, want absent (nothing stores it and no style lands in H2)")
	}
}

// TestMasterShapeV2 checks the V2 rows: MasterShape of s1 is master2
// shape 5 (the top shape), of s4 is master3 shape 6, of s3 is master3
// shape 5.
func TestMasterShapeV2(t *testing.T) {
	r := New(openFixture(t, "v2-master"))
	shapes := pageShapes(t, r.Doc())
	tests := []struct {
		shape uint32
		want  uint32
	}{
		{1, 5},
		{4, 6},
		{3, 5},
	}
	for _, tc := range tests {
		ms, ok := r.MasterShape(shapes[tc.shape])
		if !ok {
			t.Errorf("MasterShape(s%d) absent, want shape %d", tc.shape, tc.want)
			continue
		}
		if got := shapeID(t, ms); got != tc.want {
			t.Errorf("MasterShape(s%d) is shape %d, want %d", tc.shape, got, tc.want)
		}
	}
	if _, ok := r.MasterShape(nil); ok {
		t.Error("MasterShape(nil) present, want absent")
	}
	if _, ok := r.MasterShape(&Shape{}); ok {
		t.Error("MasterShape of a shape with no master present, want absent")
	}
}

// TestCellV10Master99 checks the V10 row: a shape of Master 99 reads
// its own cells only, every other cell is absent, and however many
// shapes name master 99 there is one note "master99: no such master".
func TestCellV10Master99(t *testing.T) {
	r := New(openFixture(t, "v10-defects"))
	shapes := pageShapes(t, r.Doc())
	s8, ok := shapes[8]
	if !ok {
		t.Fatal("shape 8 not found")
	}
	if cell, origin, ok := r.Cell(s8, sheet.ShapeCell("PinX")); !ok || cell.V != "6" || origin != Local {
		t.Errorf("s8 PinX = (%q, %s, %v), want (\"6\", local, true)", cell.V, origin, ok)
	}
	for _, name := range []string{"Width", "Height", "FillForegnd", "LineWeight"} {
		if _, _, ok := r.Cell(s8, sheet.ShapeCell(name)); ok {
			t.Errorf("s8 %s present, want absent beyond its own cells", name)
		}
	}
	other := &Shape{Sheet: s8.Sheet, Master: 99, HasMaster: true}
	if _, _, ok := r.Cell(other, sheet.ShapeCell("Width")); ok {
		t.Error("second Master 99 shape Width present, want absent")
	}
	if _, ok := r.MasterShape(s8); ok {
		t.Error("MasterShape(s8) present, want absent")
	}
	notes := r.Notes()
	if len(notes) != 1 {
		t.Fatalf("Notes = %v, want one note", notes)
	}
	if notes[0].At != "master99" || notes[0].Reason != "no such master" {
		t.Errorf("note = %q: %q, want \"master99: no such master\"", notes[0].At, notes[0].Reason)
	}
}

// TestCellV6Deleted checks the V6 row: a master row the instance
// deletes with Del="1" is absent, while the instance's other master
// rows still resolve.
func TestCellV6Deleted(t *testing.T) {
	r := New(openFixture(t, "v6-data"))
	shapes := pageShapes(t, r.Doc())
	s1, ok := shapes[1]
	if !ok {
		t.Fatal("shape 1 not found")
	}
	if _, _, ok := r.Cell(s1, sheet.NamedRowCell("Property", "Gone", "Value")); ok {
		t.Error("Gone Value present, want absent (the instance deletes the row)")
	}
	if cell, origin, ok := r.Cell(s1, sheet.NamedRowCell("Property", "Due", "Value")); !ok || cell.V != "2024-03-01T00:00:00" || origin != Master {
		t.Errorf("Due Value = (%q, %s, %v), want (\"2024-03-01T00:00:00\", master, true)", cell.V, origin, ok)
	}
	if cell, origin, ok := r.Cell(s1, sheet.NamedRowCell("Property", "Cost", "Value")); !ok || cell.V != "12.5" || origin != Local {
		t.Errorf("Cost Value = (%q, %s, %v), want (\"12.5\", local, true)", cell.V, origin, ok)
	}
}

// TestCellEdgeCases checks the resolver on shapes with no sheet and no
// master: a master cell still resolves for a sheet-less shape, and
// nothing at all resolves for an empty shape.
func TestCellEdgeCases(t *testing.T) {
	r := New(openFixture(t, "v2-master"))
	if cell, origin, ok := r.Cell(&Shape{Master: 2, HasMaster: true}, sheet.ShapeCell("Width")); !ok || cell.V != "1" || origin != Master {
		t.Errorf("sheet-less Width = (%q, %s, %v), want (\"1\", master, true)", cell.V, origin, ok)
	}
	if _, _, ok := r.Cell(nil, sheet.ShapeCell("Width")); ok {
		t.Error("Cell(nil, Width) present, want absent")
	}
	if _, _, ok := r.Cell(&Shape{}, sheet.ShapeCell("Width")); ok {
		t.Error("Cell of an empty shape present, want absent")
	}
	if len(r.Notes()) != 0 {
		t.Errorf("Notes = %v, want none", r.Notes())
	}
}

// TestCellAllocations checks that a Cell lookup after the master is
// indexed allocates almost nothing: the master is indexed once, never
// re-indexed per lookup.
func TestCellAllocations(t *testing.T) {
	r := New(openFixture(t, "v2-master"))
	shapes := pageShapes(t, r.Doc())
	s1 := shapes[1]
	if _, _, ok := r.Cell(s1, sheet.ShapeCell("Width")); !ok {
		t.Fatal("s1 Width absent")
	}
	if got := testing.AllocsPerRun(100, func() {
		if _, _, ok := r.Cell(s1, sheet.ShapeCell("Width")); !ok {
			t.Error("s1 Width absent")
		}
	}); got > 2 {
		t.Errorf("Cell allocates %v per run after the master is indexed, want at most 2", got)
	}
}

// TestScalingCell checks the scaling row: resolving one master cell
// for N and 4N shapes (N = 1000) allocates at most 5.5 times as much
// for 4 times the shapes, linear in the number of shapes.
func TestScalingCell(t *testing.T) {
	setup := func(n int) (func() error, error) {
		r := New(openFixture(t, "v2-master"))
		parsed, err := tree.ParseString(`<Shape ID='1'><Cell N='PinX' V='2'/></Shape>`)
		if err != nil {
			return nil, fmt.Errorf("ParseString: %w", err)
		}
		sh, _ := sheet.Index(parsed.Root())
		shapes := make([]*Shape, n)
		for i := range shapes {
			shapes[i] = &Shape{Sheet: sh, Master: 2, HasMaster: true}
		}
		return func() error {
			for _, s := range shapes {
				cell, origin, ok := r.Cell(s, sheet.ShapeCell("Width"))
				if !ok || cell.V != "1" || origin != Master {
					return fmt.Errorf("Width = (%q, %s, %v), want (\"1\", master, true)", cell.V, origin, ok)
				}
			}
			return nil
		}, nil
	}
	laws.Scales(t, "vsdx inherit Cell", setup)
}
