// Package officeapp drives a Microsoft Office app on macOS for the
// dev-only smoke and oracle tools (tools/excel-smoke, tools/word-smoke,
// tools/pptoracle): the process through pgrep -x and pkill -9 -x on its
// exact name, documents through open -F -a, and AppleScript through
// osascript. It FORCE-QUITS the app; the tools' callers must have the
// maintainer's agreement. It never runs in internal/ code.
package officeapp

import (
	"sync"
	"time"
)

// App is an Office app as the tools' logic sees it; tests use a fake.
type App interface {
	Running() (bool, error)
	Open(path string) error
	Run(lines []string, seconds int) (string, error)
	ForceQuit() error
}

// Spec names one app: its AppleScript name, its process name as
// pgrep -x matches it, its bundle path, and how long ForceQuit waits
// after the process is gone before a relaunch (LaunchServices).
type Spec struct {
	AppName    string        // AppleScript name, e.g. Microsoft Excel
	ProcName   string        // process name as pgrep -x matches it
	AppPath    string        // bundle path, e.g. /Applications/Microsoft Excel.app
	QuitSettle time.Duration // wait after the process is gone, before a relaunch
}

// MacApp is App over the installed app named by Spec.
type MacApp struct {
	spec     Spec
	mu       sync.Mutex
	launched bool
}

// NewMacApp returns a MacApp driving the app named by s.
func NewMacApp(s Spec) *MacApp { return &MacApp{spec: s} }
