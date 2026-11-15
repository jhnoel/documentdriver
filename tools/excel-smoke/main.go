// Command excel-smoke opens .xlsx and .xlsm files in Microsoft Excel on macOS and
// reports whether Excel asked to repair them, modelled on tools/word-smoke.
//
//	excel-smoke [--timeout 30] [--json] [--workdir dir] --source in.xlsx out.xlsx...
//
// Each file is copied flat into the work folder Excel has been granted
// (<repo>/corpus/pptoracle, or $PPTORACLE_WORKDIR, or --workdir: the
// folder PowerPoint and Word use; Excel is granted it once) and opened
// there in a fresh Excel; Excel reads nothing outside that folder. The tool
// FORCE-QUITS Excel before and after each file: never run it while
// the maintainer may have Excel open, and only after they agree (it
// refuses to start while Excel runs).
//
// make excel-smoke SOURCE=in.xlsx OUT="out.xlsx ..." runs it through
// run.sh, which refuses while Excel runs, builds the tool and keeps the
// JSON report under results/excel-smoke/. Files that are missing or not
// .xlsx or .xlsm are refused before Excel is touched.
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

// main runs excel-smoke, force-quitting Excel on signals if it was launched.
func main() {
	a := officeapp.NewMacApp(smoke.App)
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt, syscall.SIGTERM, syscall.SIGHUP)
	go func() {
		s := <-sig
		if a.WasLaunched() {
			if err := a.ForceQuit(); err != nil {
				fmt.Fprintf(os.Stderr, "excel-smoke: force quit: %v\n", err)
			}
		}
		fmt.Fprintf(os.Stderr, "excel-smoke: %v: Excel force-quit\n", s)
		os.Exit(130)
	}()
	os.Exit(run(a, os.Args[1:], os.Stdout, os.Stderr))
}

// run shows usage for help flags and dispatches to the smoke on macOS only.
func run(a officeapp.App, args []string, stdout, stderr io.Writer) int {
	if len(args) > 0 && (args[0] == "-h" || args[0] == "--help") {
		fmt.Fprint(stdout, usage)
		return 0
	}
	if runtime.GOOS != "darwin" {
		fmt.Fprintln(stderr, "excel-smoke: needs macOS with Microsoft Excel")
		return 2
	}
	return smoke.Main(a, args, stdout, stderr)
}
