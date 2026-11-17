package model

import (
	"math"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/jhnoel/documentdriver/internal/ooxml/tree"
	"github.com/jhnoel/documentdriver/internal/opc"
	"github.com/jhnoel/documentdriver/internal/vsdx/doc"
	"github.com/jhnoel/documentdriver/internal/vsdx/inherit"
)

// paintOf parses one shape element as an inherit.Shape (through
// readShapeHead, as the page walk builds it) and runs paint on a new
// object for it through r.
func paintOf(t *testing.T, r *inherit.Resolver, xml string) *Object {
	t.Helper()
	parsed, err := tree.ParseString(xml)
	if err != nil {
		t.Fatalf("ParseString(%q): %v", xml, err)
	}
	head := readShapeHead(parsed.Root(), 0, false)
	if !head.ok {
		t.Fatalf("shape in %q has no usable ID", xml)
	}
	o := &Object{}
	paint(o, r, head.s)
	return o
}

// TestPaintV1 checks the V1 row: s1 fill #4472C4 with origin style
// (FillForegnd local, but FillPattern from StyleSheet 0, and style
// ranks above local), line w 0.75 pt, colour #7F7F7F and dash "1"
// with origin style.
func TestPaintV1(t *testing.T) {
	d, r := openFixture(t, "v1-minimal")
	p := buildPageOf(t, d, r, 0)
	o := byID(t, p, "page0/s1")
	if o.Fill == nil || o.Fill.None || o.Fill.Hex != "#4472C4" {
		t.Errorf("s1 Fill = %+v, want {Hex #4472C4}", o.Fill)
	}
	if got := o.Origins["fill"]; got != inherit.Style {
		t.Errorf("s1 fill origin = %q, want style (FillPattern style ranks above FillForegnd local)", got)
	}
	if o.Line == nil || o.Line.None {
		t.Fatalf("s1 Line = %+v, want a line", o.Line)
	}
	if !o.Line.HasW || math.Abs(o.Line.W-0.75) > 1e-9 {
		t.Errorf("s1 Line W = %v (HasW %v), want 0.75 pt", o.Line.W, o.Line.HasW)
	}
	if o.Line.Color != "#7F7F7F" {
		t.Errorf("s1 Line Color = %q, want #7F7F7F", o.Line.Color)
	}
	if o.Line.Dash != "1" {
		t.Errorf("s1 Line Dash = %q, want 1", o.Line.Dash)
	}
	if got := o.Origins["line"]; got != inherit.Style {
		t.Errorf("s1 line origin = %q, want style", got)
	}
}

// TestPaintV2 checks the V2 rows: s1 fill #FFFFFF from its master, s2
// fill #FF0000 local and s5 fill #0000FF from its master, each with
// fill origin style (FillPattern from StyleSheet 0 ranks above the
// foreground's master or local origin).
func TestPaintV2(t *testing.T) {
	d, r := openFixture(t, "v2-master")
	p := buildPageOf(t, d, r, 0)
	for _, tc := range []struct {
		id   string
		hex  string
		orig inherit.Origin
	}{
		{"page0/s1", "#FFFFFF", inherit.Style},
		{"page0/s2", "#FF0000", inherit.Style},
		{"page0/s5", "#0000FF", inherit.Style},
	} {
		o := byID(t, p, tc.id)
		if o.Fill == nil || o.Fill.None || o.Fill.Hex != tc.hex {
			t.Errorf("%s Fill = %+v, want {Hex %s}", tc.id, o.Fill, tc.hex)
		}
		if got := o.Origins["fill"]; got != tc.orig {
			t.Errorf("%s fill origin = %q, want %s", tc.id, got, tc.orig)
		}
	}
}

// TestPaintFillPatternNone checks the FillStyle 1 row: a shape whose
// fill style is 1 ("Text Only", FillPattern 0) has fill none.
func TestPaintFillPatternNone(t *testing.T) {
	_, r := openFixture(t, "v1-minimal")
	o := paintOf(t, r, `<Shape ID='1' LineStyle='3' FillStyle='1' TextStyle='3'></Shape>`)
	if o.Fill == nil || !o.Fill.None {
		t.Errorf("Fill = %+v, want {None}", o.Fill)
	}
}

// TestPaintLinePatternNone checks the LinePattern 0 row: a shape whose
// line style is 1 ("Text Only", LinePattern 0) has line none.
func TestPaintLinePatternNone(t *testing.T) {
	_, r := openFixture(t, "v1-minimal")
	o := paintOf(t, r, `<Shape ID='1' LineStyle='1' FillStyle='3' TextStyle='3'></Shape>`)
	if o.Line == nil || !o.Line.None {
		t.Errorf("Line = %+v, want {None}", o.Line)
	}
}

// TestPaintV10Themed checks the V10 row: a FillForegnd of "Themed"
// omits the fill with the reason "themed value not derived".
func TestPaintV10Themed(t *testing.T) {
	d, r := openFixture(t, "v10-defects")
	p := buildPageOf(t, d, r, 0)
	if o := byID(t, p, "page0/s9"); o.Fill != nil {
		t.Errorf("page0/s9 Fill = %+v, want omitted (nil)", o.Fill)
	}
	o := paintOf(t, r, `<Shape ID='9' LineStyle='3' FillStyle='3' TextStyle='3'><Cell N='FillForegnd' V='Themed' F='THEMEVAL()'/></Shape>`)
	if o.Fill != nil {
		t.Errorf("Fill = %+v, want omitted (nil)", o.Fill)
	}
	if got := omittedReason(o, "fill"); got != "themed value not derived" {
		t.Errorf("fill omission = %q, want %q (omitted %v)", got, "themed value not derived", o.Omitted)
	}
}

// TestPaintLineColorUnresolved checks the LineColor "255" row: the
// colour is omitted with a reason and the weight is kept.
func TestPaintLineColorUnresolved(t *testing.T) {
	_, r := openFixture(t, "v1-minimal")
	o := paintOf(t, r, `<Shape ID='1' LineStyle='3' FillStyle='3' TextStyle='3'><Cell N='LineWeight' V='0.020833333333333336'/><Cell N='LineColor' V='255'/></Shape>`)
	if o.Line == nil || o.Line.None {
		t.Fatalf("Line = %+v, want a line with its weight kept", o.Line)
	}
	if !o.Line.HasW || math.Abs(o.Line.W-1.5) > 1e-9 {
		t.Errorf("Line W = %v (HasW %v), want 1.5 pt kept", o.Line.W, o.Line.HasW)
	}
	if o.Line.Color != "" {
		t.Errorf("Line Color = %q, want omitted (empty)", o.Line.Color)
	}
	if got := omittedReason(o, "line"); got == "" {
		t.Errorf("line omission = %q, want a reason (omitted %v)", got, o.Omitted)
	}
}

// TestPaintAbsent checks that a shape no sheet stores fill or line
// cells for is left with no fill and no line, never a default.
func TestPaintAbsent(t *testing.T) {
	_, r := openFixture(t, "v1-minimal")
	o := paintOf(t, r, `<Shape ID='1'></Shape>`)
	if o.Fill != nil {
		t.Errorf("Fill = %+v, want absent (nil)", o.Fill)
	}
	if o.Line != nil {
		t.Errorf("Line = %+v, want absent (nil)", o.Line)
	}
}

// TestPaintNil checks that paint never panics on a nil object,
// resolver or shape.
func TestPaintNil(t *testing.T) {
	paint(nil, nil, nil)
	o := &Object{}
	paint(o, nil, nil)
	if o.Fill != nil || o.Line != nil {
		t.Errorf("paint with nil resolver and shape left Fill %+v Line %+v, want both absent", o.Fill, o.Line)
	}
}

// TestPaintThemed checks the MF3 rows on V1's document with a shape
// storing the cell: a Themed FillPattern omits the fill, a Themed
// LinePattern omits the line, a Themed LineWeight with LinePattern 1
// and a colour keeps the colour and dash with no weight, and a
// FillPattern of "x" keeps the "not a number" omission.
func TestPaintThemed(t *testing.T) {
	_, r := openFixture(t, "v1-minimal")
	for _, tc := range []struct {
		name    string
		xml     string
		fill    *Paint
		line    *Line
		omitted []Omission
	}{
		{"FillPattern Themed", `<Shape ID='1'><Cell N='FillPattern' V='Themed'/></Shape>`, nil, nil, []Omission{{Prop: "fill", Reason: "themed value not derived"}}},
		{"LinePattern Themed", `<Shape ID='1'><Cell N='LinePattern' V='Themed'/></Shape>`, nil, nil, []Omission{{Prop: "line", Reason: "themed value not derived"}}},
		{"LineWeight Themed", `<Shape ID='1'><Cell N='LinePattern' V='1'/><Cell N='LineWeight' V='Themed'/><Cell N='LineColor' V='#FF0000'/></Shape>`, nil, &Line{Color: "#FF0000", Dash: "1"}, []Omission{{Prop: "line", Reason: "themed value not derived"}}},
		{"FillPattern not a number", `<Shape ID='1'><Cell N='FillPattern' V='x'/></Shape>`, nil, nil, []Omission{{Prop: "fill", Reason: "FillPattern not a number"}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			o := paintOf(t, r, tc.xml)
			if !reflect.DeepEqual(o.Fill, tc.fill) {
				t.Errorf("Fill = %+v, want %+v", o.Fill, tc.fill)
			}
			if !reflect.DeepEqual(o.Line, tc.line) {
				t.Errorf("Line = %+v, want %+v", o.Line, tc.line)
			}
			if !reflect.DeepEqual(o.Omitted, tc.omitted) {
				t.Errorf("Omitted = %v, want %v", o.Omitted, tc.omitted)
			}
		})
	}
}

// corpusTest9Path returns the path of dave-howard-vsdx
// tests/test9_rect_and_line.vsdx under VSDX_CORPUS (a vsdx directory
// holding repos/<source>/, a worktree root holding corpus/vsdx/repos,
// or a repos directory directly). It skips when VSDX_CORPUS is unset
// and fails naming the path when VSDX_CORPUS is set but the file is
// missing.
func corpusTest9Path(t *testing.T) string {
	t.Helper()
	dir := os.Getenv("VSDX_CORPUS")
	if dir == "" {
		t.Skip("no vsdx corpus (set VSDX_CORPUS)")
	}
	rel := filepath.Join("dave-howard-vsdx", "tests", "test9_rect_and_line.vsdx")
	for _, cand := range []string{
		filepath.Join(dir, "repos", rel),
		filepath.Join(dir, "corpus", "vsdx", "repos", rel),
		filepath.Join(dir, rel),
	} {
		if _, err := os.Stat(cand); err == nil {
			return cand
		}
	}
	t.Fatalf("corpus file %s not found under VSDX_CORPUS %s", filepath.Join(dir, "repos", rel), dir)
	return ""
}

// TestPaintThemedCorpus checks the MF3 corpus row: dave-howard-vsdx
// tests/test9_rect_and_line.vsdx page 0 has no object with a "not a
// number" omission for a Themed cell. It skips without VSDX_CORPUS
// and fails when VSDX_CORPUS is set but the file is missing.
func TestPaintThemedCorpus(t *testing.T) {
	path := corpusTest9Path(t)
	p, err := opc.Open(path)
	if err != nil {
		t.Fatalf("opc.Open(test9_rect_and_line.vsdx): %v", err)
	}
	t.Cleanup(func() {
		if err := p.Close(); err != nil {
			t.Errorf("close test9_rect_and_line.vsdx: %v", err)
		}
	})
	d, err := doc.Open(p)
	if err != nil {
		t.Fatalf("doc.Open(test9_rect_and_line.vsdx): %v", err)
	}
	page := buildPageOf(t, d, inherit.New(d), 0)
	for _, o := range page.Objects {
		for _, om := range o.Omitted {
			if strings.Contains(om.Reason, "not a number") {
				t.Errorf("%s omission %q, want none for a Themed cell", o.ID, om.Reason)
			}
		}
	}
}
