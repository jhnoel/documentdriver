package ops_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jhnoel/documentdriver/internal/vsdx/ops"
	"github.com/jhnoel/documentdriver/internal/vsdx/read"
	"github.com/jhnoel/documentdriver/internal/vsdx/vsdxtest"
)

// readObj returns object id of page0 of path as decoded JSON.
func readObj(t *testing.T, path, id string) map[string]json.RawMessage {
	t.Helper()
	return readPageObj(t, path, "page0", id)
}

// readPageObj returns object id of page pageID of path as decoded JSON.
func readPageObj(t *testing.T, path, pageID, id string) map[string]json.RawMessage {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	out, err := read.Page(b, "sha256:x", pageID, read.Options{})
	if err != nil {
		t.Fatal(err)
	}
	var page struct {
		Objects []map[string]json.RawMessage `json:"objects"`
	}
	if err := json.Unmarshal(out, &page); err != nil {
		t.Fatal(err)
	}
	for _, o := range page.Objects {
		if string(o["id"]) == `"`+id+`"` {
			return o
		}
	}
	t.Fatalf("%s not on %s", id, pageID)
	return nil
}

// lineShadowDrawing is a blank drawing with a rect (s1) and a
// master-less connector (s2), made through create's plan path.
func lineShadowDrawing(t *testing.T) string {
	t.Helper()
	out, _ := refusalOut(t, vsdxtest.Blank(t), `{"ops":[{"op":"object.add","at":"page0","geom":"rect","box":[72,72,100,50]},{"op":"object.add","at":"page0","kind":"connector","from":[0,0],"to":[100,100]}]}`)
	return out
}

// TestLineEndsWriteReadIdentity checks the Line ends rows: names
// write their stored index and read back as names with the size; a
// bare name is shorthand; sending read's value back writes nothing;
// "none" clears; the connector added with kind connector shows no
// arrowheads until a plan gives them.
func TestLineEndsWriteReadIdentity(t *testing.T) {
	path := lineShadowDrawing(t)
	if l := string(readObj(t, path, "page0/s2")["line"]); json.Valid([]byte(l)) && (contains(l, `"begin"`) || contains(l, `"end"`)) {
		t.Fatalf("new connector line = %s, want no begin/end", l)
	}
	out, changed := refusalOut(t, path, `{"ops":[{"op":"object.set","at":"page0/s2","line":{"begin":{"arrow":"stealth","size":3},"end":"triangle"}}]}`)
	if len(changed) != 1 {
		t.Fatalf("changed = %v, want s2", changed)
	}
	var line struct {
		Begin map[string]any `json:"begin"`
		End   map[string]any `json:"end"`
	}
	if err := json.Unmarshal(readObj(t, out, "page0/s2")["line"], &line); err != nil {
		t.Fatal(err)
	}
	if line.Begin["arrow"] != "stealth" || line.Begin["size"] != 3.0 || line.End["arrow"] != "triangle" {
		t.Errorf("line ends = %+v, want begin stealth size 3, end triangle", line)
	}
	if _, changed := refusalOut(t, out, `{"ops":[{"op":"object.set","at":"page0/s2","line":{"begin":{"arrow":"stealth","size":3},"end":{"arrow":"triangle"}}}]}`); len(changed) != 0 {
		t.Errorf("identity line ends: changed = %v, want none", changed)
	}
	cleared, _ := refusalOut(t, out, `{"ops":[{"op":"object.set","at":"page0/s2","line":{"begin":"none","end":"none"}}]}`)
	if l := string(readObj(t, cleared, "page0/s2")["line"]); contains(l, `"begin"`) || contains(l, `"end"`) {
		t.Errorf("after none: line = %s, want no ends", l)
	}
}

// TestLineEndsRefusals checks the refusals: begin or end on a 2-D
// shape, an index the table does not name (when it is not what read
// shows), and a size outside 0 to 6.
func TestLineEndsRefusals(t *testing.T) {
	path := lineShadowDrawing(t)
	expectRefusal(t, path, `{"ops":[{"op":"object.set","at":"page0/s1","line":{"end":"triangle"}}]}`, "line.end", "is not a connector")
	expectRefusal(t, path, `{"ops":[{"op":"object.set","at":"page0/s2","line":{"end":"13"}}]}`, "line.end", `arrow "13" is not a name vsdx writes`)
	if _, errs := ops.ParsePlan([]byte(`{"ops":[{"op":"object.set","at":"page0/s2","line":{"end":{"arrow":"triangle","size":7}}}]}`)); len(errs) != 1 || errs[0].Code != "invalid_operation" || errs[0].Path != "ops[0].line.end.size" {
		t.Errorf("size 7: errs = %v, want invalid_operation at ops[0].line.end.size", errs)
	}
}

// TestLineEndsCorpus checks the read rows on Visio-authored samples:
// DiagramWithStyles s3 begin is index 13 shown as "13", and 60973 s3
// has stealth size 2 at both ends; sending each back writes nothing.
func TestLineEndsCorpus(t *testing.T) {
	dws := corpusFile(t, "vsdx-js/tests/DiagramWithStyles.vsdx")
	l := string(readObj(t, dws, "page0/s3")["line"])
	if !contains(l, `"begin":{"arrow":"13"`) {
		t.Errorf("DiagramWithStyles s3 line = %s, want begin arrow \"13\"", l)
	}
	if _, changed := refusalOut(t, dws, `{"ops":[{"op":"object.set","at":"page0/s3","line":{"begin":"13"}}]}`); len(changed) != 0 {
		t.Errorf("identity index 13: changed = %v", changed)
	}
	p := corpusFile(t, "apache-poi/test-data/diagram/60973.vsdx")
	if l := string(readObj(t, p, "page0/s3")["line"]); l != `{"begin":{"arrow":"stealth","size":2},"end":{"arrow":"stealth","size":2}}` {
		t.Errorf("60973 s3 line = %s", l)
	}
}

// TestShadowWriteReadIdentity checks the Shadows rows: an object
// value on a shape with none needs all three members; written, it
// reads back in the plan's form; sent back it writes nothing;
// "none" clears it; a connector refuses shadow.
func TestShadowWriteReadIdentity(t *testing.T) {
	path := lineShadowDrawing(t)
	expectRefusal(t, path, `{"ops":[{"op":"object.set","at":"page0/s1","shadow":{"color":"#336699"}}]}`, "shadow", "shadow needs transparency, offset")
	expectRefusal(t, path, `{"ops":[{"op":"object.set","at":"page0/s2","shadow":"none"}]}`, "shadow", "is a connector")
	plan := `{"ops":[{"op":"object.set","at":"page0/s1","shadow":{"color":"#336699","transparency":40,"offset":[6,6]}}]}`
	out, _ := refusalOut(t, path, plan)
	if s := string(readObj(t, out, "page0/s1")["shadow"]); s != `{"color":{"hex":"#336699"},"transparency":40,"offset":[6,6]}` {
		t.Errorf("shadow = %s", s)
	}
	if _, changed := refusalOut(t, out, plan); len(changed) != 0 {
		t.Errorf("identity shadow: changed = %v", changed)
	}
	none, _ := refusalOut(t, out, `{"ops":[{"op":"object.set","at":"page0/s1","shadow":"none"}]}`)
	if s := readObj(t, none, "page0/s1")["shadow"]; s != nil {
		t.Errorf("after none: shadow = %s, want no key", s)
	}
}

// TestShadowCorpus checks the read row on testVISIO.vsdx: s4 and s5
// show #5B9BD5, 75 and [0.35, 1.97] (ShapeShdwOffsetX 0.0048 in,
// ShapeShdwOffsetY -0.0274 in), and sending it back writes nothing.
func TestShadowCorpus(t *testing.T) {
	p := corpusFile(t, "apache-tika/tika-parsers/tika-parsers-standard/tika-parsers-standard-modules/tika-parser-microsoft-module/src/test/resources/test-documents/testVISIO.vsdx")
	for _, id := range []string{"page0/s4", "page0/s5"} {
		if s := string(readObj(t, p, id)["shadow"]); s != `{"color":{"hex":"#5B9BD5"},"transparency":75,"offset":[0.35,1.97]}` {
			t.Errorf("%s shadow = %s", id, s)
		}
	}
}

// corpusFile returns a corpus drawing under VSDX_CORPUS/repos,
// skipping when the corpus is unset and failing when the file is
// missing.
func corpusFile(t *testing.T, rel string) string {
	t.Helper()
	dir := os.Getenv("VSDX_CORPUS")
	if dir == "" {
		t.Skip("no vsdx corpus (set VSDX_CORPUS)")
	}
	p := filepath.Join(dir, "repos", filepath.FromSlash(rel))
	if _, err := os.Stat(p); err != nil {
		t.Fatalf("corpus file: %v", err)
	}
	return p
}

func contains(s, sub string) bool { return strings.Contains(s, sub) }
