package main

import (
	"bytes"
	"encoding/json"
	"image/png"
	"math"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/jhnoel/documentdriver/internal/ooxml/tree"
)

// figurePlan is a figure-like page, as a patent figure kit draws one:
// two labelled boxes (one with a bold word and a label that wraps), an
// arrowed connector glued between them, a hatched ellipse with a white
// background, a cross-hatched closed region with a label, a curve with
// an arrowhead, a dashed and a dotted line, and a left-aligned label
// with no outline.
const figurePlan = `{"ops":[
{"op":"object.add","at":"page0","geom":"rect","as":"a","box":[72,72,144,72],"html":"<p>Sensor unit 102</p>","line":{"w":1}},
{"op":"object.add","at":"page0","geom":"rect","as":"b","box":[360,72,180,72],"html":"<p><b>Controller</b> 104 with a long label that wraps</p>","line":{"w":1}},
{"op":"object.add","at":"page0","geom":"ellipse","box":[360,250,180,90],"html":"<p><i>Memory</i> 106</p>","fill":{"pattern":"hatch_up","bg":"#FFFFFF"},"line":{"w":1}},
{"op":"object.add","at":"page0","kind":"connector","from_glue":"$a:2","to_glue":"$b:4","line":{"w":1,"end":"triangle"}},
{"op":"object.add","at":"page0","geom":"polyline","closed":true,"points":[[80,420],[260,420],[230,520],[110,520]],"fill":{"pattern":"cross_hatch"},"line":{"w":1},"html":"<p>Region 108</p>"},
{"op":"object.add","at":"page0","geom":"curve","points":[[300,560],[360,480],[440,520],[520,440]],"line":{"w":1.5,"end":"triangle"}},
{"op":"object.add","at":"page0","kind":"connector","from":[72,620],"to":[540,620],"line":{"w":1,"dash":2}},
{"op":"object.add","at":"page0","kind":"connector","from":[72,660],"to":[540,660],"line":{"w":1,"dash":3}},
{"op":"object.add","at":"page0","geom":"rect","box":[72,690,200,50],"html":"<p style=\"text-align:left\">Left top label</p>","line":"none"}
]}`

// textBlockPlan is the text block page: labels at the top, middle and
// bottom of equal boxes, a left-aligned label with a 30 pt left and
// 20 pt top margin, a label with a white background on a hatch, and a
// diamond (a closed polyline) holding its own label.
const textBlockPlan = `{"ops":[
{"op":"object.add","at":"page0","geom":"rect","box":[40,40,160,110],"html":"<p>Top label</p>","text_block":{"valign":"top"}},
{"op":"object.add","at":"page0","geom":"rect","box":[220,40,160,110],"html":"<p>Middle label</p>","text_block":{"valign":"middle"}},
{"op":"object.add","at":"page0","geom":"rect","box":[400,40,160,110],"html":"<p>Bottom label</p>","text_block":{"valign":"bottom"}},
{"op":"object.add","at":"page0","geom":"rect","box":[40,190,240,110],"html":"<p style=\"text-align:left\">Margin label</p>","text_block":{"valign":"top","margins":[30,20,4,4]}},
{"op":"object.add","at":"page0","geom":"rect","box":[320,190,240,110],"html":"<p>Hatched 210</p>","fill":{"pattern":"hatch_up","bg":"#FFFFFF"},"text_block":{"background":"#FFFFFF"}},
{"op":"object.add","at":"page0","geom":"polyline","closed":true,"points":[[300,340],[420,420],[300,500],[180,420]],"html":"<p>Decide 212</p>","line":{"w":1}}
]}`

// placementPlan is the text placement page: a region title on its
// rect's top border with a white background, an arrow label above a
// horizontal connector, a numeral at the free end of a leader line,
// and a label turned 30 degrees.
const placementPlan = `{"ops":[
{"op":"object.add","at":"page0","geom":"rect","box":[60,80,300,160],"html":"<p>Region 300</p>","fill":{"pattern":"hatch_up","bg":"#FFFFFF"},"text_block":{"box":[110,72,200,16],"background":"#FFFFFF","margins":[2,0,2,0]}},
{"op":"object.add","at":"page0","kind":"connector","from":[60,320],"to":[360,320],"line":{"end":"triangle"},"html":"<p>signal 310</p>","text_block":{"box":[160,300,100,16]}},
{"op":"object.add","at":"page0","geom":"polyline","points":[[420,140],[480,100]],"html":"<p>312</p>","text_block":{"box":[482,86,30,18],"margins":0}},
{"op":"object.add","at":"page0","geom":"rect","box":[400,250,120,80],"html":"<p>turned 314</p>","text_block":{"box":[400,280,120,20],"angle":30}}
]}`

// groupPlan is the group page: the patent kit's labelled hatched box
// (a 1.5 pt outline rect carrying its label over 0.35 pt hatch lines
// clipped to it) grouped, then resized to most of the page.
const groupPlan = `{"ops":[
{"op":"object.add","at":"page0","geom":"polyline","points":[[100,160],[160,100]],"line":{"w":0.35},"as":"h1"},
{"op":"object.add","at":"page0","geom":"polyline","points":[[100,220],[220,100]],"line":{"w":0.35},"as":"h2"},
{"op":"object.add","at":"page0","geom":"polyline","points":[[160,220],[280,100]],"line":{"w":0.35},"as":"h3"},
{"op":"object.add","at":"page0","geom":"polyline","points":[[220,220],[300,140]],"line":{"w":0.35},"as":"h4"},
{"op":"object.add","at":"page0","geom":"rect","box":[100,100,200,120],"html":"<p>Memory 106</p>","line":{"w":1.5},"fill":"none","text_block":{"background":"#FFFFFF"},"as":"box"},
{"op":"group.create","at":["$h1","$h2","$h3","$h4","$box"],"as":"g"},
{"op":"object.set","at":"$g","box":[40,40,520,700]}
]}`

// svgPage is one of the drawing test's pages: its plan, the fill
// pattern of each patterned object, the arrowheads it draws, the
// words its text shows and, by object, the number of dashes its line
// is cut into at least.
type svgPage struct {
	name     string
	plan     string
	patterns map[string]string
	heads    int
	words    []string
	dashes   map[string]int
}

// svgPages returns the drawing test's five pages: the render test's
// plan (every named pattern and the four test shapes), the figure, the
// text block page, the text placement page and the group page.
func svgPages() []svgPage {
	_, _, bell, sine, arc := renderPathsInputs()
	paths := svgPage{name: "paths", plan: renderPathsPlan(bell, sine, arc), patterns: map[string]string{}, heads: 3}
	for i, n := range patternNames {
		paths.patterns["page0/s"+strconv.Itoa(i+1)] = n
	}
	paths.patterns["page0/s15"] = "hatch_up"
	paths.patterns["page0/s17"] = "hatch_up"
	paths.patterns["page0/s21"] = "cross_hatch"
	fig := svgPage{name: "figure", plan: figurePlan, heads: 2,
		patterns: map[string]string{"page0/s3": "hatch_up", "page0/s5": "cross_hatch"},
		words:    []string{"Sensor", "unit", "102", "Controller", "104", "wraps", "Memory", "106", "Region", "108", "Left", "label"},
		dashes:   map[string]int{"page0/s7": 40, "page0/s8": 80},
	}
	tb := svgPage{name: "text_block", plan: textBlockPlan,
		patterns: map[string]string{"page0/s5": "hatch_up"},
		words:    []string{"Top", "Middle", "Bottom", "Margin", "Hatched", "210", "Decide", "212"},
	}
	place := svgPage{name: "placement", plan: placementPlan, heads: 1,
		patterns: map[string]string{"page0/s1": "hatch_up"},
		words:    []string{"Region", "300", "signal", "310", "312", "turned", "314"},
	}
	group := svgPage{name: "group", plan: groupPlan, words: []string{"Memory", "106"}}
	return []svgPage{paths, fig, tb, place, group}
}

// TestReadSVGTextPlacement checks where the drawing view puts placed
// text blocks (placementPlan): the region title's line on the rect's
// top border (y 80) with a white background behind it that covers the
// border and is drawn first; the arrow label above its line (y 320)
// and centred on x 210; the numeral beside the leader's free end
// (497, 95); and the turned label's text rotated by 30 degrees.
func TestReadSVGTextPlacement(t *testing.T) {
	file := createDrawing(t, t.TempDir(), "placement", placementPlan)
	var stdout, stderr bytes.Buffer
	if code := run([]string{"read", file, "--page", "page0", "--format", "svg"}, &stdout, &stderr); code != 0 {
		t.Fatalf("read --format svg: exit %d: %s", code, stderr.String())
	}
	d, err := tree.ParseString(stdout.String())
	if err != nil {
		t.Fatalf("not well formed: %v", err)
	}
	texts := map[string]svgText{}
	matrix := map[string]string{}
	var bg [][2]float64
	bgFirst := false
	svgElements(d.Root(), func(n *tree.Node, group string) {
		switch n.Local() {
		case "path":
			if role, _ := n.Attr("data-role"); role == "text-background" && group == "page0/s1" {
				data, _ := n.Attr("d")
				bg = pathPoints(t, data)
				_, seen := texts[group]
				bgFirst = !seen
			}
		case "text":
			x, _ := n.Attr("x")
			y, _ := n.Attr("y")
			m, _ := n.Attr("transform")
			xf, _ := strconv.ParseFloat(x, 64)
			yf, _ := strconv.ParseFloat(y, 64)
			px, py := applyMatrix(t, m, xf, yf)
			texts[group] = svgText{group, px, py}
			matrix[group] = m
		}
	})
	title, label, num := texts["page0/s1"], texts["page0/s2"], texts["page0/s3"]
	// A 12 pt line is 14.4 pt high with its 12 pt text centred in it
	// and the baseline 0.8 of 12 below the text's top, so a line
	// centred on 80 has its baseline at 80 - 7.2 + 1.2 + 9.6 = 83.6.
	if math.Abs(title.x-210) > 0.5 || math.Abs(title.y-83.6) > 0.5 {
		t.Errorf("region title anchor (%.2f, %.2f), want (210, 83.6): centred on the top border", title.x, title.y)
	}
	if math.Abs(label.x-210) > 0.5 || label.y > 320-2 || label.y < 300 {
		t.Errorf("arrow label anchor (%.2f, %.2f), want x 210 and the baseline above the line at y 320", label.x, label.y)
	}
	if math.Abs(num.x-497) > 0.5 || math.Abs(num.y-98.6) > 0.5 {
		t.Errorf("numeral anchor (%.2f, %.2f), want (497, 98.6) beside the leader's end", num.x, num.y)
	}
	if len(bg) != 4 || !bgFirst {
		t.Fatalf("title background %v (drawn first %v), want a box drawn before the text", bg, bgFirst)
	}
	minY, maxY := bg[0][1], bg[0][1]
	for _, p := range bg {
		minY, maxY = min(minY, p[1]), max(maxY, p[1])
	}
	if !(minY < 80 && maxY > 80) {
		t.Errorf("title background y %.2f to %.2f does not cover the border at y 80", minY, maxY)
	}
	// rotate(30): the text matrix's first column is (cos 30, sin 30).
	ax, ay := applyMatrix(t, matrix["page0/s4"], 1, 0)
	ox, oy := applyMatrix(t, matrix["page0/s4"], 0, 0)
	if got := math.Atan2(ay-oy, ax-ox) * 180 / math.Pi; math.Abs(got-30) > 0.01 {
		t.Errorf("turned label at %.3f degrees, want 30", got)
	}
}

// svgText is one text line of the drawing: its object and its anchor
// on the page.
type svgText struct {
	group string
	x, y  float64
}

// TestReadSVGTextBlock checks where the drawing view puts text-block
// text (textBlockPlan): the top, middle and bottom labels in that order
// within equal boxes, each inside its box; the margin label starting
// at least 30 pt from its box's left edge and 20 pt below its top; the
// white background under the hatched label covering the label's anchor
// and lying inside its box, drawn before the text; and the diamond's
// label inside the diamond's bounding box, near its centre (its text
// block is the bounding box).
func TestReadSVGTextBlock(t *testing.T) {
	file := createDrawing(t, t.TempDir(), "text_block", textBlockPlan)
	var stdout, stderr bytes.Buffer
	if code := run([]string{"read", file, "--page", "page0", "--format", "svg"}, &stdout, &stderr); code != 0 {
		t.Fatalf("read --format svg: exit %d: %s", code, stderr.String())
	}
	d, err := tree.ParseString(stdout.String())
	if err != nil {
		t.Fatalf("not well formed: %v", err)
	}
	texts := map[string]svgText{}
	var bg [][2]float64
	bgBeforeText := false
	svgElements(d.Root(), func(n *tree.Node, group string) {
		switch n.Local() {
		case "path":
			if role, _ := n.Attr("data-role"); role == "text-background" && group == "page0/s5" {
				data, _ := n.Attr("d")
				bg = pathPoints(t, data)
				_, seen := texts["page0/s5"]
				bgBeforeText = !seen
			}
		case "text":
			x, _ := n.Attr("x")
			y, _ := n.Attr("y")
			m, _ := n.Attr("transform")
			xf, _ := strconv.ParseFloat(x, 64)
			yf, _ := strconv.ParseFloat(y, 64)
			px, py := applyMatrix(t, m, xf, yf)
			texts[group] = svgText{group, px, py}
		}
	})
	for _, id := range []string{"page0/s1", "page0/s2", "page0/s3", "page0/s4", "page0/s5", "page0/s6"} {
		if _, ok := texts[id]; !ok {
			t.Fatalf("no text for %s", id)
		}
	}
	top, mid, bot := texts["page0/s1"].y-40, texts["page0/s2"].y-40, texts["page0/s3"].y-40
	if !(top < mid && mid < bot) || top < 4 || bot > 110-4 {
		t.Errorf("baselines from the box tops: top %.1f, middle %.1f, bottom %.1f; want increasing, inside the 4 pt margins of a 110 pt box", top, mid, bot)
	}
	if mt := texts["page0/s4"]; mt.x < 40+30 || mt.y < 190+20 {
		t.Errorf("margin label anchor (%.1f, %.1f), want x at least 70 and y at least 210", mt.x, mt.y)
	}
	if len(bg) != 4 {
		t.Fatalf("text background points %v, want a 4-point box", bg)
	}
	minX, minY, maxX, maxY := bg[0][0], bg[0][1], bg[0][0], bg[0][1]
	for _, p := range bg {
		minX, minY, maxX, maxY = min(minX, p[0]), min(minY, p[1]), max(maxX, p[0]), max(maxY, p[1])
	}
	ht := texts["page0/s5"]
	if ht.y < minY || ht.y > maxY || ht.x < minX || ht.x > maxX {
		t.Errorf("hatched label anchor (%.1f, %.1f) outside its background [%.1f %.1f %.1f %.1f]", ht.x, ht.y, minX, minY, maxX, maxY)
	}
	if minX < 320 || maxX > 560 || minY < 190 || maxY > 300 || maxX-minX > 200 {
		t.Errorf("background [%.1f %.1f %.1f %.1f], want inside the box [320 190 560 300] and around the text, not the whole box", minX, minY, maxX, maxY)
	}
	if !bgBeforeText {
		t.Error("the text background is drawn after the text")
	}
	if dt := texts["page0/s6"]; dt.x < 180 || dt.x > 420 || dt.y < 400 || dt.y > 440 {
		t.Errorf("diamond label anchor (%.1f, %.1f), want near the centre (300, 420) of its box", dt.x, dt.y)
	}
}

// createDrawing writes plan's drawing in dir and returns its path.
func createDrawing(t *testing.T, dir, name, plan string) string {
	t.Helper()
	planPath := filepath.Join(dir, name+".json")
	if err := os.WriteFile(planPath, []byte(plan), 0o600); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(dir, name+".vsdx")
	var stdout, stderr bytes.Buffer
	if code := run([]string{"create", planPath, "-o", file}, &stdout, &stderr); code != 0 {
		t.Fatalf("create %s: exit %d: %s", name, code, stderr.String())
	}
	return file
}

// svgElements walks the SVG document's elements in document order,
// iteratively.
func svgElements(root *tree.Node, visit func(n *tree.Node, group string)) {
	type item struct {
		n     *tree.Node
		group string
	}
	stack := []item{{root, ""}}
	for len(stack) > 0 {
		it := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		group := it.group
		if it.n.Local() == "g" {
			if id, ok := it.n.Attr("id"); ok {
				group = id
			}
		}
		visit(it.n, group)
		var kids []*tree.Node
		for c := range it.n.ElementsSeq() {
			kids = append(kids, c)
		}
		for _, kid := range slices.Backward(kids) {
			stack = append(stack, item{kid, group})
		}
	}
}

// pathPoints returns the coordinate pairs of SVG path data.
func pathPoints(t *testing.T, d string) [][2]float64 {
	t.Helper()
	var nums []float64
	for _, f := range strings.FieldsFunc(d, func(r rune) bool { return r == ' ' || r == 'M' || r == 'L' || r == 'C' || r == 'Z' }) {
		v, err := strconv.ParseFloat(f, 64)
		if err != nil {
			t.Fatalf("path data %q: %v", d, err)
		}
		nums = append(nums, v)
	}
	if len(nums)%2 != 0 {
		t.Fatalf("path data %q has an odd number of coordinates", d)
	}
	var out [][2]float64
	for i := 0; i < len(nums); i += 2 {
		out = append(out, [2]float64{nums[i], nums[i+1]})
	}
	return out
}

// applyMatrix applies an SVG matrix(a b c d e f) transform to (x, y).
func applyMatrix(t *testing.T, m string, x, y float64) (float64, float64) {
	t.Helper()
	if m == "" {
		return x, y
	}
	f := strings.Fields(strings.TrimSuffix(strings.TrimPrefix(m, "matrix("), ")"))
	if len(f) != 6 {
		t.Fatalf("transform %q is not a matrix", m)
	}
	var v [6]float64
	for i, s := range f {
		n, err := strconv.ParseFloat(s, 64)
		if err != nil {
			t.Fatalf("transform %q: %v", m, err)
		}
		v[i] = n
	}
	return v[0]*x + v[2]*y + v[4], v[1]*x + v[3]*y + v[5]
}

// TestReadSVGStructure checks read --format svg on the two test pages
// without any renderer: the output is a well-formed svg of the page's
// size in points with no notes; every patterned object draws its
// pattern as a path whose data-pattern is the plan's name; the
// arrowheads the plans ask for are filled paths; the text shows every
// label word; the dashed and dotted lines are cut into dashes; no
// element uses stroke-dasharray, markers, patterns or clip paths; and
// every path point and text anchor lies inside the page.
func TestReadSVGStructure(t *testing.T) {
	dir := t.TempDir()
	for _, pg := range svgPages() {
		t.Run(pg.name, func(t *testing.T) {
			file := createDrawing(t, dir, pg.name, pg.plan)
			var stdout, stderr bytes.Buffer
			if code := run([]string{"read", file, "--page", "page0", "--format", "svg"}, &stdout, &stderr); code != 0 {
				t.Fatalf("read --format svg: exit %d: %s", code, stderr.String())
			}
			out := stdout.String()
			for _, banned := range []string{"stroke-dasharray", "<marker", "marker-end", "<pattern", "url(#", "clip-path"} {
				if strings.Contains(out, banned) {
					t.Errorf("output uses %s", banned)
				}
			}
			d, err := tree.ParseString(out)
			if err != nil {
				t.Fatalf("not well formed: %v", err)
			}
			root := d.Root()
			if root.Local() != "svg" {
				t.Fatalf("root %s, want svg", root.Local())
			}
			for name, want := range map[string]string{"width": "612", "height": "792", "viewBox": "0 0 612 792", "data-page": "page0", "data-units": "pt", "data-notes": "[]"} {
				if got, _ := root.Attr(name); got != want {
					t.Errorf("root %s = %q, want %q", name, got, want)
				}
			}
			patterns := map[string]string{}
			heads := 0
			dashes := map[string]int{}
			var text []string
			svgElements(root, func(n *tree.Node, group string) {
				role, _ := n.Attr("data-role")
				switch n.Local() {
				case "path":
					data, _ := n.Attr("d")
					for _, p := range pathPoints(t, data) {
						if p[0] < -0.01 || p[0] > 612.01 || p[1] < -0.01 || p[1] > 792.01 {
							t.Errorf("%s %s: point %v outside the page", group, role, p)
							break
						}
					}
					switch role {
					case "pattern":
						if data == "" {
							t.Errorf("%s: empty pattern path", group)
						}
						patterns[group], _ = n.Attr("data-pattern")
					case "arrowhead":
						if fill, _ := n.Attr("fill"); fill == "none" || !strings.HasSuffix(data, "Z") {
							t.Errorf("%s: arrowhead not a filled closed path", group)
						}
						heads++
					case "line":
						dashes[group] = strings.Count(data, "M")
					}
				case "text":
					x, _ := n.Attr("x")
					y, _ := n.Attr("y")
					m, _ := n.Attr("transform")
					xf, _ := strconv.ParseFloat(x, 64)
					yf, _ := strconv.ParseFloat(y, 64)
					px, py := applyMatrix(t, m, xf, yf)
					if px < 0 || px > 612 || py < 0 || py > 792 {
						t.Errorf("%s: text anchor (%.1f, %.1f) outside the page", group, px, py)
					}
					for c := range n.ElementsSeq() {
						text = append(text, c.Text())
					}
				}
			})
			for id, want := range pg.patterns {
				if patterns[id] != want {
					t.Errorf("%s: pattern %q, want %q", id, patterns[id], want)
				}
			}
			if heads != pg.heads {
				t.Errorf("%d arrowheads, want %d", heads, pg.heads)
			}
			all := strings.Join(text, " ")
			for _, w := range pg.words {
				if !strings.Contains(all, w) {
					t.Errorf("text %q lacks %q", all, w)
				}
			}
			for id, want := range pg.dashes {
				if dashes[id] < want {
					t.Errorf("%s: line in %d pieces, want at least %d dashes", id, dashes[id], want)
				}
			}
		})
	}
}

// TestReadSVGUsage checks the flag rules of --format svg: it needs
// --page or --master, and refuses --detail and --units.
func TestReadSVGUsage(t *testing.T) {
	file := createDrawing(t, t.TempDir(), "fig", figurePlan)
	for _, args := range [][]string{
		{"read", file, "--format", "svg"},
		{"read", file, "--page", "page0", "--format", "svg", "--detail", "full"},
		{"read", file, "--page", "page0", "--format", "svg", "--units", "in"},
	} {
		var stdout, stderr bytes.Buffer
		if code := run(args, &stdout, &stderr); code != 2 || !strings.Contains(stderr.String(), `"code":"usage"`) {
			t.Errorf("%v: exit %d %s, want usage", args[2:], code, stderr.String())
		}
	}
}

// TestRenderAutoWithoutSoffice checks that render's default renderer
// draws with the svg renderer when LibreOffice cannot run (DD_SOFFICE
// names a missing file and PATH has no soffice): exit 0, a PNG of the
// page at the scale, and a result naming renderer svg with notes.
func TestRenderAutoWithoutSoffice(t *testing.T) {
	t.Setenv("DD_SOFFICE", filepath.Join(t.TempDir(), "no-soffice"))
	t.Setenv("PATH", t.TempDir())
	dir := t.TempDir()
	file := createDrawing(t, dir, "fig", figurePlan)
	out := filepath.Join(dir, "fig.png")
	var stdout, stderr bytes.Buffer
	if code := run([]string{"render", file, "--page", "page0", "-o", out}, &stdout, &stderr); code != 0 {
		t.Fatalf("render: exit %d: %s", code, stderr.String())
	}
	var res struct {
		Px       [2]int            `json:"px"`
		Renderer string            `json:"renderer"`
		Notes    []json.RawMessage `json:"notes"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &res); err != nil {
		t.Fatal(err)
	}
	if res.Renderer != "svg" || res.Px != [2]int{918, 1188} || res.Notes == nil {
		t.Errorf("result %s: want renderer svg, px [918 1188] and notes", stdout.String())
	}
	if _, _, at := decodeRenderPNG(t, out); func() bool { r, _, _ := at(144, 108); return r != 255 }() {
		t.Errorf("inside the first box is not white")
	}
}

// inkMask renders file with renderer at scale 2 and returns its ink
// mask (a pixel whose r+g+b is under 600, the render test's "dark")
// and size.
func inkMask(t *testing.T, file, renderer string) ([]bool, int, int) {
	t.Helper()
	out := filepath.Join(t.TempDir(), renderer+".png")
	var stdout, stderr bytes.Buffer
	if code := run([]string{"render", file, "--page", "page0", "--scale", "2", "--renderer", renderer, "-o", out}, &stdout, &stderr); code != 0 {
		t.Fatalf("render %s: exit %d: %s", renderer, code, stderr.String())
	}
	if keep := os.Getenv("DD_RENDER_KEEP"); keep != "" {
		b, err := os.ReadFile(out) //nolint:gosec // G304: a picture the test just wrote
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(keep, strings.TrimSuffix(filepath.Base(file), ".vsdx")+"-"+renderer+".png"), b, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	f, err := os.Open(out) //nolint:gosec // G304: a picture the test just wrote
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	img, err := png.Decode(f)
	if err != nil {
		t.Fatal(err)
	}
	b := img.Bounds()
	mask := make([]bool, b.Dx()*b.Dy())
	for y := range b.Dy() {
		for x := range b.Dx() {
			r, g, bl, _ := img.At(b.Min.X+x, b.Min.Y+y).RGBA()
			mask[y*b.Dx()+x] = r>>8+g>>8+bl>>8 < 600
		}
	}
	return mask, b.Dx(), b.Dy()
}

// unmatched returns the share of a's ink pixels with no ink of b
// within r pixels (a square neighbourhood).
func unmatched(a, b []bool, w, h, r int) float64 {
	// near[i] is true when b has ink within r of i: a box dilation,
	// rows then columns.
	rows := make([]bool, len(b))
	for y := range h {
		last := -1 << 30
		for x := range w {
			if b[y*w+x] {
				last = x
			}
			rows[y*w+x] = x-last <= r
		}
		last = 1 << 30
		for x := w - 1; x >= 0; x-- {
			if b[y*w+x] {
				last = x
			}
			if last-x <= r {
				rows[y*w+x] = true
			}
		}
	}
	near := make([]bool, len(b))
	for x := range w {
		last := -1 << 30
		for y := range h {
			if rows[y*w+x] {
				last = y
			}
			near[y*w+x] = y-last <= r
		}
		last = 1 << 30
		for y := h - 1; y >= 0; y-- {
			if rows[y*w+x] {
				last = y
			}
			if last-y <= r {
				near[y*w+x] = true
			}
		}
	}
	ink, miss := 0, 0
	for i, v := range a {
		if v {
			ink++
			if !near[i] {
				miss++
			}
		}
	}
	if ink == 0 {
		return 0
	}
	return float64(miss) / float64(ink)
}

// svgMatchTolerance is how far apart, in points, the two renderers'
// marks may lie and still match, and svgMatchMax the share of either
// renderer's ink that may have no match. Measured at scale 2 on the
// two pages (LibreOffice 26.2, 2026-11-25), the unmatched share falls
// from about 23% at 0.5 pt and 8% at 1 pt (antialiasing and the
// different fonts: Carlito in LibreOffice, scaled bitmap glyphs in
// Go) to 2.1% and 2.2% at 2 pt on the patterns page (hatches drawn at
// another phase: LibreOffice starts its hatch lines elsewhere, and
// draws dots as a triple hatch) and 0.05% and 0.5% on the figure, and
// to nothing at 4 pt, past half the coarse hatch spacing, where any
// hatch matches any other. So 2 pt is the tolerance and 4%, about
// twice the largest measured share, the bound: a fill pattern either
// renderer leaves out puts its whole inside out of reach. A missing
// arrowhead is mostly within 2 pt of its line, so the arrowheads are
// checked by count in TestReadSVGStructure, not here.
const (
	svgMatchTolerance = 2.0
	svgMatchMax       = 0.04
)

// TestRenderSVGMatchesLibreOffice compares the svg renderer with
// LibreOffice on the two test pages at scale 2: each renderer's ink
// must lie within svgMatchTolerance of the other's for all but
// svgMatchMax of it. LibreOffice is skipped where it is absent.
func TestRenderSVGMatchesLibreOffice(t *testing.T) {
	needRenderer(t)
	dir := t.TempDir()
	for _, pg := range svgPages() {
		t.Run(pg.name, func(t *testing.T) {
			file := createDrawing(t, dir, pg.name, pg.plan)
			lo, w, h := inkMask(t, file, "libreoffice")
			gosvg, w2, h2 := inkMask(t, file, "svg")
			if w != w2 || h != h2 {
				t.Fatalf("sizes %dx%d and %dx%d differ", w, h, w2, h2)
			}
			count := func(m []bool) int {
				n := 0
				for _, v := range m {
					if v {
						n++
					}
				}
				return n
			}
			// A page whose renders hold little ink would match
			// trivially: each must ink at least 1% of the page.
			if count(lo) < w*h/100 || count(gosvg) < w*h/100 {
				t.Fatalf("ink pixels: LibreOffice %d, svg %d of %d, want at least 1%% each", count(lo), count(gosvg), w*h)
			}
			r := int(svgMatchTolerance * 2)
			loMiss, svgMiss := unmatched(lo, gosvg, w, h, r), unmatched(gosvg, lo, w, h, r)
			t.Logf("%s: ink %d and %d pixels; %.2f%% of LibreOffice's unmatched, %.2f%% of svg's", pg.name, count(lo), count(gosvg), loMiss*100, svgMiss*100)
			if loMiss > svgMatchMax || svgMiss > svgMatchMax {
				t.Errorf("%s: unmatched ink LibreOffice %.2f%%, svg %.2f%%, want at most %.0f%% each", pg.name, loMiss*100, svgMiss*100, svgMatchMax*100)
			}
		})
	}
}
