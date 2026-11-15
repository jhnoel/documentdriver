package officeapp

import (
	"fmt"
	"strings"
)

// Quote returns s as an AppleScript string literal.
func Quote(s string) string {
	return `"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(s) + `"`
}

// WithTimeout wraps body lines in an AppleScript timeout block.
func WithTimeout(seconds int, body ...string) []string {
	out := []string{fmt.Sprintf("with timeout of %d seconds", seconds)}
	out = append(out, body...)
	return append(out, "end timeout")
}

// ScriptWindowTexts lists the app's window titles and the static texts
// of its windows and sheets through System Events (needs assistive
// access).
func ScriptWindowTexts(procName string) []string {
	return []string{
		"set out to {}",
		`tell application "System Events" to tell process ` + Quote(procName),
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

// scriptList lists the app's open documents' names, one per line.
func scriptList(appName, noun string) []string {
	return []string{
		"set AppleScript's text item delimiters to linefeed",
		"tell application " + Quote(appName) + " to set ns to name of " + noun,
		"return ns as text",
	}
}
