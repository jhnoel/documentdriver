package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"github.com/jhnoel/documentdriver/internal/docx/ops"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
)

const writeUsage = `usage: oracle write <command> [flags]

  fixtures  rebuild the authored write fixtures (tools/oracle/testdata/write)
  sites     scan docxdriver's commands/ for refusal sites into sites.tsv
  gen       pick the sources and write the plans (positive, negative, rule, schema)
  record    run every plan through docxdriver in track and direct mode
  compare   run the plans through the Go docx CLI and compare with the record
  show      run the selected cases through docxdriver and keep every output text
  laws      check the change-mode law on the oracle's own record
  ported    list the ported operations (contract names), one a line
`

// writeCmd is `oracle write`.
func writeCmd(args []string) error {
	if len(args) == 0 {
		fmt.Fprint(os.Stderr, writeUsage)
		return errors.New("missing write command")
	}
	switch args[0] {
	case "ported":
		for _, name := range portedOps() {
			fmt.Println(name)
		}
		return nil
	case "fixtures":
		root, err := rootDir()
		if err != nil {
			return err
		}
		return saveWriteFixtures(root)
	case "sites":
		fs := flag.NewFlagSet("write sites", flag.ExitOnError)
		var c config
		c.flags(fs)
		fs.Parse(args[1:])
		if err := c.resolve(); err != nil {
			return err
		}
		sites, err := scanSites(c.docxdriver)
		if err != nil {
			return err
		}
		fmt.Printf("write sites: %d refusal sites\n", len(sites))
		return saveSites(c.sitesPath(), sites)
	case "gen":
		return writeGenCmd(args[1:])
	case "record":
		return writeRecordCmd(args[1:])
	case "compare":
		return writeCompareCmd(args[1:])
	case "show":
		return writeShowCmd(args[1:])
	case "laws":
		return writeLawsCmd(args[1:])
	}
	fmt.Fprint(os.Stderr, writeUsage)
	return fmt.Errorf("unknown write command %q", args[0])
}

// oracleFlag registers the -oracle flag on fs and returns its value.
func oracleFlag(fs *flag.FlagSet) *string {
	return fs.String("oracle", os.Getenv("DOCX_ORACLE"), "docxdriver CLI built at 1ffb0d7 (default $DOCX_ORACLE, else <root>/bin/oracle-target/release/docxdriver)")
}

// oracleBin returns the flag value or the default docxdriver binary path.
func (c *config) oracleBin(flagValue string) string {
	if flagValue != "" {
		return flagValue
	}
	return filepath.Join(c.root, "bin", "oracle-target", "release", "docxdriver")
}

// writeFilter is the plan selection record and compare share.
type writeFilter struct {
	ops, kinds, only, modes *string
}

// addWriteFilter registers the shared write-plan selection flags on fs.
func addWriteFilter(fs *flag.FlagSet) writeFilter {
	return writeFilter{
		ops:   fs.String("ops", "", "comma-separated operations (default all)"),
		kinds: fs.String("kinds", "", "comma-separated plan kinds: positive, negative, rule, schema (default all)"),
		only:  fs.String("plan", "", "only plans whose id or source contains this substring"),
		modes: fs.String("modes", "track,direct", "change modes"),
	}
}

type writeJob struct {
	p    *WritePlan
	mode string
}

// writeJobs loads the sources and the selected plans' jobs.
func (c *config) writeJobs(f writeFilter) ([]writeJob, map[string][]byte, error) {
	plans, err := c.loadPlans()
	if err != nil {
		return nil, nil, err
	}
	if err := checkPlanIDs(plans); err != nil {
		return nil, nil, err
	}
	plans = selectPlans(plans, *f.ops, *f.kinds, *f.only)
	srcs := map[string][]byte{}
	var jobs []writeJob
	for _, p := range plans {
		k := p.Set + "\t" + p.File
		if _, ok := srcs[k]; !ok {
			b, err := os.ReadFile(c.sourcePath(p.Set, p.File))
			if err != nil {
				return nil, nil, fmt.Errorf("plan %s: %w", p.ID, err)
			}
			srcs[k] = b
		}
		for _, m := range strings.Split(*f.modes, ",") {
			jobs = append(jobs, writeJob{p, strings.TrimSpace(m)})
		}
	}
	return jobs, srcs, nil
}

// runWriteJobs runs the jobs with n workers, each in its own work dir.
func runWriteJobs(jobs []writeJob, n int, workRoot string, run func(j writeJob, work string) error) error {
	ch := make(chan int)
	var wg sync.WaitGroup
	var mu sync.Mutex
	var first error
	for w := range n {
		wg.Go(func() {
			for i := range ch {
				if err := run(jobs[i], filepath.Join(workRoot, "w"+strconv.Itoa(w))); err != nil {
					mu.Lock()
					if first == nil {
						first = err
					}
					mu.Unlock()
				}
			}
		})
	}
	for i := range jobs {
		ch <- i
	}
	close(ch)
	wg.Wait()
	return first
}

// writeRecordCmd is `oracle write record`.
func writeRecordCmd(args []string) error {
	fs := flag.NewFlagSet("write record", flag.ExitOnError)
	var c config
	c.flags(fs)
	oracle := oracleFlag(fs)
	filter := addWriteFilter(fs)
	fs.Parse(args)
	if err := c.resolve(); err != nil {
		return err
	}
	bin := c.oracleBin(*oracle)
	jobs, srcs, err := c.writeJobs(filter)
	if err != nil {
		return err
	}
	sites, err := loadSites(c.sitesPath())
	if err != nil {
		return err
	}
	var mu sync.Mutex
	got := map[string]*writeResult{}
	var unmatched []string
	work := filepath.Join(c.out, "write-work")
	err = runWriteJobs(jobs, c.jobs, work, func(j writeJob, dir string) error {
		r, err := runWrite(oracleApplier{bin}, bin, srcs[j.p.Set+"\t"+j.p.File], j.p, j.mode, dir, nil)
		if err != nil {
			return fmt.Errorf("%s %s: %w", j.p.ID, j.mode, err)
		}
		if r.Outcome == "refused" && r.message != "" && !slices.Contains([]string{"invalid_json", "invalid_plan", "invalid_operation"}, r.Code) {
			r.Sites = matchSites(sites, r.message)
			if len(r.Sites) == 0 {
				r.Sites = []string{"unmatched"}
				mu.Lock()
				unmatched = append(unmatched, r.ID+"\t"+r.Mode+"\t"+r.Code+"\t"+strings.ReplaceAll(r.message, "\n", " "))
				mu.Unlock()
			}
		}
		mu.Lock()
		got[resultKey(r.ID, r.Mode)] = r
		mu.Unlock()
		return nil
	})
	os.RemoveAll(work)
	if err != nil {
		return err
	}
	all, err := c.loadPlans()
	if err != nil {
		return err
	}
	if err := c.saveResults(all, got); err != nil {
		return err
	}
	if err := c.saveCoverage(sites, all); err != nil {
		return err
	}
	// The refusals no site matched, with their messages, for triage only
	// (messages are not part of the record).
	slices.Sort(unmatched)
	if err := os.MkdirAll(c.out, 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(c.out, "write-unmatched.tsv"), []byte(strings.Join(unmatched, "\n")+"\n"), 0o644); err != nil {
		return err
	}
	fmt.Printf("write record: %d runs\n", len(got))
	return nil
}

// resultKey builds the map key for a plan id and mode.
func resultKey(id, mode string) string { return id + "\t" + mode }

// resultsPath returns the results file path for operation op.
func (c *config) resultsPath(op string) string {
	return filepath.Join(c.writeDir(), "results", op+".jsonl")
}

// loadResults reads the recorded results of every op.
func (c *config) loadResults() (map[string]*writeResult, error) {
	out := map[string]*writeResult{}
	for _, op := range opNames() {
		b, err := os.ReadFile(c.resultsPath(op))
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if err != nil {
			return nil, err
		}
		sc := bufio.NewScanner(bytes.NewReader(b))
		sc.Buffer(nil, 1<<26)
		for sc.Scan() {
			var r writeResult
			if err := json.Unmarshal(sc.Bytes(), &r); err != nil {
				return nil, fmt.Errorf("%s: %w", c.resultsPath(op), err)
			}
			out[resultKey(r.ID, r.Mode)] = &r
		}
		if err := sc.Err(); err != nil {
			return nil, err
		}
	}
	return out, nil
}

// saveResults merges the new results into the recorded ones, drops
// results of plans that no longer exist, and writes per op the results
// (sorted by plan order and mode). Output texts are not kept: compare
// re-runs the oracle on a differing case for triage.
func (c *config) saveResults(plans []*WritePlan, got map[string]*writeResult) error {
	old, err := c.loadResults()
	if err != nil {
		return err
	}
	maps.Copy(old, got)
	byOp := map[string][]*WritePlan{}
	for _, p := range plans {
		byOp[p.Op] = append(byOp[p.Op], p)
	}
	if err := os.MkdirAll(filepath.Join(c.writeDir(), "results"), 0o755); err != nil {
		return err
	}
	for _, op := range opNames() {
		var lines bytes.Buffer
		for _, p := range byOp[op] {
			for _, m := range writeModes {
				r, ok := old[resultKey(p.ID, m)]
				if !ok {
					continue
				}
				line, err := marshalLine(r)
				if err != nil {
					return err
				}
				lines.Write(line)
			}
		}
		if err := os.WriteFile(c.resultsPath(op), lines.Bytes(), 0o644); err != nil {
			return err
		}
	}
	return nil
}

// saveCoverage writes, per refusal site, the recorded runs tagged with
// it, and how many document refusals matched no site.
func (c *config) saveCoverage(sites []site, plans []*WritePlan) error {
	res, err := c.loadResults()
	if err != nil {
		return err
	}
	hits := map[string][]string{}
	for _, p := range plans {
		for _, m := range writeModes {
			r, ok := res[resultKey(p.ID, m)]
			if !ok {
				continue
			}
			for _, s := range r.Sites {
				hits[s] = append(hits[s], p.ID+" "+m)
			}
		}
	}
	var b strings.Builder
	n := 0
	b.WriteString("# refusal site\truns tagged with it\tfirst such run (plan id and mode); 0 and \"-\" when no recorded plan reaches the site\n")
	for _, s := range sites {
		h := hits[s.ID]
		if len(h) > 0 {
			n++
			fmt.Fprintf(&b, "%s\t%d\t%s\n", s.ID, len(h), h[0])
		} else {
			fmt.Fprintf(&b, "%s\t0\t-\n", s.ID)
		}
	}
	if h := hits["unmatched"]; len(h) > 0 {
		fmt.Fprintf(&b, "unmatched\t%d\t%s\n", len(h), h[0])
	}
	var st strings.Builder
	st.WriteString("# refusal site\tstatus\tenclosing function\treason or first recorded run (statuses: recorded, open, shadowed, unreachable, dead, not-a-refusal)\n")
	counts := map[string]int{}
	for _, s := range sites {
		status, reason := "recorded", ""
		if h := hits[s.ID]; len(h) > 0 {
			reason = h[0]
		} else {
			status, reason = siteStatus(s)
		}
		counts[status]++
		fmt.Fprintf(&st, "%s\t%s\t%s\t%s\n", s.ID, status, s.Fn, reason)
	}
	if err := os.WriteFile(filepath.Join(c.writeDir(), "sites-status.tsv"), []byte(st.String()), 0o644); err != nil {
		return err
	}
	fmt.Printf("write record: sites %v\n", counts)
	fmt.Printf("write record: %d of %d refusal sites hit, %d runs with an unmatched rejection\n", n, len(sites), len(hits["unmatched"]))
	return os.WriteFile(filepath.Join(c.writeDir(), "coverage.tsv"), []byte(b.String()), 0o644)
}

// writeShowCmd is `oracle write show`: it runs the selected cases
// through docxdriver and writes every output text (indented) under
// <out>/write-show/<case>/<mode>/, for triage and for checking the
// oracle's own laws.
func writeShowCmd(args []string) error {
	fs := flag.NewFlagSet("write show", flag.ExitOnError)
	var c config
	c.flags(fs)
	oracle := oracleFlag(fs)
	filter := addWriteFilter(fs)
	fs.Parse(args)
	if err := c.resolve(); err != nil {
		return err
	}
	bin := c.oracleBin(*oracle)
	jobs, srcs, err := c.writeJobs(filter)
	if err != nil {
		return err
	}
	root := filepath.Join(c.out, "write-show")
	return runWriteJobs(jobs, c.jobs, filepath.Join(c.out, "write-work-show"), func(j writeJob, dir string) error {
		bl := &blobs{m: map[string]string{}}
		r, err := runWrite(oracleApplier{bin}, bin, srcs[j.p.Set+"\t"+j.p.File], j.p, j.mode, dir, bl)
		if err != nil {
			return err
		}
		d := filepath.Join(root, strings.ReplaceAll(j.p.ID, "/", "~"), j.mode)
		if err := os.MkdirAll(d, 0o755); err != nil {
			return err
		}
		files := map[string]string{}
		maps.Copy(files, r.Triage)
		for k, v := range r.Proj {
			_, dg, _ := strings.Cut(v, " ")
			files[k] = dg
		}
		for k, dg := range files {
			if err := os.WriteFile(filepath.Join(d, k+".txt"), indentJSON(bl.m[dg]), 0o644); err != nil {
				return err
			}
		}
		return nil
	})
}

// writeLawsCmd is `oracle write laws`: the change-mode law on the
// oracle's own record. For every AcceptAuthored case both modes accept,
// the direct output and the tracked output must project the same after
// accept-all (new paragraph ids are relabelled, relabelNewIDs). It lists
// the cases that break it, and which of them write-oracle-bugs.tsv
// explains.
func writeLawsCmd(args []string) error {
	fs := flag.NewFlagSet("write laws", flag.ExitOnError)
	var c config
	c.flags(fs)
	fs.Parse(args)
	if err := c.resolve(); err != nil {
		return err
	}
	plans, err := c.loadPlans()
	if err != nil {
		return err
	}
	res, err := c.loadResults()
	if err != nil {
		return err
	}
	known, err := loadKnownWrite(filepath.Join(c.baseline, "known", "write-oracle-bugs.tsv"))
	if err != nil {
		return err
	}
	checked, broken, explainedN := 0, 0, 0
	for _, p := range plans {
		t, d := res[resultKey(p.ID, "track")], res[resultKey(p.ID, "direct")]
		if t == nil || d == nil || t.Class != classAcceptAuthored || t.Outcome != "accepted" || d.Outcome != "accepted" {
			continue
		}
		checked++
		var diffs []string
		for _, k := range []string{"accept-doc", "accept-comments", "accept-bookmarks"} {
			if t.Proj[k] != d.Proj[k] {
				diffs = append(diffs, "proj:"+k)
			}
		}
		if len(diffs) == 0 {
			continue
		}
		broken++
		if reason, ok := explained(known, p.ID, "direct", diffs); ok {
			explainedN++
			fmt.Printf("explained\t%s\t%s\n", p.ID, reason)
			continue
		}
		fmt.Printf("open\t%s\t%s\n", p.ID, strings.Join(diffs, ","))
	}
	fmt.Printf("write laws: %d AcceptAuthored cases accepted in both modes, %d break accept(direct) = accept(track), %d explained by write-oracle-bugs.tsv, %d open\n",
		checked, broken, explainedN, broken-explainedN)
	return nil
}

// portedOps are the contract names of the operations that run: those
// whose Spec no longer embeds pending, in op table order.
func portedOps() []string {
	var out []string
	for _, d := range ops.Ops {
		if _, pending := d.New().(interface{ PendingOp() }); !pending {
			out = append(out, d.Name)
		}
	}
	return out
}
