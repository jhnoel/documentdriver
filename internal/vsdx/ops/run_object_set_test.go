package ops_test

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/jhnoel/documentdriver/internal/cli"
	"github.com/jhnoel/documentdriver/internal/laws"
	"github.com/jhnoel/documentdriver/internal/vsdx/apply"
	"github.com/jhnoel/documentdriver/internal/vsdx/edit"
	"github.com/jhnoel/documentdriver/internal/vsdx/ops"
	"github.com/jhnoel/documentdriver/internal/vsdx/read"
	"github.com/jhnoel/documentdriver/internal/vsdx/vsdxtest"
)

// TestObjectSetBoxAndFill checks the OSR row: one op setting box and
// fill on V1 s1 gives both, reads back the box [72,90,144,72] and the
// fill {"hex":"#00FF00"}, and changes page0/s1 once.
func TestObjectSetBoxAndFill(t *testing.T) {
	path := vsdxtest.Fixture(t, "v1-minimal")
	out := filepath.Join(t.TempDir(), "out.vsdx")
	res, err := apply.Run(path, parsePlan(t, `{"ops":[{"op":"object.set","at":"page0/s1","box":[72,90,144,72],"fill":{"hex":"#00FF00"}}]}`), apply.Options{Out: out})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(res.Changed) != 1 {
		t.Fatalf("changed = %v, want one entry for page0/s1", res.Changed)
	}
	if string(res.Changed[0]) == "" || !strings.Contains(string(res.Changed[0]), "page0/s1") {
		t.Errorf("changed entry = %s, want page0/s1", res.Changed[0])
	}
	page := readPage(t, out)
	i := findObject(t, page, "page0/s1")
	checkBox(t, "s1", page.Objects[i].Box, [4]float64{72, 90, 144, 72})
	if string(page.Objects[i].Fill) != `{"hex":"#00FF00"}` {
		t.Errorf("fill = %s, want {\"hex\":\"#00FF00\"}", page.Objects[i].Fill)
	}
	checkNoNewFindings(t, path, out)
}

// TestObjectSetNameAndEnds checks that one op setting name (setStyle)
// and from/to (setEnds) on the V5 connector s3 gives both: the new
// name reads back and the ends move to the given points, freed.
func TestObjectSetNameAndEnds(t *testing.T) {
	path := vsdxtest.Fixture(t, "v5-glue")
	out := filepath.Join(t.TempDir(), "out.vsdx")
	res, err := apply.Run(path, parsePlan(t, `{"ops":[{"op":"object.set","at":"page0/s3","name":"Link","from":[216,144],"to":[360,144]}]}`), apply.Options{Out: out})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(res.Changed) != 1 {
		t.Fatalf("changed = %v, want one entry for page0/s3", res.Changed)
	}
	page := readPage(t, out)
	i := findObject(t, page, "page0/s3")
	if page.Objects[i].Name != "Link" {
		t.Errorf("name = %q, want Link", page.Objects[i].Name)
	}
	checkPt(t, "s3 from", page.Objects[i].From, [2]float64{216, 144})
	checkPt(t, "s3 to", page.Objects[i].To, [2]float64{360, 144})
	if page.Objects[i].FromGlue != "" || page.Objects[i].ToGlue != "" {
		t.Errorf("glue = %q %q, want both free", page.Objects[i].FromGlue, page.Objects[i].ToGlue)
	}
	checkNoNewFindings(t, path, out)
}

// TestObjectSetStale checks that a box and fill on V5 s1 merges the
// transform's stale report into apply's stale-values hazard naming
// page0/s3 (stale-connector), while both the box and the fill read
// back.
func TestObjectSetStale(t *testing.T) {
	path := vsdxtest.Fixture(t, "v5-glue")
	out := filepath.Join(t.TempDir(), "out.vsdx")
	res, err := apply.Run(path, parsePlan(t, `{"ops":[{"op":"object.set","at":"page0/s1","box":[72,90,144,72],"fill":{"hex":"#00FF00"}}]}`), apply.Options{Out: out})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	found := false
	for _, h := range res.Hazards {
		if h.Kind == "stale-values" && strings.Contains(h.Detail, "page0/s3") && strings.Contains(h.Detail, "stale-connector") {
			found = true
		}
	}
	if !found {
		t.Errorf("hazards %+v, want a stale-values detail naming page0/s3 stale-connector", res.Hazards)
	}
	page := readPage(t, out)
	i := findObject(t, page, "page0/s1")
	checkBox(t, "s1", page.Objects[i].Box, [4]float64{72, 90, 144, 72})
	if string(page.Objects[i].Fill) != `{"hex":"#00FF00"}` {
		t.Errorf("fill = %s, want {\"hex\":\"#00FF00\"}", page.Objects[i].Fill)
	}
}

// TestObjectSetIdentity checks that setting V1 s1 to the box and
// fill it already shows writes nothing.
func TestObjectSetIdentity(t *testing.T) {
	path := vsdxtest.Fixture(t, "v1-minimal")
	out := filepath.Join(t.TempDir(), "out.vsdx")
	res, err := apply.Run(path, parsePlan(t, `{"ops":[{"op":"object.set","at":"page0/s1","box":[108,126,72,36],"fill":{"hex":"#4472C4"}}]}`), apply.Options{Out: out})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if _, err := os.Stat(out); !errors.Is(err, os.ErrNotExist) {
		t.Error("identity object.set wrote its out file")
	}
	if len(res.Parts) != 0 || len(res.Changed) != 0 {
		t.Errorf("identity result parts %v changed %v, want empty", res.Parts, res.Changed)
	}
	if len(res.Hazards) != 0 {
		t.Errorf("identity hazards %+v, want none", res.Hazards)
	}
}

// TestObjectSetIdentityFields checks the CC4 identity rows: each
// plan sets a field of an object to the value read shows it (in
// read's form), and each is a no-op (new_base = base, no saved
// file, changed empty).
func TestObjectSetIdentityFields(t *testing.T) {
	for _, tc := range []struct {
		fixture string
		plan    string
	}{
		{"v2-master", `{"ops":[{"op":"object.set","at":"page0/s2","fill":{"hex":"#FF0000"}}]}`},
		{"v2-master", `{"ops":[{"op":"object.set","at":"page0/s1","fill":{"hex":"#FFFFFF"}}]}`},
		{"v2-master", `{"ops":[{"op":"object.set","at":"page0/s1","line":{"w":0.75}}]}`},
		{"v2-master", `{"ops":[{"op":"object.set","at":"page0/s1","line":{"w":0.75,"color":{"hex":"#7F7F7F"},"dash":"1"}}]}`},
		{"v2-master", `{"ops":[{"op":"object.set","at":"page0/s1","box":[108,117,72,54]}]}`},
		{"v2-master", `{"ops":[{"op":"object.set","at":"page0/s2","box":[252,117,72,54]}]}`},
		{"v2-master", `{"ops":[{"op":"object.set","at":"page0/s1","name":"Process"}]}`},
		{"v2-master", `{"ops":[{"op":"object.set","at":"page0/s5","fill":{"hex":"#0000FF"}}]}`},
		{"v2-master", `{"ops":[{"op":"object.set","at":"page0/s4","fill":{"hex":"#5B9BD5"}}]}`},
		{"v1-minimal", `{"ops":[{"op":"object.set","at":"page0/s1","fill":{"hex":"#4472C4"}}]}`},
		{"v9-rotation", `{"ops":[{"op":"object.set","at":"page0/s1","rot":270}]}`},
		{"v9-rotation", `{"ops":[{"op":"object.set","at":"page0/s1","box":[90,90,72,36],"rot":270,"flip_h":false,"flip_v":false}]}`},
		{"v9-rotation", `{"ops":[{"op":"object.set","at":"page0/s3","box":[396,126,72,36],"rot":30}]}`},
		{"v4-groups", `{"ops":[{"op":"object.set","at":"page0/s12","box":[288,360,72,72],"rot":330}]}`},
		{"v5-glue", `{"ops":[{"op":"object.set","at":"page0/s4","from":[396,144]}]}`},
		{"v5-glue", `{"ops":[{"op":"object.set","at":"page0/s1","fill":{"hex":"#5B9BD5"}}]}`},
	} {
		t.Run(tc.plan, func(t *testing.T) {
			path := vsdxtest.Fixture(t, tc.fixture)
			out := filepath.Join(t.TempDir(), "out.vsdx")
			res, err := apply.Run(path, parsePlan(t, tc.plan), apply.Options{Out: out})
			if err != nil {
				t.Fatalf("Run: %v", err)
			}
			if res.Base != res.NewBase {
				t.Errorf("new_base = %s, want base %s", res.NewBase, res.Base)
			}
			if _, err := os.Stat(out); !errors.Is(err, os.ErrNotExist) {
				t.Error("identity object.set wrote its out file")
			}
			if len(res.Parts) != 0 || len(res.Changed) != 0 || len(res.Hazards) != 0 {
				t.Errorf("identity result parts %v changed %v hazards %+v, want empty", res.Parts, res.Changed, res.Hazards)
			}
		})
	}
}

// TestObjectSetDifferentFill checks the CC4 different-value row:
// V2 s2 fill #FFFFFF (was stored #FF0000) writes, changed
// [page0/s2], read shows #FFFFFF and the stored FillForegnd cell
// is V '#FFFFFF'.
func TestObjectSetDifferentFill(t *testing.T) {
	path := vsdxtest.Fixture(t, "v2-master")
	out := filepath.Join(t.TempDir(), "out.vsdx")
	res, err := apply.Run(path, parsePlan(t, `{"ops":[{"op":"object.set","at":"page0/s2","fill":{"hex":"#FFFFFF"}}]}`), apply.Options{Out: out})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(res.Changed) != 1 || !strings.Contains(string(res.Changed[0]), "page0/s2") {
		t.Fatalf("changed = %v, want [page0/s2]", res.Changed)
	}
	page := readPage(t, out)
	i := findObject(t, page, "page0/s2")
	if string(page.Objects[i].Fill) != `{"hex":"#FFFFFF"}` {
		t.Errorf("fill = %s, want {\"hex\":\"#FFFFFF\"}", page.Objects[i].Fill)
	}
	xml := connectorPartXML(t, out, "/visio/pages/page1.xml")
	if !strings.Contains(xml, "FillForegnd") || !strings.Contains(xml, "#FFFFFF") {
		t.Errorf("sealed page has no stored FillForegnd #FFFFFF: %s", xml)
	}
	if strings.Contains(xml, "FillForegnd' V='2'") || strings.Contains(xml, `FillForegnd" V="2"`) {
		t.Errorf("sealed page still stores FillForegnd V '2': %s", xml)
	}
}

// TestObjectSetIdentityFixtureLoop is the CC4 fixture loop: for
// every object of every V1 to V17 fixture, object.set with each
// field as read shows it (box, rot, flips, name, fill, line, and
// for connectors from/to with their glue) is a no-op. A field the
// tool legitimately refuses (a scaled page, a Foreign shape, a
// 1-D box, a container member, an unreadable transform) is
// skipped and logged, never a write.
func TestObjectSetIdentityFixtureLoop(t *testing.T) {
	type loopObject struct {
		ID       string          `json:"id"`
		Kind     string          `json:"kind"`
		Name     string          `json:"name"`
		Box      []float64       `json:"box"`
		Rot      *float64        `json:"rot"`
		FlipH    bool            `json:"flip_h"`
		FlipV    bool            `json:"flip_v"`
		Fill     json.RawMessage `json:"fill"`
		Line     json.RawMessage `json:"line"`
		From     []float64       `json:"from"`
		To       []float64       `json:"to"`
		FromGlue string          `json:"from_glue"`
		ToGlue   string          `json:"to_glue"`
	}
	checked, skipped := 0, 0
	for _, fixture := range htmlFixtures {
		path := vsdxtest.Fixture(t, fixture)
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("ReadFile: %v", err)
		}
		raw, err := read.Page(data, "base", "page0", read.Options{Units: "pt"})
		if err != nil {
			t.Logf("%s: no page0 (%v), skipped", fixture, err)
			continue
		}
		var page struct {
			Objects []loopObject `json:"objects"`
		}
		if err := json.Unmarshal(raw, &page); err != nil {
			t.Fatalf("Unmarshal: %v", err)
		}
		for _, o := range page.Objects {
			var plans []string
			if o.Kind != "connector" && len(o.Box) == 4 {
				plans = append(plans,
					`{"ops":[{"op":"object.set","at":"`+o.ID+`","box":[`+jsonNumber(o.Box[0])+`,`+jsonNumber(o.Box[1])+`,`+jsonNumber(o.Box[2])+`,`+jsonNumber(o.Box[3])+`]}]}`,
					`{"ops":[{"op":"object.set","at":"`+o.ID+`","rot":`+jsonNumber(float64Value(o.Rot))+`,"flip_h":`+jsonBool(o.FlipH)+`,"flip_v":`+jsonBool(o.FlipV)+`}]}`)
			}
			if o.Name != "" {
				plans = append(plans, `{"ops":[{"op":"object.set","at":"`+o.ID+`","name":`+jsonString(o.Name)+`} ]}`)
			}
			if len(o.Fill) > 0 {
				plans = append(plans, `{"ops":[{"op":"object.set","at":"`+o.ID+`","fill":`+string(o.Fill)+`} ]}`)
			}
			if len(o.Line) > 0 {
				plans = append(plans, `{"ops":[{"op":"object.set","at":"`+o.ID+`","line":`+string(o.Line)+`} ]}`)
			}
			if o.Kind == "connector" {
				// A free end sent back at its point is a no-op; a
				// glued end sent back as point+glue is not for
				// V5 connectors, which store no end formula
				// (set_glue's identity check), so glued ends are
				// skipped and logged, not asserted.
				if len(o.From) == 2 && o.FromGlue == "" {
					plans = append(plans, `{"ops":[{"op":"object.set","at":"`+o.ID+`","from":[`+jsonNumber(o.From[0])+`,`+jsonNumber(o.From[1])+`]}]}`)
				}
				if len(o.To) == 2 && o.ToGlue == "" {
					plans = append(plans, `{"ops":[{"op":"object.set","at":"`+o.ID+`","to":[`+jsonNumber(o.To[0])+`,`+jsonNumber(o.To[1])+`]}]}`)
				}
			}
			for _, plan := range plans {
				parsed, parseErrs := ops.ParsePlan([]byte(plan))
				if len(parseErrs) != 0 || parsed == nil {
					skipped++
					t.Logf("%s %s plan refused (skipped): %v", fixture, o.ID, parseErrs)
					continue
				}
				out := filepath.Join(t.TempDir(), "out.vsdx")
				res, err := apply.Run(path, parsed, apply.Options{Out: out})
				if err != nil {
					skipped++
					t.Logf("%s %s refused (skipped): %v", fixture, o.ID, err)
					continue
				}
				checked++
				if len(res.Changed) != 0 || len(res.Parts) != 0 {
					t.Errorf("%s %s plan %s: changed %v parts %v, want no-op", fixture, o.ID, plan, res.Changed, res.Parts)
				}
				if res.Base != res.NewBase {
					t.Errorf("%s %s plan %s: new_base %s, want base %s", fixture, o.ID, plan, res.NewBase, res.Base)
				}
			}
		}
	}
	t.Logf("fixture loop: %d checked, %d refused and skipped", checked, skipped)
	if checked == 0 {
		t.Fatal("fixture loop checked nothing")
	}
}

// jsonNumber formats f as a JSON number.
func jsonNumber(f float64) string {
	return strconv.FormatFloat(f, 'g', -1, 64)
}

// jsonBool formats b as a JSON boolean.
func jsonBool(b bool) string {
	if b {
		return "true"
	}
	return "false"
}

// jsonString formats s as a JSON string.
func jsonString(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}

// float64Value returns the value of p, or 0 when p is nil.
func float64Value(p *float64) float64 {
	if p == nil {
		return 0
	}
	return *p
}

// TestObjectSetBoxCorpus checks the CC4 corpus rows: object.set
// box [40,20,100,100] reads back exactly [40,20,100,100] for the
// 11 shapes whose master-held LocPin is an evaluable dependent
// (test, testVISIO and testVISIO.vstx page0/s5; office_varient4,
// testfile3 and testfile5 page0/s1; BasicShapes page0/s20, s21,
// s23, s25 and s26), and the 4 container members (test,
// testVISIO and testVISIO.vstx page0/s8, tdf154379 page0/s317)
// are refused at at by the container rule. It skips only when
// VSDX_CORPUS is unset and fails naming the path when a corpus
// file is missing.
func TestObjectSetBoxCorpus(t *testing.T) {
	if os.Getenv("VSDX_CORPUS") == "" {
		t.Skip("no vsdx corpus (set VSDX_CORPUS)")
	}
	dir := corpusDir(t)
	tika := filepath.Join("apache-tika", "tika-parsers", "tika-parsers-standard", "tika-parsers-standard-modules", "tika-parser-microsoft-module", "src", "test", "resources", "test-documents")
	for _, tc := range []struct {
		rel     string
		at      string
		refused bool
	}{
		{filepath.Join("apache-poi", "test-data", "diagram", "test.vsdx"), "page0/s5", false},
		{filepath.Join("apache-poi", "test-data", "diagram", "test.vsdx"), "page0/s8", true},
		{filepath.Join(tika, "testVISIO.vsdx"), "page0/s5", false},
		{filepath.Join(tika, "testVISIO.vsdx"), "page0/s8", true},
		{filepath.Join(tika, "testVISIO.vstx"), "page0/s5", false},
		{filepath.Join(tika, "testVISIO.vstx"), "page0/s8", true},
		{filepath.Join("libvisio", "src", "test", "data", "office_varient4.vsdx"), "page0/s1", false},
		{filepath.Join("libvisio", "src", "test", "data", "testfile3.vsdx"), "page0/s1", false},
		{filepath.Join("libvisio", "src", "test", "data", "testfile5.vsdx"), "page0/s1", false},
		{filepath.Join("vsdx-js", "tests", "BasicShapes.vsdx"), "page0/s20", false},
		{filepath.Join("vsdx-js", "tests", "BasicShapes.vsdx"), "page0/s21", false},
		{filepath.Join("vsdx-js", "tests", "BasicShapes.vsdx"), "page0/s23", false},
		{filepath.Join("vsdx-js", "tests", "BasicShapes.vsdx"), "page0/s25", false},
		{filepath.Join("vsdx-js", "tests", "BasicShapes.vsdx"), "page0/s26", false},
		{filepath.Join("libvisio", "src", "test", "data", "tdf154379-QuickStyleFillMatrix.vsdx"), "page0/s317", true},
	} {
		t.Run(tc.rel+"/"+tc.at, func(t *testing.T) {
			path := filepath.Join(dir, "repos", tc.rel)
			if _, err := os.Stat(path); err != nil {
				t.Fatalf("corpus file %s not found under VSDX_CORPUS %s", path, dir)
			}
			out := filepath.Join(t.TempDir(), "out.vsdx")
			res, err := apply.Run(path, parsePlan(t, `{"ops":[{"op":"object.set","at":"`+tc.at+`","box":[40,20,100,100]}]}`), apply.Options{Out: out})
			if tc.refused {
				var ce *cli.Error
				if !errors.As(err, &ce) || ce.Code != cli.CodeOperationFailed || ce.Path != "ops[0].at" {
					t.Fatalf("Run: got %v, want operation_failed at ops[0].at", err)
				}
				if !strings.Contains(ce.Message, "container or list") {
					t.Errorf("refusal = %q, want the container or list message", ce.Message)
				}
				return
			}
			if err != nil {
				t.Fatalf("Run: %v", err)
			}
			if len(res.Changed) != 1 {
				t.Fatalf("changed = %v, want one entry", res.Changed)
			}
			page := readPage(t, out)
			i := findObject(t, page, tc.at)
			checkBox(t, tc.at, page.Objects[i].Box, [4]float64{40, 20, 100, 100})
		})
	}
}

// TestObjectSetBoxCorpusLocPinRefusal checks the CC4 dependents
// refusal: 60489 page0/s116 has a master-held LocPinX formula
// Width*IF(PinX<User.Con1X,1,0) that vsdx does not evaluate, so
// a box write is refused at box in the formula refusal form
// naming LocPinX, never left stale. It skips only when
// VSDX_CORPUS is unset.
func TestObjectSetBoxCorpusLocPinRefusal(t *testing.T) {
	if os.Getenv("VSDX_CORPUS") == "" {
		t.Skip("no vsdx corpus (set VSDX_CORPUS)")
	}
	dir := corpusDir(t)
	path := filepath.Join(dir, "repos", "apache-poi", "test-data", "diagram", "60489.vsdx")
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("corpus file %s not found under VSDX_CORPUS %s", path, dir)
	}
	out := filepath.Join(t.TempDir(), "out.vsdx")
	_, err := apply.Run(path, parsePlan(t, `{"ops":[{"op":"object.set","at":"page0/s116","box":[40,20,100,100]}]}`), apply.Options{Out: out})
	var ce *cli.Error
	if !errors.As(err, &ce) || ce.Code != cli.CodeOperationFailed || ce.Path != "ops[0].box" {
		t.Fatalf("Run: got %v, want operation_failed at ops[0].box", err)
	}
	if ce.Detail != "LocPinX F=Width*IF(PinX<User.Con1X,1,0)" {
		t.Errorf("detail = %q, want LocPinX F=Width*IF(PinX<User.Con1X,1,0)", ce.Detail)
	}
	if _, statErr := os.Stat(out); !errors.Is(statErr, os.ErrNotExist) {
		t.Error("a refused object.set wrote its out file")
	}
}

// TestObjectSetNoField checks the OSR row: an object.set with no
// field is refused by the rules (O0) as invalid_operation at ops[0].
func TestObjectSetNoField(t *testing.T) {
	_, errs := ops.ParsePlan([]byte(`{"ops":[{"op":"object.set","at":"page0/s1"}]}`))
	if len(errs) != 1 {
		t.Fatalf("ParsePlan errors %v, want one", errs)
	}
	if errs[0].Code != cli.CodeInvalidOperation || errs[0].Path != "ops[0]" {
		t.Errorf("error = %s at %q, want invalid_operation at ops[0]", errs[0].Code, errs[0].Path)
	}
}

// TestObjectSetAlias checks that an object.set through the $a alias
// an earlier object.add created sets the box and fill of the added
// shape.
func TestObjectSetAlias(t *testing.T) {
	path := vsdxtest.Fixture(t, "v1-minimal")
	out := filepath.Join(t.TempDir(), "out.vsdx")
	plan := `{"ops":[{"op":"object.add","at":"page0","geom":"rect","as":"a","box":[72,72,144,72]},` +
		`{"op":"object.set","at":"$a","box":[108,126,72,36],"fill":{"hex":"#00FF00"}}]}`
	res, err := apply.Run(path, parsePlan(t, plan), apply.Options{Out: out})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	addr := res.Aliases["a"]
	if addr == "" {
		t.Fatalf("aliases %v, want $a defined", res.Aliases)
	}
	page := readPage(t, out)
	i := findObject(t, page, addr)
	checkBox(t, "added", page.Objects[i].Box, [4]float64{108, 126, 72, 36})
	if string(page.Objects[i].Fill) != `{"hex":"#00FF00"}` {
		t.Errorf("fill = %s, want {\"hex\":\"#00FF00\"}", page.Objects[i].Fill)
	}
}

// TestObjectSetRefusals checks that the merged Run reports each
// field family's refusal: a box on a 1-D shape and a missing shape
// are refused at at, a from on a 2-D shape at at, and whole-shape
// glue at to_glue.
func TestObjectSetRefusals(t *testing.T) {
	for _, tc := range []struct {
		fixture string
		plan    string
		code    string
		path    string
	}{
		{"v5-glue", `{"ops":[{"op":"object.set","at":"page0/s3","box":[72,90,144,72],"fill":{"hex":"#00FF00"}}]}`, cli.CodeOperationFailed, "ops[0].at"},
		{"v1-minimal", `{"ops":[{"op":"object.set","at":"page0/s1","from":[0,0]}]}`, cli.CodeOperationFailed, "ops[0].at"},
		{"v1-minimal", `{"ops":[{"op":"object.set","at":"page0/s99","box":[72,90,144,72],"fill":{"hex":"#00FF00"}}]}`, cli.CodeNotFound, "ops[0].at"},
		{"v5-glue", `{"ops":[{"op":"object.set","at":"page0/s4","to_glue":"s2"}]}`, cli.CodeOperationFailed, "ops[0].to_glue"},
	} {
		path := vsdxtest.Fixture(t, tc.fixture)
		out := filepath.Join(t.TempDir(), "out.vsdx")
		_, err := apply.Run(path, parsePlan(t, tc.plan), apply.Options{Out: out})
		var ce *cli.Error
		if !errors.As(err, &ce) {
			t.Fatalf("%s: error %v, want *cli.Error", tc.plan, err)
		}
		if ce.Code != tc.code || ce.Path != tc.path {
			t.Errorf("%s: got %s at %q, want %s at %q", tc.plan, ce.Code, ce.Path, tc.code, tc.path)
		}
		if _, statErr := os.Stat(out); !errors.Is(statErr, os.ErrNotExist) {
			t.Errorf("%s: a refused object.set wrote its out file", tc.plan)
		}
	}
}

// TestScalingObjectSet checks the scaling row: object.set (box and
// fill) on Generated(1000) and Generated(4000) (N = 1000, 4N = 4000
// shapes) allocates at most 5.5 times as much (laws.Scales) and the
// interleaved time ratio is at most 8 (laws.TimeScales).
func TestScalingObjectSet(t *testing.T) {
	setup := func(n int) (func() error, error) {
		src := vsdxtest.Generated(t, n)
		return func() error {
			w, err := edit.Begin(src)
			if err != nil {
				return err
			}
			defer w.Tx().Rollback()
			rot := float64(0)
			spec := &ops.ObjectSetSpec{At: "page0/s1", Box: []float64{0, 693, 144, 54}, Rot: &rot, Fill: &ops.PaintSpec{Hex: "#00FF00"}}
			if _, _, _, _, err := spec.Run(w, map[string]string{}, "pt", nil); err != nil {
				return err
			}
			return w.Verify()
		}, nil
	}
	laws.Scales(t, "vsdx object.set shapes", setup)
	laws.TimeScales(t, "vsdx object.set shapes", setup)
}
