package inherit

import (
	"fmt"
	"slices"
	"testing"

	"github.com/jhnoel/documentdriver/internal/laws"
	"github.com/jhnoel/documentdriver/internal/vsdx/sheet"
)

// TestRowsV2 checks the H3 V2 rows: Rows(s1, "Property") is the
// master's Cost, and Rows(s2, "Property") is the same row, local.
func TestRowsV2(t *testing.T) {
	r := New(openFixture(t, "v2-master"))
	shapes := pageShapes(t, r.Doc())
	cost := sheet.NamedRowCell("Property", "Cost", "")
	tests := []struct {
		name  string
		shape uint32
		want  []Row
	}{
		{"s1 Property", 1, []Row{{Key: cost, Origin: Master}}},
		{"s2 Property", 2, []Row{{Key: cost, Origin: Local}}},
	}
	for _, tc := range tests {
		s, ok := shapes[tc.shape]
		if !ok {
			t.Fatalf("%s: shape %d not found", tc.name, tc.shape)
		}
		if got := r.Rows(s, "Property"); !slices.Equal(got, tc.want) {
			t.Errorf("Rows(%s) = %v, want %v", tc.name, got, tc.want)
		}
	}
}

// TestRowsV6Deleted checks a Del row through Rows: the instance
// deletes the master's Gone row, overrides Cost, Owner and Done
// locally and adds NewRow after the master's rows, as the V6
// provenance lists the effective properties.
func TestRowsV6Deleted(t *testing.T) {
	r := New(openFixture(t, "v6-data"))
	shapes := pageShapes(t, r.Doc())
	s1, ok := shapes[1]
	if !ok {
		t.Fatal("shape 1 not found")
	}
	row := func(name string, origin Origin) Row {
		return Row{Key: sheet.NamedRowCell("Property", name, ""), Origin: origin}
	}
	want := []Row{
		row("Cost", Local),
		row("Owner", Local),
		row("Done", Local),
		row("Due", Master),
		row("Size", Master),
		row("Hidden", Master),
		row("Linked", Master),
		row("MasterOnly", Master),
		row("NewRow", Local),
	}
	if got := r.Rows(s1, "Property"); !slices.Equal(got, want) {
		t.Errorf("Rows(s1, Property) = %v, want %v", got, want)
	}
}

// TestRowsCharacterV1 checks the text style rows: s1's own Character
// row 0 is local, and its Paragraph row 0 comes from style 0, whose
// Character row 0 is the same row s1 already has.
func TestRowsCharacterV1(t *testing.T) {
	r := New(openFixture(t, "v1-minimal"))
	shapes := pageShapes(t, r.Doc())
	s1, ok := shapes[1]
	if !ok {
		t.Fatal("shape 1 not found")
	}
	wantChar := []Row{{Key: sheet.IndexedRowCell("Character", -1, 0, ""), Origin: Local}}
	if got := r.Rows(s1, "Character"); !slices.Equal(got, wantChar) {
		t.Errorf("Rows(s1, Character) = %v, want %v", got, wantChar)
	}
	wantPara := []Row{{Key: sheet.IndexedRowCell("Paragraph", -1, 0, ""), Origin: Style}}
	if got := r.Rows(s1, "Paragraph"); !slices.Equal(got, wantPara) {
		t.Errorf("Rows(s1, Paragraph) = %v, want %v", got, wantPara)
	}
}

// TestRowsEdgeCases checks Rows on a nil shape and an empty section.
func TestRowsEdgeCases(t *testing.T) {
	r := New(openFixture(t, "v2-master"))
	if got := r.Rows(nil, "Property"); got != nil {
		t.Errorf("Rows(nil, Property) = %v, want nil", got)
	}
	shapes := pageShapes(t, r.Doc())
	if got := r.Rows(shapes[1], ""); got != nil {
		t.Errorf("Rows(s1, \"\") = %v, want nil", got)
	}
	if got := r.Rows(&Shape{}, "Property"); got != nil {
		t.Errorf("Rows of an empty shape = %v, want nil", got)
	}
}

// TestTextV2 checks the H3 V2 text rows: s1 shows master shape 5's
// Text, s2 its own, s4 master3 shape 6's, and s3 none.
func TestTextV2(t *testing.T) {
	r := New(openFixture(t, "v2-master"))
	shapes := pageShapes(t, r.Doc())
	tests := []struct {
		name   string
		shape  uint32
		want   string
		origin Origin
	}{
		{"s1", 1, "Process\n", Master},
		{"s2", 2, "Review\n", Local},
		{"s4", 4, "Left\n", Master},
	}
	for _, tc := range tests {
		s, ok := shapes[tc.shape]
		if !ok {
			t.Fatalf("%s: shape %d not found", tc.name, tc.shape)
		}
		node, origin := r.Text(s)
		if node == nil {
			t.Errorf("Text(%s) = nil, want (%q, %s)", tc.name, tc.want, tc.origin)
			continue
		}
		if node.Text() != tc.want || origin != tc.origin {
			t.Errorf("Text(%s) = (%q, %s), want (%q, %s)", tc.name, node.Text(), origin, tc.want, tc.origin)
		}
	}
	if node, origin := r.Text(shapes[3]); node != nil || origin != "" {
		t.Errorf("Text(s3) = (%v, %q), want (nil, \"\")", node, origin)
	}
}

// TestTextV1 checks that s1 of V1 shows its own Text locally.
func TestTextV1(t *testing.T) {
	r := New(openFixture(t, "v1-minimal"))
	shapes := pageShapes(t, r.Doc())
	node, origin := r.Text(shapes[1])
	if node == nil || node.Text() != "Hello\n" || origin != Local {
		t.Errorf("Text(s1) = (%v, %q), want (\"Hello\\n\", local)", node, origin)
	}
	if node, origin := r.Text(nil); node != nil || origin != "" {
		t.Errorf("Text(nil) = (%v, %q), want (nil, \"\")", node, origin)
	}
	if node, origin := r.Text(&Shape{}); node != nil || origin != "" {
		t.Errorf("Text of an empty shape = (%v, %q), want (nil, \"\")", node, origin)
	}
}

// TestScalingRows checks the scaling row: listing the Property rows
// of one shape for N and 4N shapes (N = 1000) allocates at most 5.5
// times as much for 4 times the shapes, linear in the shapes.
func TestScalingRows(t *testing.T) {
	setup := func(n int) (func() error, error) {
		r := New(openFixture(t, "v2-master"))
		shapes := pageShapes(t, r.Doc())
		s2, ok := shapes[2]
		if !ok {
			return nil, fmt.Errorf("shape 2 not found")
		}
		many := make([]*Shape, n)
		for i := range many {
			many[i] = s2
		}
		want := []Row{{Key: sheet.NamedRowCell("Property", "Cost", ""), Origin: Local}}
		return func() error {
			for _, s := range many {
				if got := r.Rows(s, "Property"); !slices.Equal(got, want) {
					return fmt.Errorf("Rows = %v, want %v", got, want)
				}
			}
			return nil
		}, nil
	}
	laws.Scales(t, "vsdx inherit Rows", setup)
}
