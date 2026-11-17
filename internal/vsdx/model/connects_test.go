package model

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"slices"
	"strings"
	"testing"

	"github.com/jhnoel/documentdriver/internal/laws"
	"github.com/jhnoel/documentdriver/internal/ooxml/tree"
	"github.com/jhnoel/documentdriver/internal/vsdx/inherit"
	"github.com/jhnoel/documentdriver/internal/vsdx/place"
	"github.com/jhnoel/documentdriver/internal/vsdx/sheet"
)

// testShape builds the inherit.Shape of a hand-written Shape element.
func testShape(t *testing.T, xml string) *inherit.Shape {
	t.Helper()
	d, err := tree.ParseString(xml)
	if err != nil {
		t.Fatalf("ParseString: %v", err)
	}
	sh, _ := sheet.Index(d.Root())
	return &inherit.Shape{Sheet: sh}
}

// TestLinkConnectsV5 checks the V5 row: s3's FromGlue is "s1" and its
// ToGlue "s2:1"; Connections are the s3, s4 and s5 entries in that
// order; hazards are dangling-glue on page0/s4 and stale-connector on
// page0/s5 with the approved details, and none on s3.
func TestLinkConnectsV5(t *testing.T) {
	d, r := openFixture(t, "v5-glue")
	p := buildPageOf(t, d, r, 0)
	if o := byID(t, p, "page0/s3"); o.FromGlue != "s1" || o.ToGlue != "s2:1" {
		t.Errorf("s3 FromGlue, ToGlue = %q, %q, want s1, s2:1", o.FromGlue, o.ToGlue)
	}
	if o := byID(t, p, "page0/s4"); o.ToGlue != "#9" {
		t.Errorf("s4 ToGlue = %q, want #9", o.ToGlue)
	}
	if o := byID(t, p, "page0/s5"); o.FromGlue != "s1:1" {
		t.Errorf("s5 FromGlue = %q, want s1:1", o.FromGlue)
	}
	want := []Connection{
		{Connector: "page0/s3", From: "page0/s1", To: "page0/s2", FromGlue: "s1", ToGlue: "s2:1"},
		{Connector: "page0/s4", ToGlue: "#9"},
		{Connector: "page0/s5", From: "page0/s1", FromGlue: "s1:1"},
	}
	if !slices.Equal(p.Connections, want) {
		t.Errorf("Connections = %+v, want %+v", p.Connections, want)
	}
	wantH := []Hazard{
		{Kind: "dangling-glue", ID: "page0/s4", Detail: "to_glue names shape 9, which is not on the page"},
		{Kind: "stale-connector", ID: "page0/s5", Detail: "from end is 80.5 pt from site s1:1"},
	}
	if !slices.Equal(p.Hazards, wantH) {
		t.Errorf("Hazards = %+v, want %+v", p.Hazards, wantH)
	}
	for _, h := range p.Hazards {
		if h.ID == "page0/s3" {
			t.Errorf("s3 has hazard %+v, want none", h)
		}
	}
	if o := byID(t, p, "page0/s6"); o.FromGlue != "" || o.ToGlue != "" {
		t.Errorf("s6 FromGlue, ToGlue = %q, %q, want empty (its Connect is not an end)", o.FromGlue, o.ToGlue)
	}
}

// TestLinkConnectsV1 checks the V1 row for glue: no connections and
// no hazards.
func TestLinkConnectsV1(t *testing.T) {
	d, r := openFixture(t, "v1-minimal")
	p := buildPageOf(t, d, r, 0)
	if len(p.Connections) != 0 {
		t.Errorf("Connections = %+v, want none", p.Connections)
	}
	if len(p.Hazards) != 0 {
		t.Errorf("Hazards = %+v, want none", p.Hazards)
	}
}

// TestLinkConnectsV10 checks the V10 row for glue: a Connect with
// FromSheet 0 is a note, no hazard, and no connection.
func TestLinkConnectsV10(t *testing.T) {
	d, r := openFixture(t, "v10-defects")
	p := buildPageOf(t, d, r, 0)
	if len(p.Connections) != 0 {
		t.Errorf("Connections = %+v, want none", p.Connections)
	}
	if len(p.Hazards) != 0 {
		t.Errorf("Hazards = %+v, want none", p.Hazards)
	}
	found := false
	for _, n := range p.Notes {
		if n.At == "page0" && strings.Contains(n.Reason, "connect") {
			found = true
		}
	}
	if !found {
		t.Errorf("Notes = %+v, want a note at page0 about the connect with FromSheet 0", p.Notes)
	}
}

// TestLinkConnectsStaleUnit checks the stale rule on hand-placed
// shapes through the resolver: a whole-shape-glued begin 36 pt outside
// its target's box and a point-glued begin 36 pt from its site are
// both stale-connector, and a begin on its site is not. The target's
// box is [0,0,72,36] pt and its site is at (36,18) pt.
func TestLinkConnectsStaleUnit(t *testing.T) {
	targetXML := "<Shape ID='1'><Cell N='PinX' V='0.5'/><Cell N='PinY' V='10.75'/>" +
		"<Cell N='Width' V='1'/><Cell N='Height' V='0.5'/><Cell N='LocPinX' V='0.5'/>" +
		"<Cell N='LocPinY' V='0.25'/><Cell N='Angle' V='0'/><Cell N='FlipX' V='0'/><Cell N='FlipY' V='0'/>" +
		"<Section N='Connection'><Row IX='0'><Cell N='X' V='0.5'/><Cell N='Y' V='0.25'/></Row></Section></Shape>"
	connects := func(cell string) *tree.Node {
		d, err := tree.ParseString("<Connects><Connect FromSheet='2' FromCell='BeginX' ToSheet='1' ToCell='" + cell + "'/></Connects>")
		if err != nil {
			t.Fatalf("ParseString: %v", err)
		}
		return d.Root()
	}
	run := func(beginX, beginY, cell string) *Page {
		target := &Object{ID: "page0/s1", ShapeID: 1}
		conn := &Object{ID: "page0/s2", ShapeID: 2}
		p := &Page{ID: "page0", Objects: []*Object{target, conn}}
		connXML := "<Shape ID='2'><Cell N='BeginX' V='" + beginX + "'/><Cell N='BeginY' V='" + beginY + "'/></Shape>"
		byID := map[uint32]*placed{
			1: {obj: target, s: testShape(t, targetXML), frame: place.Page},
			2: {obj: conn, s: testShape(t, connXML), frame: place.Page},
		}
		linkConnects(p, nil, connects(cell), byID, 11)
		return p
	}
	if p := run("1.5", "10.75", "PinX"); len(p.Hazards) != 1 || p.Hazards[0].Kind != "stale-connector" || p.Hazards[0].Detail != "from end is 36 pt outside shape s1" {
		t.Errorf("whole-shape outside: Hazards = %+v, want one stale-connector stating from end is 36 pt outside shape s1", p.Hazards)
	}
	if p := run("0.5", "10.25", "Connections.X1"); len(p.Hazards) != 1 || p.Hazards[0].Detail != "from end is 36 pt from site s1:1" {
		t.Errorf("point off site: Hazards = %+v, want one stale-connector stating from end is 36 pt from site s1:1", p.Hazards)
	}
	if p := run("0.5", "10.75", "Connections.X1"); len(p.Hazards) != 0 {
		t.Errorf("on site: Hazards = %+v, want none", p.Hazards)
	}
}

// TestLinkConnectsWholeShapeV5 checks row 3: a test-built connector
// end glued whole-shape to V5 s2 and 0.25 in to the right of s2's box
// is stale-connector, "to end is 18 pt outside shape s2".
func TestLinkConnectsWholeShapeV5(t *testing.T) {
	d, r := openFixture(t, "v5-glue")
	built := buildPageOf(t, d, r, 0)
	target := byID(t, built, "page0/s2")
	conn := &Object{ID: "page0/s7", ShapeID: 7}
	p := &Page{ID: "page0", Objects: []*Object{target, conn}}
	byIDMap := map[uint32]*placed{
		2: {obj: target, s: shapeFor(target, 0, false), frame: place.Page},
		7: {obj: conn, s: testShape(t, "<Shape ID='7'><Cell N='EndX' V='5.75'/><Cell N='EndY' V='9'/></Shape>"), frame: place.Page},
	}
	doc, err := tree.ParseString("<Connects><Connect FromSheet='7' FromCell='EndX' ToSheet='2' ToCell='PinX'/></Connects>")
	if err != nil {
		t.Fatalf("ParseString: %v", err)
	}
	linkConnects(p, r, doc.Root(), byIDMap, built.H)
	want := []Hazard{{Kind: "stale-connector", ID: "page0/s7", Detail: "to end is 18 pt outside shape s2"}}
	if !slices.Equal(p.Hazards, want) {
		t.Errorf("Hazards = %+v, want %+v", p.Hazards, want)
	}
}

// TestLinkConnectsMasterSites checks row 4: a test-added connector
// (ID 6) on V2 whose end (2, 9.375) is glued to s1 Connections.X1,
// the master2 site at page (2, 9.375), has no hazard; the same end at
// (2, 8.375) is "to end is 72 pt from site s1:1".
func TestLinkConnectsMasterSites(t *testing.T) {
	d, r := openFixture(t, "v2-master")
	built := buildPageOf(t, d, r, 0)
	target := byID(t, built, "page0/s1")
	run := func(endY string) *Page {
		conn := &Object{ID: "page0/s6", ShapeID: 6}
		p := &Page{ID: "page0", Objects: []*Object{target, conn}}
		byIDMap := map[uint32]*placed{
			1: {obj: target, s: shapeFor(target, 0, false), frame: place.Page},
			6: {obj: conn, s: testShape(t, "<Shape ID='6'><Cell N='EndX' V='2'/><Cell N='EndY' V='"+endY+"'/></Shape>"), frame: place.Page},
		}
		doc, err := tree.ParseString("<Connects><Connect FromSheet='6' FromCell='EndX' ToSheet='1' ToCell='Connections.X1'/></Connects>")
		if err != nil {
			t.Fatalf("ParseString: %v", err)
		}
		linkConnects(p, r, doc.Root(), byIDMap, built.H)
		return p
	}
	if p := run("9.375"); len(p.Hazards) != 0 {
		t.Errorf("on master site: Hazards = %+v, want none", p.Hazards)
	}
	want := []Hazard{{Kind: "stale-connector", ID: "page0/s6", Detail: "to end is 72 pt from site s1:1"}}
	if p := run("8.375"); !slices.Equal(p.Hazards, want) {
		t.Errorf("off master site: Hazards = %+v, want %+v", p.Hazards, want)
	}
}

// TestLinkConnectsGeneratedHazardsAcrossViews checks row 5: the
// hazards of Generated(5000) are identical (count and contents) in
// the default view, with Sites and with Full.
func TestLinkConnectsGeneratedHazardsAcrossViews(t *testing.T) {
	d, r := openGenerated(t, 5000)
	e, ok := d.Page(0)
	if !ok {
		t.Fatalf("Generated(5000): no page 0")
	}
	var want []Hazard
	for _, o := range []Options{{}, {Sites: true}, {Full: true}} {
		p, err := BuildPage(d, r, e, o)
		if err != nil {
			t.Fatalf("BuildPage(Generated(5000), %+v): %v", o, err)
		}
		if want == nil {
			want = p.Hazards
			continue
		}
		if !slices.Equal(p.Hazards, want) {
			t.Fatalf("BuildPage(Generated(5000), %+v): Hazards differ in count or contents from the default view (%d, first %v vs %d, first %v)",
				o, len(p.Hazards), firstHazard(p.Hazards), len(want), firstHazard(want))
		}
	}
	stale := 0
	for _, h := range want {
		if h.Kind == "stale-connector" {
			stale++
		}
	}
	if stale != 2500 {
		t.Errorf("Generated(5000): stale-connector Hazards = %d, want 2500 (one stale end per generated connector)", stale)
	}
}

// firstHazard returns the first of hs, or a zero Hazard when hs is
// empty.
func firstHazard(hs []Hazard) Hazard {
	if len(hs) == 0 {
		return Hazard{}
	}
	return hs[0]
}

// TestConnectsResolverOnly checks row 6: connects.go declares no
// transform, sheetOf or cellNum and calls no sheet.Index.
func TestConnectsResolverOnly(t *testing.T) {
	f, err := parser.ParseFile(token.NewFileSet(), "connects.go", nil, 0)
	if err != nil {
		t.Fatalf("ParseFile(connects.go): %v", err)
	}
	for _, decl := range f.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok {
			continue
		}
		switch fn.Name.Name {
		case "transform", "sheetOf", "cellNum":
			t.Errorf("connects.go declares %s, want it removed", fn.Name.Name)
		}
	}
	ast.Inspect(f, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok || sel.Sel.Name != "Index" {
			return true
		}
		if id, ok := sel.X.(*ast.Ident); ok && id.Name == "sheet" {
			t.Errorf("connects.go calls sheet.Index, want sites and ends through the resolver")
		}
		return true
	})
}

// TestScalingLinkConnects checks the scaling row: BuildPage (which
// links the Connects) on Generated(1000) and Generated(4000), N and
// 4N, allocates at most 5.5 times as much (laws.Scales) and the
// laws.TimeScales time ratio is at most 8.
func TestScalingLinkConnects(t *testing.T) {
	setup := func(n int) (func() error, error) {
		d, r := openGenerated(t, n)
		e, ok := d.Page(0)
		if !ok {
			return nil, fmt.Errorf("Generated(%d): no page 0", n)
		}
		return func() error {
			p, err := BuildPage(d, r, e, Options{})
			if err != nil {
				return err
			}
			if len(p.Connections) == 0 {
				return fmt.Errorf("Generated(%d): no connections", n)
			}
			return nil
		}, nil
	}
	laws.Scales(t, "vsdx linkConnects", setup)
	laws.TimeScales(t, "vsdx linkConnects", setup)
}
