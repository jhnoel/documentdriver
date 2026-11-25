package ops_test

import (
	"strings"
	"testing"

	"github.com/jhnoel/documentdriver/internal/vsdx/vsdxtest"
)

// TestOpsOnPageAddedInPlan is the regression row for ops that looked
// their page up in the package as opened, which lacks a page an
// earlier op of the same plan added (not_found "no page1"): a
// connector added with an arrowhead, and object.set fill, line, name
// and shadow, text.set and text.replace on the new page, each in the
// plan that adds the page, write and read back.
func TestOpsOnPageAddedInPlan(t *testing.T) {
	head := `{"ops":[{"op":"page.add","name":"FIG. 2","as":"p1"},{"op":"object.add","at":"$p1","geom":"rect","box":[0,0,90,90],"html":"<p>a</p>"},`
	cases := []struct{ op, id, key, want string }{
		{`{"op":"object.add","at":"$p1","kind":"connector","from":[176,376],"to":[502,376],"line":{"w":1,"color":"#000000","end":"triangle"}}`, "page1/s2", "line", `"end":{"arrow":"triangle"}`},
		{`{"op":"object.set","at":"page1/s1","fill":{"pattern":"hatch_up"}}`, "page1/s1", "fill", `"pattern":"hatch_up"`},
		{`{"op":"object.set","at":"page1/s1","line":{"w":2}}`, "page1/s1", "line", `"w":2`},
		{`{"op":"object.set","at":"page1/s1","name":"Box"}`, "page1/s1", "name", `"Box"`},
		{`{"op":"object.set","at":"page1/s1","shadow":{"color":"#000000","transparency":0,"offset":[2,2]}}`, "page1/s1", "shadow", `"offset":[2,2]`},
		{`{"op":"text.set","at":"page1/s1","html":"<p>b</p>"}`, "page1/s1", "text", `"b"`},
		{`{"op":"text.replace","at":"page1","select":"a","with":"c"}`, "page1/s1", "text", `"c"`},
	}
	for _, tc := range cases {
		out, _ := refusalOut(t, vsdxtest.Blank(t), head+tc.op+`]}`)
		if got := string(readPageObj(t, out, "page1", tc.id)[tc.key]); !strings.Contains(got, tc.want) {
			t.Errorf("%s: %s = %s, want it to contain %s", tc.op, tc.key, got, tc.want)
		}
	}
}
