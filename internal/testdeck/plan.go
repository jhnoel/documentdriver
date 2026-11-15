package testdeck

import (
	"bytes"
	"encoding/json"
	"strings"
)

// LocalIDs rewrites a slide's JSON view so that object addresses are the
// bare ids on that slide (slide3/s17 becomes s17), for tests that work on
// one slide and write their operations through Plan.
func LocalIDs(view []byte, slide string) []byte {
	return bytes.ReplaceAll(view, []byte(`"`+slide+`/`), []byte(`"`))
}

// Plan builds a pptx plan's JSON from operations written against one
// slide: an `at` (or an object.reorder `to` that has a position) holding
// a bare object id (s17, s17~2) gets the slide's address prefix
// (slide3/s17); aliases ($name) and full addresses stay; an object.add
// without `at` is added to the slide. Tests that work on one slide write
// their operations short and get the contract's plan.
func Plan(slide string, specs ...map[string]any) []byte {
	q := func(v string) string {
		if strings.HasPrefix(v, "$") || strings.Contains(v, "/") {
			return v
		}
		return slide + "/" + v
	}
	ops := make([]map[string]any, len(specs))
	for i, spec := range specs {
		out := make(map[string]any, len(spec)+1)
		for k, v := range spec {
			out[k] = v
		}
		switch at := out["at"].(type) {
		case string:
			out["at"] = q(at)
		case []string:
			qs := make([]string, len(at))
			for j, a := range at {
				qs[j] = q(a)
			}
			out["at"] = qs
		case nil:
			if out["op"] == "object.add" {
				out["at"] = slide
			}
		}
		if to, ok := out["to"].(string); ok && out["op"] == "object.reorder" && out["position"] != nil {
			out["to"] = q(to)
		}
		ops[i] = out
	}
	b, err := json.Marshal(map[string]any{"ops": ops})
	if err != nil {
		panic(err) // the specs are test literals
	}
	return b
}
