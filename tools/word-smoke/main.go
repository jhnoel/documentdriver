// Command word-smoke opens .docx files in Microsoft Word on macOS and
// reports whether Word asked to repair them, modelled on tools/pptoracle
// smoke.
//
//	word-smoke [--timeout 30] [--json] [--workdir dir] --source in.docx out.docx...
//
// Each file is copied flat into the work folder Word has been granted
// (<repo>/corpus/pptoracle, or $PPTORACLE_WORKDIR, or --workdir) and opened
// there in a fresh Word; Word reads nothing outside that folder. The tool
// FORCE-QUITS Word before and after each file: never run it while
// the maintainer may have Word open, and only after they agree (it
// refuses to start while Word runs).
//
// make word-smoke SOURCE=in.docx OUT="out.docx ..." runs it through
// run.sh, which refuses while Word runs, builds the tool and keeps the
// JSON report under results/word-smoke/. Files that are missing or not
// .docx are refused before Word is touched.
//
// Outputs are judged relative to their source (owner decision
// 2026-11-13): an output passes when it opens clean, or when its source
// already did not and the output adds no new alert (same status, and
// every alert text it shows the source showed too). Exit status 1 when
// any output fails, 2 for usage or environment errors, 3 when the run
// had to stop.
package main

import (
	"fmt"
	"io"
	"os"
	"os/signal"
	"runtime"
	"syscall"

	"github.com/jhnoel/documentdriver/tools/internal/officeapp"
)

// main installs signal cleanup and runs the smoke command then exits.
func main() {
	a := officeapp.NewMacApp(smoke.App)
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt, syscall.SIGTERM, syscall.SIGHUP)
	go func() {
		s := <-sig
		if a.WasLaunched() {
			if err := a.ForceQuit(); err != nil {
				fmt.Fprintf(os.Stderr, "word-smoke: force quit: %v\n", err)
			}
		}
		fmt.Fprintf(os.Stderr, "word-smoke: %v: Word force-quit\n", s)
		os.Exit(130)
	}()
	os.Exit(run(a, os.Args[1:], os.Stdout, os.Stderr))
}

// run checks help and platform, then runs the smoke command.
func run(a officeapp.App, args []string, stdout, stderr io.Writer) int {
	if len(args) > 0 && (args[0] == "-h" || args[0] == "--help") {
		fmt.Fprint(stdout, usage)
		return 0
	}
	if runtime.GOOS != "darwin" {
		fmt.Fprintln(stderr, "word-smoke: needs macOS with Microsoft Word")
		return 2
	}
	return smoke.Main(a, args, stdout, stderr)
}
