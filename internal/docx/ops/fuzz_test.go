package ops

import "testing"

// FuzzParsePlan feeds arbitrary text to ParsePlanJSON: it must return a
// plan or a diagnostic, never panic, and a plan must re-parse from its
// own canonical JSON to the same canonical JSON.
func FuzzParsePlan(f *testing.F) {
	f.Add([]byte(wrapOps(`[{"op":"text.replace","at":"0000000A","select":"x","with":"y"}]`)))
	f.Add([]byte(wrapOps(`[{"op":"table.format","at":"table:1","format":{"width":{"unit":"percent","value":50},"borders":{"top":{"style":"single"}}}}]`)))
	f.Add([]byte(wrapOps(`[{"op":"paragraph.format","at":"0000000A","indent_left":-1.5,"line_spacing":{"mode":"multiple","value":1.15}}]`)))
	f.Add([]byte(wrapOps(`[{"op":"paragraph.insert","at":"table:+2","position":"after","html":["a","b"],"as":"g"}]`)))
	f.Add([]byte(wrapOps(`[{"op":"row.insert","at":"table:1/row:2","position":"before","count":2,"as":"r"},{"op":"revision.accept","all":true},{"op":"equation.delete","at":"0000000A/equation:2"}]`)))
	f.Add([]byte(`{"a":[1e400,-0,"\ud800"]}`))
	f.Fuzz(func(t *testing.T, text []byte) {
		p, d := ParsePlanJSON(text)
		if (p == nil) == (d == nil) {
			t.Fatalf("plan %v and diagnostic %v", p, d)
		}
		if p == nil {
			return
		}
		c := p.CanonicalJSON()
		q, d := ParsePlanJSON([]byte(c))
		if d != nil {
			t.Fatalf("canonical JSON %s does not parse: %v", c, d)
		}
		if q.CanonicalJSON() != c {
			t.Fatalf("canonical JSON is not stable:\n%s\n%s", c, q.CanonicalJSON())
		}
	})
}
