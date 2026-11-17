package inherit

import (
	"fmt"
	"strings"
	"testing"

	"github.com/jhnoel/documentdriver/internal/laws"
	"github.com/jhnoel/documentdriver/internal/ooxml/tree"
	"github.com/jhnoel/documentdriver/internal/vsdx/doc"
	"github.com/jhnoel/documentdriver/internal/vsdx/sheet"
)

// TestStyleCellV1 checks the H3 V1 rows: style 3 -> 6 -> 0 resolves
// LineWeight from style 0, LineColor from style 6, the shape's own
// FillForegnd locally, FillPattern from style 0, and the Character
// row 0 Size from style 0 through Rows and Cell.
func TestStyleCellV1(t *testing.T) {
	r := New(openFixture(t, "v1-minimal"))
	shapes := pageShapes(t, r.Doc())
	s1, ok := shapes[1]
	if !ok {
		t.Fatal("shape 1 not found")
	}
	tests := []struct {
		name   string
		key    sheet.Key
		wantV  string
		origin Origin
	}{
		{"s1 LineWeight", sheet.ShapeCell("LineWeight"), "0.01041666666666667", Style},
		{"s1 LineColor", sheet.ShapeCell("LineColor"), "24", Style},
		{"s1 FillForegnd", sheet.ShapeCell("FillForegnd"), "#4472c4", Local},
		{"s1 FillPattern", sheet.ShapeCell("FillPattern"), "1", Style},
	}
	for _, tc := range tests {
		cell, origin, ok := r.Cell(s1, tc.key)
		if !ok {
			t.Errorf("%s: absent, want (%q, %s)", tc.name, tc.wantV, tc.origin)
			continue
		}
		if cell.V != tc.wantV || origin != tc.origin {
			t.Errorf("%s = (%q, %s), want (%q, %s)", tc.name, cell.V, origin, tc.wantV, tc.origin)
		}
	}
	rows := r.Rows(s1, "Character")
	if len(rows) != 1 || rows[0].Key != sheet.IndexedRowCell("Character", -1, 0, "") || rows[0].Origin != Local {
		t.Fatalf("Rows(s1, Character) = %v, want [Character row 0 local]", rows)
	}
	cell, origin, ok := r.Cell(s1, rows[0].Key.WithCell("Size"))
	if !ok || cell.V != "0.1666666666666667" || origin != Style {
		t.Errorf("s1 Character row 0 Size = (%q, %s, %v), want (\"0.1666666666666667\", style, true)", cell.V, origin, ok)
	}
}

// TestStyleCellV2MasterStyle checks the H3 V2 row: s1 has no style
// attributes of its own, so its LineColor comes from master shape 5's
// styles (3 -> 6 -> 0), from style 6.
func TestStyleCellV2MasterStyle(t *testing.T) {
	r := New(openFixture(t, "v2-master"))
	shapes := pageShapes(t, r.Doc())
	s1, ok := shapes[1]
	if !ok {
		t.Fatal("shape 1 not found")
	}
	cell, origin, ok := r.Cell(s1, sheet.ShapeCell("LineColor"))
	if !ok || cell.V != "24" || origin != Style {
		t.Errorf("s1 LineColor = (%q, %s, %v), want (\"24\", style, true)", cell.V, origin, ok)
	}
}

// TestStyleCellV3 checks the V3 rows (fixture V3, committed): s1
// LineWeight from style 3 and LineColor from style 1 through style 3;
// s2 LineWeight from style 1; s4 LinePattern and FillPattern 0 from
// style 2; s3 line cells absent with one cycle note.
func TestStyleCellV3(t *testing.T) {
	r := New(openFixture(t, "v3-styles"))
	shapes := pageShapes(t, r.Doc())
	tests := []struct {
		name   string
		shape  uint32
		key    sheet.Key
		wantV  string
		origin Origin
	}{
		{"s1 LineWeight", 1, sheet.ShapeCell("LineWeight"), "0.04166666666666666", Style},
		{"s1 LineColor", 1, sheet.ShapeCell("LineColor"), "#000000", Style},
		{"s2 LineWeight", 2, sheet.ShapeCell("LineWeight"), "0.01388888888888889", Style},
		{"s4 LinePattern", 4, sheet.ShapeCell("LinePattern"), "0", Style},
		{"s4 FillPattern", 4, sheet.ShapeCell("FillPattern"), "0", Style},
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
	s3, ok := shapes[3]
	if !ok {
		t.Fatal("shape 3 not found")
	}
	for _, name := range []string{"LineWeight", "LineColor", "LinePattern"} {
		if _, _, ok := r.Cell(s3, sheet.ShapeCell(name)); ok {
			t.Errorf("s3 %s present, want absent (style cycle)", name)
		}
	}
	notes := r.Notes()
	if len(notes) != 1 {
		t.Fatalf("Notes = %v, want one cycle note", notes)
	}
	if notes[0].At != "/visio/document.xml" || notes[0].Reason != "style cycle 5 -> 6 -> 5" {
		t.Errorf("note = %q: %q, want \"%s: style cycle 5 -> 6 -> 5\"", notes[0].At, notes[0].Reason, "/visio/document.xml")
	}
}

// TestStyleCellPage checks a page category cell: PrintPageOrientation
// of a sheet-less shape comes from style 0, trying the line chain
// first.
func TestStyleCellPage(t *testing.T) {
	r := New(openFixture(t, "v1-minimal"))
	cell, origin, ok := r.Cell(nil, sheet.ShapeCell("PrintPageOrientation"))
	if !ok || cell.V != "1" || origin != Style {
		t.Errorf("PrintPageOrientation = (%q, %s, %v), want (\"1\", style, true)", cell.V, origin, ok)
	}
}

// TestStyleChainTooDeep checks that a line chain longer than
// MaxStyleDepth stops with one note and the cell is absent, even when
// the last style on the chain stores it.
func TestStyleChainTooDeep(t *testing.T) {
	const depth = 40
	styles := make(map[uint32]*doc.StyleSheet, depth)
	parsed := make([]*tree.Document, 0, depth)
	for i := range depth {
		id := uint32(i)
		cell := ""
		if i == depth-1 {
			cell = "<Cell N='LineWeight' V='9'/>"
		}
		src := fmt.Sprintf("<StyleSheet ID='%d' LineStyle='%d'>%s</StyleSheet>", id, id+1, cell)
		node, err := tree.ParseString(src)
		if err != nil {
			t.Fatalf("ParseString(style %d): %v", id, err)
		}
		parsed = append(parsed, node)
		ss := &doc.StyleSheet{ID: id, Node: node.Root()}
		if i+1 < depth {
			ss.LineStyle, ss.HasLine = id+1, true
		}
		styles[id] = ss
	}
	r := New(&doc.Document{Part: "/visio/document.xml", Styles: styles})
	sh := &Shape{Styles: [3]uint32{0, 0, 0}, HasStyle: [3]bool{true, false, false}}
	if _, _, ok := r.Cell(sh, sheet.ShapeCell("LineWeight")); ok {
		t.Error("LineWeight present, want absent (chain longer than MaxStyleDepth)")
	}
	notes := r.Notes()
	if len(notes) != 1 || !strings.HasPrefix(notes[0].Reason, "style chain too deep 0 -> 1") {
		t.Errorf("Notes = %v, want one too-deep note", notes)
	}
	_ = parsed
}

// TestStyleCellAllocations checks that a style Cell lookup after the
// style sheets are indexed allocates almost nothing.
func TestStyleCellAllocations(t *testing.T) {
	r := New(openFixture(t, "v1-minimal"))
	shapes := pageShapes(t, r.Doc())
	s1 := shapes[1]
	if _, _, ok := r.Cell(s1, sheet.ShapeCell("LineWeight")); !ok {
		t.Fatal("s1 LineWeight absent")
	}
	if got := testing.AllocsPerRun(100, func() {
		if _, _, ok := r.Cell(s1, sheet.ShapeCell("LineWeight")); !ok {
			t.Error("s1 LineWeight absent")
		}
	}); got > 2 {
		t.Errorf("style Cell allocates %v per run after indexing, want at most 2", got)
	}
}

// TestScalingStyleCell checks the scaling row: resolving one style
// cell for N and 4N shapes (N = 1000) allocates at most 5.5 times as
// much for 4 times the shapes, linear in the number of shapes.
func TestScalingStyleCell(t *testing.T) {
	setup := func(n int) (func() error, error) {
		r := New(openFixture(t, "v1-minimal"))
		shapes := pageShapes(t, r.Doc())
		s1, ok := shapes[1]
		if !ok {
			return nil, fmt.Errorf("shape 1 not found")
		}
		many := make([]*Shape, n)
		for i := range many {
			many[i] = s1
		}
		return func() error {
			for _, s := range many {
				cell, origin, ok := r.Cell(s, sheet.ShapeCell("LineWeight"))
				if !ok || cell.V != "0.01041666666666667" || origin != Style {
					return fmt.Errorf("LineWeight = (%q, %s, %v), want (\"0.01041666666666667\", style, true)", cell.V, origin, ok)
				}
			}
			return nil
		}, nil
	}
	laws.Scales(t, "vsdx inherit style Cell", setup)
}
