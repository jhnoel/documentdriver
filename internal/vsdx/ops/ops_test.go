package ops

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jhnoel/documentdriver/internal/cli"
	"github.com/jhnoel/documentdriver/internal/repo"
)

// createPlan reads a create plan of testdata/vsdx/create/plans.
func createPlan(t *testing.T, name string) []byte {
	t.Helper()
	root, err := repo.Root()
	if err != nil {
		t.Fatalf("repo root: %v", err)
	}
	b, err := os.ReadFile(filepath.Join(root, "testdata", "vsdx", "create", "plans", name))
	if err != nil {
		t.Fatalf("read %s: %v", name, err)
	}
	return b
}

// TestCreatePlans checks the create plans c2 to c6 decode with no
// error; c6's base is create's to refuse.
func TestCreatePlans(t *testing.T) {
	for _, name := range []string{"c2-shapes.json", "c3-pages.json", "c4-refused-master.json", "c5-refused-glue.json", "c6-refused-base.json"} {
		p, errs := ParsePlan(createPlan(t, name))
		if len(errs) != 0 || p == nil {
			t.Errorf("%s: plan %v errors %v, want no error", name, p, errs)
		}
	}
}

// TestParsePlanRows checks every O0 row: a plan's faults are the code
// and path of the row, in op order.
func TestParsePlanRows(t *testing.T) {
	for _, tc := range []struct {
		text string
		want []string // code@path
	}{
		{`{"ops":[]}`, []string{"invalid_plan@ops"}},
		{`{"units":"cm","ops":[{"op":"page.rename","at":"page0","name":"X"}]}`, []string{"invalid_plan@units"}},
		{`{"ops":[{"op":"object.add","at":"page0","geom":"rect","master":"master2","box":[0,0,10,10]}]}`, []string{"invalid_operation@ops[0].geom"}},
		{`{"ops":[{"op":"object.add","at":"page0","geom":"triangle","box":[0,0,10,10]}]}`, []string{"invalid_operation@ops[0].geom"}},
		{`{"ops":[{"op":"object.add","at":"page0","kind":"connector","box":[0,0,10,10],"from":[0,0],"to":[10,10]}]}`, []string{"invalid_operation@ops[0].box"}},
		{`{"ops":[{"op":"object.add","at":"page0","kind":"connector","to":[10,10]}]}`, []string{"invalid_operation@ops[0].from"}},
		{`{"ops":[{"op":"object.add","at":"page0","geom":"rect","box":[0,0,10,10],"fill":{"hex":"red"}}]}`, []string{"invalid_operation@ops[0].fill.hex"}},
		{`{"ops":[{"op":"page.set","at":"page0"}]}`, []string{"invalid_operation@ops[0]"}},
		{`{"ops":[{"op":"page.set","at":"page0","orientation":"upright"}]}`, []string{"invalid_operation@ops[0].orientation"}},
		{`{"ops":[{"op":"page.set","at":"page0","size":[0,10]}]}`, []string{"invalid_operation@ops[0].size"}},
		{`{"ops":[{"op":"object.add","at":"page0","geom":"triangle","box":[0,0,10,10]},{"op":"page.set","at":"page0","orientation":"upright"},{"op":"page.set","at":"page0","size":[0,10]}]}`,
			[]string{"invalid_operation@ops[0].geom", "invalid_operation@ops[1].orientation", "invalid_operation@ops[2].size"}},
	} {
		_, errs := ParsePlan([]byte(tc.text))
		var got []string
		for _, e := range errs {
			got = append(got, e.Code+"@"+e.Path)
		}
		if strings.Join(got, " ") != strings.Join(tc.want, " ") {
			t.Errorf("%s: got %v, want %v", tc.text, got, tc.want)
		}
	}
}

// TestGeomNamesRectEllipse checks the geom triangle message names
// rect and ellipse.
func TestGeomNamesRectEllipse(t *testing.T) {
	_, errs := ParsePlan([]byte(`{"ops":[{"op":"object.add","at":"page0","geom":"triangle","box":[0,0,10,10]}]}`))
	if len(errs) != 1 {
		t.Fatalf("geom triangle: errors %v, want one", errs)
	}
	if !strings.Contains(errs[0].Message, "rect") || !strings.Contains(errs[0].Message, "ellipse") {
		t.Errorf("geom triangle: message %q names rect and ellipse", errs[0].Message)
	}
}

// TestFillNone checks fill "none" decodes to {None}.
func TestFillNone(t *testing.T) {
	for _, text := range []string{
		`{"ops":[{"op":"object.add","at":"page0","geom":"rect","box":[0,0,10,10],"fill":"none"}]}`,
		`{"ops":[{"op":"object.set","at":"page0/s1","fill":"none"}]}`,
	} {
		p, errs := ParsePlan([]byte(text))
		if len(errs) != 0 || p == nil || len(p.Ops) != 1 {
			t.Fatalf("%s: plan %v errors %v", text, p, errs)
		}
		var fill *PaintSpec
		switch s := p.Ops[0].(type) {
		case *ObjectAddSpec:
			fill = s.Fill
		case *ObjectSetSpec:
			fill = s.Fill
		}
		if fill == nil || !fill.None || fill.Hex != "" {
			t.Errorf("%s: fill %+v, want {None}", text, fill)
		}
	}
}

// TestToGlueWhole checks to_glue "s3" decodes as whole-shape glue.
func TestToGlueWhole(t *testing.T) {
	for _, tc := range []struct {
		text string
		want string
	}{
		{`{"ops":[{"op":"object.set","at":"page0/s1","to_glue":"s3"}]}`, "s3"},
		{`{"ops":[{"op":"object.add","at":"page0","kind":"connector","from":[0,0],"to_glue":"s3"}]}`, "s3"},
	} {
		p, errs := ParsePlan([]byte(tc.text))
		if len(errs) != 0 || p == nil {
			t.Errorf("%s: plan %v errors %v, want no error", tc.text, p, errs)
			continue
		}
		switch s := p.Ops[0].(type) {
		case *ObjectSetSpec:
			if s.ToGlue == nil || *s.ToGlue != tc.want {
				t.Errorf("%s: to_glue %v, want %q", tc.text, s.ToGlue, tc.want)
			}
		case *ObjectAddSpec:
			if s.ToGlue != tc.want {
				t.Errorf("%s: to_glue %q, want %q", tc.text, s.ToGlue, tc.want)
			}
		}
	}
}

// TestParsePlanValid checks the valid O0 plans decode with no
// error.
func TestParsePlanValid(t *testing.T) {
	for _, text := range []string{
		`{"ops":[{"op":"object.add","at":"page0","geom":"rect","box":[0,0,10,10],"fill":{"hex":"#FF0000"}}]}`,
		`{"ops":[{"op":"object.add","at":"page0","kind":"connector","from_glue":"$a:2","to_glue":"$b:4"}]}`,
		`{"units":"in","ops":[{"op":"page.set","at":"page0","size":[8.5,11]}]}`,
		`{"ops":[{"op":"page.set","at":"page0","orientation":"landscape"}]}`,
		`{"ops":[{"op":"object.set","at":"$a","from_glue":"none"}]}`,
	} {
		p, errs := ParsePlan([]byte(text))
		if len(errs) != 0 || p == nil {
			t.Errorf("%s: plan %v errors %v, want no error", text, p, errs)
		}
	}
}

// FuzzParsePlan feeds arbitrary text to ParsePlan: it must return a
// plan or a diagnostic, never panic, and every fault carries a
// contract code.
func FuzzParsePlan(f *testing.F) {
	for _, s := range []string{
		`{"ops":[{"op":"object.add","at":"page0","geom":"rect","as":"a","box":[72,72,144,72],"html":"<p>Start</p>"}]}`,
		`{"ops":[{"op":"page.set","at":"page0","orientation":"landscape"},{"op":"page.add","name":"Wide","as":"p2","size":[1224,792]}]}`,
		`{"ops":[{"op":"object.add","at":"page0","master":"master2","pin":[100,100]}]}`,
		`{"ops":[]}`,
		`{"units":"cm","ops":[{"op":"page.rename","at":"page0","name":"X"}]}`,
		`{"ops":[{"op":"object.add","at":"page0","geom":"triangle","box":[0,0,10,10]}]}`,
		`{"ops":[{"op":"page.set","at":"page0","size":[0,10]}]}`,
		`{"ops":[{"op":"object.add","at":"page0","geom":"rect","box":[0,0,10,10],"fill":{"hex":"red"}}]}`,
		`{"ops":[{"op":"object.add","at":"page0","geom":"rect","box":[0,0,10,10],"fill":"none"}]}`,
		`{"ops":[{"op":"object.set","at":"page0/s1","to_glue":"s3"}]}`,
	} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, s string) {
		p, errs := ParsePlan([]byte(s))
		if (p == nil) == (len(errs) == 0) {
			t.Fatalf("plan %v and errors %v", p, errs)
		}
		for _, e := range errs {
			if e.Code == "" || e.Code == cli.CodeInternal {
				t.Fatalf("error %+v", e)
			}
		}
	})
}
