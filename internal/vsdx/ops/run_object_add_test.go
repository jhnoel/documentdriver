package ops_test

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/jhnoel/documentdriver/internal/cli"
	"github.com/jhnoel/documentdriver/internal/laws"
	"github.com/jhnoel/documentdriver/internal/repo"
	"github.com/jhnoel/documentdriver/internal/vsdx/apply"
	"github.com/jhnoel/documentdriver/internal/vsdx/create"
	"github.com/jhnoel/documentdriver/internal/vsdx/edit"
	"github.com/jhnoel/documentdriver/internal/vsdx/ops"
	"github.com/jhnoel/documentdriver/internal/vsdx/read"
	"github.com/jhnoel/documentdriver/internal/vsdx/vsdxtest"
)

// blankFile writes the blank drawing of the fixed clock to a fresh
// file and returns its path.
func blankFile(t *testing.T) string {
	t.Helper()
	b, err := create.Template(time.Date(2000, time.January, 1, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("Template: %v", err)
	}
	path := filepath.Join(t.TempDir(), "blank.vsdx")
	if err := os.WriteFile(path, b, 0o600); err != nil { //nolint:gosec // G306: temp file
		t.Fatalf("write blank: %v", err)
	}
	return path
}

// parsePlan decodes the plan text or fails the test.
func parsePlan(t *testing.T, text string) *ops.Plan {
	t.Helper()
	p, errs := ops.ParsePlan([]byte(text))
	if len(errs) != 0 || p == nil {
		t.Fatalf("ParsePlan errors %v", errs)
	}
	return p
}

// pageJSON is the decoded page of read.Page.
type pageJSON struct {
	Objects []struct {
		ID       string          `json:"id"`
		Kind     string          `json:"kind"`
		Name     string          `json:"name"`
		Master   string          `json:"master"`
		Box      []json.Number   `json:"box"`
		From     []json.Number   `json:"from"`
		To       []json.Number   `json:"to"`
		FromGlue string          `json:"from_glue"`
		ToGlue   string          `json:"to_glue"`
		HTML     string          `json:"html"`
		Text     string          `json:"text"`
		Fill     json.RawMessage `json:"fill"`
		Line     json.RawMessage `json:"line"`
	} `json:"objects"`
	Connections []struct {
		Connector string `json:"connector"`
		From      string `json:"from"`
		To        string `json:"to"`
		FromGlue  string `json:"from_glue"`
		ToGlue    string `json:"to_glue"`
	} `json:"connections"`
	Hazards []json.RawMessage `json:"hazards"`
}

// readPage reads page0 of the drawing at path through read.Page.
func readPage(t *testing.T, path string) pageJSON {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	raw, err := read.Page(b, "base", "page0", read.Options{Units: "pt"})
	if err != nil {
		t.Fatalf("read.Page: %v", err)
	}
	var page pageJSON
	if err := json.Unmarshal(raw, &page); err != nil {
		t.Fatalf("decode page: %v", err)
	}
	return page
}

// findObject returns the object with address id or fails the test.
func findObject(t *testing.T, page pageJSON, id string) int {
	t.Helper()
	for i := range page.Objects {
		if page.Objects[i].ID == id {
			return i
		}
	}
	t.Fatalf("no object %s in %d objects", id, len(page.Objects))
	return -1
}

// checkBox checks the object's box numbers to 1e-6.
func checkBox(t *testing.T, name string, got []json.Number, want [4]float64) {
	t.Helper()
	if len(got) != 4 {
		t.Fatalf("%s: box %v, want %v", name, got, want)
	}
	for i := range want {
		f, err := got[i].Float64()
		if err != nil || f < want[i]-1e-6 || f > want[i]+1e-6 {
			t.Errorf("%s: box %v, want %v", name, got, want)
			return
		}
	}
}

// checkPt checks a from/to point to 1e-6.
func checkPt(t *testing.T, name string, got []json.Number, want [2]float64) {
	t.Helper()
	if len(got) != 2 {
		t.Fatalf("%s: point %v, want %v", name, got, want)
	}
	for i := range want {
		f, err := got[i].Float64()
		if err != nil || f < want[i]-1e-6 || f > want[i]+1e-6 {
			t.Errorf("%s: point %v, want %v", name, got, want)
			return
		}
	}
}

// golden reads one c2/c3 golden input from the repository.
func golden(t *testing.T, rel string) string {
	t.Helper()
	root, err := repo.Root()
	if err != nil {
		t.Fatalf("repo root: %v", err)
	}
	b, err := os.ReadFile(filepath.Join(root, "testdata", "vsdx", rel))
	if err != nil {
		t.Fatalf("read %s: %v", rel, err)
	}
	return string(b)
}

// stripText removes the Text element and the Character and Paragraph
// sections from one shape's XML, leaving the rest for comparison.
func stripText(xml string) string {
	out := xml
	for _, tag := range []string{"<Text>", "<Section N='Character'>", "<Section N='Paragraph'>"} {
		startTag := tag
		endTag := "</Section>"
		if tag == "<Text>" {
			endTag = "</Text>"
		}
		for {
			start := strings.Index(out, startTag)
			if start < 0 {
				break
			}
			end := strings.Index(out[start:], endTag)
			if end < 0 {
				break
			}
			out = out[:start] + out[start+end+len(endTag):]
		}
	}
	return out
}

// TestObjectAddC2 checks the OA1 row: plan c2 without its pending
// prop.add on the blank gives s1, s2 and s3 as read shows them, the
// aliases, no hazards, and the goldens modulo text.
func TestObjectAddC2(t *testing.T) {
	blank := blankFile(t)
	out := filepath.Join(t.TempDir(), "out.vsdx")
	plan := parsePlan(t, `{"ops":[
 {"op":"object.add","at":"page0","geom":"rect","as":"a","box":[72,72,144,72],"html":"<p>Start</p>"},
 {"op":"object.add","at":"page0","geom":"ellipse","as":"b","box":[360,72,144,72],"html":"<p><b>End</b></p>"},
 {"op":"object.add","at":"page0","kind":"connector","as":"c","from_glue":"$a:2","to_glue":"$b:4"}
]}`)
	res, err := apply.Run(blank, plan, apply.Options{Out: out})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	wantAliases := map[string]string{"a": "page0/s1", "b": "page0/s2", "c": "page0/s3"}
	if len(res.Aliases) != len(wantAliases) {
		t.Fatalf("aliases %v, want %v", res.Aliases, wantAliases)
	}
	for k, v := range wantAliases {
		if res.Aliases[k] != v {
			t.Errorf("alias $%s = %q, want %q", k, res.Aliases[k], v)
		}
	}
	if len(res.Hazards) != 0 {
		t.Errorf("hazards %v, want one? want none", res.Hazards)
	}
	page := readPage(t, out)
	if len(page.Hazards) != 0 {
		t.Errorf("page hazards %v, want none", page.Hazards)
	}
	if len(page.Objects) != 3 {
		t.Fatalf("objects %d, want 3", len(page.Objects))
	}
	i1 := findObject(t, page, "page0/s1")
	if page.Objects[i1].Kind != "shape" {
		t.Errorf("s1 kind %q, want shape", page.Objects[i1].Kind)
	}
	checkBox(t, "s1", page.Objects[i1].Box, [4]float64{72, 72, 144, 72})
	if page.Objects[i1].HTML != "<p>Start</p>" {
		t.Errorf("s1 html %q, want <p>Start</p>", page.Objects[i1].HTML)
	}
	i2 := findObject(t, page, "page0/s2")
	if page.Objects[i2].Kind != "shape" {
		t.Errorf("s2 kind %q, want shape", page.Objects[i2].Kind)
	}
	checkBox(t, "s2", page.Objects[i2].Box, [4]float64{360, 72, 144, 72})
	if page.Objects[i2].HTML != "<p><b>End</b></p>" {
		t.Errorf("s2 html %q, want <p><b>End</b></p>", page.Objects[i2].HTML)
	}
	i3 := findObject(t, page, "page0/s3")
	if page.Objects[i3].Kind != "connector" {
		t.Errorf("s3 kind %q, want connector", page.Objects[i3].Kind)
	}
	checkPt(t, "s3 from", page.Objects[i3].From, [2]float64{216, 108})
	checkPt(t, "s3 to", page.Objects[i3].To, [2]float64{360, 108})
	if page.Objects[i3].FromGlue != "s1:2" || page.Objects[i3].ToGlue != "s2:4" {
		t.Errorf("s3 glue %q %q, want s1:2 s2:4", page.Objects[i3].FromGlue, page.Objects[i3].ToGlue)
	}
	if len(page.Connections) != 1 {
		t.Fatalf("connections %d, want one", len(page.Connections))
	}
	conn := page.Connections[0]
	if conn.Connector != "page0/s3" || conn.From != "page0/s1" || conn.To != "page0/s2" || conn.FromGlue != "s1:2" || conn.ToGlue != "s2:4" {
		t.Errorf("connection %+v, want page0/s3 s1 s2 s1:2 s2:4", conn)
	}
	// The page part's elements equal the goldens except the inserted
	// Text and sections.
	b, err := os.ReadFile(out)
	if err != nil {
		t.Fatalf("read out: %v", err)
	}
	for _, tc := range []struct{ addr, file string }{
		{"page0/s1", "create/emit/rect.xml"},
		{"page0/s2", "create/emit/ellipse.xml"},
		{"page0/s3", "create/emit/connector.xml"},
	} {
		raw, err := read.XML(b, tc.addr)
		if err != nil {
			t.Fatalf("XML(%s): %v", tc.addr, err)
		}
		if got, want := stripText(string(raw)), golden(t, tc.file); got != want {
			t.Errorf("XML(%s) without text = %s\nwant %s", tc.addr, got, want)
		}
	}
}

// TestObjectAddMasterMissing checks the c4 row: object.add with a
// master on the blank is not_found at ops[0].master.
func TestObjectAddMasterMissing(t *testing.T) {
	blank := blankFile(t)
	before, err := os.ReadFile(blank)
	if err != nil {
		t.Fatalf("read blank: %v", err)
	}
	_, err = apply.Run(blank, parsePlan(t, `{"ops":[{"op":"object.add","at":"page0","master":"master2","pin":[100,100]}]}`), apply.Options{Out: filepath.Join(t.TempDir(), "out.vsdx")})
	var ce *cli.Error
	if !errors.As(err, &ce) {
		t.Fatalf("error %v, want *cli.Error", err)
	}
	if ce.Code != cli.CodeNotFound || ce.Path != "ops[0].master" {
		t.Errorf("got %s at %q, want not_found at ops[0].master", ce.Code, ce.Path)
	}
	after, _ := os.ReadFile(blank)
	if string(after) != string(before) {
		t.Error("a refused plan changed the file")
	}
}

// TestObjectAddWholeGlue checks the c5 row: whole-shape glue on a
// master-less connector is operation_failed at ops[1].to_glue with a
// hint naming connection points.
func TestObjectAddWholeGlue(t *testing.T) {
	blank := blankFile(t)
	_, err := apply.Run(blank, parsePlan(t, `{"ops":[
 {"op":"object.add","at":"page0","geom":"rect","as":"a","box":[72,72,144,72]},
 {"op":"object.add","at":"page0","kind":"connector","from":[0,0],"to_glue":"$a"}
]}`), apply.Options{Out: filepath.Join(t.TempDir(), "out.vsdx")})
	var ce *cli.Error
	if !errors.As(err, &ce) {
		t.Fatalf("error %v, want *cli.Error", err)
	}
	if ce.Code != cli.CodeOperationFailed || ce.Path != "ops[1].to_glue" {
		t.Errorf("got %s at %q, want operation_failed at ops[1].to_glue", ce.Code, ce.Path)
	}
	if !strings.Contains(strings.ToLower(ce.Hint), "connection point") {
		t.Errorf("hint %q does not name connection points", ce.Hint)
	}
}

// TestObjectAddGluePointMissing checks the OA1 row: to_glue $a:9 on
// a shape with four connection points is not_found at ops[1].to_glue.
func TestObjectAddGluePointMissing(t *testing.T) {
	blank := blankFile(t)
	_, err := apply.Run(blank, parsePlan(t, `{"ops":[
 {"op":"object.add","at":"page0","geom":"rect","as":"a","box":[72,72,144,72]},
 {"op":"object.add","at":"page0","kind":"connector","from":[0,0],"to_glue":"$a:9"}
]}`), apply.Options{Out: filepath.Join(t.TempDir(), "out.vsdx")})
	var ce *cli.Error
	if !errors.As(err, &ce) {
		t.Fatalf("error %v, want *cli.Error", err)
	}
	if ce.Code != cli.CodeNotFound || ce.Path != "ops[1].to_glue" {
		t.Errorf("got %s at %q, want not_found at ops[1].to_glue", ce.Code, ce.Path)
	}
}

// TestObjectAddFromMaster checks adding an instance of master2 with
// a pin on V2, whose size the master supplies.
func TestObjectAddFromMaster(t *testing.T) {
	path := vsdxtest.Fixture(t, "v2-master")
	out := filepath.Join(t.TempDir(), "out.vsdx")
	res, err := apply.Run(path, parsePlan(t, `{"ops":[{"op":"object.add","at":"page0","master":"master2","as":"m","pin":[144,126]}]}`), apply.Options{Out: out})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	addr := res.Aliases["m"]
	if addr == "" {
		t.Fatalf("no alias for the new shape, aliases %v", res.Aliases)
	}
	b, err := os.ReadFile(out)
	if err != nil {
		t.Fatalf("read out: %v", err)
	}
	raw, err := read.Page(b, "base", "page0", read.Options{Units: "pt"})
	if err != nil {
		t.Fatalf("read.Page: %v", err)
	}
	var page pageJSON
	if err := json.Unmarshal(raw, &page); err != nil {
		t.Fatalf("decode page: %v", err)
	}
	i := findObject(t, page, addr)
	if page.Objects[i].Master != "master2" {
		t.Errorf("master %q, want master2", page.Objects[i].Master)
	}
	// Master2 is 1 x 0.75 in at its centre pin 144,126: the view box
	// is 72 x 54 with centre there.
	checkBox(t, "master instance", page.Objects[i].Box, [4]float64{108, 99, 72, 54})
}

// master1DCell returns the V and F of the first shape-level Cell
// named name in one shape's XML, in either quote style the edit
// layer or the corpus writes, failing the test when it is absent.
func master1DCell(t *testing.T, xml, name string) (string, string) {
	t.Helper()
	for _, marker := range []string{"<Cell N='" + name + "'", `<Cell N="` + name + `"`} {
		start := strings.Index(xml, marker)
		if start < 0 {
			continue
		}
		rest := xml[start:]
		if end := strings.Index(rest, "/>"); end >= 0 {
			rest = rest[:end]
		}
		attr := func(key string) string {
			for _, q := range []byte{'\'', '"'} {
				needle := key + "=" + string(q)
				if i := strings.Index(rest, needle); i >= 0 {
					v := rest[i+len(needle):]
					if before, _, ok := strings.Cut(v, string(q)); ok {
						return before
					}
				}
			}
			return ""
		}
		return attr("V"), attr("F")
	}
	t.Fatalf("no Cell %s in %s", name, xml)
	return "", ""
}

// TestObjectAdd1DMaster checks the CC3 row: object.add master
// "Dynamic connector" (and master97) on Connectors with
// from_glue s40:2 and to_glue s41:1 reads back from [266.5,
// 72.01] to [347.09, 72.01] with that glue and master master97,
// its BeginX..EndY cells equal page0/s42's in V and F, its Width
// is 1.1193 within 1e-9, its PinX/PinY/LocPin cells are Inh with
// a V, and it reports stale-values "<new address> route".
func TestObjectAdd1DMaster(t *testing.T) {
	for _, tc := range []struct{ name, master string }{
		{"name", "Dynamic connector"},
		{"address", "master97"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := connectorCorpusFile(t, connectorJsFile)
			out := filepath.Join(t.TempDir(), "out.vsdx")
			plan := `{"ops":[{"op":"object.add","at":"page0","master":"` + tc.master + `","from_glue":"s40:2","to_glue":"s41:1","as":"c"}]}`
			res, err := apply.Run(path, parsePlan(t, plan), apply.Options{Out: out})
			if err != nil {
				t.Fatalf("Run: %v", err)
			}
			addr := res.Aliases["c"]
			if addr == "" {
				t.Fatalf("no alias for the new connector, aliases %v", res.Aliases)
			}
			foundStale := false
			for _, h := range res.Hazards {
				if h.Kind == "stale-values" && h.Detail == addr+" route" {
					foundStale = true
				}
			}
			if !foundStale {
				t.Errorf("hazards %+v, want stale-values %s route", res.Hazards, addr)
			}
			page := readPage(t, out)
			i := findObject(t, page, addr)
			obj := page.Objects[i]
			if obj.Kind != "connector" {
				t.Errorf("kind %q, want connector", obj.Kind)
			}
			if obj.Master != "master97" {
				t.Errorf("master %q, want master97", obj.Master)
			}
			checkPt(t, "new from", obj.From, [2]float64{266.5, 72.01})
			checkPt(t, "new to", obj.To, [2]float64{347.09, 72.01})
			if obj.FromGlue != "s40:2" || obj.ToGlue != "s41:1" {
				t.Errorf("glue %q %q, want s40:2 s41:1", obj.FromGlue, obj.ToGlue)
			}
			foundConn := false
			for _, c := range page.Connections {
				if c.Connector == addr {
					foundConn = true
					if c.From != "page0/s40" || c.To != "page0/s41" || c.FromGlue != "s40:2" || c.ToGlue != "s41:1" {
						t.Errorf("connection %+v, want %s page0/s40 page0/s41 s40:2 s41:1", c, addr)
					}
				}
			}
			if !foundConn {
				t.Errorf("connections %v have no %s", page.Connections, addr)
			}
			newXML := shapeXML(t, out, addr)
			s42XML := shapeXML(t, out, "page0/s42")
			for _, cell := range []string{"BeginX", "BeginY", "EndX", "EndY"} {
				newV, newF := master1DCell(t, newXML, cell)
				wantV, wantF := master1DCell(t, s42XML, cell)
				newNum, e1 := strconv.ParseFloat(newV, 64)
				wantNum, e2 := strconv.ParseFloat(wantV, 64)
				if e1 != nil || e2 != nil || newNum < wantNum-1e-9 || newNum > wantNum+1e-9 {
					t.Errorf("%s V %q, want s42's %q", cell, newV, wantV)
				}
				if newF != wantF {
					t.Errorf("%s F %q, want s42's %q", cell, newF, wantF)
				}
			}
			widthV, widthF := master1DCell(t, newXML, "Width")
			width, werr := strconv.ParseFloat(widthV, 64)
			if werr != nil || width < 1.1193-1e-9 || width > 1.1193+1e-9 {
				t.Errorf("Width V %q is not 1.1193 within 1e-9 in %s", widthV, newXML)
			}
			if widthF != "Inh" {
				t.Errorf("Width F %q, want Inh", widthF)
			}
			for _, cell := range []string{"PinX", "PinY", "LocPinX", "LocPinY"} {
				v, f := master1DCell(t, newXML, cell)
				if f != "Inh" || v == "" {
					t.Errorf("%s (V %q, F %q), want Inh with a V", cell, v, f)
				}
			}
		})
	}
}

// TestObjectAdd1DMasterFromWithGlue checks the CC3 row added
// 2026-11-17: Connectors object.add master "Dynamic connector"
// from [0,0] from_glue "s40:2" to_glue "s41:1" is accepted,
// from_glue reads back s40:2, and the hazards are stale-connector
// and stale-values, because the begin is off its site.
func TestObjectAdd1DMasterFromWithGlue(t *testing.T) {
	path := connectorCorpusFile(t, connectorJsFile)
	out := filepath.Join(t.TempDir(), "out.vsdx")
	res, err := apply.Run(path, parsePlan(t, `{"ops":[{"op":"object.add","at":"page0","master":"Dynamic connector","from":[0,0],"from_glue":"s40:2","to_glue":"s41:1","as":"c"}]}`), apply.Options{Out: out})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	addr := res.Aliases["c"]
	if addr == "" {
		t.Fatalf("no alias for the new connector, aliases %v", res.Aliases)
	}
	foundStaleValues, foundStaleConnector := false, false
	for _, h := range res.Hazards {
		if h.Kind == "stale-values" && strings.Contains(h.Detail, addr+" route") {
			foundStaleValues = true
		}
		if h.Kind == "stale-values" && strings.Contains(h.Detail, addr) && strings.Contains(h.Detail, "stale-connector") {
			foundStaleConnector = true
		}
	}
	if !foundStaleValues {
		t.Errorf("hazards %+v, want stale-values %s route", res.Hazards, addr)
	}
	if !foundStaleConnector {
		t.Errorf("hazards %+v, want stale-connector for %s", res.Hazards, addr)
	}
	page := readPage(t, out)
	i := findObject(t, page, addr)
	obj := page.Objects[i]
	checkPt(t, "new from", obj.From, [2]float64{0, 0})
	checkPt(t, "new to", obj.To, [2]float64{347.09, 72.01})
	if obj.FromGlue != "s40:2" || obj.ToGlue != "s41:1" {
		t.Errorf("glue %q %q, want s40:2 s41:1", obj.FromGlue, obj.ToGlue)
	}
	if obj.Master != "master97" {
		t.Errorf("master %q, want master97", obj.Master)
	}
}

// TestObjectAdd1DMasterBox checks the CC3 row: object.add of a
// 1-D master with box is operation_failed at ops[0].box with
// "master97 is a 1-D master: give from and to".
func TestObjectAdd1DMasterBox(t *testing.T) {
	path := connectorCorpusFile(t, connectorJsFile)
	_, err := apply.Run(path, parsePlan(t, `{"ops":[{"op":"object.add","at":"page0","master":"Dynamic connector","box":[0,0,72,72]}]}`), apply.Options{Out: filepath.Join(t.TempDir(), "out.vsdx")})
	var ce *cli.Error
	if !errors.As(err, &ce) {
		t.Fatalf("error %v, want *cli.Error", err)
	}
	if ce.Code != cli.CodeOperationFailed || ce.Path != "ops[0].box" {
		t.Errorf("got %s at %q, want operation_failed at ops[0].box", ce.Code, ce.Path)
	}
	if !strings.Contains(ce.Message, "master97 is a 1-D master: give from and to") {
		t.Errorf("message %q does not hold the CC3 wording", ce.Message)
	}
}

// TestObjectAdd1DMasterMissingBlank checks the CC3 row: object.add
// master "Dynamic connector" on the blank drawing, which has no
// masters, is not_found at ops[0].master and changes nothing.
func TestObjectAdd1DMasterMissingBlank(t *testing.T) {
	blank := blankFile(t)
	before, err := os.ReadFile(blank)
	if err != nil {
		t.Fatalf("read blank: %v", err)
	}
	_, err = apply.Run(blank, parsePlan(t, `{"ops":[{"op":"object.add","at":"page0","master":"Dynamic connector"}]}`), apply.Options{Out: filepath.Join(t.TempDir(), "out.vsdx")})
	var ce *cli.Error
	if !errors.As(err, &ce) {
		t.Fatalf("error %v, want *cli.Error", err)
	}
	if ce.Code != cli.CodeNotFound || ce.Path != "ops[0].master" {
		t.Errorf("got %s at %q, want not_found at ops[0].master", ce.Code, ce.Path)
	}
	after, _ := os.ReadFile(blank)
	if string(after) != string(before) {
		t.Error("a refused plan changed the file")
	}
}

// TestObjectAdd1DMasterFreeAndWhole checks the other two end
// forms of the 1-D master branch: free from/to read back as
// given, and whole-shape from_glue on the dynamic master gives
// the _WALKGLUE form and a PinX Connect row.
func TestObjectAdd1DMasterFreeAndWhole(t *testing.T) {
	path := connectorCorpusFile(t, connectorJsFile)
	out := filepath.Join(t.TempDir(), "free.vsdx")
	res, err := apply.Run(path, parsePlan(t, `{"ops":[{"op":"object.add","at":"page0","master":"master97","from":[72,72],"to":[216,72],"as":"c"}]}`), apply.Options{Out: out})
	if err != nil {
		t.Fatalf("free Run: %v", err)
	}
	addr := res.Aliases["c"]
	page := readPage(t, out)
	i := findObject(t, page, addr)
	checkPt(t, "free from", page.Objects[i].From, [2]float64{72, 72})
	checkPt(t, "free to", page.Objects[i].To, [2]float64{216, 72})
	if page.Objects[i].FromGlue != "" || page.Objects[i].ToGlue != "" {
		t.Errorf("free glue %q %q, want absent", page.Objects[i].FromGlue, page.Objects[i].ToGlue)
	}
	out2 := filepath.Join(t.TempDir(), "whole.vsdx")
	res, err = apply.Run(path, parsePlan(t, `{"ops":[{"op":"object.add","at":"page0","master":"Dynamic connector","from_glue":"s40","to_glue":"s41:1","as":"c"}]}`), apply.Options{Out: out2})
	if err != nil {
		t.Fatalf("whole Run: %v", err)
	}
	addr = res.Aliases["c"]
	page = readPage(t, out2)
	i = findObject(t, page, addr)
	checkPt(t, "whole from", page.Objects[i].From, [2]float64{266.5, 72.01})
	checkPt(t, "whole to", page.Objects[i].To, [2]float64{347.09, 72.01})
	if page.Objects[i].FromGlue != "s40" || page.Objects[i].ToGlue != "s41:1" {
		t.Errorf("whole glue %q %q, want s40 s41:1", page.Objects[i].FromGlue, page.Objects[i].ToGlue)
	}
	xml := shapeXML(t, out2, addr)
	if !strings.Contains(xml, "_WALKGLUE(BegTrigger,EndTrigger,WalkPreference)") {
		t.Errorf("BeginX F is not the _WALKGLUE form: %s", xml)
	}
	partXML := connectorPartXML(t, out2, "/visio/pages/page1.xml")
	id := strings.TrimPrefix(addr, "page0/s")
	want := "<Connect FromSheet='" + id + "' FromCell='BeginX' FromPart='9' ToSheet='40' ToCell='PinX' ToPart='3'/>"
	if !strings.Contains(partXML, want) {
		t.Errorf("Connect %s missing in page: %s", want, partXML)
	}
}

// TestScalingObjectAdd checks the scaling row: adding one shape to a
// page of N shapes allocates and takes time in proportion to N.
func TestScalingObjectAdd(t *testing.T) {
	setup := func(n int) (func() error, error) {
		path := vsdxtest.Generated(t, n)
		return func() error {
			ws, err := edit.Begin(path)
			if err != nil {
				return err
			}
			defer ws.Tx().Rollback()
			_, _, _, _, err = (&ops.ObjectAddSpec{At: "page0", Geom: "rect", Box: []float64{72, 72, 144, 72}}).Run(ws, map[string]string{}, "pt", nil)
			return err
		}, nil
	}
	laws.Scales(t, "vsdx object.add", setup)
	laws.TimeScales(t, "vsdx object.add", setup)
}
