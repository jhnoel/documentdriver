// Command oracle compares the Go docx CLI with the pinned docxdriver 0.5.2
// CLI (the parity oracle) on the same inputs.
//
//	oracle record   run the docxdriver CLI and write the baseline
//	oracle compare  run the Go docx CLI and compare it with the baseline
//
// Inputs come in sets: "fixtures" (the small docx files of the docxdriver
// repository: test-docs/, the tests' fixtures and the blank template) and
// "corpus-lo" (corpus/lo of the docxdriver repository, about 1,500
// LibreOffice import/export test files). Every input runs every case of
// cases.go. The baseline (tools/oracle/baseline) holds a manifest per set
// (exit code and truncated SHA-256 of stdout and stderr per file and case)
// and, for the fixtures only, the full outputs; full corpus outputs go to
// the gitignored results/oracle. See baseline/README.md for the commands.
package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/jhnoel/documentdriver/internal/repo"
)

const usage = `usage: oracle <command> [flags]

  record   run the pinned docxdriver CLI over the inputs and write the baseline
  compare  run the Go docx CLI over the inputs and compare with the baseline
  write    the write baseline: plans for all 34 operations (run "oracle write")

Run "oracle <command> -h" for the flags.
`

// main dispatches oracle subcommands and exits 1 on error.
func main() {
	if len(os.Args) < 2 {
		fmt.Fprint(os.Stderr, usage)
		os.Exit(2)
	}
	var err error
	switch os.Args[1] {
	case "record":
		err = record(os.Args[2:])
	case "compare":
		err = compare(os.Args[2:])
	case "write":
		err = writeCmd(os.Args[2:])
	case "-h", "-help", "--help", "help":
		fmt.Print(usage)
		return
	default:
		fmt.Fprint(os.Stderr, usage)
		os.Exit(2)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "oracle:", err)
		os.Exit(1)
	}
}

// config is what both commands share: where the inputs, the baseline and
// the full outputs live, and which sets, files and cases to run.
type config struct {
	root       string // the repository (or worktree) root
	docxdriver string // the docxdriver repository: fixtures, and the corpus by default
	corpus     string // corpus/lo
	baseline   string // tools/oracle/baseline
	out        string // full outputs, gitignored
	setList    string // -sets
	sets       []string
	only       string // substring filter on input paths
	caseNames  string // comma-separated case names; empty for the default
	jobs       int
}

// flags registers the shared oracle flags on fs.
func (c *config) flags(fs *flag.FlagSet) {
	fs.StringVar(&c.docxdriver, "docxdriver", os.Getenv("DOCXDRIVER_REPO"), "docxdriver repository (default $DOCXDRIVER_REPO, else ../docxdriver beside the main checkout)")
	fs.StringVar(&c.corpus, "corpus", os.Getenv("DOCX_CORPUS"), "corpus/lo directory (default $DOCX_CORPUS, else <docxdriver>/corpus/lo)")
	fs.StringVar(&c.out, "out", "", "directory for full outputs (default <root>/results/oracle)")
	fs.StringVar(&c.setList, "sets", setFixtures+","+setAuthored+","+setCorpus, "comma-separated input sets: "+setFixtures+", "+setAuthored+", "+setCorpus)
	fs.StringVar(&c.only, "only", "", "run only inputs whose path contains this substring")
	fs.StringVar(&c.caseNames, "cases", "", "comma-separated cases (default: all for record, ported for compare)")
	fs.IntVar(&c.jobs, "j", runtime.NumCPU(), "parallel runs")
}

// resolve fills in the defaults after flag parsing.
func (c *config) resolve() error {
	root, err := repo.Root()
	if err != nil {
		return err
	}
	c.root = root
	if c.docxdriver == "" {
		main := root
		if m, ok := repo.MainCheckout(root); ok {
			main = m
		}
		c.docxdriver = filepath.Join(filepath.Dir(main), "docxdriver")
	}
	if c.corpus == "" {
		c.corpus = filepath.Join(c.docxdriver, "corpus", "lo")
	}
	if c.out == "" {
		c.out = filepath.Join(root, "results", "oracle")
	}
	c.baseline = filepath.Join(root, "tools", "oracle", "baseline")
	for s := range strings.SplitSeq(c.setList, ",") {
		if s = strings.TrimSpace(s); s == "" {
			continue
		}
		if s != setFixtures && s != setCorpus && s != setAuthored {
			return fmt.Errorf("unknown set %q (want %s, %s or %s)", s, setFixtures, setAuthored, setCorpus)
		}
		c.sets = append(c.sets, s)
	}
	if c.jobs < 1 {
		c.jobs = 1
	}
	return nil
}

// selectCases returns the cases named by -cases, or def when it is empty.
func (c *config) selectCases(def func(Case) bool) ([]Case, error) {
	var out []Case
	if c.caseNames == "" {
		for _, k := range cases {
			if def(k) {
				out = append(out, k)
			}
		}
		return out, nil
	}
	for name := range strings.SplitSeq(c.caseNames, ",") {
		k, ok := caseByName(strings.TrimSpace(name))
		if !ok {
			return nil, fmt.Errorf("unknown case %q", name)
		}
		out = append(out, k)
	}
	return out, nil
}
