package main

import (
	"bytes"
	"errors"
	"flag"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"sync"

	docxread "github.com/jhnoel/documentdriver/internal/docx/read"
)

// The comparison outcomes of one file and case.
const (
	outIdentical     = "identical"       // both succeed, outputs identical
	outBothErrorSame = "both-error-same" // both fail, outputs identical
	outDiffer        = "differ"          // both succeed, outputs differ
	outErrorMismatch = "error-mismatch"  // one fails, or both fail differently
	outNoBaseline    = "no-baseline"     // the baseline has no entry
	outIntended      = "intended"        // differs as baseline/known/read.tsv records
)

var outcomeOrder = []string{outIdentical, outBothErrorSame, outDiffer, outErrorMismatch, outNoBaseline, outIntended}

// passes reports whether an outcome keeps compare green.
func passes(outcome string) bool {
	return outcome == outIdentical || outcome == outBothErrorSame || outcome == outIntended
}

// compare runs the Go docx CLI over the inputs and compares every run
// with the oracle's recorded output in the agent contract's shape
// (contractCompare); a run without recorded output is no-baseline. It writes the Go outputs under -out/docx and
// a per-run report to -out/compare-<set>.tsv, prints counts per case, and
// fails when any run is not identical or both-error-same.
func compare(args []string) error {
	fs := flag.NewFlagSet("compare", flag.ExitOnError)
	var c config
	c.flags(fs)
	bin := fs.String("docx", "", "Go docx CLI to compare (default: build ./cmd/docx into -out)")
	show := fs.Int("show", 20, "list at most this many mismatches per set")
	fs.Parse(args)
	if err := c.resolve(); err != nil {
		return err
	}
	if *bin == "" {
		*bin = filepath.Join(c.out, "bin", "docx")
		build := exec.Command("go", "build", "-o", *bin, "./cmd/docx")
		build.Dir = c.root
		build.Stdout, build.Stderr = os.Stdout, os.Stderr
		if err := build.Run(); err != nil {
			return fmt.Errorf("build cmd/docx: %w", err)
		}
	}
	ks, err := c.selectCases(func(k Case) bool { return k.Ported })
	if err != nil {
		return err
	}
	bad := 0
	for _, set := range c.sets {
		ins, err := c.inputs(set)
		if err != nil {
			return err
		}
		m, err := loadManifest(c.manifestPath(set))
		if err != nil {
			return err
		}
		known, err := loadKnown(filepath.Join(c.baseline, "known", "read.tsv"), set)
		if err != nil {
			return err
		}
		baseFull := filepath.Join(c.out, "oracle", set)
		if set != setCorpus {
			baseFull = filepath.Join(c.baseline, set)
		}
		goFull := filepath.Join(c.out, "docx", set)
		var jobs []job
		for _, in := range ins {
			for _, k := range ks {
				jobs = append(jobs, job{in, k})
			}
		}
		type row struct {
			file, kase, outcome, note string
		}
		var mu sync.Mutex
		var rows []row
		err = runAll(jobs, c.jobs, func(j job) error {
			r, err := runCLI(*bin, j.in, j.k)
			if err != nil {
				return err
			}
			if err := writeOutputs(goFull, j.in.path, j.k.Name, r); err != nil {
				return err
			}
			want, ok := m.entries[key{j.in.path, j.k.Name}]
			outcome, note := outNoBaseline, ""
			if bo, _, full := readOutputs(baseFull, j.in.path, j.k.Name); ok && full {
				outcome, note = contractCompare(j.k.Name, bo, want.exit, r)
			}
			if reason, listed := known[key{j.in.path, j.k.Name}]; listed && outcome != outIdentical && outcome != outBothErrorSame && outcome != outNoBaseline {
				outcome, note = outIntended, reason
			}
			mu.Lock()
			rows = append(rows, row{j.in.path, j.k.Name, outcome, note})
			mu.Unlock()
			return nil
		})
		if err != nil {
			return err
		}
		slices.SortFunc(rows, func(a, b row) int {
			if c := strings.Compare(a.file, b.file); c != 0 {
				return c
			}
			return strings.Compare(a.kase, b.kase)
		})
		counts := map[string]map[string]int{}
		total := map[string]int{}
		var report strings.Builder
		var shown []string
		for _, r := range rows {
			if counts[r.kase] == nil {
				counts[r.kase] = map[string]int{}
			}
			counts[r.kase][r.outcome]++
			total[r.outcome]++
			fmt.Fprintf(&report, "%s\t%s\t%s\t%s\n", r.file, r.kase, r.outcome, r.note)
			if !passes(r.outcome) {
				bad++
				if len(shown) < *show {
					shown = append(shown, fmt.Sprintf("  %s %s: %s %s", r.kase, r.file, r.outcome, r.note))
				}
			}
		}
		if err := os.MkdirAll(c.out, 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(filepath.Join(c.out, "compare-"+set+".tsv"), []byte(report.String()), 0o644); err != nil {
			return err
		}
		fmt.Printf("%s: %d inputs\n", set, len(ins))
		fmt.Printf("  %-24s", "case")
		for _, o := range outcomeOrder {
			fmt.Printf(" %16s", o)
		}
		fmt.Println()
		for _, k := range ks {
			fmt.Printf("  %-24s", k.Name)
			for _, o := range outcomeOrder {
				fmt.Printf(" %16d", counts[k.Name][o])
			}
			fmt.Println()
		}
		fmt.Printf("  %-24s", "total")
		for _, o := range outcomeOrder {
			fmt.Printf(" %16d", total[o])
		}
		fmt.Println()
		for _, s := range shown {
			fmt.Println(s)
		}
	}
	if bad > 0 {
		return fmt.Errorf("%d runs differ from the baseline", bad)
	}
	return nil
}

// loadKnown reads the intended differences of a set from the known
// differences file: tab-separated set, file, case and reason, "#" comments.
func loadKnown(path, set string) (map[key]string, error) {
	b, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return map[key]string{}, nil
	}
	if err != nil {
		return nil, err
	}
	out := map[key]string{}
	for n, line := range strings.Split(string(b), "\n") {
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		f := strings.Split(line, "\t")
		if len(f) != 4 {
			return nil, fmt.Errorf("%s:%d: want set, file, case and reason", path, n+1)
		}
		if f[0] == set {
			out[key{f[1], f[2]}] = f[3]
		}
	}
	return out, nil
}

// readKind is the read kind a case asks for ("find" for find).
func readKind(kase string) string {
	if strings.HasPrefix(kase, "find") {
		return "find"
	}
	if kind, ok := strings.CutPrefix(kase, "read-"); ok && !strings.HasPrefix(kind, "document") {
		return kind
	}
	return "document"
}

// contractCompare compares a Go run with the oracle's recorded output of
// the same case, in the agent contract's shape (docs/contract.md): the
// oracle's CommandResult is converted with docxread.FromOracle, which is the
// list of the surface differences (no wrapper, no nulls, snake_case keys,
// addresses, one comment thread shape), and the content must then be
// equal. Not compared: notes (docxdriver has none), and the anchor ranges
// of --kind comments, which docxdriver gives as locators; their number
// must agree. A case docxdriver refused must be refused: the Go side's
// code and wording are the contract's.
func contractCompare(kase string, oracleOut []byte, oracleExit int, r result) (outcome, note string) {
	kind := readKind(kase)
	want, err := docxread.FromOracle(kind, oracleOut)
	if err != nil || oracleExit != 0 {
		if r.exit != 0 {
			return outBothErrorSame, ""
		}
		return outErrorMismatch, fmt.Sprintf("exit %d where the oracle refused", r.exit)
	}
	if r.exit != 0 {
		return outErrorMismatch, fmt.Sprintf("exit %d want 0: %s", r.exit, clip(string(r.stderr)))
	}
	got, err := docxread.Decode(r.stdout)
	if err != nil {
		return outDiffer, "stdout is not JSON: " + err.Error()
	}
	if m, ok := got["markup"].(string); ok {
		got["markup"] = withoutPlaceholders(m)
	}
	// The Go markup addresses other stories' paragraphs; docxdriver's
	// does not (known difference 16).
	got = docxread.WithoutStoryAddresses(got).(docxread.Object)
	for _, o := range []docxread.Object{want, got} {
		delete(o, "notes")
		if kind == "comments" {
			threads, _ := o["comments"].([]any)
			for _, t := range threads {
				if th, ok := t.(docxread.Object); ok {
					if a, ok := th["anchor"].(docxread.Object); ok {
						ranges, _ := a["ranges"].([]any)
						th["anchor"] = docxread.Object{"ranges": len(ranges)}
					}
				}
			}
		}
	}
	w, err1 := docxread.Encode(want, true)
	g, err2 := docxread.Encode(got, true)
	if err := errors.Join(err1, err2); err != nil {
		return outDiffer, err.Error()
	}
	if w == g {
		return outIdentical, ""
	}
	return outDiffer, " " + lineDiff([]byte(w), []byte(g))
}

// lineDiff returns the first differing line between want and got, or empty if equal.
func lineDiff(want, got []byte) string {
	if bytes.Equal(want, got) {
		return ""
	}
	wl := strings.Split(string(want), "\n")
	gl := strings.Split(string(got), "\n")
	for i := range max(len(wl), len(gl)) {
		var w, g string
		if i < len(wl) {
			w = wl[i]
		}
		if i < len(gl) {
			g = gl[i]
		}
		if w != g {
			return fmt.Sprintf("line %d: want %q got %q", i+1, clip(w), clip(g))
		}
	}
	return ""
}

// clip truncates s to 160 bytes with an ellipsis suffix when longer.
func clip(s string) string {
	const n = 160
	if len(s) > n {
		return s[:n] + "..."
	}
	return s
}
