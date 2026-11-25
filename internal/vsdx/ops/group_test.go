package ops_test

import (
	"cmp"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/jhnoel/documentdriver/internal/vsdx/apply"
	"github.com/jhnoel/documentdriver/internal/vsdx/ops"
	"github.com/jhnoel/documentdriver/internal/vsdx/vsd"
	"github.com/jhnoel/documentdriver/internal/vsdx/vsdxtest"
)

// hatchedBoxPlan is the patent kit's labelled hatched box: an outline
// rect [100, 100, 200, 120] with a 1.5 pt line carrying the label, and
// four 0.35 pt hatch lines clipped to it, grouped as $g.
const hatchedBoxPlan = `{"ops":[
	{"op":"object.add","at":"page0","geom":"rect","box":[100,100,200,120],"html":"<p>Memory 106</p>","line":{"w":1.5},"fill":"none","as":"box"},
	{"op":"object.add","at":"page0","geom":"polyline","points":[[100,160],[160,100]],"line":{"w":0.35},"as":"h1"},
	{"op":"object.add","at":"page0","geom":"polyline","points":[[100,220],[220,100]],"line":{"w":0.35},"as":"h2"},
	{"op":"object.add","at":"page0","geom":"polyline","points":[[160,220],[280,100]],"line":{"w":0.35},"as":"h3"},
	{"op":"object.add","at":"page0","geom":"polyline","points":[[220,220],[300,140]],"line":{"w":0.35},"as":"h4"},
	{"op":"group.create","at":["$box","$h1","$h2","$h3","$h4"],"as":"g","name":"Memory region"}]}`

// objString returns member key of object id on page0 of path as raw JSON.
func objString(t *testing.T, path, id, key string) string {
	t.Helper()
	return string(readObj(t, path, id)[key])
}

// TestGroupHatchedBox checks group.create on the kit's labelled
// hatched box: read shows the group (kind group, its name, box the
// members' bounds [100, 100, 200, 120]) and each member with parent
// the group, its own box, line weight (the outline 1.5 pt, the hatch
// 0.35 pt) and the outline's label; the group is stored as Visio's
// Group command writes it (Type Group, styles 3, LocPin formulas,
// ResizeMode 0, no geometry), and each member's place is bound to the
// group's size with Sheet.6!Width*k and Sheet.6!Height*k, k derived by
// hand: the outline fills the group (0.5, 0.5, 1, 1); hatch h1, box
// [100, 100, 60, 60], has its centre 30 pt from the group's left and
// 90 pt above its bottom (y 220), so PinX 30/200 = 0.15, PinY 90/120 =
// 0.75, Width 60/200 = 0.3, Height 60/120 = 0.5.
func TestGroupHatchedBox(t *testing.T) {
	out, _ := refusalOut(t, vsdxtest.Blank(t), hatchedBoxPlan)
	if got := objString(t, out, "page0/s6", "kind"); got != `"group"` {
		t.Fatalf("s6 kind = %s, want group", got)
	}
	if got := objString(t, out, "page0/s6", "name"); got != `"Memory region"` {
		t.Errorf("group name = %s", got)
	}
	if got := objString(t, out, "page0/s6", "box"); got != `[100,100,200,120]` {
		t.Errorf("group box = %s, want the members' bounds", got)
	}
	for id, want := range map[string]string{
		"page0/s1": `[100,100,200,120]`, "page0/s2": `[100,100,60,60]`, "page0/s3": `[100,100,120,120]`,
		"page0/s4": `[160,100,120,120]`, "page0/s5": `[220,140,80,80]`,
	} {
		if got := objString(t, out, id, "parent"); got != `"page0/s6"` {
			t.Errorf("%s parent = %s, want the group", id, got)
		}
		if got := objString(t, out, id, "box"); got != want {
			t.Errorf("%s box = %s, want %s (unchanged on the page)", id, got, want)
		}
		w := "1.5"
		if id != "page0/s1" {
			w = "0.35"
		}
		if got := objString(t, out, id, "line"); !strings.HasPrefix(got, `{"w":`+w+`,`) {
			t.Errorf("%s line = %s, want w %s", id, got, w)
		}
	}
	if got := objString(t, out, "page0/s1", "html"); got != `"<p>Memory 106</p>"` {
		t.Errorf("outline label = %s, want it kept on the child", got)
	}
	data, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	g := textSetShapeNode(t, data, 6)
	for attr, want := range map[string]string{"Type": "Group", "LineStyle": "3", "FillStyle": "3", "TextStyle": "3"} {
		if got, _ := g.Attr(attr); got != want {
			t.Errorf("group %s = %q, want %q", attr, got, want)
		}
	}
	gc := shapeCells(t, out, 6)
	if gc["LocPinX"][2] != "Width*0.5" || gc["LocPinY"][2] != "Height*0.5" || gc["ResizeMode"][0] != "0" || gc["Angle"][0] != "0" {
		t.Errorf("group cells %v, want Visio's LocPin formulas, ResizeMode 0, Angle 0", gc)
	}
	for c := range g.ElementsSeq() {
		if n, _ := c.Attr("N"); c.Local() == vsd.ElemSection && n == vsd.SectionGeometry {
			t.Error("the group has a Geometry section; Visio's groups have none")
		}
	}
	for id, want := range map[uint32][4]string{1: {"0.5", "0.5", "1", "1"}, 2: {"0.15", "0.75", "0.3", "0.5"}} {
		cells := shapeCells(t, out, id)
		for i, name := range []string{"PinX", "PinY", "Width", "Height"} {
			side := "Width"
			if name == "PinY" || name == "Height" {
				side = "Height"
			}
			if got := cells[name][2]; got != "Sheet.6!"+side+"*"+want[i] {
				t.Errorf("s%d %s F = %q, want Sheet.6!%s*%s", id, name, got, side, want[i])
			}
		}
	}
}

// TestGroupResizeCarriesChildren checks that object.set box on the
// group scales its members, as Visio does through their Sheet.6
// formulas: resizing [100, 100, 200, 120] to [100, 100, 300, 180]
// (1.5 times) puts hatch h1 at [100, 100, 90, 90] and the outline at
// the group's box, with the line weights unchanged; sending back the
// box read shows changes nothing.
func TestGroupResizeCarriesChildren(t *testing.T) {
	src, _ := refusalOut(t, vsdxtest.Blank(t), hatchedBoxPlan)
	out, changed := refusalOut(t, src, `{"ops":[{"op":"object.set","at":"page0/s6","box":[100,100,300,180]}]}`)
	if len(changed) != 6 {
		t.Errorf("changed = %v, want the group and its five members", changed)
	}
	for id, want := range map[string]string{"page0/s1": `[100,100,300,180]`, "page0/s2": `[100,100,90,90]`, "page0/s5": `[280,160,120,120]`} {
		if got := objString(t, out, id, "box"); got != want {
			t.Errorf("%s box = %s, want %s", id, got, want)
		}
	}
	if got := objString(t, out, "page0/s2", "line"); !strings.HasPrefix(got, `{"w":0.35,`) {
		t.Errorf("hatch line after resize = %s, want w 0.35", got)
	}
	if _, changed := refusalOut(t, out, `{"ops":[{"op":"object.set","at":"page0/s6","box":[100,100,300,180]}]}`); len(changed) != 0 {
		t.Errorf("identity resize: changed = %v", changed)
	}
}

// TestGroupUngroupRoundTrip checks that group.ungroup undoes
// group.create: the members are back on the page with the boxes and
// points they had, no formula names the removed group, and the group
// is in deleted.
func TestGroupUngroupRoundTrip(t *testing.T) {
	src, _ := refusalOut(t, vsdxtest.Blank(t), hatchedBoxPlan)
	out := filepath.Join(t.TempDir(), "out.vsdx")
	res, err := apply.Run(src, parsePlan(t, `{"ops":[{"op":"group.ungroup","at":"page0/s6"}]}`), apply.Options{Out: out})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Deleted) != 1 || res.Deleted[0] != "page0/s6" {
		t.Errorf("deleted = %v, want the group", res.Deleted)
	}
	for id, want := range map[string]string{
		"page0/s1": `[100,100,200,120]`, "page0/s2": `[100,100,60,60]`, "page0/s5": `[220,140,80,80]`,
	} {
		o := readObj(t, out, id)
		if got := string(o["box"]); got != want {
			t.Errorf("%s box = %s, want %s", id, got, want)
		}
		if _, ok := o["parent"]; ok {
			t.Errorf("%s still has a parent", id)
		}
	}
	if got := objString(t, out, "page0/s2", "points"); got != `[[100,160],[160,100]]` {
		t.Errorf("h1 points = %s, want [[100,160],[160,100]]", got)
	}
	pkgData, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(pageXML(t, pkgData), "Sheet.6!") {
		t.Error("a formula still names the removed group Sheet.6")
	}
}

// TestGroupRefusals checks group.create's and group.ungroup's rules and
// refusals.
func TestGroupRefusals(t *testing.T) {
	for _, c := range []struct{ plan, code, path string }{
		{`{"op":"group.create","at":["page0/s1"]}`, "invalid_operation", "ops[0].at"},
		{`{"op":"group.create","at":["page0/s1","page0/s1"]}`, "invalid_operation", "ops[0].at[1]"},
		{`{"op":"group.create","at":["page0/s1","page0"]}`, "invalid_operation", "ops[0].at[1]"},
		{`{"op":"group.create","at":["page0/s1","page0/s2"],"as":"1g"}`, "invalid_operation", "ops[0].as"},
		{`{"op":"group.ungroup","at":"page0"}`, "invalid_operation", "ops[0].at"},
	} {
		_, errs := ops.ParsePlan([]byte(`{"ops":[` + c.plan + `]}`))
		if len(errs) != 1 || errs[0].Code != c.code || errs[0].Path != c.path {
			t.Errorf("%s: errs = %v, want one %s at %s", c.plan, errs, c.code, c.path)
		}
	}
	src, _ := refusalOut(t, vsdxtest.Blank(t), hatchedBoxPlan)
	extra, _ := refusalOut(t, src, `{"ops":[{"op":"object.add","at":"page0","geom":"rect","box":[400,400,20,20]}]}`)
	expectRefusal(t, extra, `{"ops":[{"op":"group.create","at":["page0/s2","page0/s7"]}]}`, "at", "not in the same container")
	expectRefusal(t, src, `{"ops":[{"op":"group.ungroup","at":"page0/s1"}]}`, "at", "is not a group")
	labelled, _ := refusalOut(t, src, `{"ops":[{"op":"text.set","at":"page0/s6","html":"<p>t</p>"}]}`)
	expectRefusal(t, labelled, `{"ops":[{"op":"group.ungroup","at":"page0/s6"}]}`, "at", "has text")
}

// TestGroupProperty groups random rotated and flipped rectangles and
// lines on a blank page, checks every member shows the box (or ends)
// it had, resizes the group to the box it shows (no change), then
// ungroups and checks the boxes again, over 40 random drawings.
func TestGroupProperty(t *testing.T) {
	seed := uint64(0x9e3779b97f4a7c15)
	next := func(n int) int {
		seed ^= seed << 13
		seed ^= seed >> 7
		seed ^= seed << 17
		return int(seed % uint64(n))
	}
	for trial := range 40 {
		var plan []string
		n := 2 + next(4)
		for i := range n {
			if next(3) == 0 {
				plan = append(plan, fmt.Sprintf(`{"op":"object.add","at":"page0","kind":"connector","from":[%d,%d],"to":[%d,%d]}`, 50+next(400), 50+next(600), 60+next(400), 60+next(600)))
				continue
			}
			plan = append(plan, fmt.Sprintf(`{"op":"object.add","at":"page0","geom":"rect","box":[%d,%d,%d,%d]}`, 40+next(400), 40+next(600), 10+next(100), 10+next(100)))
			if next(2) == 0 {
				plan = append(plan, fmt.Sprintf(`{"op":"object.set","at":"page0/s%d","rot":%d,"flip_h":%v}`, i+1, next(360), next(2) == 0))
			}
		}
		src, _ := refusalOut(t, vsdxtest.Blank(t), `{"ops":[`+strings.Join(plan, ",")+`]}`)
		before := map[string]string{}
		var at []string
		for i := range n {
			id := "page0/s" + strconv.Itoa(i+1)
			at = append(at, strconv.Quote(id))
			o := readObj(t, src, id)
			before[id] = string(o["box"]) + string(o["rot"]) + string(o["from"]) + string(o["to"])
		}
		grouped, _ := refusalOut(t, src, `{"ops":[{"op":"group.create","at":[`+strings.Join(at, ",")+`]}]}`)
		gid := "page0/s" + strconv.Itoa(n+1)
		for id, want := range before {
			o := readObj(t, grouped, id)
			if got := string(o["box"]) + string(o["rot"]) + string(o["from"]) + string(o["to"]); !closeShown(got, want) {
				t.Fatalf("trial %d: %s grouped shows %s, want %s", trial, id, got, want)
			}
		}
		var gbox []float64
		if err := json.Unmarshal(readObj(t, grouped, gid)["box"], &gbox); err != nil {
			t.Fatal(err)
		}
		gb, _ := json.Marshal(gbox)
		if _, changed := refusalOut(t, grouped, `{"ops":[{"op":"object.set","at":"`+gid+`","box":`+string(gb)+`}]}`); len(changed) != 0 {
			t.Fatalf("trial %d: identity resize changed %v", trial, changed)
		}
		ungrouped, _ := refusalOut(t, grouped, `{"ops":[{"op":"group.ungroup","at":"`+gid+`"}]}`)
		for id, want := range before {
			o := readObj(t, ungrouped, id)
			if got := string(o["box"]) + string(o["rot"]) + string(o["from"]) + string(o["to"]); !closeShown(got, want) {
				t.Fatalf("trial %d: %s ungrouped shows %s, want %s", trial, id, got, want)
			}
		}
	}
}

// closeShown reports whether two shown number lists are equal, numbers
// within 0.011 (read's two decimals, rounded either way) and angles
// within 0.011 degrees modulo 360.
func closeShown(a, b string) bool {
	if a == b {
		return true
	}
	split := func(s string) []string {
		return strings.FieldsFunc(s, func(r rune) bool { return r == '[' || r == ']' || r == ',' || r == '"' })
	}
	x, y := split(a), split(b)
	if len(x) != len(y) {
		return false
	}
	for i := range x {
		p, err1 := strconv.ParseFloat(x[i], 64)
		q, err2 := strconv.ParseFloat(y[i], 64)
		if err1 != nil || err2 != nil {
			return false
		}
		if d := math.Abs(p - q); d > 0.011 && math.Abs(math.Remainder(p-q, 360)) > 0.011 {
			return false
		}
	}
	return true
}

// pageXML returns the source of page0's shapes in the saved drawing
// data, from the tree's shape nodes (the zip is compressed).
func pageXML(t *testing.T, data []byte) string {
	t.Helper()
	var b strings.Builder
	for id := uint32(1); id <= 5; id++ {
		n := textSetShapeNode(t, data, id)
		if n == nil {
			continue
		}
		for c := range n.ElementsSeq() {
			f, _ := c.Attr("F")
			b.WriteString(f + "\n")
		}
	}
	return b.String()
}

// hatchSegments returns n parallel segments of slope 1 on the page (x -
// y constant, a "\" hatch with y down) clipped to the box [x0, y0, w,
// h], spaced evenly over the box's x - y range, each [[x, y], [x, y]]
// rounded to read's two decimals, as the patent kit clips its hatch.
func hatchSegments(n int, x0, y0, w, h float64) [][][]float64 {
	r := func(v float64) float64 { return math.Round(v*100) / 100 }
	var out [][][]float64
	for i := range n {
		k := x0 - (y0 + h) + (float64(i)+0.5)*(w+h)/float64(n)
		var pts [][]float64
		for _, x := range []float64{x0, x0 + w} {
			if y := x - k; y >= y0 && y <= y0+h {
				pts = append(pts, []float64{r(x), r(y)})
			}
		}
		for _, y := range []float64{y0, y0 + h} {
			if x := y + k; x > x0 && x < x0+w {
				pts = append(pts, []float64{r(x), r(y)})
			}
		}
		slices.SortFunc(pts, func(a, b []float64) int { return cmp.Compare(a[0], b[0]) })
		out = append(out, [][]float64{pts[0], pts[len(pts)-1]})
	}
	return out
}

// TestMultiPathHatch checks the kit's hatch as one shape of 12
// disjoint clipped segments (object.add paths) grouped with its
// labelled outline rect: read shows the 12 paths exactly as written
// and no points; the shape stores one Geometry section per path, IX 0
// to 11, each NoFill 1 (open) with a RelMoveTo and one RelLineTo; its
// line weight is its own; resizing the group to twice its width scales
// every segment's x about the group's left; and a shape of several
// paths carries text (its block the bounds of its paths).
func TestMultiPathHatch(t *testing.T) {
	segs := hatchSegments(12, 100, 100, 240, 140)
	var paths []string
	for _, s := range segs {
		b, _ := json.Marshal(s)
		paths = append(paths, `{"points":`+string(b)+`}`)
	}
	plan := `{"ops":[
		{"op":"object.add","at":"page0","geom":"polyline","paths":[` + strings.Join(paths, ",") + `],"line":{"w":0.35},"html":"<p>12</p>","as":"hatch"},
		{"op":"object.add","at":"page0","geom":"rect","box":[100,100,240,140],"html":"<p>Memory 106</p>","line":{"w":1.5},"fill":"none","text_block":{"background":"#FFFFFF"},"as":"box"},
		{"op":"group.create","at":["$hatch","$box"],"as":"g"}]}`
	out, _ := refusalOut(t, vsdxtest.Blank(t), plan)
	o := readObj(t, out, "page0/s1")
	var got []struct {
		Points [][]float64 `json:"points"`
		Closed bool        `json:"closed"`
	}
	if err := json.Unmarshal(o["paths"], &got); err != nil {
		t.Fatalf("paths %s: %v", o["paths"], err)
	}
	if len(got) != 12 {
		t.Fatalf("%d paths, want 12", len(got))
	}
	for i, p := range got {
		if p.Closed || !slices.EqualFunc(p.Points, segs[i], slices.Equal[[]float64]) {
			t.Errorf("path %d = %v closed %v, want %v open", i, p.Points, p.Closed, segs[i])
		}
	}
	if _, ok := o["points"]; ok {
		t.Error("a shape of several paths shows points")
	}
	if got := string(o["line"]); !strings.HasPrefix(got, `{"w":0.35,`) {
		t.Errorf("line = %s, want w 0.35", got)
	}
	if got := string(o["html"]); got != `"<p>12</p>"` {
		t.Errorf("html = %s, want the shape's own text", got)
	}
	data, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	n := 0
	for sec := range textSetShapeNode(t, data, 1).ElementsSeq() {
		if name, _ := sec.Attr("N"); sec.Local() != vsd.ElemSection || name != vsd.SectionGeometry {
			continue
		}
		if ix, _ := sec.Attr("IX"); ix != strconv.Itoa(n) {
			t.Errorf("section %d has IX %q", n, ix)
		}
		var rows []string
		noFill := ""
		for c := range sec.ElementsSeq() {
			switch c.Local() {
			case vsd.ElemCell:
				if name, _ := c.Attr("N"); name == "NoFill" {
					noFill, _ = c.Attr("V")
				}
			case vsd.ElemRow:
				typ, _ := c.Attr("T")
				rows = append(rows, typ)
			}
		}
		if noFill != "1" || !slices.Equal(rows, []string{"RelMoveTo", "RelLineTo"}) {
			t.Errorf("section %d: NoFill %q rows %v, want 1 and RelMoveTo, RelLineTo", n, noFill, rows)
		}
		n++
	}
	if n != 12 {
		t.Errorf("%d Geometry sections, want 12", n)
	}
	resized, _ := refusalOut(t, out, `{"ops":[{"op":"object.set","at":"page0/s3","box":[100,100,480,140]}]}`)
	if err := json.Unmarshal(readObj(t, resized, "page0/s1")["paths"], &got); err != nil {
		t.Fatal(err)
	}
	for i, p := range got {
		for j, pt := range p.Points {
			want := 100 + 2*(segs[i][j][0]-100)
			if math.Abs(pt[0]-want) > 0.011 || math.Abs(pt[1]-segs[i][j][1]) > 0.011 {
				t.Errorf("resized path %d point %d = %v, want x %.2f, y %.2f", i, j, pt, want, segs[i][j][1])
			}
		}
	}
}

// TestMultiPathRules checks the plan faults of paths.
func TestMultiPathRules(t *testing.T) {
	two := `[{"points":[[0,0],[10,10]]},{"points":[[0,10],[10,0]]}]`
	for _, c := range []struct{ op, code, path string }{
		{`"geom":"polyline","paths":` + two + `,"points":[[0,0],[1,1]]`, "invalid_operation", "ops[0].paths"},
		{`"geom":"polyline","paths":[{"points":[[0,0],[10,"x"]]}]`, "invalid_plan", "ops[0].paths[0].points[1][1]"},
		{`"geom":"polyline","paths":[{"points":[[0,0],[10,10]]},{"points":[[5,5]]}]`, "invalid_operation", "ops[0].paths[1].points"},
		{`"geom":"polyline","paths":` + two + `,"fill":{"hex":"#FF0000"}`, "invalid_operation", "ops[0].fill"},
		{`"geom":"polyline","paths":` + two + `,"line":{"end":"triangle"}`, "invalid_operation", "ops[0].line"},
		{`"geom":"polyline","paths":[]`, "invalid_plan", "ops[0].paths"},
		{`"geom":"polyline","paths":[{"pts":[[0,0],[1,1]]}]`, "invalid_plan", "ops[0].paths[0].pts"},
		{`"geom":"rect","box":[0,0,10,10],"paths":` + two, "invalid_operation", "ops[0].paths"},
	} {
		_, errs := ops.ParsePlan([]byte(`{"ops":[{"op":"object.add","at":"page0",` + c.op + `}]}`))
		if len(errs) == 0 || errs[0].Code != c.code || errs[0].Path != c.path {
			t.Errorf("%s: errs = %v, want %s at %s", c.op, errs, c.code, c.path)
		}
	}
	// A closed path among open ones may be filled.
	if _, errs := ops.ParsePlan([]byte(`{"ops":[{"op":"object.add","at":"page0","geom":"polyline","paths":[{"points":[[0,0],[10,0],[10,10]],"closed":true},{"points":[[0,20],[10,30]]}],"fill":{"hex":"#FF0000"}}]}`)); len(errs) != 0 {
		t.Errorf("a filled closed path among open ones: %v", errs)
	}
}
