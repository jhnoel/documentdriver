package sheet

import (
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/jhnoel/documentdriver/internal/laws"
	"github.com/jhnoel/documentdriver/internal/ooxml/tree"
)

// indexXML parses src as one XML document and indexes its root element.
func indexXML(t *testing.T, src string) (*Sheet, []string) {
	t.Helper()
	doc, err := tree.ParseString(src)
	if err != nil {
		t.Fatalf("ParseString: %v", err)
	}
	return Index(doc.Root())
}

// TestIndexShapeCells checks the shape-level rows: PinX with a unit, a
// Width inherited from the master, and an absent Height.
func TestIndexShapeCells(t *testing.T) {
	s, skipped := indexXML(t, `<Shape ID='1'><Cell N='PinX' V='2' U='IN'/><Cell N='Width' V='1' F='Inh'/></Shape>`)
	if len(skipped) != 0 {
		t.Fatalf("skipped = %v, want none", skipped)
	}
	pin, ok := s.Cell(ShapeCell("PinX"))
	if !ok {
		t.Fatal("PinX absent")
	}
	if pin.V != "2" || pin.U != "IN" || !pin.HasV || pin.HasF {
		t.Errorf("PinX = %+v, want V %q U %q HasV and no HasF", pin, "2", "IN")
	}
	if pin.N != "PinX" || pin.Node == nil {
		t.Errorf("PinX = %+v, want N PinX and a node", pin)
	}
	width, ok := s.Cell(ShapeCell("Width"))
	if !ok {
		t.Fatal("Width absent")
	}
	if !width.Inherited() {
		t.Errorf("Width = %+v, want Inherited", width)
	}
	if _, ok := s.Cell(ShapeCell("Height")); ok {
		t.Error("Height present, want absent")
	}
}

// TestIndexPropertyRow checks a named Property row and its Rows entry.
func TestIndexPropertyRow(t *testing.T) {
	s, skipped := indexXML(t, `<Shape ID='1'><Section N='Property'><Row N='Cost'><Cell N='Value' V='12.5'/></Row></Section></Shape>`)
	if len(skipped) != 0 {
		t.Fatalf("skipped = %v, want none", skipped)
	}
	cell, ok := s.Cell(NamedRowCell("Property", "Cost", "Value"))
	if !ok {
		t.Fatal("Property Cost Value absent")
	}
	if cell.V != "12.5" {
		t.Errorf("Property Cost Value V = %q, want %q", cell.V, "12.5")
	}
	want := []Key{NamedRowCell("Property", "Cost", "")}
	if got := s.Rows("Property"); !slices.Equal(got, want) {
		t.Errorf("Rows(Property) = %v, want %v", got, want)
	}
}

// TestIndexGeometry checks a repeatable Geometry section: one section
// cell and one indexed row cell.
func TestIndexGeometry(t *testing.T) {
	s, skipped := indexXML(t, `<Shape ID='1'><Section N='Geometry' IX='0'><Cell N='NoFill' V='0'/><Row T='MoveTo' IX='1'><Cell N='X' V='0'/></Row></Section></Shape>`)
	if len(skipped) != 0 {
		t.Fatalf("skipped = %v, want none", skipped)
	}
	cell, ok := s.Cell(IndexedRowCell("Geometry", 0, 1, "X"))
	if !ok {
		t.Fatal("Geometry row cell absent")
	}
	if cell.V != "0" {
		t.Errorf("Geometry row cell V = %q, want %q", cell.V, "0")
	}
	sectionCell, ok := s.Cell(Key{Section: "Geometry", SectionIX: 0, RowIX: -1, Cell: "NoFill"})
	if !ok {
		t.Fatal("Geometry section cell absent")
	}
	if sectionCell.V != "0" {
		t.Errorf("Geometry section cell V = %q, want %q", sectionCell.V, "0")
	}
}

// TestIndexCharacterRows checks Character rows IX 0 and 1 in stored order.
func TestIndexCharacterRows(t *testing.T) {
	s, skipped := indexXML(t, `<Shape ID='1'><Section N='Character'><Row IX='0'><Cell N='Size' V='0.1'/></Row><Row IX='1'><Cell N='Style' V='1'/></Row></Section></Shape>`)
	if len(skipped) != 0 {
		t.Fatalf("skipped = %v, want none", skipped)
	}
	want := []Key{IndexedRowCell("Character", -1, 0, ""), IndexedRowCell("Character", -1, 1, "")}
	if got := s.Rows("Character"); !slices.Equal(got, want) {
		t.Errorf("Rows(Character) = %v, want %v", got, want)
	}
	if cell, ok := s.Cell(IndexedRowCell("Character", -1, 1, "Style")); !ok || cell.V != "1" {
		t.Errorf("Character row 1 Style = %+v, %v; want V 1", cell, ok)
	}
}

// TestIndexControlRow checks a named Control row (V5 s6).
func TestIndexControlRow(t *testing.T) {
	s, skipped := indexXML(t, `<Shape ID='1'><Section N='Control'><Row N='Row_1'><Cell N='X' V='0.5'/></Row></Section></Shape>`)
	if len(skipped) != 0 {
		t.Fatalf("skipped = %v, want none", skipped)
	}
	cell, ok := s.Cell(NamedRowCell("Control", "Row_1", "X"))
	if !ok {
		t.Fatal("Control Row_1 X absent")
	}
	if cell.V != "0.5" {
		t.Errorf("Control Row_1 X V = %q, want %q", cell.V, "0.5")
	}
}

// TestIndexDeleted checks both deleted forms: a deleted Property row and
// a deleted Geometry section.
func TestIndexDeleted(t *testing.T) {
	s, skipped := indexXML(t, `<Shape ID='1'><Section N='Property'><Row N='Old' Del='1'/></Section></Shape>`)
	if len(skipped) != 0 {
		t.Fatalf("skipped = %v, want none", skipped)
	}
	if !s.Deleted(NamedRowCell("Property", "Old", "Value")) {
		t.Error("Deleted(Property Old Value) = false, want true")
	}
	g, skipped := indexXML(t, `<Shape ID='1'><Section N='Geometry' IX='1' Del='1'/></Shape>`)
	if len(skipped) != 0 {
		t.Fatalf("skipped = %v, want none", skipped)
	}
	if !g.Deleted(IndexedRowCell("Geometry", 1, 0, "X")) {
		t.Error("Deleted(Geometry IX 1 row cell) = false, want true")
	}
	if !g.Deleted(Key{Section: "Geometry", SectionIX: 1, RowIX: -1, Cell: "NoFill"}) {
		t.Error("Deleted(Geometry IX 1 section cell) = false, want true")
	}
	if g.Deleted(IndexedRowCell("Geometry", 0, 0, "X")) {
		t.Error("Deleted(Geometry IX 0) = true, want false")
	}
}

// TestIndexNestedShapes checks that a nested Shapes shape never
// contributes cells to the shape that holds it.
func TestIndexNestedShapes(t *testing.T) {
	s, skipped := indexXML(t, `<Shape ID='1'><Cell N='PinX' V='2'/><Shapes><Shape ID='2'><Cell N='PinX' V='9'/></Shape></Shapes></Shape>`)
	if len(skipped) != 0 {
		t.Fatalf("skipped = %v, want none", skipped)
	}
	cell, ok := s.Cell(ShapeCell("PinX"))
	if !ok {
		t.Fatal("PinX absent")
	}
	if cell.V != "2" {
		t.Errorf("PinX V = %q, want its own %q, never 9", cell.V, "2")
	}
}

// TestIndexSkipped checks the two skipped rows: a repeated cell (the
// first wins, one skipped entry) and a row with neither N nor IX.
func TestIndexSkipped(t *testing.T) {
	s, skipped := indexXML(t, `<Shape ID='1'><Cell N='PinX' V='2'/><Cell N='PinX' V='9'/></Shape>`)
	if cell, ok := s.Cell(ShapeCell("PinX")); !ok || cell.V != "2" {
		t.Errorf("PinX = %+v, %v; want first V 2", cell, ok)
	}
	if len(skipped) != 1 {
		t.Errorf("repeated PinX: skipped = %v, want one entry", skipped)
	}
	r, skipped := indexXML(t, `<Shape ID='1'><Section N='Property'><Row><Cell N='Value' V='1'/></Row></Section></Shape>`)
	if len(skipped) != 1 {
		t.Errorf("row without N or IX: skipped = %v, want one entry", skipped)
	}
	if _, ok := r.Cell(NamedRowCell("Property", "Value", "Value")); ok {
		t.Error("cell of skipped row indexed, want absent")
	}
}

// TestIndexAllocations checks that indexing a 20-cell shape allocates
// no more than 8 times per run.
func TestIndexAllocations(t *testing.T) {
	var b strings.Builder
	b.WriteString(`<Shape ID='1'>`)
	for i := range 20 {
		b.WriteString(`<Cell N='C`)
		b.WriteString(strconv.Itoa(i))
		b.WriteString(`' V='`)
		b.WriteString(strconv.Itoa(i))
		b.WriteString(`'/>`)
	}
	b.WriteString(`</Shape>`)
	doc, err := tree.ParseString(b.String())
	if err != nil {
		t.Fatalf("ParseString: %v", err)
	}
	root := doc.Root()
	if got := testing.AllocsPerRun(100, func() { Index(root) }); got > 8 {
		t.Errorf("Indexing a 20-cell shape allocates %v per run, want at most 8", got)
	}
}

// TestIndexNil checks that indexing a nil element returns an empty sheet
// rather than panicking.
func TestIndexNil(t *testing.T) {
	s, skipped := Index(nil)
	if s == nil {
		t.Fatal("Index(nil) returned a nil sheet")
	}
	if len(skipped) != 0 {
		t.Fatalf("skipped = %v, want none", skipped)
	}
	if _, ok := s.Cell(ShapeCell("PinX")); ok {
		t.Error("PinX present in an empty sheet, want absent")
	}
	if s.Deleted(ShapeCell("PinX")) || s.Rows("Property") != nil {
		t.Error("empty sheet reports a deletion or rows, want none")
	}
}

// TestDeletedRowZero checks that a deleted row IX 0 does not delete row 1.
func TestDeletedRowZero(t *testing.T) {
	s, skipped := indexXML(t, `<Shape><Section N='Character'><Row IX='0' Del='1'/><Row IX='1'><Cell N='Size' V='1'/></Row></Section></Shape>`)
	if len(skipped) != 0 {
		t.Fatalf("skipped = %v, want none", skipped)
	}
	if s.Deleted(IndexedRowCell("Character", -1, 1, "Size")) {
		t.Error("Deleted(Character row 1 Size) = true, want false")
	}
	if !s.Deleted(IndexedRowCell("Character", -1, 0, "Size")) {
		t.Error("Deleted(Character row 0 Size) = false, want true")
	}
	cell, ok := s.Cell(IndexedRowCell("Character", -1, 1, "Size"))
	if !ok {
		t.Fatal("Character row 1 Size absent")
	}
	if cell.V != "1" {
		t.Errorf("Character row 1 Size V = %q, want %q", cell.V, "1")
	}
}

// TestDeletedSectionCharacter checks that a deleted Character section
// deletes its rows.
func TestDeletedSectionCharacter(t *testing.T) {
	s, skipped := indexXML(t, `<Shape><Section N='Character' Del='1'/></Shape>`)
	if len(skipped) != 0 {
		t.Fatalf("skipped = %v, want none", skipped)
	}
	if !s.Deleted(IndexedRowCell("Character", -1, 0, "Size")) {
		t.Error("Deleted(Character row 0 Size) = false, want true")
	}
	if !s.Deleted(IndexedRowCell("Character", -1, 5, "Size")) {
		t.Error("Deleted(Character row 5 Size) = false, want true")
	}
}

// TestDeletedGeometrySectionIX checks that a deleted Geometry section
// deletes only its own section IX.
func TestDeletedGeometrySectionIX(t *testing.T) {
	s, skipped := indexXML(t, `<Shape><Section N='Geometry' IX='0'><Row IX='1'><Cell N='X' V='0'/></Row></Section><Section N='Geometry' IX='1' Del='1'/></Shape>`)
	if len(skipped) != 0 {
		t.Fatalf("skipped = %v, want none", skipped)
	}
	if !s.Deleted(IndexedRowCell("Geometry", 1, 3, "X")) {
		t.Error("Deleted(Geometry IX 1 row 3 X) = false, want true")
	}
	if s.Deleted(IndexedRowCell("Geometry", 0, 1, "X")) {
		t.Error("Deleted(Geometry IX 0 row 1 X) = true, want false")
	}
}

// TestDeletedNamedPropertyRow checks that a deleted Property row deletes
// only that named row.
func TestDeletedNamedPropertyRow(t *testing.T) {
	s, skipped := indexXML(t, `<Shape><Section N='Property'><Row N='A' Del='1'/></Section></Shape>`)
	if len(skipped) != 0 {
		t.Fatalf("skipped = %v, want none", skipped)
	}
	if !s.Deleted(NamedRowCell("Property", "A", "Value")) {
		t.Error("Deleted(Property A Value) = false, want true")
	}
	if s.Deleted(NamedRowCell("Property", "B", "Value")) {
		t.Error("Deleted(Property B Value) = true, want false")
	}
}

// TestRowsGeometryMixedSections checks that Rows mixes the Geometry
// sections in stored order with each key carrying its SectionIX.
func TestRowsGeometryMixedSections(t *testing.T) {
	s, skipped := indexXML(t, `<Shape><Section N='Geometry' IX='0'><Row IX='1'/><Row IX='2'/></Section><Section N='Geometry' IX='1'><Row IX='1'/></Section></Shape>`)
	if len(skipped) != 0 {
		t.Fatalf("skipped = %v, want none", skipped)
	}
	want := []Key{IndexedRowCell("Geometry", 0, 1, ""), IndexedRowCell("Geometry", 0, 2, ""), IndexedRowCell("Geometry", 1, 1, "")}
	if got := s.Rows("Geometry"); !slices.Equal(got, want) {
		t.Errorf("Rows(Geometry) = %v, want %v", got, want)
	}
}

// TestRowsDuplicateRow checks that a repeated row is listed once and
// its second cells are skipped, one entry per cell.
func TestRowsDuplicateRow(t *testing.T) {
	s, skipped := indexXML(t, `<Shape><Section N='Character'><Row IX='3'><Cell N='Size' V='1'/><Cell N='Style' V='0'/></Row><Row IX='3'><Cell N='Size' V='9'/><Cell N='Style' V='9'/></Row></Section></Shape>`)
	want := []Key{IndexedRowCell("Character", -1, 3, "")}
	if got := s.Rows("Character"); !slices.Equal(got, want) {
		t.Errorf("Rows(Character) = %v, want %v", got, want)
	}
	if len(skipped) != 2 {
		t.Errorf("skipped = %v, want one entry per repeated cell (2)", skipped)
	}
	if cell, ok := s.Cell(IndexedRowCell("Character", -1, 3, "Size")); !ok || cell.V != "1" {
		t.Errorf("Character row 3 Size = %+v, %v; want first V 1", cell, ok)
	}
}

// TestScalingIndexGeometryRows checks that Index scales linearly with
// Geometry rows: 20,000 rows cost at most 8 times the time and 5.5
// times the bytes and allocations of 5,000 rows.
func TestScalingIndexGeometryRows(t *testing.T) {
	measure := func(n int) laws.Cost {
		t.Helper()
		var b strings.Builder
		b.WriteString(`<Shape><Section N='Geometry' IX='0'>`)
		for i := range n {
			b.WriteString(`<Row T='LineTo' IX='`)
			b.WriteString(strconv.Itoa(i))
			b.WriteString(`'/>`)
		}
		b.WriteString(`</Section></Shape>`)
		doc, err := tree.ParseString(b.String())
		if err != nil {
			t.Fatalf("ParseString: %v", err)
		}
		root := doc.Root()
		run := func() error {
			Index(root)
			return nil
		}
		if err := run(); err != nil {
			t.Fatalf("Index warm: %v", err)
		}
		cost, err := laws.Measure(5, run)
		if err != nil {
			t.Fatalf("Measure: %v", err)
		}
		return cost
	}
	small := measure(5000)
	large := measure(20000)
	t.Logf("Index Geometry rows: 5000 rows %d bytes %d allocs %s, 20000 rows %d bytes %d allocs %s", small.Bytes, small.Mallocs, small.Time, large.Bytes, large.Mallocs, large.Time)
	if ratio := float64(large.Bytes) / float64(max(small.Bytes, 1)); ratio > 5.5 {
		t.Errorf("Index allocates %.2f times the bytes for 4 times the rows (budget 5.5)", ratio)
	}
	if ratio := float64(large.Mallocs) / float64(max(small.Mallocs, 1)); ratio > 5.5 {
		t.Errorf("Index makes %.2f times the allocations for 4 times the rows (budget 5.5)", ratio)
	}
	if ratio := float64(large.Time) / float64(max(small.Time, 1)); ratio > 8 {
		t.Errorf("Index takes %.2f times the time for 4 times the rows (budget 8)", ratio)
	}
}

// FuzzIndex parses arbitrary bytes and indexes every element, checking
// that Index never panics. It is a fuzz target, not a unit test.
func FuzzIndex(f *testing.F) {
	f.Add([]byte(`<Shape ID='1'><Cell N='PinX' V='2' U='IN'/><Cell N='Width' V='1' F='Inh'/></Shape>`))
	f.Add([]byte(`<Shape ID='1'><Section N='Geometry' IX='0'><Cell N='NoFill' V='0'/><Row T='MoveTo' IX='1'><Cell N='X' V='0'/></Row></Section></Shape>`))
	f.Add([]byte(`<Shape ID='1'><Section N='Property'><Row N='Cost'><Cell N='Value' V='12.5'/></Row></Section></Shape>`))
	f.Fuzz(func(_ *testing.T, data []byte) {
		doc, err := tree.Parse(data)
		if err != nil {
			return
		}
		stack := []*tree.Node{doc.Node()}
		for len(stack) > 0 {
			node := stack[len(stack)-1]
			stack = stack[:len(stack)-1]
			if node.Kind() == tree.ElementNode {
				Index(node)
			}
			for child := range node.ElementsSeq() {
				stack = append(stack, child)
			}
		}
	})
}
