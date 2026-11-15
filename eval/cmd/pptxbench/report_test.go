package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jhnoel/documentdriver/eval/grade"
)

func writeTrial(t *testing.T, run, task, cond string, n int, tj TrialJSON, g *grade.Result) {
	t.Helper()
	d := filepath.Join(run, task, cond, string(rune('0'+n)))
	if err := os.MkdirAll(d, 0o755); err != nil {
		t.Fatal(err)
	}
	tj.Task, tj.Condition, tj.Repeat = task, cond, n
	b, _ := json.Marshal(tj)
	if err := os.WriteFile(filepath.Join(d, "trial.json"), b, 0o644); err != nil {
		t.Fatal(err)
	}
	if g != nil {
		gb, _ := json.Marshal(g)
		if err := os.WriteFile(filepath.Join(d, "grade.json"), gb, 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func TestReportAndCompare(t *testing.T) {
	a, b := t.TempDir(), t.TempDir()
	ok := TrialJSON{Status: statusOK, CostUSD: 0.01, WallSeconds: 10, ToolCalls: ToolCalls{Count: 2}, Tokens: Tokens{Input: 10, CachedInput: 100, Output: 20}}
	writeTrial(t, a, "t1", "json", 1, ok, &grade.Result{Pass: true})
	writeTrial(t, a, "t1", "json", 2, ok, &grade.Result{Pass: false, WrongObject: true})
	writeTrial(t, a, "t1", "json", 3, TrialJSON{Status: statusTimeout, Error: "killed"}, nil)
	writeTrial(t, b, "t1", "svg", 1, ok, &grade.Result{Pass: true})

	var out bytes.Buffer
	if err := writeReport(&out, a); err != nil {
		t.Fatal(err)
	}
	s := out.String()
	// 3 trials, 1 harness error, 2 valid, 1 pass of 2, 1 wrong object of 2.
	for _, want := range []string{"1/2 50%", "harness errors (not model failures)", "t1 [json] #3: timeout killed"} {
		if !strings.Contains(s, want) {
			t.Errorf("report lacks %q:\n%s", want, s)
		}
	}
	out.Reset()
	if err := writeCompare(&out, a, b); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "tasks paired: 1 (B better 1, worse 0, same 0)") {
		t.Errorf("compare:\n%s", out.String())
	}
}
