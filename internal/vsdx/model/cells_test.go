package model

import (
	"fmt"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/jhnoel/documentdriver/internal/laws"
	"github.com/jhnoel/documentdriver/internal/ooxml/tree"
	"github.com/jhnoel/documentdriver/internal/vsdx/inherit"
	"github.com/jhnoel/documentdriver/internal/vsdx/sheet"
)

// TestCellKey checks the M7 CellKey rows: shape cells, named rows
// with their prefixes, Geometry, Connection, the indexed text rows
// and Layer.
func TestCellKey(t *testing.T) {
	rows := []struct {
		key  sheet.Key
		want string
	}{
		{sheet.ShapeCell("PinX"), "PinX"},
		{sheet.NamedRowCell("Property", "Cost", "Value"), "Prop.Cost.Value"},
		{sheet.NamedRowCell("Control", "Row_1", "X"), "Controls.Row_1.X"},
		{sheet.NamedRowCell("User", "visVersion", "Value"), "User.visVersion.Value"},
		{sheet.IndexedRowCell("Geometry", 0, 2, "X"), "Geometry1.X2"},
		{sheet.Key{Section: "Geometry", SectionIX: 0, RowIX: -1, Cell: "NoFill"}, "Geometry1.NoFill"},
		{sheet.IndexedRowCell("Connection", -1, 0, "X"), "Connections.X1"},
		{sheet.IndexedRowCell("Character", -1, 1, "Size"), "Char.Size[2]"},
		{sheet.IndexedRowCell("Paragraph", -1, 0, "HorzAlign"), "Para.HorzAlign[1]"},
		{sheet.IndexedRowCell("Layer", -1, 0, "Name"), "Layer[1].Name"},
		// The remaining forms named in the CellKey comment.
		{sheet.NamedRowCell("Hyperlink", "Row_1", "Address"), "Hyperlink.Row_1.Address"},
		{sheet.NamedRowCell("Actions", "Row_1", "Menu"), "Actions.Row_1.Menu"},
		{sheet.IndexedRowCell("Tabs", -1, 0, "Position"), "Tabs.Position[1]"},
		{sheet.IndexedRowCell("Field", -1, 0, "Value"), "Fields.Value[1]"},
		{sheet.IndexedRowCell("Geometry", 1, 1, "Y"), "Geometry2.Y1"},
		{sheet.IndexedRowCell("Connection", -1, 2, "Y"), "Connections.Y3"},
		{sheet.IndexedRowCell("Scratch", -1, 0, "X"), "Scratch.X1"},
	}
	for _, row := range rows {
		if got := CellKey(row.key); got != row.want {
			t.Errorf("CellKey(%+v) = %q, want %q", row.key, got, row.want)
		}
	}
}

// TestRawCellsV1 checks the M7 rawCells row on V1's s1: the stored
// cells start with the five shape-level cells and include the
// Character row cell.
func TestRawCellsV1(t *testing.T) {
	d, r := openFixture(t, "v1-minimal")
	p := buildPageOf(t, d, r, 0)
	o := byID(t, p, "page0/s1")
	sh, _ := sheet.Index(o.Node)
	got := rawCells(sh)
	wantStart := []RawCell{
		{Key: "PinX", V: "2"},
		{Key: "PinY", V: "9"},
		{Key: "Width", V: "1"},
		{Key: "Height", V: "0.5"},
		{Key: "LocPinX", V: "0.5", F: "Width*0.5"},
	}
	if len(got) < len(wantStart) {
		t.Fatalf("rawCells(V1 s1) = %v, want at least %v", got, wantStart)
	}
	if !slices.Equal(got[:len(wantStart)], wantStart) {
		t.Errorf("rawCells(V1 s1) starts %v, want %v", got[:len(wantStart)], wantStart)
	}
	if !slices.Contains(got, RawCell{Key: "Char.Style[1]", V: "1"}) {
		t.Errorf("rawCells(V1 s1) = %v, want it to include Char.Style[1] = 1", got)
	}
	if !slices.Contains(got, RawCell{Key: "Geometry1.NoFill", V: "0"}) {
		t.Errorf("rawCells(V1 s1) = %v, want it to include Geometry1.NoFill = 0", got)
	}
	if !slices.Contains(got, RawCell{Key: "Geometry1.X2", V: "1"}) {
		t.Errorf("rawCells(V1 s1) = %v, want it to include Geometry1.X2 = 1", got)
	}
}

// cellsShape returns the sheet of one synthetic shape element.
func cellsShape(t *testing.T, inner string) *sheet.Sheet {
	t.Helper()
	parsed, err := tree.ParseString(`<Shape ID='1'>` + inner + `</Shape>`)
	if err != nil {
		t.Fatalf("ParseString: %v", err)
	}
	sh, _ := sheet.Index(parsed.Root())
	return sh
}

// TestRawCellsOrder checks that rawCells keeps stored order across
// shape, section and row cells, keeps the first of a repeated cell
// and leaves out cells of skipped rows and sections.
func TestRawCellsOrder(t *testing.T) {
	sh := cellsShape(t,
		`<Cell N='PinX' V='2'/><Cell N='PinX' V='9'/>`+
			`<Section N='Character'><Row IX='0'><Cell N='Style' V='1'/></Row></Section>`+
			`<Section N='Property'><Row N='Cost'><Cell N='Value' V='12.5' U='CY'/></Row></Section>`+
			`<Section N='Geometry' IX='0'><Cell N='NoFill' V='0'/><Row IX='1'><Cell N='X' V='0'/></Row></Section>`+
			`<Section><Row IX='0'><Cell N='X' V='1'/></Row></Section>`+
			`<Section N='Character'><Row><Cell N='Size' V='9'/></Row></Section>`)
	want := []RawCell{
		{Key: "PinX", V: "2"},
		{Key: "Char.Style[1]", V: "1"},
		{Key: "Prop.Cost.Value", V: "12.5", U: "CY"},
		{Key: "Geometry1.NoFill", V: "0"},
		{Key: "Geometry1.X1", V: "0"},
	}
	if got := rawCells(sh); !reflect.DeepEqual(got, want) {
		t.Errorf("rawCells = %#v, want %#v", got, want)
	}
}

// TestRawCellsNil checks rawCells on nil and empty sheets: no cells,
// never a panic.
func TestRawCellsNil(t *testing.T) {
	if got := rawCells(nil); got != nil {
		t.Errorf("rawCells(nil) = %v, want nil", got)
	}
	if got := rawCells(&sheet.Sheet{}); got != nil {
		t.Errorf("rawCells(empty) = %v, want nil", got)
	}
	if got := rawCells(cellsShape(t, "")); got != nil {
		t.Errorf("rawCells(no cells) = %v, want nil", got)
	}
}

// TestBoxOrigin checks the M7 boxOrigin rows: the most inherited of
// the origins, in the order local < inherited < master < style.
func TestBoxOrigin(t *testing.T) {
	rows := []struct {
		origins []inherit.Origin
		want    inherit.Origin
	}{
		{[]inherit.Origin{inherit.Local, inherit.Master}, inherit.Master},
		{[]inherit.Origin{inherit.Local, inherit.Inherited}, inherit.Inherited},
		{[]inherit.Origin{inherit.Style, inherit.Master}, inherit.Style},
		{nil, ""},
		{[]inherit.Origin{inherit.Local}, inherit.Local},
		{[]inherit.Origin{inherit.Inherited, inherit.Master}, inherit.Master},
		{[]inherit.Origin{inherit.Local, inherit.Inherited, inherit.Master, inherit.Style}, inherit.Style},
		{[]inherit.Origin{"", inherit.Local}, inherit.Local},
	}
	for _, row := range rows {
		if got := boxOrigin(row.origins...); got != row.want {
			t.Errorf("boxOrigin(%v) = %q, want %q", row.origins, got, row.want)
		}
	}
}

// TestScalingRawCells checks the scaling row: rawCells on a shape
// with N and 4N stored cells (N = 1000) allocates at most 5.5 times
// as much (laws.Scales) and the laws.TimeScales time ratio is at most 8.
func TestScalingRawCells(t *testing.T) {
	setup := func(n int) (func() error, error) {
		var b strings.Builder
		b.WriteString(`<Shape ID='1'>`)
		for i := range n {
			b.WriteString(`<Cell N='C`)
			b.WriteString(strconv.Itoa(i))
			b.WriteString(`' V='`)
			b.WriteString(strconv.Itoa(i))
			b.WriteString(`'/>`)
		}
		b.WriteString(`</Shape>`)
		parsed, err := tree.ParseString(b.String())
		if err != nil {
			return nil, err
		}
		sh, _ := sheet.Index(parsed.Root())
		return func() error {
			got := rawCells(sh)
			if len(got) != n {
				return fmt.Errorf("rawCells = %d cells, want %d", len(got), n)
			}
			if got[0].Key != "C0" || got[n-1].Key != "C"+strconv.Itoa(n-1) {
				return fmt.Errorf("rawCells first, last = %q, %q", got[0].Key, got[n-1].Key)
			}
			return nil
		}, nil
	}
	laws.Scales(t, "vsdx rawCells", setup)
	laws.TimeScales(t, "vsdx rawCells", setup)
}
