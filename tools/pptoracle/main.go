// Command pptoracle uses Microsoft PowerPoint for Mac as the oracle for
// pptxdriver: whether PowerPoint opens a deck without a repair prompt
// (smoke), what PowerPoint draws (render), and what PowerPoint writes when
// it saves (resave).
//
// It needs macOS with PowerPoint installed and refuses to run while
// PowerPoint is open, because it launches PowerPoint fresh for every file
// and force-quits it afterwards, on errors and on signals. Decks are copied
// into a run folder under a work directory that PowerPoint has been granted
// access to once (see officeapp.DefaultWorkdir), so PowerPoint never touches the
// originals, leaves no ~$ lock files beside them and saves without a
// "Grant File Access" dialog. Opening alone needs no grant, since decks are
// handed to PowerPoint with open(1). The first run asks macOS for
// permission to control PowerPoint.
package main

import (
	"fmt"
	"io"
	"os"
	"os/signal"
	"runtime"
	"sync"
	"syscall"

	"github.com/jhnoel/documentdriver/tools/internal/officeapp"
)

const usage = `usage: pptoracle <command> [args]

  smoke   open decks in PowerPoint and report repair prompts
  render  export slides to PNG through PowerPoint
  resave  save a deck through PowerPoint to a new file

Run "pptoracle <command> -h" for details. macOS with Microsoft PowerPoint
only; refuses to run while PowerPoint is open.
`

// cleanupList holds actions to run on a signal.
type cleanupList struct {
	mu  sync.Mutex
	fns []func()
}

// add appends fn to the cleanup actions to run later.
func (c *cleanupList) add(fn func()) {
	c.mu.Lock()
	c.fns = append(c.fns, fn)
	c.mu.Unlock()
}

// run executes cleanup actions in reverse order and clears them.
func (c *cleanupList) run() {
	c.mu.Lock()
	fns := c.fns
	c.fns = nil
	c.mu.Unlock()
	for i := len(fns) - 1; i >= 0; i-- {
		fns[i]()
	}
}

var cleanup cleanupList

// main installs signal cleanup and runs the CLI command then exits.
func main() {
	a := officeapp.NewMacApp(officeapp.Spec{AppName: appName, ProcName: procName, AppPath: appPath, QuitSettle: 0})
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt, syscall.SIGTERM, syscall.SIGHUP)
	go func() {
		s := <-sig
		if a.WasLaunched() {
			if err := a.ForceQuit(); err != nil {
				fmt.Fprintf(os.Stderr, "pptoracle: force quit: %v\n", err)
			}
		}
		cleanup.run()
		fmt.Fprintf(os.Stderr, "pptoracle: %v: PowerPoint force-quit, run folder removed\n", s)
		os.Exit(130)
	}()
	os.Exit(run(a, os.Args[1:], os.Stdout, os.Stderr))
}

// run dispatches the command and returns its exit code.
func run(a app, args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprint(stderr, usage)
		return 2
	}
	cmd, rest := args[0], args[1:]
	var fn func(app, []string, io.Writer, io.Writer) int
	switch cmd {
	case "smoke":
		fn = cmdSmoke
	case "render":
		fn = cmdRender
	case "resave":
		fn = cmdResave
	case "-h", "--help", "help":
		fmt.Fprint(stdout, usage)
		return 0
	default:
		fmt.Fprintf(stderr, "pptoracle: unknown command %q\n%s", cmd, usage)
		return 2
	}
	if runtime.GOOS != "darwin" && !isHelp(rest) {
		fmt.Fprintln(stderr, "pptoracle: needs macOS with Microsoft PowerPoint")
		return 2
	}
	return fn(a, rest, stdout, stderr)
}

// isHelp reports whether the first argument asks for help.
func isHelp(args []string) bool {
	return len(args) > 0 && (args[0] == "-h" || args[0] == "--help")
}
