package officeapp

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"
)

// Open outcomes.
const (
	StatusClean        = "clean"
	StatusRepair       = "repair"
	StatusOpenError    = "open-error"
	StatusAccessDialog = "access-dialog"
	StatusDialog       = "dialog"
	StatusNotListed    = "not-listed"
	StatusCrashed      = "crashed"
	StatusError        = "error"
)

// errAbort stops the whole run: a permission error makes every later
// file meaningless.
var errAbort = errors.New("abort")

// clock, sleep and the poll interval are replaceable in tests.
var (
	clock        = time.Now
	sleep        = time.Sleep
	pollInterval = time.Second
)

// openResult is the outcome of opening one file.
type openResult struct {
	Status  string   `json:"status"`
	Alerts  []string `json:"alerts,omitempty"`
	Seconds float64  `json:"seconds"`
	Detail  string   `json:"detail,omitempty"`
}

// record is one report line.
type record struct {
	File string `json:"file"`
	Role string `json:"role"` // source or output
	openResult
	Pass bool `json:"pass"`
}

// Smoke is one smoke tool: its name (flag set, messages, copy prefix),
// the AppleScript noun of its documents ("workbooks", "documents"),
// the extensions it opens, its alert classifier, its app and its usage
// text.
type Smoke struct {
	Tool     string // flag set, messages, copy prefix
	Noun     string // AppleScript noun, plural
	Exts     []string
	Classify func(texts []string) string
	App      Spec
	Usage    string
}

// shortApp returns the app's short name, without the Microsoft prefix.
func (s Smoke) shortApp() string {
	return strings.TrimPrefix(s.App.AppName, "Microsoft ")
}

// singular returns the singular of a plural noun ("workbooks" becomes
// "workbook").
func singular(noun string) string {
	return strings.TrimSuffix(noun, "s")
}

// hasExt reports whether name ends in one of the smoke's extensions.
func (s Smoke) hasExt(name string) bool {
	l := strings.ToLower(name)
	return slices.ContainsFunc(s.Exts, func(e string) bool { return strings.HasSuffix(l, e) })
}

// mentionsExt reports whether text contains one of the smoke's
// extensions.
func (s Smoke) mentionsExt(text string) bool {
	l := strings.ToLower(text)
	return slices.ContainsFunc(s.Exts, func(e string) bool { return strings.Contains(l, e) })
}

// passes judges an output against its source (nil: no source). Without
// a source, or with a clean one, the output must be clean. With a source
// that already prompted, the output may show the same status and no
// alert text the source did not.
func passes(out openResult, src *openResult) bool {
	if out.Status == StatusClean {
		return true
	}
	if src == nil || src.Status == StatusClean || out.Status != src.Status {
		return false
	}
	for _, a := range out.Alerts {
		if !slices.Contains(src.Alerts, a) {
			return false
		}
	}
	return true
}

// Launch retries: right after the app is force-quit, LaunchServices can
// still refuse to start it (open fails with error -600, procNotFound)
// though the process is gone. Such an open is retried, after a pause
// that grows with each try, before the file is reported as an error.
const (
	launchRetries = 2
	launchBackoff = 2 * time.Second
)

// openRetrying opens path in the app, retrying a launch race (-600).
func openRetrying(a App, path string) error {
	var err error
	for try := range launchRetries + 1 {
		if try > 0 {
			sleep(time.Duration(try) * launchBackoff)
		}
		if err = a.Open(path); err == nil || !launchRace(err) {
			return err
		}
	}
	return err
}

// launchRace reports whether an open failed because LaunchServices did
// not yet see the quit app gone (error -600).
func launchRace(err error) bool { return strings.Contains(err.Error(), "error -600") }

// openAndWait launches a fresh app on path and waits until the document
// is listed, an alert is read, the app dies, or the timeout passes.
func (s Smoke) openAndWait(a App, path string, timeout time.Duration) (openResult, error) {
	if err := a.ForceQuit(); err != nil {
		return openResult{Status: StatusError, Detail: err.Error()}, err
	}
	start := clock()
	elapsed := func() float64 { return float64(int(clock().Sub(start).Seconds()*10+0.5)) / 10 }
	if err := openRetrying(a, path); err != nil {
		return openResult{Status: StatusError, Detail: err.Error(), Seconds: elapsed()}, nil
	}
	name := filepath.Base(path)
	deadline := start.Add(timeout)
	timeouts, polls := 0, 0
	noAssistive := false
	for clock().Before(deadline) {
		sleep(pollInterval)
		polls++
		out, err := a.Run(scriptList(s.App.AppName, s.Noun), 3)
		if err == nil && slices.Contains(strings.Split(out, "\n"), name) {
			return openResult{Status: StatusClean, Seconds: elapsed()}, nil
		}
		if IsOSA(err, ErrNotAuthorised) {
			msg := "macOS has not authorised this process to control " + s.App.AppName + " (-1743); allow it in System Settings > Privacy & Security > Automation"
			return openResult{Status: StatusError, Detail: msg, Seconds: elapsed()}, fmt.Errorf("%w: %s", errAbort, msg)
		}
		if IsOSA(err, ErrAETimeout) {
			timeouts++
		}
		if !noAssistive {
			texts, werr := a.Run(ScriptWindowTexts(s.App.ProcName), 5)
			switch {
			case IsOSA(werr, ErrNoAssistive, ErrAssistiveAlt):
				noAssistive = true
			case werr == nil:
				lines := strings.Split(texts, "\n")
				if st := s.Classify(lines); st != "" {
					return openResult{Status: st, Alerts: s.alertTexts(lines), Seconds: elapsed()}, nil
				}
			}
		}
		if polls >= 3 {
			if up, rerr := a.Running(); rerr == nil && !up {
				return openResult{Status: StatusCrashed, Seconds: elapsed()}, nil
			}
		}
	}
	res := openResult{Status: StatusNotListed, Seconds: elapsed()}
	if timeouts*2 > polls {
		res.Status = StatusDialog // a modal alert owns the app's main thread
	}
	if noAssistive {
		res.Detail = "alert text unreadable: no assistive access for System Events"
	}
	return res, nil
}

// alertTexts keeps the non-empty texts, with file names (which differ
// between a source and its outputs) left out.
func (s Smoke) alertTexts(lines []string) []string {
	var out []string
	for _, l := range lines {
		l = strings.TrimSpace(l)
		if l == "" || s.hasExt(l) {
			continue
		}
		out = append(out, s.blankFileName(l))
	}
	return out
}

// blankFileName blanks a quoted or bare file name inside an alert text.
func (s Smoke) blankFileName(text string) string {
	fields := strings.Fields(text)
	for i, f := range fields {
		if s.mentionsExt(f) {
			fields[i] = "<file>"
		}
	}
	return strings.Join(fields, " ")
}

// Main opens each file in the app and reports pass or fail against the
// source, returning the exit code.
func (s Smoke) Main(a App, args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet(s.Tool, flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.Usage = func() { fmt.Fprint(stderr, s.Usage) }
	timeout := fs.Float64("timeout", 30, "seconds to wait for each "+singular(s.Noun))
	source := fs.String("source", "", "the source every other file is judged against")
	asJSON := fs.Bool("json", false, "write JSON lines")
	workdir := fs.String("workdir", DefaultWorkdir(), "folder "+s.shortApp()+" has been granted (flat copies live here)")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	files := fs.Args()
	if len(files) == 0 && *source == "" {
		fs.Usage()
		return 2
	}
	if code := s.checkInputs(append([]string{*source}, files...), stderr); code != 0 {
		return code
	}
	if code := s.preflight(a, *workdir, stderr); code != 0 {
		return code
	}
	report := func(r record) {
		if *asJSON {
			if err := json.NewEncoder(stdout).Encode(r); err != nil {
				fmt.Fprintln(stderr, s.Tool+":", err)
			}
			return
		}
		verdict := "pass"
		if !r.Pass {
			verdict = "FAIL"
		}
		fmt.Fprintf(stdout, "%-4s %-13s %5.1fs %s %s %s\n", verdict, r.Status, r.Seconds, r.Role, filepath.Base(r.File), strings.Join(r.Alerts, " | "))
	}
	seq := 0
	open := func(path string) (openResult, error) {
		seq++
		cp := filepath.Join(*workdir, s.Tool+"-"+strconv.Itoa(os.Getpid())+"-"+strconv.Itoa(seq)+"--"+filepath.Base(path))
		defer os.Remove(cp)
		defer os.Remove(filepath.Join(*workdir, "~$"+filepath.Base(cp)))
		if err := CopyFile(path, cp); err != nil {
			return openResult{Status: StatusError, Detail: err.Error()}, nil
		}
		res, err := s.openAndWait(a, cp, time.Duration(*timeout*float64(time.Second)))
		if qerr := a.ForceQuit(); qerr != nil && err == nil {
			err = fmt.Errorf("%w: %w", errAbort, qerr)
		}
		return res, err
	}
	var src *openResult
	if *source != "" {
		res, err := open(*source)
		report(record{File: *source, Role: "source", openResult: res, Pass: true})
		if err != nil {
			fmt.Fprintln(stderr, s.Tool+":", err)
			return 3
		}
		src = &res
	}
	failed := 0
	for _, f := range files {
		res, err := open(f)
		ok := passes(res, src)
		report(record{File: f, Role: "output", openResult: res, Pass: ok})
		if err != nil {
			fmt.Fprintln(stderr, s.Tool+":", err)
			return 3
		}
		if !ok {
			failed++
		}
	}
	if failed > 0 {
		return 1
	}
	return 0
}

// checkInputs refuses, before the app is touched, files that are not
// regular files with one of the smoke's extensions ("" is skipped: no
// source).
func (s Smoke) checkInputs(paths []string, stderr io.Writer) int {
	for _, p := range paths {
		if p == "" {
			continue
		}
		st, err := os.Stat(p)
		switch {
		case err != nil:
			fmt.Fprintln(stderr, s.Tool+":", err)
			return 2
		case !st.Mode().IsRegular() || !s.hasExt(p):
			fmt.Fprintf(stderr, "%s: %s is not a %s file\n", s.Tool, p, strings.Join(s.Exts, " or "))
			return 2
		}
	}
	return 0
}

// preflight refuses to run while the app is open (the tool force-quits
// it and the user may have unsaved work) or without the granted folder.
func (s Smoke) preflight(a App, workdir string, stderr io.Writer) int {
	if _, err := os.Stat(s.App.AppPath); err != nil {
		fmt.Fprintf(stderr, "%s needs %s\n", s.Tool, s.App.AppPath)
		return 2
	}
	if st, err := os.Stat(workdir); err != nil || !st.IsDir() {
		fmt.Fprintf(stderr, "%s: work folder %q missing: create it and grant %s access to it once, or pass --workdir\n", s.Tool, workdir, s.shortApp())
		return 2
	}
	up, err := a.Running()
	if err != nil {
		fmt.Fprintln(stderr, s.Tool+":", err)
		return 2
	}
	if up {
		fmt.Fprintln(stderr, s.Tool+": "+s.App.AppName+" is running; save your work and quit it first (this tool force-quits "+s.shortApp()+")")
		return 2
	}
	return 0
}
