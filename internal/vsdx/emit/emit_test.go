package emit

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jhnoel/documentdriver/internal/repo"
)

// TestShapeXML checks chunk K3 rows (designer goldens and named cases).
func TestShapeXML(t *testing.T) {
	rect := Shape{ID: 1, Geom: "rect", PinX: 2, PinY: 9.5, Width: 2, Height: 1,
		LocPinX: 1, LocPinY: 0.5, Style: 3}
	got, err := ShapeXML(rect)
	if err != nil {
		t.Fatalf("ShapeXML(rect) error = %v", err)
	}
	if want := golden(t, "rect.xml"); string(got) != want {
		t.Errorf("ShapeXML(rect) = %s\nwant %s", got, want)
	}

	ellipse := Shape{ID: 2, Geom: "ellipse", PinX: 6, PinY: 9.5, Width: 2, Height: 1,
		LocPinX: 1, LocPinY: 0.5, Style: 3}
	got, err = ShapeXML(ellipse)
	if err != nil {
		t.Fatalf("ShapeXML(ellipse) error = %v", err)
	}
	if want := golden(t, "ellipse.xml"); string(got) != want {
		t.Errorf("ShapeXML(ellipse) = %s\nwant %s", got, want)
	}
}

// TestShapeXMLName checks that a shape named "A&B" writes escaped
// NameU/Name attributes after ID (chunk K3 row).
func TestShapeXMLName(t *testing.T) {
	got, err := ShapeXML(Shape{ID: 1, Name: "A&B", Geom: "rect", Width: 2, Height: 1, Style: 3})
	if err != nil {
		t.Fatalf("ShapeXML(named) error = %v", err)
	}
	s := string(got)
	want := "NameU='A&amp;B' Name='A&amp;B'"
	if !strings.Contains(s, want) {
		t.Errorf("ShapeXML(named) = %s, want it to add %s after ID", s, want)
	}
	if strings.Index(s, "ID='1'") > strings.Index(s, want) {
		t.Errorf("ShapeXML(named) = %s, want %s after ID", s, want)
	}
}

// TestShapeXMLFill checks that fill "#FF0000" writes FillForegnd and
// FillPattern after FlipY (chunk K3 row).
func TestShapeXMLFill(t *testing.T) {
	got, err := ShapeXML(Shape{ID: 1, Geom: "rect", Width: 2, Height: 1,
		FillHex: "#FF0000", Style: 3})
	if err != nil {
		t.Fatalf("ShapeXML(fill) error = %v", err)
	}
	s := string(got)
	want := "<Cell N='FlipY' V='0'/><Cell N='FillForegnd' V='#FF0000'/><Cell N='FillPattern' V='1'/>"
	if !strings.Contains(s, want) {
		t.Errorf("ShapeXML(fill) = %s, want it to add fill cells after FlipY as %s", s, want)
	}
}

// TestShapeXMLGeom checks that an unknown geom is refused (chunk K3
// row: geom "triangle").
func TestShapeXMLGeom(t *testing.T) {
	if _, err := ShapeXML(Shape{ID: 1, Geom: "triangle", Width: 1, Height: 1, Style: 3}); err == nil {
		t.Errorf("ShapeXML(triangle) error = nil, want an error")
	}
}

// TestShapeXMLWidth checks that a zero width is refused (chunk K3
// row: width 0).
func TestShapeXMLWidth(t *testing.T) {
	if _, err := ShapeXML(Shape{ID: 1, Geom: "rect", Width: 0, Height: 1, Style: 3}); err == nil {
		t.Errorf("ShapeXML(width 0) error = nil, want an error")
	}
}

// TestShapeXMLHeight checks that a zero height is refused: the LocPin
// Height* formula divides by it.
func TestShapeXMLHeight(t *testing.T) {
	if _, err := ShapeXML(Shape{ID: 1, Geom: "rect", Width: 1, Height: 0, Style: 3}); err == nil {
		t.Errorf("ShapeXML(height 0) error = nil, want an error")
	}
}

// TestShapeXMLLine checks that line colour and weight write their cells:
// "<Cell N='LineColor' V='#00FF00'/>" and, for a weight, a LineWeight
// cell in inches.
func TestShapeXMLLine(t *testing.T) {
	got, err := ShapeXML(Shape{ID: 1, Geom: "rect", Width: 2, Height: 1,
		LineHex: "#00FF00", LineWeight: 0.025, Style: 3})
	if err != nil {
		t.Fatalf("ShapeXML(line) error = %v", err)
	}
	s := string(got)
	if want := "<Cell N='LineColor' V='#00FF00'/>"; !strings.Contains(s, want) {
		t.Errorf("ShapeXML(line) = %s, want it to contain %s", s, want)
	}
	if want := "<Cell N='LineWeight' V='0.025'/>"; !strings.Contains(s, want) {
		t.Errorf("ShapeXML(line) = %s, want it to contain %s", s, want)
	}
}

// TestShapeXMLLocPin checks that the LocPin formulas carry the pin's
// fraction of the shape: LocPinX 0.5 of Width 2 is Width*0.25.
func TestShapeXMLLocPin(t *testing.T) {
	got, err := ShapeXML(Shape{ID: 1, Geom: "rect", Width: 2, Height: 1,
		LocPinX: 0.5, LocPinY: 0.25, Style: 3})
	if err != nil {
		t.Fatalf("ShapeXML(locpin) error = %v", err)
	}
	s := string(got)
	if want := "<Cell N='LocPinX' V='0.5' F='Width*0.25'/>"; !strings.Contains(s, want) {
		t.Errorf("ShapeXML(locpin) = %s, want it to contain %s", s, want)
	}
	if want := "<Cell N='LocPinY' V='0.25' F='Height*0.25'/>"; !strings.Contains(s, want) {
		t.Errorf("ShapeXML(locpin) = %s, want it to contain %s", s, want)
	}
}

// TestShapeXMLFlip checks that flips and angle write their stored
// values: FlipX true as "1", angle 1.5 radians as "1.5".
func TestShapeXMLFlip(t *testing.T) {
	got, err := ShapeXML(Shape{ID: 1, Geom: "rect", Width: 2, Height: 1,
		Angle: 1.5, FlipX: true, FlipY: true, Style: 3})
	if err != nil {
		t.Fatalf("ShapeXML(flip) error = %v", err)
	}
	s := string(got)
	for _, want := range []string{
		"<Cell N='Angle' V='1.5'/>",
		"<Cell N='FlipX' V='1'/>",
		"<Cell N='FlipY' V='1'/>",
	} {
		if !strings.Contains(s, want) {
			t.Errorf("ShapeXML(flip) = %s, want it to contain %s", s, want)
		}
	}
}

// golden returns the designer golden file testdata/vsdx/create/emit/
// name, failing the test when it cannot be read.
func golden(t *testing.T, name string) string {
	t.Helper()
	root, err := repo.Root()
	if err != nil {
		t.Fatalf("repo root: %v", err)
	}
	body, err := os.ReadFile(filepath.Join(root, "testdata", "vsdx", "create", "emit", name)) //nolint:gosec // G304: golden path built from the repo root and a fixed name, never from a document
	if err != nil {
		t.Fatalf("read golden %s: %v", name, err)
	}
	return string(body)
}
