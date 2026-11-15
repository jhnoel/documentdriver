package ops

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/jhnoel/documentdriver/internal/opc"
	"github.com/jhnoel/documentdriver/internal/pptx/derive"
	"github.com/jhnoel/documentdriver/internal/pptx/ops/edit"
)

func compile(t *testing.T, plan string) []Op {
	t.Helper()
	p, err := DecodePlan(strings.NewReader(plan))
	if err != nil {
		t.Fatal(err)
	}
	o, err := p.Compile()
	if err != nil {
		t.Fatal(err)
	}
	return o
}

// m3Plan touches every M3 operation on the diagram fixture.
const m3Plan = `{"ops": [
  {"op": "object.add", "at": "slide1", "as": "box", "kind": "shape", "name": "New", "geom": "roundRect",
   "box": [500, 400, 80, 40], "adj": {"adj": 20000}, "fill": {"scheme": "accent2"},
   "line": {"w": 1, "color": {"hex": "#00FF00"}, "dash": "dash"}, "html": "<p><b>New</b> box</p>"},
  {"op": "object.add", "at": "slide1", "kind": "connector", "name": "Link", "geom": "bentConnector3", "first": "h",
   "from": [200, 125], "to": [500, 420], "from_glue": "s10:3", "to_glue": "$box:1",
   "line": {"w": 2, "color": {"scheme": "accent1"}, "tail": "triangle"}},
  {"op": "object.set", "at": "slide1/s11", "fill": "none", "geom": "rect", "html": "DB"},
  {"op": "text.set", "at": "slide1/s10", "html": "<p style=\"text-align:center\"><b>API</b></p>"},
  {"op": "text.replace", "at": "slide1/s3", "select": "two", "with": "2"},
  {"op": "object.duplicate", "at": "slide1/s70", "as": "copy", "dx": 0, "dy": 60},
  {"op": "object.delete", "at": "slide1/s71"},
  {"op": "object.reorder", "at": "slide1/s10", "to": "front"},
  {"op": "object.move", "at": "slide1/s21", "dx": 5, "dy": 5}
]}`

// TestM3AtomicityOpFailure: a failure injected at operation k leaves the
// file identical, with no temporary file.
func TestM3AtomicityOpFailure(t *testing.T) {
	n := len(compile(t, m3Plan))
	for k := range n {
		t.Run(fmt.Sprint(k), func(t *testing.T) {
			path, before, mtime := diagram(t)
			plan := compile(t, m3Plan)
			plan = append(plan[:k:k], append([]Op{funcOp(func(*opc.Package) error { return errors.New("injected") })}, plan[k:]...)...)
			if err := Apply(path, plan...); err == nil {
				t.Fatal("plan succeeded")
			}
			assertUntouched(t, path, before, mtime)
		})
	}
	path, before, _ := diagram(t)
	if err := Apply(path, compile(t, m3Plan)...); err != nil {
		t.Fatal(err)
	}
	if after, _ := os.ReadFile(path); string(after) == string(before) {
		t.Fatal("plan wrote nothing")
	}
}

// TestM3AtomicitySaveStages: a failure at each save stage before the
// rename leaves the file identical.
func TestM3AtomicitySaveStages(t *testing.T) {
	for _, stage := range []string{opc.StageCreate, opc.StageWrite, opc.StageChmod, opc.StageSync, opc.StageClose, opc.StageRename} {
		t.Run(stage, func(t *testing.T) {
			path, before, mtime := diagram(t)
			defer opc.SetFaultHook(func(st, _ string) error {
				if st == stage {
					return errors.New("injected at " + st)
				}
				return nil
			})()
			if err := Apply(path, compile(t, m3Plan)...); err == nil {
				t.Fatal("commit succeeded")
			}
			assertUntouched(t, path, before, mtime)
		})
	}
}

// TestM3FailedOpWritesNothing: an operation that fails part-way (the
// second glue does not resolve) leaves the file untouched.
func TestM3FailedOpWritesNothing(t *testing.T) {
	path, before, mtime := diagram(t)
	err := Apply(path, compile(t, `{"ops": [
	  {"op": "object.set", "at": "slide1/s12", "from_glue": "s10:2", "to_glue": "s404:1"}]}`)...)
	if !errors.Is(err, edit.ErrNoObject) {
		t.Fatalf("err = %v", err)
	}
	assertUntouched(t, path, before, mtime)
}

func TestCompileRejects(t *testing.T) {
	for _, c := range []struct {
		op   string
		want error
		msg  string
	}{
		{`{"op": "object.add", "at": "slide1", "kind": "shape", "geom": "rect", "box": [1,2,3,4]}`, ErrMissing, "name"},
		{`{"op": "object.add", "at": "slide1", "kind": "shape", "name": "x", "box": [1,2,3,4]}`, ErrMissing, "geom"},
		{`{"op": "object.add", "at": "slide1", "kind": "shape", "name": "x", "geom": "rect"}`, ErrMissing, "box"},
		{`{"op": "object.add", "at": "slide1", "kind": "connector", "name": "x", "geom": "bentConnector3", "from": [0,0], "to": [1,1]}`, ErrMissing, "first"},
		{`{"op": "object.add", "at": "slide1", "kind": "connector", "name": "x", "geom": "straightConnector1", "from": [0,0]}`, ErrMissing, "from and to"},
		{`{"op": "object.add", "at": "slide1", "kind": "picture", "name": "x", "geom": "rect", "box": [1,2,3,4]}`, ErrMissing, "image"},
		{`{"op": "object.add", "at": "slide1", "kind": "picture", "name": "x", "image": "a.png"}`, ErrMissing, "box"},
		{`{"op": "object.set", "at": "slide1/s1", "path": "M 0 0 A 5 5 0 0 1 10 10"}`, derive.ErrPathSyntax, "cubic curves"},
		{`{"op": "object.set", "at": "slide1/s1", "path": "M 0 0 S 1 1 2 2"}`, derive.ErrPathSyntax, "smooth"},
		{`{"op": "object.add", "at": "slide1", "kind": "shape", "name": "x", "geom": "custom", "box": [1,2,3,4]}`, ErrMissing, "path"},
		{`{"op": "group.create", "at": ["slide1/s1", "slide1/s2"]}`, ErrMissing, "name"},
		{`{"op": "group.create", "name": "G", "at": []}`, ErrMissing, "at"},
		{`{"op": "slide.add"}`, ErrNotImplemented, "slide.add"},
		{`{"op": "text.set", "at": "slide1/s1"}`, ErrMissing, "html"},
		{`{"op": "text.replace", "at": "slide1/s1", "with": "x"}`, ErrMissing, "select"},
		{`{"op": "text.replace", "at": "slide1/s1", "select": "", "with": "x"}`, ErrMissing, "select"},
		{`{"op": "text.replace", "at": "slide1/s1", "select": "x"}`, ErrMissing, "with"},
		{`{"op": "object.reorder", "at": "slide1/s1", "to": "slide1/s2"}`, ErrMissing, "position"},
	} {
		p, err := DecodePlan(strings.NewReader(`{"ops": [` + c.op + `]}`))
		if err != nil {
			t.Fatalf("%s: %v", c.op, err)
		}
		_, err = p.Compile()
		if !errors.Is(err, c.want) || !strings.Contains(err.Error(), c.msg) {
			t.Errorf("%s: err = %v, want %v naming %q", c.op, err, c.want, c.msg)
		}
	}
}

// slideXML returns slide1 of a deck.
func slideXML(t *testing.T, path string) string {
	t.Helper()
	p, err := opc.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	pt, err := p.Part(slide1)
	if err != nil {
		t.Fatal(err)
	}
	b, err := pt.Bytes()
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// TestAddWritesOnlyWhatThePlanSays: a new shape holds the template, the
// transform, the geometry and the properties the plan gives, nothing
// else, in schema order, in front of every other object.
func TestAddWritesOnlyWhatThePlanSays(t *testing.T) {
	path, _, _ := diagram(t)
	if err := Apply(path, compile(t, `{"ops": [
	  {"op": "object.add", "at": "slide1", "kind": "shape", "name": "Cache", "geom": "rect", "box": [100, 50, 90, 60],
	   "fill": {"scheme": "accent2"}}]}`)...); err != nil {
		t.Fatal(err)
	}
	s := slideXML(t, path)
	want := `<p:sp><p:nvSpPr><p:cNvPr id="1000" name="Cache"/><p:cNvSpPr/><p:nvPr/></p:nvSpPr><p:spPr><a:xfrm><a:off x="1270000" y="635000"/><a:ext cx="1143000" cy="762000"/></a:xfrm><a:prstGeom prst="rect"/><a:solidFill><a:schemeClr val="accent2"/></a:solidFill></p:spPr></p:sp></p:spTree>`
	if !strings.Contains(s, want) {
		t.Fatalf("new shape:\n%s", s[strings.LastIndex(s, "<p:sp>"):])
	}
}

// TestLineOverrideKeepsStyle: writing one outline property of an object
// whose line comes from p:style adds a local override and keeps the style.
func TestLineOverrideKeepsStyle(t *testing.T) {
	path, _, _ := diagram(t)
	if err := Apply(path, compile(t, `{"ops": [
	  {"op": "object.set", "at": "slide1/s70", "line": {"w": 3}}]}`)...); err != nil {
		t.Fatal(err)
	}
	s := slideXML(t, path)
	i := strings.Index(s, `name="Styled"`)
	sp := s[i : i+strings.Index(s[i:], "</p:sp>")]
	if !strings.Contains(sp, `<a:ln w="38100"/></p:spPr><p:style><a:lnRef idx="2">`) {
		t.Fatalf("styled shape:\n%s", sp)
	}
}

func TestSetRejects(t *testing.T) {
	for _, c := range []struct{ op, want string }{
		{`{"op": "object.set", "at": "slide1/s12", "from_glue": "s40:1"}`, "ambiguous"},
		{`{"op": "object.set", "at": "slide1/s10", "fill": {"scheme": "accent2", "hex": "#000000"}}`, "does not match"},
		{`{"op": "object.set", "at": "slide1/s10", "fill": {"type": "gradFill"}}`, "not editable"},
		{`{"op": "object.set", "at": "slide1/s50", "adj": {"adj": 1}}`, "adj is for presets"},
		{`{"op": "object.set", "at": "slide1/s50", "custom": {"gd": [["x", "val 1"]]}}`, "formulas"},
		{`{"op": "object.set", "at": "slide1/s10", "custom": {"rect": ["l", "t", "r", "b"]}}`, "give path first"},
		{`{"op": "object.set", "at": "slide1/s12", "path": "M 0 0 L 10 10"}`, "drawn by its preset"},
		{`{"op": "object.set", "at": "slide1/s10", "from_glue": "s11:1"}`, "only connectors"},
		{`{"op": "object.set", "at": "slide1/s12", "first": "v"}`, "elbow connectors"},
		{`{"op": "object.set", "at": "slide1/s13", "elbows": [[1, 2], [3, 4]]}`, "through adj"},
		{`{"op": "object.reorder", "at": "slide1/s21", "to": "slide1/s10", "position": "after"}`, "different containers"},
		{`{"op": "object.delete", "at": "slide1/s30"}`, ""},
	} {
		path, before, mtime := diagram(t)
		err := Apply(path, compile(t, `{"ops": [`+c.op+`]}`)...)
		if c.want == "" {
			if err != nil {
				t.Errorf("%s: %v", c.op, err)
			}
			continue
		}
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: err = %v, want %q", c.op, err, c.want)
		}
		assertUntouched(t, path, before, mtime)
	}
}

// TestMoveAndBackAfterAdd: d then -d on a new object, and a duplicate
// moved and moved back, read as they started.
func TestMoveAndBackAfterAdd(t *testing.T) {
	path, before, mtime := diagram(t)
	err := Apply(path, compile(t, `{"ops": [
	  {"op": "object.move", "at": "slide1/s12", "dx": 7.13, "dy": -3.2},
	  {"op": "object.move", "at": "slide1/s12", "dx": -7.13, "dy": 3.2},
	  {"op": "object.move", "at": "slide1/s26", "dx": 1, "dy": 1},
	  {"op": "object.move", "at": "slide1/s26", "dx": -1, "dy": -1}]}`)...)
	if err != nil {
		t.Fatal(err)
	}
	assertUntouched(t, path, before, mtime)
}
