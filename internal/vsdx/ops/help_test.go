package ops

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/jhnoel/documentdriver/internal/cli"
)

// TestHelpNotesEveryField checks the CC7 row for object.set (and, for
// every op, so help <op> can print a note per field): every field of
// every op's spec has a doc note.
func TestHelpNotesEveryField(t *testing.T) {
	for i := range Ops {
		def := &Ops[i]
		for _, f := range cli.FieldDocs(reflect.TypeOf(def.New()).Elem(), nil) {
			if f.Doc == "" {
				t.Errorf("%s: field %s has no note", def.Name, f.Name)
			}
		}
	}
}

// TestHelpRegistryComplete checks the CC7 row that for every op in
// the table the help has the three sections: every implemented op has
// registered rules and refuses, both non-empty.
func TestHelpRegistryComplete(t *testing.T) {
	for i := range Ops {
		def := &Ops[i]
		if !def.Implemented() {
			continue
		}
		info, ok := HelpFor(def.Name)
		if !ok {
			t.Errorf("%s: no help data registered", def.Name)
			continue
		}
		if len(info.Rules) == 0 {
			t.Errorf("%s: no rules registered", def.Name)
		}
		if len(info.Refuses) == 0 {
			t.Errorf("%s: no refuses registered", def.Name)
		}
	}
}

// strPtr returns a pointer to s.
func strPtr(s string) *string { return &s }

// helpRuleCases derive the rules list from the check methods'
// messages: each case is a spec that breaks exactly that check, and
// want is the message checkSpec returns for it. The html cases are
// separate because checkHTML's messages come from the text grammar,
// so their want is "" and any grammar message stands for htmlRule.
func helpRuleCases() []struct {
	op   string
	spec Spec
	want string
} {
	box := []float64{0, 0, 10, 10}
	pin := []float64{0, 0}
	pt := []float64{0, 0}
	pt2 := []float64{10, 10}
	return []struct {
		op   string
		spec Spec
		want string
	}{
		{"object.add", &ObjectAddSpec{At: "x", Geom: "rect", Box: box}, "at must be a page or shape address or $name"},
		{"object.add", &ObjectAddSpec{At: "page0", As: "1bad", Geom: "rect", Box: box}, "as must match [A-Za-z_][A-Za-z0-9_]*"},
		{"object.add", &ObjectAddSpec{At: "page0", Kind: "shape", Geom: "rect", Box: box}, "kind must be connector"},
		{"object.add", &ObjectAddSpec{At: "page0", Geom: "triangle", Box: box}, "geom must be rect or ellipse"},
		{"object.add", &ObjectAddSpec{At: "page0", Master: "page0/s1", Box: box}, "master must be a master address or a master name"},
		{"object.add", &ObjectAddSpec{At: "page0", Geom: "rect", Master: "master2", Box: box}, "object.add takes master or geom, not both"},
		{"object.add", &ObjectAddSpec{At: "page0", Kind: "connector", Master: "master2"}, "connector takes no master"},
		{"object.add", &ObjectAddSpec{At: "page0", Kind: "connector", Geom: "rect"}, "connector takes no geom"},
		{"object.add", &ObjectAddSpec{At: "page0", Box: box}, "object.add needs master, geom or kind connector"},
		{"object.add", &ObjectAddSpec{At: "page0", Kind: "connector", Box: box}, "connector takes no box"},
		{"object.add", &ObjectAddSpec{At: "page0", Kind: "connector", Pin: pin}, "connector takes no pin"},
		{"object.add", &ObjectAddSpec{At: "page0", Geom: "rect", Box: box, From: pt}, "shape takes no from"},
		{"object.add", &ObjectAddSpec{At: "page0", Geom: "rect", Box: box, To: pt}, "shape takes no to"},
		{"object.add", &ObjectAddSpec{At: "page0", Geom: "rect", Box: box, FromGlue: "s1"}, "shape takes no from_glue"},
		{"object.add", &ObjectAddSpec{At: "page0", Geom: "rect", Box: box, ToGlue: "s1"}, "shape takes no to_glue"},
		{"object.add", &ObjectAddSpec{At: "page0", Geom: "rect"}, "shape needs box or pin"},
		{"object.add", &ObjectAddSpec{At: "page0", Geom: "rect", Box: box, Pin: pin}, "shape takes box or pin, not both"},
		{"object.add", &ObjectAddSpec{At: "page0", Geom: "rect", Box: []float64{0, 0, 0, 10}}, "box must be four numbers with positive width and height"},
		{"object.add", &ObjectAddSpec{At: "page0", Geom: "rect", Pin: []float64{0}}, "pin must be two numbers"},
		{"object.add", &ObjectAddSpec{At: "page0", Kind: "connector", From: []float64{0}, To: pt2}, "from must be two numbers"},
		{"object.add", &ObjectAddSpec{At: "page0", Kind: "connector", From: pt, To: []float64{0}}, "to must be two numbers"},
		{"object.add", &ObjectAddSpec{At: "page0", Kind: "connector", To: pt2}, "connector needs from or from_glue"},
		{"object.add", &ObjectAddSpec{At: "page0", Kind: "connector", From: pt}, "connector needs to or to_glue"},
		{"object.add", &ObjectAddSpec{At: "page0", Kind: "connector", FromGlue: "bogus", To: pt2}, "from_glue must name a shape or $name, with an optional point"},
		{"object.add", &ObjectAddSpec{At: "page0", Kind: "connector", From: pt, ToGlue: "bogus"}, "to_glue must name a shape or $name, with an optional point"},
		{"object.add", &ObjectAddSpec{At: "page0", Geom: "rect", Box: box, HTML: strPtr("<bogus>x</bogus>")}, ""},
		{"object.set", &ObjectSetSpec{At: "x", Box: box}, "at must be a shape address or $name"},
		{"object.set", &ObjectSetSpec{At: "page0/s1", Box: []float64{0, 0, 0, 10}}, "box must be four numbers with positive width and height"},
		{"object.set", &ObjectSetSpec{At: "page0/s1", Box: box, From: []float64{0}}, "from must be two numbers"},
		{"object.set", &ObjectSetSpec{At: "page0/s1", Box: box, From: pt, To: []float64{0}}, "to must be two numbers"},
		{"object.set", &ObjectSetSpec{At: "page0/s1", Box: box, FromGlue: strPtr("bogus")}, "from_glue must name a shape or $name, with an optional point, or none"},
		{"object.set", &ObjectSetSpec{At: "page0/s1", Box: box, ToGlue: strPtr("bogus")}, "to_glue must name a shape or $name, with an optional point, or none"},
		{"object.set", &ObjectSetSpec{At: "page0/s1"}, "object.set needs a field to set"},
		{"text.set", &TextSetSpec{At: "x", HTML: "<p>x</p>"}, "at must be a shape address or $name"},
		{"text.set", &TextSetSpec{At: "page0/s1", HTML: "<p>x</p>", Delete: []string{"x"}}, "delete must list field addresses or $name"},
		{"text.set", &TextSetSpec{At: "page0/s1", HTML: "<bogus>x</bogus>"}, ""},
		{"prop.add", &PropAddSpec{At: "x", Name: "Cost", Type: "string"}, "at must be a shape address or $name"},
		{"prop.add", &PropAddSpec{At: "page0/s1", Type: "string"}, "name must not be empty"},
		{"prop.add", &PropAddSpec{At: "page0/s1", Name: "1Cost", Type: "string"}, "name must be ASCII letters, digits and _, not starting with a digit"},
		{"prop.add", &PropAddSpec{At: "page0/s1", Name: "Cost", Type: "date", Value: map[string]any{"date": "not-a-date"}}, "date value must be YYYY-MM-DDThh:mm:ss"},
		{"prop.set", &PropSetSpec{At: "page0/s1", Value: "x"}, "at must name a shape data row or $name"},
		{"prop.set", &PropSetSpec{At: "page0/s1/prop:Cost", Value: map[string]any{"date": "not-a-date"}}, "date value must be YYYY-MM-DDThh:mm:ss"},
		{"page.add", &PageAddSpec{Name: ""}, "name must not be empty"},
		{"page.add", &PageAddSpec{Name: "X", As: "1bad"}, "as must match [A-Za-z_][A-Za-z0-9_]*"},
		{"page.add", &PageAddSpec{Name: "X", At: "x", Position: "before"}, "at must be a page address or $name"},
		{"page.add", &PageAddSpec{Name: "X", Orientation: "diagonal"}, "orientation must be portrait or landscape"},
		{"page.add", &PageAddSpec{Name: "X", Size: []float64{0, 0}}, "size must be two positive numbers"},
		{"page.add", &PageAddSpec{Name: "X", At: "page0"}, "at and position go together: the page to add next to, and before or after"},
		{"page.rename", &PageRenameSpec{At: "x", Name: "X"}, "at must be a page address or $name"},
		{"page.rename", &PageRenameSpec{At: "page0"}, "name must not be empty"},
		{"page.set", &PageSetSpec{At: "x", Size: []float64{100, 100}}, "at must be a page address or $name"},
		{"page.set", &PageSetSpec{At: "page0", Orientation: "diagonal"}, "orientation must be portrait or landscape"},
		{"page.set", &PageSetSpec{At: "page0", Size: []float64{0, 0}}, "size must be two positive numbers"},
		{"page.set", &PageSetSpec{At: "page0"}, "page.set needs orientation or size"},
		{"object.move", &ObjectMoveSpec{At: "x"}, "at must be a shape address or $name"},
		{"object.delete", &ObjectDeleteSpec{At: "x"}, "at must be a shape address or $name"},
		{"object.reorder", &ObjectReorderSpec{At: "x", To: "front"}, "at must be a shape address or $name"},
		{"object.reorder", &ObjectReorderSpec{At: "page0/s1", To: "bogus"}, "to must be front, back or a shape address"},
		{"object.reorder", &ObjectReorderSpec{At: "page0/s1", To: "front", Position: "before"}, "position goes with a shape in to, not front or back"},
		{"object.reorder", &ObjectReorderSpec{At: "page0/s1", To: "page0/s2"}, "position (before or after) is needed with a shape in to"},
		{"text.replace", &TextReplaceSpec{At: "x", Select: "a", With: "b"}, "at must be a shape or page address or $name"},
		{"text.replace", &TextReplaceSpec{At: "page0/s1", With: "b"}, "select must not be empty"},
		{"text.replace", &TextReplaceSpec{At: "page0/s1", Select: "a", With: "b", Occurrence: -1}, "occurrence is 1 or more"},
		{"text.replace", &TextReplaceSpec{At: "page0/s1", Select: "a", With: "b", Occurrence: 1, All: true}, "occurrence and all exclude each other"},
		{"prop.delete", &PropDeleteSpec{At: "page0/s1"}, "at must be a shape data row address or $name"},
		{"page.delete", &PageDeleteSpec{At: "x"}, "at must be a page address or $name"},
		{"page.reorder", &PageReorderSpec{At: "x", To: "page1", Position: "before"}, "at must be a page address or $name"},
		{"page.reorder", &PageReorderSpec{At: "page0", To: "x", Position: "before"}, "to must be a page address or $name"},
	}
}

// TestHelpRulesMatchCheck checks the CC7 drift row: every message a
// check method can return (derived by running checkSpec on a spec
// that breaks it) appears in the op's help rules, and every help
// rule is such a message, so the two cannot drift. The html rule is
// the one exception: checkHTML's messages come from the text
// grammar, so a bad html stands for htmlRule instead of a literal
// message. Adding a message to a check without a case and a rules
// line fails this test.
func TestHelpRulesMatchCheck(t *testing.T) {
	cases := helpRuleCases()
	seen := map[string]map[string]bool{}
	for _, tc := range cases {
		msg, _, bad := checkSpec(tc.spec)
		if !bad {
			t.Errorf("%s: case broke no check, want %q", tc.op, tc.want)
			continue
		}
		info, ok := HelpFor(tc.op)
		if !ok {
			t.Errorf("%s: no help data registered", tc.op)
			continue
		}
		if tc.want == "" {
			if !slices.Contains(info.Rules, htmlRule) {
				t.Errorf("%s: html case message %q, rules lack %q", tc.op, msg, htmlRule)
			}
			continue
		}
		if msg != tc.want {
			t.Errorf("%s: checkSpec = %q, want %q", tc.op, msg, tc.want)
		}
		if !slices.Contains(info.Rules, msg) {
			t.Errorf("%s: check message %q is not in help rules %v", tc.op, msg, info.Rules)
		}
		if seen[tc.op] == nil {
			seen[tc.op] = map[string]bool{}
		}
		seen[tc.op][msg] = true
	}
	for i := range Ops {
		def := &Ops[i]
		if !def.Implemented() {
			continue
		}
		info, _ := HelpFor(def.Name)
		for _, rule := range info.Rules {
			if rule == htmlRule {
				continue
			}
			if !seen[def.Name][rule] {
				t.Errorf("%s: help rule %q is not a message any case breaks", def.Name, rule)
			}
		}
	}
}

// TestHelpObjectSetRemovedRule checks the CC7 row as CC10 left it:
// CC10 removed the from/from_glue exclusivity check, so neither the
// check nor help object.set's rules carry its message.
func TestHelpObjectSetRemovedRule(t *testing.T) {
	info, _ := HelpFor("object.set")
	for _, rule := range info.Rules {
		if strings.Contains(rule, "connector takes from or from_glue, not both") {
			t.Errorf("object.set rules carry the removed rule %q", rule)
		}
	}
}

// checkSourceMessages returns every string literal returned as the
// first result of a `return "msg", "field", true` in the non-test
// check sources of package ops (rules.go, the rules_<topic>.go
// files and the spec_*.go files), as TestHelpRulesFromCheckSources
// and the proof it makes need: the messages the check methods state
// as literals. Messages built by needAddress or by the text grammar
// are not literals and are covered by TestHelpRulesMatchCheck's
// cases instead.
func checkSourceMessages(t *testing.T) []string {
	t.Helper()
	files := []string{"rules.go", "rules_html.go", "rules_page.go", "rules_prop.go"}
	specs, err := filepath.Glob("spec_*.go")
	if err != nil {
		t.Fatalf("Glob(spec_*.go): %v", err)
	}
	files = append(files, specs...)
	var out []string
	for _, name := range files {
		fset := token.NewFileSet()
		f, err := parser.ParseFile(fset, name, nil, 0)
		if err != nil {
			t.Fatalf("ParseFile(%s): %v", name, err)
		}
		ast.Inspect(f, func(n ast.Node) bool {
			ret, ok := n.(*ast.ReturnStmt)
			if !ok || len(ret.Results) != 3 {
				return true
			}
			if id, ok := ret.Results[2].(*ast.Ident); !ok || id.Name != "true" {
				return true
			}
			lit, ok := ret.Results[0].(*ast.BasicLit)
			if !ok || lit.Kind != token.STRING {
				return true
			}
			msg, err := strconv.Unquote(lit.Value)
			if err != nil {
				t.Fatalf("Unquote(%s in %s): %v", lit.Value, name, err)
			}
			out = append(out, msg)
			return true
		})
	}
	return out
}

// TestHelpRulesFromCheckSources checks the CC7 drift row a second
// way, deriving the rules list from the check methods' own source:
// every literal message a check can return appears in some op's
// help rules. Adding a message to a check without adding it to the
// op's rules fails this test (proved by adding one). The want-fail
// half is in TestHelpRulesMatchCheck, which proves each help rule
// is a message a check returns.
func TestHelpRulesFromCheckSources(t *testing.T) {
	var rules []string
	for i := range Ops {
		info, _ := HelpFor(Ops[i].Name)
		rules = append(rules, info.Rules...)
	}
	for _, msg := range checkSourceMessages(t) {
		if !slices.Contains(rules, msg) {
			t.Errorf("check message %q is not in any op's help rules", msg)
		}
	}
}
