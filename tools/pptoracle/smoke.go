package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/jhnoel/documentdriver/tools/internal/officeapp"
)

// record is one JSON line of the smoke report.
type record struct {
	File    string  `json:"file"`
	Role    string  `json:"role"` // "source" or "output"
	Source  string  `json:"source,omitempty"`
	Status  string  `json:"status"`
	Seconds float64 `json:"seconds"`
	Detail  string  `json:"detail,omitempty"`
}

// summary counts outcomes over a run.
type summary struct {
	Opened   int
	Skipped  int
	Failures []string // outputs that were not clean although their source was
}

const smokeUsage = `usage: pptoracle smoke [--timeout 20] [--all] [--source deck.pptx] [--json] [--workdir dir] files-or-dirs...

Open each deck in Microsoft PowerPoint and report whether it opened cleanly.
Outputs are paired with sources by name: <name>--source.pptx is the
unedited deck and <name>--<op>[-<hash>].pptx its outputs; or pass --source.
Each source is opened first, and its outputs are skipped when PowerPoint
does not open the source cleanly. Without --all one output per source and
operation is opened. Exit status 1 when any output whose source opened
cleanly (or that has no source) did not open cleanly.

Statuses: clean, repair (alert text read through System Events, which
needs assistive access), access-dialog, dialog (an alert blocks loading:
PowerPoint lists the deck as "missing value" or stops answering; the
repair prompt looks like this when its text cannot be read), not-listed,
crashed, error, skipped.
`

// cmdSmoke opens decks in PowerPoint and returns the exit code.
func cmdSmoke(a app, args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("smoke", flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.Usage = func() { fmt.Fprint(stderr, smokeUsage) }
	timeout := fs.Float64("timeout", 20, "seconds to wait for each deck")
	all := fs.Bool("all", false, "open every output, not one per source and operation")
	source := fs.String("source", "", "the source of every other file given")
	asJSON := fs.Bool("json", false, "write JSON lines instead of text")
	workdir := fs.String("workdir", officeapp.DefaultWorkdir(), "folder PowerPoint may read and write (granted once); copies live here")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	inputs := fs.Args()
	if *source != "" {
		inputs = append([]string{*source}, inputs...)
	}
	if len(inputs) == 0 {
		fs.Usage()
		return 2
	}
	files, err := collect(inputs)
	if err != nil {
		fmt.Fprintln(stderr, "pptoracle smoke:", err)
		return 2
	}
	groups, err := pair(files, *all, *source)
	if err != nil {
		fmt.Fprintln(stderr, "pptoracle smoke:", err)
		return 2
	}
	if code := preflight(a, stderr); code != 0 {
		return code
	}
	rd, err := newRunDir(*workdir)
	if err != nil {
		fmt.Fprintln(stderr, "pptoracle smoke:", err)
		return 2
	}
	cleanup.add(func() { rd.Close() })
	defer rd.Close()
	sum, err := runSmoke(a, rd, groups, time.Duration(*timeout*float64(time.Second)), reporter(stdout, *asJSON))
	if err != nil {
		fmt.Fprintln(stderr, "pptoracle smoke:", err)
		return 3
	}
	if !*asJSON {
		fmt.Fprintf(stdout, "%d opened, %d skipped (source not clean), %d failed\n", sum.Opened, sum.Skipped, len(sum.Failures))
		for _, f := range sum.Failures {
			fmt.Fprintln(stdout, "  not clean:", f)
		}
	}
	if len(sum.Failures) > 0 {
		return 1
	}
	return 0
}

// preflight refuses to run while PowerPoint is open: the tool force-quits
// it, and the user may have unsaved work.
func preflight(a app, stderr io.Writer) int {
	if _, err := os.Stat(appPath); err != nil {
		fmt.Fprintf(stderr, "pptoracle needs %s\n", appPath)
		return 2
	}
	up, err := a.Running()
	if err != nil {
		fmt.Fprintln(stderr, "pptoracle:", err)
		return 2
	}
	if up {
		fmt.Fprintln(stderr, "pptoracle: Microsoft PowerPoint is running; save your work and quit it first (this tool force-quits PowerPoint)")
		return 2
	}
	return 0
}

// reporter returns a function that prints one record.
func reporter(w io.Writer, asJSON bool) func(record) {
	n := 0
	enc := json.NewEncoder(w)
	return func(r record) {
		if asJSON {
			if err := enc.Encode(r); err != nil {
				fmt.Fprintf(os.Stderr, "pptoracle: write record: %v\n", err)
			}
			return
		}
		n++
		line := fmt.Sprintf("[%d] %-13s %6.1fs %s %s", n, r.Status, r.Seconds, r.Role, filepath.Base(r.File))
		if r.Detail != "" {
			line += "  (" + r.Detail + ")"
		}
		fmt.Fprintln(w, line)
	}
}

// runSmoke opens every source, then its outputs, each in a fresh
// PowerPoint, from a private copy under rd.
func runSmoke(a app, rd *runDir, groups []group, timeout time.Duration, report func(record)) (summary, error) {
	var sum summary
	seq := 0
	open := func(path string) (openResult, error) {
		seq++
		cp := rd.file(strconv.Itoa(seq) + "--" + filepath.Base(path))
		defer os.Remove(cp)
		defer os.Remove(filepath.Join(filepath.Dir(cp), "~$"+filepath.Base(cp)))
		if err := officeapp.CopyFile(path, cp); err != nil {
			return openResult{Status: statusError, Detail: err.Error()}, nil
		}
		res, err := openAndWait(a, cp, timeout)
		if qerr := a.ForceQuit(); qerr != nil && err == nil {
			err = fmt.Errorf("%w: %v", errAbort, qerr)
		}
		return res, err
	}
	for _, g := range groups {
		sourceClean := true
		if g.Source != "" {
			res, err := open(g.Source)
			sum.Opened++
			report(record{File: g.Source, Role: "source", Status: res.Status, Seconds: round1(res.Seconds), Detail: res.Detail})
			if err != nil {
				return sum, err
			}
			sourceClean = res.Status == statusClean
		}
		for _, out := range g.Outputs {
			if !sourceClean {
				sum.Skipped++
				report(record{File: out, Role: "output", Source: g.Source, Status: statusSkipped,
					Detail: "source did not open cleanly"})
				continue
			}
			res, err := open(out)
			sum.Opened++
			report(record{File: out, Role: "output", Source: g.Source, Status: res.Status, Seconds: round1(res.Seconds), Detail: res.Detail})
			if err != nil {
				return sum, err
			}
			if res.Status != statusClean {
				sum.Failures = append(sum.Failures, out)
			}
		}
	}
	return sum, nil
}

// round1 rounds seconds to one decimal place.
func round1(s float64) float64 { return float64(int(s*10+0.5)) / 10 }
