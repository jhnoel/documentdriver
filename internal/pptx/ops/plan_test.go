package ops

import (
	"strings"
	"testing"
)

func TestDecodePlan(t *testing.T) {
	p, err := DecodePlan(strings.NewReader(`{"ops": [
		{"op": "object.set", "at": "slide1/s12", "box": [72, 120, 180, 54], "rot": 0},
		{"op": "object.move", "at": "slide1/s13", "dx": 10},
		{"op": "object.add", "at": "slide1", "as": "cache", "kind": "shape", "geom": "rect",
		 "box": [300, 200, 90, 60], "fill": {"scheme": "accent2"}, "html": "Cache"}
	]}`))
	if err != nil {
		t.Fatal(err)
	}
	if p.Units != "pt" || len(p.Ops) != 3 {
		t.Fatalf("plan = %+v", p)
	}
	set := p.Ops[0].(*SetSpec)
	if !set.Box.Set || set.Box.V != [4]float64{72, 120, 180, 54} {
		t.Errorf("box = %+v", set.Box)
	}
	// Zero is a value, distinct from absent.
	if !set.Rot.Set || set.Rot.V != 0 || set.FlipH.Set {
		t.Errorf("rot = %+v, flipH = %+v", set.Rot, set.FlipH)
	}
	mv := p.Ops[1].(*MoveSpec)
	if !mv.DX.Set || mv.DY.Set {
		t.Errorf("move = %+v", mv)
	}
	add := p.Ops[2].(*AddSpec)
	if !add.Fill.Set || add.Fill.V.Color.Scheme.V != "accent2" || add.Line.Set {
		t.Errorf("add = %+v", add)
	}
}

func TestDecodePlanRejects(t *testing.T) {
	for _, c := range []struct{ plan, want string }{
		{`{"ops": [{"op": "object.set", "at": "slide1/s1", "bx": [1,2,3,4]}]}`, `unknown field "bx"`},
		{`{"extra": 1, "ops": [{"op": "object.delete", "at": "slide1/s1"}]}`, `unknown field "extra"`},
		{`{"slide": "slide1", "ops": [{"op": "object.delete", "at": "slide1/s1"}]}`, `unknown field "slide"`},
		{`{"ops": [{"op": "object.delete", "at": "slide1/s1", "at": "slide1/s2"}]}`, `duplicate key "at"`},
		{`{"ops": [{"op": "object.delete"}]}`, `missing field "at"`},
		{`{"ops": [{"op": "object.zap"}]}`, `unknown operation "object.zap"`},
		{`{"ops": [{"op": "object.set", "at": "slide1/s1", "rot": null}]}`, `null is not a value`},
		{`{"ops": [{"op": "object.add", "at": "slide1", "kind": "shape", "fill": {"shceme": "x"}}]}`, `unknown field "shceme"`},
		{`{"units": "px", "ops": []}`, `units is pt or emu`},
		{`{"ops": []}`, `at least one operation`},
		{`{"operations": []}`, `unknown field "operations"`},
		{`{"ops": []} {}`, `unexpected text after the JSON value`},
	} {
		_, err := DecodePlan(strings.NewReader(c.plan))
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: err = %v, want %q", c.plan, err, c.want)
		}
	}
}
