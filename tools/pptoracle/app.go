package main

import "strings"

// app is the PowerPoint process as the smoke and render logic see it. The
// real implementation is officeapp.MacApp; tests use a fake.
type app interface {
	// Running reports whether the PowerPoint process exists.
	Running() (bool, error)
	// Open launches PowerPoint on path (open -F -a: no window restore).
	Open(path string) error
	// Run runs an AppleScript with a timeout in seconds.
	Run(lines []string, seconds int) (string, error)
	// ForceQuit kills PowerPoint and waits until the process is gone.
	ForceQuit() error
}

// presentations returns the names of the open presentations.
func presentations(a app, seconds int) ([]string, error) {
	out, err := a.Run(scriptListPresentations(), seconds)
	if err != nil {
		return nil, err
	}
	if out == "" {
		return nil, nil
	}
	return strings.Split(out, "\n"), nil
}

// windowTexts returns PowerPoint's window titles and alert texts, or an
// error when System Events has no assistive access.
func windowTexts(a app) ([]string, error) {
	out, err := a.Run(scriptWindowTitles(), 5)
	if err != nil {
		return nil, err
	}
	if out == "" {
		return nil, nil
	}
	return strings.Split(out, "\n"), nil
}

// errNotAuthorisedMsg explains -1743.
const errNotAuthorisedMsg = "macOS has not authorised this process to control Microsoft PowerPoint (-1743); allow it in System Settings > Privacy & Security > Automation and run again"
