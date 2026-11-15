package apply

import (
	"slices"
	"strings"
	"testing"
)

// TestOpsOnInsertedParagraphs: list.set, section.set_boundary and
// paragraph.replace act on a paragraph an earlier op of the plan
// inserted, in both modes (the boundary direct only), and what an op
// deletes through its fields is in the result's deleted.
func TestOpsOnInsertedParagraphs(t *testing.T) {
	ins := `{"op":"paragraph.insert","at":"0000000A","position":"after","html":"new","as":"n"}`
	for _, c := range []struct {
		name, ops string
		modes     []string
	}{
		{"list.set", `[` + ins + `,{"op":"list.set","at":["$n"],"format":"bullet"}]`, []string{"track", "direct"}},
		{"paragraph.replace", `[` + ins + `,{"op":"paragraph.replace","at":"$n","html":"newer"}]`, []string{"track", "direct"}},
		{"section.set_boundary", `[` + ins + `,{"op":"section.set_boundary","at":"$n"}]`, []string{"direct"}},
	} {
		for _, mode := range c.modes {
			path := writeDocx(t, insertBody)
			r := commit(t, path, opsText(t, path, mode, c.ops))
			if r.Outcome != Committed {
				t.Errorf("%s %s: %+v", c.name, mode, r.Diagnostic)
			}
		}
	}
	for _, mode := range []string{"track", "direct"} {
		path := writeDocx(t, replaceBody)
		r := commit(t, path, opsText(t, path, mode, replaceOp("0000000A", "x", "0000000A/opaque:1", "0000000A/opaque:2", "0000000A/equation:1")))
		if r.Outcome != Committed {
			t.Fatalf("replace %s: %+v", mode, r.Diagnostic)
		}
		for _, want := range []string{"0000000A/opaque:1", "0000000A/opaque:2", "0000000A/equation:1"} {
			if !slices.Contains(r.Deleted, want) {
				t.Errorf("replace %s: deleted %v lacks %s", mode, r.Deleted, want)
			}
		}
	}
	path := writeDocx(t, strings.ReplaceAll(boundaryBody, `r:id`, `w:dummy`))
	r := commit(t, path, opsText(t, path, "direct", `[{"op":"section.set_boundary","at":"0000000B","remove":true}]`))
	if r.Outcome != Committed || !slices.Contains(r.Deleted, "section:0000000B") {
		t.Errorf("boundary remove: %s deleted %v", r.Outcome, r.Deleted)
	}
}
