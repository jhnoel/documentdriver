package main

import (
	"bytes"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/jhnoel/documentdriver/internal/docx/ops"
	"github.com/jhnoel/documentdriver/internal/repo"
)

func testConfig(t *testing.T) *config {
	t.Helper()
	root, err := repo.Root()
	if err != nil {
		t.Fatal(err)
	}
	return &config{root: root, baseline: filepath.Join(root, "tools", "oracle", "baseline")}
}

func TestPlanTOML(t *testing.T) {
	v, err := parseJV([]byte(`{"base":"sha256:00","author":"A","change_mode":"track","ops":[` +
		`{"op":"format_text","at":"0000000A","select":"a\"b\\c\u0001\u007f","font_size":72.0,"occurrence":2,"clear":[],"x":null},` +
		`{"op":"edit_table","at":"table:1","action":{"kind":"insert_rows","row":1,"count":12345678901234567890}}]}`))
	if err != nil {
		t.Fatal(err)
	}
	got, err := planTOML(v)
	if err != nil {
		t.Fatal(err)
	}
	want := `base = "sha256:00"
author = "A"
change_mode = "track"

[[ops]]
op = "format_text"
at = "0000000A"
select = "a\"b\\c\u0001\u007F"
font_size = 72.0
occurrence = 2
clear = []

[[ops]]
op = "edit_table"
at = "table:1"
action = { kind = "insert_rows", row = 1, count = 12345678901234567890.0 }
`
	if got != want {
		t.Fatalf("planTOML:\n%s\nwant:\n%s", got, want)
	}
}

func TestRelabel(t *testing.T) {
	in := `{"markup":"<ins data-docx-revision=\"9\">a</ins><del data-docx-revision=\"4\">b</del><ins data-docx-revision=\"9\">c</ins><span data-docx-comment-start=\"7\"></span>","comments":[{"id":"7","replies":[{"id":"8","parentId":"7"}]}]}`
	want := `{"markup":"<ins data-docx-revision=\"r1\">a</ins><del data-docx-revision=\"r2\">b</del><ins data-docx-revision=\"r1\">c</ins><span data-docx-comment-start=\"c1\"></span>","comments":[{"id":"c1","replies":[{"id":"c2","parentId":"c1"}]}]}`
	if got := relabel("doc", in); got != want {
		t.Fatalf("relabel:\n%s\nwant\n%s", got, want)
	}
}

// TestTranslateRoundTrip: every stored plan (contract form) translates
// down to docxdriver's form, and up again to itself; text.insert has no
// docxdriver op and is checked through its equivalent.
func TestTranslateRoundTrip(t *testing.T) {
	c := testConfig(t)
	plans, err := c.loadPlans()
	if err != nil {
		t.Fatal(err)
	}
	if len(plans) == 0 {
		t.Skip("no plans recorded")
	}
	for _, p := range plans {
		list := p.Ops
		if len(p.Equivalent) > 0 {
			list = p.Equivalent
		}
		if p.Plan != "" || len(list) == 0 || p.Kind == kindSchema || p.Kind == kindRule {
			continue // deliberately broken shapes translate best-effort only
		}
		down, err := downTranslate(`{"ops":` + string(list) + `}`)
		if err != nil {
			t.Errorf("%s: %v", p.ID, err)
			continue
		}
		v, err := parseJV([]byte(down))
		if err != nil {
			t.Fatal(err)
		}
		up, err := upOps(encodeJV(v.get("ops")))
		if err != nil {
			t.Fatal(err)
		}
		orig, err := parseJV(list)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(list), `"all":true`) && strings.Contains(string(list), "text.replace") {
			continue // text.replace all runs as revise_text; not invertible by design
		}
		if strings.Contains(string(list), `"delete":`) {
			continue // the delete field has no docxdriver form
		}
		if !sameJSON(encodeJV(orig), up) {
			t.Errorf("%s: round trip\n  %s\n->%s\n->%s", p.ID, encodeJV(orig), down, up)
		}
	}
}

// sameJSON compares two JSON texts as values (member order aside).
func sameJSON(a, b string) bool {
	va, err1 := parseJV([]byte(a))
	vb, err2 := parseJV([]byte(b))
	return err1 == nil && err2 == nil && canonJV(va) == canonJV(vb)
}

func canonJV(v *jv) string {
	if v.kind == 'o' {
		parts := make([]string, 0, len(v.members))
		for _, m := range v.members {
			parts = append(parts, jsonString(m.key)+":"+canonJV(m.val))
		}
		sortStrings(parts)
		return "{" + strings.Join(parts, ",") + "}"
	}
	if v.kind == 'a' {
		parts := make([]string, len(v.items))
		for i, it := range v.items {
			parts[i] = canonJV(it)
		}
		return "[" + strings.Join(parts, ",") + "]"
	}
	return encodeJV(v)
}

func sortStrings(s []string) { slices.Sort(s) }

// TestPlanCoverage: every op of the table has positive and negative
// cases, every rule with an example its own case, and every plan id is
// unique and filed under its op.
func TestPlanCoverage(t *testing.T) {
	c := testConfig(t)
	plans, err := c.loadPlans()
	if err != nil {
		t.Fatal(err)
	}
	if len(plans) == 0 {
		t.Skip("no plans recorded")
	}
	if err := checkPlanIDs(plans); err != nil {
		t.Fatal(err)
	}
	have := map[string]map[string]bool{}
	rules := map[string]bool{}
	for _, p := range plans {
		if have[p.Op] == nil {
			have[p.Op] = map[string]bool{}
		}
		have[p.Op][p.Kind] = true
		if p.Rule != nil {
			rules[p.ID] = true
		}
	}
	for _, d := range ops.OracleOps() {
		if !have[d.Name][kindPositive] && !have[d.Name][kindDirty] {
			t.Errorf("%s: no positive case", d.Name)
		}
		if !have[d.Name][kindNegative] && !have[d.Name][kindRule] && !have[d.Name][kindSchema] {
			t.Errorf("%s: no negative case", d.Name)
		}
		for i, r := range d.Rules {
			if r.Example != "" && !rules[d.Name+"/rule/"+strconv.Itoa(i)] {
				t.Errorf("%s rule %d: no case", d.Name, i)
			}
		}
	}
}

// TestWriteFixtures keeps the committed fixtures equal to their builder.
func TestWriteFixtures(t *testing.T) {
	c := testConfig(t)
	for name, want := range writeFixtures() {
		got, err := os.ReadFile(filepath.Join(c.root, filepath.FromSlash(writeFixtureDir), name))
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(got, want) {
			t.Errorf("%s differs from its builder: run go run ./tools/oracle write fixtures", name)
		}
	}
}

func TestUpCode(t *testing.T) {
	for _, c := range []struct{ code, msg, want string }{
		{"operation_rejected", `blocked in paragraph 1: select matched nothing (current visible text: "x")`, "not_found"},
		{"operation_rejected", "blocked in paragraph 1: occurrence 99 is out of range: select matched 1 times", "not_found"},
		{"source_mismatch", "", "stale_base"},
		{"integrity_regression", "", "invalid_output"},
		{"invalid_plan", "", "invalid_plan"},
	} {
		if got := upCode(c.code, c.msg); got != c.want {
			t.Errorf("upCode(%s, %q) = %s, want %s", c.code, c.msg, got, c.want)
		}
	}
	if got := upPath("ops[1].comment_id", []string{"x", "comment.reply"}); got != "ops[1].at" {
		t.Errorf("upPath = %s", got)
	}
	if got := upPath("ops[0].action.count", []string{"row.insert"}); got != "ops[0].count" {
		t.Errorf("upPath = %s", got)
	}
}

// TestKnownWriteFiles keeps every known/write-*.tsv parseable.
func TestKnownWriteFiles(t *testing.T) {
	c := testConfig(t)
	paths, err := filepath.Glob(filepath.Join(c.baseline, "known", knownWriteGlob))
	if err != nil {
		t.Fatal(err)
	}
	if len(paths) == 0 {
		t.Fatal("no known/write-*.tsv files")
	}
	if _, err := loadKnownWrite(paths...); err != nil {
		t.Fatal(err)
	}
}

func TestKnownMatching(t *testing.T) {
	known := []knownWrite{{"edit_table/*/*/insert-*", "*", "proj:*", "copies"}, {"x/positive/s/*", "track", "part:*", "parts"}}
	if _, ok := explained(known, "edit_table/positive/write:mixed/insert-a-column", "direct", []string{"proj:doc"}); !ok {
		t.Error("glob id with proj prefix not matched")
	}
	if _, ok := explained(known, "x/positive/s/a", "track", []string{"part:+word/a.xml"}); !ok {
		t.Error("part prefix not matched")
	}
	if _, ok := explained(known, "edit_table/positive/write:mixed/delete-a-row", "direct", []string{"proj:doc"}); ok {
		t.Error("unlisted case matched")
	}
	got := relabelNewIDs(`{"at":"10000001","x":"<p id=\"ABCDEF01\">","y":"ABCDEF01","z":"12345678"}`, map[string]bool{"10000001": true})
	if want := `{"at":"10000001","x":"<p id=\"n1\">","y":"n1","z":"n2"}`; got != want {
		t.Errorf("relabelNewIDs = %s", got)
	}
}
