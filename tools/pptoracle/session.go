package main

import (
	"errors"
	"fmt"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/jhnoel/documentdriver/tools/internal/officeapp"
)

// Open outcomes.
const (
	statusClean        = "clean"         // listed in presentations
	statusRepair       = "repair"        // a repair/problem alert was read from the window
	statusAccessDialog = "access-dialog" // "Grant File Access": the file is outside the sandbox
	statusDialog       = "dialog"        // an alert blocks loading (repair prompt or error), text unreadable
	statusNotListed    = "not-listed"    // PowerPoint answered but never listed the file
	statusCrashed      = "crashed"       // the process went away
	statusError        = "error"         // the tool failed (launch, copy, script)
	statusSkipped      = "skipped"       // source not clean, so the output was not opened
)

// errAbort stops the whole run (not one file): permission errors.
var errAbort = errors.New("abort")

// clock is time.Now, replaceable in tests.
var (
	clock = time.Now
	sleep = time.Sleep
)

// pollInterval is the delay between presentation listings.
var pollInterval = time.Second

// openResult is the outcome of opening one file.
type openResult struct {
	Status  string
	Detail  string
	Seconds float64
}

// openAndWait launches PowerPoint fresh on path and waits until the file
// is listed or the timeout passes. It leaves PowerPoint running on success
// so the caller can script it; the caller force-quits afterwards. It
// returns errAbort (wrapped) for conditions that make every later file
// meaningless, such as -1743.
func openAndWait(a app, path string, timeout time.Duration) (openResult, error) {
	if err := a.ForceQuit(); err != nil {
		return openResult{Status: statusError, Detail: err.Error()}, err
	}
	start := clock()
	elapsed := func() float64 { return clock().Sub(start).Seconds() }
	if err := a.Open(path); err != nil {
		return openResult{Status: statusError, Detail: err.Error(), Seconds: elapsed()}, nil
	}
	name := filepath.Base(path)
	deadline := start.Add(timeout)
	var lastErr error
	var lastNames []string
	timeouts, polls := 0, 0
	noAssistive := false
	for clock().Before(deadline) {
		sleep(pollInterval)
		polls++
		names, err := presentations(a, 3)
		if err == nil && slices.Contains(names, name) {
			return openResult{Status: statusClean, Seconds: elapsed()}, nil
		}
		if err == nil {
			lastNames = names
		}
		if officeapp.IsOSA(err, errNotAuthorised) {
			return openResult{Status: statusError, Detail: errNotAuthorisedMsg, Seconds: elapsed()},
				fmt.Errorf("%w: %s", errAbort, errNotAuthorisedMsg)
		}
		if err != nil {
			lastErr = err
			if officeapp.IsOSA(err, errAETimeout) {
				timeouts++
			}
		}
		if !noAssistive {
			texts, werr := windowTexts(a)
			switch {
			case officeapp.IsOSA(werr, errNoAssistive, errAssistiveAlt):
				noAssistive = true
			case werr == nil:
				if s := classifyWindows(texts); s != "" {
					return openResult{Status: s, Detail: joinTexts(texts), Seconds: elapsed()}, nil
				}
			}
		}
		if polls >= 3 {
			up, rerr := a.Running()
			if rerr == nil && !up {
				return openResult{Status: statusCrashed, Seconds: elapsed()}, nil
			}
		}
	}
	res := openResult{Status: statusNotListed, Seconds: elapsed()}
	// PowerPoint answering nothing for most of the wait means a modal
	// alert owns its main thread; the repair prompt is the usual one.
	// A presentation whose name is "missing value" is one PowerPoint
	// started loading and then stopped on an alert. Observed with
	// PowerPoint 16.115 on a deck with a:off x="notanumber" (a known repair
	// case) and on decks PowerPoint rejects.
	if timeouts*2 > polls || slices.Contains(lastNames, "missing value") {
		res.Status = statusDialog
	}
	if lastErr != nil {
		res.Detail = lastErr.Error()
	}
	if lastNames != nil {
		res.Detail = strings.TrimSpace(res.Detail + fmt.Sprintf(" open presentations: %q", lastNames))
	}
	if noAssistive {
		res.Detail += " (alert text unreadable: no assistive access for System Events)"
	}
	return res, nil
}

// joinTexts joins texts with " | " separators.
func joinTexts(t []string) string {
	s := ""
	for i, x := range t {
		if i > 0 {
			s += " | "
		}
		s += x
	}
	return s
}
