package ops

import (
	"os"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/jhnoel/documentdriver/internal/ooxml/schema"

	"github.com/jhnoel/documentdriver/internal/cli"
)

// isPorted reports whether an operation runs: its Spec no longer embeds
// pending. The list comes from the op table, so porting an operation
// needs no edit here.
func isPorted(spec Spec) bool {
	_, pend := spec.(interface{ PendingOp() })
	return !pend
}

// portedInP4 are the operations ported in P4, which must stay ported.
var portedInP4 = []string{"revision.accept", "revision.reject", "paragraph.format", "paragraph.insert", "paragraph.delete", "comment.add"}

// TestOpTable checks the table against the contract (docs/contract.md,
// section 3): 42 operations, each with its own Spec type, description and
// run file, and each docxdriver operation reached by the oracle mapping.
func TestOpTable(t *testing.T) {
	if len(Ops) != 42 {
		t.Fatalf("%d operations, want 42", len(Ops))
	}
	seen := map[string]bool{}
	types := map[reflect.Type]string{}
	reached := map[string]bool{}
	for _, d := range Ops {
		if seen[d.Name] {
			t.Errorf("%s listed twice", d.Name)
		}
		seen[d.Name] = true
		noun, verb, ok := strings.Cut(d.Name, ".")
		if !ok || noun == "" || verb == "" || strings.ToLower(d.Name) != d.Name {
			t.Errorf("%s is not noun.verb", d.Name)
		}
		if _, err := os.Stat("run_" + strings.ReplaceAll(d.Name, ".", "_") + ".go"); err != nil {
			t.Errorf("%s has no run file: %v", d.Name, err)
		}
		typ := reflect.TypeOf(d.New())
		if other, dup := types[typ]; dup {
			t.Errorf("%s and %s share the Spec type %s", d.Name, other, typ)
		}
		types[typ] = d.Name
		if got, ok := OpOf(d.New()); !ok || got.Name != d.Name {
			t.Errorf("OpOf(%s) does not find %s", typ, d.Name)
		}
		if d.Oracle == "" {
			if d.Name != "text.insert" {
				t.Errorf("%s names no docxdriver operation", d.Name)
			}
		} else if o, ok := OracleLookup(d.Oracle); !ok {
			t.Errorf("%s runs as unknown %s", d.Name, d.Oracle)
		} else {
			reached[d.Oracle] = true
			// One docxdriver operation shared by several is told apart
			// by what each fixes.
			if len(ContractOps(d.Oracle)) > 1 && len(d.Fixed) == 0 {
				t.Errorf("%s shares %s and fixes nothing", d.Name, o.Name)
			}
		}
		if d.Description() == "" {
			t.Errorf("%s has no description", d.Name)
		}
		if slices.Contains(portedInP4, d.Name) && !isPorted(d.New()) {
			t.Errorf("%s was ported in P4 but its spec embeds pending again", d.Name)
		}
		// The Direct column follows the Track class where the class
		// decides it.
		switch {
		case d.Track == Untracked && d.Direct != Same, d.Track == DirectOnly && d.Direct != OnlyDirect,
			d.Direct == Same && d.Track != Untracked, d.Direct == OnlyDirect && d.Track != DirectOnly:
			t.Errorf("%s: Track %s and Direct %s disagree", d.Name, d.Track, d.Direct)
		}
		// The Direct column follows the Track class where the class
		// decides it.
		switch {
		case d.Track == Untracked && d.Direct != Same, d.Track == DirectOnly && d.Direct != OnlyDirect,
			d.Direct == Same && d.Track != Untracked, d.Direct == OnlyDirect && d.Track != DirectOnly:
			t.Errorf("%s: Track %s and Direct %s disagree", d.Name, d.Track, d.Direct)
		}
		for i, r := range d.Rules {
			if r.Msg == "" || r.Bad == nil {
				t.Errorf("%s rule %d: no message or check", d.Name, i)
			}
		}
	}
	if len(opDocs) != len(Ops) {
		t.Errorf("%d descriptions for %d operations", len(opDocs), len(Ops))
	}
	if n := len(OracleOps()); n != 34 {
		t.Errorf("%d docxdriver operations, want 34", n)
	}
	for _, o := range OracleOps() {
		if !reached[o.Name] {
			t.Errorf("no contract operation runs as %s", o.Name)
		}
	}
}

// TestFromOracle: a docxdriver plan translated into the contract's
// grammar names the contract's operations and carries each moved field.
func TestFromOracle(t *testing.T) {
	for _, c := range []struct{ oracle, want string }{
		{`{"op":"replace_text","at":"0000000A","select":"a","with":"b"}`, `{"at":"0000000A","op":"text.replace","select":"a","with":"b"}`},
		{`{"op":"insert_paragraph","at":"0000000A","position":"after","with":"<b>x</b>"}`, `{"at":"0000000A","html":"<b>x</b>","op":"paragraph.insert","position":"after"}`},
		{`{"op":"replace_equation","at":"0000000A","latex":"x","equation":2}`, `{"at":"0000000A/equation:2","latex":"x","op":"equation.replace"}`},
		{`{"op":"delete_equation","at":"0000000A"}`, `{"at":"0000000A","op":"equation.delete"}`},
		{`{"op":"edit_table","at":"table:1","action":{"kind":"delete"}}`, `{"at":"table:1","op":"table.delete"}`},
		{`{"op":"edit_table","at":"table:1","action":{"kind":"insert_rows","row":2,"position":"before","count":3,"as":"r"}}`,
			`{"as":"r","at":"table:1/row:2","count":3,"op":"row.insert","position":"before"}`},
		{`{"op":"edit_table","at":"$t","action":{"kind":"delete_columns","column":4,"count":1}}`, `{"at":"$t/column:4","count":1,"op":"column.delete"}`},
		{`{"op":"edit_table","at":"table:2","action":{"kind":"merge_cells","row":1,"column":2,"row_span":2,"column_span":1}}`,
			`{"at":"table:2/row:1/cell:2","column_span":1,"op":"cell.merge","row_span":2}`},
		{`{"op":"edit_table","at":"table:2","action":{"kind":"split_cell","row":1,"column":2}}`, `{"at":"table:2/row:1/cell:2","op":"cell.split"}`},
		{`{"op":"set_page_margins","section":2,"top":72}`, `{"at":"section:2","op":"section.set_margins","top":72}`},
		{`{"op":"set_section_layout","section_handle":"section:final","columns":2}`, `{"at":"section:final","columns":2,"op":"section.set_layout"}`},
		{`{"op":"set_header","section":1,"kind":"first","with":"<p>h</p>"}`, `{"at":"section:1","html":"<p>h</p>","kind":"first","op":"header.set"}`},
		{`{"op":"clear_footer"}`, `{"op":"footer.delete"}`},
		{`{"op":"comment_reply","comment_id":"3","text":"t"}`, `{"at":"comment:3","op":"comment.reply","text":"t"}`},
		{`{"op":"bookmark_rename","name":"Old","new_name":"New"}`, `{"at":"bookmark:Old","name":"New","op":"bookmark.rename"}`},
		{`{"op":"bookmark_move","name":"B","at":"0000000A","select":"x"}`, `{"at":"bookmark:B","op":"bookmark.move","select":"x","to":"0000000A"}`},
		{`{"op":"revision_settle","target":"all","action":"reject"}`, `{"all":true,"op":"revision.reject"}`},
		{`{"op":"revision_settle","target":"7","action":"accept"}`, `{"at":"revision:7","op":"revision.accept"}`},
		{`{"op":"convert_revisions_to_formatting","target":"all"}`, `{"all":true,"op":"revision.convert_to_formatting"}`},
		{`{"op":"convert_revisions_to_formatting","target":"all","at":["0000000A"]}`, `{"at":["0000000A"],"op":"revision.convert_to_formatting"}`},
		{`{"op":"convert_revisions_to_formatting","target":{"author":"Ann"},"at":["0000000A"]}`, `{"at":["0000000A"],"author":"Ann","op":"revision.convert_to_formatting"}`},
		{`{"op":"convert_revisions_to_formatting","target":{"author":"Ann"}}`, `{"all":true,"author":"Ann","op":"revision.convert_to_formatting"}`},
		{`{"op":"convert_revisions_to_formatting","target":{"id":"4"}}`, `{"at":"revision:4","op":"revision.convert_to_formatting"}`},
		// What the contract has no name for stays docxdriver's and is refused.
		{`{"op":"edit_table","at":"table:1","action":{"kind":"transpose"}}`, `{"action":{"kind":"transpose"},"at":"table:1","op":"edit_table"}`},
		{`{"op":"frobnicate","at":"x"}`, `{"at":"x","op":"frobnicate"}`},
	} {
		got, err := OpFromOracle([]byte(c.oracle))
		if err != nil || string(got) != c.want {
			t.Errorf("%s:\n got %s (%v)\nwant %s", c.oracle, got, err, c.want)
		}
	}
	// Every docxdriver operation has a translation the table knows.
	for _, o := range OracleOps() {
		for _, r := range o.Rules {
			if r.Example == "" {
				continue
			}
			got, err := OpFromOracle([]byte(r.Example))
			if err != nil {
				t.Fatalf("%s: %v", r.Example, err)
			}
			v, _ := parseJSON(got)
			if _, ok := Lookup(v.get("op").s); !ok {
				t.Errorf("%s translates to %s, which names no contract operation", r.Example, got)
			}
		}
	}
}

// TestNewPlan: a plan built from Go values is the plan its JSON parses
// to, checked by the same rules.
func TestNewPlan(t *testing.T) {
	p, d := NewPlan(zeroBase, "Ann", ModeTrack,
		&ReplaceText{At: "0000000A", Select: "a", With: "b", Occurrence: Ptr(uint32(2))},
		&InsertRows{At: "table:1/row:2", Position: "after", Count: 1, Alias: Ptr("r")},
		&AcceptRevisions{All: true},
		&FormatParagraph{At: "0000000A", Alignment: Ptr("CENTER")})
	if d != nil {
		t.Fatal(d)
	}
	want := `{"author":"Ann","base":"` + zeroBase + `","change_mode":"track","ops":[` +
		`{"at":"0000000A","occurrence":2,"op":"text.replace","select":"a","with":"b"},` +
		`{"as":"r","at":"table:1/row:2","count":1,"op":"row.insert","position":"after"},` +
		`{"all":true,"op":"revision.accept"},` +
		`{"alignment":"center","at":"0000000A","clear":[],"op":"paragraph.format"}]}`
	if got := p.CanonicalJSON(); got != want {
		t.Errorf("canonical JSON:\n got %s\nwant %s", got, want)
	}
	q, d := ParsePlanJSON([]byte(p.CanonicalJSON()))
	if d != nil || q.CanonicalJSON() != p.CanonicalJSON() {
		t.Errorf("the built plan does not parse back to itself: %v", d)
	}
	// The rules run: the first broken one refuses the plan with its path.
	_, d = NewPlan(zeroBase, "Ann", ModeTrack, &AcceptRevisions{All: true}, &DeleteRows{At: "table:1", Count: 1})
	if d == nil || d.Code != "invalid_operation" || d.Path != "ops[1].at" {
		t.Errorf("a bad address: %+v", d)
	}
	if _, d = NewPlan(zeroBase, "", ModeTrack, &AcceptRevisions{All: true}); d == nil || d.Code != "invalid_author" {
		t.Errorf("an empty author: %+v", d)
	}
	if _, d = NewPlan(zeroBase, "Ann", ModeTrack); d == nil || d.Code != "empty_plan" {
		t.Errorf("an empty plan: %+v", d)
	}
}

// TestRuleExamples checks that each rule's example breaks that rule and
// no earlier one, and gets the rule's message and path.
func TestRuleExamples(t *testing.T) {
	for _, d := range Ops {
		for i, r := range d.Rules {
			if r.Example == "" {
				continue
			}
			_, diag := ParsePlanJSON([]byte(wrapOps("[" + r.Example + "]")))
			if diag == nil || diag.Code != "invalid_operation" {
				t.Errorf("%s rule %d: example gives %+v, want invalid_operation", d.Name, i, diag)
				continue
			}
			var p Plan
			text := []byte(wrapOps("[" + r.Example + "]"))
			v, perr := cli.ParseJSON(text)
			if perr != nil {
				t.Fatal(perr)
			}
			dec := cli.NewDecoder(text)
			if dec.Struct(v, "", &p); dec.Err() != nil {
				t.Fatal(dec.Err())
			}
			for j, earlier := range d.Rules[:i] {
				if earlier.Bad(p.Ops[0].Spec) {
					t.Errorf("%s rule %d: example also breaks rule %d", d.Name, i, j)
				}
			}
			if !r.Bad(p.Ops[0].Spec) {
				t.Errorf("%s rule %d: example does not break it", d.Name, i)
			}
			if r.Explain == nil {
				if diag.Message != r.Msg || diag.Path != cli.Join("ops[0]", r.Path) {
					t.Errorf("%s rule %d: got %s %q, want %s %q", d.Name, i, diag.Path, diag.Message, cli.Join("ops[0]", r.Path), r.Msg)
				}
			}
		}
	}
}

// enumValues parses an enum tag: plan values, each optionally
// "=stored" when the document stores another value.
func enumValues(tag string) [][2]string {
	var out [][2]string
	for item := range strings.SplitSeq(tag, ",") {
		plan, stored, ok := strings.Cut(item, "=")
		if !ok {
			stored = plan
		}
		out = append(out, [2]string{plan, stored})
	}
	return out
}

// TestPlanEnumsInSchema checks that every plan enum value tagged with a
// WordprocessingML simple type maps into that type's enumeration.
func TestPlanEnumsInSchema(t *testing.T) {
	checked := 0
	var walk func(reflect.Type, string)
	walk = func(rt reflect.Type, at string) {
		for rt.Kind() == reflect.Pointer || rt.Kind() == reflect.Slice {
			rt = rt.Elem()
		}
		if rt.Kind() != reflect.Struct {
			return
		}
		for f := range rt.Fields() {
			f := f
			walk(f.Type, at+"."+f.Name)
			st, ok := f.Tag.Lookup("schema")
			if !ok {
				continue
			}
			allowed := schema.Enum(st)
			if len(allowed) == 0 {
				t.Errorf("%s: %s has no enumeration", at+"."+f.Name, st)
			}
			for _, v := range enumValues(f.Tag.Get("enum")) {
				checked++
				if !slices.Contains(allowed, v[1]) {
					t.Errorf("%s: %s (stored %s) is not in %s", at+"."+f.Name, v[0], v[1], st)
				}
			}
		}
	}
	for _, d := range Ops {
		walk(reflect.TypeOf(d.New()), d.Name)
	}
	for _, c := range unitEnums["HighlightColor"] {
		checked++
		if !slices.Contains(schema.Enum("w:ST_HighlightColor"), c) {
			t.Errorf("highlight %s is not in w:ST_HighlightColor", c)
		}
	}
	for _, k := range []string{"default", "first", "even"} {
		checked++
		if !slices.Contains(schema.Enum("w:ST_HdrFtr"), k) {
			t.Errorf("kind %s is not in w:ST_HdrFtr", k)
		}
	}
	if checked < 50 {
		t.Errorf("only %d enum values checked", checked)
	}
}

// TestEnumTagsMatchRules checks that the enum tags (which the schema and
// docs show) list the values the rules accept.
func TestEnumTagsMatchRules(t *testing.T) {
	plan := func(tag string) []string {
		var out []string
		for _, v := range enumValues(tag) {
			out = append(out, v[0])
		}
		return out
	}
	tags := map[string][]string{
		"list format":   listFormats,
		"break type":    sectionBreaks,
		"border style":  borderStyles,
		"text clear":    formatTextClear,
		"section break": sectionBreaks,
	}
	field := func(v any, name string) string {
		f, ok := reflect.TypeOf(v).Elem().FieldByName(name)
		if !ok {
			t.Fatalf("no field %s", name)
		}
		return f.Tag.Get("enum")
	}
	got := map[string][]string{
		"list format":   plan(field(&SetList{}, "Format")),
		"break type":    plan(field(&SetSectionLayout{}, "BreakType")),
		"border style":  plan(field(&TableBorder{}, "Style")),
		"text clear":    plan(field(&FormatText{}, "Clear")),
		"section break": plan(field(&SetSectionBoundary{}, "BreakType")),
	}
	for k, want := range tags {
		if !slices.Equal(got[k], want) {
			t.Errorf("%s: tag %v, rules %v", k, got[k], want)
		}
	}
}

func TestFormatFloat(t *testing.T) {
	for f, want := range map[float64]string{
		0: "0.0", 1: "1.0", 1.5: "1.5", 72: "72.0", 0.05: "0.05", 1e15: "1000000000000000.0",
		1e16: "1e+16", 1.5e16: "1.5e+16", 1e-5: "0.00001", 1e-6: "1e-6", 1.25e-7: "1.25e-7",
		123456789012345.6: "123456789012345.6", 4.611686018427388e17: "4.611686018427388e+17",
		-2.5: "-2.5", 1e300: "1e+300",
	} {
		if got := formatFloat(f); got != want {
			t.Errorf("formatFloat(%v) = %s, want %s", f, got, want)
		}
	}
}

func TestNewBookmarkName(t *testing.T) {
	for name, want := range map[string]string{
		"Clause_4_2":                  "",
		"Ünïcode":                     "",
		"_Toc1":                       `name "_Toc1" starts with "_"`,
		"Clause 4":                    `name "Clause 4" is not a valid bookmark name: use underscores instead of spaces, e.g. "Clause_4"`,
		"4x":                          `it starts with a digit`,
		"a-b":                         `it contains a character other than`,
		"":                            `it is empty`,
		strings.Repeat("a", 41):       `has 41 characters`,
		"a" + strings.Repeat("é", 39): "",
	} {
		got := newBookmarkName(name, "name")
		if want == "" && got != "" || !strings.Contains(got, want) {
			t.Errorf("newBookmarkName(%q) = %q, want it to contain %q", name, got, want)
		}
	}
}

// TestParsePlanEnvelope: the contract's envelope. base is optional,
// author is required only where a name is recorded, and every fault
// found without the document comes back together with its path.
func TestParsePlanEnvelope(t *testing.T) {
	ok := func(plan string) *Plan {
		t.Helper()
		p, errs := ParsePlan([]byte(plan))
		if errs != nil {
			t.Fatalf("%s: %v", plan, errs)
		}
		return p
	}
	if p := ok(`{"change_mode":"direct","ops":[{"op":"revision.accept","all":true}]}`); p.Base != "" || p.Author != "" || len(p.Ops) != 1 {
		t.Errorf("no base, no author in direct mode: %+v", p)
	}
	if p := ok(`{"base":"` + zeroBase + `","contract":1,"author":"Ann","change_mode":"track","ops":[{"op":"text.replace","at":"0000000A","select":"a","with":"b"}]}`); string(p.Base) != zeroBase || p.Author != "Ann" {
		t.Errorf("full envelope: %+v", p)
	}
	for _, c := range []struct {
		plan string
		want []string
	}{
		{`{"change_mode":"track","ops":[{"op":"revision.accept","all":true}]}`, []string{"invalid_plan author"}},
		{`{"change_mode":"direct","ops":[{"op":"comment.add","at":"0000000A","text":"t"}]}`, []string{"invalid_plan author"}},
		{`{"author":"","change_mode":"direct","ops":[{"op":"revision.accept","all":true}]}`, []string{"invalid_plan author"}},
		{`{"author":"Ann","ops":[]}`, []string{"invalid_plan ops", "invalid_plan change_mode"}},
		{`{"author":"Ann","change_mode":"suggest","ops":[{"op":"revision.accept","all":true}]}`, []string{"invalid_plan change_mode"}},
		{`{"author":"Ann","change_mode":"direct","preview_key":"x","ops":[{"op":"revision.accept","all":true}]}`, []string{"invalid_plan preview_key"}},
		{`{"author":"Ann","change_mode":"direct","ops":[{"op":"revision.accept","all":true}],"ops":[]}`, []string{"invalid_json "}},
		{`{"author":"Ann","change_mode":"direct","contract":2,"ops":[{"op":"revision.accept","all":true}]}`, []string{"invalid_plan contract"}},
		// Several faults, in plan order, each with its path.
		{`{"author":"Ann","change_mode":"direct","ops":[
			{"op":"replace_text","at":"0000000A","select":"a","with":"b"},
			{"op":"text.replace","at":"0000000A","select":"","with":"b"},
			{"op":"row.insert","at":"table:1/row:1","position":"sideways","count":1},
			{"op":"cell.merge","at":"table:1/row:1/cell:1","row_span":1,"column_span":1},
			{"op":"text.format","at":"0000000A","select":"x","bold":true,"colour":"#FF0000"},
			{"op":"revision.accept","all":true}]}`,
			[]string{"invalid_plan ops[0].op", "invalid_operation ops[1].select", "invalid_plan ops[2].position",
				"invalid_operation ops[3].column_span", "invalid_plan ops[4].colour"}},
	} {
		_, errs := ParsePlan([]byte(c.plan))
		var got []string
		for _, e := range errs {
			got = append(got, e.Code+" "+e.Path)
		}
		if !slices.Equal(got, c.want) {
			t.Errorf("%s:\n got %q\nwant %q", c.plan, got, c.want)
		}
	}
	// What ParsePlan accepts is what ParsePlanJSON accepts with the same
	// operations, and gives the same operations.
	a := ok(`{"author":"Ann","change_mode":"track","ops":[{"op":"paragraph.format","at":"0000000A","alignment":"CENTER","indent_left":36.5}]}`)
	b, d := ParsePlanJSON([]byte(wrapOps(`[{"op":"paragraph.format","at":"0000000A","alignment":"CENTER","indent_left":36.5}]`)))
	if d != nil || !reflect.DeepEqual(a.Ops, b.Ops) {
		t.Errorf("the two entry points decode operations differently: %v", d)
	}
}
