package officeapp

import (
	"errors"
	"regexp"
	"strconv"
	"strings"
)

// AppleScript error numbers the tools act on.
const (
	ErrAETimeout      = -1712 // the app did not answer: usually a modal alert
	ErrNotAuthorised  = -1743 // Automation permission not granted
	ErrNoAssistive    = -1728 // System Events: no assistive access
	ErrAssistiveAlt   = -25211
	ErrAppNotRunning  = -600 // the app is not running (LaunchServices)
	ErrAppNotRunning2 = -609
)

// errNumRE matches the trailing AppleScript error number in osascript
// stderr, such as "12:40: execution error: ... (-1712)".
var errNumRE = regexp.MustCompile(`\((-?\d+)\)\s*$`)

// OSAError is a failed osascript run with its AppleScript error number.
type OSAError struct {
	Num int
	Msg string
}

// Error returns the trimmed AppleScript error message.
func (e *OSAError) Error() string { return strings.TrimSpace(e.Msg) }

// ParseOSAError builds an OSAError from stderr, extracting its trailing
// error number.
func ParseOSAError(stderr string) *OSAError {
	s := strings.TrimSpace(stderr)
	e := &OSAError{Msg: s}
	if m := errNumRE.FindStringSubmatch(s); m != nil {
		if n, err := strconv.Atoi(m[1]); err == nil {
			e.Num = n
		}
	}
	return e
}

// IsOSA reports whether err is an OSAError with one of nums.
func IsOSA(err error, nums ...int) bool {
	var oe *OSAError
	if !errors.As(err, &oe) {
		return false
	}
	for _, n := range nums {
		if oe.Num == n {
			return true
		}
	}
	return false
}
