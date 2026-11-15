package main

import (
	"encoding/json"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/jhnoel/documentdriver/internal/docx/apply"
	"github.com/jhnoel/documentdriver/internal/docx/revise"
)

// collectDates gathers every "date" value in v, as parsed times.
func collectDates(t *testing.T, v any) []time.Time {
	t.Helper()
	var out []time.Time
	var walk func(v any)
	walk = func(v any) {
		switch x := v.(type) {
		case map[string]any:
			for k, val := range x {
				if k == "date" {
					s, _ := val.(string)
					parsed, err := time.Parse(revise.DateLayout, s)
					if err != nil {
						t.Fatalf("date %q: %v", s, err)
					}
					out = append(out, parsed)
					continue
				}
				walk(val)
			}
		case []any:
			for _, item := range x {
				walk(item)
			}
		}
	}
	walk(v)
	return out
}

// stripDates returns a copy of v with every "date" key removed.
func stripDates(v any) any {
	switch x := v.(type) {
	case map[string]any:
		out := make(map[string]any, len(x))
		for k, val := range x {
			if k == "date" {
				continue
			}
			out[k] = stripDates(val)
		}
		return out
	case []any:
		out := make([]any, len(x))
		for i, item := range x {
			out[i] = stripDates(item)
		}
		return out
	default:
		return v
	}
}

func parseChanged(t *testing.T, stdout string) map[string]any {
	t.Helper()
	var res map[string]any
	if err := json.Unmarshal([]byte(stdout), &res); err != nil {
		t.Fatalf("apply output is not JSON: %v\n%s", err, stdout)
	}
	return res
}

// TestDryRunStampsRealClock: a tracked text.replace (with a comment, the
// stamp the changed units carry) run with --dry-run reports date fields
// that are not the preview time and are within a few seconds of now.
func TestDryRunStampsRealClock(t *testing.T) {
	doc, clockPlan := clockDoc(t)
	before := time.Now().UTC()
	_, stdout := applyRaw(t, doc, clockPlan, "--dry-run")
	res := parseChanged(t, stdout)
	if res["dry_run"] != true {
		t.Fatalf("dry_run is not true: %v", res)
	}
	dates := collectDates(t, res["changed"])
	if len(dates) == 0 {
		t.Fatalf("no date fields in changed: %s", stdout)
	}
	for _, d := range dates {
		if d.Format(revise.DateLayout) == apply.PreviewTime {
			t.Errorf("a dry run stamped the preview time %s: %s", apply.PreviewTime, stdout)
		}
		if diff := d.Sub(before); diff < -10*time.Second || diff > 10*time.Second {
			t.Errorf("a dry run stamped %s, not within seconds of %s: %s", d.Format(revise.DateLayout), before.Format(revise.DateLayout), stdout)
		}
	}
}

// TestDryRunClockFlagWins: with --dry-run --clock the dates are exactly that.
func TestDryRunClockFlagWins(t *testing.T) {
	const at = "2030-01-02T03:04:05Z"
	doc, clockPlan := clockDoc(t)
	_, stdout := applyRaw(t, doc, clockPlan, "--dry-run", "--clock", at)
	res := parseChanged(t, stdout)
	dates := collectDates(t, res["changed"])
	if len(dates) == 0 {
		t.Fatalf("no date fields in changed: %s", stdout)
	}
	for _, d := range dates {
		if d.Format(revise.DateLayout) != at {
			t.Errorf("a dry run with --clock %s stamped %s: %s", at, d.Format(revise.DateLayout), stdout)
		}
	}
}

// TestDryRunMatchesCommitAsideFromDates: a dry run and a real apply of
// the same plan report the same changed after the date values are removed.
func TestDryRunMatchesCommitAsideFromDates(t *testing.T) {
	doc, clockPlan := clockDoc(t)
	_, dryOut := applyRaw(t, doc, clockPlan, "--dry-run")
	out := filepath.Join(t.TempDir(), "out.docx")
	_, commitOut := applyRaw(t, doc, clockPlan, "-o", out)
	dry := parseChanged(t, dryOut)
	commit := parseChanged(t, commitOut)
	if !reflect.DeepEqual(stripDates(dry["changed"]), stripDates(commit["changed"])) {
		t.Errorf("changed differs aside from dates:\ndry:    %v\ncommit: %v", stripDates(dry["changed"]), stripDates(commit["changed"]))
	}
}
