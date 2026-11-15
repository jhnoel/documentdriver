package ops

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// The differential test runs every case through the Go parser and
// compares it with docxdriver 0.5.2's parse_plan_json, whose results are
// recorded in testdata/oracle/results.jsonl. Regenerate them with
//
//	DOCX_PLAN_ORACLE=<planoracle binary> go test ./internal/docx/ops -run TestOracleRegenerate
//
// where the binary is built from testdata/oracle/planoracle (see its
// README). A case whose text changed, or a new case, fails until the
// results are regenerated.

const (
	oracleDir   = "testdata/oracle"
	resultsFile = "testdata/oracle/results.jsonl"
	zeroBase    = "sha256:0000000000000000000000000000000000000000000000000000000000000000"
)

type oracleCase struct {
	Name string `json:"name"`
	Text string `json:"text"`
}

// outcome is a parse result in the oracle harness's shape.
type outcome struct {
	OK         bool        `json:"ok"`
	Canonical  string      `json:"canonical,omitempty"`
	PreviewKey string      `json:"preview_key,omitempty"`
	Diagnostic *Diagnostic `json:"diagnostic,omitempty"`
}

type oracleResult struct {
	Name string  `json:"name"`
	Text string  `json:"text"`
	Rust outcome `json:"rust"`
}

func wrapOps(ops string) string {
	return `{"base":"` + zeroBase + `","author":"Reviewer","change_mode":"track","ops":` + ops + `}`
}

// readLines returns a file's lines, without blank and # comment lines.
func readLines(t *testing.T, name string) []string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(oracleDir, name))
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for line := range strings.SplitSeq(string(b), "\n") {
		if strings.TrimSpace(line) == "" || strings.HasPrefix(line, "#") {
			continue
		}
		out = append(out, line)
	}
	return out
}

// oracleCases is every differential case, in a stable order.
func oracleCases(t *testing.T) []oracleCase {
	t.Helper()
	var cases []oracleCase
	for _, d := range OracleOps() {
		for i, r := range d.Rules {
			if r.Example == "" {
				continue
			}
			cases = append(cases, oracleCase{Name: fmt.Sprintf("rule/%s/%d", d.Name, i), Text: wrapOps("[" + r.Example + "]")})
		}
	}
	for i, line := range readLines(t, "serde-cases.txt") {
		name := fmt.Sprintf("serde/%d", i)
		switch {
		case strings.HasPrefix(line, "TOP "):
			plan := map[string]any{"base": zeroBase, "author": "Reviewer", "change_mode": "track",
				"ops": []any{map[string]any{"op": "delete_paragraphs", "at": []any{"0000000A"}}}}
			var top map[string]any
			if err := json.Unmarshal([]byte(line[4:]), &top); err != nil {
				t.Fatalf("%s: %v", line, err)
			}
			maps.Copy(plan, top)
			cases = append(cases, oracleCase{Name: name, Text: marshalPlain(t, plan)})
		case strings.HasPrefix(line, "RAWJ "):
			var s string
			if err := json.Unmarshal([]byte(line[5:]), &s); err != nil {
				t.Fatalf("%s: %v", line, err)
			}
			cases = append(cases, oracleCase{Name: name, Text: s})
		case strings.HasPrefix(line, "RAW "):
			cases = append(cases, oracleCase{Name: name, Text: line[4:]})
		default:
			cases = append(cases, oracleCase{Name: name, Text: wrapOps(line)})
		}
	}
	for _, depth := range []int{127, 128} {
		cases = append(cases, oracleCase{Name: "serde/depth" + strconv.Itoa(depth),
			Text: strings.Repeat("[", depth) + strings.Repeat("]", depth)})
	}
	for i, line := range readLines(t, "valid-ops.txt") {
		cases = append(cases, oracleCase{Name: fmt.Sprintf("valid/%d", i), Text: wrapOps("[" + line + "]")})
		if i%3 == 0 {
			cases = append(cases, oracleCase{Name: fmt.Sprintf("valid/%d/direct", i),
				Text: strings.Replace(wrapOps("["+line+"]"), `"track"`, `"direct"`, 1)})
		}
	}
	seen := map[string]int{}
	for _, line := range readLines(t, "source-plans.jsonl") {
		var c oracleCase
		if err := json.Unmarshal([]byte(line), &c); err != nil {
			t.Fatal(err)
		}
		seen[c.Name]++
		if n := seen[c.Name]; n > 1 {
			c.Name += "#" + strconv.Itoa(n)
		}
		cases = append(cases, c)
	}
	return cases
}

// marshalPlain encodes v without HTML escaping.
func marshalPlain(t *testing.T, v any) string {
	t.Helper()
	var b bytes.Buffer
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		t.Fatal(err)
	}
	return strings.TrimSuffix(b.String(), "\n")
}

// goOutcome is the Go parser's outcome for a case. The cases are in
// docxdriver's grammar (their recorded oracle results are keyed by their
// text), so the plan is translated to the contract's first; text that is
// not JSON goes to the parser as it is.
func goOutcome(text string) outcome {
	plan := []byte(text)
	if t, err := FromOracle(plan); err == nil {
		plan = t
	}
	_, d := ParsePlanJSON(plan)
	if d != nil {
		return outcome{Diagnostic: d}
	}
	return outcome{OK: true}
}

func TestOracleRegenerate(t *testing.T) {
	bin := os.Getenv("DOCX_PLAN_ORACLE")
	if bin == "" {
		t.Skip("set DOCX_PLAN_ORACLE to the planoracle binary to regenerate the oracle results")
	}
	cases := oracleCases(t)
	var in bytes.Buffer
	for _, c := range cases {
		in.WriteString(marshalPlain(t, map[string]string{"text": c.Text}) + "\n")
	}
	cmd := exec.Command(bin) //nolint:gosec // G702: the oracle binary is the one the developer names in DOCX_PLAN_ORACLE
	cmd.Stdin = &in
	out, err := cmd.Output()
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSuffix(string(out), "\n"), "\n")
	if len(lines) != len(cases) {
		t.Fatalf("oracle answered %d of %d cases", len(lines), len(cases))
	}
	var b bytes.Buffer
	for i, c := range cases {
		var o outcome
		if err := json.Unmarshal([]byte(lines[i]), &o); err != nil {
			t.Fatal(err)
		}
		b.WriteString(marshalPlain(t, oracleResult{Name: c.Name, Text: c.Text, Rust: o}) + "\n")
	}
	if err := os.WriteFile(resultsFile, b.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
}

func loadResults(t *testing.T) map[string]oracleResult {
	t.Helper()
	f, err := os.Open(resultsFile)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	out := map[string]oracleResult{}
	sc := bufio.NewScanner(f)
	sc.Buffer(nil, 1<<24)
	for sc.Scan() {
		var r oracleResult
		if err := json.Unmarshal(sc.Bytes(), &r); err != nil {
			t.Fatal(err)
		}
		out[r.Name] = r
	}
	if err := sc.Err(); err != nil {
		t.Fatal(err)
	}
	return out
}

// summary is an outcome as the differences list records it: "ok", or
// the diagnostic's code.
func summary(o outcome) string {
	if o.OK {
		return "ok"
	}
	return o.Diagnostic.Code
}

// TestOracle is the parity the contract keeps with docxdriver's plan
// parsing (docs/plan.md, decision 2): for every recorded case, the same
// plan translated into the contract's grammar is accepted when
// docxdriver accepts it and refused with the same code when docxdriver
// refuses it. Messages and paths are the contract's own. Every case that
// differs is listed in Differences with its class.
func TestOracle(t *testing.T) {
	results := loadResults(t)
	diffs := map[string]Difference{}
	for _, d := range Differences {
		diffs[d.Case] = d
	}
	classes := map[string]bool{}
	for _, c := range DifferenceClasses {
		classes[c.Name] = true
	}
	for _, d := range Differences {
		if !classes[d.Class] {
			t.Errorf("difference for %s has unknown class %q", d.Case, d.Class)
		}
	}
	used := map[string]bool{}
	counts := map[string]int{}
	for _, c := range oracleCases(t) {
		r, ok := results[c.Name]
		if !ok || r.Text != c.Text {
			t.Errorf("%s: no oracle result for the current text; regenerate (see TestOracleRegenerate)", c.Name)
			continue
		}
		got := goOutcome(c.Text)
		kind := "valid"
		if !r.Rust.OK {
			kind = r.Rust.Diagnostic.Code
		}
		counts[kind]++
		if d, ok := diffs[c.Name]; ok {
			used[c.Name] = true
			if rs, gs := summary(r.Rust), summary(got); rs != d.Rust || gs != d.Go {
				t.Errorf("%s: listed difference %s: oracle %s, Go %s; listed %s, %s", c.Name, d.Class, rs, gs, d.Rust, d.Go)
			}
			counts["listed"]++
			continue
		}
		if rs, gs := summary(r.Rust), summary(got); rs != gs {
			msg := ""
			if got.Diagnostic != nil {
				msg = got.Diagnostic.Path + ": " + got.Diagnostic.Message
			}
			t.Errorf("%s: oracle %s, Go %s %s\n  text: %s", c.Name, rs, gs, msg, c.Text)
		}
	}
	for _, d := range Differences {
		if d.Case != "" && !used[d.Case] {
			t.Errorf("difference %s names case %s, which no longer exists", d.Class, d.Case)
		}
	}
	t.Logf("cases by oracle outcome: %v", counts)
}
