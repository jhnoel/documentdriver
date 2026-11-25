package ops_test

import (
	"encoding/json"
	"math"
	"strconv"
	"strings"
	"testing"

	"github.com/jhnoel/documentdriver/internal/cli"
	"github.com/jhnoel/documentdriver/internal/vsdx/ops"
	"github.com/jhnoel/documentdriver/internal/vsdx/vsd"
	"github.com/jhnoel/documentdriver/internal/vsdx/vsdxtest"
)

// planFault decodes plan and returns its one fault, failing the test
// when there is not exactly one.
func planFault(t *testing.T, plan string) *cli.Error {
	t.Helper()
	_, errs := ops.ParsePlan([]byte(plan))
	if len(errs) != 1 {
		t.Fatalf("%s: faults %v, want one", plan, errs)
	}
	return errs[0]
}

// TestFillDecodeFaults checks the plan faults of fill (docs/contract.md
// §3, Pattern fills): the code and path of each malformed form.
func TestFillDecodeFaults(t *testing.T) {
	add := func(fill string) string {
		return `{"ops":[{"op":"object.add","at":"page0","geom":"rect","box":[0,0,10,10],"fill":` + fill + `}]}`
	}
	cases := []struct {
		fill, code, path, msg string
	}{
		{`{"pattern":"zigzag"}`, cli.CodeInvalidOperation, "ops[0].fill.pattern", "not a pattern name"},
		{`{"pattern":1}`, cli.CodeInvalidOperation, "ops[0].fill.pattern", "from 2 to 24"},
		{`{"pattern":25}`, cli.CodeInvalidOperation, "ops[0].fill.pattern", "from 2 to 24"},
		{`{"pattern":2.5}`, cli.CodeInvalidOperation, "ops[0].fill.pattern", "from 2 to 24"},
		{`{"pattern":true}`, cli.CodeInvalidPlan, "ops[0].fill.pattern", "pattern name or number"},
		{`{"pattern":"dots","hex":"black"}`, cli.CodeInvalidOperation, "ops[0].fill.hex", "#RRGGBB"},
		{`{"pattern":"dots","bg":"#12"}`, cli.CodeInvalidOperation, "ops[0].fill.bg", "#RRGGBB"},
		{`{"pattern":"dots","bg":{"hex":"#12"}}`, cli.CodeInvalidOperation, "ops[0].fill.bg.hex", "#RRGGBB"},
		{`{"pattern":"dots","bg":{"rgb":"#FFFFFF"}}`, cli.CodeInvalidPlan, "ops[0].fill.bg.rgb", "unknown field"},
		{`{"pattern":"dots","bg":7}`, cli.CodeInvalidPlan, "ops[0].fill.bg", "expected"},
		{`{"hex":"#FFFFFF","bg":"none"}`, cli.CodeInvalidPlan, "ops[0].fill.bg", "bg needs pattern"},
		{`{"hex":"#FFFFFF","density":"fine"}`, cli.CodeInvalidPlan, "ops[0].fill.density", "unknown field"},
		{`{}`, cli.CodeInvalidPlan, "ops[0].fill.hex", "missing field"},
	}
	for _, tc := range cases {
		e := planFault(t, add(tc.fill))
		if e.Code != tc.code || e.Path != tc.path || !strings.Contains(e.Message, tc.msg) {
			t.Errorf("fill %s: %s at %s %q, want %s at %s containing %q", tc.fill, e.Code, e.Path, e.Message, tc.code, tc.path, tc.msg)
		}
	}
}

// TestPathDecodeFaults checks the plan faults of geom polyline and
// curve: each rule's field, and that a point's fault names the point.
func TestPathDecodeFaults(t *testing.T) {
	cases := []struct{ op, path, msg string }{
		{`"geom":"rect","box":[0,0,9,9],"points":[[0,0],[1,1]]`, "ops[0].points", "go with geom polyline or curve"},
		{`"geom":"curve","pin":[1,1],"points":[[0,0],[1,1]]`, "ops[0].pin", "not box or pin"},
		{`"geom":"polyline"`, "ops[0].points", "need points"},
		{`"geom":"polyline","points":[[0,0],[1,1,1]]`, "ops[0].points[1]", "[x, y] pairs"},
		{`"geom":"curve","closed":true,"points":[[0,0],[5,5]]`, "ops[0].points", "at least 2 points, a closed one 3"},
		{`"geom":"polyline","points":[[0,3],[9,3]]`, "ops[0].points", "positive width and height"},
		{`"geom":"polyline","points":[[0,0],[9,3]],"fill":{"pattern":"dots"}`, "ops[0].fill", "fill needs closed"},
	}
	for _, tc := range cases {
		e := planFault(t, `{"ops":[{"op":"object.add","at":"page0",`+tc.op+`}]}`)
		if e.Path != tc.path || !strings.Contains(e.Message, tc.msg) {
			t.Errorf("%s: %s at %s %q, want %s containing %q", tc.op, e.Code, e.Path, e.Message, tc.path, tc.msg)
		}
	}
}

// fillOf returns the read fill of id in the drawing at path.
func fillOf(t *testing.T, path, id string) string {
	t.Helper()
	return string(readObj(t, path, id)["fill"])
}

// TestPatternFillWriteRead checks the Pattern fills rows: every named
// pattern writes its FillPattern number and reads back as its name,
// black on a clear background by default; a number reads back as the
// name when the table names it, else as the number; read's value sent
// back writes nothing; object.set moves between solid, pattern and
// none, and a solid hex equal to a pattern's colour still makes the
// fill solid.
func TestPatternFillWriteRead(t *testing.T) {
	var b strings.Builder
	b.WriteString(`{"ops":[`)
	names := []string{"hatch_up", "grid", "cross_hatch", "hatch_down", "horizontal", "vertical", "dots", "horizontal_fine", "vertical_fine", "hatch_down_fine", "hatch_up_fine", "grid_fine", "cross_hatch_fine"}
	if len(names) != len(vsd.FillPatternIndex) {
		t.Fatalf("test names %d, table %d: the rows must cover the table", len(names), len(vsd.FillPatternIndex))
	}
	for _, n := range names {
		b.WriteString(`{"op":"object.add","at":"page0","geom":"rect","box":[0,0,10,10],"fill":{"pattern":"` + n + `"}},`)
	}
	b.WriteString(`{"op":"object.add","at":"page0","geom":"rect","box":[0,0,10,10],"fill":{"pattern":10,"hex":"#ff0000","bg":{"hex":"#00ff00"}}},`)
	b.WriteString(`{"op":"object.add","at":"page0","geom":"rect","box":[0,0,10,10],"fill":{"pattern":"2","bg":"#FFFFFF"}}]}`)
	out, _ := refusalOut(t, vsdxtest.Blank(t), b.String())
	for i, n := range names {
		id := "page0/s" + strconv.Itoa(i+1)
		want := `{"hex":"#000000","pattern":"` + n + `","bg":"none"}`
		if got := fillOf(t, out, id); got != want {
			t.Errorf("%s fill = %s, want %s", id, got, want)
		}
	}
	if got, want := fillOf(t, out, "page0/s14"), `{"hex":"#FF0000","pattern":"10","bg":{"hex":"#00FF00"}}`; got != want {
		t.Errorf("numbered pattern fill = %s, want %s", got, want)
	}
	if got, want := fillOf(t, out, "page0/s15"), `{"hex":"#000000","pattern":"hatch_up","bg":{"hex":"#FFFFFF"}}`; got != want {
		t.Errorf("pattern 2 fill = %s, want %s", got, want)
	}
	for _, id := range []string{"page0/s1", "page0/s14", "page0/s15"} {
		plan := `{"ops":[{"op":"object.set","at":"` + id + `","fill":` + fillOf(t, out, id) + `}]}`
		if _, changed := refusalOut(t, out, plan); len(changed) != 0 {
			t.Errorf("identity %s: changed = %v, want none", plan, changed)
		}
	}
	solid, changed := refusalOut(t, out, `{"ops":[{"op":"object.set","at":"page0/s1","fill":{"hex":"#000000"}}]}`)
	if len(changed) != 1 || fillOf(t, solid, "page0/s1") != `{"hex":"#000000"}` {
		t.Errorf("solid over pattern: changed %v, fill %s, want solid #000000", changed, fillOf(t, solid, "page0/s1"))
	}
	back, _ := refusalOut(t, solid, `{"ops":[{"op":"object.set","at":"page0/s1","fill":{"pattern":"grid","hex":"#0000FF","bg":{"hex":"#FFFFFF"}}}]}`)
	if got, want := fillOf(t, back, "page0/s1"), `{"hex":"#0000FF","pattern":"grid","bg":{"hex":"#FFFFFF"}}`; got != want {
		t.Errorf("pattern over solid = %s, want %s", got, want)
	}
	clear, _ := refusalOut(t, back, `{"ops":[{"op":"object.set","at":"page0/s1","fill":{"pattern":"grid","hex":"#0000FF"}}]}`)
	if got, want := fillOf(t, clear, "page0/s1"), `{"hex":"#0000FF","pattern":"grid","bg":"none"}`; got != want {
		t.Errorf("default background = %s, want %s", got, want)
	}
	none, _ := refusalOut(t, clear, `{"ops":[{"op":"object.set","at":"page0/s1","fill":"none"}]}`)
	if got := fillOf(t, none, "page0/s1"); got != `"none"` {
		t.Errorf("fill none = %s", got)
	}
}

// pathObj is the read of one path shape.
type pathObj struct {
	Geom   string          `json:"geom"`
	Closed bool            `json:"closed"`
	Points [][]float64     `json:"points"`
	Box    []float64       `json:"box"`
	Line   json.RawMessage `json:"line"`
}

// readPath reads the path fields of id in the drawing at path.
func readPath(t *testing.T, path, id string) pathObj {
	t.Helper()
	o := readObj(t, path, id)
	b, err := json.Marshal(o)
	if err != nil {
		t.Fatal(err)
	}
	var p pathObj
	if err := json.Unmarshal(b, &p); err != nil {
		t.Fatal(err)
	}
	return p
}

// checkPoints compares read points with the plan's within the view's
// two-decimal number form.
func checkPoints(t *testing.T, name string, got, want [][]float64) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("%s: %d points, want %d (%v)", name, len(got), len(want), got)
	}
	for i := range want {
		if math.Abs(got[i][0]-want[i][0]) > 0.006 || math.Abs(got[i][1]-want[i][1]) > 0.006 {
			t.Errorf("%s: point %d = %v, want %v", name, i, got[i], want[i])
		}
	}
}

// pointsJSON renders points as plan JSON.
func pointsJSON(pts [][]float64) string {
	b, err := json.Marshal(pts)
	if err != nil {
		panic(err)
	}
	return string(b)
}

// TestPathAddRead checks the four test shapes of the path rows: a bell
// curve, a sine-like series (polyline), an arc (curve) and a closed
// trapezoid read back with their geom, closedness, points and box (the
// points' bounds), in points and with --units in; a closed path's
// repeated last point is dropped.
func TestPathAddRead(t *testing.T) {
	var bell, sine, arc [][]float64
	for i := 0; i <= 40; i++ {
		x := 72 + float64(i)*6
		bell = append(bell, []float64{x, 560 - 120*math.Exp(-math.Pow((float64(i)-20)/6, 2)/2)})
	}
	for i := 0; i <= 50; i++ {
		sine = append(sine, []float64{340 + float64(i)*5, 500 - 30*math.Sin(float64(i)/5)})
	}
	for i := 0; i <= 8; i++ {
		a := float64(i) * math.Pi / 16
		arc = append(arc, []float64{400 + 80*math.Cos(a), 700 - 80*math.Sin(a)})
	}
	trap := [][]float64{{80, 720}, {220, 720}, {190, 640}, {110, 640}}
	plan := `{"ops":[` +
		`{"op":"object.add","at":"page0","geom":"curve","points":` + pointsJSON(bell) + `},` +
		`{"op":"object.add","at":"page0","geom":"polyline","points":` + pointsJSON(sine) + `},` +
		`{"op":"object.add","at":"page0","geom":"curve","points":` + pointsJSON(arc) + `},` +
		`{"op":"object.add","at":"page0","geom":"polyline","closed":true,"points":` + pointsJSON(append(trap, trap[0])) + `,"fill":{"pattern":"cross_hatch"}}]}`
	out, _ := refusalOut(t, vsdxtest.Blank(t), plan)
	for _, tc := range []struct {
		id, geom string
		closed   bool
		pts      [][]float64
		box      []float64
	}{
		{"page0/s1", "curve", false, bell, []float64{72, 440, 240, 119.54}},
		{"page0/s2", "polyline", false, sine, []float64{340, 470.01, 250, 59.87}},
		{"page0/s3", "curve", false, arc, []float64{400, 620, 80, 80}},
		{"page0/s4", "polyline", true, trap, []float64{80, 640, 140, 80}},
	} {
		p := readPath(t, out, tc.id)
		if p.Geom != tc.geom || p.Closed != tc.closed {
			t.Errorf("%s: geom %q closed %v, want %q %v", tc.id, p.Geom, p.Closed, tc.geom, tc.closed)
		}
		checkPoints(t, tc.id, p.Points, tc.pts)
		for i := range tc.box {
			if math.Abs(p.Box[i]-tc.box[i]) > 0.006 {
				t.Errorf("%s: box %v, want %v", tc.id, p.Box, tc.box)
				break
			}
		}
	}
}

// TestPathLineEnds checks that line begin and end write on an open
// path (object.add and object.set) and read back, and are refused on a
// closed path and a rect as on any shape that is not a connector.
func TestPathLineEnds(t *testing.T) {
	plan := `{"ops":[` +
		`{"op":"object.add","at":"page0","geom":"polyline","points":[[0,0],[50,20],[100,0]],"line":{"w":1,"end":"triangle"}},` +
		`{"op":"object.add","at":"page0","geom":"curve","closed":true,"points":[[0,0],[50,20],[100,0]]},` +
		`{"op":"object.add","at":"page0","geom":"rect","box":[0,0,10,10]}]}`
	out, _ := refusalOut(t, vsdxtest.Blank(t), plan)
	if l := string(readPath(t, out, "page0/s1").Line); !strings.Contains(l, `"end":{"arrow":"triangle"}`) || strings.Contains(l, `"begin"`) {
		t.Errorf("open polyline line = %s, want end triangle only", l)
	}
	set, _ := refusalOut(t, out, `{"ops":[{"op":"object.set","at":"page0/s1","line":{"begin":"stealth"}}]}`)
	if l := string(readPath(t, set, "page0/s1").Line); !strings.Contains(l, `"begin":{"arrow":"stealth"}`) {
		t.Errorf("after set: line = %s, want begin stealth", l)
	}
	expectRefusal(t, out, `{"ops":[{"op":"object.set","at":"page0/s2","line":{"end":"triangle"}}]}`, "line.end", "open path")
	expectRefusal(t, out, `{"ops":[{"op":"object.set","at":"page0/s3","line":{"end":"triangle"}}]}`, "line.end", "open path")
}

// TestPathBoxScales checks that object.set box on a path scales its
// points with the box (the rows are fractions of Width and Height), so
// read shows the points moved and stretched.
func TestPathBoxScales(t *testing.T) {
	out, _ := refusalOut(t, vsdxtest.Blank(t), `{"ops":[{"op":"object.add","at":"page0","geom":"polyline","points":[[100,100],[150,200],[200,100]]}]}`)
	set, _ := refusalOut(t, out, `{"ops":[{"op":"object.set","at":"page0/s1","box":[0,0,200,50]}]}`)
	checkPoints(t, "scaled", readPath(t, set, "page0/s1").Points, [][]float64{{0, 0}, {100, 50}, {200, 0}})
}

// TestPathInGroup checks that a path added in a group reads back at the
// view points the plan gave, through the group's frame.
func TestPathInGroup(t *testing.T) {
	path := vsdxtest.Fixture(t, "v4-groups")
	pts := [][]float64{{230, 440}, {260, 480}, {280, 450}}
	out, changed := refusalOut(t, path, `{"ops":[{"op":"object.add","at":"page0/s12","geom":"curve","points":`+pointsJSON(pts)+`}]}`)
	if len(changed) != 1 {
		t.Fatalf("changed = %v, want the new shape", changed)
	}
	var added struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal([]byte(changed[0]), &added); err != nil {
		t.Fatal(err)
	}
	checkPoints(t, "in rotated group", readPath(t, out, added.ID).Points, pts)
}
