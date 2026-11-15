package main

import (
	"strings"

	"github.com/jhnoel/documentdriver/tools/internal/officeapp"
)

// appName is PowerPoint's AppleScript name; procName is its process name
// as pgrep -x sees it (the executable in Contents/MacOS).
const (
	appName  = "Microsoft PowerPoint"
	procName = "Microsoft PowerPoint"
	appPath  = "/Applications/Microsoft PowerPoint.app"
)

// AppleScript error numbers the tool reacts to.
const (
	errAETimeout      = -1712 // app did not answer: usually a modal alert
	errNotAuthorised  = -1743 // Automation permission not granted
	errNoAssistive    = -1728 // System Events: osascript not allowed assistive access
	errAssistiveAlt   = -25211
	errAppNotRunning  = -600
	errAppNotRunning2 = -609
)

// tellPPT addresses PowerPoint.
func tellPPT(stmt string) string {
	return "tell application " + officeapp.Quote(appName) + " to " + stmt
}

// scriptListPresentations lists the open presentations' file names, one
// per line.
func scriptListPresentations() []string {
	return []string{
		"set AppleScript's text item delimiters to linefeed",
		tellPPT("set ns to name of presentations"),
		"return ns as text",
	}
}

// scriptWindowTitles lists the titles of PowerPoint's windows and sheets
// through System Events (needs assistive access), one per line. Static
// texts inside each window are included so a repair alert's message is
// visible even when the window has no title.
func scriptWindowTitles() []string {
	return []string{
		"set out to {}",
		`tell application "System Events" to tell process ` + officeapp.Quote(procName),
		"repeat with w in windows",
		"set end of out to (name of w as text)",
		"try",
		"repeat with t in (value of static texts of w)",
		"set end of out to (t as text)",
		"end repeat",
		"end try",
		"try",
		"repeat with s in sheets of w",
		"repeat with t in (value of static texts of s)",
		"set end of out to (t as text)",
		"end repeat",
		"end repeat",
		"end try",
		"end repeat",
		"end tell",
		"set AppleScript's text item delimiters to linefeed",
		"return out as text",
	}
}

// scriptSaveAs saves the first presentation to dst in the given PowerPoint
// file format ("save as PNG", "save as PDF", "save as Open XML
// presentation"). PowerPoint 16.115 needs the destination as an HFS path
// string: given a POSIX file it reports success and writes nothing.
func scriptSaveAs(dst, format string) []string {
	return []string{
		"set dst to (POSIX file " + officeapp.Quote(dst) + ") as text",
		tellPPT("save presentation 1 in dst as " + format),
	}
}

// classifyWindows looks for known PowerPoint alerts in window texts.
func classifyWindows(texts []string) string {
	for _, t := range texts {
		l := strings.ToLower(t)
		switch {
		case strings.Contains(l, "grant file access") || strings.Contains(l, "additional permissions are required"):
			return statusAccessDialog
		case strings.Contains(l, "repair") || strings.Contains(l, "found a problem with content") ||
			strings.Contains(l, "can't read") || strings.Contains(l, "cannot read"):
			return statusRepair
		}
	}
	return ""
}
