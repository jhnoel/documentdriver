package main

import (
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/jhnoel/documentdriver/internal/docx/ops"
)

// nullFields names fields that accept an explicit null. None do:
// internal/cli refuses null everywhere ("null is not a value; leave
// the field out"), including inside arrays, custom decoders and
// any-typed values, before any of them run. If a field ever does
// accept null, list it here and keep null for exactly that field.
var nullFields = map[string]bool{}

// TestSchemaAllowsNoNull walks every operation's properties in the
// generated schema and asserts none allows null, except a field in
// nullFields.
func TestSchemaAllowsNoNull(t *testing.T) {
	var check func(path string, v any)
	check = func(path string, v any) {
		switch x := v.(type) {
		case obj:
			keys := make([]string, 0, len(x))
			for k := range x {
				keys = append(keys, k)
			}
			slices.Sort(keys)
			for _, k := range keys {
				if k == "default" && x[k] == nil && !nullFields[path] {
					t.Errorf("%s: schema offers \"default\": null", path)
				}
				if k == "type" && allowsNull(x[k]) && !nullFields[path] {
					t.Errorf("%s: schema allows null (type %v)", path, x[k])
				}
				check(path+"/"+k, x[k])
			}
		case map[string]obj:
			keys := make([]string, 0, len(x))
			for k := range x {
				keys = append(keys, k)
			}
			slices.Sort(keys)
			for _, k := range keys {
				check(path+"/"+k, x[k])
			}
		case []any:
			for i, e := range x {
				check(fmt.Sprintf("%s/%d", path, i), e)
			}
		}
	}
	check("", planSchema())
	if len(nullFields) != 0 {
		for f := range nullFields {
			t.Logf("field %s accepts null (exception)", f)
		}
	}
}

func allowsNull(v any) bool {
	switch x := v.(type) {
	case string:
		return x == "null"
	case []any:
		for _, e := range x {
			if e == "null" {
				return true
			}
		}
	}
	return false
}

// TestParserRefusesNull checks that the sample optional fields the
// schema no longer marks nullable are refused by the parser when
// written as null, so schema and parser cannot drift.
func TestParserRefusesNull(t *testing.T) {
	for _, c := range []struct {
		plan string
		path string
	}{
		{`{"change_mode":"direct","ops":[{"op":"text.replace","at":"0000000A","select":"a","with":"b","occurrence":null}]}`, "ops[0].occurrence"},
		{`{"change_mode":"direct","ops":[{"op":"paragraph.insert","at":"0000000A","position":"before","html":"x","style":null}]}`, "ops[0].style"},
		{`{"change_mode":"direct","ops":[{"op":"section.set_margins","top":null}]}`, "ops[0].top"},
	} {
		_, errs := ops.ParsePlan([]byte(c.plan))
		if len(errs) == 0 {
			t.Errorf("%s: parser accepted an explicit null", c.plan)
			continue
		}
		var got []string
		for _, e := range errs {
			got = append(got, e.Code+" "+e.Path+" "+e.Error())
		}
		joined := strings.Join(got, "; ")
		if !strings.Contains(joined, "invalid_plan "+c.path) || !strings.Contains(joined, "null is not a value") {
			t.Errorf("%s: got %s, want invalid_plan at %s for null", c.plan, joined, c.path)
		}
	}
}
