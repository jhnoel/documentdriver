package main

import (
	"encoding/json"
	"fmt"
	"io"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"text/tabwriter"

	"github.com/jhnoel/documentdriver/eval/grade"
)

// trialRec is one trial read back from a run directory.
type trialRec struct {
	T     TrialJSON
	G     *grade.Result // nil when not graded
	Valid bool          // status ok and graded
}

// loadRun reads every <run>/<task>/<condition>/<n>/trial.json.
func loadRun(dir string) ([]*trialRec, error) {
	m, err := filepath.Glob(filepath.Join(dir, "*", "*", "*", "trial.json"))
	if err != nil {
		return nil, err
	}
	var out []*trialRec
	for _, p := range m {
		b, err := os.ReadFile(p)
		if err != nil {
			return nil, err
		}
		r := &trialRec{}
		if err := json.Unmarshal(b, &r.T); err != nil {
			return nil, fmt.Errorf("%s: %w", p, err)
		}
		if gb, err := os.ReadFile(filepath.Join(filepath.Dir(p), "grade.json")); err == nil {
			var g grade.Result
			if json.Unmarshal(gb, &g) == nil {
				r.G = &g
			}
		}
		r.Valid = r.T.Status == statusOK && r.G != nil
		out = append(out, r)
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("%s: no trials (expected <task>/<condition>/<n>/trial.json)", dir)
	}
	sort.Slice(out, func(i, j int) bool {
		a, b := out[i].T, out[j].T
		if a.Task != b.Task {
			return a.Task < b.Task
		}
		if a.Condition != b.Condition {
			return a.Condition < b.Condition
		}
		return a.Repeat < b.Repeat
	})
	return out, nil
}

// agg is the aggregate of a set of trials.
type agg struct {
	Trials, HarnessErrors, Valid int
	Pass, WrongObject            int
	Tokens                       Tokens // sums over valid trials
	Cost, Wall                   float64
	Calls                        int
	Errors                       int
}

// add folds one trial record into the aggregate totals.
func (a *agg) add(r *trialRec) {
	a.Trials++
	if !r.Valid {
		a.HarnessErrors++
		return
	}
	a.Valid++
	if r.G.Pass {
		a.Pass++
	}
	if r.G.WrongObject {
		a.WrongObject++
	}
	a.Tokens.Input += r.T.Tokens.Input + r.T.Tokens.CacheWrite
	a.Tokens.CachedInput += r.T.Tokens.CachedInput
	a.Tokens.Output += r.T.Tokens.Output
	a.Cost += r.T.CostUSD
	a.Wall += r.T.WallSeconds
	a.Calls += r.T.ToolCalls.Count
	a.Errors += r.T.ToolCalls.Errors
}

// mean returns x averaged over the valid trials, or zero when none.
func (a *agg) mean(x float64) float64 {
	if a.Valid == 0 {
		return 0
	}
	return x / float64(a.Valid)
}

// rate returns a count as a percentage of valid trials, or "-".
func (a *agg) rate(n int) string {
	if a.Valid == 0 {
		return "-"
	}
	return fmt.Sprintf("%.0f%%", 100*float64(n)/float64(a.Valid))
}

const reportHeader = "group\ttrials\tharness err\tvalid\tsuccess\twrong-obj\ttok in\ttok cached\ttok out\tcost $\ttool calls\twall s\n"

// row returns one aggregate labelled as a tab-separated report row.
func (a *agg) row(label string) string {
	return fmt.Sprintf("%s\t%d\t%d\t%d\t%s\t%s\t%.0f\t%.0f\t%.0f\t%.4f\t%.1f\t%.0f\n", label, a.Trials, a.HarnessErrors, a.Valid,
		fmt.Sprintf("%d/%d %s", a.Pass, a.Valid, a.rate(a.Pass)), a.rate(a.WrongObject),
		a.mean(float64(a.Tokens.Input)), a.mean(float64(a.Tokens.CachedInput)), a.mean(float64(a.Tokens.Output)),
		a.mean(a.Cost), a.mean(float64(a.Calls)), a.mean(a.Wall))
}

// writeReport prints per-task and per-condition aggregates. Token, cost,
// call and time columns are means per valid trial; harness errors are
// counted apart and excluded from every rate.
func writeReport(w io.Writer, dir string) error {
	recs, err := loadRun(dir)
	if err != nil {
		return err
	}
	byCond := map[string]*agg{}
	byTaskCond := map[[2]string]*agg{}
	var conds []string
	var keys [][2]string
	for _, r := range recs {
		c := r.T.Condition
		if byCond[c] == nil {
			byCond[c] = &agg{}
			conds = append(conds, c)
		}
		byCond[c].add(r)
		k := [2]string{r.T.Task, c}
		if byTaskCond[k] == nil {
			byTaskCond[k] = &agg{}
			keys = append(keys, k)
		}
		byTaskCond[k].add(r)
	}
	sort.Strings(conds)
	fmt.Fprintf(w, "run %s\nmeans are per valid trial (status ok and graded); harness errors are excluded from rates.\n", dir)
	fmt.Fprintln(w, "tok in = uncached input + cache writes.")
	fmt.Fprintln(w, "\nper condition")
	tw := tabwriter.NewWriter(w, 2, 4, 2, ' ', 0)
	fmt.Fprint(tw, reportHeader)
	for _, c := range conds {
		fmt.Fprint(tw, byCond[c].row(c))
	}
	tw.Flush()
	fmt.Fprintln(w, "\nper task and condition")
	tw = tabwriter.NewWriter(w, 2, 4, 2, ' ', 0)
	fmt.Fprint(tw, reportHeader)
	for _, k := range keys {
		fmt.Fprint(tw, byTaskCond[k].row(k[0]+" ["+k[1]+"]"))
	}
	tw.Flush()
	var errs []string
	for _, r := range recs {
		if r.T.Status != statusOK || !r.Valid {
			errs = append(errs, fmt.Sprintf("  %s [%s] #%d: %s %s", r.T.Task, r.T.Condition, r.T.Repeat, r.T.Status, r.T.Error))
		}
	}
	if len(errs) > 0 {
		fmt.Fprintln(w, "\nharness errors (not model failures)")
		fmt.Fprintln(w, strings.Join(errs, "\n"))
	}
	return nil
}

// cmdReport prints the aggregate report for a run directory.
func cmdReport(args []string) error {
	if len(args) != 1 {
		fmt.Fprintln(os.Stderr, "usage: pptxbench report <run dir>")
		return exitErr(2)
	}
	return writeReport(os.Stdout, args[0])
}

// writeCompare pairs two runs by task and prints per-task deltas (B minus A)
// and totals over the tasks both runs have valid trials for.
func writeCompare(w io.Writer, dirA, dirB string) error {
	load := func(dir string) (map[string]*agg, map[string]bool, error) {
		recs, err := loadRun(dir)
		if err != nil {
			return nil, nil, err
		}
		m := map[string]*agg{}
		conds := map[string]bool{}
		for _, r := range recs {
			if m[r.T.Task] == nil {
				m[r.T.Task] = &agg{}
			}
			m[r.T.Task].add(r)
			conds[r.T.Condition] = true
		}
		return m, conds, nil
	}
	a, ca, err := load(dirA)
	if err != nil {
		return err
	}
	b, cb, err := load(dirB)
	if err != nil {
		return err
	}
	names := func(m map[string]bool) string {
		return strings.Join(slices.Sorted(maps.Keys(m)), ",")
	}
	fmt.Fprintf(w, "A %s [%s]\nB %s [%s]\npaired by task; delta = B - A; means per valid trial\n\n", dirA, names(ca), dirB, names(cb))
	var tasks []string
	for t := range a {
		if b[t] != nil {
			tasks = append(tasks, t)
		}
	}
	sort.Strings(tasks)
	tw := tabwriter.NewWriter(w, 2, 4, 2, ' ', 0)
	fmt.Fprintln(tw, "task\tA success\tB success\tdelta\tA cost $\tB cost $\tdelta\tA calls\tB calls\tdelta\tA wall s\tB wall s\tdelta\tA tok out\tB tok out\tdelta")
	var sa, sb agg
	better, worse, same := 0, 0, 0
	for _, t := range tasks {
		x, y := a[t], b[t]
		if x.Valid == 0 || y.Valid == 0 {
			fmt.Fprintf(tw, "%s\t%s\t%s\t(no valid trials on one side)\n", t, x.rate(x.Pass), y.rate(y.Pass))
			continue
		}
		px, py := float64(x.Pass)/float64(x.Valid), float64(y.Pass)/float64(y.Valid)
		switch {
		case py > px:
			better++
		case py < px:
			worse++
		default:
			same++
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\t%+.0f%%\t%.4f\t%.4f\t%+.4f\t%.1f\t%.1f\t%+.1f\t%.0f\t%.0f\t%+.0f\t%.0f\t%.0f\t%+.0f\n", t,
			x.rate(x.Pass), y.rate(y.Pass), 100*(py-px),
			x.mean(x.Cost), y.mean(y.Cost), y.mean(y.Cost)-x.mean(x.Cost),
			x.mean(float64(x.Calls)), y.mean(float64(y.Calls)), y.mean(float64(y.Calls))-x.mean(float64(x.Calls)),
			x.mean(x.Wall), y.mean(y.Wall), y.mean(y.Wall)-x.mean(x.Wall),
			x.mean(float64(x.Tokens.Output)), y.mean(float64(y.Tokens.Output)), y.mean(float64(y.Tokens.Output))-x.mean(float64(x.Tokens.Output)))
		for _, p := range []struct {
			s *agg
			m *agg
		}{{&sa, x}, {&sb, y}} {
			p.s.Trials += p.m.Trials
			p.s.HarnessErrors += p.m.HarnessErrors
			p.s.Valid += p.m.Valid
			p.s.Pass += p.m.Pass
			p.s.WrongObject += p.m.WrongObject
			p.s.Cost += p.m.Cost
			p.s.Wall += p.m.Wall
			p.s.Calls += p.m.Calls
			p.s.Tokens.Output += p.m.Tokens.Output
		}
	}
	tw.Flush()
	fmt.Fprintf(w, "\ntasks paired: %d (B better %d, worse %d, same %d)\n", len(tasks), better, worse, same)
	fmt.Fprintf(w, "overall on paired tasks: A success %s, B success %s; A wrong-object %s, B wrong-object %s\n",
		sa.rate(sa.Pass), sb.rate(sb.Pass), sa.rate(sa.WrongObject), sb.rate(sb.WrongObject))
	fmt.Fprintf(w, "mean cost/trial A $%.4f B $%.4f; mean calls A %.1f B %.1f; mean wall A %.0fs B %.0fs; harness errors A %d B %d\n",
		sa.mean(sa.Cost), sb.mean(sb.Cost), sa.mean(float64(sa.Calls)), sb.mean(float64(sb.Calls)),
		sa.mean(sa.Wall), sb.mean(sb.Wall), sa.HarnessErrors, sb.HarnessErrors)
	return nil
}

// cmdCompare prints paired per-task deltas for two run directories.
func cmdCompare(args []string) error {
	if len(args) != 2 {
		fmt.Fprintln(os.Stderr, "usage: pptxbench compare <run A> <run B>")
		return exitErr(2)
	}
	return writeCompare(os.Stdout, args[0], args[1])
}
